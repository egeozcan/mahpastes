package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTempStoreTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.Exec(`
		CREATE TABLE clips (
			id INTEGER PRIMARY KEY,
			content_type TEXT NOT NULL,
			data BLOB NOT NULL,
			filename TEXT
		)
	`)
	if err != nil {
		t.Fatalf("failed to create clips table: %v", err)
	}

	return db
}

func TestParseClipIDFromTempFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expectID int64
		expectOK bool
	}{
		{name: "id_with_original_name", filename: "42_document.txt", expectID: 42, expectOK: true},
		{name: "id_with_extension", filename: "7.png", expectID: 7, expectOK: true},
		{name: "id_only", filename: "19", expectID: 19, expectOK: true},
		{name: "invalid_prefix", filename: "clip_12.txt", expectID: 0, expectOK: false},
		{name: "invalid_separator", filename: "12x.txt", expectID: 0, expectOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := parseClipIDFromTempFilename(tc.filename)
			if ok != tc.expectOK {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.expectOK)
			}
			if id != tc.expectID {
				t.Fatalf("id mismatch: got %d want %d", id, tc.expectID)
			}
		})
	}
}

func TestPrepareClipFile_LeaseRefresh(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		1, "image/png", []byte("first"), "lease.png")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	store := NewTempClipStore(db, tempDir, 60*time.Minute, 10*time.Minute)
	first, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}

	firstInfo, err := os.Stat(first.AbsPath)
	if err != nil {
		t.Fatalf("failed to stat first prepared file: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	second, err := store.PrepareClipFile(1)
	if err != nil {
		t.Fatalf("second prepare failed: %v", err)
	}

	if first.AbsPath != second.AbsPath {
		t.Fatalf("expected same temp file path, got %q and %q", first.AbsPath, second.AbsPath)
	}

	secondInfo, err := os.Stat(second.AbsPath)
	if err != nil {
		t.Fatalf("failed to stat second prepared file: %v", err)
	}

	if secondInfo.ModTime().Before(firstInfo.ModTime()) {
		t.Fatalf("expected lease modtime to refresh, got first=%v second=%v", firstInfo.ModTime(), secondInfo.ModTime())
	}
}

func TestPrune_RemovesStaleAndOrphanFiles(t *testing.T) {
	tempDir := t.TempDir()
	db := newTempStoreTestDB(t, tempDir)

	_, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, ?, ?, ?)`,
		1, "text/plain", []byte("keep"), "keep.txt")
	if err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}

	store := NewTempClipStore(db, tempDir, 60*time.Minute, 10*time.Minute)

	freshExisting := filepath.Join(tempDir, "1_keep.txt")
	staleExisting := filepath.Join(tempDir, "1_stale.txt")
	freshOrphan := filepath.Join(tempDir, "999_orphan.txt")
	staleUnknown := filepath.Join(tempDir, "random.tmp")

	for _, file := range []string{freshExisting, staleExisting, freshOrphan, staleUnknown} {
		if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
			t.Fatalf("failed to create %s: %v", file, err)
		}
	}

	staleTime := time.Now().Add(-2 * time.Hour)
	for _, file := range []string{staleExisting, staleUnknown} {
		if err := os.Chtimes(file, staleTime, staleTime); err != nil {
			t.Fatalf("failed to set stale modtime for %s: %v", file, err)
		}
	}

	if err := store.Prune(true); err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if _, err := os.Stat(freshExisting); err != nil {
		t.Fatalf("expected fresh existing file to remain, stat error: %v", err)
	}
	if _, err := os.Stat(staleExisting); !os.IsNotExist(err) {
		t.Fatalf("expected stale existing file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(freshOrphan); !os.IsNotExist(err) {
		t.Fatalf("expected orphan file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(staleUnknown); !os.IsNotExist(err) {
		t.Fatalf("expected stale unknown file to be removed, got err=%v", err)
	}
}
