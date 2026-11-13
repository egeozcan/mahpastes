package main

import (
	"log"
	"sync/atomic"
	"time"

	"go-clipboard/internal/wailsbridge"
	"go-clipboard/internal/windowstate"
)

// geometryRestorePending is set from the moment a saved state is loaded until
// restoreWindowGeometry has applied it (first DomReady). It stays false when
// there is nothing to restore, so a first run's placement is saved.
var geometryRestorePending atomic.Bool

// How long restoreWindowGeometry waits for the window to report the restored
// position before saves resume anyway.
const (
	geometrySettleTimeout = time.Second
	geometrySettlePoll    = 10 * time.Millisecond
)

// windowGeometrySource is the part of wailsbridge.Bridge that saving reads.
type windowGeometrySource interface {
	WindowGeometry() (wailsbridge.WindowGeometry, bool)
}

// saveWindowGeometry records the main window's placement in dataDir. Wails
// calls OnBeforeClose for the close button, Cmd+Q / the app menu's Quit and
// runtime Quit on every platform, so this runs on each way the app exits while
// the window getters still work (they are disabled once shutdown starts).
// With options.App.HideWindowOnClose (keep running when closed) the close
// button hides the window natively, without reaching OnBeforeClose, so
// WindowHidden saves it then. A write is skipped when nothing changed.
func saveWindowGeometry(b windowGeometrySource, dataDir string) {
	// Until the saved position is applied the window sits where Wails centred
	// it; a quit that early must not replace the saved position with that.
	if geometryRestorePending.Load() {
		return
	}
	g, ok := b.WindowGeometry()
	if !ok {
		return
	}
	// On Windows g.X/g.Y are absolute virtual-desktop pixels (GetWindowRect),
	// which is what moveWindowNative takes back; elsewhere they are relative
	// to the current screen, as WindowSetPosition expects.
	cur := windowstate.State{Width: g.Width, Height: g.Height, X: g.X, Y: g.Y, HasPos: true, Maximised: g.Maximised}
	prev, hadPrev := windowstate.Load(dataDir)
	next, ok := windowstate.Merge(prev, hadPrev, cur, g.Normal)
	if !ok || (hadPrev && next == prev) {
		return
	}
	if err := windowstate.Save(dataDir, next); err != nil {
		log.Printf("window state: %v", err)
	}
}

// WindowHidden saves the window geometry when the window may just have been
// hidden. With keep-running, Wails hides the window on the close button by
// itself (Windows' WindowHide, GTK's hide-on-delete, AppKit's orderOut)
// without calling Go, so the frontend reports it: on window blur, which hiding
// the active window always causes, and on visibilitychange. Saving on a plain
// focus loss is harmless, and an unchanged geometry is not written. Does
// nothing before the saved geometry has been restored, when the window quits
// on close (OnBeforeClose saves it then) or never shows (headless runs).
func (d *App) WindowHidden() {
	if !d.lifecycle.saveGeometryOnHide || d.bridge == nil {
		return
	}
	saveWindowGeometry(d.bridge, d.lifecycle.dataDir)
}

// restoreWindowGeometry applies the parts of a saved state that Wails cannot
// take before the window exists: shrinking a size saved on a larger display to
// the current screen, and the position. Size and maximised state are already
// set through options.App.
func restoreWindowGeometry(b *wailsbridge.Bridge, saved windowstate.State) {
	// On Windows the window always opens on the default monitor and the saved
	// position is absolute, so move first: the size is then clamped against
	// the monitor the window landed on (Wails reports the window's monitor as
	// current), not shrunk to the default one. Elsewhere the position is
	// relative to the current screen and its visibility is judged at the
	// clamped size, so the size comes first and the move after.
	if nativeWindowPositioning && !saved.Maximised && saved.HasPos {
		moveWindowNative(saved.X, saved.Y)
	}
	var screen *windowstate.Screen
	if w, h, ok := b.CurrentScreenSize(); ok {
		screen = &windowstate.Screen{Width: w, Height: h}
	}
	plan := windowstate.PlanRestore(saved, screen, !nativeWindowPositioning)
	if plan.Resize {
		b.WindowSetSize(plan.Width, plan.Height)
	}
	if !plan.Move {
		geometryRestorePending.Store(false)
		return
	}
	b.WindowSetPosition(plan.X, plan.Y)
	// Not inline: this runs on Wails' message loop.
	go func() {
		awaitWindowPosition(b, plan.X, plan.Y, geometrySettleTimeout)
		geometryRestorePending.Store(false)
	}()
}

// awaitWindowPosition returns once the window reports position x,y, or after
// timeout. On macOS Wails queues the setters on the main thread while the
// getters read the window directly, so a save straight after the restore
// could still see the centred placement. The timeout covers a position the
// window system adjusts or (Linux, second monitor) reports differently.
func awaitWindowPosition(b windowGeometrySource, x, y int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		g, ok := b.WindowGeometry()
		if !ok || (g.X == x && g.Y == y) || !time.Now().Before(deadline) {
			return
		}
		time.Sleep(geometrySettlePoll)
	}
}
