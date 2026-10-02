package plugin

import (
	"database/sql"
	"testing"

	lua "github.com/yuin/gopher-lua"
	_ "modernc.org/sqlite"
)

// tags.update writes the color straight to the row the UI interpolates into
// markup and inline styles, so it accepts only what app.go's UpdateTag does.
func TestTagsUpdateRejectsUnsafeColor(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT UNIQUE, color TEXT);
		INSERT INTO tags (id, name, color) VALUES (1, 'work', 'not a color')`); err != nil {
		t.Fatal(err)
	}

	L := lua.NewState()
	defer L.Close()
	NewTagsAPI(db, TagHostFuncs{}).Register(L)

	color := func() string {
		var c string
		if err := db.QueryRow(`SELECT color FROM tags WHERE id = 1`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}

	if err := L.DoString(`ok, err = tags.update(1, {color = 'red" onmouseover="alert(1)'})`); err != nil {
		t.Fatal(err)
	}
	if L.GetGlobal("ok") != lua.LFalse || L.GetGlobal("err") == lua.LNil {
		t.Fatalf("tags.update accepted markup as a color (ok=%v)", L.GetGlobal("ok"))
	}
	if c := color(); c != "not a color" {
		t.Fatalf("rejected color was stored: %q", c)
	}

	// Renaming without a color works whatever is stored (the app resets
	// invalid stored colors when the database opens; see normalizeTagColors).
	if err := L.DoString(`ok, err = tags.update(1, {name = 'jobs'})`); err != nil {
		t.Fatal(err)
	}
	if L.GetGlobal("ok") != lua.LTrue {
		t.Fatalf("rename without a color failed: %v", L.GetGlobal("err"))
	}

	// An empty color keeps the stored one, as App.UpdateTag does.
	if err := L.DoString(`ok, err = tags.update(1, {name = 'jobs2', color = ''})`); err != nil {
		t.Fatal(err)
	}
	if L.GetGlobal("ok") != lua.LTrue || color() != "not a color" {
		t.Fatalf("empty color did not keep the stored one: ok=%v err=%v color=%q", L.GetGlobal("ok"), L.GetGlobal("err"), color())
	}

	if err := L.DoString(`ok, err = tags.update(1, {color = '#3B82F6'})`); err != nil {
		t.Fatal(err)
	}
	if L.GetGlobal("ok") != lua.LTrue || color() != "#3B82F6" {
		t.Fatalf("valid hex color not applied: ok=%v color=%q", L.GetGlobal("ok"), color())
	}
}
