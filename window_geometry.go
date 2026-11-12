package main

import (
	"log"

	"go-clipboard/internal/wailsbridge"
	"go-clipboard/internal/windowstate"
)

// saveWindowGeometry records the main window's placement in dataDir. Wails
// calls OnBeforeClose for the close button, Cmd+Q / the app menu's Quit and
// runtime Quit on every platform, so this runs on each way the app exits while
// the window getters still work (they are disabled once shutdown starts).
// With options.App.HideWindowOnClose (keep running when closed) the close
// button hides the window without reaching OnBeforeClose; the geometry is then
// saved by the quit that eventually ends the app.
func saveWindowGeometry(b *wailsbridge.Bridge, dataDir string) {
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
	if !ok {
		return
	}
	if err := windowstate.Save(dataDir, next); err != nil {
		log.Printf("window state: %v", err)
	}
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
	if plan.Move {
		b.WindowSetPosition(plan.X, plan.Y)
	}
}
