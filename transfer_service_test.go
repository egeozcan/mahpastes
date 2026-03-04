package main

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildTransferCapabilitiesMatrix(t *testing.T) {
	tests := []struct {
		goos           string
		dragEnabled    bool
		expectStrategy string
		expectReason   bool
		nativeDrag     bool
	}{
		{goos: "darwin", dragEnabled: true, expectStrategy: "file-uri-v1", expectReason: false, nativeDrag: true},
		{goos: "windows", dragEnabled: true, expectStrategy: "file-uri-v1", expectReason: false, nativeDrag: false},
		{goos: "linux", dragEnabled: false, expectStrategy: "linux-fileuri-v1", expectReason: true, nativeDrag: false},
		{goos: "freebsd", dragEnabled: false, expectStrategy: "", expectReason: true, nativeDrag: false},
	}

	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			caps := buildTransferCapabilities(tc.goos)
			if caps.DragOut.Enabled != tc.dragEnabled {
				t.Fatalf("drag enabled mismatch: got %v want %v", caps.DragOut.Enabled, tc.dragEnabled)
			}
			if caps.DragOut.Strategy != tc.expectStrategy {
				t.Fatalf("strategy mismatch: got %q want %q", caps.DragOut.Strategy, tc.expectStrategy)
			}
			if tc.expectReason && caps.DragOut.Reason == "" {
				t.Fatalf("expected non-empty reason for %s", tc.goos)
			}
			if caps.DragOut.NativeDrag != tc.nativeDrag {
				t.Fatalf("native_drag mismatch: got %v want %v", caps.DragOut.NativeDrag, tc.nativeDrag)
			}
		})
	}
}

func TestPrepareClipForTransferReturnsDescriptor(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		5, "image/png", []byte("png-binary"), "preview.png")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	app := &App{
		db:      db,
		tempDir: tempDir,
	}
	app.transferHandler = &TransferFileHandler{app: app}
	app.tempStore = NewTempClipStore(db, tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
	service := NewTransferService(app)

	item, err := service.PrepareClipForTransfer(PrepareTransferRequest{
		ClipID:  5,
		Channel: "drag_out",
	})
	if err != nil {
		t.Fatalf("prepare transfer failed: %v", err)
	}

	if item.ClipID != 5 {
		t.Fatalf("clip id mismatch: got %d", item.ClipID)
	}
	if item.ContentType != "image/png" {
		t.Fatalf("content type mismatch: got %q", item.ContentType)
	}
	if item.Filename != "preview.png" {
		t.Fatalf("filename mismatch: got %q", item.Filename)
	}
	if !strings.HasPrefix(item.FileURL, "file://") {
		t.Fatalf("expected file URL, got %q", item.FileURL)
	}
	if _, err := os.Stat(item.AbsPath); err != nil {
		t.Fatalf("prepared file does not exist at %q: %v", item.AbsPath, err)
	}

	if !strings.Contains(filepath.Base(item.AbsPath), "5_") {
		t.Fatalf("expected prepared path basename to include clip id, got %q", filepath.Base(item.AbsPath))
	}

	if !strings.HasPrefix(item.TransferURL, "/transfer/") {
		t.Fatalf("transfer_url should start with /transfer/, got %q", item.TransferURL)
	}
	if !strings.HasSuffix(item.TransferURL, "/"+filepath.Base(item.AbsPath)) {
		t.Fatalf("transfer_url should end with filename, got %q", item.TransferURL)
	}
	// Token-based URL should have 3 segments: /transfer/{token}/{filename}
	parts := strings.Split(strings.TrimPrefix(item.TransferURL, "/transfer/"), "/")
	if len(parts) != 2 || len(parts[0]) != 32 {
		t.Fatalf("transfer_url should contain a 32-char token, got %q", item.TransferURL)
	}

	now := time.Now()
	if item.LeaseExpiresAt.Before(now.Add(55*time.Minute)) || item.LeaseExpiresAt.After(now.Add(65*time.Minute)) {
		t.Fatalf("unexpected lease expiry: %v", item.LeaseExpiresAt)
	}
}

func TestPrepareClipForTransferMissingClip(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	app := &App{db: db, tempDir: tempDir}
	app.tempStore = NewTempClipStore(db, tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
	service := NewTransferService(app)

	_, err := service.PrepareClipForTransfer(PrepareTransferRequest{ClipID: 999, Channel: "drag_out"})
	if err == nil {
		t.Fatalf("expected error for missing clip")
	}
	if !strings.Contains(err.Error(), "clip not found") {
		t.Fatalf("expected clip not found error, got %v", err)
	}
}

func TestStartNativeDragOutRequiresInput(t *testing.T) {
	caps := buildTransferCapabilities(goruntime.GOOS)
	if !caps.DragOut.NativeDrag {
		t.Skip("native drag not supported on this platform")
	}

	service := NewTransferService(&App{})

	ok, err := service.StartNativeDragOut(StartNativeDragRequest{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if ok {
		t.Fatalf("expected started=false")
	}
	if !strings.Contains(err.Error(), "either abs_path or clip_id must be provided") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartNativeDragOutRejectsRelativePath(t *testing.T) {
	caps := buildTransferCapabilities(goruntime.GOOS)
	if !caps.DragOut.NativeDrag {
		t.Skip("native drag not supported on this platform")
	}

	service := NewTransferService(&App{})

	ok, err := service.StartNativeDragOut(StartNativeDragRequest{
		AbsPath: "relative/path.txt",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if ok {
		t.Fatalf("expected started=false")
	}
	if !strings.Contains(err.Error(), "abs_path must be absolute") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileURLFromAbsPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "unix path", input: "/tmp/file.png", expect: "file:///tmp/file.png"},
		{name: "windows drive path", input: "C:/Users/test/file.png", expect: "file:///C:/Users/test/file.png"},
		{name: "windows path with spaces", input: "C:/Users/My User/file.png", expect: "file:///C:/Users/My%20User/file.png"},
		{name: "unix path with spaces", input: "/tmp/my files/test.png", expect: "file:///tmp/my%20files/test.png"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fileURLFromAbsPath(tc.input)
			if got != tc.expect {
				t.Fatalf("fileURLFromAbsPath(%q) = %q, want %q", tc.input, got, tc.expect)
			}
		})
	}
}

func TestGetExistingPreparedClipForTransfer(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		9, "text/plain", []byte("hello"), "note.txt")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	app := &App{
		db:      db,
		tempDir: tempDir,
	}
	app.transferHandler = &TransferFileHandler{app: app}
	app.tempStore = NewTempClipStore(db, tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
	service := NewTransferService(app)

	// No existing file yet.
	found, err := service.GetExistingPreparedClipForTransfer(PrepareTransferRequest{ClipID: 9, Channel: "drag_out"})
	if err != nil {
		t.Fatalf("lookup existing failed: %v", err)
	}
	if found != nil {
		t.Fatalf("expected nil when no prepared file exists")
	}

	_, err = service.PrepareClipForTransfer(PrepareTransferRequest{ClipID: 9, Channel: "drag_out"})
	if err != nil {
		t.Fatalf("prepare transfer failed: %v", err)
	}

	found, err = service.GetExistingPreparedClipForTransfer(PrepareTransferRequest{ClipID: 9, Channel: "drag_out"})
	if err != nil {
		t.Fatalf("lookup existing failed: %v", err)
	}
	if found == nil {
		t.Fatalf("expected prepared item")
	}
	if found.ClipID != 9 {
		t.Fatalf("expected clip id 9, got %d", found.ClipID)
	}
	if !strings.HasPrefix(found.FileURL, "file://") {
		t.Fatalf("expected file url, got %q", found.FileURL)
	}
	if !strings.HasPrefix(found.TransferURL, "/transfer/") || !strings.Contains(found.TransferURL[len("/transfer/"):], "/") {
		t.Fatalf("expected token-based transfer url, got %q", found.TransferURL)
	}
}
