//go:build !windows

package osnotify

import "runtime"

// lockThread pins the worker to one OS thread for the backend calls.
func lockThread() func() {
	runtime.LockOSThread()
	return runtime.UnlockOSThread
}
