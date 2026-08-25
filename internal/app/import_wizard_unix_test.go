//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A FIFO stats fine and is not a directory, but reading one blocks forever.
// The scan must never offer it for import, and resolveSessionPath must refuse
// it even if a name somehow reaches it — the same reasoning already recorded on
// PathProbe.IsRegular in paste_paths.go.
//
// Lives in a !windows file because syscall.Mkfifo does not exist on Windows;
// a runtime GOOS skip would still fail to compile there.
func TestStartImportSessionSkipsFIFO(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	writeFile(t, root, "ok.txt", "readable")
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	res, err := app.StartImportSession(root, true)
	if err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}

	for _, e := range res.Entries {
		if e.Name == "pipe" {
			t.Fatal("a FIFO was offered for import; reading it would block forever")
		}
	}
	if res.Skipped.NonRegular != 1 {
		t.Errorf("Skipped.NonRegular = %d, want 1", res.Skipped.NonRegular)
	}
	if _, _, err := app.resolveSessionPath("pipe"); err == nil {
		t.Error("resolveSessionPath accepted a FIFO")
	}
}

// A file that was regular at scan time but is a FIFO by apply time must be
// refused rather than read.
func TestResolveSessionPathRejectsPostScanFIFOSwap(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	root := t.TempDir()
	victim := writeFile(t, root, "a.txt", "innocent")

	if _, err := app.StartImportSession(root, false); err != nil {
		t.Fatalf("StartImportSession: %v", err)
	}
	if _, _, err := app.resolveSessionPath("a.txt"); err != nil {
		t.Fatalf("baseline resolve failed: %v", err)
	}

	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := syscall.Mkfifo(victim, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	if _, _, err := app.resolveSessionPath("a.txt"); err == nil {
		t.Fatal("a swapped FIFO resolved; it must be refused")
	}
}
