//go:build !windows

package app

import (
	"errors"
	"os"
	"syscall"
)

// acquireLock places a non-blocking exclusive advisory lock on f using flock(2).
// Returns an error if the lock is held by another process.
func acquireLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errors.New("lock held by another process")
	}
	return err
}
