//go:build !windows

package main

// nativeWindowPositioning is false where Wails' WindowGetPosition and
// WindowSetPosition agree (both relative to the window's current screen), so
// the saved position is checked against that screen and set through Wails.
const nativeWindowPositioning = false

func moveWindowNative(x, y int) {}
