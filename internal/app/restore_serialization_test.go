package app

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestTagMutationBlocksWhileRestoreLockHeld pins the read side of
// backupRestoreMu: a tag mutation must not proceed while a restore (write
// lock) is in progress, so it can neither tag a restored row under a stale id
// nor commit into the window where publication hooks are suspended.
func TestTagMutationBlocksWhileRestoreLockHeld(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	if _, err := app.db.Exec(`INSERT INTO clips (content_type, data) VALUES ('text/plain', 'x')`); err != nil {
		t.Fatal(err)
	}
	tag, err := app.CreateTag("serialize-test-tag")
	if err != nil {
		t.Fatal(err)
	}

	app.backupRestoreMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			app.backupRestoreMu.Unlock()
		}
	}
	defer unlock()

	done := make(chan error, 1)
	go func() { done <- app.AddTagToClip(1, tag.ID) }()

	// Long enough for the call to have completed if nothing were holding it —
	// it is one small transaction. Mirrors TestBackupAndRestoreShareOneMutex.
	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("AddTagToClip finished (err=%v) while the backup/restore write lock was held: tag mutations are not serialized against restore", err)
	default:
	}

	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddTagToClip after unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AddTagToClip never completed after the write lock was released")
	}

	var n int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE clip_id = 1 AND tag_id = ?`, tag.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("clip_tags rows = %d, want 1", n)
	}
}

// TestConcurrentTagMutationsDoNotSerializeEachOther is the control for the
// read-lock choice: two tag mutations hold read locks, so neither excludes the
// other and both complete without a restore in the picture.
func TestConcurrentTagMutationsDoNotSerializeEachOther(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	if _, err := app.db.Exec(`INSERT INTO clips (content_type, data) VALUES ('text/plain', 'a'), ('text/plain', 'b')`); err != nil {
		t.Fatal(err)
	}
	tag, err := app.CreateTag("concurrent-tag")
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 2)
	go func() { errs <- app.AddTagToClip(1, tag.ID) }()
	go func() { errs <- app.AddTagToClip(2, tag.ID) }()
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("concurrent AddTagToClip: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent AddTagToClip calls did not both complete — read locks are excluding each other")
		}
	}
	app.shareHookWG.Wait()
}

// TestTagMutationProceedsUnderAnotherReadLock is the discriminating half of
// the read-lock choice, and the reason the test above is not enough on its own:
// two mutations serialized behind a plain Mutex would still both finish inside
// that test's timeout, so it passes either way. Here the test goroutine holds
// backupRestoreMu for read for the whole call. Under an exclusive Mutex the
// mutation could never acquire it and the test times out; under the RWMutex it
// proceeds, which is precisely the "readers do not exclude readers" semantics
// the write-lock test relies on to mean something.
func TestTagMutationProceedsUnderAnotherReadLock(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	if _, err := app.db.Exec(`INSERT INTO clips (content_type, data) VALUES ('text/plain', 'x')`); err != nil {
		t.Fatal(err)
	}
	tag, err := app.CreateTag("reader-concurrency-tag")
	if err != nil {
		t.Fatal(err)
	}

	// Held for the entire duration of the mutation below.
	app.backupRestoreMu.RLock()
	defer app.backupRestoreMu.RUnlock()

	done := make(chan error, 1)
	go func() { done <- app.AddTagToClip(1, tag.ID) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddTagToClip under a concurrent read lock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AddTagToClip blocked while another reader held backupRestoreMu: the lock is excluding readers, so restores and tag mutations are serialized by an exclusive mutex rather than the intended RWMutex")
	}

	app.shareHookWG.Wait()
}

// TestShareManagerStopWaitsForLoops pins the loopWG mechanics: Stop must not
// return while a tracked goroutine is still running, and must return promptly
// once it exits. This is what lets RestoreBackup replace the database after
// Stop without a descheduled receive loop writing pre-restore clips into it.
func TestShareManagerStopWaitsForLoops(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	exited := make(chan struct{})
	m.spawnLoop(func() {
		<-release
		close(exited)
	})

	stopped := make(chan struct{})
	go func() {
		m.Stop()
		close(stopped)
	}()

	time.Sleep(200 * time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("Stop returned while a tracked loop goroutine was still running")
	default:
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the tracked goroutine exited")
	}
	<-exited
}

// TestShareManagerStopBoundedWait pins the other half of the contract: a
// wedged loop must not make Stop unfinishable. The wait gives up after
// loopStopTimeout with a log line instead of hanging shutdown or a restore.
func TestShareManagerStopBoundedWait(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	wedge := make(chan struct{})
	defer close(wedge) // let the goroutine exit at test end
	m.spawnLoop(func() { <-wedge })

	start := time.Now()
	m.Stop()
	elapsed := time.Since(start)
	if elapsed < loopStopTimeout {
		t.Fatalf("Stop returned in %s — it should have waited out the %s bound for the wedged loop", elapsed, loopStopTimeout)
	}
	if elapsed > loopStopTimeout+3*time.Second {
		t.Fatalf("Stop took %s — far beyond the %s bound", elapsed, loopStopTimeout)
	}
}

// TestShareManagerStopReturnsPromptlyWithLiveFollowLoop drives the real
// unblock chain: a follow resumed by ResumeAll runs runFollowLoop through
// spawnLoop, and Stop's ctx cancel must unwind it (dial timeouts, stream
// resets) fast enough that the bounded wait never fires.
func TestShareManagerStopReturnsPromptlyWithLiveFollowLoop(t *testing.T) {
	db := newTestDB(t)

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	symkey := make([]byte, 32)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (5, 'follow-tag', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, created_at) VALUES (?, ?, 5, 0, 0)`,
		pid.String(), symkey,
	); err != nil {
		t.Fatal(err)
	}

	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ResumeAll(); err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	go func() {
		m.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(loopStopTimeout + 5*time.Second):
		t.Fatal("Stop hung with a live follow loop — the ctx-cancel unblock chain is broken")
	}
}

// TestSpawnLoopRefusedAfterStop pins the admission gate: once Stop has begun,
// no new loop may join the WaitGroup — an Add racing Stop's Wait is invalid
// usage, and a goroutine registered after the wait passed would escape the
// drain and touch whatever database exists by the time it runs.
func TestSpawnLoopRefusedAfterStop(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.Stop()

	ran := make(chan struct{}, 1)
	if m.spawnLoop(func() { ran <- struct{}{} }) {
		t.Fatal("spawnLoop admitted a goroutine after Stop")
	}
	if m.admitLoop() {
		t.Fatal("admitLoop admitted work after Stop")
	}
	select {
	case <-ran:
		t.Fatal("refused spawnLoop still ran its function")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestStopMarksPublicationsRemoved pins the corpse guard: a handshake that
// looked a publication up before Stop re-checks removed under fmu before
// registering, so Stop must mark every publication, not merely close its
// followers.
func TestStopMarksPublicationsRemoved(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (9, 'stop-tag', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartShare(9); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	var pub *publication
	for _, p := range m.publications {
		pub = p
	}
	m.mu.RUnlock()
	if pub == nil {
		t.Fatal("no publication registered")
	}

	m.Stop()

	pub.fmu.Lock()
	removed := pub.removed
	pub.fmu.Unlock()
	if !removed {
		t.Fatal("Stop left the publication not marked removed — a late handshake could still register into it")
	}
}

// TestOversizedClipRefusedBeforeEmission pins the publication-side size
// ceiling: a clip whose envelope burst could never fit a follower's send
// queue is refused up front — no ring rows, no seq consumed, no envelope
// buffering — with a warn in the share log. Without the refusal, emission
// buffered the whole encrypted burst in memory until commit, an unbounded
// heap spike for arbitrarily large clips.
func TestOversizedClipRefusedBeforeEmission(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	m.maxShareableClipOverride = 64 // bytes — tiny, so a small clip trips it

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (3, 'big-tag', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartShare(3); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 128) // over the 64-byte override
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data) VALUES (11, 'application/octet-stream', ?)`, body); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (11, 3)`); err != nil {
		t.Fatal(err)
	}

	if err := m.OnClipCreated(11, []int64{3}); err != nil {
		t.Fatalf("OnClipCreated: refusal must not be an error: %v", err)
	}

	var ringRows, lastSeq int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring`).Scan(&ringRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT last_seq FROM shares`).Scan(&lastSeq); err != nil {
		t.Fatal(err)
	}
	if ringRows != 0 || lastSeq != 0 {
		t.Fatalf("oversized clip was emitted: ring rows=%d last_seq=%d, want 0 and 0", ringRows, lastSeq)
	}

	warned := false
	for _, e := range m.GetShareLogs(0, 0) {
		if e.Level == "warn" && strings.Contains(e.Message, "share ceiling") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("refusal was silent — no warn entry in the share log")
	}

	// Control: a clip under the ceiling still publishes.
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data) VALUES (12, 'text/plain', 'ok')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (12, 3)`); err != nil {
		t.Fatal(err)
	}
	if err := m.OnClipCreated(12, []int64{3}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring`).Scan(&ringRows); err != nil {
		t.Fatal(err)
	}
	if ringRows != 3 {
		t.Fatalf("in-bounds clip after a refusal: ring rows=%d, want 3 (start+chunk+end)", ringRows)
	}
}

// TestOversizedClipStartFieldsRefused pins the header-field ceiling: a clip
// whose filename (or metadata) is absurdly large is refused before any
// payload is built — RenameClip accepts arbitrary lengths, and marshaling a
// huge filename would allocate it wholesale just for the envelope frame cap
// to reject it afterwards.
func TestOversizedClipStartFieldsRefused(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (4, 'name-tag', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartShare(4); err != nil {
		t.Fatal(err)
	}
	hugeName := strings.Repeat("n", MaxClipStartFieldBytes+1)
	if _, err := db.Exec(
		`INSERT INTO clips (id, content_type, data, filename) VALUES (21, 'text/plain', 'tiny', ?)`, hugeName,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (21, 4)`); err != nil {
		t.Fatal(err)
	}

	if err := m.OnClipCreated(21, []int64{4}); err != nil {
		t.Fatalf("OnClipCreated: header refusal must not be an error: %v", err)
	}
	var ringRows int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring`).Scan(&ringRows); err != nil {
		t.Fatal(err)
	}
	if ringRows != 0 {
		t.Fatalf("clip with oversized clip_start fields was emitted: ring rows=%d, want 0", ringRows)
	}
	warned := false
	for _, e := range m.GetShareLogs(0, 0) {
		if e.Level == "warn" && strings.Contains(e.Message, "header ceiling") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("header refusal was silent — no warn entry in the share log")
	}
}

// TestRingRetransmitMetaRowCap pins the metadata window bound: the query may
// materialize at most rowCap rows regardless of ring size, and reports that
// survivors remain beyond the window.
func TestRingRetransmitMetaRowCap(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1,'t','#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, 1, X'00', X'01', 10, 'active', 0)`); err != nil {
		t.Fatal(err)
	}
	now := int64(1_000_000)
	for seq := 1; seq <= 10; seq++ {
		if err := RingInsert(db, 1, uint64(seq), KindClipChunk, []byte{0xAB}, now); err != nil {
			t.Fatal(err)
		}
	}

	meta, truncated, err := RingRetransmitMeta(db, 1, 0, now, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 4 {
		t.Fatalf("window rows = %d, want 4 (the cap)", len(meta))
	}
	if !truncated {
		t.Fatal("truncated = false with 10 survivors and a 4-row cap")
	}
	if meta[len(meta)-1].Seq != 4 {
		t.Fatalf("window ends at seq %d, want 4", meta[len(meta)-1].Seq)
	}

	meta, truncated, err = RingRetransmitMeta(db, 1, 0, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 10 || truncated {
		t.Fatalf("exact-fit window: rows=%d truncated=%v, want 10 and false", len(meta), truncated)
	}
}

// TestPlanCatchupBatchMetaTruncation pins the paging semantics: a truncated
// metadata window forces the plan truncated (drain-close, follower pages),
// and no gap may jump past the window's last seq — the all-orphan fallback
// targets pubLastSeq, which would silently skip every unseen survivor.
func TestPlanCatchupBatchMetaTruncation(t *testing.T) {
	rows := []RingRowMeta{
		{Seq: 1, Kind: KindClipStart, ByteLen: 100},
		{Seq: 2, Kind: KindClipChunk, ByteLen: 100},
		{Seq: 3, Kind: KindClipEnd, ByteLen: 100},
	}
	plan := planCatchupBatch(rows, 0, 100, defaultCatchupCaps(), true)
	if !plan.Truncated {
		t.Fatal("meta-truncated window must force a truncated plan")
	}
	if len(plan.Send) != 3 {
		t.Fatalf("send = %d rows, want 3", len(plan.Send))
	}

	// All-orphan truncated window: without the clamp the gap targets
	// pubLastSeq (100), skipping every survivor beyond the window.
	orphans := []RingRowMeta{
		{Seq: 5, Kind: KindClipChunk, ByteLen: 100},
		{Seq: 6, Kind: KindClipEnd, ByteLen: 100},
	}
	plan = planCatchupBatch(orphans, 4, 100, defaultCatchupCaps(), true)
	if !plan.Truncated {
		t.Fatal("all-orphan truncated window must stay truncated")
	}
	if plan.GapTarget > 6 {
		t.Fatalf("gap target %d jumps past the window end 6 — unseen survivors would be skipped", plan.GapTarget)
	}

	// Control: same rows, window NOT truncated → prior semantics unchanged.
	plan = planCatchupBatch(orphans, 4, 100, defaultCatchupCaps(), false)
	if plan.GapTarget != 100 || plan.Truncated {
		t.Fatalf("untruncated all-orphan window: gap=%d truncated=%v, want 100 and false", plan.GapTarget, plan.Truncated)
	}
}
