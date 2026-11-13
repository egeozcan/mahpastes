//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// instanceLockOffsetHigh is the high half of the offset of the single locked
// byte (offset 0x7FFFFFFF_00000000).
const instanceLockOffsetHigh = 0x7FFFFFFF

// acquireLock places a non-blocking exclusive lock on f using LockFileEx.
// Returns an error if the lock is held by another process.
func acquireLock(f *os.File) error {
	handle := windows.Handle(f.Fd())
	// Windows byte-range locks are mandatory: a locked range cannot even be
	// read through another handle. Locking the whole file would hide the PID
	// the holder writes at offset 0 from the process that loses the race
	// (readHolderPID would always see 0). So lock one byte far past anything
	// the file holds; a range beyond end-of-file is allowed. An older build
	// that locks the whole file still conflicts with this byte, both ways.
	overlapped := windows.Overlapped{OffsetHigh: instanceLockOffsetHigh}
	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY = non-blocking exclusive.
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err := windows.LockFileEx(handle, flags, 0, 1, 0, &overlapped)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return errors.New("lock held by another process")
		}
		return err
	}
	return nil
}
