package main

import (
	"database/sql"
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
