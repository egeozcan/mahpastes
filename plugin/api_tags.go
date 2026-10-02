package plugin

import (
	"database/sql"
	"log"
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Tag color palette - MUST stay in sync with app.go:tagColors
// Both copies exist because they're in different packages and this is a simple slice
var tagColors = []string{
	"#78716C", // stone
	"#EF4444", // red
	"#F59E0B", // amber
	"#22C55E", // green
	"#3B82F6", // blue
	"#8B5CF6", // violet
	"#EC4899", // pink
	"#06B6D4", // cyan
}

const maxTagNameLength = 50

// tagColorPattern - MUST stay in sync with app.go:tagColorPattern. Tag colors
// are interpolated into the UI's markup and inline styles, so tags.update only
// accepts a hex color or a bare CSS keyword.
var tagColorPattern = regexp.MustCompile(`^(#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})|[a-zA-Z]{1,30})$`)

// TagCreateResult holds the fields returned by the tag-creation callback.
// This mirrors the main.Tag struct without importing the main package.
type TagCreateResult struct {
	ID    int64
	Name  string
	Color string
}

// TagCreateFunc creates a tag by name and returns its details.
// It delegates to App.CreateTag so that subtag auto-creation works.
type TagCreateFunc func(name string) (*TagCreateResult, error)

// TagClipFunc adds a tag to, or removes it from, one clip through the host app.
// The host takes (clipID, tagID) — the reverse of the Lua argument order.
type TagClipFunc func(clipID, tagID int64) error

// TagDeleteFunc deletes a tag through the host app.
type TagDeleteFunc func(tagID int64) error

// TagUpdateFunc renames and/or recolors a tag through the host app. An empty
// name or color keeps the stored one.
type TagUpdateFunc func(tagID int64, name, color string) error

// TagHostFuncs routes the tags API's mutations through the host app, so a
// plugin's tag change has the same side effects as the user's:
//   - Create: subtag auto-creation (App.CreateTag).
//   - AddToClip: tree exclusivity, publication to a share on the tag, and the
//     restore exclusion lock (App.AddTagToClip). Without it a clip a plugin
//     tags into a shared folder never reaches followers.
//   - RemoveFromClip: orphan cleanup and the tag:removed_from_clip event
//     (App.RemoveTagFromClip).
//   - Delete: StopShare/StopServing and the follow precondition
//     (App.DeleteTag). A bare DELETE cascades a share's row away while its
//     publication stays live in memory.
//   - Update: name validation (the reserved _api segment), the served-subtree
//     guard, the descendant cascade and the tag:updated events
//     (App.UpdateTag). A bare UPDATE renamed a/b to x and left a/b/c behind.
//
// A nil field falls back to direct SQL on the plugin's handle with none of
// those side effects — the path for an API built without a host app.
type TagHostFuncs struct {
	Create         TagCreateFunc
	AddToClip      TagClipFunc
	RemoveFromClip TagClipFunc
	Delete         TagDeleteFunc
	Update         TagUpdateFunc
}

// TagsAPI provides tag operations to plugins
type TagsAPI struct {
	db   *sql.DB
	host TagHostFuncs
}

// NewTagsAPI creates a new tags API instance. Each nil field of host uses the
// legacy SQL path for that operation.
func NewTagsAPI(db *sql.DB, host TagHostFuncs) *TagsAPI {
	return &TagsAPI{db: db, host: host}
}

// Register adds the tags module to the Lua state
func (t *TagsAPI) Register(L *lua.LState) {
	tagsMod := L.NewTable()

	tagsMod.RawSetString("list", L.NewFunction(t.list))
	tagsMod.RawSetString("get", L.NewFunction(t.get))
	tagsMod.RawSetString("create", L.NewFunction(t.create))
	tagsMod.RawSetString("update", L.NewFunction(t.update))
	tagsMod.RawSetString("delete", L.NewFunction(t.deleteTag))
	tagsMod.RawSetString("add_to_clip", L.NewFunction(t.addToClip))
	tagsMod.RawSetString("remove_from_clip", L.NewFunction(t.removeFromClip))
	tagsMod.RawSetString("get_for_clip", L.NewFunction(t.getForClip))

	L.SetGlobal("tags", tagsMod)
}

// list returns all tags with usage counts
func (t *TagsAPI) list(L *lua.LState) int {
	rows, err := t.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		GROUP BY t.id
		ORDER BY t.name
	`)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer rows.Close()

	result := L.NewTable()
	for rows.Next() {
		var id int64
		var name, color string
		var count int

		if err := rows.Scan(&id, &name, &color, &count); err != nil {
			log.Printf("tags.list: failed to scan row: %v", err)
			continue
		}

		tag := L.NewTable()
		tag.RawSetString("id", lua.LNumber(id))
		tag.RawSetString("name", lua.LString(name))
		tag.RawSetString("color", lua.LString(color))
		tag.RawSetString("count", lua.LNumber(count))

		result.Append(tag)
	}

	L.Push(result)
	return 1
}

// get returns a single tag by ID
func (t *TagsAPI) get(L *lua.LState) int {
	id := L.CheckInt64(1)

	var name, color string
	var count int

	err := t.db.QueryRow(`
		SELECT t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE t.id = ?
		GROUP BY t.id
	`, id).Scan(&name, &color, &count)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		return 1
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tag := L.NewTable()
	tag.RawSetString("id", lua.LNumber(id))
	tag.RawSetString("name", lua.LString(name))
	tag.RawSetString("color", lua.LString(color))
	tag.RawSetString("count", lua.LNumber(count))

	L.Push(tag)
	return 1
}

// create creates a new tag with auto-assigned color.
// When a TagCreateFunc is available it delegates to App.CreateTag so that
// subtag auto-creation (ancestor tags) works correctly.
func (t *TagsAPI) create(L *lua.LState) int {
	name := strings.TrimSpace(L.CheckString(1))

	if name == "" {
		L.Push(lua.LNil)
		L.Push(lua.LString("tag name cannot be empty"))
		return 2
	}
	if len(name) > maxTagNameLength {
		L.Push(lua.LNil)
		L.Push(lua.LString("tag name too long"))
		return 2
	}

	// Delegate to App.CreateTag when available (handles subtag auto-creation)
	if t.host.Create != nil {
		result, err := t.host.Create(name)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		tag := L.NewTable()
		tag.RawSetString("id", lua.LNumber(result.ID))
		tag.RawSetString("name", lua.LString(result.Name))
		tag.RawSetString("color", lua.LString(result.Color))
		tag.RawSetString("count", lua.LNumber(0))

		L.Push(tag)
		return 1
	}

	// Legacy fallback: direct SQL (no subtag auto-creation)
	tx, err := t.db.Begin()
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	color := tagColors[count%len(tagColors)]

	dbResult, err := tx.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			L.Push(lua.LNil)
			L.Push(lua.LString("tag already exists: " + name))
			return 2
		}
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	id, _ := dbResult.LastInsertId()

	if err := tx.Commit(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	tag := L.NewTable()
	tag.RawSetString("id", lua.LNumber(id))
	tag.RawSetString("name", lua.LString(name))
	tag.RawSetString("color", lua.LString(color))
	tag.RawSetString("count", lua.LNumber(0))

	L.Push(tag)
	return 1
}

// update updates a tag's name and/or color
func (t *TagsAPI) update(L *lua.LState) int {
	id := L.CheckInt64(1)
	opts := L.CheckTable(2)

	if t.host.Update != nil {
		var name, color string
		if v := opts.RawGetString("name"); v != lua.LNil {
			name = strings.TrimSpace(v.String())
			if name == "" {
				L.Push(lua.LFalse)
				L.Push(lua.LString("tag name cannot be empty"))
				return 2
			}
		}
		if v := opts.RawGetString("color"); v != lua.LNil {
			color = v.String()
		}
		if err := t.host.Update(id, name, color); err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LTrue)
		return 1
	}

	// Legacy fallback: direct SQL (no cascade, validation or events)

	// Get current values
	var currentName, currentColor string
	err := t.db.QueryRow("SELECT name, color FROM tags WHERE id = ?", id).Scan(&currentName, &currentColor)
	if err == sql.ErrNoRows {
		L.Push(lua.LFalse)
		L.Push(lua.LString("tag not found"))
		return 2
	}
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Apply updates
	name := currentName
	color := currentColor

	if nameVal := opts.RawGetString("name"); nameVal != lua.LNil {
		name = strings.TrimSpace(nameVal.String())
		if name == "" {
			L.Push(lua.LFalse)
			L.Push(lua.LString("tag name cannot be empty"))
			return 2
		}
		if len(name) > maxTagNameLength {
			L.Push(lua.LFalse)
			L.Push(lua.LString("tag name too long"))
			return 2
		}
	}

	// An empty color keeps the stored one, as App.UpdateTag does.
	if colorVal := opts.RawGetString("color"); colorVal != lua.LNil && colorVal.String() != "" {
		color = colorVal.String()
		if !tagColorPattern.MatchString(color) {
			L.Push(lua.LFalse)
			L.Push(lua.LString("invalid tag color: use a hex color such as #3B82F6"))
			return 2
		}
	}

	_, err = t.db.Exec("UPDATE tags SET name = ?, color = ? WHERE id = ?", name, color, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			L.Push(lua.LFalse)
			L.Push(lua.LString("tag name already exists: " + name))
			return 2
		}
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// deleteTag deletes a tag
func (t *TagsAPI) deleteTag(L *lua.LState) int {
	id := L.CheckInt64(1)

	var err error
	if t.host.Delete != nil {
		err = t.host.Delete(id)
	} else {
		_, err = t.db.Exec("DELETE FROM tags WHERE id = ?", id)
	}
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// addToClip adds a tag to a clip
func (t *TagsAPI) addToClip(L *lua.LState) int {
	tagID := L.CheckInt64(1)
	clipID := L.CheckInt64(2)

	// Validate tag exists
	var tagExists int
	if err := t.db.QueryRow("SELECT 1 FROM tags WHERE id = ?", tagID).Scan(&tagExists); err == sql.ErrNoRows {
		L.Push(lua.LFalse)
		L.Push(lua.LString("tag not found"))
		return 2
	}

	// Validate clip exists
	var clipExists int
	if err := t.db.QueryRow("SELECT 1 FROM clips WHERE id = ?", clipID).Scan(&clipExists); err == sql.ErrNoRows {
		L.Push(lua.LFalse)
		L.Push(lua.LString("clip not found"))
		return 2
	}

	var err error
	if t.host.AddToClip != nil {
		err = t.host.AddToClip(clipID, tagID)
	} else {
		_, err = t.db.Exec("INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID)
	}
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// removeFromClip removes a tag from a clip
func (t *TagsAPI) removeFromClip(L *lua.LState) int {
	tagID := L.CheckInt64(1)
	clipID := L.CheckInt64(2)

	// Validate tag exists
	var tagExists int
	if err := t.db.QueryRow("SELECT 1 FROM tags WHERE id = ?", tagID).Scan(&tagExists); err == sql.ErrNoRows {
		L.Push(lua.LFalse)
		L.Push(lua.LString("tag not found"))
		return 2
	}

	// Validate clip exists
	var clipExists int
	if err := t.db.QueryRow("SELECT 1 FROM clips WHERE id = ?", clipID).Scan(&clipExists); err == sql.ErrNoRows {
		L.Push(lua.LFalse)
		L.Push(lua.LString("clip not found"))
		return 2
	}

	var err error
	if t.host.RemoveFromClip != nil {
		err = t.host.RemoveFromClip(clipID, tagID)
	} else {
		_, err = t.db.Exec("DELETE FROM clip_tags WHERE clip_id = ? AND tag_id = ?", clipID, tagID)
	}
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// getForClip returns all tags for a specific clip
func (t *TagsAPI) getForClip(L *lua.LState) int {
	clipID := L.CheckInt64(1)

	rows, err := t.db.Query(`
		SELECT t.id, t.name, t.color
		FROM tags t
		INNER JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE ct.clip_id = ?
		ORDER BY t.name
	`, clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer rows.Close()

	result := L.NewTable()
	for rows.Next() {
		var id int64
		var name, color string

		if err := rows.Scan(&id, &name, &color); err != nil {
			log.Printf("tags.get_for_clip: failed to scan row: %v", err)
			continue
		}

		tag := L.NewTable()
		tag.RawSetString("id", lua.LNumber(id))
		tag.RawSetString("name", lua.LString(name))
		tag.RawSetString("color", lua.LString(color))
		tag.RawSetString("count", lua.LNumber(0)) // Not calculated for clip tags

		result.Append(tag)
	}

	L.Push(result)
	return 1
}
