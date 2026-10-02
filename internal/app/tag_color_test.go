package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func tagRow(t *testing.T, app *App, id int64) (name, color string) {
	t.Helper()
	if err := app.db.QueryRow(`SELECT name, color FROM tags WHERE id = ?`, id).Scan(&name, &color); err != nil {
		t.Fatal(err)
	}
	return name, color
}

// Every view that shows a tag interpolates its color into markup or an inline
// style, and UpdateTag takes the color as free text from the REST API. Only
// values that cannot leave that context are accepted.
func TestUpdateTagValidatesColor(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111')`); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{
		`red"><img src=x onerror=alert(1)>`,
		`red; background-image: url(https://example.com/x)`,
		`#12345`,
		`rgb(1,2,3)`,
	} {
		if err := app.UpdateTag(1, "work", bad); err == nil {
			t.Fatalf("UpdateTag accepted color %q", bad)
		}
		if _, color := tagRow(t, app, 1); color != "#111111" {
			t.Fatalf("rejected color %q was stored as %q", bad, color)
		}
	}

	for _, good := range []string{"#3B82F6", "#abc", "#abcd", "#00ff00cc", "red", "RebeccaPurple"} {
		if err := app.UpdateTag(1, "work", good); err != nil {
			t.Fatalf("UpdateTag rejected color %q: %v", good, err)
		}
		if _, color := tagRow(t, app, 1); color != good {
			t.Fatalf("color = %q, want %q", color, good)
		}
	}
}

// `mp tag update --name` sends no color, and that used to blank it.
func TestUpdateTagEmptyColorKeepsCurrent(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111')`); err != nil {
		t.Fatal(err)
	}
	if err := app.UpdateTag(1, "jobs", ""); err != nil {
		t.Fatal(err)
	}
	if name, color := tagRow(t, app, 1); name != "jobs" || color != "#111111" {
		t.Fatalf("after rename with no color: %q %q, want jobs #111111", name, color)
	}
}

// Renames from the folder views send no color, so they keep whatever is
// stored rather than writing back a cached copy (which reverted a color
// changed elsewhere in the meantime, and failed on one that is not valid).
func TestUpdateTagEmptyNameKeepsName(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111')`); err != nil {
		t.Fatal(err)
	}
	if err := app.UpdateTag(1, "", "#00ff00"); err != nil {
		t.Fatalf("color-only UpdateTag: %v", err)
	}
	if name, color := tagRow(t, app, 1); name != "work" || color != "#00ff00" {
		t.Fatalf("after a color-only update: %q %q, want work #00ff00", name, color)
	}
	if err := app.UpdateTag(1, "  ", ""); err != nil {
		t.Fatalf("no-op UpdateTag: %v", err)
	}
	if name, color := tagRow(t, app, 1); name != "work" || color != "#00ff00" {
		t.Fatalf("after a no-op update: %q %q", name, color)
	}
}

// Colors written before validation existed, or restored from a backup, are
// reset to the default when the database opens — so no view, export or new
// subtag ever starts from one.
func TestInitDBNormalizesStoredTagColors(t *testing.T) {
	db, _ := newProductionSchemaDB(t)
	mustExecSQL(t, db, `INSERT INTO tags (id, name, color) VALUES
		(1, 'bad', 'red" onmouseover="alert(1)'), (2, 'blank', ''), (3, 'ok', '#3B82F6'), (4, 'word', 'teal')`)
	db.Close()

	// The next launch opens the same database.
	reopened, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer reopened.Close()
	app := &App{db: reopened}
	for id, want := range map[int64]string{1: tagColors[0], 2: tagColors[0], 3: "#3B82F6", 4: "teal"} {
		if _, color := tagRow(t, app, id); color != want {
			t.Errorf("tag %d color = %q, want %q", id, color, want)
		}
	}
}

// A backup carries tags.color as written; the restore applies the same reset.
func TestRestoreBackupNormalizesTagColors(t *testing.T) {
	restoreDataDir(t)
	srcDB := newBackupTestDB(t)
	good := filepath.Join(t.TempDir(), "good.zip")
	if err := (&App{db: srcDB}).CreateBackup(good); err != nil {
		t.Fatal(err)
	}
	crafted := rewriteBackupSQL(t, good,
		"INSERT INTO tags (id, name, color) VALUES (1, 'planted', 'red\" onmouseover=\"alert(1)');\n")
	dst := newBackupTestDB(t)
	if err := (&App{db: dst}).RestoreBackup(crafted, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if _, color := tagRow(t, &App{db: dst}, 1); color != tagColors[0] {
		t.Fatalf("restored tag color = %q, want the default %q", color, tagColors[0])
	}
}

// A new subtag inherits its nearest ancestor's color; one that is not valid
// is not passed on.
func TestCreateTagDoesNotInheritAnInvalidColor(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	if _, err := app.db.Exec(`INSERT INTO tags (name, color) VALUES ('parent', 'red" x="1')`); err != nil {
		t.Fatal(err)
	}
	child, err := app.CreateTag("parent/child")
	if err != nil {
		t.Fatal(err)
	}
	if !isValidTagColor(child.Color) {
		t.Fatalf("subtag inherited color %q", child.Color)
	}
}

// `mp tag update <tag> --color X` sends no name; the handler used to pass the
// empty name through and fail with "tag name cannot be empty".
func TestHandleUpdateTagColorOnlyKeepsName(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	manager := NewAPIManager(app)
	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111'), (2, 'work/client1', '#222222')`); err != nil {
		t.Fatal(err)
	}

	put := func(kc *apiKeyContext, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tags/2", strings.NewReader(body))
		req.SetPathValue("id", "2")
		rec := httptest.NewRecorder()
		manager.handleUpdateTag(rec, withKey(req, kc))
		return rec
	}

	// A key scoped to "work" may recolor its own subtag without restating the name.
	if rec := put(&apiKeyContext{KeyID: 1, Role: "admin", ScopedTagID: 1}, `{"color":"#00ff00"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("color-only update: %d %q", rec.Code, rec.Body.String())
	}
	if name, color := tagRow(t, app, 2); name != "work/client1" || color != "#00ff00" {
		t.Fatalf("after color-only update: %q %q", name, color)
	}

	if rec := put(&apiKeyContext{KeyID: 1, Role: "editor"}, `{"color":"x\" onmouseover=\"alert(1)"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("markup in color: %d %q, want 400", rec.Code, rec.Body.String())
	}
}

// tagUpdatedCounterSrc counts its on_tag_updated deliveries in storage.
const tagUpdatedCounterSrc = `Plugin = { name = "Updated Counter", version = "1.0.0",
  events = {"tag:updated"} }

function on_tag_updated(data)
  local n = tonumber(storage.get("updated_runs") or "0") or 0
  storage.set("updated_runs", tostring(n + 1))
end`

// With empty fields meaning "keep", an update can change nothing — `PUT {}`
// or a recolor to the same color. That must not announce a tag:updated the
// UI and plugins would act on.
func TestUpdateTagThatChangesNothingIsSilent(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagUpdatedCounterSrc)
	defer pm.Shutdown()
	id := mustCreateTag(t, app, "work")
	_, color := tagRow(t, app, id)

	for _, c := range []struct{ name, color string }{{"", ""}, {"work", ""}, {"", color}, {"work", color}} {
		if err := app.UpdateTag(id, c.name, c.color); err != nil {
			t.Fatalf("UpdateTag(%q, %q): %v", c.name, c.color, err)
		}
	}
	if got := pluginStorageValue(t, app, pluginID, "updated_runs"); got != "" {
		t.Fatalf("no-op updates emitted tag:updated %s time(s)", got)
	}
	if err := app.UpdateTag(id, "", "#00ff00"); err != nil {
		t.Fatal(err)
	}
	if got := pluginStorageValue(t, app, pluginID, "updated_runs"); got != "1" {
		t.Fatalf("a real recolor emitted tag:updated %q times, want 1", got)
	}
}
