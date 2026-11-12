package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/openrequest"
	"go-clipboard/internal/wailsbridge"
)

func newOpenRequestTestApp(t *testing.T) *App {
	t.Helper()
	d := newDesktopTestApp(t, func() string { return "" })
	d.openRequests = openrequest.NewQueue(time.Millisecond)
	return d
}

// Files are vetted where they are imported, not where they were named: only
// existing regular files outside the app's own temp dir go to the frontend.
func TestTakeOpenRequestsSortsImportableFiles(t *testing.T) {
	d := newOpenRequestTestApp(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(good, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(d.core.TempDir(), "1_a.txt")
	if err := os.WriteFile(own, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")

	d.submitOpenRequest(openrequest.Request{
		Files:    []string{good, dir, missing, own},
		URLs:     []string{"mahpastes://show"},
		Activate: true,
	})
	got, err := d.TakeOpenRequests()
	if err != nil {
		t.Fatal(err)
	}
	want := OpenRequests{
		Files:   []string{good},
		Skipped: []string{dir, missing, own},
		URLs:    []string{"mahpastes://show"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TakeOpenRequests() = %#v\nwant %#v", got, want)
	}

	// Drained: the next call is empty, never nil slices for the frontend.
	again, _ := d.TakeOpenRequests()
	if again.Files == nil || again.Skipped == nil || again.URLs == nil ||
		len(again.Files)+len(again.Skipped)+len(again.URLs) != 0 {
		t.Fatalf("second take = %#v, want empty non-nil slices", again)
	}
}

func TestOpenRequestRaisesWindowAndNotifiesFrontend(t *testing.T) {
	d := newOpenRequestTestApp(t)
	bridge := wailsbridge.NewForTesting()
	var shown atomic.Int32
	bridge.SetTestShowWindow(func() { shown.Add(1) })
	var mu sync.Mutex
	var events []string
	bridge.SetTestEventSink(func(name string, _ ...interface{}) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	})

	// Arrives before the window exists (cold launch): queued, nothing raised.
	d.submitOpenRequest(openrequest.Request{Files: []string{"/early.png"}, Activate: true})
	if shown.Load() != 0 {
		t.Fatal("raised a window that does not exist yet")
	}

	d.attachOpenRequests(bridge)
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) == 1 && events[0] == openRequestEvent
	})

	// A relaunch with nothing to open only raises.
	d.submitOpenRequest(openrequest.Request{Activate: true})
	if shown.Load() != 1 {
		t.Fatalf("plain activate raised %d times, want 1", shown.Load())
	}
}

// A second launch finds the lock held, forwards its arguments over the
// instance socket and reports success, so main() exits without a window.
func TestSecondLaunchHandsOffToRunningInstance(t *testing.T) {
	dataDir := shortTempDir(t)
	lock, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	running := newOpenRequestTestApp(t)
	srv, err := openrequest.Listen(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(running.submitOpenRequest)
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "sent.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, handedOff, err := acquireInstanceOrHandOff(dataDir, openrequest.Request{Files: []string{file}, Activate: true}, true)
	if err != nil || !handedOff || second != nil {
		t.Fatalf("acquireInstanceOrHandOff = (%v, %v, %v), want a handoff", second, handedOff, err)
	}
	got, _ := running.TakeOpenRequests()
	if !reflect.DeepEqual(got.Files, []string{file}) {
		t.Fatalf("running instance received %#v", got)
	}
}

// When the lock holder does not listen (mahpastesd, or a build without the
// socket), the launch fails with the lock error as before.
func TestSecondLaunchWithoutListenerKeepsLockError(t *testing.T) {
	old := handoffTimeout
	handoffTimeout = 200 * time.Millisecond
	defer func() { handoffTimeout = old }()

	dataDir := shortTempDir(t)
	lock, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	for _, allow := range []bool{true, false} {
		second, handedOff, err := acquireInstanceOrHandOff(dataDir, openrequest.Request{Activate: true}, allow)
		if handedOff || second != nil {
			t.Fatalf("allow=%v: got a lock or handoff with the lock held elsewhere", allow)
		}
		if !coreapp.IsInstanceLockError(err) {
			t.Fatalf("allow=%v: err = %v, want the instance lock error", allow, err)
		}
	}
}

func TestFirstLaunchTakesTheLock(t *testing.T) {
	dataDir := shortTempDir(t)
	lock, handedOff, err := acquireInstanceOrHandOff(dataDir, openrequest.Request{Activate: true}, true)
	if err != nil || handedOff || lock == nil {
		t.Fatalf("acquireInstanceOrHandOff = (%v, %v, %v), want the lock", lock, handedOff, err)
	}
	lock.Release()
}

// shortTempDir keeps the instance socket path under the sockaddr_un limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mpmain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
