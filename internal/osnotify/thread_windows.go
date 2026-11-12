//go:build windows

package osnotify

import (
	"log"
	"runtime"
	_ "unsafe" // for go:linkname

	// Linked for winrtInitialize below; the Wails toast backend uses it too.
	_ "git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

// winrtInitialize is go-toast's one-time RoInitialize(RO_INIT_MULTITHREADED).
// go-toast sets a process-wide "initialised" flag before calling RoInitialize,
// and go-ole reports S_FALSE ("this thread is already initialised") as an
// error. So the worker thread must join the apartment through this function
// rather than its own CoInitializeEx: otherwise go-toast's first push of every
// run fails with S_FALSE, falls back to a PowerShell script and still returns
// an error. Wails links into wintoast the same way.
//
//go:linkname winrtInitialize git.sr.ht/~jackmordaunt/go-toast/v2/wintoast.initialize
func winrtInitialize() error

// lockThread pins the worker to one OS thread and joins it to the COM
// multithreaded apartment. The Wails toast backend registers a COM class
// factory (CoRegisterClassObject) during init, which fails on a thread that
// never initialised COM, and pushes every toast through WinRT on this thread.
func lockThread() func() {
	runtime.LockOSThread()
	if err := winrtInitialize(); err != nil {
		// InitNotifications then fails and Settings shows the reason.
		log.Printf("notifications: joining the COM apartment: %v", err)
	}
	// Never unlock: go-toast keeps the thread in the apartment for the life
	// of the process. A goroutine that exits while locked takes its thread
	// with it, so no other goroutine inherits that COM state.
	return func() {}
}
