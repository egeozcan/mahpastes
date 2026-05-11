package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// InstanceLock represents an advisory lock on a data directory, preventing
// multiple instances of mahpastes from running against the same database.
type InstanceLock struct {
	file *os.File
	path string
}

// AcquireInstanceLock attempts to acquire an exclusive advisory lock on
// <dataDir>/instance.lock. Returns an error if another instance already holds
// the lock; the error message includes the holding PID and a hint about
// MAHPASTES_DATA_DIR for running independent instances.
func AcquireInstanceLock(dataDir string) (*InstanceLock, error) {
	lockPath := filepath.Join(dataDir, "instance.lock")

	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open instance lock %s: %w", lockPath, err)
	}

	if err := acquireLock(f); err != nil {
		holder := readHolderPID(f)
		f.Close()
		return nil, &InstanceLockError{
			DataDir:   dataDir,
			LockPath:  lockPath,
			HolderPID: holder,
			Cause:     err,
		}
	}

	// Lock held — safe to overwrite PID atomically (we are the sole writer).
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, fmt.Errorf("truncate instance lock: %w", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("write pid to instance lock: %w", err)
	}

	return &InstanceLock{file: f, path: lockPath}, nil
}

// Release closes the lock fd. The OS releases the advisory lock automatically
// on close (or on process exit). The lock file itself is left on disk to avoid
// unlink/recreate races with a concurrent acquirer.
func (l *InstanceLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// readHolderPID reads the PID from an existing lock file. Returns 0 if the
// file is empty or unparseable (e.g. a stale file from a crashed instance that
// never wrote its PID).
func readHolderPID(f *os.File) int {
	buf := make([]byte, 32)
	n, _ := f.ReadAt(buf, 0)
	if n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		return 0
	}
	return pid
}

// InstanceLockError is returned when AcquireInstanceLock cannot obtain the
// lock because another instance holds it.
type InstanceLockError struct {
	DataDir   string
	LockPath  string
	HolderPID int
	Cause     error
}

func (e *InstanceLockError) Error() string {
	var holder string
	if e.HolderPID > 0 {
		holder = fmt.Sprintf("PID %d", e.HolderPID)
	} else {
		holder = "another process"
	}
	return fmt.Sprintf(
		"another mahpastes instance is already running against %s (held by %s). "+
			"To run a second instance against a different database, set MAHPASTES_DATA_DIR to a different path.",
		e.DataDir, holder,
	)
}

func (e *InstanceLockError) Unwrap() error { return e.Cause }

// IsInstanceLockError reports whether err is an InstanceLockError.
func IsInstanceLockError(err error) bool {
	var ile *InstanceLockError
	return errors.As(err, &ile)
}
