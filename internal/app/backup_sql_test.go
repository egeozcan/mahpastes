package app

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rewriteBackupSQL copies the backup at src to a new ZIP whose database.sql is
// replaced by sqlText — the shape of a crafted backup handed to a user.
func rewriteBackupSQL(t *testing.T, src, sqlText string) string {
	t.Helper()
	r, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	dst := filepath.Join(t.TempDir(), "crafted.zip")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for _, f := range r.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "database.sql" {
			if _, err := io.WriteString(w, sqlText); err != nil {
				t.Fatal(err)
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}

// restoreRowsInTx runs restoreBackupRows over sqlText in its own transaction,
// committing only on success, as RestoreBackup does.
func restoreRowsInTx(t *testing.T, db *sql.DB, sqlText string) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := restoreBackupRows(tx, strings.NewReader(sqlText)); err != nil {
		return err
	}
	return tx.Commit()
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Restore used to Exec each ";\n"-separated piece of database.sql, so a
// backup could run any statement against the live database. Only the
// INSERT-only dialect the export writes is accepted now; anything else fails
// the restore before it is applied.
func TestRestoreBackupRowsRejectsAnythingButInserts(t *testing.T) {
	attached := filepath.Join(t.TempDir(), "planted.db")
	for name, payload := range map[string]string{
		"attach":          "ATTACH DATABASE '" + attached + "' AS planted;\nCREATE TABLE planted.x (y);\n",
		"drop":            "DROP TABLE tags;\n",
		"delete":          "DELETE FROM tags;\n",
		"trigger":         "CREATE TRIGGER t AFTER INSERT ON clips BEGIN DELETE FROM tags; END;\n",
		"pragma":          "PRAGMA writable_schema = 1;\n",
		"insert select":   "INSERT INTO tags (name, color) SELECT name, color FROM tags;\n",
		"subquery value":  "INSERT INTO tags (name, color) VALUES ((SELECT 'x'), '#fff');\n",
		"function value":  "INSERT INTO tags (name, color) VALUES (char(65), '#fff');\n",
		"second stmt":     "INSERT INTO tags (name, color) VALUES ('a', '#fff'); DELETE FROM tags;\n",
		"quoted table":    `INSERT INTO "tags" (name, color) VALUES ('a', '#fff');` + "\n",
		"unterminated":    "INSERT INTO tags (name, color) VALUES ('a', '#fff')\n",
		"odd hex":         "INSERT INTO clips (id, data) VALUES (1, X'ABC');\n",
		"string run-on":   "INSERT INTO tags (name, color) VALUES ('a;\n", // EOF inside the literal
		"upsert":          "INSERT INTO tags (name, color) VALUES ('a', '#fff') ON CONFLICT DO NOTHING;\n",
		"replace keyword": "REPLACE INTO tags (name, color) VALUES ('a', '#fff');\n",
	} {
		t.Run(name, func(t *testing.T) {
			db := newBackupTestDB(t)
			if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'keep', '#111111')`); err != nil {
				t.Fatal(err)
			}
			err := restoreRowsInTx(t, db, payload)
			if !errors.Is(err, errBackupSQLSyntax) {
				t.Fatalf("restore of %q = %v, want a syntax refusal", payload, err)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM tags`); n != 1 {
				t.Fatalf("tags has %d rows after a refused restore, want the original 1", n)
			}
			if _, err := os.Stat(attached); err == nil {
				t.Fatal("ATTACH in a backup created a database file on disk")
			}
		})
	}
}

// Syntactically valid rows for a table a backup does not cover — api_keys,
// above all, which a restore neither clears nor exports — or for columns this
// install lacks are skipped, not inserted; the rest of the backup still lands.
func TestRestoreBackupRowsSkipsUnlistedTablesAndColumns(t *testing.T) {
	db := newBackupTestDB(t)
	if _, err := db.Exec(`CREATE TABLE api_keys (id INTEGER PRIMARY KEY, key_hash TEXT, role TEXT)`); err != nil {
		t.Fatal(err)
	}
	err := restoreRowsInTx(t, db, `-- crafted
INSERT INTO api_keys (id, key_hash, role) VALUES (1, 'abc', 'admin');
INSERT INTO sqlite_master (type, name) VALUES ('table', 'x');
INSERT INTO tags (id, name, color, from_a_newer_version) VALUES (2, 'extra-col', '#fff', 1);
INSERT INTO tags (id, name, name) VALUES (3, 'dup', 'dup');
INSERT INTO tags (id, name) VALUES (4);
INSERT INTO settings (key, value) VALUES ('some_api_key', 'planted');
INSERT INTO settings (key, value) VALUES (X'736f6d655f6170695f6b6579', 'planted-as-blob');
INSERT INTO settings (key, value) VALUES ('theme', 'dark');
INSERT INTO TAGS (ID, NAME, COLOR) VALUES (5, 'upper', '#fff');
`)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM api_keys`); n != 0 {
		t.Fatal("a backup row was inserted into api_keys")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM tags WHERE id IN (3, 4)`); n != 0 {
		t.Fatalf("%d malformed tag rows were inserted", n)
	}
	// A column this install lacks (a backup from a newer version) is dropped,
	// not the row: skipping every row of a table that grew a column emptied
	// the whole table while the restore reported success.
	if n := countRows(t, db, `SELECT COUNT(*) FROM tags WHERE id = 2 AND name = 'extra-col' AND color = '#fff'`); n != 1 {
		t.Fatal("a row with a column from a newer version was dropped instead of the column")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM settings WHERE CAST(key AS TEXT) = 'some_api_key'`); n != 0 {
		t.Fatal("a sensitive setting (never exported) was restored, as text or as a blob")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM settings WHERE key = 'theme'`); n != 1 {
		t.Fatal("an ordinary setting was not restored")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM tags WHERE id = 5 AND name = 'upper'`); n != 1 {
		t.Fatal("identifiers are case-insensitive in SQL; an upper-case row was dropped")
	}
}

// Every value form formatSQLValue writes comes back as it went in. The old
// ";\n" split also cut any string containing ";\n" in two, losing the row.
func TestRestoreBackupRowsRoundTripsExport(t *testing.T) {
	src := newBackupTestDB(t)
	blob := []byte{0, 1, 2, 0xfe, 0xff, '\'', ';', '\n'}
	tricky := "it's a ';\n' -- not a comment\r\n\ttabs \"quotes\" ünïcödé"
	if _, err := src.Exec(`INSERT INTO clips (id, content_type, data, filename, created_at, expires_at, is_archived, name) VALUES (1, 'text/plain', ?, ?, -42, NULL, 1, '')`,
		blob, tricky); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (2, 'text/plain', X'', 'empty')`); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Exec(`INSERT INTO settings (key, value) VALUES ('float', 1.5)`); err != nil {
		t.Fatal(err)
	}

	var dump bytes.Buffer
	for _, table := range []string{"clips", "settings"} {
		if _, err := exportTableToSQL(src, table, &dump, nil); err != nil {
			t.Fatal(err)
		}
	}

	dst := newBackupTestDB(t)
	if err := restoreRowsInTx(t, dst, dump.String()); err != nil {
		t.Fatalf("restore: %v\n%s", err, dump.String())
	}

	var gotBlob []byte
	var gotName string
	var created, archived int64
	var expires sql.NullInt64
	var name string
	if err := dst.QueryRow(`SELECT data, filename, created_at, expires_at, is_archived, name FROM clips WHERE id = 1`).
		Scan(&gotBlob, &gotName, &created, &expires, &archived, &name); err != nil {
		t.Fatalf("restored clip 1: %v", err)
	}
	if !bytes.Equal(gotBlob, blob) || gotName != tricky || created != -42 || expires.Valid || archived != 1 || name != "" {
		t.Fatalf("clip 1 restored as data=%x filename=%q created=%d expires=%v archived=%d name=%q",
			gotBlob, gotName, created, expires, archived, name)
	}
	var emptyLen int
	if err := dst.QueryRow(`SELECT length(data) FROM clips WHERE id = 2`).Scan(&emptyLen); err != nil || emptyLen != 0 {
		t.Fatalf("empty blob restored as length %d (%v)", emptyLen, err)
	}
	var f float64
	if err := dst.QueryRow(`SELECT value FROM settings WHERE key = 'float'`).Scan(&f); err != nil || f != 1.5 {
		t.Fatalf("float restored as %v (%v)", f, err)
	}
}

// End to end: a crafted backup is refused as a whole and the library is left
// exactly as it was.
func TestRestoreBackupRefusesCraftedSQL(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatal(err)
	}

	srcDB := newBackupTestDB(t)
	good := filepath.Join(t.TempDir(), "good.zip")
	if err := (&App{db: srcDB}).CreateBackup(good); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	attached := filepath.Join(t.TempDir(), "planted.db")
	crafted := rewriteBackupSQL(t, good,
		"INSERT INTO tags (id, name, color) VALUES (7, 'looks-normal', '#fff');\n"+
			"ATTACH DATABASE '"+attached+"' AS planted;\n")

	dstDB := newBackupTestDB(t)
	if _, err := dstDB.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (999, 'text/plain', 'sentinel', 'sentinel.txt')`); err != nil {
		t.Fatal(err)
	}
	err := (&App{db: dstDB}).RestoreBackup(crafted, "none")
	if err == nil || !errors.Is(err, errBackupSQLSyntax) {
		t.Fatalf("RestoreBackup of a crafted backup = %v, want a syntax refusal", err)
	}
	if n := countRows(t, dstDB, `SELECT COUNT(*) FROM clips WHERE id = 999`); n != 1 {
		t.Fatal("a refused restore still cleared the library")
	}
	if n := countRows(t, dstDB, `SELECT COUNT(*) FROM tags WHERE id = 7`); n != 0 {
		t.Fatal("rows before the refused statement were committed")
	}
	if _, err := os.Stat(attached); err == nil {
		t.Fatal("ATTACH in a backup created a database file on disk")
	}
}

// Every table the export writes must be one the restore accepts, or its rows
// are silently skipped on restore; and every table the restore clears must be
// exported, or a restore empties it for good.
func TestBackupTablesMatchExport(t *testing.T) {
	db := newBackupTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'x', 'x.txt')`,
		`INSERT INTO tags (id, name, color) VALUES (1, 't', '#fff')`,
		`INSERT INTO clip_tags (clip_id, tag_id) VALUES (1, 1)`,
		`INSERT INTO settings (key, value) VALUES ('theme', 'dark')`,
		`INSERT INTO watched_folders (id, path, is_paused) VALUES (1, '/x', 0)`,
		`INSERT INTO plugins (id, name) VALUES (1, 'p')`,
		`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (1, 'k', 'v')`,
		`INSERT INTO plugin_permissions (plugin_id, permission, pending_reconfirm) VALUES (1, 'x', 0)`,
		`INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, created_at) VALUES (1, 1, X'00', X'01', 1, 0)`,
		`INSERT INTO follows (id, remote_peer_id, symkey, local_tag_id, created_at) VALUES (1, 'peer', X'00', 1, 0)`,
		`INSERT INTO share_ring (id, publication_id, seq, kind, envelope_bytes, ts) VALUES (1, 1, 1, 'clip_start', X'00', 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	dump := filepath.Join(t.TempDir(), "database.sql")
	if _, _, err := (&App{db: db}).exportDatabaseToSQL(dump); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dump)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	listed := map[string]bool{}
	for _, table := range backupTables {
		listed[table] = true
	}
	exported := map[string]bool{}
	reader := newBackupSQLReader(f)
	for {
		ins, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("the export is not in the dialect the restore parses: %v", err)
		}
		exported[ins.table] = true
		if !listed[ins.table] {
			t.Errorf("exportDatabaseToSQL writes %q but backupTables does not list it: restore would skip its rows", ins.table)
		}
	}
	for _, table := range backupTables {
		if !exported[table] {
			t.Errorf("backupTables lists %q but the export did not write it: restore would clear it and put nothing back", table)
		}
	}
}

// newProductionSchemaDB opens a database through initDB — the real schema,
// foreign keys and all — in a fresh data dir, which it also points
// MAHPASTES_DATA_DIR at.
func newProductionSchemaDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dataDir := restoreDataDir(t)
	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dataDir
}

func mustExecSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// SQLite answers some failures — a full disk above all — by rolling back the
// whole transaction. The row writer used to treat every insert error as one
// skipped row and carry on, so everything after it ran in autocommit on the
// live database: backup rows merged into the library being replaced, every
// share invalidated, every watch folder paused, and then Commit failed with
// the restore reporting an error over the wreckage. Only a row the schema
// refuses (a constraint) may be skipped; anything else aborts the restore.
func TestRestoreBackupAbortsWhenTheDatabaseFails(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	big := make([]byte, 4<<20)
	for i := range big {
		big[i] = byte(i * 13)
	}
	for i := 0; i < 5; i++ {
		mustExecSQL(t, src, `INSERT INTO clips (content_type, data, filename) VALUES ('image/png', ?, ?)`, big, "big"+strconv.Itoa(i)+".png")
	}
	mustExecSQL(t, src, `INSERT INTO tags (id, name, color) VALUES (1, 'restored-tag', '#fff')`)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}

	dst, _ := newProductionSchemaDB(t)
	dst.SetMaxOpenConns(1) // the page cap below is per connection
	mustExecSQL(t, dst, `INSERT INTO clips (id, content_type, data, filename) VALUES (999, 'text/plain', X'00', 'sentinel')`)
	mustExecSQL(t, dst, `INSERT INTO tags (id, name, color) VALUES (50, 'orig-tag', '#000000')`)
	mustExecSQL(t, dst, `INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, created_at, status) VALUES (1, 50, X'00', X'01', 0, 0, 'active')`)
	var pages int64
	if err := dst.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	// Room for about two of the five clips: the third insert hits SQLITE_FULL.
	mustExecSQL(t, dst, `PRAGMA max_page_count = `+strconv.FormatInt(pages+2600, 10))

	if err := (&App{db: dst}).RestoreBackup(backup, "none"); err == nil {
		t.Fatal("RestoreBackup returned nil on a full disk")
	}
	if n := countRows(t, dst, `SELECT COUNT(*) FROM clips`); n != 1 {
		t.Fatalf("library has %d clips after a failed restore, want only the original sentinel", n)
	}
	if n := countRows(t, dst, `SELECT COUNT(*) FROM tags WHERE name = 'restored-tag'`); n != 0 {
		t.Fatal("rows from the backup were merged into the library by a failed restore")
	}
	var status string
	if err := dst.QueryRow(`SELECT status FROM shares WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("original share gone after a failed restore: %v", err)
	}
	if status != "active" {
		t.Fatalf("original share is %q after a failed restore, want active", status)
	}
}

// Restored plugins re-ask for every permission (pending_reconfirm) and
// restored watch folders start paused. Those updates used to print a warning
// on failure and commit anyway — leaving a backup's plugins with their
// permissions live and its folders importing.
func TestRestoreBackupFailsWhenPermissionsCannotBeReset(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	mustExecSQL(t, src, `INSERT INTO plugins (id, filename, name, version) VALUES (1, 'p.lua', 'P', '1.0')`)
	mustExecSQL(t, src, `INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (1, 'fs_read', '/x')`)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}

	dst, _ := newProductionSchemaDB(t)
	mustExecSQL(t, dst, `INSERT INTO clips (id, content_type, data, filename) VALUES (999, 'text/plain', X'00', 'sentinel')`)
	mustExecSQL(t, dst, `CREATE TRIGGER fail_reconfirm BEFORE UPDATE OF pending_reconfirm ON plugin_permissions
		BEGIN SELECT RAISE(ABORT, 'injected'); END;`)

	if err := (&App{db: dst}).RestoreBackup(backup, "none"); err == nil {
		t.Fatal("RestoreBackup succeeded with restored plugin permissions still confirmed")
	}
	if n := countRows(t, dst, `SELECT COUNT(*) FROM clips WHERE id = 999`); n != 1 {
		t.Fatal("the failed restore was committed")
	}
}

// A plugin row's filename is joined to the plugins dir to read and, on
// removal, delete the plugin's file. A crafted backup could set it to a path
// out of that dir, and removing the unfamiliar plugin it shows then deleted
// whatever file the path named.
func TestRestoreBackupSkipsPluginRowsWithAPath(t *testing.T) {
	db := newBackupTestDB(t)
	err := restoreRowsInTx(t, db, `
INSERT INTO plugins (id, name, filename) VALUES (1, 'traversal', '../../important.txt');
INSERT INTO plugins (id, name, filename) VALUES (2, 'absolute', '/etc/hosts');
INSERT INTO plugins (id, name, filename) VALUES (3, 'nested', 'sub/p.lua');
INSERT INTO plugins (id, name, filename) VALUES (4, 'not lua', 'notes.txt');
INSERT INTO plugins (id, name, filename) VALUES (5, 'ok', 'fine-plugin.lua');
INSERT INTO plugins (id, name, filename) VALUES (6, 'blob', X'2e2e2f782e6c7561');
`)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	var names []string
	rows, err := db.Query(`SELECT name FROM plugins ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if strings.Join(names, ",") != "ok" {
		t.Fatalf("restored plugins %v, want only the one with a bare .lua filename", names)
	}
}

// plugin_permissions rows reach the plugin card as stored, and restore marks
// every one pending so the user opens the card to reconfirm. Only the types
// the app grants are restored; anything else did not come from this app.
func TestRestoreBackupSkipsUnknownPermissionTypes(t *testing.T) {
	prod, _ := newProductionSchemaDB(t)
	mustExecSQL(t, prod, `INSERT INTO plugins (id, filename, name) VALUES (7, 'p.lua', 'P')`)
	tx, err := prod.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := restoreBackupRows(tx, strings.NewReader(`
INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (7, '<img src=x onerror=alert(1)>', '/tmp');
INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (7, X'66735f72656164', '/blob');
INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (7, 'fs_read', '/a');
INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (7, 'fs_write', '/b');
INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (7, 'network', 'example.com');
`)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var types []string
	rows, err := tx.Query(`SELECT permission_type FROM plugin_permissions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ty string
		if err := rows.Scan(&ty); err != nil {
			t.Fatal(err)
		}
		types = append(types, ty)
	}
	if strings.Join(types, ",") != "fs_read,fs_write,network" {
		t.Fatalf("restored permission types %q, want only the ones the app grants", types)
	}
}

// Every production table round-trips byte- and type-exact through a real
// CreateBackup and RestoreBackup, foreign keys and NOT NULL columns included.
func TestRestoreBackupRoundTripsProductionSchema(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	exp := "2030-01-02 03:04:05"
	// Each with the content_hash every production insert writes: the restore
	// recomputes it from the bytes, so only a true one round-trips unchanged.
	big := bytes.Repeat([]byte{0, 1, 0xfe, '\'', ';', '\n'}, 50000)
	mustExecSQL(t, src, `INSERT INTO clips (id, content_type, data, filename, content_hash) VALUES (1, 'image/png', ?, 'big.png', ?)`, big, computeContentHash(big))
	text := []byte("it's ;\n -- x\x00")
	mustExecSQL(t, src, `INSERT INTO clips (id, content_type, data, filename, expires_at, metadata, content_hash) VALUES (2, 'text/plain', ?, 'o''neil;
.txt', ?, '{"k":"v;\n''q''"}', ?)`, text, exp, computeContentHash(text))
	mustExecSQL(t, src, `INSERT INTO clips (id, content_type, data, filename, is_archived, content_hash) VALUES (3, 'text/plain', X'', NULL, 1, ?)`, computeContentHash(nil))
	mustExecSQL(t, src, `INSERT INTO tags (id, name, color) VALUES (1, 'work', '#111111'), (2, 'work/client', '#222222')`)
	mustExecSQL(t, src, `INSERT INTO clip_tags (clip_id, tag_id) VALUES (1, 1), (2, 2)`)
	mustExecSQL(t, src, `INSERT INTO settings (key, value) VALUES ('theme', 'dark'), ('n', '-42')`)
	mustExecSQL(t, src, `INSERT INTO plugins (id, filename, name, version, enabled, status) VALUES (1, 'p.lua', 'P', '1.0', 0, 'disabled')`)
	mustExecSQL(t, src, `INSERT INTO plugin_storage (plugin_id, key, value) VALUES (1, 'k', ?)`, []byte("v\x00"))
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}
	want := dumpBackupTables(t, src)

	dst, _ := newProductionSchemaDB(t)
	if err := (&App{db: dst}).RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	got := dumpBackupTables(t, dst)
	for _, table := range backupTables {
		if table == "plugins" || table == "plugin_permissions" {
			continue // restore resets status fields on these by design
		}
		if got[table] != want[table] {
			t.Errorf("%s differs after a round trip\nwant: %.400s\n got: %.400s", table, want[table], got[table])
		}
	}
}

// dumpBackupTables renders every backupTables row as typeof:quote per column,
// in rowid order.
func dumpBackupTables(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range backupTables {
		rows, err := db.Query(`SELECT name FROM pragma_table_info('` + table + `')`)
		if err != nil {
			t.Fatal(err)
		}
		var exprs []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			exprs = append(exprs, `typeof("`+c+`") || ':' || quote("`+c+`")`)
		}
		rows.Close()
		data, err := db.Query(`SELECT ` + strings.Join(exprs, ` || '|' || `) + ` FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for data.Next() {
			var line sql.NullString
			if err := data.Scan(&line); err != nil {
				t.Fatal(err)
			}
			b.WriteString(line.String)
			b.WriteString("\n")
		}
		data.Close()
		out[table] = b.String()
	}
	return out
}

// A backup's clips.content_hash is just a claim. Dedup trusts it — an upload
// whose hash matches is answered with the existing clip and discarded, and
// DeduplicateAll deletes clips whose hashes match — so a planted hash could
// swap a well-known file for the backup's clip, or delete clips whose bytes
// differ. The restore computes it from the bytes instead.
func TestRestoreBackupRecomputesContentHashes(t *testing.T) {
	db, _ := newProductionSchemaDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	planted := computeContentHash([]byte("hello"))
	if err := restoreBackupRows(tx, strings.NewReader(`
INSERT INTO clips (id, content_type, data, filename, content_hash) VALUES (1, 'text/plain', X'3c623e', 'a.txt', '`+planted+`');
INSERT INTO clips (id, content_type, data, filename) VALUES (2, 'text/plain', 'as text', 'b.txt');
`)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for id, data := range map[int64][]byte{1: []byte("<b>"), 2: []byte("as text")} {
		var got string
		if err := tx.QueryRow(`SELECT content_hash FROM clips WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := computeContentHash(data); got != want {
			t.Fatalf("clip %d content_hash %s, want the hash of its bytes %s", id, got, want)
		}
	}
}

// Watch folders come back paused and plugin permissions pending; follows came
// back live, so a backup's follow rows dialed their peers — a feed into a
// local tag from a peer of the backup's choosing, told when and from where
// the user is online — the moment the restore finished.
func TestRestoreBackupPausesFollows(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	mustExecSQL(t, src, `INSERT INTO tags (id, name, color) VALUES (1, 'feed', '#111111')`)
	mustExecSQL(t, src, `INSERT INTO follows (id, remote_peer_id, symkey, local_tag_id, created_at, paused) VALUES (1, 'peer', X'00', 1, 0, 0)`)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}
	dst, _ := newProductionSchemaDB(t)
	if err := (&App{db: dst}).RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if n := countRows(t, dst, `SELECT COUNT(*) FROM follows WHERE paused = 1`); n != 1 {
		t.Fatal("a restored follow is not paused")
	}
}

// A running tag server keeps its tag's id and name from when it started and
// looks its clips up by that id. After a restore the id can name a different
// tag: a server started for "public" on every interface would serve, and
// with read-write API access let the network edit, whatever the backup filed
// under that id.
func TestRestoreBackupStopsTagServers(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	mustExecSQL(t, src, `INSERT INTO tags (id, name, color) VALUES (5, 'private', '#111111')`)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}

	app, cleanup := setupTestApp(t)
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(os.Getenv("MAHPASTES_DATA_DIR"), "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, app.db, `INSERT INTO tags (id, name, color) VALUES (5, 'public', '#111111')`)
	if _, err := app.serveManager.StartServing(5, 0, false, "none"); err != nil {
		t.Fatalf("StartServing: %v", err)
	}
	if err := app.RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if app.serveManager.IsServing(5) {
		t.Fatal("a tag server kept running across a restore that gave its tag id to another tag")
	}
}

// The export always writes clip data as a blob (or text) literal. A row whose
// data is a number binds as INTEGER or REAL — accepted by the BLOB column —
// and would slip past the content_hash recompute, keeping a planted hash.
func TestRestoreBackupSkipsClipsWhoseDataIsNotBytes(t *testing.T) {
	db, _ := newProductionSchemaDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	planted := computeContentHash([]byte("a well-known file"))
	if err := restoreBackupRows(tx, strings.NewReader(`
INSERT INTO clips (id, content_type, data, filename, content_hash) VALUES (1, 'text/plain', 7, 'int.txt', '`+planted+`');
INSERT INTO clips (id, content_type, data, filename, content_hash) VALUES (2, 'text/plain', 7.500000, 'real.txt', '`+planted+`');
INSERT INTO clips (id, content_type, data, filename) VALUES (3, 'text/plain', X'6f6b', 'ok.txt');
`)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d clips restored, want only the one whose data is bytes", n)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM clips WHERE content_hash = ?`, planted).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a planted content_hash survived the restore (%d rows, %v)", n, err)
	}
}

// StartServing reads the tag's name and id from the database as it stands.
// Started while a restore is replacing the rows, a server would be serving,
// once it commits, whatever tag the backup gave that id — after the restore
// had already stopped every server for exactly that reason.
func TestStartServingRefusedDuringRestore(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	mustExecSQL(t, app.db, `INSERT INTO tags (id, name, color) VALUES (5, 'public', '#111111')`)

	app.backupRestoreMu.Lock()
	_, err := app.serveManager.StartServing(5, 0, false, "none")
	app.backupRestoreMu.Unlock()
	if err == nil {
		app.serveManager.StopAll()
		t.Fatal("StartServing succeeded while a restore held the restore lock")
	}
	if _, err := app.serveManager.StartServing(5, 0, false, "none"); err != nil {
		t.Fatalf("StartServing after the restore: %v", err)
	}
}

// The restore stops tag servers so none serves the wrong tag; a graceful
// shutdown buys nothing there and waited up to 3 s per server on any client
// with a request in flight — one trickling a body, needing no credentials —
// while the restore held every tag mutation off.
func TestRestoreDoesNotWaitOnTagServerClients(t *testing.T) {
	src, _ := newProductionSchemaDB(t)
	backup := filepath.Join(t.TempDir(), "b.zip")
	if err := (&App{db: src}).CreateBackup(backup); err != nil {
		t.Fatal(err)
	}
	app, cleanup := setupTestApp(t)
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(os.Getenv("MAHPASTES_DATA_DIR"), "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, app.db, `INSERT INTO tags (id, name, color) VALUES (5, 'public', '#111111')`)
	info, err := app.serveManager.StartServing(5, 0, false, "readwrite")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", info.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A request whose body never finishes arriving, short enough (under
	// net/http's 256 KB) that the server keeps draining it after answering.
	if _, err := conn.Write([]byte("POST /_api/doc HTTP/1.1\r\nHost: x\r\nContent-Length: 100000\r\n\r\n{")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	if err := app.RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("RestoreBackup took %v waiting on a tag-serve client", took)
	}
}
