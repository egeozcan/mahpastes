package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestInstanceLock_AcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if lock == nil {
		t.Fatal("first acquire returned nil lock")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	// After release, a fresh acquire should succeed.
	lock2, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Fatalf("second release failed: %v", err)
	}
}

func TestInstanceLock_RejectsSecondAcquire(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	_, err = AcquireInstanceLock(dir)
	if err == nil {
		t.Fatal("second acquire unexpectedly succeeded")
	}
	if !IsInstanceLockError(err) {
		t.Fatalf("expected InstanceLockError, got %T: %v", err, err)
	}

	var ile *InstanceLockError
	for e := err; e != nil; {
		if cast, ok := e.(*InstanceLockError); ok {
			ile = cast
			break
		}
		break
	}
	if ile == nil {
		t.Fatalf("could not extract InstanceLockError from %v", err)
	}

	if ile.HolderPID != os.Getpid() {
		t.Errorf("expected holder PID %d, got %d", os.Getpid(), ile.HolderPID)
	}
	if !strings.Contains(ile.Error(), "MAHPASTES_DATA_DIR") {
		t.Errorf("error message missing MAHPASTES_DATA_DIR hint: %s", ile.Error())
	}
	if !strings.Contains(ile.Error(), dir) {
		t.Errorf("error message missing data dir: %s", ile.Error())
	}
}

func TestInstanceLock_WritesPID(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	data, err := os.ReadFile(lock.path)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse PID from lock file %q: %v", string(data), err)
	}
	if pid != os.Getpid() {
		t.Errorf("expected lock file PID %d, got %d", os.Getpid(), pid)
	}
}
