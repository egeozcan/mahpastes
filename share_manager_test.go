package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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

// openFollowerStream dials the publisher over libp2p, opens a stream, and
// writes a handshake. Returns the follower-side stream so the test can read
// replayed/live frames.
func openFollowerStream(t *testing.T, follower host.Host, pub peer.AddrInfo, symkey, shareID []byte, sinceSeq uint64) network.Stream {
	t.Helper()
	ctx := context.Background()
	if err := follower.Connect(ctx, pub); err != nil {
		t.Fatalf("connect: %v", err)
	}
	s, err := follower.NewStream(ctx, pub.ID, ShareProtocolID)
	if err != nil {
		t.Fatalf("new stream: %v", err)
	}
	hs := BuildHandshake(symkey, shareID, sinceSeq)
	if _, err := s.Write(hs); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	return s
}

// waitForFollowers polls until the publication's follower map reaches want.
// Returns true on success, false on timeout.
func (m *ShareManager) waitForFollowers(pubID int64, want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		pub, ok := m.publications[pubID]
		m.mu.RUnlock()
		if ok {
			pub.fmu.Lock()
			n := len(pub.followers)
			pub.fmu.Unlock()
			if n >= want {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestPublisherStreamReplaysRing(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, err := NewShareManager(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xAA}, 32)
	shareID := DeriveShareID(symkey)
	res, _ := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, shareID, time.Now().Unix())
	pubID, _ := res.LastInsertId()

	m.registerPublication(pubID, 1, shareID, symkey, "active")

	plain1, _ := MarshalPayload(ClipChunkPayload{Seq: 1, Kind: KindClipChunk, ClipID: 1, Index: 0, Data: []byte("hi")})
	env1, _ := EncryptEnvelope(symkey, shareID, 1, plain1)
	if err := RingInsert(db, pubID, 1, KindClipChunk, env1, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	plain2, _ := MarshalPayload(ClipChunkPayload{Seq: 2, Kind: KindClipChunk, ClipID: 1, Index: 1, Data: []byte("there")})
	env2, _ := EncryptEnvelope(symkey, shareID, 2, plain2)
	if err := RingInsert(db, pubID, 2, KindClipChunk, env2, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	follower, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	defer s.Close()

	s.SetReadDeadline(time.Now().Add(3 * time.Second))
	f1, err := ReadFrame(s)
	if err != nil {
		t.Fatalf("read1: %v", err)
	}
	pt1, err := DecryptEnvelope(symkey, shareID, 1, f1)
	if err != nil || !bytes.Contains(pt1, []byte("hi")) {
		t.Fatalf("decrypt1: %v %q", err, pt1)
	}
	f2, err := ReadFrame(s)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	pt2, err := DecryptEnvelope(symkey, shareID, 2, f2)
	if err != nil || !bytes.Contains(pt2, []byte("there")) {
		t.Fatalf("decrypt2: %v %q", err, pt2)
	}
}

func TestPublisherStreamRejectsWrongHMAC(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xBB}, 32)
	shareID := DeriveShareID(symkey)
	res, _ := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, shareID, time.Now().Unix())
	pubID, _ := res.LastInsertId()
	m.registerPublication(pubID, 1, shareID, symkey, "active")

	follower, _ := libp2p.New()
	defer follower.Close()
	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}

	// Send handshake with a different key so the HMAC won't verify against the
	// publisher's stored symkey.
	wrong := bytes.Repeat([]byte{0xCC}, 32)
	s := openFollowerStream(t, follower, pubInfo, wrong, shareID, 0)

	s.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := s.Read(buf); err == nil {
		t.Fatal("expected stream reset; got data")
	}
}

// TestPublisherStreamRejectsOverPublicationCap lowers the per-publication cap
// to 2, saturates it with real follower streams, then verifies a 3rd is
// rejected.
func TestPublisherStreamRejectsOverPublicationCap(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()
	m.setMaxStreamsPerPubForTest(2)

	symkey := bytes.Repeat([]byte{0xDE}, 32)
	shareID := DeriveShareID(symkey)
	res, _ := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, shareID, time.Now().Unix())
	pubID, _ := res.LastInsertId()
	m.registerPublication(pubID, 1, shareID, symkey, "active")

	// Use two separate follower hosts to avoid per-peer cap (MaxStreamsPerPeer=4,
	// so two streams from one peer is fine, but keeping hosts distinct makes the
	// intent explicit: saturate the per-publication cap independent of per-peer).
	follower1, _ := libp2p.New()
	defer follower1.Close()
	follower2, _ := libp2p.New()
	defer follower2.Close()
	follower3, _ := libp2p.New()
	defer follower3.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}

	s1 := openFollowerStream(t, follower1, pubInfo, symkey, shareID, 0)
	defer s1.Close()
	s2 := openFollowerStream(t, follower2, pubInfo, symkey, shareID, 0)
	defer s2.Close()

	if !m.waitForFollowers(pubID, 2, 3*time.Second) {
		t.Fatal("publication never reached 2 registered followers")
	}

	s3 := openFollowerStream(t, follower3, pubInfo, symkey, shareID, 0)
	defer s3.Close()
	s3.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := s3.Read(buf); err == nil {
		t.Fatal("expected stream reset after cap exceeded; got data")
	}
}

func TestStartShareInsertsRowAndRegisters(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	if info.ShareString == "" {
		t.Fatal("empty share string")
	}
	if info.Status != "active" {
		t.Fatalf("status %q", info.Status)
	}

	peerID, key, err := DecodeShareString(info.ShareString)
	if err != nil {
		t.Fatal(err)
	}
	if len(peerID) != 32 || len(key) != 32 {
		t.Fatalf("bad sizes: peerID=%d key=%d", len(peerID), len(key))
	}

	// One-publication-per-tag: second StartShare on the same tag must fail.
	if _, err := m.StartShare(1); err == nil {
		t.Fatal("expected duplicate StartShare to fail")
	}
}

func TestStopShareClosesAndDeletes(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.StopShare(info.TagID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shares WHERE id = ?`, info.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("shares row still present")
	}
}
