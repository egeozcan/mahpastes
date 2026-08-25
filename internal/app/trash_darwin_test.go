//go:build darwin

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// moveToTrash is the one piece of this feature that no other test reaches:
// every wizard test sets MAHPASTES_TRASH_MODE=remove, which short-circuits
// trashFile before the CGo call. Without this test the Objective-C path would
// only ever be compiled, never run.
//
// It cleans up after itself so a test run does not silt up the developer's
// Trash, and skips rather than fails where the Trash is unavailable (sandboxed
// CI, a tmpdir on a volume with no .Trashes).
func TestMoveToTrashDarwin(t *testing.T) {
	if !trashIsRecoverable() {
		t.Skip("platform reports no recoverable trash")
	}

	// Trash the file from the home volume: NSFileManager relocates within a
	// volume, and a temp dir on another volume would exercise a different path.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	name := fmt.Sprintf("mahpastes-trash-test-%d.txt", time.Now().UnixNano())
	src := filepath.Join(home, name)
	if err := os.WriteFile(src, []byte("trash me"), 0o644); err != nil {
		t.Skipf("cannot write to home directory: %v", err)
	}

	trashed := filepath.Join(home, ".Trash", name)
	t.Cleanup(func() {
		os.Remove(src)
		os.Remove(trashed)
	})

	if err := moveToTrash(src); err != nil {
		t.Fatalf("moveToTrash: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("file is still at its original path after being trashed")
	}
	// The point of trashing rather than deleting: it is still recoverable.
	if _, err := os.Stat(trashed); err != nil {
		t.Errorf("file is not in ~/.Trash after moveToTrash: %v", err)
	}
}

func TestMoveToTrashRejectsEmptyPath(t *testing.T) {
	if err := moveToTrash(""); err == nil {
		t.Error("moveToTrash(\"\") should error")
	}
}

// trashFile honours the test-only escape hatch the e2e launcher sets, so runs
// do not fill the developer's Trash with fixtures.
func TestTrashFileHonoursRemoveMode(t *testing.T) {
	t.Setenv("MAHPASTES_TRASH_MODE", "remove")

	p := filepath.Join(t.TempDir(), "gone.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := trashFile(p); err != nil {
		t.Fatalf("trashFile: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file should have been removed outright")
	}
}
