package main

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	_ "modernc.org/sqlite"
)

// TestMain disables WAN bootstrap so unit tests do not hit the public IPFS
// bootstrap peers over the network. mDNS and direct peerstore seeding are
// sufficient for all in-process tests.
func TestMain(m *testing.M) {
	os.Setenv("MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP", "1")
	os.Exit(m.Run())
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// sqlite :memory: is per-connection. The sql pool may open new connections
	// on demand and each one is a fresh empty DB. Pin to 1 so every test
	// goroutine sees the same schema even under concurrent load (publisher
	// handler, emitter, consumer all hitting the same DB).
	db.SetMaxOpenConns(1)
	schema := `CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT UNIQUE, color TEXT DEFAULT '#888');
CREATE TABLE clips (id INTEGER PRIMARY KEY, content_type TEXT, data BLOB, filename TEXT, metadata TEXT DEFAULT '{}');
CREATE TABLE clip_tags (clip_id INTEGER, tag_id INTEGER, PRIMARY KEY(clip_id,tag_id));
CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER NOT NULL, symkey BLOB NOT NULL, share_id BLOB NOT NULL UNIQUE, last_seq INTEGER NOT NULL DEFAULT 0, clips_sent INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at INTEGER NOT NULL);
CREATE UNIQUE INDEX idx_shares_tag_id ON shares(tag_id);
CREATE TABLE follows (id INTEGER PRIMARY KEY, remote_peer_id TEXT, symkey BLOB, local_tag_id INTEGER, last_seq INTEGER DEFAULT 0, clips_received INTEGER NOT NULL DEFAULT 0, last_seen_at INTEGER, created_at INTEGER);
CREATE INDEX idx_follows_peer ON follows(remote_peer_id);
CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER, FOREIGN KEY(publication_id) REFERENCES shares(id) ON DELETE CASCADE);
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

func TestOnClipCreatedEmitsChunks(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}

	// 2.5 MiB clip → 3 chunks (ChunkSize = 1 MiB).
	data := bytes.Repeat([]byte{0x77}, int(2.5*float64(ChunkSize)))
	r, err := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('image/png', ?, 'big.png', '{}')`, data)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
		t.Fatal(err)
	}

	if err := m.OnClipCreated(clipID, []int64{1}); err != nil {
		t.Fatalf("OnClipCreated: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, info.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("ring count %d want 5 (start + 3 chunks + end)", count)
	}

	var lastSeq int64
	if err := db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, info.ID).Scan(&lastSeq); err != nil {
		t.Fatal(err)
	}
	if lastSeq != 5 {
		t.Fatalf("last_seq %d want 5", lastSeq)
	}

	var envBytes []byte
	var endSeq int64
	if err := db.QueryRow(`SELECT seq, envelope_bytes FROM share_ring WHERE publication_id = ? AND kind = ?`, info.ID, KindClipEnd).Scan(&endSeq, &envBytes); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	pub := m.publications[info.ID]
	m.mu.RUnlock()
	pt, err := DecryptEnvelope(pub.symkey, pub.shareID, uint64(endSeq), envBytes)
	if err != nil {
		t.Fatal(err)
	}
	var end ClipEndPayload
	if err := UnmarshalPayload(pt, &end); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if !bytes.Equal(end.SHA256, want[:]) {
		t.Fatal("clip_end sha256 mismatch")
	}
}

// blockingWriter never returns from Write — simulates a stalled follower so
// the send scheduler's byte cap trips.
type blockingWriter struct{}

func (b *blockingWriter) Write(p []byte) (int, error) {
	// Block forever. The runSender goroutine will sit here while enqueue
	// accumulates pending bytes and trips the cap.
	select {}
}

func TestSendSchedulerShedsOverCap(t *testing.T) {
	blocker := &blockingWriter{}
	fc := newFollowerConn(nil, blocker)

	closed := make(chan struct{}, 1)
	fc.onClose = func() {
		select {
		case closed <- struct{}{}:
		default:
		}
	}

	// 33 × 1 MiB envelopes — 32 MiB cap should trip on or before the 33rd.
	data := bytes.Repeat([]byte{1}, 1<<20)
	for i := 0; i < 33; i++ {
		fc.enqueue(data)
	}

	select {
	case <-closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected connection shed after 32 MiB queued")
	}
}

func TestOnClipCreatedSkipsNonMatchingTag(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa'), (2, 'y', '#bbb')`); err != nil {
		t.Fatal(err)
	}
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatal(err)
	}

	r, err := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', 'hi', 'a.txt', '{}')`)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 2)`, clipID); err != nil {
		t.Fatal(err)
	}

	if err := m.OnClipCreated(clipID, []int64{2}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, info.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ring count %d want 0 (tag mismatch)", count)
	}
}

func TestFollowAndReceiveClip(t *testing.T) {
	ctx := context.Background()

	// Publisher
	pubDB := newTestDB(t)
	pubDir := t.TempDir()
	pubM, _ := NewShareManager(ctx, pubDB, pubDir)
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`)
	info, _ := pubM.StartShare(1)

	// Follower
	fDB := newTestDB(t)
	fDir := t.TempDir()
	fM, _ := NewShareManager(ctx, fDB, fDir)
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`)

	// Prime the follower's peerstore with publisher's addrs (skip DHT resolution for test speed).
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	followInfo, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if followInfo.LocalTagName != "inbox" {
		t.Fatalf("local tag %q", followInfo.LocalTagName)
	}

	// Publish a clip on the publisher side
	r, _ := pubDB.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', 'hello!', 'a.txt', '{}')`)
	clipID, _ := r.LastInsertId()
	pubDB.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID)
	pubM.OnClipCreated(clipID, []int64{1})

	// Wait for follower to receive and persist
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		fDB.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n)
		if n >= 1 {
			var data []byte
			fDB.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&data)
			if string(data) == "hello!" {
				// Check local tag association
				var tagID int64
				fDB.QueryRow(`SELECT tag_id FROM clip_tags ORDER BY clip_id DESC LIMIT 1`).Scan(&tagID)
				if tagID == 99 {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("follower did not persist clip within 5s")
}

// Lock the "session ends → status offline" invariant that DisconnectFollowForTest
// and the offline-catchup e2e both depend on.
func TestFollowSessionEndFlipsStatusOffline(t *testing.T) {
	ctx := context.Background()

	// Publisher
	pubDB := newTestDB(t)
	pubM, _ := NewShareManager(ctx, pubDB, t.TempDir())
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	info, _ := pubM.StartShare(1)

	// Follower
	fDB := newTestDB(t)
	fM, _ := NewShareManager(ctx, fDB, t.TempDir())
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`)
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	followInfo, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatal(err)
	}

	// Read status under f.mu to avoid a benign data race with
	// setFollowStatus. The actual correctness property the test verifies
	// is the flip to "offline" — that invariant is unchanged.
	readStatus := func(id int64) string {
		fM.mu.RLock()
		f := fM.follows[id]
		fM.mu.RUnlock()
		if f == nil {
			return ""
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.status
	}

	// Wait for connected.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if readStatus(followInfo.ID) == "connected" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Kill the active session without deleting the row.
	if err := fM.DisconnectFollowForTest(followInfo.ID); err != nil {
		t.Fatal(err)
	}

	// Assert the status flips to offline before the next reconnect.
	// This is the invariant the e2e test relies on: a mid-stream session
	// cancel must be observable in GetShareStatus / card renders.
	sawOffline := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if readStatus(followInfo.ID) == "offline" {
			sawOffline = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawOffline {
		t.Fatal("expected status to flip to 'offline' after DisconnectFollowForTest")
	}
}

func TestResumeAllReplaysTables(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()

	// Generate a real libp2p peer ID for the pre-seeded follow row so
	// peer.Decode in ResumeAll accepts it. (Garbage peer IDs are
	// intentionally skipped by ResumeAll — see log in share_manager.go.)
	_, pub, _ := libp2pCrypto.GenerateEd25519Key(cryptoRand.Reader)
	fakePeerID, _ := peer.IDFromPublicKey(pub)

	// Pre-seed a publication and a follow row directly.
	symkey := bytes.Repeat([]byte{0xAA}, 32)
	sid := DeriveShareID(symkey)
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#888'), (2, 'inbox', '#888')`)
	db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, sid, time.Now().Unix())
	db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (?, ?, ?, 0, 'invalid', ?)`, int64(99), bytes.Repeat([]byte{0xBB}, 32), DeriveShareID(bytes.Repeat([]byte{0xBB}, 32)), time.Now().Unix())
	db.Exec(`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, last_seen_at, created_at) VALUES (?, ?, 2, 0, 0, ?)`, fakePeerID.String(), symkey, time.Now().Unix())

	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()
	if err := m.ResumeAll(); err != nil {
		t.Fatalf("ResumeAll: %v", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.publications) != 2 {
		t.Fatalf("publications %d want 2", len(m.publications))
	}
	// Only one should be 'active' with a handler (the other is 'invalid')
	actives := 0
	for _, p := range m.publications {
		if p.status == "active" {
			actives++
		}
	}
	if actives != 1 {
		t.Fatalf("actives %d want 1", actives)
	}
	if len(m.follows) != 1 {
		t.Fatalf("follows %d want 1", len(m.follows))
	}
}

// resolveOrCreateTag must reject malformed names (empty segments, reserved
// _api) the same way App.CreateTag does — otherwise follow flows quietly mint
// rows like "incoming/" with empty leaf segments.
func TestResolveOrCreateTagRejectsEmptyPathSegment(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m, _ := NewShareManager(ctx, db, t.TempDir())
	defer m.Stop()

	bad := []string{"incoming/", "/incoming", "a//b", "  /  ", "_api", "a/_api/b"}
	for _, name := range bad {
		if _, err := m.resolveOrCreateTag(name); err == nil {
			t.Errorf("resolveOrCreateTag(%q) accepted a malformed name", name)
		}
	}
	// Sanity: a clean name still works.
	if _, err := m.resolveOrCreateTag("ok/path"); err != nil {
		t.Fatalf("resolveOrCreateTag(\"ok/path\"): %v", err)
	}
}

// TestFollowConnection rejects self-follow with a typed sentinel error so the
// UI can distinguish it from an unreachable peer (different error surface,
// different remediation).
func TestTestFollowConnectionRejectsSelfFollow(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m, err := NewShareManager(ctx, db, t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer m.Stop()
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	err = m.TestFollowConnection(info.ShareString)
	if !errors.Is(err, ErrSelfFollow) {
		t.Fatalf("TestFollowConnection: got %v; want errors.Is(ErrSelfFollow)", err)
	}
}

// UpdateFollowTag must swap both the DB row's local_tag_id and the in-memory
// follow's localTagID. Resolving a brand-new tag name should auto-create it.
func TestUpdateFollowTagSwapsLocalTagID(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, _ := NewShareManager(ctx, pubDB, t.TempDir())
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'src', '#aaa')`)
	info, _ := pubM.StartShare(1)

	fDB := newTestDB(t)
	fM, _ := NewShareManager(ctx, fDB, t.TempDir())
	defer fM.Stop()
	fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (50, 'inbox-old', '#aaa')`)
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)

	fi, err := fM.Follow(info.ShareString, "inbox-old")
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	// Switch to a fresh tag name that doesn't exist yet — UpdateFollowTag
	// must create it.
	updated, err := fM.UpdateFollowTag(fi.ID, "inbox-new/from-alice")
	if err != nil {
		t.Fatalf("UpdateFollowTag: %v", err)
	}
	if updated.LocalTagName != "inbox-new/from-alice" {
		t.Fatalf("local tag name %q want inbox-new/from-alice", updated.LocalTagName)
	}
	// New tag row exists.
	var newID int64
	if err := fDB.QueryRow(`SELECT id FROM tags WHERE name = 'inbox-new/from-alice'`).Scan(&newID); err != nil {
		t.Fatalf("new tag not created: %v", err)
	}
	if updated.LocalTagID != newID {
		t.Fatalf("LocalTagID %d want %d", updated.LocalTagID, newID)
	}
	// DB row points at the new tag.
	var dbTagID int64
	fDB.QueryRow(`SELECT local_tag_id FROM follows WHERE id = ?`, fi.ID).Scan(&dbTagID)
	if dbTagID != newID {
		t.Fatalf("DB local_tag_id %d want %d", dbTagID, newID)
	}
	// In-memory follow updated.
	fM.mu.RLock()
	memTag := fM.follows[fi.ID].localTagID
	fM.mu.RUnlock()
	if memTag != newID {
		t.Fatalf("in-memory localTagID %d want %d", memTag, newID)
	}
}

// FollowWithoutDial must persist a follows row even when the peer is
// unreachable — that's the whole point of the "Follow anyway" path. The
// reconnect loop takes over asynchronously; we only verify the row exists and
// the in-memory follow map is populated.
func TestFollowWithoutDialInsertsRowWithoutPeerContact(t *testing.T) {
	ctx := context.Background()

	// Publisher lives long enough to emit a share string, then goes away.
	pubDB := newTestDB(t)
	pubM, _ := NewShareManager(ctx, pubDB, t.TempDir())
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	info, _ := pubM.StartShare(1)
	pubM.Stop() // kill the publisher — follower cannot dial

	// Follower does NOT get the publisher's addrs in its peerstore: any dial
	// would fail. FollowWithoutDial must succeed anyway.
	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatalf("follower NewShareManager: %v", err)
	}
	defer fM.Stop()

	fi, err := fM.FollowWithoutDial(info.ShareString, "inbox-offline")
	if err != nil {
		t.Fatalf("FollowWithoutDial: %v", err)
	}
	if fi.LocalTagName != "inbox-offline" {
		t.Fatalf("local tag %q want inbox-offline", fi.LocalTagName)
	}
	// DB row
	var n int
	fDB.QueryRow(`SELECT COUNT(*) FROM follows WHERE id = ?`, fi.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("follows rows %d want 1", n)
	}
	// In-memory follow registered (reconnect loop is running).
	fM.mu.RLock()
	_, ok := fM.follows[fi.ID]
	fM.mu.RUnlock()
	if !ok {
		t.Fatalf("follow %d not registered in memory", fi.ID)
	}
}

func TestStopShare_NoEventWhenNothingActive(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, err := NewShareManager(ctx, db, dir)
	if err != nil {
		t.Fatalf("NewShareManager: %v", err)
	}
	defer m.Stop()

	var fired bool
	m.eventFn = func(name string, data ...any) {
		if name == "share:publication-removed" {
			fired = true
		}
	}

	if err := m.StopShare(999); err != nil {
		t.Fatalf("StopShare: %v", err)
	}
	if fired {
		t.Fatalf("share:publication-removed should not fire when nothing was active")
	}
}
