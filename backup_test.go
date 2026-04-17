package main

import (
	"archive/zip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// newBackupTestDB creates a minimal SQLite DB with all tables that exportDatabaseToSQL
// references, so CreateBackup can complete without a real mahpastes install.
// SetMaxOpenConns(1) is required for in-memory and file-based SQLite under Go's
// database/sql connection pool — without it concurrent tests can open multiple
// connections and the in-memory state (or temp-file locking) becomes unreliable.
func newBackupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE clips (id INTEGER PRIMARY KEY, content_type TEXT, data BLOB, filename TEXT, created_at INTEGER, expires_at INTEGER, is_archived INTEGER, name TEXT)`,
		`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, color TEXT)`,
		`CREATE TABLE clip_tags (clip_id INTEGER, tag_id INTEGER)`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE watched_folders (id INTEGER PRIMARY KEY, path TEXT, is_paused INTEGER)`,
		`CREATE TABLE plugins (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE plugin_storage (plugin_id INTEGER, key TEXT, value TEXT)`,
		`CREATE TABLE plugin_permissions (plugin_id INTEGER, permission TEXT, pending_reconfirm INTEGER)`,
		`CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER, symkey BLOB, share_id BLOB UNIQUE, last_seq INTEGER, clips_sent INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at INTEGER)`,
		`CREATE TABLE follows (id INTEGER PRIMARY KEY, remote_peer_id TEXT, symkey BLOB, local_tag_id INTEGER, last_seq INTEGER DEFAULT 0, clips_received INTEGER NOT NULL DEFAULT 0, last_seen_at INTEGER, created_at INTEGER)`,
		`CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER, FOREIGN KEY(publication_id) REFERENCES shares(id) ON DELETE CASCADE)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	return db
}

// TestCreateBackupIncludesIdentityKey verifies that CreateBackup includes
// share_identity.key in the ZIP when the file is present in dataDir.
func TestCreateBackupIncludesIdentityKey(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	// Write a fake identity file with known content.
	wantContent := []byte("fake-identity-key-bytes-for-test")
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	if err := os.WriteFile(identityPath, wantContent, 0600); err != nil {
		t.Fatalf("write identity file: %v", err)
	}

	// Also create plugins dir so CreateBackup doesn't trip on os.ReadDir.
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	app := &App{db: newBackupTestDB(t)}

	destZip := filepath.Join(t.TempDir(), "backup.zip")
	if err := app.CreateBackup(destZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	// Open the ZIP and look for share_identity.key.
	r, err := zip.OpenReader(destZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	var found *zip.File
	for _, f := range r.File {
		if f.Name == ShareIdentityFile {
			found = f
			break
		}
	}
	if found == nil {
		t.Fatalf("%s not found in backup ZIP", ShareIdentityFile)
	}

	rc, err := found.Open()
	if err != nil {
		t.Fatalf("open zip entry: %v", err)
	}
	defer rc.Close()

	gotContent, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read zip entry: %v", err)
	}

	if string(gotContent) != string(wantContent) {
		t.Errorf("identity file content mismatch\n  got:  %q\n  want: %q", gotContent, wantContent)
	}
}

// TestCreateBackupNoIdentityKey verifies that CreateBackup succeeds and does NOT
// include share_identity.key in the ZIP when the file is absent (fresh install).
func TestCreateBackupNoIdentityKey(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	// No identity file — just the plugins dir.
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	app := &App{db: newBackupTestDB(t)}

	destZip := filepath.Join(t.TempDir(), "backup.zip")
	if err := app.CreateBackup(destZip); err != nil {
		t.Fatalf("CreateBackup should not fail when identity file is absent: %v", err)
	}

	r, err := zip.OpenReader(destZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == ShareIdentityFile {
			t.Errorf("%s unexpectedly present in backup ZIP for fresh install", ShareIdentityFile)
		}
	}
}

// TestBackupIncludesShareTables verifies that CreateBackup emits INSERT statements
// for shares, follows, and share_ring in the exported database.sql.
func TestBackupIncludesShareTables(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	db := newBackupTestDB(t)

	// Insert a tag so the FK reference in shares is valid.
	res, err := db.Exec(`INSERT INTO tags (name, color) VALUES ('test-tag', '#aabbcc')`)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	tagID, _ := res.LastInsertId()

	// Insert a share row referencing the tag.
	shareKey := []byte("fakesymkey32bytesXXXXXXXXXXXXXXX")
	shareID := []byte("fakeshareid16XXX")
	if _, err := db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, clips_sent, status, created_at) VALUES (?,?,?,0,0,'active',1000000)`,
		tagID, shareKey, shareID,
	); err != nil {
		t.Fatalf("insert share: %v", err)
	}

	// Insert a follows row.
	followKey := []byte("fakefollowkey32bXXXXXXXXXXXXXXXX")
	if _, err := db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, clips_received, created_at) VALUES ('peer1',?,?,0,0,1000001)`,
		followKey, tagID,
	); err != nil {
		t.Fatalf("insert follow: %v", err)
	}

	// Insert a share_ring row referencing shares(id)=1.
	envelope := []byte("fakeenvelope")
	if _, err := db.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (1, 1, 'clip', ?, 1000002)`,
		envelope,
	); err != nil {
		t.Fatalf("insert share_ring: %v", err)
	}

	app := &App{db: db}
	destZip := filepath.Join(t.TempDir(), "backup.zip")
	if err := app.CreateBackup(destZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	// Open ZIP and extract database.sql content.
	r, err := zip.OpenReader(destZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	var sqlContent string
	for _, f := range r.File {
		if f.Name == "database.sql" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open database.sql: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read database.sql: %v", err)
			}
			sqlContent = string(data)
			break
		}
	}

	if sqlContent == "" {
		t.Fatal("database.sql not found in backup ZIP")
	}

	for _, want := range []string{
		"INSERT INTO shares",
		"INSERT INTO follows",
		"INSERT INTO share_ring",
	} {
		if !strings.Contains(sqlContent, want) {
			t.Errorf("database.sql missing %q\n--- sql snippet ---\n%s\n---", want, sqlContent[:min(500, len(sqlContent))])
		}
	}
}

// TestRestoreKeepInvalidatesShares verifies that RestoreBackup with policy "keep"
// marks all restored shares rows as status='invalid'.
func TestRestoreKeepInvalidatesShares(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	// --- Build source app with a shares row, create backup ---
	srcDB := newBackupTestDB(t)
	srcRes, err := srcDB.Exec(`INSERT INTO tags (name, color) VALUES ('pub-tag', '#112233')`)
	if err != nil {
		t.Fatalf("insert tag in src: %v", err)
	}
	srcTagID, _ := srcRes.LastInsertId()
	if _, err := srcDB.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, clips_sent, status, created_at) VALUES (?,?,?,0,0,'active',2000000)`,
		srcTagID, []byte("srckey32bytesXXXXXXXXXXXXXXXXXXX"), []byte("srcshare16XXXXXX"),
	); err != nil {
		t.Fatalf("insert share in src: %v", err)
	}

	srcApp := &App{db: srcDB}
	backupZip := filepath.Join(t.TempDir(), "src.zip")
	if err := srcApp.CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	// --- Build destination app with a pre-existing identity file (triggers "keep") ---
	if err := os.WriteFile(filepath.Join(dataDir, ShareIdentityFile), []byte("local-identity"), 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	dstDB := newBackupTestDB(t)
	dstApp := &App{db: dstDB}

	if err := dstApp.RestoreBackup(backupZip, "keep"); err != nil {
		t.Fatalf("RestoreBackup(keep): %v", err)
	}

	// All restored shares rows must be status='invalid'.
	rows, err := dstDB.Query(`SELECT status FROM shares`)
	if err != nil {
		t.Fatalf("query shares after restore: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
		if status != "invalid" {
			t.Errorf("shares row status = %q, want %q", status, "invalid")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one shares row after restore, got none")
	}
}

// TestRestoreBackupUnknownPolicyErrors verifies that RestoreBackup returns an
// error immediately for an unrecognised identityPolicy without touching the DB.
func TestRestoreBackupUnknownPolicyErrors(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	// Build a valid backup to pass to RestoreBackup.
	srcDB := newBackupTestDB(t)
	srcApp := &App{db: srcDB}
	backupZip := filepath.Join(t.TempDir(), "src.zip")
	if err := srcApp.CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	// Seed the destination DB with a sentinel clip so we can verify it wasn't touched.
	dstDB := newBackupTestDB(t)
	if _, err := dstDB.Exec(
		`INSERT INTO clips (id, content_type, data, filename, created_at, is_archived) VALUES (999, 'text/plain', 'sentinel', 'sentinel.txt', 0, 0)`,
	); err != nil {
		t.Fatalf("insert sentinel: %v", err)
	}

	dstApp := &App{db: dstDB}
	err := dstApp.RestoreBackup(backupZip, "garbage")
	if err == nil {
		t.Fatal("expected error for unknown identity policy, got nil")
	}
	if !strings.Contains(err.Error(), "garbage") {
		t.Errorf("error message should mention the bad policy value; got: %v", err)
	}

	// Sentinel clip must still be present — DB was not modified.
	var count int
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE id = 999`).Scan(&count); err != nil {
		t.Fatalf("query sentinel: %v", err)
	}
	if count != 1 {
		t.Errorf("sentinel clip missing after rejected restore (count=%d)", count)
	}
}
