//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// acquireLock places a non-blocking exclusive lock on f using LockFileEx.
// Returns an error if the lock is held by another process.
func acquireLock(f *os.File) error {
	handle := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY = non-blocking exclusive.
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	// Lock the entire file (max range).
	err := windows.LockFileEx(handle, flags, 0, 0xFFFFFFFF, 0xFFFFFFFF, &overlapped)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return errors.New("lock held by another process")
		}
		return err
	}
	return nil
}
