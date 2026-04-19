package main

import (
	"database/sql"
	"testing"
)

func TestMigrateTagReferences_APIKeyScope(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("src")
	dst, _ := app.CreateTag("dst")
	app.db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES ('k', 'h', 'p', 'viewer', ?)`, src.ID)

	tx, _ := app.db.Begin()
	defer tx.Rollback()
	if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tx.Commit()

	var scoped sql.NullInt64
	app.db.QueryRow(`SELECT scoped_tag_id FROM api_keys WHERE name = 'k'`).Scan(&scoped)
	if !scoped.Valid || scoped.Int64 != dst.ID {
		t.Fatalf("scope should be %d, got %v", dst.ID, scoped)
	}
}

func TestMigrateTagReferences_WatchFolderAutoTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("src")
	dst, _ := app.CreateTag("dst")
	app.db.Exec(`INSERT INTO watched_folders (path, auto_tag_id) VALUES ('/tmp/y', ?)`, src.ID)

	tx, _ := app.db.Begin()
	defer tx.Rollback()
	if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tx.Commit()

	var at sql.NullInt64
	app.db.QueryRow(`SELECT auto_tag_id FROM watched_folders WHERE path = '/tmp/y'`).Scan(&at)
	if !at.Valid || at.Int64 != dst.ID {
		t.Fatalf("auto_tag_id should be %d, got %v", dst.ID, at)
	}
}

func TestMigrateTagReferences_HiddenList(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("src")
	dst, _ := app.CreateTag("dst")
	if err := app.SetHiddenTags([]int64{src.ID}); err != nil {
		t.Fatalf("SetHiddenTags: %v", err)
	}

	tx, _ := app.db.Begin()
	defer tx.Rollback()
	if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	tx.Commit()

	ids, _ := app.GetHiddenTags()
	hasDst := false
	for _, id := range ids {
		if id == src.ID {
			t.Fatalf("hidden list should not still contain src id")
		}
		if id == dst.ID {
			hasDst = true
		}
	}
	if !hasDst {
		t.Fatalf("hidden list should now contain dst id %d, got %v", dst.ID, ids)
	}
}
