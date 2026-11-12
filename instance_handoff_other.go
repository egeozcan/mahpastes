//go:build !windows

package main

// allowSetForegroundWindow is a Windows foreground-lock concern; elsewhere the
// running instance raises its own window.
func allowSetForegroundWindow(int) {}

// showStartupError has nothing extra to do outside Windows: the message is
// logged to stderr by the caller, where a terminal launch shows it.
func showStartupError(string) {}
