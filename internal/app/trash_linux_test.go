//go:build linux

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMoveToTrashLinux(t *testing.T) {
	if _, err := exec.LookPath("gio"); err != nil {
		t.Skip("gio not installed")
	}
	dir := t.TempDir()
	// gio refuses system-internal mounts unless the file shares a device with
	// the home directory, so make the temp dir the home.
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "share"))
	f := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveToTrash(f); err != nil {
		t.Fatalf("moveToTrash: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("original still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "share", "Trash", "files", "victim.txt")); err != nil {
		t.Fatalf("not in trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "share", "Trash", "info", "victim.txt.trashinfo")); err != nil {
		t.Fatalf("missing trashinfo: %v", err)
	}
}
