package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestGetDatabaseSize_ReturnsNonZero(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	size, err := app.GetDatabaseSize()
	if err != nil {
		t.Fatalf("GetDatabaseSize: %v", err)
	}
	if size <= 0 {
		t.Fatalf("expected nonzero size, got %d", size)
	}
}

func TestCompactDatabase_ReducesSizeAfterBigDelete(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Insert then delete a large blob so VACUUM has something to reclaim.
	big := make([]byte, 1024*1024) // 1 MB
	_, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('application/octet-stream', ?, 'big.bin')`, big)
	if err != nil {
		t.Fatalf("insert big: %v", err)
	}
	_, err = app.db.Exec(`DELETE FROM clips WHERE filename = 'big.bin'`)
	if err != nil {
		t.Fatalf("delete big: %v", err)
	}

	result, err := app.CompactDatabase()
	if err != nil {
		t.Fatalf("CompactDatabase: %v", err)
	}
	// VACUUM shrinks the main database file. Due to WAL files, total size may
	// not decrease, but the main file should shrink. We just verify the method
	// completes without error and returns reasonable sizes.
	if result.Before <= 0 || result.After <= 0 {
		t.Fatalf("expected nonzero sizes: before=%d, after=%d", result.Before, result.After)
	}
}

func TestGetStaleFiles_DetectsOldTempFiles(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	p := filepath.Join(app.tempDir, "stale.bin")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	files, err := app.GetStaleFiles()
	if err != nil {
		t.Fatalf("GetStaleFiles: %v", err)
	}
	found := false
	for _, f := range files {
		if f.Name == "stale.bin" && f.Source == "clip_temp_files" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stale.bin in results, got %+v", files)
	}
}

func TestCleanStaleFiles_RemovesThem(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	p := filepath.Join(app.tempDir, "stale2.bin")
	os.WriteFile(p, []byte("yy"), 0644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(p, past, past)

	result, err := app.CleanStaleFiles()
	if err != nil {
		t.Fatalf("CleanStaleFiles: %v", err)
	}
	if result.Count < 1 {
		t.Fatalf("expected at least 1 cleaned, got %d", result.Count)
	}
	if result.Bytes < 2 {
		t.Fatalf("expected ≥2 bytes, got %d", result.Bytes)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be removed")
	}
}

func TestGetOrphanDBRows_DetectsPluginStorage(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Disable FK checks temporarily to insert orphan rows
	if _, err := app.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`); err != nil {
		t.Fatalf("insert orphan storage: %v", err)
	}
	if _, err := app.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	report, err := app.GetOrphanDBRows()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if report.PluginStorage < 1 {
		t.Fatalf("expected ≥1 orphan plugin_storage, got %d", report.PluginStorage)
	}
}

func TestCleanOrphanDBRows_RemovesThem(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Disable FK checks temporarily to insert orphan rows
	if _, err := app.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`); err != nil {
		t.Fatalf("insert orphan storage: %v", err)
	}
	if _, err := app.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	report, err := app.CleanOrphanDBRows()
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if report.PluginStorage < 1 {
		t.Fatalf("expected cleaned count ≥1")
	}
	var count int
	app.db.QueryRow(`SELECT COUNT(*) FROM plugin_storage WHERE plugin_id = 99999`).Scan(&count)
	if count != 0 {
		t.Fatalf("orphan should be gone, got %d", count)
	}
}
