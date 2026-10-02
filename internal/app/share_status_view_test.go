package app

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

// statusOf returns the single publication GetShareStatus reports, failing the
// test if there is not exactly one.
func statusOf(t *testing.T, m *ShareManager) ShareInfo {
	t.Helper()
	shares, _ := m.GetShareStatus()
	if len(shares) != 1 {
		t.Fatalf("GetShareStatus reported %d publications, want 1", len(shares))
	}
	return shares[0]
}

// An emission holds its publication's fmu from the first chunk read to the
// last ring insert — seconds for a large clip. GetShareStatus used to take that
// lock for every publication, under m.mu.RLock, so the UI's status poll waited
// out the emission; and a StopShare arriving meanwhile queued its write lock
// behind the poll, which in turn parked every later m.mu reader (the next
// clip's OnClipCreated among them) until the emission finished.
func TestGetShareStatusDoesNotWaitForEmission(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	res, err := db.Exec(`INSERT INTO tags (name) VALUES ('pub')`)
	if err != nil {
		t.Fatal(err)
	}
	tagID, _ := res.LastInsertId()
	info, err := m.StartShare(tagID)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	m.mu.RLock()
	pub := m.publications[info.ID]
	m.mu.RUnlock()

	// A real follower, so the count under test is one a handshake produced.
	follower, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()
	s := openFollowerStream(t, follower,
		peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()},
		pub.symkey, pub.shareID, 0)
	if !m.waitForFollowers(info.ID, 1, 5*time.Second) {
		t.Fatal("follower never registered")
	}

	// Stand in for an emission in progress.
	pub.fmu.Lock()
	done := make(chan []ShareInfo, 1)
	go func() {
		shares, _ := m.GetShareStatus()
		done <- shares
	}()
	select {
	case shares := <-done:
		pub.fmu.Unlock()
		if len(shares) != 1 || shares[0].Status != "active" || shares[0].Followers != 1 {
			t.Fatalf("status during emission = %+v, want one active publication with 1 follower", shares)
		}
	case <-time.After(2 * time.Second):
		pub.fmu.Unlock()
		<-done
		t.Fatal("GetShareStatus waited for the emission's fmu")
	}

	// The reported view follows every mutation made under fmu.
	if err := m.PauseShare(tagID); err != nil {
		t.Fatalf("PauseShare: %v", err)
	}
	if got := statusOf(t, m); got.Status != "paused" || got.Followers != 0 {
		t.Fatalf("after pause: %q with %d followers, want paused with 0 (pause closes every follower)", got.Status, got.Followers)
	}
	if err := m.ResumeShare(tagID); err != nil {
		t.Fatalf("ResumeShare: %v", err)
	}
	if got := statusOf(t, m); got.Status != "active" {
		t.Fatalf("after resume: %q, want active", got.Status)
	}

	// A follower that disconnects on its own leaves through the drain
	// goroutine's removal, which must refresh the count too.
	s2 := openFollowerStream(t, follower,
		peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()},
		pub.symkey, pub.shareID, 0)
	if !m.waitForFollowers(info.ID, 1, 5*time.Second) {
		t.Fatal("second follower never registered")
	}
	if got := statusOf(t, m); got.Followers != 1 {
		t.Fatalf("reported %d followers after reconnect, want 1", got.Followers)
	}
	s2.Reset()
	_ = s.Reset()
	deadline := time.Now().Add(5 * time.Second)
	for statusOf(t, m).Followers != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("reported follower count stuck at %d after the follower disconnected", statusOf(t, m).Followers)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// GetShareStatus copies the maps under m.mu and reads the database after
// releasing it, so a StopShare or Unfollow can delete the row in between. The
// entry must then be left out, not reported with zeroed counters and a
// creation date of 1970.
func TestGetShareStatusSkipsEntriesWhoseRowIsGone(t *testing.T) {
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// The state just after StopShare deleted the row but before this read:
	// in the map, not in the table.
	m.registerPublication(4242, 1, make([]byte, 16), make([]byte, 32), "active")
	shares, _ := m.GetShareStatus()
	if len(shares) != 0 {
		t.Fatalf("GetShareStatus reported %+v for a share whose row is gone", shares)
	}
}
