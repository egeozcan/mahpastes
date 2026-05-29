package app

import (
	"database/sql"
	"strings"
	"testing"
	"time"
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

func TestMergeTag_BasicReassignment(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("a/x")
	dst, _ := app.CreateTag("b/y")

	clipID := insertTestClip(t, app, "c.txt", "text/plain", []byte("x"))
	app.AddTagToClip(clipID, src.ID)

	if err := app.MergeTag(src.ID, dst.ID); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	var count int
	app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, src.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("source tag should be deleted")
	}
	tags, _ := app.GetClipTags(clipID)
	foundDst := false
	for _, tag := range tags {
		if tag.ID == dst.ID {
			foundDst = true
		}
	}
	if !foundDst {
		t.Fatalf("clip %d must have destination tag", clipID)
	}
}

func TestMergeTag_SubtreeMove(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	_, _ = app.CreateTag("a/x")
	foo, _ := app.CreateTag("a/x/foo")
	bar, _ := app.CreateTag("a/x/bar/baz")
	_, _ = app.CreateTag("b/y")

	var srcID, dstID int64
	app.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/x'`).Scan(&srcID)
	app.db.QueryRow(`SELECT id FROM tags WHERE name = 'b/y'`).Scan(&dstID)

	if err := app.MergeTag(srcID, dstID); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}

	var name string
	app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, foo.ID).Scan(&name)
	if name != "b/y/foo" {
		t.Fatalf("foo should be renamed to b/y/foo, got %q", name)
	}
	app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, bar.ID).Scan(&name)
	if name != "b/y/bar/baz" {
		t.Fatalf("bar/baz should be renamed to b/y/bar/baz, got %q", name)
	}
}

func TestMergeTag_BlockedByActiveShare(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("src-share")
	dst, _ := app.CreateTag("dst-share")
	_, err := app.db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, created_at) VALUES (?, X'00', X'01', 0)`, src.ID)
	if err != nil {
		t.Fatalf("insert share: %v", err)
	}
	err = app.MergeTag(src.ID, dst.ID)
	if err == nil || !strings.Contains(err.Error(), "share") {
		t.Fatalf("expected share blocker, got %v", err)
	}
}

// Regression test for the tree-exclusivity bypass. Merge must NOT leave a
// clip with two tags in the same root tree.
func TestMergeTag_PreservesSameTreeExclusivity_CrossRoot(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("a/x")
	_, _ = app.CreateTag("c/other")
	dst, _ := app.CreateTag("c/y")

	clipID := insertTestClip(t, app, "c.txt", "text/plain", []byte("x"))
	if err := app.AddTagToClip(clipID, src.ID); err != nil {
		t.Fatalf("add src: %v", err)
	}
	// Bypass AddTagToClip's exclusivity on purpose — seed a clip that
	// already carries a tag in destination's root tree.
	var cOtherID int64
	app.db.QueryRow(`SELECT id FROM tags WHERE name = 'c/other'`).Scan(&cOtherID)
	if _, err := app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipID, cOtherID); err != nil {
		t.Fatalf("seed c/other: %v", err)
	}

	if err := app.MergeTag(src.ID, dst.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	tags, _ := app.GetClipTags(clipID)
	names := make([]string, 0, len(tags))
	for _, tt := range tags {
		names = append(names, tt.Name)
	}
	if len(tags) != 1 || tags[0].ID != dst.ID {
		t.Fatalf("expected clip to hold only dest c/y, got %v", names)
	}
}

// Clips that weren't part of the merge must not be rewritten.
func TestMergeTag_PreservesSameTreeExclusivity_DoesNotTouchUnaffectedClips(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	src, _ := app.CreateTag("a/x")
	dst, _ := app.CreateTag("c/y")
	_, _ = app.CreateTag("c/other")

	// Clip A participates in the merge.
	clipA := insertTestClip(t, app, "a.txt", "text/plain", []byte("A"))
	app.AddTagToClip(clipA, src.ID)

	// Clip B has dest AND dest-root sibling already — NOT touched by merge.
	clipB := insertTestClip(t, app, "b.txt", "text/plain", []byte("B"))
	var cOtherID int64
	app.db.QueryRow(`SELECT id FROM tags WHERE name = 'c/other'`).Scan(&cOtherID)
	app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipB, dst.ID)
	app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipB, cOtherID)

	if err := app.MergeTag(src.ID, dst.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	bTags, _ := app.GetClipTags(clipB)
	if len(bTags) != 2 {
		t.Fatalf("merge should not touch unrelated clip B, got %d tags", len(bTags))
	}
}

func TestMergeTag_EmitsFrontendEvent(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	captured := make(chan map[string]any, 1)
	setTestEventSink(t, app, func(name string, data ...interface{}) {
		if name == "tag:merged" && len(data) > 0 {
			if m, ok := data[0].(map[string]any); ok {
				select {
				case captured <- m:
				default:
				}
			}
		}
	})

	src, _ := app.CreateTag("src-evt")
	dst, _ := app.CreateTag("dst-evt")
	if err := app.MergeTag(src.ID, dst.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	select {
	case m := <-captured:
		if m["source_id"] != src.ID || m["dest_id"] != dst.ID {
			t.Fatalf("unexpected payload: %+v", m)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("no tag:merged event emitted")
	}
}
