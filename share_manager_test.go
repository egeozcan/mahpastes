package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	schema := `CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT UNIQUE, color TEXT DEFAULT '#888');
CREATE TABLE clips (id INTEGER PRIMARY KEY, content_type TEXT, data BLOB, filename TEXT, metadata TEXT DEFAULT '{}');
CREATE TABLE clip_tags (clip_id INTEGER, tag_id INTEGER, PRIMARY KEY(clip_id,tag_id));
CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER NOT NULL, symkey BLOB NOT NULL, share_id BLOB NOT NULL UNIQUE, last_seq INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at INTEGER NOT NULL);
CREATE UNIQUE INDEX idx_shares_tag_id ON shares(tag_id);
CREATE TABLE follows (id INTEGER PRIMARY KEY, remote_peer_id TEXT, symkey BLOB, local_tag_id INTEGER, last_seq INTEGER DEFAULT 0, last_seen_at INTEGER, created_at INTEGER);
CREATE INDEX idx_follows_peer ON follows(remote_peer_id);
CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER);
CREATE UNIQUE INDEX idx_share_ring_pub_seq ON share_ring(publication_id, seq);
CREATE INDEX idx_share_ring_ts ON share_ring(ts);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestShareManagerInitStop(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := newTestDB(t)

	m, err := NewShareManager(ctx, db, dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if m.Host() == nil {
		t.Fatal("host not initialized")
	}
	if m.Host().ID().String() == "" {
		t.Fatal("empty peer id")
	}
	// Identity file exists
	if _, err := filepath.Glob(filepath.Join(dir, ShareIdentityFile)); err != nil {
		t.Fatal(err)
	}
	m.Stop()
}
