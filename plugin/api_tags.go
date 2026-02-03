package plugin

import (
	"database/sql"
	"log"
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

// TagsAPI provides tag operations to plugins
type TagsAPI struct {
	db *sql.DB
}

// NewTagsAPI creates a new tags API instance
func NewTagsAPI(db *sql.DB) *TagsAPI {
	return &TagsAPI{db: db}
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

// create creates a new tag with auto-assigned color
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

	// Use transaction to prevent race condition in color assignment
	tx, err := t.db.Begin()
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer tx.Rollback()

	// Get count of existing tags to determine color
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	color := tagColors[count%len(tagColors)]

	result, err := tx.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
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

	id, _ := result.LastInsertId()

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

	if colorVal := opts.RawGetString("color"); colorVal != lua.LNil {
		color = colorVal.String()
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

	_, err := t.db.Exec("DELETE FROM tags WHERE id = ?", id)
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

	_, err := t.db.Exec("INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID)
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

	_, err := t.db.Exec("DELETE FROM clip_tags WHERE clip_id = ? AND tag_id = ?", clipID, tagID)
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
