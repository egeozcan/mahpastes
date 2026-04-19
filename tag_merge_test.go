package main

import (
	"database/sql"
	"testing"
)

// insertTestClip inserts a clip into the real initDB schema (no size column)
// and returns its ID.
func insertTestClip(t *testing.T, app *App, filename, contentType string, data []byte) int64 {
	t.Helper()
	res, err := app.db.Exec(
		`INSERT INTO clips (filename, content_type, data) VALUES (?, ?, ?)`,
		filename, contentType, data,
	)
	if err != nil {
		t.Fatalf("insertTestClip: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestPreviewMergeTag_CountsClipsAndDescendants(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("a/x")
	_, _ = app.CreateTag("a/x/foo")
	_, _ = app.CreateTag("a/x/bar")
	dst, _ := app.CreateTag("b/y")

	clipID := insertTestClip(t, app, "clip.txt", "text/plain", []byte("hi"))
	app.AddTagToClip(clipID, src.ID)

	preview, err := app.PreviewMergeTag(src.ID, dst.ID)
	if err != nil {
		t.Fatalf("PreviewMergeTag: %v", err)
	}
	if preview.ClipCount != 1 {
		t.Fatalf("ClipCount = %d, want 1", preview.ClipCount)
	}
	if preview.DescendantCount != 2 {
		t.Fatalf("DescendantCount = %d, want 2", preview.DescendantCount)
	}
	if len(preview.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %v", preview.Blockers)
	}
}

func TestPreviewMergeTag_BlockedBySelf(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	src, _ := app.CreateTag("t")
	preview, err := app.PreviewMergeTag(src.ID, src.ID)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview.Blockers) == 0 {
		t.Fatalf("expected self blocker")
	}
}

func TestPreviewMergeTag_BlockedByDestinationIsDescendant(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	src, _ := app.CreateTag("a")
	dst, _ := app.CreateTag("a/x")
	preview, _ := app.PreviewMergeTag(src.ID, dst.ID)
	if len(preview.Blockers) == 0 {
		t.Fatalf("expected descendant blocker")
	}
}

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
