package wailsbridge

import (
	rt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// WindowGeometry is the main window's current placement.
type WindowGeometry struct {
	Width, Height, X, Y int
	Maximised           bool
	// Normal is true when the window is neither maximised, minimised nor fullscreen.
	Normal bool
}

// WindowGeometry reads the window's current placement. ok is false when the
// bridge is inactive.
func (b *Bridge) WindowGeometry() (g WindowGeometry, ok bool) {
	if !b.active() {
		return g, false
	}
	g.Width, g.Height = rt.WindowGetSize(b.ctx)
	g.X, g.Y = rt.WindowGetPosition(b.ctx)
	g.Maximised = rt.WindowIsMaximised(b.ctx)
	g.Normal = rt.WindowIsNormal(b.ctx)
	return g, true
}

// WindowSetPosition moves the window, relative to the screen it is on.
func (b *Bridge) WindowSetPosition(x, y int) {
	if b.active() {
		rt.WindowSetPosition(b.ctx, x, y)
	}
}

// CurrentScreenSize returns the logical size of the screen the window is on
// (falling back to the primary one). ok is false when it cannot be determined.
func (b *Bridge) CurrentScreenSize() (w, h int, ok bool) {
	if !b.active() {
		return 0, 0, false
	}
	screens, err := rt.ScreenGetAll(b.ctx)
	if err != nil {
		return 0, 0, false
	}
	pick := -1
	for i, s := range screens {
		if s.IsCurrent {
			pick = i
			break
		}
		if s.IsPrimary && pick < 0 {
			pick = i
		}
	}
	if pick < 0 {
		return 0, 0, false
	}
	s := screens[pick]
	w, h = s.Size.Width, s.Size.Height
	if w == 0 || h == 0 {
		w, h = s.Width, s.Height
	}
	return w, h, w > 0 && h > 0
}
