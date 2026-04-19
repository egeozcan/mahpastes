package main

import (
	"database/sql"
	"strings"
	"testing"
)

func TestCheckTagReferencePreconditions_NoBlockers(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("unreferenced")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	blockers, err := app.checkTagReferencePreconditions(tag.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(blockers) != 0 {
		t.Fatalf("expected no blockers, got %v", blockers)
	}
}

func TestCheckTagReferencePreconditions_BlockedByFollow(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("subscribed")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO follows
		(remote_peer_id, symkey, local_tag_id, created_at)
		VALUES ('peer', X'00', ?, strftime('%s','now'))`, tag.ID); err != nil {
		t.Fatalf("insert follow: %v", err)
	}
	blockers, err := app.checkTagReferencePreconditions(tag.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(blockers) == 0 {
		t.Fatalf("expected follow blocker, got empty")
	}
}

func TestDeleteTag_BlockedByFollow(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, _ := app.CreateTag("blocked")
	if _, err := app.db.Exec(`INSERT INTO follows
		(remote_peer_id, symkey, local_tag_id, created_at)
		VALUES ('peer', X'00', ?, strftime('%s','now'))`, tag.ID); err != nil {
		t.Fatalf("insert follow: %v", err)
	}

	err := app.DeleteTag(tag.ID)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "follow") {
		t.Fatalf("expected error mentioning follow, got %q", err.Error())
	}

	var count int
	app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tag.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("tag should still exist after blocked delete")
	}
}

func TestDeleteTag_NullsWatchFolderAutoTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, _ := app.CreateTag("autotag-target")
	if _, err := app.db.Exec(`INSERT INTO watched_folders (path, auto_tag_id) VALUES ('/tmp/x', ?)`, tag.ID); err != nil {
		t.Fatalf("insert wf: %v", err)
	}

	if err := app.DeleteTag(tag.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var autoTag sql.NullInt64
	app.db.QueryRow(`SELECT auto_tag_id FROM watched_folders WHERE path = '/tmp/x'`).Scan(&autoTag)
	if autoTag.Valid {
		t.Fatalf("auto_tag_id should be NULL, got %d", autoTag.Int64)
	}
}

func TestDeleteTag_RemovesFromHiddenList(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, _ := app.CreateTag("hide-me")
	if err := app.SetHiddenTags([]int64{tag.ID}); err != nil {
		t.Fatalf("SetHiddenTags: %v", err)
	}
	if err := app.DeleteTag(tag.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ids, err := app.GetHiddenTags()
	if err != nil {
		t.Fatalf("GetHiddenTags: %v", err)
	}
	for _, id := range ids {
		if id == tag.ID {
			t.Fatalf("hidden list should not contain deleted tag id %d", id)
		}
	}
}
