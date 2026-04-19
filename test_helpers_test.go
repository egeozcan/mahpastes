package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/internal/wailsbridge"
)

// setupTestApp returns an App backed by a real initDB()-initialized SQLite
// DB, plus a ShareManager, ServeManager, wailsbridge, and tempDir. Use this
// for any test that touches follows/watched_folders/settings/api_keys/
// plugin_storage/shares/plugin_permissions, or that expects serveManager /
// shareManager / bridge to be non-nil.
//
// The older setupTestDBWithTags (tag_hierarchy_test.go:142) only covers
// clips+tags+clip_tags; keep using it for narrow tag-hierarchy tests.
func setupTestApp(t *testing.T) (*App, func()) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}

	ctx := context.Background()
	app := &App{
		db:      db,
		bridge:  wailsbridge.NewForTesting(),
		tempDir: filepath.Join(dir, "clip_temp_files"),
	}
	if err := os.MkdirAll(app.tempDir, 0o755); err != nil {
		t.Fatalf("mkdir tempDir: %v", err)
	}

	sm, err := NewShareManager(ctx, db, dir)
	if err != nil {
		t.Fatalf("NewShareManager: %v", err)
	}
	app.shareManager = sm

	// ServeManager only requires *App; no extra parameters.
	app.serveManager = NewServeManager(app)

	cleanup := func() {
		if app.shareManager != nil {
			app.shareManager.Stop()
		}
		if app.serveManager != nil {
			app.serveManager.StopAll()
		}
		db.Close()
	}
	return app, cleanup
}

func TestSetupTestApp_SmokeSchema(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	for _, table := range []string{
		"clips", "tags", "clip_tags", "settings", "watched_folders",
		"plugins", "plugin_permissions", "plugin_storage",
		"api_keys", "shares", "follows", "share_ring",
	} {
		var count int
		if err := app.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?`,
			table,
		).Scan(&count); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("expected table %q to exist after setupTestApp", table)
		}
	}
	if app.serveManager == nil {
		t.Error("serveManager must be non-nil")
	}
	if app.shareManager == nil {
		t.Error("shareManager must be non-nil")
	}
	if app.bridge == nil {
		t.Error("bridge must be non-nil")
	}
}
