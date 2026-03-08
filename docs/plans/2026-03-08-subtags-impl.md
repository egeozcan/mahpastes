# Subtags (Hierarchical Tags) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add hierarchical tag support using `/`-separated names with cascading filters, folder browsing mode, tree-structured dropdowns, subtag folders in tag serve, and expanded API key scoping.

**Architecture:** No schema changes — hierarchy derived from parsing `/` in tag names. Backend resolves descendants via `LIKE 'prefix/%'` queries. Frontend builds tree structures from the flat tag list. Folder mode is a new UI toggle that renders subtags as navigable folder cards.

**Tech Stack:** Go (backend), Vanilla JS (frontend), SQLite, Playwright (e2e), Tailwind CSS

---

### Task 1: Backend Tag Hierarchy Helpers

Add pure helper functions for resolving tag hierarchy from names. These are the foundation everything else builds on.

**Files:**
- Create: `tag_hierarchy.go`
- Create: `tag_hierarchy_test.go`

**Step 1: Write tests for hierarchy helpers**

Create `tag_hierarchy_test.go`:

```go
package main

import (
	"testing"
)

func TestGetTagDepth(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"work", 0},
		{"work/client1", 1},
		{"work/client1/projectABC", 2},
	}
	for _, tt := range tests {
		if got := getTagDepth(tt.name); got != tt.expected {
			t.Errorf("getTagDepth(%q) = %d, want %d", tt.name, got, tt.expected)
		}
	}
}

func TestGetParentTagName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"work", ""},
		{"work/client1", "work"},
		{"work/client1/projectABC", "work/client1"},
	}
	for _, tt := range tests {
		if got := getParentTagName(tt.name); got != tt.expected {
			t.Errorf("getParentTagName(%q) = %q, want %q", tt.name, got, tt.expected)
		}
	}
}

func TestGetShortTagName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"work", "work"},
		{"work/client1", "client1"},
		{"work/client1/projectABC", "projectABC"},
	}
	for _, tt := range tests {
		if got := getShortTagName(tt.name); got != tt.expected {
			t.Errorf("getShortTagName(%q) = %q, want %q", tt.name, got, tt.expected)
		}
	}
}

func TestGetAncestorTagNames(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
	}{
		{"work", nil},
		{"work/client1", []string{"work"}},
		{"work/client1/projectABC", []string{"work", "work/client1"}},
	}
	for _, tt := range tests {
		got := getAncestorTagNames(tt.name)
		if len(got) != len(tt.expected) {
			t.Errorf("getAncestorTagNames(%q) = %v, want %v", tt.name, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("getAncestorTagNames(%q)[%d] = %q, want %q", tt.name, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestIsDescendantOf(t *testing.T) {
	tests := []struct {
		child, parent string
		expected      bool
	}{
		{"work/client1", "work", true},
		{"work/client1/projectABC", "work", true},
		{"work/client1/projectABC", "work/client1", true},
		{"work", "work", false},
		{"work2/client1", "work", false},
		{"working/client1", "work", false},
	}
	for _, tt := range tests {
		if got := isDescendantOf(tt.child, tt.parent); got != tt.expected {
			t.Errorf("isDescendantOf(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.expected)
		}
	}
}

func TestIsImmediateChildOf(t *testing.T) {
	tests := []struct {
		child, parent string
		expected      bool
	}{
		{"work/client1", "work", true},
		{"work/client1/projectABC", "work", false},
		{"work/client1/projectABC", "work/client1", true},
		{"work", "", true},
		{"work/client1", "", false},
	}
	for _, tt := range tests {
		if got := isImmediateChildOf(tt.child, tt.parent); got != tt.expected {
			t.Errorf("isImmediateChildOf(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.expected)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestGetTagDepth -v`
Expected: FAIL — function not defined

**Step 3: Implement hierarchy helpers**

Create `tag_hierarchy.go`:

```go
package main

import "strings"

// getTagDepth returns the nesting depth of a tag (0 for top-level).
func getTagDepth(name string) int {
	return strings.Count(name, "/")
}

// getParentTagName returns the parent tag name, or "" if top-level.
func getParentTagName(name string) string {
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return ""
	}
	return name[:i]
}

// getShortTagName returns the leaf segment of a tag name.
func getShortTagName(name string) string {
	i := strings.LastIndex(name, "/")
	if i < 0 {
		return name
	}
	return name[i+1:]
}

// getAncestorTagNames returns all ancestor names from root to immediate parent.
// e.g., "a/b/c" → ["a", "a/b"]
func getAncestorTagNames(name string) []string {
	parts := strings.Split(name, "/")
	if len(parts) <= 1 {
		return nil
	}
	ancestors := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		ancestors = append(ancestors, strings.Join(parts[:i], "/"))
	}
	return ancestors
}

// isDescendantOf returns true if child is a descendant of parent.
// "work/client1" is a descendant of "work". "work" is NOT a descendant of "work".
func isDescendantOf(child, parent string) bool {
	return strings.HasPrefix(child, parent+"/")
}

// isImmediateChildOf returns true if child is a direct child of parent.
// parent="" means top-level check (no "/" in child).
func isImmediateChildOf(child, parent string) bool {
	if parent == "" {
		return !strings.Contains(child, "/")
	}
	if !strings.HasPrefix(child, parent+"/") {
		return false
	}
	rest := child[len(parent)+1:]
	return !strings.Contains(rest, "/")
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestGetTagDepth|TestGetParentTagName|TestGetShortTagName|TestGetAncestorTagNames|TestIsDescendantOf|TestIsImmediateChildOf" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add tag_hierarchy.go tag_hierarchy_test.go
git commit -m "feat: add tag hierarchy helper functions"
```

---

### Task 2: Backend — getDescendantTagIDs and getChildTagIDs

Add database-backed helpers for resolving tag descendants and immediate children. These are used by GetClips, hidden tags, serve, and API scope validation.

**Files:**
- Modify: `app.go` (add methods)
- Modify: `tag_hierarchy_test.go` (add integration tests)

**Step 1: Write tests**

Add to `tag_hierarchy_test.go` — these are integration tests requiring a real DB:

```go
func setupTestDBWithTags(t *testing.T) (*App, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	app := &App{}
	db, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	app.db = db
	cleanup := func() { db.Close() }
	return app, cleanup
}

func TestGetDescendantTagIDs(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	work, _ := app.CreateTag("work")
	client1, _ := app.CreateTag("work/client1")
	project, _ := app.CreateTag("work/client1/projectABC")
	app.CreateTag("personal")

	ids, err := app.getDescendantTagIDs(work.ID)
	if err != nil {
		t.Fatalf("getDescendantTagIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 descendants, got %d", len(ids))
	}
	// Should include client1 and project but not personal
	idSet := map[int64]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet[client1.ID] || !idSet[project.ID] {
		t.Errorf("expected descendants %d and %d, got %v", client1.ID, project.ID, ids)
	}
}

func TestGetChildTags(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	work, _ := app.CreateTag("work")
	app.CreateTag("work/client1")
	app.CreateTag("work/client2")
	app.CreateTag("work/client1/projectABC")
	app.CreateTag("personal")

	children, err := app.getChildTags(work.ID)
	if err != nil {
		t.Fatalf("getChildTags: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}
}
```

Add the `path/filepath` import at top of test file.

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestGetDescendantTagIDs|TestGetChildTags" -v`
Expected: FAIL — methods not defined

**Step 3: Implement in app.go**

Add these methods after the existing tag functions (after `deleteTagIfOrphaned` around line 1142):

```go
// getDescendantTagIDs returns all tag IDs that are descendants of the given tag.
func (a *App) getDescendantTagIDs(tagID int64) ([]int64, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query("SELECT id FROM tags WHERE name LIKE ?", parentName+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// getChildTags returns immediate child tags of the given tag.
func (a *App) getChildTags(tagID int64) ([]Tag, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE t.name LIKE ?
		GROUP BY t.id
		ORDER BY t.name`, parentName+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var children []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Count); err != nil {
			return nil, err
		}
		if isImmediateChildOf(tag.Name, parentName) {
			children = append(children, tag)
		}
	}
	return children, rows.Err()
}

// getTopLevelTags returns tags with no parent (no "/" in name).
func (a *App) getTopLevelTags() ([]Tag, error) {
	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE t.name NOT LIKE '%/%'
		GROUP BY t.id
		ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Count); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// getDescendantClipCount returns total clip count for a tag and all its descendants.
func (a *App) getDescendantClipCount(tagID int64) (int, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return 0, err
	}
	var count int
	err = a.db.QueryRow(`
		SELECT COUNT(DISTINCT ct.clip_id)
		FROM clip_tags ct
		JOIN tags t ON ct.tag_id = t.id
		WHERE t.name = ? OR t.name LIKE ?`, parentName, parentName+"/%").Scan(&count)
	return count, err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestGetDescendantTagIDs|TestGetChildTags" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy_test.go
git commit -m "feat: add descendant/child tag resolution methods"
```

---

### Task 3: Backend — CreateTag Auto-Creates Intermediates

Modify `CreateTag` to auto-create ancestor tags when a `/`-separated name is given.

**Files:**
- Modify: `app.go` (CreateTag method, ~line 959)
- Modify: `tag_hierarchy_test.go`

**Step 1: Write test**

Add to `tag_hierarchy_test.go`:

```go
func TestCreateTagAutoCreatesIntermediates(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Creating a deep tag should auto-create ancestors
	tag, err := app.CreateTag("work/client1/projectABC")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tag.Name != "work/client1/projectABC" {
		t.Errorf("expected name 'work/client1/projectABC', got %q", tag.Name)
	}

	// Ancestors should exist
	tags, err := app.GetTags()
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	names := map[string]bool{}
	for _, tg := range tags {
		names[tg.Name] = true
	}
	if !names["work"] {
		t.Error("ancestor 'work' was not auto-created")
	}
	if !names["work/client1"] {
		t.Error("ancestor 'work/client1' was not auto-created")
	}
}

func TestCreateTagIntermediatesInheritColor(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// Create a top-level tag first to establish a color
	work, _ := app.CreateTag("work")

	// Create a deep descendant — intermediates should inherit work's color
	app.CreateTag("work/client1/projectABC")

	tags, _ := app.GetTags()
	for _, tg := range tags {
		if tg.Name == "work/client1" && tg.Color != work.Color {
			t.Errorf("intermediate 'work/client1' got color %q, expected %q (inherited from 'work')", tg.Color, work.Color)
		}
	}
}

func TestCreateTagExistingIntermediatesNotDuplicated(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	app.CreateTag("work")
	app.CreateTag("work/client1/projectABC")

	tags, _ := app.GetTags()
	count := 0
	for _, tg := range tags {
		if tg.Name == "work" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 'work' tag, got %d", count)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestCreateTagAutoCreatesIntermediates|TestCreateTagIntermediatesInheritColor|TestCreateTagExistingIntermediatesNotDuplicated" -v`
Expected: FAIL — intermediates not auto-created

**Step 3: Modify CreateTag in app.go**

Replace the `CreateTag` method (around line 959-1014). The key change is: before creating the requested tag, iterate through `getAncestorTagNames()` and create any missing ancestors. Ancestors inherit color from the nearest existing ancestor, or get a new color.

In `app.go`, modify `CreateTag`:

```go
func (a *App) CreateTag(name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name cannot be empty")
	}
	if len(name) > 50 {
		return nil, fmt.Errorf("tag name cannot exceed 50 characters")
	}

	// Auto-create ancestor tags if needed
	ancestors := getAncestorTagNames(name)
	var inheritColor string
	for _, ancestorName := range ancestors {
		var existing int64
		err := a.db.QueryRow("SELECT id FROM tags WHERE name = ?", ancestorName).Scan(&existing)
		if err == nil {
			// Ancestor exists — grab its color for inheritance
			a.db.QueryRow("SELECT color FROM tags WHERE id = ?", existing).Scan(&inheritColor)
			continue
		}
		// Create the ancestor
		color := inheritColor
		if color == "" {
			var count int
			a.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
			color = tagColors[count%len(tagColors)]
		}
		res, err := a.db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", ancestorName, color)
		if err != nil {
			// Might be a race condition — try to read again
			a.db.QueryRow("SELECT color FROM tags WHERE name = ?", ancestorName).Scan(&inheritColor)
			continue
		}
		inheritColor = color
		id, _ := res.LastInsertId()
		runtime.EventsEmit(a.ctx, "tags-changed")
		if a.pluginManager != nil {
			a.pluginManager.EmitEvent("tag:created", map[string]interface{}{
				"id": id, "name": ancestorName, "color": color,
			})
		}
	}

	// Now create the actual tag
	color := inheritColor
	if color == "" {
		var count int
		a.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count)
		color = tagColors[count%len(tagColors)]
	}

	result, err := a.db.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
	if err != nil {
		return nil, fmt.Errorf("tag '%s' already exists", name)
	}

	id, _ := result.LastInsertId()
	tag := &Tag{ID: id, Name: name, Color: color}

	runtime.EventsEmit(a.ctx, "tags-changed")
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:created", map[string]interface{}{
			"id": id, "name": name, "color": color,
		})
	}

	return tag, nil
}
```

Note: The test `setupTestDBWithTags` creates an `App` without a `ctx` or `pluginManager`. Guard `runtime.EventsEmit` calls: only call when `a.ctx != nil`. Add a helper:

```go
func (a *App) emitEvent(event string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event)
	}
}

func (a *App) emitPluginEvent(name string, data map[string]interface{}) {
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent(name, data)
	}
}
```

Use `a.emitEvent("tags-changed")` and `a.emitPluginEvent(...)` throughout. Update the test's `setupTestDBWithTags` to NOT set `ctx`.

**Step 4: Run tests**

Run: `go test -run "TestCreateTag" -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy.go tag_hierarchy_test.go
git commit -m "feat: auto-create intermediate tags on subtag creation"
```

---

### Task 4: Backend — UpdateTag Cascade Rename

Modify `UpdateTag` to rename all descendant tags when a parent is renamed.

**Files:**
- Modify: `app.go` (UpdateTag method, ~line 1017)
- Modify: `tag_hierarchy_test.go`

**Step 1: Write test**

```go
func TestUpdateTagCascadesRename(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	app.CreateTag("work/client1/projectABC")

	tags, _ := app.GetTags()
	var workID int64
	for _, tg := range tags {
		if tg.Name == "work" {
			workID = tg.ID
			break
		}
	}

	err := app.UpdateTag(workID, "job", "")
	if err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}

	tags, _ = app.GetTags()
	names := map[string]bool{}
	for _, tg := range tags {
		names[tg.Name] = true
	}
	if !names["job"] {
		t.Error("'work' was not renamed to 'job'")
	}
	if !names["job/client1"] {
		t.Error("'work/client1' was not renamed to 'job/client1'")
	}
	if !names["job/client1/projectABC"] {
		t.Error("'work/client1/projectABC' was not renamed to 'job/client1/projectABC'")
	}
	if names["work"] || names["work/client1"] || names["work/client1/projectABC"] {
		t.Error("old names should not exist after rename")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestUpdateTagCascadesRename -v`
Expected: FAIL

**Step 3: Modify UpdateTag in app.go**

In the existing `UpdateTag` (around line 1017-1044), after renaming the tag itself, cascade rename descendants:

```go
func (a *App) UpdateTag(id int64, name string, color string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	if len(name) > 50 {
		return fmt.Errorf("tag name cannot exceed 50 characters")
	}

	// Get the old name for cascade rename
	var oldName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&oldName)
	if err != nil {
		return fmt.Errorf("tag not found")
	}

	// Update the tag itself
	if color != "" {
		_, err = a.db.Exec("UPDATE tags SET name = ?, color = ? WHERE id = ?", name, color, id)
	} else {
		_, err = a.db.Exec("UPDATE tags SET name = ? WHERE id = ?", name, id)
	}
	if err != nil {
		return fmt.Errorf("tag name '%s' already exists", name)
	}

	// Cascade rename descendants: replace old prefix with new prefix
	if oldName != name {
		oldPrefix := oldName + "/"
		newPrefix := name + "/"
		_, err = a.db.Exec(`UPDATE tags SET name = ? || SUBSTR(name, ?) WHERE name LIKE ?`,
			newPrefix, len(oldPrefix)+1, oldPrefix+"%")
		if err != nil {
			return fmt.Errorf("failed to rename descendant tags: %w", err)
		}
	}

	a.emitEvent("tags-changed")
	a.emitPluginEvent("tag:updated", map[string]interface{}{
		"id": id, "name": name, "color": color,
	})

	return nil
}
```

**Step 4: Run tests**

Run: `go test -run TestUpdateTag -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy_test.go
git commit -m "feat: cascade rename descendant tags on parent rename"
```

---

### Task 5: Backend — deleteTagIfOrphaned Respects Children

Modify `deleteTagIfOrphaned` to NOT delete a tag if it has child tags, even if it has 0 clips.

**Files:**
- Modify: `app.go` (~line 1130-1142)
- Modify: `tag_hierarchy_test.go`

**Step 1: Write test**

```go
func TestDeleteTagIfOrphanedKeepsParentWithChildren(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	app.CreateTag("work/client1")

	// "work" has 0 clips but has child "work/client1" — should NOT be deleted
	tags, _ := app.GetTags()
	var workID int64
	for _, tg := range tags {
		if tg.Name == "work" {
			workID = tg.ID
			break
		}
	}

	app.deleteTagIfOrphaned(workID)

	tags, _ = app.GetTags()
	found := false
	for _, tg := range tags {
		if tg.Name == "work" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'work' should not have been deleted — it has children")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestDeleteTagIfOrphanedKeepsParentWithChildren -v`
Expected: FAIL — tag gets deleted because current code only checks clip count

**Step 3: Modify deleteTagIfOrphaned in app.go**

Replace the existing function (~line 1130):

```go
func (a *App) deleteTagIfOrphaned(tagID int64) {
	var clipCount int
	a.db.QueryRow("SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?", tagID).Scan(&clipCount)
	if clipCount > 0 {
		return
	}

	// Check if this tag has children
	var tagName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName)
	if err != nil {
		return
	}
	var childCount int
	a.db.QueryRow("SELECT COUNT(*) FROM tags WHERE name LIKE ?", tagName+"/%").Scan(&childCount)
	if childCount > 0 {
		return
	}

	a.db.Exec("DELETE FROM tags WHERE id = ?", tagID)
	a.emitEvent("tags-changed")
	a.emitPluginEvent("tag:deleted", map[string]interface{}{"id": tagID})
}
```

**Step 4: Run tests**

Run: `go test -run TestDeleteTagIfOrphaned -v`
Expected: PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy_test.go
git commit -m "fix: don't auto-delete tags that have child tags"
```

---

### Task 6: Backend — GetClips Expands Tag Filters to Include Descendants

Modify `GetClips` to expand each active tag filter to include all descendant tag IDs.

**Files:**
- Modify: `app.go` (GetClips method, ~line 380)
- Modify: `tag_hierarchy_test.go`

**Step 1: Write test**

```go
func TestGetClipsExpandsTagFilterToDescendants(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	app.CreateTag("work/client1")

	// Create a clip and tag it with "work/client1"
	clipID, err := app.createTestClip("test.txt", "text/plain", []byte("hello"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	tags, _ := app.GetTags()
	var client1ID, workID int64
	for _, tg := range tags {
		if tg.Name == "work/client1" {
			client1ID = tg.ID
		}
		if tg.Name == "work" {
			workID = tg.ID
		}
	}
	app.AddTagToClip(clipID, client1ID)

	// Filtering by "work" should include clips tagged with "work/client1"
	clips, err := app.GetClips(false, []int64{workID}, nil, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips: %v", err)
	}
	if len(clips) != 1 {
		t.Errorf("expected 1 clip when filtering by parent tag, got %d", len(clips))
	}
}
```

Also add a helper `createTestClip` to the test file:

```go
func (a *App) createTestClip(filename, contentType string, data []byte) (int64, error) {
	result, err := a.db.Exec(
		"INSERT INTO clips (filename, content_type, data, size) VALUES (?, ?, ?, ?)",
		filename, contentType, data, len(data))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestGetClipsExpandsTagFilterToDescendants -v`
Expected: FAIL — 0 clips returned

**Step 3: Modify GetClips in app.go**

At the beginning of `GetClips` (around line 380), before building the SQL query, expand the tag filter IDs:

```go
// Expand tag filters to include descendants
if len(activeTagIDs) > 0 {
	expanded := make([]int64, 0, len(activeTagIDs))
	for _, tagID := range activeTagIDs {
		expanded = append(expanded, tagID)
		descendants, err := a.getDescendantTagIDs(tagID)
		if err == nil {
			expanded = append(expanded, descendants...)
		}
	}
	activeTagIDs = expanded
}
```

Insert this right after the function signature, before the existing query building logic. The rest of the function remains the same — it already handles a list of tag IDs with INNER JOINs.

**Important**: The current AND logic joins once per active tag filter. With expansion, we need to change this: instead of requiring a clip to match ALL expanded IDs, each original filter becomes an OR group. A clip must match at least one ID from each original filter's expansion.

Revised approach — expand each filter group separately:

```go
// Expand each tag filter to include its descendants
type tagFilterGroup struct {
	ids []int64
}
var filterGroups []tagFilterGroup
if len(activeTagIDs) > 0 {
	for _, tagID := range activeTagIDs {
		group := tagFilterGroup{ids: []int64{tagID}}
		descendants, err := a.getDescendantTagIDs(tagID)
		if err == nil {
			group.ids = append(group.ids, descendants...)
		}
		filterGroups = append(filterGroups, group)
	}
}
```

Then in the SQL building section, replace the INNER JOIN per tag with an INNER JOIN per group using `IN (?)` placeholders:

```go
for i, group := range filterGroups {
	alias := fmt.Sprintf("ct%d", i)
	placeholders := make([]string, len(group.ids))
	for j, id := range group.ids {
		placeholders[j] = "?"
		args = append(args, id)
	}
	query += fmt.Sprintf(" INNER JOIN clip_tags %s ON c.id = %s.clip_id AND %s.tag_id IN (%s)",
		alias, alias, alias, strings.Join(placeholders, ","))
}
```

This replaces the existing INNER JOIN logic for active tag filtering.

**Step 4: Run tests**

Run: `go test -run TestGetClips -v` and then run full test suite `go test ./... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy_test.go
git commit -m "feat: expand tag filters to include descendant tags"
```

---

### Task 7: Backend — Hidden Tags Resolves Descendants

Modify the hidden tag logic so hiding a parent tag also hides all its descendants.

**Files:**
- Modify: `app.go` (GetClips hidden tag handling, ~line 400-410)
- Modify: `tag_hierarchy_test.go`

**Step 1: Write test**

```go
func TestHiddenTagHidesDescendants(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	app.CreateTag("work/client1")

	tags, _ := app.GetTags()
	var workID, client1ID int64
	for _, tg := range tags {
		if tg.Name == "work" {
			workID = tg.ID
		}
		if tg.Name == "work/client1" {
			client1ID = tg.ID
		}
	}

	// Create two clips
	clip1, _ := app.createTestClip("a.txt", "text/plain", []byte("a"))
	clip2, _ := app.createTestClip("b.txt", "text/plain", []byte("b"))
	app.AddTagToClip(clip1, client1ID)
	// clip2 has no tags

	// Hide "work" — should also hide clips tagged with "work/client1"
	hiddenIDs := []int64{workID}
	clips, err := app.GetClips(false, nil, hiddenIDs, "date", "desc")
	if err != nil {
		t.Fatalf("GetClips: %v", err)
	}
	// Only clip2 should show (clip1 is in hidden subtree)
	if len(clips) != 1 {
		t.Errorf("expected 1 visible clip, got %d", len(clips))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestHiddenTagHidesDescendants -v`
Expected: FAIL — clip tagged with child shows despite parent being hidden

**Step 3: Modify GetClips hidden tag expansion**

In the hidden tag expansion section of `GetClips` (around line 400-410), expand hidden tag IDs to include descendants:

```go
// Expand hidden tags to include descendants
if len(hiddenTagIDs) > 0 {
	expandedHidden := make([]int64, 0, len(hiddenTagIDs))
	for _, tagID := range hiddenTagIDs {
		expandedHidden = append(expandedHidden, tagID)
		descendants, err := a.getDescendantTagIDs(tagID)
		if err == nil {
			expandedHidden = append(expandedHidden, descendants...)
		}
	}
	hiddenTagIDs = expandedHidden
}
```

Insert this before the existing hidden tag SQL exclusion logic.

**Step 4: Run tests**

Run: `go test -run TestHiddenTag -v`
Expected: PASS

**Step 5: Commit**

```bash
git add app.go tag_hierarchy_test.go
git commit -m "feat: hidden tags cascade to descendants"
```

---

### Task 8: Backend — Serve Manager Subtag Folders

Modify the serve manager to show subtag folders as directories in the HTTP listing.

**Files:**
- Modify: `serve_manager.go`
- Modify: `server_security_test.go`

**Step 1: Write test**

Add to `server_security_test.go`:

```go
func TestServeManagerSubtagDirectories(t *testing.T) {
	db := newServerTestDB(t)

	// Create tags: "work", "work/client1"
	db.Exec("INSERT INTO tags (id, name, color) VALUES (1, 'work', 'stone')")
	db.Exec("INSERT INTO tags (id, name, color) VALUES (2, 'work/client1', 'stone')")

	// Create clips: one tagged "work", one tagged "work/client1"
	db.Exec("INSERT INTO clips (id, filename, content_type, data, size) VALUES (1, 'readme.txt', 'text/plain', 'hello', 5)")
	db.Exec("INSERT INTO clips (id, filename, content_type, data, size) VALUES (2, 'report.pdf', 'application/pdf', 'data', 4)")
	db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (1, 1)")
	db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (2, 2)")

	mgr := NewServeManager(db, nil)

	// Root listing for tag "work" should show readme.txt + client1/ folder
	entries := mgr.buildDirectoryListing(1)

	hasFile := false
	hasFolder := false
	for _, e := range entries {
		if e.Name == "readme.txt" && e.Type == "file" {
			hasFile = true
		}
		if e.Name == "client1" && e.Type == "directory" {
			hasFolder = true
		}
	}
	if !hasFile {
		t.Error("expected readme.txt file in listing")
	}
	if !hasFolder {
		t.Error("expected client1/ directory in listing")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestServeManagerSubtagDirectories -v`
Expected: FAIL — method doesn't exist

**Step 3: Modify serve_manager.go**

Major changes needed:

1. Add a `Type` field to `directoryEntry`: `Type string \`json:"type"\``
2. Modify `makeHandler` to handle path segments for subtag navigation
3. Add `buildDirectoryListing` method that combines direct clips + immediate child tag folders
4. Update HTML directory listing to show folder icons for directories

Update `directoryEntry`:
```go
type directoryEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Type        string `json:"type"` // "file" or "directory"
}
```

Add `buildDirectoryListing`:
```go
func (sm *ServeManager) buildDirectoryListing(tagID int64) []directoryEntry {
	// Get direct files
	files := sm.buildFileList(tagID)
	entries := make([]directoryEntry, 0, len(files))
	for _, f := range files {
		entries = append(entries, directoryEntry{
			Name: f.filename, Size: f.size, ContentType: f.contentType, Type: "file",
		})
	}

	// Get immediate child tags as folders
	var tagName string
	sm.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName)

	rows, _ := sm.db.Query("SELECT t.id, t.name FROM tags t WHERE t.name LIKE ?", tagName+"/%")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var childID int64
			var childName string
			rows.Scan(&childID, &childName)
			if isImmediateChildOf(childName, tagName) {
				entries = append(entries, directoryEntry{
					Name: getShortTagName(childName),
					Type: "directory",
				})
			}
		}
	}
	return entries
}
```

Modify `makeHandler` to resolve path segments to subtag IDs:

```go
// In makeHandler, after extracting the path:
// Resolve path segments to a subtag
currentTagID := tagID
currentTagName := tagName
if requestPath != "" && requestPath != "index.html" {
	// Check if path starts with a subtag folder
	segments := strings.Split(requestPath, "/")
	for i, seg := range segments {
		if i == len(segments)-1 {
			// Last segment — could be a file
			break
		}
		// Look for child tag
		childTagName := currentTagName + "/" + seg
		var childID int64
		err := sm.db.QueryRow("SELECT id FROM tags WHERE name = ?", childTagName).Scan(&childID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		currentTagID = childID
		currentTagName = childTagName
	}
	// The last segment is the filename within the resolved tag
	requestPath = segments[len(segments)-1]
}
```

Update `serveDirectoryListing` to handle the `Type` field — show folder icon for directories, and make them clickable links with trailing `/`.

**Step 4: Run tests**

Run: `go test -run TestServeManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add serve_manager.go server_security_test.go
git commit -m "feat: serve subtags as directory folders in tag serve"
```

---

### Task 9: Backend — API Manager Scope Expansion for Subtags

Modify the API manager so tag-scoped keys can access the full subtree.

**Files:**
- Modify: `api_manager.go`
- Modify: `server_security_test.go`

**Step 1: Write tests**

Add to `server_security_test.go`:

```go
func TestScopedKeyCanAccessSubtags(t *testing.T) {
	db := newServerTestDB(t)

	db.Exec("INSERT INTO tags (id, name, color) VALUES (1, 'work', 'stone')")
	db.Exec("INSERT INTO tags (id, name, color) VALUES (2, 'work/client1', 'stone')")
	db.Exec("INSERT INTO clips (id, filename, content_type, data, size) VALUES (1, 'test.txt', 'text/plain', 'hello', 5)")
	db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (1, 2)") // clip tagged with work/client1

	mgr := &APIManager{db: db}
	keyCtx := &apiKeyContext{KeyID: 1, Role: "admin", ScopedTagID: 1} // scoped to "work"

	// enforceTagScope should allow access to clip in subtag
	req := httptest.NewRequest("GET", "/api/v1/clips/1", nil)
	ctx := context.WithValue(req.Context(), apiKeyContextKey, keyCtx)
	req = req.WithContext(ctx)

	err := mgr.enforceTagScope(req, 1)
	if err != nil {
		t.Errorf("scoped key should be able to access clip in subtag: %v", err)
	}
}

func TestScopedKeyCanCreateSubtag(t *testing.T) {
	db := newServerTestDB(t)

	db.Exec("INSERT INTO tags (id, name, color) VALUES (1, 'work', 'stone')")

	mgr := &APIManager{db: db}

	// Scoped key should be allowed to create "work/client1" but not "personal"
	allowed := mgr.isTagInScope("work/client1", "work")
	if !allowed {
		t.Error("work/client1 should be in scope of work")
	}
	notAllowed := mgr.isTagInScope("personal", "work")
	if notAllowed {
		t.Error("personal should NOT be in scope of work")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run "TestScopedKeyCanAccessSubtags|TestScopedKeyCanCreateSubtag" -v`

**Step 3: Modify api_manager.go**

1. **`enforceTagScope`** (~line 409-420): Change the clip_tags check from exact `tag_id = ?` to checking if any of the clip's tags are the scoped tag or a descendant:

```go
func (am *APIManager) enforceTagScope(r *http.Request, clipID int64) error {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID == 0 {
		return nil
	}
	// Get the scoped tag name
	var scopedName string
	am.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName)

	// Check if clip has the scoped tag or any descendant
	var count int
	am.db.QueryRow(`
		SELECT COUNT(*) FROM clip_tags ct
		JOIN tags t ON ct.tag_id = t.id
		WHERE ct.clip_id = ? AND (t.id = ? OR t.name LIKE ?)`,
		clipID, keyCtx.ScopedTagID, scopedName+"/%").Scan(&count)
	if count == 0 {
		return fmt.Errorf("clip not in scoped tag")
	}
	return nil
}
```

2. **`isTagInScope`** (new helper):
```go
func (am *APIManager) isTagInScope(tagName, scopeName string) bool {
	return tagName == scopeName || isDescendantOf(tagName, scopeName)
}
```

3. **`handleCreateTag`** (~line 871-896): Allow scoped keys to create subtags under their scope:
```go
// Instead of blanket-blocking, check if the new tag name is in scope
if keyCtx.ScopedTagID > 0 {
	var scopedName string
	am.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName)
	if !am.isTagInScope(body.Name, scopedName) {
		jsonError(w, http.StatusForbidden, "tag-scoped key can only create subtags under its scope")
		return
	}
}
```

4. **`handleListTags`** (~line 843-869): Return scoped tag AND all descendants:
```go
if keyCtx.ScopedTagID > 0 {
	// Return scoped tag + descendants
	var scopedName string
	am.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName)
	// Query tags where name = scopedName OR name LIKE scopedName/%
	// ... build filtered tag list
}
```

5. **`handleListClips`** (~line 470-478): Expand scoped tag filter to include descendants (same pattern as GetClips).

6. **`handleAddTagToClip`** (~line 969-978): Allow adding scoped tag or any descendant tag.

7. **Serve handlers**: Already check `keyCtx.ScopedTagID == body.TagID` — leave as-is since you serve at the parent tag level.

**Step 4: Run tests**

Run: `go test -run "TestScopedKey" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add api_manager.go server_security_test.go
git commit -m "feat: tag-scoped API keys can access full subtag subtree"
```

---

### Task 10: Backend — Plugin API Tags Update

Update the plugin tag API to support auto-creation of intermediates and pass through the updated behavior.

**Files:**
- Modify: `plugin/api_tags.go`

**Step 1: Verify plugin API delegates to App methods**

Read `plugin/api_tags.go` to confirm `create` calls `app.CreateTag(name)`. If it does, the auto-create behavior is inherited automatically. Same for update/delete.

**Step 2: Update if needed**

If the plugin API has its own tag creation logic (e.g., direct SQL), update it to use `app.CreateTag()`. If it already delegates, no changes needed — just verify with a manual test.

**Step 3: Commit if changes were made**

```bash
git add plugin/api_tags.go
git commit -m "fix: plugin tag API uses app.CreateTag for subtag support"
```

---

### Task 11: Frontend — Tag Hierarchy Utility Functions

Add JavaScript helpers for tag hierarchy, mirroring the Go helpers.

**Files:**
- Modify: `frontend/js/utils.js`

**Step 1: Add hierarchy utilities to utils.js**

Add at the end of `frontend/js/utils.js`:

```javascript
function getTagDepth(name) {
    return (name.match(/\//g) || []).length;
}

function getParentTagName(name) {
    const i = name.lastIndexOf('/');
    return i < 0 ? '' : name.substring(0, i);
}

function getShortTagName(name) {
    const i = name.lastIndexOf('/');
    return i < 0 ? name : name.substring(i + 1);
}

function isDescendantOf(child, parent) {
    return child.startsWith(parent + '/');
}

function isImmediateChildOf(child, parent) {
    if (parent === '') return !child.includes('/');
    if (!child.startsWith(parent + '/')) return false;
    return !child.substring(parent.length + 1).includes('/');
}

function buildTagTree(tags) {
    // Build a tree from flat tag list
    // Returns array of root nodes: { tag, children: [...] }
    const byName = {};
    for (const tag of tags) {
        byName[tag.name] = { tag, children: [] };
    }

    const roots = [];
    for (const tag of tags) {
        const parentName = getParentTagName(tag.name);
        if (parentName && byName[parentName]) {
            byName[parentName].children.push(byName[tag.name]);
        } else {
            roots.push(byName[tag.name]);
        }
    }
    return roots;
}
```

**Step 2: Commit**

```bash
git add frontend/js/utils.js
git commit -m "feat: add tag hierarchy utility functions to frontend"
```

---

### Task 12: Frontend — Tree-Structured Tag Filter Dropdown

Replace the flat checkbox list in the tag filter dropdown with an indented tree.

**Files:**
- Modify: `frontend/js/tags.js` (renderTagFilterDropdown, ~line 35-67)

**Step 1: Rewrite renderTagFilterDropdown**

Replace the function body to use `buildTagTree()` and render recursively with indentation:

```javascript
function renderTagFilterDropdown() {
    const tagFilterList = document.getElementById('tag-filter-list');
    const allTags = getAllTags();
    const activeTagFilters = getActiveTagFilters();
    const hiddenTags = getHiddenTags();
    tagFilterList.innerHTML = '';

    if (allTags.length === 0) {
        tagFilterList.innerHTML = '<div class="px-3 py-2 text-xs text-stone-400">No tags yet</div>';
        return;
    }

    const tree = buildTagTree(allTags);

    function renderNode(node, depth) {
        const { tag, children } = node;

        // Skip if parent is hidden (unless it's the hidden tag itself shown at top level)
        const parentName = getParentTagName(tag.name);
        if (parentName) {
            const parentTag = allTags.find(t => t.name === parentName);
            if (parentTag && hiddenTags.includes(parentTag.id)) return;
        }

        const isActive = activeTagFilters.includes(tag.id);
        const isHidden = hiddenTags.includes(tag.id);
        const hasChildren = children.length > 0;

        const item = document.createElement('label');
        item.className = `flex items-center gap-2 px-3 py-1.5 cursor-pointer hover:bg-stone-50 text-xs ${isHidden ? 'opacity-50' : ''}`;
        item.style.paddingLeft = `${12 + depth * 16}px`;
        item.setAttribute('data-testid', `tag-checkbox-${tag.name}`);

        item.innerHTML = `
            <input type="checkbox" class="rounded border-stone-300" ${isActive ? 'checked' : ''}>
            <span class="w-2 h-2 rounded-full flex-shrink-0" style="background-color: ${tag.color}"></span>
            <span class="truncate">${getShortTagName(tag.name)}</span>
            <span class="ml-auto text-stone-400 text-[10px]">${tag.count}</span>
            ${isHidden ? '<svg class="w-3 h-3 text-stone-400 flex-shrink-0" ...><!-- eye-slash --></svg>' : ''}
        `;

        item.querySelector('input').addEventListener('change', () => toggleTagFilter(tag.id));
        tagFilterList.appendChild(item);

        // Render children
        if (!isHidden) {
            for (const child of children) {
                renderNode(child, depth + 1);
            }
        }
    }

    for (const root of tree) {
        renderNode(root, 0);
    }
}
```

**Step 2: Test manually**

Run: `make dev` and verify the tag filter dropdown shows tree structure.

**Step 3: Commit**

```bash
git add frontend/js/tags.js
git commit -m "feat: tree-structured tag filter dropdown"
```

---

### Task 13: Frontend — Tree-Structured Tag Popover

Update the tag popover (for assigning tags to clips) to use the same tree structure.

**Files:**
- Modify: `frontend/js/tags.js` (renderTagPopoverList, ~line 243-288)

**Step 1: Rewrite renderTagPopoverList**

Same approach as the filter dropdown — use `buildTagTree()` and render recursively with indentation and checkboxes.

**Step 2: Test manually**

Run: `make dev`, click tag button on a clip card, verify tree structure.

**Step 3: Commit**

```bash
git add frontend/js/tags.js
git commit -m "feat: tree-structured tag assignment popover"
```

---

### Task 14: Frontend — Card Tag Pills Show Short Names

Modify card tag rendering to show only the leaf name with a full-path tooltip.

**Files:**
- Modify: `frontend/js/tags.js` (renderCardTags, ~line 397-455)

**Step 1: Modify renderCardTags**

In the tag pill rendering, change the text content to use `getShortTagName(tag.name)` and add a `title` attribute with the full name:

```javascript
// In the pill creation:
pill.textContent = getShortTagName(tag.name);
pill.title = tag.name; // tooltip shows full path
```

**Step 2: Test manually and commit**

```bash
git add frontend/js/tags.js
git commit -m "feat: card tag pills show short name with full-path tooltip"
```

---

### Task 15: Frontend — Folder Mode Button

Add the folder mode toggle button next to the sort button in the header.

**Files:**
- Modify: `frontend/index.html` (~line 76-85, near sort button)
- Modify: `frontend/js/app.js` (add folderMode state)

**Step 1: Add button HTML**

In `frontend/index.html`, add a folder mode button right after the sort button:

```html
<button id="folder-mode-btn" data-testid="folder-mode-button"
    class="p-1.5 rounded-md border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 transition-colors"
    aria-label="Toggle folder mode" aria-pressed="false">
    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
    </svg>
</button>
```

**Step 2: Add state and toggle handler in app.js**

```javascript
let folderMode = false;

function toggleFolderMode() {
    folderMode = !folderMode;
    const btn = document.getElementById('folder-mode-btn');
    btn.setAttribute('aria-pressed', folderMode);
    btn.classList.toggle('bg-stone-100', folderMode);
    btn.classList.toggle('border-stone-300', folderMode);
    loadClips();
}

// In init:
document.getElementById('folder-mode-btn').addEventListener('click', toggleFolderMode);
```

Expose `folderMode` state via getter: `function isFolderMode() { return folderMode; }`

**Step 3: Commit**

```bash
git add frontend/index.html frontend/js/app.js
git commit -m "feat: add folder mode toggle button"
```

---

### Task 16: Frontend — Folder Mode Rendering

When folder mode is active, render subtag folders as cards alongside clips.

**Files:**
- Modify: `frontend/js/ui.js` (renderClips or the main render function)
- Modify: `frontend/js/app.js` (loadClips to fetch folder data)
- Modify: `frontend/js/wails-api.js` (add GetChildTags, GetTopLevelTags, GetDescendantClipCount wrappers)

**Step 1: Add Wails API wrappers**

Add to `wails-api.js`:

```javascript
async function getChildTags(tagId) {
    return await window.go.main.App.GetChildTags(tagId);
}

async function getTopLevelTags() {
    return await window.go.main.App.GetTopLevelTags();
}

async function getDescendantClipCount(tagId) {
    return await window.go.main.App.GetDescendantClipCount(tagId);
}
```

**Step 2: Expose backend methods**

In `app.go`, make `getChildTags`, `getTopLevelTags`, and `getDescendantClipCount` exported (capitalize):

```go
func (a *App) GetChildTags(tagID int64) ([]Tag, error) { return a.getChildTags(tagID) }
func (a *App) GetTopLevelTags() ([]Tag, error) { return a.getTopLevelTags() }
func (a *App) GetDescendantClipCount(tagID int64) (int, error) { return a.getDescendantClipCount(tagID) }
```

Run `make bindings` to regenerate frontend bindings.

**Step 3: Modify loadClips in app.js**

When folder mode is on, after loading clips, also fetch folder data:

```javascript
async function loadClips() {
    // ... existing clip loading ...

    if (folderMode) {
        let folderTags;
        if (activeTagFilters.length > 0) {
            // Show children of the deepest active filter
            const currentTagId = activeTagFilters[activeTagFilters.length - 1];
            folderTags = await getChildTags(currentTagId);
        } else {
            folderTags = await getTopLevelTags();
        }
        // Get descendant counts for each folder
        for (const tag of folderTags) {
            tag.descendantCount = await getDescendantClipCount(tag.id);
        }
        renderFolderCards(folderTags);
    }
}
```

**Step 4: Add renderFolderCards to ui.js**

```javascript
function renderFolderCards(folderTags) {
    const gallery = document.getElementById('clip-gallery');
    // Insert folder cards before clip cards
    for (const tag of folderTags) {
        const card = document.createElement('li');
        card.className = 'bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col items-center justify-center p-4 cursor-pointer transition-all duration-150 hover:border-stone-300 hover:scale-[1.02]';
        card.setAttribute('data-testid', `folder-card-${getShortTagName(tag.name)}`);
        card.innerHTML = `
            <svg class="w-8 h-8 mb-2" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="${tag.color}">
                <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
            </svg>
            <span class="text-xs font-medium text-stone-700">${getShortTagName(tag.name)}</span>
            <span class="text-[10px] text-stone-400 mt-0.5">${tag.descendantCount} clips</span>
        `;
        card.addEventListener('click', () => navigateToFolder(tag.id));
        gallery.insertBefore(card, gallery.firstChild);
    }
}
```

**Step 5: Add navigateToFolder**

```javascript
function navigateToFolder(tagId) {
    // Add tag filter for this folder and reload
    if (!activeTagFilters.includes(tagId)) {
        activeTagFilters.push(tagId);
    }
    updateActiveTagsDisplay();
    loadClips();
}
```

**Step 6: Run `make dev` and test manually, then commit**

```bash
git add frontend/js/ui.js frontend/js/app.js frontend/js/wails-api.js app.go
make bindings
git add frontend/wailsjs/
git commit -m "feat: folder mode renders subtag folders as navigable cards"
```

---

### Task 17: Frontend — Breadcrumb Navigation in Folder Mode

Make the active tag pills act as breadcrumbs in folder mode, showing the full path and allowing navigation by removing segments.

**Files:**
- Modify: `frontend/js/tags.js` (updateActiveTagsDisplay, ~line 81-144)

**Step 1: Modify updateActiveTagsDisplay**

When folder mode is active and a tag filter with `/` is active, render each path segment as a separate clickable breadcrumb pill:

```javascript
function updateActiveTagsDisplay() {
    const container = document.getElementById('active-tags-container');
    const activeTagFilters = getActiveTagFilters();
    const allTags = getAllTags();

    if (activeTagFilters.length === 0) {
        container.classList.add('hidden');
        return;
    }
    container.classList.remove('hidden');
    container.innerHTML = '';

    if (isFolderMode()) {
        // Breadcrumb mode: show path segments
        // Find the current deepest tag
        const currentTag = allTags.find(t => t.id === activeTagFilters[activeTagFilters.length - 1]);
        if (currentTag) {
            const segments = currentTag.name.split('/');
            let path = '';
            for (let i = 0; i < segments.length; i++) {
                path = i === 0 ? segments[i] : path + '/' + segments[i];
                const segTag = allTags.find(t => t.name === path);
                if (!segTag) continue;

                const pill = document.createElement('span');
                pill.className = 'inline-flex items-center gap-1 bg-stone-100 text-stone-700 text-[11px] font-medium px-2 py-0.5 rounded-full';
                pill.setAttribute('data-testid', `tag-pill-${segTag.name}`);

                const nameSpan = document.createElement('span');
                nameSpan.textContent = segments[i];
                pill.appendChild(nameSpan);

                const removeBtn = document.createElement('button');
                removeBtn.className = 'text-stone-400 hover:text-stone-600';
                removeBtn.innerHTML = '&times;';
                removeBtn.addEventListener('click', () => {
                    // Navigate up: remove this tag and all deeper tags from filters
                    const newFilters = activeTagFilters.filter(id => {
                        const tag = allTags.find(t => t.id === id);
                        return tag && !isDescendantOf(tag.name, segTag.name) && tag.id !== segTag.id;
                    });
                    // If removing non-root, navigate to parent
                    const parentName = getParentTagName(segTag.name);
                    if (parentName) {
                        const parentTag = allTags.find(t => t.name === parentName);
                        if (parentTag && !newFilters.includes(parentTag.id)) {
                            newFilters.push(parentTag.id);
                        }
                    }
                    setActiveTagFilters(newFilters);
                    updateActiveTagsDisplay();
                    loadClips();
                });
                pill.appendChild(removeBtn);
                container.appendChild(pill);

                // Add separator between segments
                if (i < segments.length - 1) {
                    const sep = document.createElement('span');
                    sep.className = 'text-stone-300 text-xs mx-0.5';
                    sep.textContent = '/';
                    container.appendChild(sep);
                }
            }
        }
    } else {
        // Normal mode: existing pill rendering
        // ... keep existing code
    }

    // Add clear all button
    const clearBtn = document.createElement('button');
    clearBtn.id = 'clear-tag-filters';
    clearBtn.className = 'text-[10px] text-stone-400 hover:text-stone-600 ml-1';
    clearBtn.textContent = 'Clear all';
    clearBtn.addEventListener('click', clearAllTagFilters);
    container.appendChild(clearBtn);
}
```

**Step 2: Test manually and commit**

```bash
git add frontend/js/tags.js
git commit -m "feat: breadcrumb navigation for folder mode"
```

---

### Task 18: Frontend — Settings Hidden Tags (Top-Level Only)

Modify the settings modal to only show top-level tags as toggleable for hiding.

**Files:**
- Modify: `frontend/js/settings.js` (renderHiddenTagsSettings)

**Step 1: Filter tags in renderHiddenTagsSettings**

In the function that renders the hidden tag toggles, filter to only show tags where `!tag.name.includes('/')` OR tags whose parent doesn't exist (orphaned subtags):

```javascript
const topLevelTags = allTags.filter(tag => {
    if (!tag.name.includes('/')) return true;
    // Orphaned subtag — parent doesn't exist
    const parentName = getParentTagName(tag.name);
    return !allTags.some(t => t.name === parentName);
});
```

Use `topLevelTags` instead of `allTags` when rendering the toggle list.

**Step 2: Test manually and commit**

```bash
git add frontend/js/settings.js
git commit -m "feat: only show top-level tags in hidden tags settings"
```

---

### Task 19: Frontend — Folder Mode Clip Filtering

In folder mode, only show clips tagged directly with the current tag, not clips in subtags.

**Files:**
- Modify: `app.go` (add GetClipsDirect or a flag to GetClips)
- Modify: `frontend/js/app.js`

**Step 1: Add backend support**

Add a new method or parameter to `GetClips` that does NOT expand tag filters to descendants. This is needed for folder mode where we want only directly-tagged clips.

Option: Add `GetClipsDirect(archived bool, tagIDs []int64, hiddenTagIDs []int64, sort string, dir string)` that skips the descendant expansion. Or add an `expandTags bool` parameter to `GetClips`.

Simplest approach — add a separate method:

```go
func (a *App) GetClipsDirect(archived bool, activeTagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	// Same as GetClips but WITHOUT expanding activeTagIDs to descendants
	// Still expands hiddenTagIDs to descendants
	// ... copy the query logic but skip the expansion step for activeTagIDs
}
```

Alternatively, refactor `GetClips` to accept an `expandFilters bool` parameter. Since Wails binds methods, adding a parameter changes the API. Separate method is cleaner.

**Step 2: Update frontend**

In `loadClips()`, when folder mode is on:
```javascript
if (folderMode && activeTagFilters.length > 0) {
    clips = await getClipsDirect(archived, activeTagFilters, effectiveHidden, sortField, sortDir);
} else {
    clips = await getClips(archived, activeTagFilters, effectiveHidden, sortField, sortDir);
}
```

**Step 3: Add wails-api wrapper and bindings**

```javascript
async function getClipsDirect(archived, tagIds, hiddenTagIds, sort, dir) {
    return await window.go.main.App.GetClipsDirect(archived, tagIds, hiddenTagIds, sort, dir);
}
```

Run `make bindings`.

**Step 4: Commit**

```bash
git add app.go frontend/js/app.js frontend/js/wails-api.js
make bindings
git add frontend/wailsjs/
git commit -m "feat: folder mode shows only directly-tagged clips"
```

---

### Task 20: E2E Tests — Subtag CRUD

**Files:**
- Create: `e2e/tests/tags/subtag-crud.spec.ts`

**Step 1: Write tests**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Subtag CRUD', () => {
    test('creating a subtag auto-creates intermediate tags', async ({ app }) => {
        await app.createTag('work/client1/projectABC');
        const tags = await app.getAllTags();
        const names = tags.map(t => t.name);
        expect(names).toContain('work');
        expect(names).toContain('work/client1');
        expect(names).toContain('work/client1/projectABC');
    });

    test('intermediate tags inherit parent color', async ({ app }) => {
        await app.createTag('work');
        const workTag = (await app.getAllTags()).find(t => t.name === 'work');
        await app.createTag('work/client1');
        const client1Tag = (await app.getAllTags()).find(t => t.name === 'work/client1');
        expect(client1Tag.color).toBe(workTag.color);
    });

    test('renaming parent cascades to children', async ({ app }) => {
        await app.createTag('work/client1/projectABC');
        const workTag = (await app.getAllTags()).find(t => t.name === 'work');
        // Rename via backend
        await app.page.evaluate(([id]) => window.go.main.App.UpdateTag(id, 'job', ''), [workTag.id]);
        const tags = await app.getAllTags();
        const names = tags.map(t => t.name);
        expect(names).toContain('job');
        expect(names).toContain('job/client1');
        expect(names).toContain('job/client1/projectABC');
        expect(names).not.toContain('work');
    });

    test('deleting parent does not delete children', async ({ app }) => {
        await app.createTag('work/client1');
        const workTag = (await app.getAllTags()).find(t => t.name === 'work');
        await app.page.evaluate(([id]) => window.go.main.App.DeleteTag(id), [workTag.id]);
        const tags = await app.getAllTags();
        const names = tags.map(t => t.name);
        expect(names).toContain('work/client1');
        expect(names).not.toContain('work');
    });

    test('parent with children is not auto-deleted when it has 0 clips', async ({ app }) => {
        await app.createTag('work/client1');
        // "work" has 0 clips but has child — should not be auto-deleted
        const tags = await app.getAllTags();
        expect(tags.map(t => t.name)).toContain('work');
    });
});
```

**Step 2: Run tests**

Run: `cd e2e && npx playwright test tests/tags/subtag-crud.spec.ts`
Expected: all PASS

**Step 3: Commit**

```bash
git add e2e/tests/tags/subtag-crud.spec.ts
git commit -m "test: add subtag CRUD e2e tests"
```

---

### Task 21: E2E Tests — Subtag Filtering

**Files:**
- Create: `e2e/tests/tags/subtag-filter.spec.ts`

**Step 1: Write tests**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import { createTempFile, generateTestImage } from '../../helpers/test-data';

test.describe('Subtag Filtering', () => {
    test('filtering by parent tag shows clips tagged with subtags', async ({ app }) => {
        await app.createTag('work/client1');
        const image = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(image);
        // Tag clip with child tag
        const tags = await app.getAllTags();
        const client1 = tags.find(t => t.name === 'work/client1');
        const work = tags.find(t => t.name === 'work');
        await app.addTagToClip(path.basename(image), 'work/client1');
        // Filter by parent
        await app.filterByTag('work');
        await app.expectClipCount(1);
    });

    test('filter dropdown shows tree structure', async ({ app }) => {
        await app.createTag('work/client1');
        await app.createTag('work/client2');
        await app.openTagFilterDropdown();
        // Verify indentation (child items have more padding)
        // ... check DOM structure
    });

    test('hidden parent hides clips in subtags', async ({ app }) => {
        await app.createTag('work/client1');
        const image = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(image);
        await app.addTagToClip(path.basename(image), 'work/client1');
        await app.setHiddenTags(['work']);
        await app.refreshClips();
        await app.expectClipCount(0);
    });
});
```

**Step 2: Run and commit**

```bash
cd e2e && npx playwright test tests/tags/subtag-filter.spec.ts
git add e2e/tests/tags/subtag-filter.spec.ts
git commit -m "test: add subtag filtering e2e tests"
```

---

### Task 22: E2E Tests — Folder Mode

**Files:**
- Create: `e2e/tests/tags/folder-mode.spec.ts`

**Step 1: Write tests**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';

test.describe('Folder Mode', () => {
    test('folder mode button toggles state', async ({ app }) => {
        const btn = app.page.locator('[data-testid="folder-mode-button"]');
        await expect(btn).toHaveAttribute('aria-pressed', 'false');
        await btn.click();
        await expect(btn).toHaveAttribute('aria-pressed', 'true');
        await btn.click();
        await expect(btn).toHaveAttribute('aria-pressed', 'false');
    });

    test('folder mode shows tag folders', async ({ app }) => {
        await app.createTag('work/client1');
        const btn = app.page.locator('[data-testid="folder-mode-button"]');
        await btn.click();
        await expect(app.page.locator('[data-testid="folder-card-work"]')).toBeVisible();
    });

    test('clicking folder navigates into subtag', async ({ app }) => {
        await app.createTag('work/client1');
        const image = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(image);
        await app.addTagToClip(path.basename(image), 'work/client1');

        const btn = app.page.locator('[data-testid="folder-mode-button"]');
        await btn.click();

        // Click work folder
        await app.page.locator('[data-testid="folder-card-work"]').click();

        // Should now show client1 folder and any direct clips
        await expect(app.page.locator('[data-testid="folder-card-client1"]')).toBeVisible();
    });

    test('breadcrumb navigation works in folder mode', async ({ app }) => {
        await app.createTag('work/client1/projectABC');
        const btn = app.page.locator('[data-testid="folder-mode-button"]');
        await btn.click();

        // Navigate deep
        await app.page.locator('[data-testid="folder-card-work"]').click();
        await app.page.locator('[data-testid="folder-card-client1"]').click();

        // Should see breadcrumb: work / client1
        await expect(app.page.locator('[data-testid="tag-pill-work"]')).toBeVisible();
        await expect(app.page.locator('[data-testid="tag-pill-work/client1"]')).toBeVisible();
    });
});
```

**Step 2: Run and commit**

```bash
cd e2e && npx playwright test tests/tags/folder-mode.spec.ts
git add e2e/tests/tags/folder-mode.spec.ts
git commit -m "test: add folder mode e2e tests"
```

---

### Task 23: E2E Test Fixtures — Add Subtag Helpers

Add helper methods to AppHelper for subtag operations.

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add selectors**

Add to `selectors.ts` in the `tags` section:

```typescript
folderModeButton: '[data-testid="folder-mode-button"]',
folderCard: (name: string) => `[data-testid="folder-card-${name}"]`,
```

**Step 2: Add fixture methods**

Add to `test-fixtures.ts`:

```typescript
async toggleFolderMode() {
    await this.page.locator(selectors.tags.folderModeButton).click();
}

async expectFolderVisible(name: string) {
    await expect(this.page.locator(selectors.tags.folderCard(name))).toBeVisible();
}

async clickFolder(name: string) {
    await this.page.locator(selectors.tags.folderCard(name)).click();
}
```

**Step 3: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts e2e/helpers/selectors.ts
git commit -m "test: add subtag/folder mode helpers to test fixtures"
```

---

### Task 24: Documentation Updates

Update all affected documentation pages.

**Files:**
- Modify: `docs/docs/features/tags.md`
- Modify: `docs/docs/features/tag-serve.md`
- Modify: `docs/docs/features/rest-api.md`
- Modify: `docs/docs/developers/database-schema.md`
- Modify: `docs/docs/developers/api-reference.md`
- Modify: `docs/docs/plugins/api-reference.md`

**Step 1: Update tags.md**

Add a "Subtags (Hierarchical Tags)" section covering:
- Creating subtags with `/` separator
- Auto-creation of intermediates
- Cascading filters
- Folder mode
- Tree-structured dropdowns
- Hidden tag inheritance

**Step 2: Update tag-serve.md**

Add section on subtag folder navigation in served URLs.

**Step 3: Update rest-api.md**

Document tag-scoped key subtag access:
- Scoped keys can access clips in subtags
- Scoped keys can create subtags under their scope
- List tags returns scoped subtree

**Step 4: Update database-schema.md**

Add note about hierarchy-from-names convention.

**Step 5: Update api-reference.md**

Document new methods: `GetChildTags`, `GetTopLevelTags`, `GetDescendantClipCount`, `GetClipsDirect`.

**Step 6: Update plugins/api-reference.md**

Document that `tags.create` now auto-creates intermediates.

**Step 7: Commit**

```bash
git add docs/docs/
git commit -m "docs: update documentation for subtags feature"
```

---

### Task 25: Run Full Test Suite

**Step 1: Run Go tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 2: Run e2e tests**

Run: `cd e2e && npm test`
Expected: all PASS

**Step 3: Fix any failures**

If any existing tests break due to the subtag changes (e.g., tag count assertions, hidden tag tests), update them.

**Step 4: Final commit**

```bash
git add -A
git commit -m "fix: update tests for subtag compatibility"
```

---

### Task 26: Regenerate Tailwind and Final Verification

**Step 1: Rebuild Tailwind CSS**

Run: `cd frontend && npx @tailwindcss/cli -i css/main.css -o dist/output.css`

**Step 2: Full build test**

Run: `make build`
Expected: clean build

**Step 3: Manual smoke test**

Run: `make dev` and verify:
- Create subtag `work/client1/projectABC` — intermediates auto-created
- Filter by `work` — shows clips in subtags
- Folder mode — shows folder cards, navigation works
- Tag pills show short names with tooltips
- Hidden parent hides subtag clips
- Settings only shows top-level tags for hiding

**Step 4: Final commit if any fixes needed**
