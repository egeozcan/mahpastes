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
// button hides the window natively, without reaching OnBeforeClose, so
// WindowHidden saves it then. A write is skipped when nothing changed.
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
// nothing when the window quits on close (OnBeforeClose saves it then) or
// never shows (headless runs).
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
	if plan.Move {
		b.WindowSetPosition(plan.X, plan.Y)
	}
}
