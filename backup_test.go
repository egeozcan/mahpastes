package main

import (
	"archive/zip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newBackupTestDB creates a minimal SQLite DB with all tables that exportDatabaseToSQL
// references, so CreateBackup can complete without a real mahpastes install.
func newBackupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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
