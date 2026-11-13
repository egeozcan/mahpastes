package app

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each check used to run its own walk of the process table on its own OS
// thread: 2000 files settling together took seconds on ~1900 threads. However
// many callers arrive at once, one walk runs at a time and they share it.
func TestWriteOpenSnapshotsShareOneWalk(t *testing.T) {
	var running, peak, walks atomic.Int32
	s := newWriteOpenSnapshots(func() map[openFileID]struct{} {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		walks.Add(1)
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return map[openFileID]struct{}{{dev: 1, ino: 7}: {}}
	})

	const callers = 2000
	start := make(chan struct{})
	var wg sync.WaitGroup
	var wrong atomic.Int32
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			id := openFileID{dev: 1, ino: 7}
			if i%2 == 1 {
				id.ino = 8
			}
			// Age 0: the strictest caller, the one before a deletion.
			if s.openForWriting(id, 0) != (i%2 == 0) {
				wrong.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wrong.Load() != 0 {
		t.Fatalf("%d callers got the wrong answer", wrong.Load())
	}
	if peak.Load() != 1 {
		t.Fatalf("%d walks ran at once, want 1", peak.Load())
	}
	// Two when every caller is waiting before the first walk ends; a slow
	// scheduler can let a few arrive later.
	if n := walks.Load(); n > 10 {
		t.Fatalf("%d walks for %d simultaneous callers", n, callers)
	}
}

// A list is only as fresh as the start of its walk: a caller must never be
// answered from a walk that began before the age it accepts.
func TestWriteOpenSnapshotsHonourMaxAge(t *testing.T) {
	var walks atomic.Int32
	var open atomic.Bool
	id := openFileID{dev: 3, ino: 9}
	s := newWriteOpenSnapshots(func() map[openFileID]struct{} {
		walks.Add(1)
		if open.Load() {
			return map[openFileID]struct{}{id: {}}
		}
		return nil
	})

	if s.openForWriting(id, 0) {
		t.Fatal("reported open before any writer")
	}
	// A writer appears. A caller that accepts an old list may miss it and
	// must not cost a walk; one that accepts none must see it.
	open.Store(true)
	if s.openForWriting(id, time.Hour) {
		t.Fatal("an hour-old list was not reused")
	}
	if walks.Load() != 1 {
		t.Fatalf("reusing a list took %d walks, want 1", walks.Load())
	}
	if !s.openForWriting(id, 0) {
		t.Fatal("age 0 was answered from a list taken before the writer opened the file")
	}
	open.Store(false)
	time.Sleep(30 * time.Millisecond)
	if s.openForWriting(id, 10*time.Millisecond) {
		t.Fatal("a 10 ms limit was answered from a list 30 ms old")
	}
	if walks.Load() != 3 {
		t.Fatalf("%d walks, want 3", walks.Load())
	}
}

// A walk in progress when the caller arrives began too early for age 0: the
// caller waits for the next one rather than taking its result.
func TestWriteOpenSnapshotsSkipWalkBegunBeforeTheCall(t *testing.T) {
	id := openFileID{dev: 3, ino: 9}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var open atomic.Bool
	s := newWriteOpenSnapshots(func() map[openFileID]struct{} {
		was := open.Load() // what the walk saw when it began
		entered <- struct{}{}
		<-release
		if was {
			return map[openFileID]struct{}{id: {}}
		}
		return nil
	})
	first := make(chan bool, 1)
	go func() { first <- s.openForWriting(id, 0) }()
	<-entered
	open.Store(true) // the writer opens the file mid-walk
	second := make(chan bool, 1)
	go func() { second <- s.openForWriting(id, 0) }()
	time.Sleep(20 * time.Millisecond) // let the second caller queue behind the walk
	close(release)
	if <-first {
		t.Fatal("the first walk cannot have seen the writer")
	}
	if !<-second {
		t.Fatal("a caller that arrived mid-walk was answered from that walk")
	}
}

// The real check under the load that used to pin a thread per file.
func TestFileOpenForWritingManyAtOnce(t *testing.T) {
	if !fileBusyCheckSupported {
		t.Skip("this platform cannot tell whether a file is open for writing")
	}
	dir := t.TempDir()
	held := filepath.Join(dir, "held.bin")
	w, err := os.Create(held)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	idle := filepath.Join(dir, "idle.bin")
	if err := os.WriteFile(idle, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	heldInfo, err := os.Stat(held)
	if err != nil {
		t.Fatal(err)
	}
	idleInfo, err := os.Stat(idle)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 2000
	began := time.Now()
	var wg sync.WaitGroup
	var wrong atomic.Int32
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if !fileOpenForWriting(held, heldInfo, 0) {
					wrong.Add(1)
				}
			} else if fileOpenForWriting(idle, idleInfo, 0) {
				wrong.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wrong.Load() != 0 {
		t.Fatalf("%d of %d checks were wrong", wrong.Load(), callers)
	}
	// Measured before the walks were shared: 7.3 s on ~1865 threads.
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("%d simultaneous checks took %v", callers, took)
	}
}
