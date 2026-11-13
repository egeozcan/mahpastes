//go:build !windows

package main

// allowSetForegroundWindow is a Windows foreground-lock concern; elsewhere the
// running instance raises its own window.
func allowSetForegroundWindow(int) {}
