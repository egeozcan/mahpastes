package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/internal/wailsbridge"
)

func newDesktopTestApp(t *testing.T, saveTo func() string) *App {
	t.Helper()
	dir := t.TempDir()
	db := newTransferServiceTestDB(t, dir)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'hello', 'a.txt')`); err != nil {
		t.Fatal(err)
	}
	core := newTransferServiceTestApp(t, db, filepath.Join(dir, "tmp"))
	bridge := wailsbridge.NewForTesting()
	bridge.SetTestSaveFile(func(wailsbridge.FileDialogOptions) (string, error) { return saveTo(), nil })
	d := &App{desktopCore: core, core: core, bridge: bridge}
	return d
}

// A cancelled save dialog must be distinguishable from a written file, or the
// frontend toasts "Download complete." for a download that never happened.
func TestBulkDownloadToFileReportsCancel(t *testing.T) {
	d := newDesktopTestApp(t, func() string { return "" })
	path, err := d.BulkDownloadToFile([]int64{1})
	if err != nil {
		t.Fatalf("cancel returned error: %v", err)
	}
	if path != "" {
		t.Fatalf("cancel returned path %q, want empty", path)
	}
}

func TestBulkDownloadToFileReturnsWrittenPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "clips.zip")
	d := newDesktopTestApp(t, func() string { return out })
	path, err := d.BulkDownloadToFile([]int64{1})
	if err != nil {
		t.Fatalf("BulkDownloadToFile: %v", err)
	}
	if path != out {
		t.Fatalf("path = %q, want %q", path, out)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "1_a.txt" {
		t.Fatalf("unexpected zip entries: %v", zr.File)
	}
}

func TestSaveClipToFileReportsCancelAndPath(t *testing.T) {
	d := newDesktopTestApp(t, func() string { return "" })
	if path, err := d.SaveClipToFile(1); err != nil || path != "" {
		t.Fatalf("cancel: path=%q err=%v, want empty/nil", path, err)
	}

	out := filepath.Join(t.TempDir(), "a.txt")
	d = newDesktopTestApp(t, func() string { return out })
	path, err := d.SaveClipToFile(1)
	if err != nil || path != out {
		t.Fatalf("save: path=%q err=%v, want %q", path, err, out)
	}
	if b, _ := os.ReadFile(out); string(b) != "hello" {
		t.Fatalf("saved bytes = %q", b)
	}
}
