//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	gioOnce sync.Once
	gioPath string
)

func gioBinary() string {
	gioOnce.Do(func() {
		if p, err := exec.LookPath("gio"); err == nil {
			gioPath = p
		}
	})
	return gioPath
}

// trashIsRecoverable is true when the gio tool (glib2, which implements the
// freedesktop.org Trash spec) is installed. Without it the wizard warns that
// delete is permanent.
func trashIsRecoverable() bool { return gioBinary() != "" }

// moveToTrash sends the file to the freedesktop trash via `gio trash`. When gio
// is missing it removes the file (the wizard already warned); when gio fails
// (e.g. tmpfs or other system-internal mounts) the error is returned and the
// file is left alone, because the UI promised the Trash.
func moveToTrash(absPath string) error {
	gio := gioBinary()
	if gio == "" {
		return os.Remove(absPath)
	}
	out, err := exec.Command(gio, "trash", "--", absPath).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("move to trash: %s", msg)
	}
	return nil
}
