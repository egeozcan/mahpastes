package app

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

// Tag subtrees were matched with SQL LIKE: name LIKE parent || '/%'. LIKE folds
// ASCII case and reads _ and % in the parent's name as wildcards, so every
// operation on "my_tag"'s subtree also reached "my-tag/…" and "myXtag/…", and
// every operation on "Work"'s reached "work/…" — renames and merges rewrote
// names in trees the user never touched, filters listed their clips, and a key
// scoped to one tree could read the other.

func insertTagNames(t *testing.T, app *App, names ...string) map[string]int64 {
	t.Helper()
	ids := map[string]int64{}
	for _, n := range names {
		res, err := app.db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#111111')`, n)
		if err != nil {
			t.Fatalf("insert tag %q: %v", n, err)
		}
		ids[n], _ = res.LastInsertId()
	}
	return ids
}

func allTagNames(t *testing.T, app *App) []string {
	t.Helper()
	rows, err := app.db.Query(`SELECT name FROM tags`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func TestRenameCascadeTouchesOnlyItsOwnSubtree(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	ids := insertTagNames(t, app, "my_tag", "my_tag/c", "my-tag/child", "Work", "Work/a", "work/notes")

	if err := app.UpdateTag(ids["my_tag"], "z", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.UpdateTag(ids["Work"], "Job", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"Job", "Job/a", "my-tag/child", "work/notes", "z", "z/c"}
	if got := allTagNames(t, app); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after renames: %v, want %v", got, want)
	}
}

// Renaming a tag into its own subtree made the renamed row match its own
// cascade: photos → photos/2024 produced photos/2024/2024.
func TestRenameIntoOwnSubtreeIsRefused(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	ids := insertTagNames(t, app, "photos", "photos/old")
	if err := app.UpdateTag(ids["photos"], "photos/2024", ""); err == nil {
		t.Fatal("renaming a tag into its own subtree succeeded")
	}
	if got := allTagNames(t, app); strings.Join(got, ",") != "photos,photos/old" {
		t.Fatalf("tags after a refused rename: %v", got)
	}
}

func TestMergeTouchesOnlyItsOwnSubtree(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	ids := insertTagNames(t, app, "a_b", "a_b/c", "aXb/d", "dest")
	if err := app.MergeTag(ids["a_b"], ids["dest"]); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	names := strings.Join(allTagNames(t, app), ",")
	if !strings.Contains(names, "dest/c") || !strings.Contains(names, "aXb/d") || strings.Contains(names, "dest/d") {
		t.Fatalf("tags after merging a_b into dest: %s", names)
	}
}

func TestDescendantLookupsAreExact(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	ids := insertTagNames(t, app, "my_tag", "my_tag/c", "my-tag/child", "MY_TAG/x")

	got, err := app.getDescendantTagIDs(ids["my_tag"])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != ids["my_tag/c"] {
		t.Fatalf("descendants of my_tag: %v, want only my_tag/c (%d)", got, ids["my_tag/c"])
	}
	children, err := app.getChildTags(ids["my_tag"])
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Name != "my_tag/c" {
		t.Fatalf("children of my_tag: %+v", children)
	}
}

func TestScopedKeyCannotReachALookalikeTree(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	am := NewAPIManager(app)
	ids := insertTagNames(t, app, "my_proj", "my-proj/x", "MY_PROJ/y")
	res, err := db.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('text/plain', 'secret', 'other-team.txt')`)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := res.LastInsertId()
	for _, tag := range []string{"my-proj/x", "MY_PROJ/y"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, ids[tag]); err != nil {
			t.Fatal(err)
		}
	}
	if err := am.enforceTagScope(&apiKeyContext{KeyID: 1, Role: "editor", ScopedTagID: ids["my_proj"]}, clipID); err == nil {
		t.Fatal("a key scoped to my_proj reached a clip filed under my-proj/ and MY_PROJ/")
	}
}

// An upload whose bytes match an existing clip returns that clip (and tags it
// into the key's scope). The lookup ignored scope, so a key scoped to work/a
// that uploaded the bytes of a clip filed under work/b got that clip back —
// its name and metadata — and moved it into work/a by tree exclusivity, where
// the key could then read, change or delete it.
func TestScopedUploadDoesNotDedupIntoAnotherScope(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	manager := NewAPIManager(app)
	ids := insertTagNames(t, app, "work", "work/a", "work/b")
	secret := []byte("the b team's file")
	res, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename, content_hash) VALUES ('text/plain', ?, 'b-team-secret-name.txt', ?)`,
		secret, computeContentHash(secret))
	if err != nil {
		t.Fatal(err)
	}
	origID, _ := res.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, origID, ids["work/b"]); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "upload.txt")
	_, _ = part.Write(secret)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clips", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	manager.handleCreateClip(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "editor", ScopedTagID: ids["work/a"]}))

	if strings.Contains(rec.Body.String(), "b-team-secret-name") {
		t.Fatalf("the upload's response disclosed the out-of-scope clip: %s", rec.Body.String())
	}
	var tags []string
	rows, err := app.db.Query(`SELECT t.name FROM clip_tags ct JOIN tags t ON t.id = ct.tag_id WHERE ct.clip_id = ?`, origID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tags = append(tags, n)
	}
	if strings.Join(tags, ",") != "work/b" {
		t.Fatalf("the original clip is now tagged %v, want it left in work/b", tags)
	}
}

func scopedUpload(t *testing.T, manager *APIManager, scopedTagID int64, data []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "upload.txt")
	_, _ = part.Write(data)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clips", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	manager.handleCreateClip(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "editor", ScopedTagID: scopedTagID}))
	if rec.Code >= 300 {
		t.Fatalf("scoped upload: %d %s", rec.Code, rec.Body.String())
	}
}

// The scoped dedup must find the key's own earlier copy: looking up the hash
// anywhere and then checking scope always hit the out-of-scope original, so
// every re-upload of the same bytes by a sync job added another copy.
func TestScopedReuploadFindsItsOwnCopy(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	manager := NewAPIManager(app)
	ids := insertTagNames(t, app, "work", "work/a", "work/b")
	data := []byte("shared bytes")
	res, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename, content_hash) VALUES ('text/plain', ?, 'orig.txt', ?)`, data, computeContentHash(data))
	if err != nil {
		t.Fatal(err)
	}
	origID, _ := res.LastInsertId()
	mustExecSQL(t, app.db, `INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, origID, ids["work/b"])

	for i := 0; i < 3; i++ {
		scopedUpload(t, manager, ids["work/a"], data)
	}
	if n := countRows(t, app.db, `SELECT COUNT(*) FROM clips`); n != 2 {
		t.Fatalf("%d clips after three scoped uploads of the same bytes, want the original and one copy", n)
	}
}

// A duplicate already inside the key's scope is returned as it is. Re-tagging
// it with the scope's root tag moved a clip filed under work/a/deep up to
// work/a by tree exclusivity.
func TestScopedReuploadLeavesTheClipWhereItIs(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	manager := NewAPIManager(app)
	ids := insertTagNames(t, app, "work", "work/a", "work/a/deep")
	data := []byte("deep bytes")
	res, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename, content_hash) VALUES ('text/plain', ?, 'deep.txt', ?)`, data, computeContentHash(data))
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := res.LastInsertId()
	mustExecSQL(t, app.db, `INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, ids["work/a/deep"])

	scopedUpload(t, manager, ids["work/a"], data)
	var name string
	if err := app.db.QueryRow(`SELECT t.name FROM clip_tags ct JOIN tags t ON t.id = ct.tag_id WHERE ct.clip_id = ?`, clipID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "work/a/deep" {
		t.Fatalf("re-uploading moved the clip to %q", name)
	}
}

// SQLite's length and substr stop at NUL, so a name holding one lost its
// subtree to every subtree query; invalid UTF-8 made the cascade's character
// offsets disagree and garbled child names. Neither is a name anyone means.
func TestTagNamesMustBeCleanText(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	ids := insertTagNames(t, app, "ok")
	for _, bad := range []string{"a\x00b", "p\xc3\xa9\xa9", "tab\there", "line\nbreak", "bell\x07"} {
		if _, err := app.CreateTag(bad); err == nil {
			t.Errorf("CreateTag accepted %q", bad)
		}
		if err := app.UpdateTag(ids["ok"], bad, ""); err == nil {
			t.Errorf("UpdateTag accepted %q", bad)
		}
	}
	if _, err := app.CreateTag("café/ünïcødé 日本"); err != nil {
		t.Fatalf("a legitimate non-ASCII name was refused: %v", err)
	}
}

// A tag created before names were checked keeps working: recoloring it while
// passing its own (legacy) name back is not a rename and must not be refused.
func TestLegacyTagNameCanStillBeRecolored(t *testing.T) {
	app := &App{db: newServerTestDB(t)}
	ids := insertTagNames(t, app, "tab\there")
	if err := app.UpdateTag(ids["tab\there"], "tab\there", "#112233"); err != nil {
		t.Fatalf("recoloring a legacy-named tag: %v", err)
	}
	if _, color := tagRow(t, app, ids["tab\there"]); color != "#112233" {
		t.Fatalf("color = %q", color)
	}
	if err := app.UpdateTag(ids["tab\there"], "still\tbad", ""); err == nil {
		t.Fatal("renaming to another control-character name was accepted")
	}
}
