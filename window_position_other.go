//go:build !windows

package main

// nativeWindowPositioning is false outside Windows: the saved position is
// checked against the window's current screen and set through Wails. On macOS
// Wails' WindowGetPosition and WindowSetPosition are both relative to the
// current screen's visible frame. On Linux the getter returns root-window
// coordinates while the setter adds the current monitor's origin, so they
// only agree when the window opens on a monitor at the desktop origin; a
// position saved on another monitor fails the visibility check (or lands on
// the opening monitor) rather than being restored there. Fixing that needs
// GDK monitor geometry through cgo. Wayland ignores positions entirely.
const nativeWindowPositioning = false

func moveWindowNative(x, y int) {}
