//go:build windows

package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// lockWholeFile takes the lock the way builds before the single-byte lock
// did: LockFileEx over the maximum range.
func lockWholeFile(f *os.File) error {
	var overlapped windows.Overlapped
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 0xFFFFFFFF, 0xFFFFFFFF, &overlapped)
}

// An instance of an older build (whole-file lock) and one of this build
// (single far byte) must still exclude each other on the same data directory.
func TestInstanceLock_ExcludesOlderWholeFileLock(t *testing.T) {
	t.Run("older build holds the lock", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := lockWholeFile(f); err != nil {
			t.Fatalf("whole-file lock: %v", err)
		}
		lock, err := AcquireInstanceLock(dir)
		if err == nil {
			lock.Release()
			t.Fatal("acquired the lock while an older build's whole-file lock was held")
		}
		if !IsInstanceLockError(err) {
			t.Fatalf("expected InstanceLockError, got %T: %v", err, err)
		}
	})

	t.Run("this build holds the lock", func(t *testing.T) {
		dir := t.TempDir()
		lock, err := AcquireInstanceLock(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
		f, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDWR, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := lockWholeFile(f); err == nil {
			t.Fatal("an older build's whole-file lock succeeded while this build held the lock")
		}
	})
}

// The process that loses the race must be able to read who holds the lock:
// the handoff passes that PID to AllowSetForegroundWindow, and the error the
// user sees names it.
func TestInstanceLock_HolderPIDReadableWhileLocked(t *testing.T) {
	dir := t.TempDir()
	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	_, err = AcquireInstanceLock(dir)
	ile, ok := err.(*InstanceLockError)
	if !ok {
		t.Fatalf("expected *InstanceLockError, got %T: %v", err, err)
	}
	if ile.HolderPID != os.Getpid() {
		t.Fatalf("holder PID = %d, want %d", ile.HolderPID, os.Getpid())
	}
}
