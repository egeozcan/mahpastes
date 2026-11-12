//go:build windows

package osnotify

import (
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

// sFalse is the HRESULT CoInitializeEx returns when the thread was already
// initialised in the same apartment; it still needs a CoUninitialize.
const sFalse = syscall.Errno(1)

// lockThread pins the worker to one OS thread and joins it to the COM
// multithreaded apartment. The Wails toast backend registers a COM class
// factory (CoRegisterClassObject) during init, which fails on a thread that
// never initialised COM; Wails' own goroutines do not.
func lockThread() func() {
	runtime.LockOSThread()
	err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	joined := err == nil || err == sFalse
	return func() {
		if joined {
			windows.CoUninitialize()
		}
		runtime.UnlockOSThread()
	}
}
