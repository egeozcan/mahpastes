package app

import (
	"bytes"
	"context"
	"database/sql"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	_ "modernc.org/sqlite"
)

// seedRingEnvelope encrypts a payload at seq and stores it as a ring row with
// a fresh (in-TTL) timestamp.
func seedRingEnvelope(t *testing.T, db *sql.DB, pubID int64, symkey, shareID []byte, seq uint64, kind string, payload any) {
	t.Helper()
	pt, err := MarshalPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	env, err := EncryptEnvelope(symkey, shareID, seq, pt)
	if err != nil {
		t.Fatal(err)
	}
	if err := RingInsert(db, pubID, seq, kind, env, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
}

func seedRingClipStart(t *testing.T, db *sql.DB, pubID int64, symkey, shareID []byte, seq, clipID uint64) {
	t.Helper()
	seedRingEnvelope(t, db, pubID, symkey, shareID, seq, KindClipStart, ClipStartPayload{
		Seq: seq, TS: time.Now().UnixMilli(), Kind: KindClipStart, ClipID: clipID,
		Filename: "a.txt", ContentType: "text/plain", Metadata: map[string]string{},
		TotalSize: 2, ChunkCount: 1,
	})
}

func seedRingClipChunk(t *testing.T, db *sql.DB, pubID int64, symkey, shareID []byte, seq, clipID uint64, index uint32, data []byte) {
	t.Helper()
	seedRingEnvelope(t, db, pubID, symkey, shareID, seq, KindClipChunk, ClipChunkPayload{
		Seq: seq, Kind: KindClipChunk, ClipID: clipID, Index: index, Data: data,
	})
}

func seedRingClipEnd(t *testing.T, db *sql.DB, pubID int64, symkey, shareID []byte, seq, clipID uint64) {
	t.Helper()
	seedRingEnvelope(t, db, pubID, symkey, shareID, seq, KindClipEnd, ClipEndPayload{
		Seq: seq, Kind: KindClipEnd, ClipID: clipID, SHA256: bytes.Repeat([]byte{0}, 32),
	})
}

// readEnvelopeAt reads the next frame and decrypts it at the given seq,
// returning the wire kind and the plaintext.
func readEnvelopeAt(t *testing.T, s network.Stream, symkey, shareID []byte, seq uint64) (string, []byte) {
	t.Helper()
	_ = s.SetReadDeadline(time.Now().Add(3 * time.Second))
	frame, err := ReadFrame(s)
	if err != nil {
		t.Fatalf("read frame at seq %d: %v", seq, err)
	}
	pt, err := DecryptEnvelope(symkey, shareID, seq, frame)
	if err != nil {
		t.Fatalf("decrypt at seq %d: %v", seq, err)
	}
	kind, _, err := PeekPayloadKind(pt)
	if err != nil {
		t.Fatalf("peek kind at seq %d: %v", seq, err)
	}
	return kind, pt
}

// newTestPublication inserts an active share row with the given last_seq and
// registers it in the manager, returning the publication id.
func newTestPublication(t *testing.T, m *ShareManager, db *sql.DB, symkey, shareID []byte, lastSeq int64) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, ?, 'active', ?)`,
		symkey, shareID, lastSeq, time.Now().Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}
	pubID, _ := res.LastInsertId()
	m.registerPublication(pubID, 1, shareID, symkey, "active")
	return pubID
}

func TestPlanRetransmit(t *testing.T) {
	start := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipStart} }
	chunk := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipChunk} }
	end := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipEnd} }

	cases := []struct {
		name        string
		rows        []RingRowMeta
		sinceSeq    uint64
		pubLastSeq  uint64
		wantFirst   uint64 // 0 == expect nothing to send
		wantCount   int
		wantGapTo   uint64 // 0 == expect no gap
		description string
	}{
		{
			name:        "contiguous ring needs no gap",
			rows:        []RingRowMeta{start(1), chunk(2), end(3)},
			sinceSeq:    0,
			pubLastSeq:  3,
			wantFirst:   1,
			wantCount:   3,
			wantGapTo:   0,
			description: "first row is exactly sinceSeq+1",
		},
		{
			name:        "everything evicted gaps to last_seq",
			rows:        nil,
			sinceSeq:    0,
			pubLastSeq:  5,
			wantFirst:   0,
			wantCount:   0,
			wantGapTo:   5,
			description: "no rows survive but the publication has moved on",
		},
		{
			name:        "head evicted gaps to first surviving clip_start minus one",
			rows:        []RingRowMeta{start(7), chunk(8), end(9)},
			sinceSeq:    0,
			pubLastSeq:  9,
			wantFirst:   7,
			wantCount:   3,
			wantGapTo:   6,
			description: "follower must land on 7 after the gap",
		},
		{
			name:        "orphan head rows are skipped",
			rows:        []RingRowMeta{chunk(5), end(6), start(7), chunk(8), end(9)},
			sinceSeq:    0,
			pubLastSeq:  9,
			wantFirst:   7,
			wantCount:   3,
			wantGapTo:   6,
			description: "5 and 6 belong to a clip whose clip_start was evicted",
		},
		{
			name:        "all-orphan ring gaps past every surviving row",
			rows:        []RingRowMeta{chunk(5), end(6)},
			sinceSeq:    0,
			pubLastSeq:  6,
			wantFirst:   0,
			wantCount:   0,
			wantGapTo:   6,
			description: "nothing assemblable survives",
		},
		{
			name:        "caught-up follower gets neither rows nor gap",
			rows:        nil,
			sinceSeq:    9,
			pubLastSeq:  9,
			wantFirst:   0,
			wantCount:   0,
			wantGapTo:   0,
			description: "since_seq >= last_seq is the steady state",
		},
		{
			name:        "single-seq hole still produces a gap",
			rows:        []RingRowMeta{start(3), end(4)},
			sinceSeq:    1,
			pubLastSeq:  4,
			wantFirst:   3,
			wantCount:   2,
			wantGapTo:   2,
			description: "gap target may equal sinceSeq+1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			send, gapTarget := planRetransmit(tc.rows, tc.sinceSeq, tc.pubLastSeq)
			if len(send) != tc.wantCount {
				t.Fatalf("send count %d want %d (%s)", len(send), tc.wantCount, tc.description)
			}
			if tc.wantFirst != 0 && send[0].Seq != tc.wantFirst {
				t.Fatalf("first sent seq %d want %d", send[0].Seq, tc.wantFirst)
			}
			if gapTarget != tc.wantGapTo {
				t.Fatalf("gap target %d want %d (%s)", gapTarget, tc.wantGapTo, tc.description)
			}
			// A gap must always land the follower exactly one seq below the
			// first row it is about to receive, or the follower re-bricks.
			if gapTarget != 0 && len(send) > 0 && gapTarget+1 != send[0].Seq {
				t.Fatalf("gap target %d does not align with first row seq %d", gapTarget, send[0].Seq)
			}
		})
	}
}

// TestPublisherSynthesizesGapForEmptyRing covers the "everything evicted"
// shape: the follower's first frame must be a gap sealed at since_seq+1 that
// carries it up to the publication's last_seq.
func TestPublisherSynthesizesGapForEmptyRing(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m, err := NewShareManager(ctx, db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xAA}, 32)
	shareID := DeriveShareID(symkey)
	newTestPublication(t, m, db, symkey, shareID, 5)

	follower, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	defer s.Close()

	// Sealed at since_seq+1 = 1, which is the only seq the follower will try.
	kind, pt := readEnvelopeAt(t, s, symkey, shareID, 1)
	if kind != KindGap {
		t.Fatalf("first frame kind %q want %q", kind, KindGap)
	}
	var gap GapPayload
	if err := UnmarshalPayload(pt, &gap); err != nil {
		t.Fatal(err)
	}
	if gap.Seq != 5 {
		t.Fatalf("gap target %d want 5 (shares.last_seq)", gap.Seq)
	}
}

// TestPublisherSkipsOrphanHeadRowsAndGaps covers the "head evicted mid-clip"
// shape: rows whose clip_start is gone are dropped, and the gap lands the
// follower one seq below the first surviving clip_start.
func TestPublisherSkipsOrphanHeadRowsAndGaps(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m, err := NewShareManager(ctx, db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xBB}, 32)
	shareID := DeriveShareID(symkey)
	pubID := newTestPublication(t, m, db, symkey, shareID, 9)

	// Clip 1's header was evicted; only its tail survives (orphans).
	seedRingClipChunk(t, db, pubID, symkey, shareID, 5, 1, 0, []byte("orphan"))
	seedRingClipEnd(t, db, pubID, symkey, shareID, 6, 1)
	// Clip 2 survives whole.
	seedRingClipStart(t, db, pubID, symkey, shareID, 7, 2)
	seedRingClipChunk(t, db, pubID, symkey, shareID, 8, 2, 0, []byte("keep"))
	seedRingClipEnd(t, db, pubID, symkey, shareID, 9, 2)

	follower, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	defer s.Close()

	kind, pt := readEnvelopeAt(t, s, symkey, shareID, 1)
	if kind != KindGap {
		t.Fatalf("first frame kind %q want %q", kind, KindGap)
	}
	var gap GapPayload
	if err := UnmarshalPayload(pt, &gap); err != nil {
		t.Fatal(err)
	}
	if gap.Seq != 6 {
		t.Fatalf("gap target %d want 6 (one below the surviving clip_start)", gap.Seq)
	}

	// The orphan rows at 5 and 6 must NOT be on the wire — the next three
	// frames are clip 2, contiguous from 7.
	if k, _ := readEnvelopeAt(t, s, symkey, shareID, 7); k != KindClipStart {
		t.Fatalf("frame at seq 7 kind %q want %q", k, KindClipStart)
	}
	k, chunkPT := readEnvelopeAt(t, s, symkey, shareID, 8)
	if k != KindClipChunk {
		t.Fatalf("frame at seq 8 kind %q want %q", k, KindClipChunk)
	}
	if !bytes.Contains(chunkPT, []byte("keep")) {
		t.Fatal("frame at seq 8 is not clip 2's chunk")
	}
	if k, _ := readEnvelopeAt(t, s, symkey, shareID, 9); k != KindClipEnd {
		t.Fatalf("frame at seq 9 kind %q want %q", k, KindClipEnd)
	}
}

// TestFollowerResumesAcrossLargeSeqGap is the end-to-end regression for the
// brick: a brand-new follower handshakes at since_seq=0 against a publication
// whose history is 100 seqs ahead. The old follower brute-forced only
// lastSeq+1..lastSeq+31, so every decrypt failed, the session errored, and
// runFollowLoop reconnected into the identical failure forever. With the
// publisher synthesizing a gap the follower lands on the live seq and
// assembles the clip.
func TestFollowerResumesAcrossLargeSeqGap(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, err := NewShareManager(ctx, pubDB, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pubM.Stop()
	if _, err := pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := pubM.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}

	// Stand in for a long-lived publication whose early history has already
	// aged out of the ring: 100 seqs published, none of them retained. The
	// gap from since_seq=0 is far wider than the old 31-seq scan window.
	if _, err := pubDB.Exec(`UPDATE shares SET last_seq = 100 WHERE id = ?`, info.ID); err != nil {
		t.Fatal(err)
	}

	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer fM.Stop()
	if _, err := fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	followInfo, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	// Publish a clip — it occupies seqs 101..103.
	r, err := pubDB.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', 'after the gap', 'a.txt', '{}')`)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	if _, err := pubDB.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
		t.Fatal(err)
	}
	if err := pubM.OnClipCreated(clipID, []int64{1}); err != nil {
		t.Fatalf("OnClipCreated: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var data []byte
		if err := fDB.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&data); err == nil && string(data) == "after the gap" {
			// The follower must have jumped the whole gap, not crawled it.
			var lastSeq int64
			if err := fDB.QueryRow(`SELECT last_seq FROM follows WHERE id = ?`, followInfo.ID).Scan(&lastSeq); err != nil {
				t.Fatal(err)
			}
			if lastSeq != 103 {
				t.Fatalf("follower last_seq %d want 103 (gap to 100 + clip at 101..103)", lastSeq)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("follower never assembled the clip published beyond the seq gap")
}

// senderGoroutines counts the followerConn sender goroutines currently parked
// or running. A leaked one is invisible to every other assertion — the map
// entry is gone, the stream is reset, and only the stack shows it is still
// there — so this reads the stacks directly.
func senderGoroutines() int {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), "followerConn).runSender")
		}
		buf = make([]byte, 2*len(buf))
	}
}

// TestHandshakeEncodeFailureClosesFollowerConn drives the gap-envelope
// encryption failure inside handlePublisherStream and asserts the connection is
// torn down completely, not just unregistered.
//
// The publication is given a 16-byte symkey. DeriveShareID hashes whatever it
// is handed and the handshake HMAC takes any key length, so the handshake
// verifies and the handler runs all the way to synthesizing the catch-up gap —
// where EncryptEnvelope rejects the key size. That is the exact error path,
// reached deterministically rather than by fault injection.
func TestHandshakeEncodeFailureClosesFollowerConn(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m, err := NewShareManager(ctx, db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xD1}, 16)
	shareID := DeriveShareID(symkey)
	// last_seq above the follower's since_seq with an empty ring is what makes
	// the handler synthesize a gap at all.
	pubID := newTestPublication(t, m, db, symkey, shareID, 5)

	// Proves the handler got past HMAC verification and into the registration
	// section, so the followerConn below really was constructed. Without this
	// the goroutine assertion could pass by never having created one.
	var reached atomic.Bool
	m.testHookBeforeRegister = func(*publication) { reached.Store(true) }

	// Baseline rather than an absolute count: an unrelated leak elsewhere in
	// the suite must not decide this test.
	base := senderGoroutines()

	follower, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	defer s.Close()

	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := ReadFrame(s); err == nil {
		t.Fatal("publisher sent a frame although it could not seal the gap envelope")
	}
	if !reached.Load() {
		t.Fatal("handshake never reached registration — the test did not exercise the error path")
	}
	if !m.waitForFollowers(pubID, 0, 5*time.Second) {
		t.Fatal("failed handshake left its follower registered")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if senderGoroutines() <= base {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sender goroutine still parked after the handshake failed (%d, baseline %d)", senderGoroutines(), base)
}
