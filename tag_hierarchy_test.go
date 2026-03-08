package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Pure helper tests
// ---------------------------------------------------------------------------

func TestGetTagDepth(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"photos", 0},
		{"photos/vacation", 1},
		{"photos/vacation/2024", 2},
		{"a/b/c/d/e", 4},
		{"", 0},
	}
	for _, tt := range tests {
		if got := getTagDepth(tt.name); got != tt.want {
			t.Errorf("getTagDepth(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestGetParentTagName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"photos", ""},
		{"photos/vacation", "photos"},
		{"photos/vacation/2024", "photos/vacation"},
		{"a/b/c", "a/b"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := getParentTagName(tt.name); got != tt.want {
			t.Errorf("getParentTagName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGetShortTagName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"photos", "photos"},
		{"photos/vacation", "vacation"},
		{"photos/vacation/2024", "2024"},
		{"a/b/c", "c"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := getShortTagName(tt.name); got != tt.want {
			t.Errorf("getShortTagName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGetAncestorTagNames(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"photos", nil},
		{"photos/vacation", []string{"photos"}},
		{"photos/vacation/2024", []string{"photos", "photos/vacation"}},
		{"a/b/c/d", []string{"a", "a/b", "a/b/c"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := getAncestorTagNames(tt.name)
		if !stringSliceEqual(got, tt.want) {
			t.Errorf("getAncestorTagNames(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsDescendantOf(t *testing.T) {
	tests := []struct {
		child, parent string
		want          bool
	}{
		{"photos/vacation", "photos", true},
		{"photos/vacation/2024", "photos", true},
		{"photos/vacation/2024", "photos/vacation", true},
		{"photos", "photos", false},          // self is not descendant
		{"photos2/vacation", "photos", false}, // different prefix
		{"photography", "photos", false},      // "photography" != "photos/..."
		{"", "photos", false},
		{"photos/vacation", "", false}, // empty parent is not a valid hierarchy root
	}
	for _, tt := range tests {
		if got := isDescendantOf(tt.child, tt.parent); got != tt.want {
			t.Errorf("isDescendantOf(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}

func TestIsImmediateChildOf(t *testing.T) {
	tests := []struct {
		child, parent string
		want          bool
	}{
		// Parent = "" means top-level check
		{"photos", "", true},
		{"photos/vacation", "", false},

		// Normal parent
		{"photos/vacation", "photos", true},
		{"photos/vacation/2024", "photos", false},     // grandchild, not immediate
		{"photos/vacation/2024", "photos/vacation", true},
		{"photos", "photos", false},                    // self
		{"photography", "photos", false},               // different prefix
		{"photos2/vacation", "photos", false},

		// Edge cases
		{"a/b", "a", true},
		{"a/b/c", "a", false},
		{"a/b/c", "a/b", true},
	}
	for _, tt := range tests {
		if got := isImmediateChildOf(tt.child, tt.parent); got != tt.want {
			t.Errorf("isImmediateChildOf(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration test helpers
// ---------------------------------------------------------------------------

func setupTestDBWithTags(t *testing.T) (*App, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys%3Don")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	for _, stmt := range []string{
		`CREATE TABLE clips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content_type TEXT NOT NULL,
			data BLOB NOT NULL,
			filename TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			is_archived INTEGER DEFAULT 0,
			content_hash TEXT DEFAULT '',
			size INTEGER DEFAULT 0
		)`,
		`CREATE TABLE tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL
		)`,
		`CREATE TABLE clip_tags (
			clip_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (clip_id, tag_id),
			FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	app := &App{db: db}
	cleanup := func() { db.Close() }
	return app, cleanup
}

func (a *App) createTestClip(filename, contentType string, data []byte) (int64, error) {
	result, err := a.db.Exec(
		"INSERT INTO clips (filename, content_type, data, size) VALUES (?, ?, ?, ?)",
		filename, contentType, data, len(data))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func createTestTag(t *testing.T, db *sql.DB, name, color string) int64 {
	t.Helper()
	result, err := db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
	if err != nil {
		t.Fatalf("failed to create tag %q: %v", name, err)
	}
	id, _ := result.LastInsertId()
	return id
}

func tagClip(t *testing.T, db *sql.DB, clipID, tagID int64) {
	t.Helper()
	_, err := db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID)
	if err != nil {
		t.Fatalf("failed to tag clip %d with tag %d: %v", clipID, tagID, err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests for DB-backed methods
// ---------------------------------------------------------------------------

func TestGetDescendantTagIDs(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	photosID := createTestTag(t, app.db, "photos", "#aaa")
	vacationID := createTestTag(t, app.db, "photos/vacation", "#bbb")
	beachID := createTestTag(t, app.db, "photos/vacation/beach", "#ccc")
	workID := createTestTag(t, app.db, "photos/work", "#ddd")
	createTestTag(t, app.db, "documents", "#eee") // unrelated

	ids, err := app.getDescendantTagIDs(photosID)
	if err != nil {
		t.Fatalf("getDescendantTagIDs: %v", err)
	}
	wantIDs := map[int64]bool{vacationID: true, beachID: true, workID: true}
	if len(ids) != len(wantIDs) {
		t.Fatalf("got %d descendants, want %d", len(ids), len(wantIDs))
	}
	for _, id := range ids {
		if !wantIDs[id] {
			t.Errorf("unexpected descendant ID %d", id)
		}
	}

	// vacation should only have beach as descendant
	ids, err = app.getDescendantTagIDs(vacationID)
	if err != nil {
		t.Fatalf("getDescendantTagIDs(vacation): %v", err)
	}
	if len(ids) != 1 || ids[0] != beachID {
		t.Errorf("vacation descendants = %v, want [%d]", ids, beachID)
	}

	// leaf tag should have no descendants
	ids, err = app.getDescendantTagIDs(beachID)
	if err != nil {
		t.Fatalf("getDescendantTagIDs(beach): %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("beach descendants = %v, want empty", ids)
	}

	// non-existent tag should error
	_, err = app.getDescendantTagIDs(9999)
	if err == nil {
		t.Error("expected error for non-existent tag")
	}
}

func TestGetChildTags(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	photosID := createTestTag(t, app.db, "photos", "#aaa")
	createTestTag(t, app.db, "photos/vacation", "#bbb")
	createTestTag(t, app.db, "photos/vacation/beach", "#ccc")
	createTestTag(t, app.db, "photos/work", "#ddd")
	createTestTag(t, app.db, "documents", "#eee")

	children, err := app.getChildTags(photosID)
	if err != nil {
		t.Fatalf("getChildTags: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("got %d children, want 2", len(children))
	}
	names := map[string]bool{}
	for _, c := range children {
		names[c.Name] = true
	}
	if !names["photos/vacation"] || !names["photos/work"] {
		t.Errorf("children names = %v, want photos/vacation and photos/work", names)
	}

	// non-existent tag should error
	_, err = app.getChildTags(9999)
	if err == nil {
		t.Error("expected error for non-existent tag")
	}
}

func TestGetTopLevelTags(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	createTestTag(t, app.db, "photos", "#aaa")
	createTestTag(t, app.db, "photos/vacation", "#bbb")
	createTestTag(t, app.db, "documents", "#ccc")
	createTestTag(t, app.db, "documents/work", "#ddd")

	tags, err := app.getTopLevelTags()
	if err != nil {
		t.Fatalf("getTopLevelTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("got %d top-level tags, want 2", len(tags))
	}
	names := map[string]bool{}
	for _, tag := range tags {
		names[tag.Name] = true
	}
	if !names["photos"] || !names["documents"] {
		t.Errorf("top-level names = %v, want photos and documents", names)
	}
}

func TestGetTopLevelTags_Empty(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	tags, err := app.getTopLevelTags()
	if err != nil {
		t.Fatalf("getTopLevelTags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("got %d tags, want 0", len(tags))
	}
}

func TestGetTopLevelTags_WithCounts(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	photosID := createTestTag(t, app.db, "photos", "#aaa")
	createTestTag(t, app.db, "photos/vacation", "#bbb")
	docsID := createTestTag(t, app.db, "documents", "#ccc")

	clip1, _ := app.createTestClip("a.png", "image/png", []byte("a"))
	clip2, _ := app.createTestClip("b.png", "image/png", []byte("b"))
	tagClip(t, app.db, clip1, photosID)
	tagClip(t, app.db, clip2, photosID)
	tagClip(t, app.db, clip1, docsID)

	tags, err := app.getTopLevelTags()
	if err != nil {
		t.Fatalf("getTopLevelTags: %v", err)
	}
	for _, tag := range tags {
		switch tag.Name {
		case "photos":
			if tag.Count != 2 {
				t.Errorf("photos count = %d, want 2", tag.Count)
			}
		case "documents":
			if tag.Count != 1 {
				t.Errorf("documents count = %d, want 1", tag.Count)
			}
		}
	}
}

func TestGetDescendantClipCount(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	photosID := createTestTag(t, app.db, "photos", "#aaa")
	vacationID := createTestTag(t, app.db, "photos/vacation", "#bbb")
	beachID := createTestTag(t, app.db, "photos/vacation/beach", "#ccc")
	workID := createTestTag(t, app.db, "photos/work", "#ddd")

	clip1, _ := app.createTestClip("a.png", "image/png", []byte("a"))
	clip2, _ := app.createTestClip("b.png", "image/png", []byte("b"))
	clip3, _ := app.createTestClip("c.png", "image/png", []byte("c"))

	// clip1 tagged with photos and photos/vacation (should count once)
	tagClip(t, app.db, clip1, photosID)
	tagClip(t, app.db, clip1, vacationID)

	// clip2 tagged with photos/vacation/beach
	tagClip(t, app.db, clip2, beachID)

	// clip3 tagged with photos/work
	tagClip(t, app.db, clip3, workID)

	count, err := app.getDescendantClipCount(photosID)
	if err != nil {
		t.Fatalf("getDescendantClipCount(photos): %v", err)
	}
	if count != 3 {
		t.Errorf("photos descendant clip count = %d, want 3", count)
	}

	count, err = app.getDescendantClipCount(vacationID)
	if err != nil {
		t.Fatalf("getDescendantClipCount(vacation): %v", err)
	}
	if count != 2 {
		t.Errorf("vacation descendant clip count = %d, want 2", count)
	}

	count, err = app.getDescendantClipCount(beachID)
	if err != nil {
		t.Fatalf("getDescendantClipCount(beach): %v", err)
	}
	if count != 1 {
		t.Errorf("beach descendant clip count = %d, want 1", count)
	}

	// non-existent tag should error
	_, err = app.getDescendantClipCount(9999)
	if err == nil {
		t.Error("expected error for non-existent tag")
	}
}

func TestGetChildTags_WithCounts(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	photosID := createTestTag(t, app.db, "photos", "#aaa")
	vacationID := createTestTag(t, app.db, "photos/vacation", "#bbb")
	workID := createTestTag(t, app.db, "photos/work", "#ddd")

	clip1, _ := app.createTestClip("a.png", "image/png", []byte("a"))
	clip2, _ := app.createTestClip("b.png", "image/png", []byte("b"))
	tagClip(t, app.db, clip1, vacationID)
	tagClip(t, app.db, clip2, vacationID)
	tagClip(t, app.db, clip1, workID)

	children, err := app.getChildTags(photosID)
	if err != nil {
		t.Fatalf("getChildTags: %v", err)
	}
	for _, child := range children {
		switch child.Name {
		case "photos/vacation":
			if child.Count != 2 {
				t.Errorf("vacation count = %d, want 2", child.Count)
			}
		case "photos/work":
			if child.Count != 1 {
				t.Errorf("work count = %d, want 1", child.Count)
			}
		}
	}
}

func TestGetChildTags_LeafReturnsEmpty(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	createTestTag(t, app.db, "photos", "#aaa")
	beachID := createTestTag(t, app.db, "photos/vacation/beach", "#ccc")

	children, err := app.getChildTags(beachID)
	if err != nil {
		t.Fatalf("getChildTags: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("got %d children for leaf tag, want 0", len(children))
	}
}

// ---------------------------------------------------------------------------
// CreateTag auto-intermediates tests
// ---------------------------------------------------------------------------

func TestCreateTagAutoCreatesIntermediates(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	tag, err := app.CreateTag("work/client1/projectABC")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tag.Name != "work/client1/projectABC" {
		t.Errorf("returned tag name = %q, want %q", tag.Name, "work/client1/projectABC")
	}

	// Verify ancestors were auto-created
	var workName, client1Name string
	err = app.db.QueryRow("SELECT name FROM tags WHERE name = ?", "work").Scan(&workName)
	if err != nil {
		t.Fatalf("ancestor 'work' not found: %v", err)
	}
	err = app.db.QueryRow("SELECT name FROM tags WHERE name = ?", "work/client1").Scan(&client1Name)
	if err != nil {
		t.Fatalf("ancestor 'work/client1' not found: %v", err)
	}

	// Total should be 3 tags
	var count int
	app.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
	if count != 3 {
		t.Errorf("total tag count = %d, want 3", count)
	}
}

func TestCreateTagIntermediatesInheritColor(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create root tag first with a known color
	_, err := app.db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", "work", "#FF0000")
	if err != nil {
		t.Fatalf("failed to create work tag: %v", err)
	}

	// Create a deeply nested tag — intermediate "work/client1" should inherit work's color
	tag, err := app.CreateTag("work/client1/projectABC")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// The created tag itself should inherit from "work" (via "work/client1" which inherits from "work")
	if tag.Color != "#FF0000" {
		t.Errorf("created tag color = %q, want %q", tag.Color, "#FF0000")
	}

	// Check the intermediate "work/client1" also inherited the color
	var client1Color string
	err = app.db.QueryRow("SELECT color FROM tags WHERE name = ?", "work/client1").Scan(&client1Color)
	if err != nil {
		t.Fatalf("ancestor 'work/client1' not found: %v", err)
	}
	if client1Color != "#FF0000" {
		t.Errorf("work/client1 color = %q, want %q", client1Color, "#FF0000")
	}
}

func TestCreateTagExistingIntermediatesNotDuplicated(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work" first
	_, err := app.CreateTag("work")
	if err != nil {
		t.Fatalf("CreateTag(work): %v", err)
	}

	// Now create a descendant — "work" should not be duplicated
	_, err = app.CreateTag("work/client1/projectABC")
	if err != nil {
		t.Fatalf("CreateTag(work/client1/projectABC): %v", err)
	}

	// Verify only one "work" tag exists
	var count int
	err = app.db.QueryRow("SELECT COUNT(*) FROM tags WHERE name = ?", "work").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count 'work' tags: %v", err)
	}
	if count != 1 {
		t.Errorf("'work' tag count = %d, want 1", count)
	}

	// Total: work + work/client1 + work/client1/projectABC = 3
	err = app.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count all tags: %v", err)
	}
	if count != 3 {
		t.Errorf("total tag count = %d, want 3", count)
	}
}

func TestCreateTagNoSlash(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	tag, err := app.CreateTag("simple")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tag.Name != "simple" {
		t.Errorf("returned tag name = %q, want %q", tag.Name, "simple")
	}
	if tag.ID == 0 {
		t.Error("returned tag ID should not be 0")
	}
	if tag.Color == "" {
		t.Error("returned tag color should not be empty")
	}

	// Verify only one tag was created (no intermediates)
	var count int
	app.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
	if count != 1 {
		t.Errorf("total tag count = %d, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// UpdateTag cascade rename tests
// ---------------------------------------------------------------------------

func TestUpdateTagCascadesRename(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create work/client1/projectABC (auto-creates intermediates)
	_, err := app.CreateTag("work/client1/projectABC")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// Get the "work" tag's ID
	var workID int64
	err = app.db.QueryRow("SELECT id FROM tags WHERE name = ?", "work").Scan(&workID)
	if err != nil {
		t.Fatalf("failed to find 'work' tag: %v", err)
	}

	// Rename "work" → "job"
	err = app.UpdateTag(workID, "job", "#aaa")
	if err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}

	// Verify all names updated
	for _, want := range []string{"job", "job/client1", "job/client1/projectABC"} {
		var name string
		err := app.db.QueryRow("SELECT name FROM tags WHERE name = ?", want).Scan(&name)
		if err != nil {
			t.Errorf("expected tag %q to exist after rename, but not found", want)
		}
	}

	// Verify old names are gone
	for _, old := range []string{"work", "work/client1", "work/client1/projectABC"} {
		var name string
		err := app.db.QueryRow("SELECT name FROM tags WHERE name = ?", old).Scan(&name)
		if err == nil {
			t.Errorf("old tag %q should not exist after rename", old)
		}
	}
}

func TestUpdateTagCascadesRenameNoFalsePositive(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work/client1" and "working/stuff"
	_, err := app.CreateTag("work/client1")
	if err != nil {
		t.Fatalf("CreateTag(work/client1): %v", err)
	}
	_, err = app.CreateTag("working/stuff")
	if err != nil {
		t.Fatalf("CreateTag(working/stuff): %v", err)
	}

	// Get the "work" tag's ID
	var workID int64
	err = app.db.QueryRow("SELECT id FROM tags WHERE name = ?", "work").Scan(&workID)
	if err != nil {
		t.Fatalf("failed to find 'work' tag: %v", err)
	}

	// Rename "work" → "job"
	err = app.UpdateTag(workID, "job", "#aaa")
	if err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}

	// Verify "work/client1" was renamed to "job/client1"
	var name string
	err = app.db.QueryRow("SELECT name FROM tags WHERE name = ?", "job/client1").Scan(&name)
	if err != nil {
		t.Error("expected 'job/client1' to exist after rename")
	}

	// Verify "working/stuff" was NOT renamed (prefix boundary check)
	err = app.db.QueryRow("SELECT name FROM tags WHERE name = ?", "working/stuff").Scan(&name)
	if err != nil {
		t.Error("'working/stuff' should still exist — it is not a descendant of 'work'")
	}

	// Verify "working" was NOT renamed
	err = app.db.QueryRow("SELECT name FROM tags WHERE name = ?", "working").Scan(&name)
	if err != nil {
		t.Error("'working' should still exist — it is not a descendant of 'work'")
	}
}

// ---------------------------------------------------------------------------
// deleteTagIfOrphaned respects children tests
// ---------------------------------------------------------------------------

func TestDeleteTagIfOrphanedKeepsParentWithChildren(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work/client1" (auto-creates "work")
	_, err := app.CreateTag("work/client1")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// Get the "work" tag's ID — it has 0 clips but has a child
	var workID int64
	err = app.db.QueryRow("SELECT id FROM tags WHERE name = ?", "work").Scan(&workID)
	if err != nil {
		t.Fatalf("failed to find 'work' tag: %v", err)
	}

	// Attempt to auto-delete — should NOT delete because it has children
	app.deleteTagIfOrphaned(workID)

	// Verify "work" still exists
	var name string
	err = app.db.QueryRow("SELECT name FROM tags WHERE id = ?", workID).Scan(&name)
	if err != nil {
		t.Error("'work' tag should NOT have been deleted — it has child tags")
	}
}

func TestDeleteTagIfOrphanedDeletesLeafWithNoClips(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create a leaf tag with no clips and no children
	leafID := createTestTag(t, app.db, "orphan-leaf", "#aaa")

	// Attempt to auto-delete — should delete because it's a leaf with no clips
	app.deleteTagIfOrphaned(leafID)

	// Verify "orphan-leaf" is gone
	var name string
	err := app.db.QueryRow("SELECT name FROM tags WHERE id = ?", leafID).Scan(&name)
	if err == nil {
		t.Error("'orphan-leaf' tag should have been deleted — it has no clips and no children")
	}
}

// ---------------------------------------------------------------------------
// GetClips filter expansion tests
// ---------------------------------------------------------------------------

func TestGetClipsExpandsTagFilterToDescendants(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work" and "work/client1"
	workID := createTestTag(t, app.db, "work", "#aaa")
	client1ID := createTestTag(t, app.db, "work/client1", "#bbb")

	// Create a clip tagged with "work/client1" only (not "work" directly)
	clipID, err := app.createTestClip("doc.txt", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clipID, client1ID)

	// GetClips with filter on "work" should expand to include "work/client1"
	clips, err := app.GetClips(false, []int64{workID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips: %v", err)
	}
	if len(clips) != 1 {
		t.Errorf("GetClips with expanded filter: got %d clips, want 1", len(clips))
	}
	if len(clips) == 1 && clips[0].ID != clipID {
		t.Errorf("GetClips returned clip ID %d, want %d", clips[0].ID, clipID)
	}
}

func TestGetClipsDirectDoesNotExpand(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work" and "work/client1"
	workID := createTestTag(t, app.db, "work", "#aaa")
	client1ID := createTestTag(t, app.db, "work/client1", "#bbb")

	// Create a clip tagged with "work/client1" only (not "work" directly)
	clipID, err := app.createTestClip("doc.txt", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clipID, client1ID)

	// GetClipsDirect with filter on "work" should NOT expand — clip is not directly tagged "work"
	clips, err := app.GetClipsDirect(false, []int64{workID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClipsDirect: %v", err)
	}
	if len(clips) != 0 {
		t.Errorf("GetClipsDirect without expansion: got %d clips, want 0", len(clips))
	}

	// But filtering directly by "work/client1" should return the clip
	clips, err = app.GetClipsDirect(false, []int64{client1ID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClipsDirect(client1): %v", err)
	}
	if len(clips) != 1 {
		t.Errorf("GetClipsDirect with direct tag: got %d clips, want 1", len(clips))
	}
}

func TestHiddenTagHidesDescendants(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create "work" and "work/client1"
	workID := createTestTag(t, app.db, "work", "#aaa")
	client1ID := createTestTag(t, app.db, "work/client1", "#bbb")

	// Create two clips: one tagged "work/client1", one untagged
	clipTagged, err := app.createTestClip("doc.txt", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clipTagged, client1ID)

	clipUntagged, err := app.createTestClip("other.txt", "text/plain", []byte("world"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}

	// Hide "work" — should also hide clips tagged with "work/client1"
	clips, err := app.GetClips(false, nil, []int64{workID}, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips: %v", err)
	}
	if len(clips) != 1 {
		t.Fatalf("GetClips with hidden work: got %d clips, want 1", len(clips))
	}
	if clips[0].ID != clipUntagged {
		t.Errorf("GetClips returned clip ID %d, want %d (untagged)", clips[0].ID, clipUntagged)
	}

	// GetClipsDirect should also expand hidden tags (hidden always expands)
	clips, err = app.GetClipsDirect(false, nil, []int64{workID}, "date", "desc")
	if err != nil {
		t.Fatalf("GetClipsDirect: %v", err)
	}
	if len(clips) != 1 {
		t.Fatalf("GetClipsDirect with hidden work: got %d clips, want 1", len(clips))
	}
	if clips[0].ID != clipUntagged {
		t.Errorf("GetClipsDirect returned clip ID %d, want %d (untagged)", clips[0].ID, clipUntagged)
	}
}

func TestGetClipsANDLogicWithExpansion(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create tag hierarchy: "work", "work/client1", "photos", "photos/vacation"
	workID := createTestTag(t, app.db, "work", "#aaa")
	client1ID := createTestTag(t, app.db, "work/client1", "#bbb")
	photosID := createTestTag(t, app.db, "photos", "#ccc")
	vacationID := createTestTag(t, app.db, "photos/vacation", "#ddd")

	// clip1: tagged with "work/client1" AND "photos/vacation" — matches both groups
	clip1, err := app.createTestClip("a.txt", "text/plain", []byte("both"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clip1, client1ID)
	tagClip(t, app.db, clip1, vacationID)

	// clip2: tagged with "work/client1" only — matches work group but not photos group
	clip2, err := app.createTestClip("b.txt", "text/plain", []byte("work only"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clip2, client1ID)
	_ = clip2

	// clip3: tagged with "photos/vacation" only — matches photos group but not work group
	clip3, err := app.createTestClip("c.txt", "text/plain", []byte("photos only"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tagClip(t, app.db, clip3, vacationID)
	_ = clip3

	// Filter by "work" AND "photos" — should only return clip1
	clips, err := app.GetClips(false, []int64{workID, photosID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips: %v", err)
	}
	if len(clips) != 1 {
		t.Fatalf("GetClips with AND logic: got %d clips, want 1", len(clips))
	}
	if clips[0].ID != clip1 {
		t.Errorf("GetClips returned clip ID %d, want %d", clips[0].ID, clip1)
	}

	// Filter by just "work" — should return clip1 and clip2
	clips, err = app.GetClips(false, []int64{workID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips(work only): %v", err)
	}
	if len(clips) != 2 {
		t.Errorf("GetClips with work filter: got %d clips, want 2", len(clips))
	}

	// Filter by just "photos" — should return clip1 and clip3
	clips, err = app.GetClips(false, []int64{photosID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips(photos only): %v", err)
	}
	if len(clips) != 2 {
		t.Errorf("GetClips with photos filter: got %d clips, want 2", len(clips))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func stringSliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
