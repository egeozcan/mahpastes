//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// modUser32 is shared with window_position_windows.go.
var procAllowSetForegroundWindow = modUser32.NewProc("AllowSetForegroundWindow")

// asfwAny lets any process take the foreground (ASFW_ANY), used when the lock
// file did not name the holder.
const asfwAny = ^uintptr(0)

// allowSetForegroundWindow lets the running instance bring its window to the
// front when it handles our request. Without it Windows' foreground lock
// usually leaves that window flashing in the taskbar instead.
func allowSetForegroundWindow(pid int) {
	target := asfwAny
	if pid > 0 {
		target = uintptr(pid)
	}
	_, _, _ = procAllowSetForegroundWindow.Call(target)
}

// showStartupError puts a startup failure in front of the user. The app is a
// GUI-subsystem binary, so on Windows nothing written to stderr is ever seen.
func showStartupError(message string) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	caption, _ := windows.UTF16PtrFromString("mahpastes")
	_, _ = windows.MessageBox(0, text, caption, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
