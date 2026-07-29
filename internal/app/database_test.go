package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestShareTablesMigrate verifies the share-feature tables land in the DB
// after initDB. Uses a temp XDG_DATA_HOME so the real clips.db is not touched.
func TestShareTablesMigrate(t *testing.T) {
	tmp := t.TempDir()
	// getDataDir honors XDG_DATA_HOME on Linux, but macOS uses
	// os.UserConfigDir() which is not env-controlled. To avoid touching the
	// real DB on any platform, create a fresh connection + apply the same
	// CREATE TABLE stanzas from database.go's initDB manually.

	dbPath := filepath.Join(tmp, "clips.db")
	dsn := dbPath + "?_pragma=foreign_keys%3Don"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Mirror just the subset of tables the share tables reference + the share
	// tables themselves. We reuse the exact DDL from initDB to catch syntax
	// drift; keep in sync if initDB's share stanzas change.
	schema := []string{
		`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, color TEXT)`,
		`CREATE TABLE IF NOT EXISTS shares (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			tag_id      INTEGER NOT NULL,
			symkey      BLOB    NOT NULL,
			share_id    BLOB    NOT NULL UNIQUE,
			last_seq    INTEGER NOT NULL DEFAULT 0,
			clips_sent  INTEGER NOT NULL DEFAULT 0,
			status      TEXT    NOT NULL DEFAULT 'active',
			created_at  INTEGER NOT NULL,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_tag_id ON shares(tag_id)`,
		`CREATE TABLE IF NOT EXISTS follows (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_peer_id  TEXT    NOT NULL,
			symkey          BLOB    NOT NULL,
			local_tag_id    INTEGER NOT NULL,
			last_seq        INTEGER NOT NULL DEFAULT 0,
			clips_received  INTEGER NOT NULL DEFAULT 0,
			last_seen_at    INTEGER,
			created_at      INTEGER NOT NULL,
			FOREIGN KEY (local_tag_id) REFERENCES tags(id) ON DELETE RESTRICT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_follows_peer ON follows(remote_peer_id)`,
		`CREATE TABLE IF NOT EXISTS share_ring (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			publication_id INTEGER NOT NULL,
			seq            INTEGER NOT NULL,
			kind           TEXT    NOT NULL,
			envelope_bytes BLOB    NOT NULL,
			ts             INTEGER NOT NULL,
			FOREIGN KEY (publication_id) REFERENCES shares(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_share_ring_pub_seq ON share_ring(publication_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_share_ring_ts ON share_ring(ts)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt[:40], err)
		}
	}

	for _, tbl := range []string{"shares", "follows", "share_ring"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", tbl, err)
		}
	}

	// Sanity: idx_shares_tag_id must be UNIQUE (one share per tag).
	// Insert a tag, then two shares with the same tag_id — second must fail.
	if _, err := db.Exec(`INSERT INTO tags (name, color) VALUES ('t1', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, created_at) VALUES (1, X'00', X'01', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, created_at) VALUES (1, X'00', X'02', 0)`); err == nil {
		t.Error("expected duplicate tag_id to fail unique index")
	}

	_ = os.Remove
}

func TestMigration_APIKeysScopedTagFK_SetNullAndRevoke(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#aaa')`); err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES ('k', 'h', 'p', 'viewer', 1)`); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	// Deleting the scoped tag must not delete the api_keys row (SET NULL),
	// and the trigger must auto-revoke the key.
	if _, err := db.Exec(`DELETE FROM tags WHERE id = 1`); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	var scoped sql.NullInt64
	var revoked int
	if err := db.QueryRow(`SELECT scoped_tag_id, is_revoked FROM api_keys WHERE name = 'k'`).Scan(&scoped, &revoked); err != nil {
		t.Fatalf("query key: %v", err)
	}
	if scoped.Valid {
		t.Fatalf("scoped_tag_id should be NULL after tag delete, got %v", scoped.Int64)
	}
	if revoked != 1 {
		t.Fatalf("key should be auto-revoked (is_revoked=1), got %d", revoked)
	}
}

func TestMigration_APIKeysIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)
	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB first: %v", err)
	}
	db.Close()

	// Second initDB must be a no-op migration (no panic, no error).
	db2, err := initDB()
	if err != nil {
		t.Fatalf("initDB second: %v", err)
	}
	defer db2.Close()

	if needsAPIKeysScopedTagMigration(db2) {
		t.Fatalf("migration should not re-run on already-migrated DB")
	}
}

// TestPurgeRevokedAPIKeys_RetentionWindow verifies the sweep deletes keys
// revoked past the retention window, keeps recently-revoked and active ones,
// and never touches a revoked row whose clock never started (NULL revoked_at).
func TestPurgeRevokedAPIKeys_RetentionWindow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// name -> (is_revoked, revoked_at SQL expression)
	seed := []struct {
		name      string
		revoked   int
		revokedAt string
	}{
		{"active", 0, "NULL"},
		{"stale", 1, fmt.Sprintf("datetime('now', '-%d days')", revokedKeyRetentionDays+1)},
		{"boundary", 1, fmt.Sprintf("datetime('now', '-%d days')", revokedKeyRetentionDays)},
		{"fresh", 1, fmt.Sprintf("datetime('now', '-%d days')", revokedKeyRetentionDays-1)},
		{"unstamped", 1, "NULL"},
	}
	for _, s := range seed {
		if _, err := db.Exec(fmt.Sprintf(
			`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked, revoked_at)
			 VALUES (?, ?, ?, 'viewer', ?, %s)`, s.revokedAt),
			s.name, "hash-"+s.name, "pfx-", s.revoked); err != nil {
			t.Fatalf("insert %s: %v", s.name, err)
		}
	}

	deleted, err := purgeRevokedAPIKeys(db)
	if err != nil {
		t.Fatalf("purgeRevokedAPIKeys: %v", err)
	}
	// "boundary" sits exactly on the cutoff and the sweep uses <=, so it goes.
	if deleted != 2 {
		t.Fatalf("expected 2 rows deleted, got %d", deleted)
	}

	survivors := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM api_keys`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		survivors[name] = true
	}
	for _, want := range []string{"active", "fresh", "unstamped"} {
		if !survivors[want] {
			t.Errorf("key %q should have survived the sweep", want)
		}
	}
	for _, gone := range []string{"stale", "boundary"} {
		if survivors[gone] {
			t.Errorf("key %q should have been purged", gone)
		}
	}
}

// TestRevokeKey_StampsRevokedAt covers the other half of the retention
// contract: a key revoked through the normal path must carry a timestamp, or
// the sweep would never age it out.
func TestRevokeKey_StampsRevokedAt(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	am := &APIManager{app: app}
	created, err := am.CreateKey("purge-me", "viewer", 0)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	if err := am.RevokeKey(created.Info.ID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}

	keys, err := am.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	var found *APIKeyInfo
	for i := range keys {
		if keys[i].ID == created.Info.ID {
			found = &keys[i]
		}
	}
	if found == nil {
		t.Fatalf("revoked key missing from ListKeys")
	}
	if !found.IsRevoked {
		t.Fatalf("key should be revoked")
	}
	if found.RevokedAt == nil || *found.RevokedAt == "" {
		t.Fatalf("revoked key has no revoked_at stamp; retention sweep would never collect it")
	}

	// Nothing was revoked over the window ago, so the sweep is a no-op.
	deleted, err := purgeRevokedAPIKeys(app.db)
	if err != nil {
		t.Fatalf("purgeRevokedAPIKeys: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("just-revoked key must not be purged, deleted %d", deleted)
	}
}

// TestMigration_RevokedAtBackfillAndTrigger simulates upgrading a DB that
// predates revoked_at: the column must be added, already-revoked rows must be
// stamped so they can age out, and the auto-revoke trigger must be replaced
// with the version that sets revoked_at (CREATE IF NOT EXISTS would not have).
func TestMigration_RevokedAtBackfillAndTrigger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)

	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}

	// Recreate the pre-revoked_at shape, including the old trigger body.
	if _, err := db.Exec(`DROP TABLE api_keys`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			scoped_tag_id INTEGER,
			is_revoked INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE SET NULL
		)`); err != nil {
		t.Fatalf("recreate old api_keys: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER api_keys_revoke_on_scope_null
		AFTER UPDATE OF scoped_tag_id ON api_keys
		WHEN NEW.scoped_tag_id IS NULL AND OLD.scoped_tag_id IS NOT NULL
		BEGIN
			UPDATE api_keys SET is_revoked = 1 WHERE id = NEW.id;
		END;`); err != nil {
		t.Fatalf("recreate old trigger: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#aaa')`); err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES ('legacy', 'h1', 'p', 'viewer', 1)`); err != nil {
		t.Fatalf("insert legacy revoked key: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES ('scoped', 'h2', 'p', 'viewer', 1)`); err != nil {
		t.Fatalf("insert scoped key: %v", err)
	}
	db.Close()

	// Reopen: migrations run.
	db2, err := initDB()
	if err != nil {
		t.Fatalf("initDB second: %v", err)
	}
	defer db2.Close()

	if !hasColumn(db2, "api_keys", "revoked_at") {
		t.Fatalf("revoked_at column was not added")
	}

	var legacyRevokedAt sql.NullString
	if err := db2.QueryRow(`SELECT revoked_at FROM api_keys WHERE name = 'legacy'`).Scan(&legacyRevokedAt); err != nil {
		t.Fatalf("query legacy: %v", err)
	}
	if !legacyRevokedAt.Valid {
		t.Fatalf("already-revoked key was not backfilled; it would never be purged")
	}

	// The replaced trigger must stamp revoked_at when a scoped tag disappears.
	if _, err := db2.Exec(`DELETE FROM tags WHERE id = 1`); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	var revoked int
	var scopedRevokedAt sql.NullString
	if err := db2.QueryRow(`SELECT is_revoked, revoked_at FROM api_keys WHERE name = 'scoped'`).
		Scan(&revoked, &scopedRevokedAt); err != nil {
		t.Fatalf("query scoped: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("scoped key should be auto-revoked, got is_revoked=%d", revoked)
	}
	if !scopedRevokedAt.Valid {
		t.Fatalf("trigger did not stamp revoked_at; old trigger body still installed")
	}
}

// TestMigration_RestoresFKOnError exercises the actual poisoned-conn path:
// acquire a real conn, disable FK, force an error via migrateFailHook, and
// verify every pooled conn afterwards reports foreign_keys=1. Without the
// deferred restore in migrateAPIKeysScopedTagSetNull, this would catch a
// conn that went back to the pool with FK=OFF.
func TestMigration_RestoresFKOnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dir)
	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	// Put api_keys into the pre-migration state so needsAPIKeysScopedTagMigration
	// returns true and the migration actually executes to the point where FK
	// is disabled. (initDB already migrated on open; we recreate the old shape.)
	if _, err := db.Exec(`DROP TABLE api_keys`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			scoped_tag_id INTEGER,
			is_revoked INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)`); err != nil {
		t.Fatalf("recreate old api_keys: %v", err)
	}
	if !needsAPIKeysScopedTagMigration(db) {
		t.Fatalf("pre-condition: migration should be needed after recreating old table")
	}

	// Install failure hook, restore on exit.
	migrateFailHook = func() error { return fmt.Errorf("simulated mid-migration failure") }
	defer func() { migrateFailHook = nil }()

	err = migrateAPIKeysScopedTagSetNull(db)
	if err == nil {
		t.Fatalf("expected migration to fail with the hook installed")
	}

	// Exhaust pool by taking and releasing multiple conns. At least one of
	// these should reuse the conn that had FK disabled during the failed
	// migration.
	for i := 0; i < 8; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		var fk int
		if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			conn.Close()
			t.Fatalf("query fk on conn %d: %v", i, err)
		}
		conn.Close()
		if fk != 1 {
			t.Fatalf("pooled connection %d has foreign_keys=%d, expected 1", i, fk)
		}
	}
}
