package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStopShareRejectsInFlightHandshake covers the window between
// handlePublisherStream's findPublicationByShareID and its pub.fmu
// acquisition. A StopShare landing there used to leave the handler
// registering a follower into a publication that had already been dropped
// from m.publications and had closeAllFollowers run on it: the stream then
// lingered on an orphaned object, so the publisher carried a follower for a
// share that no longer existed.
//
// The two hooks pin the ordering exactly. Sequencing matters because after
// StopShare's DELETE the handler's own last_seq lookup fails and it resets by
// accident — the interesting window is between the removal and the DELETE.
func TestStopShareRejectsInFlightHandshake(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, err := NewShareManager(ctx, pubDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager: %v", err)
	}
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)

	// Both hooks are installed BEFORE StartShare: registerPublication's m.mu
	// write is the happens-before edge to the handler goroutine's read.
	var (
		once          sync.Once
		inWindow      = make(chan struct{})
		marked        = make(chan struct{})
		releaseDelete = make(chan struct{})
		capturedMu    sync.Mutex
		captured      *publication
	)
	pubM.testHookBeforeRegister = func(p *publication) {
		once.Do(func() {
			capturedMu.Lock()
			captured = p
			capturedMu.Unlock()
			close(inWindow)
			// Hold the handshake here until StopShare has torn the
			// publication down but not yet deleted its row.
			<-marked
		})
	}
	pubM.testHookAfterStopMark = func() {
		close(marked)
		<-releaseDelete
	}

	info, err := pubM.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager follower: %v", err)
	}
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`)
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	if _, err := fM.Follow(info.ShareString, "inbox"); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	select {
	case <-inWindow:
	case <-time.After(10 * time.Second):
		t.Fatal("handshake never reached the pre-register hook — test proved nothing")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- pubM.StopShare(1) }()

	// Wait for StopShare to finish its fmu section before asserting on it;
	// the same signal is what releases the parked handshake.
	select {
	case <-marked:
	case <-time.After(10 * time.Second):
		t.Fatal("StopShare never reached its post-mark hook")
	}

	capturedMu.Lock()
	pub := captured
	capturedMu.Unlock()

	// The handshake is now released and racing to register. Poll rather than
	// sample once: without the re-check the bad registration persists (the
	// handler's io.Copy keeps the orphaned stream open), so a stale entry
	// here is stable, not transient.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		pub.fmu.Lock()
		n := len(pub.followers)
		removed := pub.removed
		pub.fmu.Unlock()
		if !removed {
			close(releaseDelete)
			t.Fatal("StopShare did not mark the publication removed")
		}
		if n != 0 {
			close(releaseDelete)
			t.Fatalf("stopped publication has %d follower(s) — handshake registered into a removed share", n)
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(releaseDelete)
	if err := <-stopDone; err != nil {
		t.Fatalf("StopShare: %v", err)
	}
}

// TestPauseShareRejectsInFlightHandshake is the pause arm of the same
// re-check. A PauseShare landing in the lookup→register window used to leave
// a follower attached to a paused publication: closeAllFollowers had already
// run, so nothing ever closed the late arrival and the share reported a
// follower it would never send to.
func TestPauseShareRejectsInFlightHandshake(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, err := NewShareManager(ctx, pubDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager: %v", err)
	}
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)

	var (
		once       sync.Once
		hookFired  atomic.Bool
		capturedMu sync.Mutex
		captured   *publication
	)
	pubM.testHookBeforeRegister = func(p *publication) {
		once.Do(func() {
			capturedMu.Lock()
			captured = p
			capturedMu.Unlock()
			// PauseShare completes entirely (mark + close) while the
			// handshake sits here, so the handler resumes into a paused
			// publication whose rows are all still present.
			if err := pubM.PauseShare(1); err != nil {
				t.Errorf("PauseShare from hook: %v", err)
			}
			hookFired.Store(true)
		})
	}

	info, err := pubM.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager follower: %v", err)
	}
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`)
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	if _, err := fM.Follow(info.ShareString, "inbox"); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !hookFired.Load() {
		time.Sleep(20 * time.Millisecond)
	}
	if !hookFired.Load() {
		t.Fatal("handshake never reached the pre-register hook — test proved nothing")
	}

	capturedMu.Lock()
	pub := captured
	capturedMu.Unlock()

	stable := time.Now().Add(1 * time.Second)
	for time.Now().Before(stable) {
		pub.fmu.Lock()
		n := len(pub.followers)
		pub.fmu.Unlock()
		if n != 0 {
			t.Fatalf("paused publication has %d follower(s) — handshake registered despite the pause", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestPauseResumeShareConcurrentStress hammers the publication-scoped fields
// from every goroutine that touches them — PauseShare/ResumeShare writing
// status, GetShareStatus and OnClipCreated reading it, and a real follower
// re-handshaking into handlePublisherStream throughout. It asserts no
// invariant beyond "does not deadlock and ends consistent"; the point is to
// give the race detector concurrent access to pub.status and pub.followers.
func TestPauseResumeShareConcurrentStress(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, err := NewShareManager(ctx, pubDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager: %v", err)
	}
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	pubDB.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (7, 'text/plain', ?, 'a.txt')`, []byte("hello share"))
	pubDB.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (7, 1)`)

	info, err := pubM.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatalf("NewShareManager follower: %v", err)
	}
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`)
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	followInfo, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				_ = pubM.PauseShare(1)
			} else {
				_ = pubM.ResumeShare(1)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				pubM.GetShareStatus()
				_ = pubM.OnClipCreated(7, []int64{1})
				fM.GetShareStatus()
				_ = fM.ReconnectFollow(followInfo.ID)
				time.Sleep(3 * time.Millisecond)
			}
		}()
	}

	time.Sleep(1500 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Leave the share active so the deferred Stop tears down a live one.
	if err := pubM.ResumeShare(1); err != nil {
		t.Fatalf("final ResumeShare: %v", err)
	}
	pubM.mu.RLock()
	var pub *publication
	for _, p := range pubM.publications {
		if p.tagID == 1 {
			pub = p
		}
	}
	pubM.mu.RUnlock()
	if pub == nil {
		t.Fatal("publication vanished during the stress run")
	}
	if got := pub.currentStatus(); got != "active" {
		t.Fatalf("final status %q want active", got)
	}
	var dbStatus string
	if err := pubDB.QueryRow(`SELECT status FROM shares WHERE tag_id = 1`).Scan(&dbStatus); err != nil {
		t.Fatalf("read shares.status: %v", err)
	}
	if dbStatus != "active" {
		t.Fatalf("shares.status %q want active — memory and DB diverged", dbStatus)
	}
}
