package app

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	_ "modernc.org/sqlite"
)

// recordingWriter captures everything the send scheduler writes. Stands in for
// a libp2p stream so scheduler behaviour can be asserted without a transport.
type recordingWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *recordingWriter) bytesWritten() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// ringRowOfSize builds the metadata of a ring row whose envelope occupies
// exactly size bytes, so budget arithmetic in the planner tests is exact.
func ringRowOfSize(seq uint64, kind string, size int) RingRowMeta {
	return RingRowMeta{Seq: seq, Kind: kind, ByteLen: int64(size)}
}

// clipRows builds one clip's worth of ring row metadata (start, chunk, end)
// starting at startSeq, each envelope `each` bytes.
func clipRows(startSeq uint64, each int) []RingRowMeta {
	return []RingRowMeta{
		ringRowOfSize(startSeq, KindClipStart, each),
		ringRowOfSize(startSeq+1, KindClipChunk, each),
		ringRowOfSize(startSeq+2, KindClipEnd, each),
	}
}

func generousCaps() catchupCaps {
	return catchupCaps{softBytes: 1 << 30, softSlots: 1 << 20, hardBytes: 1 << 30, hardSlots: 1 << 20}
}

func TestPlanCatchupBatch(t *testing.T) {
	// Three 3-row clips at seqs 1..3, 4..6, 7..9, 100 bytes per envelope.
	threeClips := func() []RingRowMeta {
		var out []RingRowMeta
		out = append(out, clipRows(1, 100)...)
		out = append(out, clipRows(4, 100)...)
		out = append(out, clipRows(7, 100)...)
		return out
	}

	cases := []struct {
		name          string
		rows          []RingRowMeta
		sinceSeq      uint64
		pubLastSeq    uint64
		caps          catchupCaps
		wantSendSeqs  []uint64
		wantGapTo     uint64
		wantTruncated bool
		wantSkipped   []seqRange
		why           string
	}{
		{
			name:         "everything fits in one batch",
			rows:         threeClips(),
			pubLastSeq:   9,
			caps:         generousCaps(),
			wantSendSeqs: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9},
			why:          "a backlog inside budget behaves exactly as before batching",
		},
		{
			name:          "slot budget cuts on a clip boundary",
			rows:          threeClips(),
			pubLastSeq:    9,
			caps:          catchupCaps{softBytes: 1 << 30, softSlots: 7, hardBytes: 1 << 30, hardSlots: 1 << 20},
			wantSendSeqs:  []uint64{1, 2, 3, 4, 5, 6},
			wantTruncated: true,
			why:           "7 slots holds two whole clips, never two clips plus a stray clip_start",
		},
		{
			name:          "byte budget cuts on a clip boundary",
			rows:          threeClips(),
			pubLastSeq:    9,
			caps:          catchupCaps{softBytes: 500, softSlots: 1 << 20, hardBytes: 1 << 30, hardSlots: 1 << 20},
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			why:           "500 bytes fits one 300-byte clip; the second would overshoot",
		},
		{
			name:          "clip over the soft budget is sent alone",
			rows:          threeClips(),
			pubLastSeq:    9,
			caps:          catchupCaps{softBytes: 100, softSlots: 1 << 20, hardBytes: 1 << 30, hardSlots: 1 << 20},
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			why:           "no clip fits the soft budget, but a truncated batch owns the whole queue",
		},
		{
			name:          "over-soft clip as the final group is still drain-closed",
			rows:          clipRows(1, 100),
			pubLastSeq:    3,
			caps:          catchupCaps{softBytes: 100, softSlots: 1 << 20, hardBytes: 1 << 30, hardSlots: 1 << 20},
			wantSendSeqs:  []uint64{1, 2, 3},
			wantTruncated: true,
			why:           "an over-soft clip owns the connection wherever it falls: staying registered would let a live envelope enqueue behind a near-full queue and reset the conn mid-drain",
		},
		{
			name: "clip over the hard budget is skipped and the gap covers it",
			// A 900-byte head clip followed by two 300-byte ones.
			rows:       append(append(clipRows(1, 300), clipRows(4, 100)...), clipRows(7, 100)...),
			pubLastSeq: 9,
			caps:       catchupCaps{softBytes: 400, softSlots: 1 << 20, hardBytes: 500, hardSlots: 1 << 20},
			// Clip 2 is batched; clip 3 is merely deferred, not skipped.
			wantSendSeqs:  []uint64{4, 5, 6},
			wantGapTo:     3,
			wantTruncated: true,
			wantSkipped:   []seqRange{{Start: 1, End: 3}},
			why:           "an undeliverable head clip must not stall the rest of the backlog forever",
		},
		{
			name:          "consecutive undeliverable clips each extend the gap",
			rows:          threeClips(),
			pubLastSeq:    9,
			caps:          catchupCaps{softBytes: 100, softSlots: 2, hardBytes: 1 << 30, hardSlots: 2},
			wantSendSeqs:  nil,
			wantGapTo:     9,
			wantTruncated: false,
			wantSkipped:   []seqRange{{Start: 1, End: 3}, {Start: 4, End: 6}, {Start: 7, End: 9}},
			why:           "3-row clips never fit a 2-slot hard cap; all three are skipped in one pass",
		},
		{
			name:          "head eviction gap survives batching",
			rows:          append(clipRows(7, 100), clipRows(10, 100)...),
			pubLastSeq:    12,
			caps:          catchupCaps{softBytes: 1 << 30, softSlots: 3, hardBytes: 1 << 30, hardSlots: 1 << 20},
			wantSendSeqs:  []uint64{7, 8, 9},
			wantGapTo:     6,
			wantTruncated: true,
			why:           "the gap from planRetransmit and the batch cut must both hold",
		},
		{
			name:         "orphan head rows are still dropped",
			rows:         append([]RingRowMeta{ringRowOfSize(5, KindClipChunk, 100), ringRowOfSize(6, KindClipEnd, 100)}, clipRows(7, 100)...),
			pubLastSeq:   9,
			caps:         generousCaps(),
			wantSendSeqs: []uint64{7, 8, 9},
			wantGapTo:    6,
			why:          "rows whose clip_start was evicted stay unsendable",
		},
		{
			name:       "empty ring gaps to the publication head",
			rows:       nil,
			pubLastSeq: 5,
			caps:       generousCaps(),
			wantGapTo:  5,
			why:        "unchanged from planRetransmit",
		},
		{
			name:       "caught-up follower gets nothing",
			rows:       nil,
			sinceSeq:   9,
			pubLastSeq: 9,
			caps:       generousCaps(),
			why:        "steady state must not synthesize a gap",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planCatchupBatch(tc.rows, tc.sinceSeq, tc.pubLastSeq, tc.caps, false)

			var gotSeqs []uint64
			for _, r := range plan.Send {
				gotSeqs = append(gotSeqs, r.Seq)
			}
			if fmt.Sprint(gotSeqs) != fmt.Sprint(tc.wantSendSeqs) {
				t.Fatalf("send seqs %v want %v (%s)", gotSeqs, tc.wantSendSeqs, tc.why)
			}
			if plan.GapTarget != tc.wantGapTo {
				t.Fatalf("gap target %d want %d (%s)", plan.GapTarget, tc.wantGapTo, tc.why)
			}
			if plan.Truncated != tc.wantTruncated {
				t.Fatalf("truncated %v want %v (%s)", plan.Truncated, tc.wantTruncated, tc.why)
			}
			if fmt.Sprint(plan.Skipped) != fmt.Sprint(tc.wantSkipped) {
				t.Fatalf("skipped %v want %v (%s)", plan.Skipped, tc.wantSkipped, tc.why)
			}
			// The gap is sealed at sinceSeq+1 and tells the follower where to
			// land; if it does not land exactly one below the first batched
			// row, every row that follows fails to decrypt.
			if plan.GapTarget != 0 && len(plan.Send) > 0 && plan.GapTarget+1 != plan.Send[0].Seq {
				t.Fatalf("gap target %d does not align with first batched seq %d", plan.GapTarget, plan.Send[0].Seq)
			}
			// A batch may only ever end on a clip_end, or the follower's
			// assembler is left holding half a clip.
			if plan.Truncated && len(plan.Send) > 0 {
				if last := plan.Send[len(plan.Send)-1]; last.Kind != KindClipEnd {
					t.Fatalf("truncated batch ends on %q at seq %d, want %q", last.Kind, last.Seq, KindClipEnd)
				}
			}
			// Truncation with nothing to send would reconnect forever.
			if plan.Truncated && len(plan.Send) == 0 {
				t.Fatal("truncated with an empty batch: the follower would loop without making progress")
			}
			// The batch's blobs are fetched with ONE range query over
			// [first seq, last seq], which is only equivalent to the plan if
			// the batch is a contiguous run of the surviving rows. If any
			// input row fell inside that span unsent, the fetch would return
			// it too and fetchPlannedEnvelopes would reject the whole batch.
			if len(plan.Send) > 0 {
				first, last := plan.Send[0].Seq, plan.Send[len(plan.Send)-1].Seq
				var inSpan, sent []uint64
				for _, r := range tc.rows {
					if r.Seq >= first && r.Seq <= last {
						inSpan = append(inSpan, r.Seq)
					}
				}
				for _, r := range plan.Send {
					sent = append(sent, r.Seq)
				}
				if fmt.Sprint(inSpan) != fmt.Sprint(sent) {
					t.Fatalf("batch %d..%d sends %v but the ring holds %v in that span — the range fetch is not equivalent to the plan", first, last, sent, inSpan)
				}
			}
		})
	}
}

// TestPlanCatchupBatchNeverSplitsAClipUnderProductionCaps is the guard for the
// numbers themselves: the default budget must always admit at least one
// ordinary clip, or every catch-up would degenerate into a skip.
func TestPlanCatchupBatchAdmitsAFullSizeClipUnderDefaultCaps(t *testing.T) {
	// A 30 MiB clip: clip_start + 30 × 1 MiB chunks + clip_end.
	rows := []RingRowMeta{ringRowOfSize(1, KindClipStart, 512)}
	for i := 0; i < 30; i++ {
		rows = append(rows, ringRowOfSize(uint64(2+i), KindClipChunk, ChunkSize+64))
	}
	rows = append(rows, ringRowOfSize(32, KindClipEnd, 128))

	plan := planCatchupBatch(rows, 0, 32, defaultCatchupCaps(), false)
	if len(plan.Skipped) != 0 {
		t.Fatalf("a 30 MiB clip was skipped as undeliverable: %v", plan.Skipped)
	}
	if len(plan.Send) != len(rows) {
		t.Fatalf("sent %d of %d rows; a single clip must go out whole", len(plan.Send), len(rows))
	}
	// 30 MiB is past the 16 MiB soft cap, so this batch is hard-tier: it must
	// be drain-closed even though it is the only group in the backlog.
	if !plan.Truncated {
		t.Fatal("a hard-tier batch was not flagged truncated; live fan-out on top of it could overflow the send queue")
	}
}

// TestEmitFanOutHoldsUntilCommit is the regression for the durability
// inversion: envelopes used to reach followers per-INSERT, so a failed commit
// left the publisher rolled back while followers had already stored the clip
// and advanced past its seqs. The next emission then reused those seqs and the
// follow desynced permanently.
func TestEmitFanOutHoldsUntilCommit(t *testing.T) {
	db := newTestDB(t)
	m := &ShareManager{db: db, logs: newShareLogBuffer()}

	symkey := bytes.Repeat([]byte{0xC1}, 32)
	shareID := DeriveShareID(symkey)
	res, err := db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`,
		symkey, shareID, time.Now().Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}
	pubID, _ := res.LastInsertId()
	p := &publication{
		id: pubID, tagID: 1, shareID: shareID, symkey: symkey,
		status: "active", followers: map[network.Stream]*followerConn{},
	}

	w := &recordingWriter{}
	fc := newFollowerConn(nil, w)
	defer fc.close()
	p.followers[nil] = fc

	payload := "rolled back"
	res, err = db.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, 'a.txt', '{}')`, payload)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := res.LastInsertId()

	// Fail the emission at the very last write before commit — the point
	// where every envelope has already been built and (previously) sent.
	if _, err := db.Exec(
		`CREATE TRIGGER fail_last_seq BEFORE UPDATE OF last_seq ON shares BEGIN SELECT RAISE(ABORT, 'injected'); END;`,
	); err != nil {
		t.Fatal(err)
	}
	if err := m.emitClipForPublication(p, clipID, "text/plain", "a.txt", map[string]string{}, int64(len(payload)), 1); err == nil {
		t.Fatal("emission with a failing last_seq update should have returned an error")
	}

	// Give the sender goroutine a chance to write anything it was handed.
	time.Sleep(150 * time.Millisecond)
	if got := w.bytesWritten(); len(got) != 0 {
		t.Fatalf("%d bytes reached the follower from a rolled-back emission", len(got))
	}
	var ringRows, lastSeq int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, pubID).Scan(&ringRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, pubID).Scan(&lastSeq); err != nil {
		t.Fatal(err)
	}
	if ringRows != 0 || lastSeq != 0 {
		t.Fatalf("rollback left ring rows=%d last_seq=%d, want 0/0", ringRows, lastSeq)
	}

	// The seqs the failed emission would have burned are reused by the next
	// one. A follower that had received the first attempt would now fail to
	// decrypt; one that received nothing consumes this cleanly at 1..3.
	if _, err := db.Exec(`DROP TRIGGER fail_last_seq`); err != nil {
		t.Fatal(err)
	}
	payload2 := "committed"
	res, err = db.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, 'b.txt', '{}')`, payload2)
	if err != nil {
		t.Fatal(err)
	}
	clipID2, _ := res.LastInsertId()
	if err := m.emitClipForPublication(p, clipID2, "text/plain", "b.txt", map[string]string{}, int64(len(payload2)), 1); err != nil {
		t.Fatalf("second emission: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var wire []byte
	for time.Now().Before(deadline) {
		wire = w.bytesWritten()
		if len(wire) > 0 {
			// Wait for all three envelopes to land.
			time.Sleep(50 * time.Millisecond)
			wire = w.bytesWritten()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	r := bytes.NewReader(wire)
	for _, step := range []struct {
		seq  uint64
		kind string
	}{{1, KindClipStart}, {2, KindClipChunk}, {3, KindClipEnd}} {
		frame, err := ReadFrame(r)
		if err != nil {
			t.Fatalf("read frame at seq %d: %v", step.seq, err)
		}
		pt, err := DecryptEnvelope(symkey, shareID, step.seq, frame)
		if err != nil {
			t.Fatalf("decrypt at seq %d: %v — the failed emission burned this seq", step.seq, err)
		}
		kind, _, err := PeekPayloadKind(pt)
		if err != nil {
			t.Fatal(err)
		}
		if kind != step.kind {
			t.Fatalf("seq %d kind %q want %q", step.seq, kind, step.kind)
		}
	}
}

// TestFollowerConnFinishAfterDrain covers the graceful "finish then close"
// mode: everything queued is written, the connection then closes itself, and
// nothing can be enqueued behind it (which would panic on a closed channel).
func TestFollowerConnFinishAfterDrain(t *testing.T) {
	w := &recordingWriter{}
	fc := newFollowerConn(nil, w)

	closed := make(chan struct{})
	fc.onClose = func() { close(closed) }

	for i := 0; i < 5; i++ {
		fc.enqueue([]byte{byte(i)})
	}
	fc.finishAfterDrain()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("connection never closed after its queue drained")
	}
	if got := w.bytesWritten(); !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("wrote %v, want every queued envelope in order", got)
	}
	if !fc.isDraining() {
		t.Fatal("isDraining false after finishAfterDrain")
	}
	// Must be a no-op rather than a send on a closed channel.
	fc.enqueue([]byte{9})
	// close() after the sender already closed must stay idempotent.
	fc.close()
	if got := w.bytesWritten(); len(got) != 5 {
		t.Fatalf("wrote %d bytes after draining, want 5", len(got))
	}
}

// TestLiveFanOutSkipsDrainingFollower: a follower finishing a truncated batch
// is behind on purpose. Splicing live envelopes onto it would put them on the
// far side of a seq hole it cannot decrypt across.
func TestLiveFanOutSkipsDrainingFollower(t *testing.T) {
	m := &ShareManager{}

	// followers is keyed by network.Stream and these conns are streamless, so
	// each one gets its own single-entry publication. The assertion is about
	// liveFanOutLocked's per-conn decision, not about map contents.
	fanOutTo := func(fc *followerConn) {
		p := &publication{followers: map[network.Stream]*followerConn{nil: fc}}
		p.fmu.Lock()
		m.liveFanOutLocked(p, []byte("live"))
		p.fmu.Unlock()
	}

	live := newFollowerConn(nil, &recordingWriter{})
	defer live.close()
	fanOutTo(live)

	draining := newFollowerConn(nil, &recordingWriter{})
	draining.finishAfterDrain()
	fanOutTo(draining)

	time.Sleep(150 * time.Millisecond)
	if got := live.writer.(*recordingWriter).bytesWritten(); !bytes.Equal(got, []byte("live")) {
		t.Fatalf("connected follower got %q, want the live envelope", got)
	}
	if got := draining.writer.(*recordingWriter).bytesWritten(); len(got) != 0 {
		t.Fatalf("draining follower got %q, want nothing — it is behind on purpose", got)
	}
}

// TestCatchupBacklogConvergesAcrossBatches is the end-to-end proof for the
// overflow fix: a backlog several batches deep is delivered completely, via
// graceful close-and-reconnect cycles, with no clip lost and no churn.
//
// The catch-up budget is shrunk for the test so a handful of tiny clips spans
// several batches instead of the 16 MiB the production budget would need.
func TestCatchupBacklogConvergesAcrossBatches(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, err := NewShareManager(ctx, pubDB, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pubM.Stop()
	// Six slots = two 3-envelope clips per batch.
	pubM.setCatchupCapsForTest(catchupCaps{
		softBytes: 1 << 20, softSlots: 6,
		hardBytes: CatchupHardBytesCap, hardSlots: CatchupHardEnvelopesCap,
	})

	if _, err := pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := pubM.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}

	// Publish the whole backlog before anyone follows, so all of it must
	// come back out of the ring on handshake.
	const backlog = 6
	for i := 0; i < backlog; i++ {
		body := fmt.Sprintf("backlog clip %d", i)
		r, err := pubDB.Exec(
			`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, ?, '{}')`,
			body, fmt.Sprintf("clip-%d.txt", i))
		if err != nil {
			t.Fatal(err)
		}
		clipID, _ := r.LastInsertId()
		if _, err := pubDB.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
			t.Fatal(err)
		}
		if err := pubM.OnClipCreated(clipID, []int64{1}); err != nil {
			t.Fatal(err)
		}
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

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := fDB.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= backlog {
			// Every clip, exactly once, and the durable boundary parked at
			// the publisher's head rather than mid-backlog.
			var distinct int
			if err := fDB.QueryRow(`SELECT COUNT(DISTINCT data) FROM clips`).Scan(&distinct); err != nil {
				t.Fatal(err)
			}
			if distinct != backlog || n != backlog {
				t.Fatalf("follower holds %d clips (%d distinct), want exactly %d", n, distinct, backlog)
			}
			var followLast, pubLast int64
			if err := fDB.QueryRow(`SELECT last_seq FROM follows WHERE id = ?`, followInfo.ID).Scan(&followLast); err != nil {
				t.Fatal(err)
			}
			if err := pubDB.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, info.ID).Scan(&pubLast); err != nil {
				t.Fatal(err)
			}
			if followLast != pubLast {
				t.Fatalf("follower last_seq %d, publisher last_seq %d — backlog did not fully converge", followLast, pubLast)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	var n int
	_ = fDB.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n)
	t.Fatalf("follower received %d of %d backlogged clips", n, backlog)
}

// TestPlanCatchupBatchRewindsOnPublisherRegression pins the publisher half of
// the seq-regression fix. A publication restored from an older backup under
// identity policy "takeover" keeps its share_id and symkey but comes back with
// a last_seq BELOW what an already-synced follower has consumed. Before the
// fix planRetransmit's forward-only guard (pubLastSeq > sinceSeq) returned no
// gap and no rows, so the handshake produced an empty answer, the follower sat
// connected at a seq the publisher would never reach again, and every
// reconnect reproduced it exactly — a permanent brick.
func TestPlanCatchupBatchRewindsOnPublisherRegression(t *testing.T) {
	start := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipStart} }
	chunk := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipChunk} }
	end := func(seq uint64) RingRowMeta { return RingRowMeta{Seq: seq, Kind: KindClipEnd} }

	cases := []struct {
		name       string
		rows       []RingRowMeta
		sinceSeq   uint64
		pubLastSeq uint64
		wantRewind bool
		wantGapTo  uint64
		wantSend   int
		why        string
	}{
		{
			name:       "restored backup leaves the follower ahead of the publisher",
			sinceSeq:   100,
			pubLastSeq: 50,
			wantRewind: true,
			wantGapTo:  50,
			why:        "the follower must be carried back to the publisher's head, not left at 100",
		},
		{
			name:       "publisher reset all the way to zero still rewinds",
			sinceSeq:   7,
			pubLastSeq: 0,
			wantRewind: true,
			wantGapTo:  0,
			why:        "gap target 0 is a real instruction here, not the no-gap sentinel",
		},
		{
			name:       "one seq behind is still a regression",
			sinceSeq:   51,
			pubLastSeq: 50,
			wantRewind: true,
			wantGapTo:  50,
		},
		{
			name:       "caught up exactly is not a regression",
			sinceSeq:   50,
			pubLastSeq: 50,
			wantRewind: false,
			wantGapTo:  0,
			why:        "the follower is level with the publisher — idle, no gap, unchanged behaviour",
		},
		{
			name:       "rows above a regressed last_seq are dropped",
			rows:       []RingRowMeta{start(101), chunk(102), end(103)},
			sinceSeq:   100,
			pubLastSeq: 50,
			wantRewind: true,
			wantGapTo:  50,
			wantSend:   0,
			why:        "impossible while ring writes and last_seq share a transaction; if a corrupt DB produces it, sending rows sealed above a gap that lands below them would desync the follower again",
		},
		{
			name:       "normal catch-up is untouched by the regression branch",
			rows:       []RingRowMeta{start(4), chunk(5), end(6)},
			sinceSeq:   3,
			pubLastSeq: 6,
			wantRewind: false,
			wantGapTo:  0,
			wantSend:   3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planCatchupBatch(tc.rows, tc.sinceSeq, tc.pubLastSeq, defaultCatchupCaps(), false)
			if plan.Rewind != tc.wantRewind {
				t.Fatalf("Rewind=%v want %v: %s", plan.Rewind, tc.wantRewind, tc.why)
			}
			if plan.GapTarget != tc.wantGapTo {
				t.Fatalf("GapTarget=%d want %d: %s", plan.GapTarget, tc.wantGapTo, tc.why)
			}
			if len(plan.Send) != tc.wantSend {
				t.Fatalf("Send has %d rows want %d: %s", len(plan.Send), tc.wantSend, tc.why)
			}
			if plan.Rewind && plan.Truncated {
				t.Fatal("a rewind carries no backlog, so it must not ask the caller to close after drain")
			}
		})
	}
}

// TestRewindHandshakeIsStable is the anti-oscillation check. A follower whose
// session drops right after the rewind — before any clip lands — comes back at
// the rewound boundary. That second handshake must be an ordinary idle one, or
// the pair would trade rewinds forever.
func TestRewindHandshakeIsStable(t *testing.T) {
	first := planCatchupBatch(nil, 100, 50, defaultCatchupCaps(), false)
	if !first.Rewind || first.GapTarget != 50 {
		t.Fatalf("first handshake: Rewind=%v GapTarget=%d want true and 50", first.Rewind, first.GapTarget)
	}

	second := planCatchupBatch(nil, first.GapTarget, 50, defaultCatchupCaps(), false)
	if second.Rewind {
		t.Fatal("second handshake rewound again — the follower and publisher would oscillate forever")
	}
	if second.GapTarget != 0 || len(second.Send) != 0 {
		t.Fatalf("second handshake GapTarget=%d Send=%d want a clean idle (0 and 0)", second.GapTarget, len(second.Send))
	}
}

// TestFollowerConnCloseStopsSenderGoroutine pins what the handshake error
// paths in handlePublisherStream depend on. Those paths used to drop the
// registration and reset the stream but never close the followerConn, so
// runSender stayed parked on a queue that nobody would close or feed again —
// one goroutine leaked per failed handshake, and a follower that retries on a
// backoff loop retries forever.
//
// close() has to be safe here twice over: after envelopes are already queued,
// and again after the drain goroutine in handlePublisherStream calls it on the
// way out.
func TestFollowerConnCloseStopsSenderGoroutine(t *testing.T) {
	w := &recordingWriter{}
	fc := newFollowerConn(nil, w)

	closed := make(chan struct{})
	fc.onClose = func() { close(closed) }

	fc.enqueue([]byte{1, 2, 3})
	fc.close()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("close() did not shut the connection down — runSender is still parked on its queue")
	}
	if !fc.isDraining() {
		t.Fatal("isDraining false after close — a later enqueue would send on a closed channel")
	}

	// The drain goroutine calls close() again after the error path already
	// did; a second close of the queue would panic.
	fc.close()
	fc.close()

	// And a late live fan-out must find the connection shut rather than panic.
	fc.enqueue([]byte{4})
}

// seedRingClips writes n three-envelope clips (start, chunk, end) of `each`
// bytes per envelope into publication 1's ring, at seqs 1..3n.
func seedRingClips(t *testing.T, db *sql.DB, n, each int, ts int64) {
	t.Helper()
	seq := uint64(0)
	for c := 0; c < n; c++ {
		for _, kind := range []string{KindClipStart, KindClipChunk, KindClipEnd} {
			seq++
			if err := RingInsert(db, 1, seq, kind, bytes.Repeat([]byte{byte(seq)}, each), ts); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestCatchupFetchesOnlyThePlannedBatch is the memory bound end to end. The
// ring holds far more than one batch may carry; catch-up must plan over
// metadata and then load only the batch it chose. Loading the survivors first
// and trimming afterwards — what the old single-query retransmit did — meant a
// publisher with several full 256 MiB rings could be driven out of memory by
// authorized reconnects alone.
func TestCatchupFetchesOnlyThePlannedBatch(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()

	// 12 clips × 3 envelopes × 1000 bytes = 36 rows, 36000 bytes of ring.
	const clips, each = 12, 1000
	seedRingClips(t, db, clips, each, now)
	const ringRows, ringBytes = clips * 3, clips * 3 * each

	// A batch budget several times smaller than the ring: two 3000-byte clips
	// fit the soft cap, a third does not.
	caps := catchupCaps{softBytes: 6500, softSlots: 1 << 20, hardBytes: 10000, hardSlots: 1 << 20}

	meta, _, err := RingRetransmitMeta(db, 1, 0, now, CatchupMetaRowCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != ringRows {
		t.Fatalf("metadata pass saw %d rows, want the whole ring (%d)", len(meta), ringRows)
	}

	plan := planCatchupBatch(meta, 0, ringRows, caps, false)
	if len(plan.Send) != 6 {
		t.Fatalf("planned %d envelopes, want 6 (two whole clips inside the soft budget)", len(plan.Send))
	}
	if !plan.Truncated {
		t.Fatal("a batch that left backlog behind must be truncated so the follower reconnects for the rest")
	}

	envelopes, err := fetchPlannedEnvelopes(db, 1, plan.Send, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != len(plan.Send) {
		t.Fatalf("fetched %d envelopes for a %d-envelope batch", len(envelopes), len(plan.Send))
	}
	var fetched int64
	for i, env := range envelopes {
		fetched += int64(len(env))
		if int64(len(env)) != plan.Send[i].ByteLen {
			t.Fatalf("envelope %d is %d bytes, planned %d", i, len(env), plan.Send[i].ByteLen)
		}
		// Each seeded envelope is filled with its own seq byte, so this also
		// proves the rows line up with the seqs that were planned.
		if env[0] != byte(plan.Send[i].Seq) {
			t.Fatalf("envelope %d carries the blob for seq %d, planned seq %d", i, env[0], plan.Send[i].Seq)
		}
	}
	if fetched != int64(len(plan.Send)*each) {
		t.Fatalf("fetched %d bytes for the batch, want %d", fetched, len(plan.Send)*each)
	}
	// The point of the split: the blobs pulled into memory are the batch, not
	// the backlog.
	if fetched >= ringBytes {
		t.Fatalf("fetched %d bytes but the ring only holds %d — the whole backlog was loaded", fetched, ringBytes)
	}
	if fetched > caps.hardBytes {
		t.Fatalf("fetched %d bytes, over the %d-byte hard cap that bounds a batch", fetched, caps.hardBytes)
	}
}

// TestFetchPlannedEnvelopesRejectsRacedEviction covers the one way the two
// queries can disagree. RingEvict deletes globally and runs both from the ring
// sweeper (no fmu at all) and from emitClipForPublication (the emitting
// publication's fmu only), so neither is excluded by the fmu a handshake
// holds: rows planned from metadata can be gone by the time their blobs are
// fetched. Enqueueing what survived would put a seq hole in the follower's
// stream that it can never decrypt past, so the fetch fails the handshake
// instead and the follower reconnects into a freshly planned batch.
func TestFetchPlannedEnvelopesRejectsRacedEviction(t *testing.T) {
	plan := func(t *testing.T, db *sql.DB, now int64) []RingRowMeta {
		t.Helper()
		meta, _, err := RingRetransmitMeta(db, 1, 0, now, CatchupMetaRowCap)
		if err != nil {
			t.Fatal(err)
		}
		p := planCatchupBatch(meta, 0, uint64(len(meta)), generousCaps(), false)
		if len(p.Send) != 9 {
			t.Fatalf("planned %d envelopes, want all 9", len(p.Send))
		}
		return p.Send
	}

	t.Run("unraced fetch returns the batch", func(t *testing.T) {
		db := openTestDBWithShareRing(t)
		now := time.Now().Unix()
		seedRingClips(t, db, 3, 64, now)
		send := plan(t, db, now)

		envelopes, err := fetchPlannedEnvelopes(db, 1, send, now)
		if err != nil {
			t.Fatalf("unraced fetch failed: %v", err)
		}
		if len(envelopes) != len(send) {
			t.Fatalf("fetched %d envelopes, planned %d", len(envelopes), len(send))
		}
	})

	t.Run("head trimmed by the cap sweep", func(t *testing.T) {
		db := openTestDBWithShareRing(t)
		now := time.Now().Unix()
		seedRingClips(t, db, 3, 64, now)
		send := plan(t, db, now)

		// Eviction is oldest-first, so this is the shape a raced sweep takes.
		if _, err := db.Exec(`DELETE FROM share_ring WHERE publication_id = 1 AND seq <= 3`); err != nil {
			t.Fatal(err)
		}
		if _, err := fetchPlannedEnvelopes(db, 1, send, now); err == nil {
			t.Fatal("fetch accepted a batch missing its first clip — the follower would receive envelopes it cannot decrypt")
		}
	})

	t.Run("row vanished from the middle of the span", func(t *testing.T) {
		db := openTestDBWithShareRing(t)
		now := time.Now().Unix()
		seedRingClips(t, db, 3, 64, now)
		send := plan(t, db, now)

		if _, err := db.Exec(`DELETE FROM share_ring WHERE publication_id = 1 AND seq = 5`); err != nil {
			t.Fatal(err)
		}
		if _, err := fetchPlannedEnvelopes(db, 1, send, now); err == nil {
			t.Fatal("fetch accepted a batch with a hole in it")
		}
	})

	t.Run("row replaced under the same seq", func(t *testing.T) {
		db := openTestDBWithShareRing(t)
		now := time.Now().Unix()
		seedRingClips(t, db, 3, 64, now)
		send := plan(t, db, now)

		// Count still matches, so only the per-row check can catch this.
		if _, err := db.Exec(`DELETE FROM share_ring WHERE publication_id = 1 AND seq = 5`); err != nil {
			t.Fatal(err)
		}
		if err := RingInsert(db, 1, 5, KindClipStart, bytes.Repeat([]byte{9}, 128), now); err != nil {
			t.Fatal(err)
		}
		if _, err := fetchPlannedEnvelopes(db, 1, send, now); err == nil {
			t.Fatal("fetch accepted a row that is not the one that was planned")
		}
	})

	t.Run("empty batch needs no fetch", func(t *testing.T) {
		db := openTestDBWithShareRing(t)
		envelopes, err := fetchPlannedEnvelopes(db, 1, nil, time.Now().Unix())
		if err != nil || envelopes != nil {
			t.Fatalf("empty batch: envelopes=%v err=%v, want no query and no error", envelopes, err)
		}
	})
}
