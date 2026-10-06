package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectionMountIsExcludedFromWatchAndImport(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	allowUnpickedImport(t)

	home := t.TempDir()
	mount := filepath.Join(home, "Mahpastes")
	writeFile(t, mount, "Active/clip [0123].txt", "already a clip")
	writeFile(t, home, "notes.txt", "keep me")
	// A symlink to the mount must not get around the check.
	link := filepath.Join(t.TempDir(), "clips-link")
	if err := os.Symlink(mount, link); err != nil {
		t.Fatal(err)
	}
	app.SetProjectionMount(mount)

	for _, path := range []string{mount, filepath.Join(mount, "Active"), link} {
		if _, err := app.AddWatchedFolder(WatchedFolderConfig{Path: path}); !errors.Is(err, errProjectionMount) {
			t.Errorf("AddWatchedFolder(%s) error = %v", path, err)
		}
		if _, err := app.StartImportSession(path, true); !errors.Is(err, errProjectionMount) {
			t.Errorf("StartImportSession(%s) error = %v", path, err)
		}
	}

	res, err := app.StartImportSession(home, true)
	if err != nil {
		t.Fatalf("StartImportSession(home): %v", err)
	}
	paths := relPaths(res)
	for _, r := range paths {
		if strings.HasPrefix(r, "Mahpastes") {
			t.Errorf("clips folder entry offered for import: %s", r)
		}
	}
	if len(paths) != 1 || paths[0] != "notes.txt" {
		t.Errorf("entries = %v, want only notes.txt", paths)
	}
	if res.Skipped.AppClips != 1 {
		t.Errorf("Skipped.AppClips = %d, want 1", res.Skipped.AppClips)
	}
	_ = app.EndImportSession()

	// Once unmounted, the directory is an ordinary folder again.
	app.SetProjectionMount("")
	if _, err := app.AddWatchedFolder(WatchedFolderConfig{Path: mount}); err != nil {
		t.Errorf("AddWatchedFolder after unmount: %v", err)
	}
}
