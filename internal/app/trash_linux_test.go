//go:build linux

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gioTrashHome points gio's home and data dirs at a temp dir so nothing lands
// in the developer's real Trash. gio refuses system-internal mounts (tmpfs
// /tmp) unless the file shares a device with the home directory, so the temp
// dir has to be the home.
func gioTrashHome(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("gio"); err != nil {
		t.Skip("gio not installed")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "share"))
	return dir
}

func TestMoveToTrashLinux(t *testing.T) {
	dir := gioTrashHome(t)
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

// A gio failure must surface as an error. It must never be papered over with a
// permanent delete, because the wizard told the user the file goes to Trash.
func TestMoveToTrashLinuxReturnsGioFailure(t *testing.T) {
	dir := gioTrashHome(t)
	if err := moveToTrash(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("trashing a missing file succeeded; want the gio error")
	}
}

func TestMoveToTrashLinuxRefusesRelativePath(t *testing.T) {
	dir := gioTrashHome(t)
	t.Chdir(dir)
	if err := os.WriteFile("rel.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveToTrash("rel.txt"); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "rel.txt")); err != nil {
		t.Fatalf("relative-path refusal touched the file: %v", err)
	}
}
