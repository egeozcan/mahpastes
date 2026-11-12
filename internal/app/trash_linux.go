//go:build linux

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	gioOnce sync.Once
	gioPath string
)

// gioTrashTimeout bounds one `gio trash` call. Trashing is a rename into the
// home or per-volume trash, so this only fires on a wedged mount; the wizard
// then reports that file as failed instead of hanging the whole apply.
const gioTrashTimeout = 30 * time.Second

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
	// gio reads a relative argument that looks like a URI ("sftp://...") as a
	// URI; an absolute path is always a local path.
	if !filepath.IsAbs(absPath) {
		return fmt.Errorf("move to trash: %q is not an absolute path", absPath)
	}
	gio := gioBinary()
	if gio == "" {
		return os.Remove(absPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gioTrashTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gio, "trash", "--", absPath)
	// The local VFS implements the trash spec in-process, which keeps a stuck
	// or missing gvfs daemon (D-Bus) out of the path of a local delete.
	cmd.Env = append(os.Environ(), "GIO_USE_VFS=local")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("move to trash: gio trash timed out after %s", gioTrashTimeout)
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("move to trash: %s", msg)
	}
	return nil
}
