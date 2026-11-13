package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
		Files:            []string{good},
		FileSizes:        []int64{3},
		Skipped:          []string{dir, missing, own},
		TooLarge:         []string{},
		Links:            []openrequest.Link{{Kind: openrequest.LinkShow, URL: "mahpastes://show"}},
		UnsupportedLinks: []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TakeOpenRequests() = %#v\nwant %#v", got, want)
	}

	// Drained: the next call is empty, never nil slices for the frontend.
	again, _ := d.TakeOpenRequests()
	if again.Files == nil || again.FileSizes == nil || again.TooLarge == nil || again.Skipped == nil || again.Links == nil || again.UnsupportedLinks == nil ||
		len(again.Files)+len(again.Skipped)+len(again.Links)+len(again.UnsupportedLinks) != 0 || again.PasteClipboard {
		t.Fatalf("second take = %#v, want empty non-nil slices", again)
	}
}

// Links are parsed on the way out: valid ones go to the frontend typed, the
// rest are handed back verbatim to be reported. A paste request survives the
// queue on its own, with nothing else to open.
func TestTakeOpenRequestsParsesLinksAndPaste(t *testing.T) {
	d := newOpenRequestTestApp(t)
	d.submitOpenRequest(openrequest.Request{
		URLs:     []string{"mahpastes://clip/42", "mahpastes://delete/42", "mahpastes://clip/0"},
		Activate: true,
	})
	d.submitOpenRequest(openrequest.Request{PasteClipboard: true, Activate: true})

	got, err := d.TakeOpenRequests()
	if err != nil {
		t.Fatal(err)
	}
	want := OpenRequests{
		Files:            []string{},
		FileSizes:        []int64{},
		Skipped:          []string{},
		TooLarge:         []string{},
		Links:            []openrequest.Link{{Kind: openrequest.LinkClip, ClipID: 42, URL: "mahpastes://clip/42"}},
		UnsupportedLinks: []string{"mahpastes://delete/42", "mahpastes://clip/0"},
		PasteClipboard:   true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TakeOpenRequests() = %#v\nwant %#v", got, want)
	}
}

// A file the user cannot read is skipped up front; otherwise it would abort
// the import of every other file in the same batch.
func TestTakeOpenRequestsSkipsUnreadableFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions enforced for this user")
	}
	d := newOpenRequestTestApp(t)
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.txt")
	locked := filepath.Join(dir, "locked.txt")
	for _, p := range []string{ok, locked} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}

	d.submitOpenRequest(openrequest.Request{Files: []string{locked, ok}})
	got, err := d.TakeOpenRequests()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Files, []string{ok}) || !reflect.DeepEqual(got.Skipped, []string{locked}) {
		t.Fatalf("TakeOpenRequests() = %#v, want ok.txt imported and locked.txt skipped", got)
	}
}

// A file SQLite could never hold as a clip is reported, not handed to the
// frontend to be read whole into memory; the other files of the request still
// import, with their sizes so the frontend can bound a batch.
func TestTakeOpenRequestsReportsFilesTooLargeToStore(t *testing.T) {
	d := newOpenRequestTestApp(t)
	old := maxOpenedFileBytes
	maxOpenedFileBytes = 8
	t.Cleanup(func() { maxOpenedFileBytes = old })

	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	atLimit := filepath.Join(dir, "limit.bin")
	big := filepath.Join(dir, "big.mov")
	for p, n := range map[string]int{small: 2, atLimit: 8, big: 9} {
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	d.submitOpenRequest(openrequest.Request{Files: []string{big, small, atLimit}})
	got, err := d.TakeOpenRequests()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Files, []string{small, atLimit}) ||
		!reflect.DeepEqual(got.FileSizes, []int64{2, 8}) ||
		!reflect.DeepEqual(got.TooLarge, []string{big}) ||
		len(got.Skipped) != 0 {
		t.Fatalf("TakeOpenRequests() = %#v, want big.mov too large and the rest imported with sizes", got)
	}
}

// The real limit must stay under what SQLite accepts for one row.
func TestMaxOpenedFileBytesFitsASQLiteRow(t *testing.T) {
	if maxOpenedFileBytes <= 0 || maxOpenedFileBytes >= 1_000_000_000 {
		t.Fatalf("maxOpenedFileBytes = %d, want under SQLite's 1,000,000,000-byte row limit", maxOpenedFileBytes)
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

// A macOS hidden start hides the app once the page has loaded. A launch that
// was acknowledged before that must leave the window showing.
func TestDomReadyKeepsWindowARelaunchAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		req      *openrequest.Request
		wantHide int32
	}{
		{"relaunch before load", &openrequest.Request{Activate: true}, 0},
		{"hidden relaunch", &openrequest.Request{Files: []string{"/a.png"}}, 1},
		{"nothing asked", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newOpenRequestTestApp(t)
			bridge := wailsbridge.NewForTesting()
			var shown, hidden atomic.Int32
			bridge.SetTestShowWindow(func() { shown.Add(1) })
			if tc.req != nil {
				d.submitRelaunchRequest(*tc.req)
			}
			d.attachOpenRequests(bridge)
			d.hideUnlessWindowAsked(func() { hidden.Add(1) })
			if hidden.Load() != tc.wantHide {
				t.Fatalf("hidden %d times, want %d", hidden.Load(), tc.wantHide)
			}
			if tc.wantHide == 0 && shown.Load() != 1 {
				t.Fatalf("shown %d times, want 1", shown.Load())
			}
		})
	}
}

// The second launch may spell the data directory differently (a symlink, as
// /tmp is on macOS). The lock is per inode, so the handoff must still arrive.
func TestSecondLaunchHandsOffThroughSymlinkedDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	base := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", base)
	t.Setenv("TMPDIR", base)
	// Long enough that the socket lives at the hashed fallback path.
	dataDir := filepath.Join(base, strings.Repeat("d", 120))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(dataDir, link); err != nil {
		t.Fatal(err)
	}
	lock, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	srv, err := openrequest.Listen(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	got := make(chan openrequest.Request, 1)
	go srv.Serve(func(r openrequest.Request) { got <- r })

	second, handedOff, err := acquireInstanceOrHandOff(link, openrequest.Request{Activate: true}, true)
	if second != nil {
		second.Release()
	}
	if err != nil || !handedOff {
		t.Fatalf("handedOff = %v, err = %v; want a handoff", handedOff, err)
	}
	if r := <-got; !r.Activate {
		t.Fatalf("holder got %#v", r)
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

// An instance that is shutting down closes its socket before it releases the
// lock. A launch in that gap must not fail with "already running": it takes
// the lock once it is free and starts normally.
func TestSecondLaunchTakesLockFreedDuringHandoff(t *testing.T) {
	oldTimeout, oldAttempt := handoffTimeout, handoffAttempt
	handoffTimeout, handoffAttempt = 5*time.Second, 50*time.Millisecond
	defer func() { handoffTimeout, handoffAttempt = oldTimeout, oldAttempt }()

	dataDir := shortTempDir(t)
	held, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		held.Release()
		close(released)
	}()

	lock, handedOff, err := acquireInstanceOrHandOff(dataDir, openrequest.Request{Activate: true}, true)
	<-released
	if err != nil || handedOff || lock == nil {
		t.Fatalf("acquireInstanceOrHandOff = (%v, %v, %v), want the freed lock", lock, handedOff, err)
	}
	lock.Release()
}

// A request the running instance refuses is not retried until the timeout.
func TestSecondLaunchRefusedFailsAtOnce(t *testing.T) {
	old := handoffTimeout
	handoffTimeout = 10 * time.Second
	defer func() { handoffTimeout = old }()

	dataDir := shortTempDir(t)
	lock, err := coreapp.AcquireInstanceLock(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	srv, err := openrequest.Listen(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(func(openrequest.Request) {})
	defer srv.Close()

	// Over the item cap but far under the size cap, so the server reads the
	// whole request and answers with a refusal on every OS.
	short := string(filepath.Separator) + "f"
	if runtime.GOOS == "windows" {
		short = `C:\f`
	}
	tooMany := make([]string, 10001)
	for i := range tooMany {
		tooMany[i] = short
	}
	start := time.Now()
	second, handedOff, err := acquireInstanceOrHandOff(dataDir, openrequest.Request{Files: tooMany, Activate: true}, true)
	if handedOff || second != nil || !coreapp.IsInstanceLockError(err) {
		t.Fatalf("acquireInstanceOrHandOff = (%v, %v, %v), want the lock error", second, handedOff, err)
	}
	if !errors.Is(err, openrequest.ErrRefused) {
		t.Fatalf("err = %v, want it to carry ErrRefused", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("refused handoff took %s, want an immediate failure", elapsed)
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

// macOS delivers the files of the process's own command line through
// application:openFile: as well. For a hidden start that delivery must not
// count as asking for the window, or `--hidden a.txt` would show it.
func TestLaunchFileEchoDoesNotAskForTheWindow(t *testing.T) {
	dir := t.TempDir()
	arg := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(arg, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "b.txt")

	d := newOpenRequestTestApp(t)
	launch := openrequest.FromArgs([]string{"--hidden", arg}, dir)
	d.submitOpenRequest(launch)
	echo := newLaunchFileEcho(launch)
	d.submitOpenRequest(echo.request(arg))
	var hidden atomic.Int32
	d.hideUnlessWindowAsked(func() { hidden.Add(1) })
	if hidden.Load() != 1 {
		t.Fatal("the echo of a --hidden launch's own file kept the window")
	}

	// The argument comes back once; the same file opened later is the user.
	if r := echo.request(arg); !r.Activate {
		t.Fatal("a later open of the launch file did not ask for the window")
	}
	if r := echo.request(other); !r.Activate {
		t.Fatal("another file's open did not ask for the window")
	}

	// A launch that shows its window has no echo to discount.
	shown := openrequest.FromArgs([]string{arg}, dir)
	if r := newLaunchFileEcho(shown).request(arg); !r.Activate {
		t.Fatal("a file open of a visible launch did not ask for the window")
	}
}

// AppKit may name the argument by another path to the same file.
func TestLaunchFileEchoMatchesAnotherSpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	dir := t.TempDir()
	arg := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(arg, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	echo := newLaunchFileEcho(openrequest.Request{Files: []string{arg}})
	if r := echo.request(filepath.Join(link, "a.txt")); r.Activate {
		t.Fatal("the launch file under another spelling asked for the window")
	}
}
