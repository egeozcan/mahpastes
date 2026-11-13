package main

import (
	"testing"
	"time"

	"go-clipboard/internal/wailsbridge"
	"go-clipboard/internal/windowstate"
)

// fakeGeometry reports a fixed window placement, as the bridge would. With
// settleAfter set, the first settleAfter reads report stale instead: a move
// still queued on the main thread.
type fakeGeometry struct {
	g           wailsbridge.WindowGeometry
	stale       wailsbridge.WindowGeometry
	settleAfter int
	reads       int
}

func (f *fakeGeometry) WindowGeometry() (wailsbridge.WindowGeometry, bool) {
	f.reads++
	if f.reads <= f.settleAfter {
		return f.stale, true
	}
	return f.g, true
}

// Wails opens the window centred and the saved position is only applied on
// the first DomReady. A quit (or a keep-running blur) before that must not
// write the centred placement over the saved one.
func TestSaveWindowGeometryWaitsForRestore(t *testing.T) {
	t.Cleanup(func() { geometryRestorePending.Store(false) })

	dir := t.TempDir()
	saved := windowstate.State{Width: 900, Height: 650, X: 40, Y: 30, HasPos: true}
	if err := windowstate.Save(dir, saved); err != nil {
		t.Fatal(err)
	}
	centred := &fakeGeometry{g: wailsbridge.WindowGeometry{Width: 900, Height: 650, X: 306, Y: 59, Normal: true}}

	// As main does once it has loaded a saved state.
	geometryRestorePending.Store(true)

	saveWindowGeometry(centred, dir)
	if got, ok := windowstate.Load(dir); !ok || got != saved {
		t.Fatalf("saved position overwritten before the restore ran: %+v (ok=%v)", got, ok)
	}
	if centred.reads != 0 {
		t.Fatalf("geometry read %d times while the restore was pending", centred.reads)
	}

	// The restore lifts the gate (an inactive bridge makes its calls no-ops).
	restoreWindowGeometry(nil, saved)
	if geometryRestorePending.Load() {
		t.Fatal("restoreWindowGeometry left saving disabled")
	}
	moved := &fakeGeometry{g: wailsbridge.WindowGeometry{Width: 900, Height: 650, X: 120, Y: 80, Normal: true}}
	saveWindowGeometry(moved, dir)
	want := windowstate.State{Width: 900, Height: 650, X: 120, Y: 80, HasPos: true}
	if got, ok := windowstate.Load(dir); !ok || got != want {
		t.Fatalf("after the restore: got %+v (ok=%v), want %+v", got, ok, want)
	}
}

// With nothing to restore, the first placement is saved even before DomReady.
func TestSaveWindowGeometryFirstRunSavesAtOnce(t *testing.T) {
	dir := t.TempDir()
	src := &fakeGeometry{g: wailsbridge.WindowGeometry{Width: 1280, Height: 800, X: 116, Y: 60, Normal: true}}
	saveWindowGeometry(src, dir)
	want := windowstate.State{Width: 1280, Height: 800, X: 116, Y: 60, HasPos: true}
	if got, ok := windowstate.Load(dir); !ok || got != want {
		t.Fatalf("first run: got %+v (ok=%v), want %+v", got, ok, want)
	}
}

// The setters are asynchronous on macOS: saves must stay off until the window
// reports the restored position, but not forever if it never does.
func TestAwaitWindowPosition(t *testing.T) {
	centred := wailsbridge.WindowGeometry{Width: 900, Height: 650, X: 306, Y: 59, Normal: true}
	restored := wailsbridge.WindowGeometry{Width: 900, Height: 650, X: 40, Y: 30, Normal: true}

	src := &fakeGeometry{g: restored, stale: centred, settleAfter: 3}
	awaitWindowPosition(src, 40, 30, time.Minute)
	if src.reads != 4 {
		t.Fatalf("returned after %d reads, want 4 (the first read at the restored position)", src.reads)
	}

	// A position the window never takes: give up at the timeout.
	never := &fakeGeometry{g: centred}
	start := time.Now()
	awaitWindowPosition(never, 40, 30, 50*time.Millisecond)
	if el := time.Since(start); el < 50*time.Millisecond || el > 5*time.Second {
		t.Fatalf("gave up after %v, want about 50ms", el)
	}
	if never.reads < 2 {
		t.Fatalf("polled %d times", never.reads)
	}

	// An inactive bridge (shutdown) ends the wait at once.
	start = time.Now()
	awaitWindowPosition((*wailsbridge.Bridge)(nil), 40, 30, time.Minute)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("inactive bridge waited %v", el)
	}
}
