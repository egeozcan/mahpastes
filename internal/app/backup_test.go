package app

import (
	"archive/zip"
	"context"
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
	db := openBackupTestDB(t, filepath.Join(t.TempDir(), "test.db"))
	db.SetMaxOpenConns(1)
	return db
}

// openBackupTestDB opens dsn and creates the backup-relevant schema. Callers
// that need concurrency (a writer running against a reader's open snapshot)
// use this directly so they can supply WAL pragmas and leave the pool alone.
func openBackupTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	return openBackupTestDBWithDriver(t, "sqlite", dsn)
}

// openBackupTestDBWithDriver is openBackupTestDB over a named driver, for the
// tests that need commits to run through an instrumented one.
func openBackupTestDBWithDriver(t *testing.T, driverName, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dsn)
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
		`CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER, symkey BLOB, share_id BLOB UNIQUE, last_seq INTEGER, clips_sent INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at INTEGER)`,
		`CREATE TABLE follows (id INTEGER PRIMARY KEY, remote_peer_id TEXT, symkey BLOB, local_tag_id INTEGER, last_seq INTEGER DEFAULT 0, clips_received INTEGER NOT NULL DEFAULT 0, last_seen_at INTEGER, created_at INTEGER)`,
		`CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER, FOREIGN KEY(publication_id) REFERENCES shares(id) ON DELETE CASCADE)`,
		// Mirrors production: without this index a test asserting "no UNIQUE
		// conflict on the next seq" would pass even with the fix reverted.
		`CREATE UNIQUE INDEX idx_share_ring_pub_seq ON share_ring(publication_id, seq)`,
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

func TestRestoreBackupPromotesMarkdownClips(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	srcDB := newBackupTestDB(t)
	if _, err := srcDB.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('application/octet-stream', '# Restored', 'restored.md')`); err != nil {
		t.Fatalf("insert source clip: %v", err)
	}
	backupZip := filepath.Join(t.TempDir(), "markdown.zip")
	if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	dstDB := newBackupTestDB(t)
	if err := (&App{db: dstDB}).RestoreBackup(backupZip, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	var contentType string
	if err := dstDB.QueryRow(`SELECT content_type FROM clips WHERE filename = 'restored.md'`).Scan(&contentType); err != nil {
		t.Fatalf("query restored clip: %v", err)
	}
	if contentType != "text/markdown" {
		t.Fatalf("content type = %q, want text/markdown", contentType)
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

// seedSkewedShare inserts a tag, one active share with last_seq=lastSeq, and
// share_ring rows for seqs ringSeqs. Passing ring seqs above lastSeq reproduces
// the cross-table skew a non-snapshot export used to bake into a backup.
func seedSkewedShare(t *testing.T, db *sql.DB, tagName string, lastSeq int64, ringSeqs ...int64) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#445566')`, tagName)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	tagID, _ := res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, clips_sent, status, created_at) VALUES (?,?,?,?,0,'active',3000000)`,
		tagID, []byte("skewkey32bytesXXXXXXXXXXXXXXXXXX"), []byte("skewshare16X"+tagName), lastSeq,
	)
	if err != nil {
		t.Fatalf("insert share: %v", err)
	}
	pubID, _ := res.LastInsertId()

	for _, seq := range ringSeqs {
		if _, err := db.Exec(
			`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?,?,'clip_chunk',?,3000001)`,
			pubID, seq, []byte("env"),
		); err != nil {
			t.Fatalf("insert share_ring seq %d: %v", seq, err)
		}
	}
	return pubID
}

// TestNormalizeShareSeqsLiftsLastSeqAboveRing covers the repair itself: a
// publication whose ring outran its recorded last_seq must come out with
// last_seq at the ring's maximum, so the next emission's seq is free.
func TestNormalizeShareSeqsLiftsLastSeqAboveRing(t *testing.T) {
	db := newBackupTestDB(t)
	pubID := seedSkewedShare(t, db, "skewed-tag", 50, 51, 52, 53)

	// Sanity: the index that makes seq reuse fatal is actually present, so the
	// "seq 54 inserts cleanly" assertion below is not vacuous.
	if _, err := db.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?,53,'clip_chunk',?,3000002)`,
		pubID, []byte("dup"),
	); err == nil {
		t.Fatal("expected UNIQUE(publication_id, seq) conflict re-inserting seq 53, got none")
	}

	if err := normalizeShareSeqs(db); err != nil {
		t.Fatalf("normalizeShareSeqs: %v", err)
	}

	var lastSeq int64
	if err := db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, pubID).Scan(&lastSeq); err != nil {
		t.Fatalf("read last_seq: %v", err)
	}
	if lastSeq != 53 {
		t.Fatalf("last_seq after normalize = %d, want 53", lastSeq)
	}

	// The next emission allocates last_seq+1; it must not collide with the ring.
	if _, err := db.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?,?,'clip_start',?,3000003)`,
		pubID, lastSeq+1, []byte("next"),
	); err != nil {
		t.Fatalf("insert at seq %d after normalize: %v", lastSeq+1, err)
	}
}

// TestNormalizeShareSeqsLeavesConsistentRowsAlone verifies the repair never
// lowers last_seq: a publication ahead of its ring (the normal state after ring
// eviction) must be untouched, and one with an empty ring must keep its value.
func TestNormalizeShareSeqsLeavesConsistentRowsAlone(t *testing.T) {
	db := newBackupTestDB(t)
	aheadID := seedSkewedShare(t, db, "evicted-tag", 90, 88, 89)
	emptyID := seedSkewedShare(t, db, "empty-ring-tag", 7)

	if err := normalizeShareSeqs(db); err != nil {
		t.Fatalf("normalizeShareSeqs: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   int64
		want int64
	}{
		{"ring behind last_seq", aheadID, 90},
		{"empty ring", emptyID, 7},
	} {
		var got int64
		if err := db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, tc.id).Scan(&got); err != nil {
			t.Fatalf("%s: read last_seq: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: last_seq = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestExportTableToSQLUsesTxSnapshot proves the export's snapshot guarantee
// without goroutines: it interleaves a committed write between two table reads
// on the same transaction and asserts the second read does not see it.
//
// The DSN mirrors initDB's pragmas — WAL is what lets the writer commit while
// the reader's snapshot is open — and the pool is left at its default so the
// writer gets its own connection.
func TestExportTableToSQLUsesTxSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snapshot.db")
	dsn := dbPath + "?_pragma=busy_timeout%3D5000&_pragma=journal_mode%3Dwal&_pragma=foreign_keys%3Don"
	db := openBackupTestDB(t, dsn)

	pubID := seedSkewedShare(t, db, "snap-tag", 50, 50)

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer tx.Rollback()

	// First read pins the snapshot (BEGIN alone does not).
	var sharesOut strings.Builder
	if _, err := exportTableToSQL(tx, "shares", &sharesOut, nil); err != nil {
		t.Fatalf("export shares: %v", err)
	}
	if !strings.Contains(sharesOut.String(), "INSERT INTO shares") {
		t.Fatalf("shares export produced no rows:\n%s", sharesOut.String())
	}

	// Mimic an emission committing mid-export: a ring row plus the matching
	// last_seq bump, on a different connection.
	wtx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin write tx: %v", err)
	}
	if _, err := wtx.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?,51,'clip_start',?,3000004)`,
		pubID, []byte("mid-export"),
	); err != nil {
		t.Fatalf("insert ring row mid-export: %v", err)
	}
	if _, err := wtx.Exec(`UPDATE shares SET last_seq = 51 WHERE id = ?`, pubID); err != nil {
		t.Fatalf("bump last_seq mid-export: %v", err)
	}
	if err := wtx.Commit(); err != nil {
		t.Fatalf("commit write tx: %v", err)
	}

	// The write is durable outside the snapshot...
	var live int64
	if err := db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, pubID).Scan(&live); err != nil {
		t.Fatalf("read live last_seq: %v", err)
	}
	if live != 51 {
		t.Fatalf("live last_seq = %d, want 51 (write did not commit)", live)
	}

	// ...and invisible inside it, so the exported ring cannot outrun the
	// exported shares row.
	var ringOut strings.Builder
	count, err := exportTableToSQL(tx, "share_ring", &ringOut, nil)
	if err != nil {
		t.Fatalf("export share_ring: %v", err)
	}
	if count != 1 {
		t.Fatalf("share_ring export row count = %d, want 1 (snapshot leaked the concurrent write)\n%s", count, ringOut.String())
	}
	if strings.Contains(ringOut.String(), "mid-export") {
		t.Errorf("share_ring export contains the concurrently-inserted row:\n%s", ringOut.String())
	}
}

// TestRestoreNormalizesSkewedShareSeqs is the end-to-end guard: a backup taken
// from an already-skewed database (which is what old, non-snapshot exports
// produced) must restore into a database where the invariant holds.
func TestRestoreNormalizesSkewedShareSeqs(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)

	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	srcDB := newBackupTestDB(t)
	seedSkewedShare(t, srcDB, "skewed-pub", 50, 51, 52, 53)

	srcApp := &App{db: srcDB}
	backupZip := filepath.Join(t.TempDir(), "skewed.zip")
	if err := srcApp.CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	dstDB := newBackupTestDB(t)
	dstApp := &App{db: dstDB}
	if err := dstApp.RestoreBackup(backupZip, "takeover"); err != nil {
		t.Fatalf("RestoreBackup(takeover): %v", err)
	}

	var lastSeq, maxRing int64
	if err := dstDB.QueryRow(`SELECT last_seq FROM shares LIMIT 1`).Scan(&lastSeq); err != nil {
		t.Fatalf("read restored last_seq: %v", err)
	}
	if err := dstDB.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM share_ring`).Scan(&maxRing); err != nil {
		t.Fatalf("read restored max ring seq: %v", err)
	}
	if maxRing != 53 {
		t.Fatalf("restored max ring seq = %d, want 53 (ring rows were not restored)", maxRing)
	}
	if lastSeq < maxRing {
		t.Fatalf("restored last_seq = %d, below max ring seq %d: invariant still violated", lastSeq, maxRing)
	}
	if lastSeq != 53 {
		t.Errorf("restored last_seq = %d, want 53", lastSeq)
	}
}

// TestBackupExcludesPasswordPluginSettings verifies the generic plugin_storage
// filter: a storage key declared type = "password" in the plugin's manifest
// must not appear in the exported SQL and must be named in the excluded list.
// The plugin is present in the DB but NOT loaded in the manager, exercising the
// parse-manifest-from-disk fallback.
func TestBackupExcludesPasswordPluginSettings(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	pm, pluginsDir := newTestPluginManager(t, app)
	defer pm.Shutdown()

	// Manifest on disk declaring a password setting.
	manifestSrc := `Plugin = { name = "Secret Plugin", version = "1.0.0",
  settings = {
    {key = "api_token", type = "password", label = "Token"},
    {key = "server_url", type = "text", label = "Server URL"}
  } }`
	manifestPath := filepath.Join(pluginsDir, "secret-plugin.lua")
	if err := os.WriteFile(manifestPath, []byte(manifestSrc), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Register the plugin row without loading it into the manager.
	res, err := app.db.Exec(
		`INSERT INTO plugins (filename, name, version, enabled, status) VALUES ('secret-plugin.lua', 'Secret Plugin', '1.0.0', 0, 'enabled')`,
	)
	if err != nil {
		t.Fatalf("insert plugin: %v", err)
	}
	pluginID, _ := res.LastInsertId()

	for key, value := range map[string]string{
		"api_token":  "super-secret-token-value",
		"server_url": "http://localhost:8181",
	} {
		if _, err := app.db.Exec(
			`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (?, ?, ?)`,
			pluginID, key, value,
		); err != nil {
			t.Fatalf("insert plugin_storage %s: %v", key, err)
		}
	}

	sqlPath := filepath.Join(t.TempDir(), "export.sql")
	_, excluded, err := app.exportDatabaseToSQL(sqlPath)
	if err != nil {
		t.Fatalf("exportDatabaseToSQL: %v", err)
	}

	sqlBytes, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	exported := string(sqlBytes)

	if strings.Contains(exported, "super-secret-token-value") {
		t.Error("password-typed plugin setting leaked into the backup SQL")
	}
	if !strings.Contains(exported, "http://localhost:8181") {
		t.Error("non-password plugin setting must still be exported")
	}

	want := "Secret Plugin:api_token"
	found := false
	for _, e := range excluded {
		if e == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("excluded list should name %q, got %v", want, excluded)
	}
}
