package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go-clipboard/internal/autostart"
	"go-clipboard/internal/desktopprefs"
	"go-clipboard/internal/openrequest"
	"go-clipboard/internal/wailsbridge"
)

// The login entry passes the flag main.go looks for, and the LaunchAgent is
// attributed to the bundle the app is signed as.
func TestAutostartAgreesWithAppIdentity(t *testing.T) {
	if autostart.HiddenFlag != openrequest.StartHiddenFlag {
		t.Fatalf("autostart passes %q but the app looks for %q", autostart.HiddenFlag, openrequest.StartHiddenFlag)
	}
	if got := renderPlist(t, "build/darwin/Info.plist")["CFBundleIdentifier"]; got != autostart.BundleID {
		t.Fatalf("autostart.BundleID = %q, Info.plist says %v", autostart.BundleID, got)
	}
}

// newLifecycleTestApp returns an App whose bridge answers the unsaved-work
// question with answer (or err) and counts how often it asked and raised the
// window.
func newLifecycleTestApp(t *testing.T, answer bool, err error) (*App, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	asked, shown := &atomic.Int32{}, &atomic.Int32{}
	b := wailsbridge.NewForTesting()
	b.SetTestConfirm(func(opts wailsbridge.ConfirmOptions) (bool, error) {
		asked.Add(1)
		if opts.ConfirmLabel == "" || opts.CancelLabel == "" || opts.Message == "" {
			t.Errorf("question lacks labels or text: %+v", opts)
		}
		return answer, err
	})
	b.SetTestShowWindow(func() { shown.Add(1) })
	d := &App{bridge: b, lifecycle: &desktopLifecycle{dataDir: t.TempDir()}}
	return d, asked, shown
}

func TestBeforeCloseQuitsWithoutAskingWhenNothingIsUnsaved(t *testing.T) {
	d, asked, _ := newLifecycleTestApp(t, false, nil)
	if d.beforeClose(context.Background()) {
		t.Fatal("a clean editor must not prevent quitting")
	}
	if asked.Load() != 0 {
		t.Fatal("nothing to ask about")
	}
}

func TestBeforeCloseAsksAboutUnsavedChanges(t *testing.T) {
	keep, asked, shown := newLifecycleTestApp(t, false, nil)
	keep.SetUnsavedChanges(true)
	if !keep.beforeClose(context.Background()) {
		t.Fatal("answering keep editing must cancel the quit")
	}
	if asked.Load() != 1 || shown.Load() != 1 {
		t.Fatalf("asked %d times, raised the window %d times; want 1 and 1", asked.Load(), shown.Load())
	}

	discard, _, _ := newLifecycleTestApp(t, true, nil)
	discard.SetUnsavedChanges(true)
	if discard.beforeClose(context.Background()) {
		t.Fatal("answering quit must let the quit go ahead")
	}

	reported, _, _ := newLifecycleTestApp(t, false, nil)
	reported.SetUnsavedChanges(true)
	reported.SetUnsavedChanges(false)
	if reported.beforeClose(context.Background()) {
		t.Fatal("a cleared flag must not prevent quitting")
	}
}

// The app must stay quittable when the question cannot be asked.
func TestBeforeCloseFailsOpen(t *testing.T) {
	d, _, _ := newLifecycleTestApp(t, false, errors.New("no display"))
	d.SetUnsavedChanges(true)
	if d.beforeClose(context.Background()) {
		t.Fatal("a dialog error must not keep the app open")
	}

	// No runtime at all: the real ConfirmDialog reports ErrNoDialog.
	bare := &App{bridge: wailsbridge.NewForTesting(), lifecycle: &desktopLifecycle{}}
	bare.SetUnsavedChanges(true)
	if bare.beforeClose(context.Background()) {
		t.Fatal("without a runtime the quit must go ahead")
	}
}

func TestBeforeCloseSkipsQuestionOnSignalQuit(t *testing.T) {
	d, asked, _ := newLifecycleTestApp(t, false, nil)
	d.SetUnsavedChanges(true)
	d.signalQuit() // bridge without runtime: Quit is a no-op
	if d.beforeClose(context.Background()) {
		t.Fatal("a signal-initiated quit must never be held up by a dialog")
	}
	if asked.Load() != 0 {
		t.Fatal("no one may be there to answer during a SIGTERM")
	}
}

func TestBeforeCloseDoesNotStackDialogs(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var asked atomic.Int32
	b := wailsbridge.NewForTesting()
	b.SetTestConfirm(func(wailsbridge.ConfirmOptions) (bool, error) {
		if asked.Add(1) == 1 {
			close(started)
		}
		<-release
		return true, nil
	})
	b.SetTestShowWindow(func() {})
	d := &App{bridge: b, lifecycle: &desktopLifecycle{}}
	d.SetUnsavedChanges(true)

	var wg sync.WaitGroup
	wg.Add(1)
	var first bool
	go func() {
		defer wg.Done()
		first = d.beforeClose(context.Background())
	}()
	<-started
	if !d.beforeClose(context.Background()) {
		t.Fatal("a second quit while the question is up must wait for its answer")
	}
	close(release)
	wg.Wait()
	if first {
		t.Fatal("the answer to the first question decides")
	}
	if asked.Load() != 1 {
		t.Fatalf("asked %d times, want 1", asked.Load())
	}
}

func TestDomReadyClearsUnsavedAndHidesOnce(t *testing.T) {
	d, _, _ := newLifecycleTestApp(t, false, nil)
	d.SetUnsavedChanges(true)
	d.lifecycle.hideAfterLoad = true
	d.domReady(context.Background())
	d.domReady(context.Background())
	if d.lifecycle.unsaved.Load() {
		t.Fatal("a reloaded page has no unsaved changes")
	}
}

func TestQuitOnSignalQuitsThenExitsAfterGrace(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	var quits atomic.Int32
	exited := make(chan int, 1)
	go quitOnSignal(sigs, func() { quits.Add(1) }, func(code int) { exited <- code }, 50*time.Millisecond)

	select {
	case <-exited:
		t.Fatal("must not exit before a signal")
	case <-time.After(20 * time.Millisecond):
	}
	sigs <- syscall.SIGTERM
	select {
	case code := <-exited:
		if code == 0 {
			t.Fatal("a forced exit must not report success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a stuck shutdown must end in exit after the grace period")
	}
	// quit runs on its own goroutine, so it may land just after exit.
	deadline := time.Now().Add(2 * time.Second)
	for quits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if quits.Load() != 1 {
		t.Fatalf("quit ran %d times, want 1", quits.Load())
	}
}

// A quit that never returns (a wedged UI thread) must not keep the deadline
// from ending the process.
func TestQuitOnSignalExitsWhenQuitBlocks(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	block := make(chan struct{})
	defer close(block)
	exited := make(chan int, 1)
	go quitOnSignal(sigs, func() { <-block }, func(code int) { exited <- code }, 50*time.Millisecond)
	sigs <- syscall.SIGTERM
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("a blocked quit must not stop the grace period from ending the process")
	}
}

func TestQuitOnSignalSecondSignalExitsAtOnce(t *testing.T) {
	sigs := make(chan os.Signal, 2)
	exited := make(chan int, 1)
	go quitOnSignal(sigs, func() {}, func(code int) { exited <- code }, time.Hour)
	sigs <- os.Interrupt
	sigs <- os.Interrupt
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("a second signal must exit without waiting for the grace period")
	}
}

func TestQuitOnSignalClosedChannel(t *testing.T) {
	sigs := make(chan os.Signal)
	close(sigs)
	done := make(chan struct{})
	go func() {
		quitOnSignal(sigs, func() { t.Error("no signal, no quit") }, func(int) { t.Error("no signal, no exit") }, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a closed channel must end the watcher")
	}
}

func TestKeepRunningSettingIsStoredForNextLaunch(t *testing.T) {
	// GetDesktopLifecycle also reads the launch at login entry: point every
	// place it looks at an empty temp dir instead of the real home.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPIMAGE", "")
	d, _, _ := newLifecycleTestApp(t, false, nil)
	d.lifecycle.keepRunningActive = false
	if err := d.SetKeepRunningWhenClosed(true); err != nil {
		t.Fatal(err)
	}
	prefs, err := desktopprefs.Load(d.lifecycle.dataDir)
	if err != nil || prefs.KeepRunningWhenClosed == nil || !*prefs.KeepRunningWhenClosed {
		t.Fatalf("stored %v, %v", prefs.KeepRunningWhenClosed, err)
	}
	got, err := d.GetDesktopLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	if !got.KeepRunningWhenClosed || got.KeepRunningActive {
		t.Fatalf("stored choice applies next launch, this one keeps its own: %+v", got)
	}
}

// WindowHidden runs on every blur. Without keep-running, or before Wails has
// started (no bridge), it must neither fail nor write the prefs file.
func TestWindowHiddenIsANoOpWithoutAWindowToSave(t *testing.T) {
	for _, keep := range []bool{false, true} {
		dir := t.TempDir()
		d := &App{lifecycle: &desktopLifecycle{dataDir: dir, saveGeometryOnHide: keep}}
		d.WindowHidden()
		if _, err := os.Stat(desktopprefs.Path(dir)); !os.IsNotExist(err) {
			t.Errorf("saveGeometryOnHide=%v: prefs file written (stat err %v)", keep, err)
		}
	}
}
