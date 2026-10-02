package app

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// A publisher refuses a follower by resetting the stream after reading its
// handshake (unknown share_id, bad HMAC, paused share, stream caps). The
// handshake write itself only buffers into the muxer, so "written" says
// nothing about whether the publisher took it. These tests pin what does: the
// publisher answers every accepted handshake with a frame, the follower counts
// a session as accepted only once one decrypts (or, for a publisher too old to
// answer, once the stream outlives a grace period), and only an accepted
// session resets the reconnect ladder or reports "connected".

// followStatusLog records the status carried by every share:follow-updated
// event, whichever payload shape carried it.
type followStatusLog struct {
	mu       sync.Mutex
	statuses []string
}

func recordFollowStatuses(m *ShareManager) *followStatusLog {
	l := &followStatusLog{}
	m.SetEventFn(func(name string, data ...any) {
		if name != "share:follow-updated" || len(data) == 0 {
			return
		}
		var status string
		switch v := data[0].(type) {
		case FollowInfo:
			status = v.Status
		case map[string]any:
			status, _ = v["status"].(string)
		}
		l.mu.Lock()
		l.statuses = append(l.statuses, status)
		l.mu.Unlock()
	})
	return l
}

func (l *followStatusLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.statuses)
}

func (l *followStatusLog) since(mark int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.statuses[mark:]...)
}

func isConnectedStatus(s string) bool { return strings.HasPrefix(s, "connected") }

func followStatusOf(m *ShareManager, id int64) string {
	m.mu.RLock()
	f := m.follows[id]
	m.mu.RUnlock()
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func waitFollowStatus(m *ShareManager, id int64, pred func(string) bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred(followStatusOf(m, id)) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func countFollowLogs(m *ShareManager, followID int64, prefix string) int {
	n := 0
	for _, e := range m.GetShareLogs(followID, 0) {
		if strings.HasPrefix(e.Message, prefix) {
			n++
		}
	}
	return n
}

// newSharedPublisher starts a manager sharing tag 1.
func newSharedPublisher(t *testing.T) (*ShareManager, *sql.DB, ShareInfo) {
	t.Helper()
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}
	return m, db, info
}

// newPrimedFollower starts a follower manager that already knows pub's
// addresses, with the given reconnect policy installed before any follow.
func newPrimedFollower(t *testing.T, pub peer.AddrInfo, timing *followTiming) (*ShareManager, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	if timing != nil {
		m.setFollowTimingForTest(*timing)
	}
	m.Host().Peerstore().AddAddrs(pub.ID, pub.Addrs, time.Hour)
	return m, db
}

func publishTextClip(t *testing.T, m *ShareManager, db *sql.DB, body string) {
	t.Helper()
	r, err := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, 'a.txt', '{}')`, body)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
		t.Fatal(err)
	}
	if err := m.OnClipCreated(clipID, []int64{1}); err != nil {
		t.Fatal(err)
	}
}

// TestPublisherAnswersIdleHandshakeWithAcceptFrame: a follower that is caught
// up gets nothing from catch-up, so before the accept frame an accepted idle
// session was byte-for-byte indistinguishable from a refused one until the
// refusal's reset arrived. The answer is a no-op gap — target = since_seq,
// sealed at since_seq+1 — which every follower already accepts as a first
// frame, and the next live envelope still lands at since_seq+1.
func TestPublisherAnswersIdleHandshakeWithAcceptFrame(t *testing.T) {
	for _, since := range []uint64{0, 5} {
		since := since
		t.Run("", func(t *testing.T) {
			db := newTestDB(t)
			m, err := NewShareManager(context.Background(), db, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Stop()
			symkey := bytes.Repeat([]byte{0xA5}, 32)
			shareID := DeriveShareID(symkey)
			newTestPublication(t, m, db, symkey, shareID, int64(since))

			follower, err := libp2p.New()
			if err != nil {
				t.Fatal(err)
			}
			defer follower.Close()
			s := openFollowerStream(t, follower, peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}, symkey, shareID, since)
			defer s.Close()

			kind, pt := readEnvelopeAt(t, s, symkey, shareID, since+1)
			if kind != KindGap {
				t.Fatalf("first frame kind %q want %q", kind, KindGap)
			}
			var gap GapPayload
			if err := UnmarshalPayload(pt, &gap); err != nil {
				t.Fatal(err)
			}
			if gap.Seq != since {
				t.Fatalf("accept gap target %d want %d — it must not move the follower", gap.Seq, since)
			}

			// The first live clip is sealed at since+1, the seq a follower
			// rewound to since by that gap will try next.
			if _, err := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', 'live', 'a.txt', '{}')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (1, 1)`); err != nil {
				t.Fatal(err)
			}
			if err := m.OnClipCreated(1, []int64{1}); err != nil {
				t.Fatal(err)
			}
			if kind, _ := readEnvelopeAt(t, s, symkey, shareID, since+1); kind != KindClipStart {
				t.Fatalf("first live frame kind %q want %q", kind, KindClipStart)
			}
		})
	}
}

// The accept frame is a gap whose target equals the durable boundary. The
// rewind warning exists for targets BELOW it; one that merely matches must not
// tell every caught-up follower, on every reconnect, that history rewound.
func TestAcceptFrameIsNotReportedAsRewind(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xA6}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)
	setFollowBoundary(t, db, f, 5)

	accept, err := encodeGapEnvelope(symkey, shareID, 6, 5)
	if err != nil {
		t.Fatal(err)
	}
	stream := bytes.NewBuffer(accept)
	for _, r := range clipRing(t, symkey, shareID, 6, 1, []byte("after accept"), false) {
		stream.Write(r.EnvelopeBytes)
	}
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	if lastSeq, received := followCursorRow(t, db, f.id); lastSeq != 8 || received != 1 {
		t.Fatalf("follows last_seq=%d clips_received=%d, want 8 and 1", lastSeq, received)
	}
	if n := countFollowLogs(m, f.id, "publisher history rewound"); n != 0 {
		t.Fatalf("%d rewind warnings for an accept frame, want 0", n)
	}
}

func TestRealRewindStillWarns(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xA7}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)
	setFollowBoundary(t, db, f, 100)

	gap, err := encodeGapEnvelope(symkey, shareID, 101, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.consumeStream(context.Background(), f, bytes.NewBuffer(gap)); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}
	if n := countFollowLogs(m, f.id, "publisher history rewound"); n != 1 {
		t.Fatalf("%d rewind warnings for a gap below the boundary, want 1", n)
	}
}

func TestConsumeStreamAcceptsOnFirstDecryptedFrame(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xA8}, 32)
	shareID := DeriveShareID(symkey)
	accept, err := encodeGapEnvelope(symkey, shareID, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		stream []byte
		want   int
	}{
		{"accept frame", accept, 1},
		{"catch-up clip, accepted once", streamOf(clipRing(t, symkey, shareID, 1, 1, []byte("c"), false)).Bytes(), 1},
		{"first frame does not decrypt", envelopeAt(t, symkey, shareID, 2, GapPayload{Seq: 9, Kind: KindGap}), 0},
		{"no frame at all", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, f := newConsumerFixture(t, symkey)
			calls := 0
			f.acceptSession = func() { calls++ }
			_ = m.consumeStream(context.Background(), f, bytes.NewBuffer(tc.stream))
			if calls != tc.want {
				t.Fatalf("acceptSession called %d times, want %d", calls, tc.want)
			}
		})
	}
}

// TestRefusedFollowerBacksOff is the regression for the 1 Hz redial loop: a
// follower of a paused or stopped share counted every refused handshake as a
// healthy session, reset its ladder to the floor, flipped its card to
// "connected" and back, and logged "handshake complete" — forever.
//
// The ladder is shrunk to a 100ms floor doubling to an 800ms cap, without
// jitter, so the count is exact. After the accepted session ends, redials
// land at 0.1, 0.2, 0.4, 0.8 and 1.6s (each wait is the rung reached so far,
// and dial time only pushes them later), so a 2s window holds at most 5. A
// floor-pinned loop would make ~20.
func TestRefusedFollowerBacksOff(t *testing.T) {
	timing := followTiming{
		floor: 100 * time.Millisecond, cap: 800 * time.Millisecond,
		acceptGrace: FollowAcceptGrace, noJitter: true,
	}
	const window = 2 * time.Second
	const maxAttempts = 5

	cases := []struct {
		name   string
		refuse func(*ShareManager) error
		resume func(*ShareManager) error
	}{
		{"paused share", func(m *ShareManager) error { return m.PauseShare(1) }, func(m *ShareManager) error { return m.ResumeShare(1) }},
		{"stopped share", func(m *ShareManager) error { return m.StopShare(1) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pubM, _, info := newSharedPublisher(t)
			fM, fDB := newPrimedFollower(t, peer.AddrInfo{ID: pubM.Host().ID(), Addrs: pubM.Host().Addrs()}, &timing)
			if _, err := fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`); err != nil {
				t.Fatal(err)
			}
			events := recordFollowStatuses(fM)
			fi, err := fM.Follow(info.ShareString, "inbox")
			if err != nil {
				t.Fatal(err)
			}
			if !waitFollowStatus(fM, fi.ID, isConnectedStatus, 3*time.Second) {
				t.Fatal("follow never connected before the share was refused")
			}

			evMark := events.mark()
			endedBefore := countFollowLogs(fM, fi.ID, "session ended")
			handshakesBefore := countFollowLogs(fM, fi.ID, "handshake complete")
			if err := tc.refuse(pubM); err != nil {
				t.Fatal(err)
			}
			time.Sleep(window)

			// One "session ended" is the accepted session the refusal closed;
			// every other one is a refused redial.
			attempts := countFollowLogs(fM, fi.ID, "session ended") - endedBefore - 1
			t.Logf("%d refused redials in %v", attempts, window)
			if attempts > maxAttempts {
				t.Fatalf("%d redials in %v against a refusing publisher, want at most %d — the ladder is not growing", attempts, window, maxAttempts)
			}
			if attempts < 2 {
				t.Fatalf("%d redials in %v, want at least 2 — the follow must keep retrying", attempts, window)
			}
			if n := countFollowLogs(fM, fi.ID, "handshake complete") - handshakesBefore; n != 0 {
				t.Fatalf("%d \"handshake complete\" entries for refused sessions, want 0", n)
			}
			after := events.since(evMark)
			for _, s := range after {
				if isConnectedStatus(s) {
					t.Fatalf("follow reported %q while the publisher refused it (events %v)", s, after)
				}
			}
			// The refusal itself takes the card offline once; refused redials
			// change nothing the card shows, so they must not refresh it.
			if len(after) > 1 {
				t.Fatalf("%d share:follow-updated events after the refusal, want at most 1 (events %v)", len(after), after)
			}

			if tc.resume == nil {
				return
			}
			if err := tc.resume(pubM); err != nil {
				t.Fatal(err)
			}
			// The ladder is capped, so an accepting publisher is found within
			// one cap wait without any user action.
			if !waitFollowStatus(fM, fi.ID, isConnectedStatus, timing.cap+3*time.Second) {
				t.Fatal("follow did not reconnect after the share resumed")
			}
		})
	}
}

// TestFollowReachesConnectedOnAcceptFrame: with the grace fallback pushed out
// to an hour, only the publisher's accept frame can make an idle follow report
// connected — and it must do so promptly, then still receive clips.
func TestFollowReachesConnectedOnAcceptFrame(t *testing.T) {
	pubM, pubDB, info := newSharedPublisher(t)
	timing := defaultFollowTiming()
	timing.acceptGrace = time.Hour
	fM, fDB := newPrimedFollower(t, peer.AddrInfo{ID: pubM.Host().ID(), Addrs: pubM.Host().Addrs()}, &timing)
	if _, err := fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	fi, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if !waitFollowStatus(fM, fi.ID, isConnectedStatus, 3*time.Second) {
		t.Fatalf("idle follow stuck at %q — the accept frame never arrived or was not counted", followStatusOf(fM, fi.ID))
	}
	if n := countFollowLogs(fM, fi.ID, "handshake complete"); n != 1 {
		t.Fatalf("%d \"handshake complete\" entries, want 1", n)
	}

	publishTextClip(t, pubM, pubDB, "after accept")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var data []byte
		if err := fDB.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&data); err == nil && string(data) == "after accept" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("follower never received the clip published after the accept frame")
}

// TestFollowAcceptsSilentPublisherAfterGrace covers publishers from before the
// accept frame: they read the handshake, register the follower and send
// nothing while idle. Such a session must not look connected at once (a
// refusal could still be on its way), but must once it outlives the grace.
func TestFollowAcceptsSilentPublisherAfterGrace(t *testing.T) {
	priv, _, err := libp2pCrypto.GenerateEd25519Key(cryptoRand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old, err := libp2p.New(libp2p.Identity(priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	handshakes := make(chan struct{}, 16)
	old.SetStreamHandler(ShareProtocolID, func(s network.Stream) {
		buf := make([]byte, HandshakeBytesLen)
		if _, err := io.ReadFull(s, buf); err != nil {
			_ = s.Reset()
			return
		}
		select {
		case handshakes <- struct{}{}:
		default:
		}
		_, _ = io.Copy(io.Discard, s)
		_ = s.Close()
	})

	pubKey, err := PublicKeyBytes(priv)
	if err != nil {
		t.Fatal(err)
	}
	shareString, err := EncodeShareString(pubKey, bytes.Repeat([]byte{0xA9}, 32))
	if err != nil {
		t.Fatal(err)
	}

	const grace = 400 * time.Millisecond
	timing := defaultFollowTiming()
	timing.acceptGrace = grace
	fM, _ := newPrimedFollower(t, peer.AddrInfo{ID: old.ID(), Addrs: old.Addrs()}, &timing)
	fi, err := fM.FollowWithoutDial(shareString, "inbox")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-handshakes:
	case <-time.After(5 * time.Second):
		t.Fatal("follower never reached the silent publisher")
	}
	time.Sleep(grace / 4)
	if s := followStatusOf(fM, fi.ID); isConnectedStatus(s) {
		t.Fatalf("status %q before the grace elapsed — nothing has proven the publisher accepted yet", s)
	}
	if !waitFollowStatus(fM, fi.ID, isConnectedStatus, grace+3*time.Second) {
		t.Fatalf("status %q after the grace — a silent session that stays up must count as accepted", followStatusOf(fM, fi.ID))
	}
	if n := countFollowLogs(fM, fi.ID, "handshake complete"); n != 1 {
		t.Fatalf("%d \"handshake complete\" entries, want 1", n)
	}
}

// Full jitter over the ladder spreads followers that a publisher restart or
// pause dropped at the same instant, so they do not redial in lockstep.
func TestFollowWaitIsJittered(t *testing.T) {
	ft := defaultFollowTiming()
	for _, backoff := range []time.Duration{ft.floor, 4 * time.Second, ft.cap} {
		lo := ft.floor / 2
		seen := map[time.Duration]bool{}
		for i := 0; i < 200; i++ {
			w := ft.wait(backoff)
			if w < lo || w > backoff {
				t.Fatalf("wait(%v) = %v, want within [%v, %v]", backoff, w, lo, backoff)
			}
			seen[w] = true
		}
		if len(seen) < 2 {
			t.Fatalf("wait(%v) returned one value over 200 draws — no jitter", backoff)
		}
	}
	ft.noJitter = true
	if w := ft.wait(4 * time.Second); w != 4*time.Second {
		t.Fatalf("noJitter wait = %v, want the rung itself", w)
	}
}
