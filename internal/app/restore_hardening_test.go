package app

import (
	"archive/zip"
	"errors"
	"go-clipboard/plugin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A scoped key merging duplicates used to keep the oldest copy — wherever it
// was filed — and give it the scoped copy's tag, pulling an out-of-scope
// original into the key's view.
func TestScopedKeyCannotMergeDuplicates(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	manager := NewAPIManager(app)
	ids := insertTagNames(t, app, "work", "work/a", "work/b")
	data := []byte("same bytes")
	hash := computeContentHash(data)
	res, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename, content_hash) VALUES ('text/plain', ?, 'orig.txt', ?)`, data, hash)
	if err != nil {
		t.Fatal(err)
	}
	origID, _ := res.LastInsertId()
	mustExecSQL(t, app.db, `INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, origID, ids["work/b"])
	res, err = app.db.Exec(`INSERT INTO clips (content_type, data, filename, content_hash) VALUES ('text/plain', ?, 'copy.txt', ?)`, data, hash)
	if err != nil {
		t.Fatal(err)
	}
	copyID, _ := res.LastInsertId()
	mustExecSQL(t, app.db, `INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, copyID, ids["work/a"])

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dedup/x/merge", nil)
	req.SetPathValue("clipId", strconv.FormatInt(copyID, 10))
	rec := httptest.NewRecorder()
	manager.handleMergeDuplicates(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "editor", ScopedTagID: ids["work/a"]}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("scoped merge: status %d, want 403", rec.Code)
	}
	if n := countRows(t, app.db, `SELECT COUNT(*) FROM clip_tags WHERE clip_id = ? AND tag_id = ?`, origID, ids["work/a"]); n != 0 {
		t.Fatal("the out-of-scope original was tagged into the key's scope")
	}
	if n := countRows(t, app.db, `SELECT COUNT(*) FROM clips`); n != 2 {
		t.Fatalf("%d clips left, want both copies untouched", n)
	}
}

// An upload inserts its clip and then tags it. A restore landing in between
// deletes the clip, and the id it held — and the tag id — now name restored
// rows: tagging went ahead and filed an unrelated restored clip under the
// upload's tag.
func TestUploadTagRefusedAfterRestoreReplacedTheClip(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The backup holds clip 1 and tag 1, the very ids the upload below uses.
	srcDB := newBackupTestDB(t)
	mustExecSQL(t, srcDB, `INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'restored', 'restored.txt')`)
	mustExecSQL(t, srcDB, `INSERT INTO tags (id, name, color) VALUES (1, 'restored-tag', '#000000')`)
	backupZip := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
		t.Fatal(err)
	}

	dstDB := newBackupTestDB(t)
	app := &App{db: dstDB}
	mustExecSQL(t, dstDB, `INSERT INTO tags (id, name, color) VALUES (1, 'uploads', '#000000')`)
	epoch := app.currentRestoreEpoch()
	mustExecSQL(t, dstDB, `INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'uploaded', 'up.txt')`)

	if err := app.RestoreBackup(backupZip, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	err := app.addTagToNewClip(1, 1, epoch)
	if !errors.Is(err, errRestoredSinceInsert) {
		t.Fatalf("tagging after the restore: err = %v, want errRestoredSinceInsert", err)
	}
	if n := countRows(t, dstDB, `SELECT COUNT(*) FROM clip_tags`); n != 0 {
		t.Fatal("the restored clip was tagged by the upload that raced the restore")
	}

	// With no restore in between, the same call tags as AddTagToClip does.
	if err := app.addTagToNewClip(1, 1, app.currentRestoreEpoch()); err != nil {
		t.Fatalf("tagging with a current epoch: %v", err)
	}
}

func writeTestBackupZip(t *testing.T, manifest, sqlText string, extra map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	files := map[string]string{"manifest.json": manifest, "database.sql": sqlText}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// The confirm dialog showed the manifest's summary, which is whatever the
// ZIP's author wrote: "0 plugins" over a database.sql that installs one.
func TestRestoreConfirmCountsComeFromDatabaseSQL(t *testing.T) {
	manifest := `{"format_version":1,"app_version":"1.0.0","summary":{"clips":0,"tags":0,"plugins":0,"watch_folders":0}}`
	sqlText := "INSERT INTO plugins (id, filename, name, enabled) VALUES (1, 'evil.lua', 'Evil', 1);\n" +
		"INSERT INTO plugins (id, filename, name, enabled) VALUES (2, '../escape.lua', 'Refused', 1);\n" +
		"INSERT INTO clips (id, content_type, data) VALUES (1, 'text/plain', 'a');\n" +
		"INSERT INTO clips (id, content_type, data) VALUES (2, 'text/plain', 'b');\n" +
		"INSERT INTO tags (id, name) VALUES (1, 't');\n" +
		"INSERT INTO watched_folders (id, path) VALUES (1, '/tmp/x');\n"
	path := writeTestBackupZip(t, manifest, sqlText, nil)

	got, err := InspectBackupForRestore(path)
	if err != nil {
		t.Fatalf("InspectBackupForRestore: %v", err)
	}
	want := BackupSummary{Clips: 2, Tags: 1, Plugins: 1, WatchFolders: 1}
	if got.Summary != want {
		t.Fatalf("summary = %+v, want %+v (the row restore would skip not counted)", got.Summary, want)
	}

	// A database.sql the restore would refuse is refused here too, rather
	// than shown with the manifest's numbers.
	bad := writeTestBackupZip(t, manifest, "DROP TABLE clips;\n", nil)
	if _, err := InspectBackupForRestore(bad); err == nil {
		t.Fatal("a backup whose database.sql is not INSERT-only was accepted")
	}
}

// SameSite=Strict ignores ports, so a page a tag server hosts on another port
// of the same host is same-site with the web UI and its requests carry the
// session cookie. A cookie-authenticated write must come from the UI's own
// origin; bearer-key clients and plain reads are unaffected.
func TestSessionCookieWritesMustBeSameOrigin(t *testing.T) {
	db := newServerTestDB(t)
	mustExecSQL(t, db, `INSERT INTO api_keys (id, name, key_hash, key_prefix, role) VALUES (1, 'admin', 'h', 'p', 'admin')`)
	manager := NewAPIManager(&App{db: db}, t.TempDir())
	token := signSessionToken(manager.signingKey, 1, time.Now().Add(time.Hour))
	handler := manager.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"same-origin fetch", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://pastes.test:44557"}, http.StatusNoContent},
		{"tag server on another port", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://pastes.test:8080"}, http.StatusForbidden},
		{"cross-site form post", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"older browser, matching Origin", http.MethodDelete, map[string]string{"Origin": "http://pastes.test:44557"}, http.StatusNoContent},
		{"older browser, other port", http.MethodDelete, map[string]string{"Origin": "http://pastes.test:8080"}, http.StatusForbidden},
		{"opaque origin", http.MethodPut, map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"no browser headers", http.MethodPost, nil, http.StatusForbidden},
		{"cross-site read", http.MethodGet, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://pastes.test:44557/api/v1/clips/1", nil)
			req.AddCookie(&http.Cookie{Name: "_mp_session", Value: token})
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// Restored plugins used to run straight away: LoadPlugins loads every enabled
// row, and a backup's plugins rows and files are whatever its author wrote.
// Now a restored plugin keeps the backup's state only when its file is one
// this install had already reviewed, byte for byte; every other one comes
// back disabled and held for review.
func TestRestoreHoldsUnreviewedPluginsForReview(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)
	pluginsDir := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "-- mine\n"
	const changedBefore = "-- reviewed version\n"
	const changedAfter = "-- edited inside the backup\n"
	const planted = "-- planted\n"
	for name, body := range map[string]string{"mine.lua": mine, "changed.lua": changedBefore, "pending.lua": planted} {
		if err := os.WriteFile(filepath.Join(pluginsDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dstDB := newBackupTestDB(t)
	mustExecSQL(t, dstDB, `INSERT INTO plugins (id, filename, name, enabled, status) VALUES (1, 'mine.lua', 'Mine', 1, 'enabled')`)
	mustExecSQL(t, dstDB, `INSERT INTO plugins (id, filename, name, enabled, status) VALUES (2, 'changed.lua', 'Changed', 1, 'enabled')`)
	// Held by an earlier restore and never approved: not a review.
	mustExecSQL(t, dstDB, `INSERT INTO plugins (id, filename, name, enabled, status) VALUES (3, 'pending.lua', 'Pending', 0, ?)`, plugin.StatusNeedsReview)

	manifest := `{"format_version":1,"app_version":"1.0.0","summary":{}}`
	sqlText := "INSERT INTO plugins (id, filename, name, enabled, status) VALUES (10, 'mine.lua', 'Mine', 1, 'enabled');\n" +
		"INSERT INTO plugins (id, filename, name, enabled, status) VALUES (11, 'changed.lua', 'Changed', 1, 'enabled');\n" +
		"INSERT INTO plugins (id, filename, name, enabled, status) VALUES (12, 'evil.lua', 'Evil', 1, 'enabled');\n" +
		"INSERT INTO plugins (id, filename, name, enabled, status) VALUES (13, 'pending.lua', 'Pending', 1, 'enabled');\n"
	zipPath := writeTestBackupZip(t, manifest, sqlText, map[string]string{
		"plugins/mine.lua":    mine,
		"plugins/changed.lua": changedAfter,
		"plugins/evil.lua":    "-- evil\n",
		"plugins/pending.lua": planted,
	})

	if err := (&App{db: dstDB}).RestoreBackup(zipPath, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	want := map[string]struct {
		enabled int
		status  string
	}{
		"mine.lua":    {1, "enabled"},
		"changed.lua": {0, plugin.StatusNeedsReview},
		"evil.lua":    {0, plugin.StatusNeedsReview},
		"pending.lua": {0, plugin.StatusNeedsReview},
	}
	for name, w := range want {
		var enabled int
		var status string
		if err := dstDB.QueryRow(`SELECT enabled, status FROM plugins WHERE filename = ?`, name).Scan(&enabled, &status); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if enabled != w.enabled || status != w.status {
			t.Errorf("%s: enabled=%d status=%q, want enabled=%d status=%q", name, enabled, status, w.enabled, w.status)
		}
	}
}
