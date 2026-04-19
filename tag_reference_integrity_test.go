package main

import (
	"database/sql"
	"strings"
	"testing"
	"time"
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

func TestUpdateTag_BlockedByServedSubtree(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	_, _ = app.CreateTag("a/x")
	child, _ := app.CreateTag("a/x/foo")

	if app.serveManager == nil {
		t.Skip("serveManager not initialized in test harness")
	}
	if _, err := app.serveManager.StartServing(child.ID, 0, false, "none"); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	defer app.serveManager.StopServing(child.ID)

	// Rename the ancestor — should be blocked because a descendant is served.
	var parentID int64
	app.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/x'`).Scan(&parentID)
	err := app.UpdateTag(parentID, "a/z", "#aaa")
	if err == nil {
		t.Fatalf("expected rename to be blocked, got nil")
	}
	if !strings.Contains(err.Error(), "served") {
		t.Fatalf("expected 'served' in error, got %q", err.Error())
	}
}

// Asserts that UpdateTag emits a "tag:updated" runtime event with old+new
// names, so the frontend can re-resolve folder-view state.
func TestUpdateTag_EmitsFrontendEvent(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	captured := make(chan map[string]any, 1)
	app.bridge.SetTestEventSink(func(name string, data ...interface{}) {
		if name == "tag:updated" && len(data) > 0 {
			if m, ok := data[0].(map[string]any); ok {
				select {
				case captured <- m:
				default:
				}
			}
		}
	})

	tag, _ := app.CreateTag("before")
	if err := app.UpdateTag(tag.ID, "after", "#aaa"); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	select {
	case m := <-captured:
		if m["old_name"] != "before" || m["new_name"] != "after" {
			t.Fatalf("unexpected payload: %+v", m)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("no tag:updated event emitted")
	}
}
