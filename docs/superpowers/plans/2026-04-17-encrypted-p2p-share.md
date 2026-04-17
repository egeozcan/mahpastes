# Encrypted P2P Tag Sharing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the encrypted P2P tag-sharing feature specified in `docs/superpowers/specs/2026-04-17-encrypted-p2p-share-design.md` — libp2p-go transport, DHT discovery, chunked envelopes, on-disk ciphertext ring, share view UI.

**Architecture:** New `ShareManager` (in-process registry for publications/follows, owns one libp2p host) + `ShareService` Wails binding + pure-function `share_codec` / `share_protocol` / `share_identity` helpers. Persistence via three new SQLite tables (`shares`, `follows`, `share_ring`) and one identity file (`share_identity.key`). Frontend: 2×2 drawer grid with new Share tab; two-section Share view; Create/Follow modals.

**Tech Stack:** Go 1.24 + libp2p-go (host, Kademlia DHT, AutoRelay, DCUtR, Noise, Yamux), `modernc.org/sqlite`, `github.com/fxamacker/cbor/v2` for CBOR, `crypto/aes` + `crypto/cipher` GCM, `crypto/hmac`, `crypto/sha256`. Frontend: vanilla JS + Tailwind, plus one QR library (`qrcode` npm, bundled as single file under `frontend/js/vendor/qrcode.min.js`).

---

## File Structure

**New Go files (repo root):**
- `share_codec.go` / `share_codec_test.go` — encode/decode share strings, derive `share_id` from key
- `share_identity.go` / `share_identity_test.go` — load/generate/persist libp2p Ed25519 keypair
- `share_protocol.go` / `share_protocol_test.go` — envelope framing, GCM encrypt/decrypt, handshake, chunk assembly
- `share_ring.go` / `share_ring_test.go` — `share_ring` table ops (insert, retransmit query, eviction DML)
- `share_manager.go` / `share_manager_test.go` — lifecycle, libp2p host setup, publications/follows registries, emission, fan-out
- `share_service.go` — Wails-bound thin facade (StartShare, StopShare, Follow, Unfollow, GetShareStatus)
- `share_types.go` — shared Go types (ShareInfo, FollowInfo, envelope kinds)

**Modified Go files:**
- `main.go` — register `ShareService` in `Bind:` slice
- `app.go` — `ShareManager` field on App; init in `startup`; stop in `shutdown`; `OnClipCreated` hook after every clip insert; `StopShare` call in `DeleteTag`
- `database.go` — three new `CREATE TABLE` stanzas
- `backup.go` — include/restore `share_identity.key`; restore prompt integration (new Wails method)

**New frontend files:**
- `frontend/js/share.js` — Share view renderer, modals, parser, event listeners
- `frontend/js/vendor/qrcode.min.js` — bundled QR library (vendored, not npm-build)

**Modified frontend files:**
- `frontend/index.html` — change drawer tablist to 2×2 grid, add Share tab, add `<section id="share-view">`, add Create-Share and Follow-Share modal markup
- `frontend/js/app.js` — register Share tab in view switcher
- `frontend/js/utils.js` — small helpers if missing (e.g., relative-time formatter — check first)

**New E2E tests:** `e2e/tests/share/` with 12 spec files (see Phase 14).

**Dependencies:**
- Add: `github.com/libp2p/go-libp2p@latest`, `github.com/libp2p/go-libp2p-kad-dht@latest`, `github.com/fxamacker/cbor/v2@latest`
- Add dev: `qrcode` as a vendored single file (not npm-installed — mahpastes has no JS build step besides Tailwind)

---

## Execution guidance

- **TDD is mandatory.** Every behavior gets a failing test first.
- **Each commit is green.** Never commit red tests unless the step is explicitly "run test to verify it fails" and you don't commit until the implementation step passes.
- **Pattern references:** `serve_manager.go` + `serve_service.go` are the closest existing analogues. Follow their structure when in doubt.
- **Bindings regeneration:** after any change to `App` or `ShareService` exported methods, run `make bindings`. Commit the regenerated `frontend/wailsjs/` files separately.
- **E2E baseline:** per CLAUDE.md, run `cd e2e && npm test 2>&1 | tail -50` before Phase 1 and before every Phase boundary. Fix anything broken before proceeding.
- **Commit messages:** Conventional commits (`feat:`, `fix:`, `test:`, `refactor:`, `docs:`). Scope via phase: `feat(share):`.

---

## Phase 0 — Baseline, dependencies, scaffolding

### Task 0.1: Verify baseline tests pass

- [ ] **Step 1: Run existing e2e suite.**

Run: `cd e2e && npm test 2>&1 | tail -50`
Expected: all green, or a known-failing set you capture in notes. If anything fails, **fix it first** before touching share code (CLAUDE.md rule).

- [ ] **Step 2: Run Go tests.**

Run: `go test ./... 2>&1 | tail -30`
Expected: all green.

- [ ] **Step 3: Verify the app builds.**

Run: `make build 2>&1 | tail -20`
Expected: clean build.

### Task 0.2: Add Go dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add libp2p + CBOR.**

Run:
```bash
go get github.com/libp2p/go-libp2p@v0.38.0
go get github.com/libp2p/go-libp2p-kad-dht@v0.29.0
go get github.com/fxamacker/cbor/v2@v2.7.0
go mod tidy
```

If newer minor versions are available at the time of implementation, pin to the current stable; do not use `@latest`.

- [ ] **Step 2: Verify compile.**

Run: `go build ./...`
Expected: builds successfully (no code uses the new deps yet).

- [ ] **Step 3: Commit.**

```bash
git add go.mod go.sum
git commit -m "feat(share): add libp2p-go and CBOR dependencies"
```

### Task 0.3: Create share_types.go skeleton

**Files:**
- Create: `share_types.go`

- [ ] **Step 1: Write the skeleton.**

```go
package main

import "time"

// Envelope kinds (wire-level plaintext discriminators).
const (
	KindClipStart = "clip_start"
	KindClipChunk = "clip_chunk"
	KindClipEnd   = "clip_end"
	KindGap       = "gap"
)

// Protocol constants.
const (
	ShareProtocolID     = "/mahpastes/share/1.0.0"
	ShareStringPrefix   = "mp-share:v1:"
	ShareIdentityFile   = "share_identity.key"
	ShareStagingDirName = "share-staging"

	// Handshake
	HandshakeHMACContext = "mp-share-v1-follow"

	// Size limits
	ChunkSize      = 1 << 20   // 1 MiB plaintext per chunk
	MaxEnvelopeLen = ChunkSize + 64 // length-prefixed ciphertext cap

	// Ring retention
	RingTTLSeconds      = 3600              // 1h
	RingBytesCapPerPub  = 256 * (1 << 20)   // 256 MiB
	RingSweepInterval   = 15 * time.Minute
	StagingSweepInterval = 6 * time.Hour
	StagingMaxAge       = 24 * time.Hour

	// Per-follower send scheduler
	SendQueueBytesCap     = 32 * (1 << 20) // 32 MiB
	SendQueueEnvelopesCap = 256

	// Rate limits
	MaxStreamsPerPublication = 128
	MaxStreamsPerPeer        = 4
	HandshakeTimeout         = 5 * time.Second
	ReconnectFloor           = time.Second
	ReconnectCap             = 30 * time.Second
)

// ShareInfo — one entry in the publisher-side Sharing list (frontend DTO).
type ShareInfo struct {
	ID         int64  `json:"id"`
	TagID      int64  `json:"tag_id"`
	TagName    string `json:"tag_name"`
	ShareString string `json:"share_string"`
	Status     string `json:"status"`       // "active" | "invalid"
	Followers  int    `json:"followers"`
	ClipsPushed int64 `json:"clips_pushed"` // derived from last_seq / chunks_per_clip estimate
	CreatedAt  int64  `json:"created_at"`
}

// FollowInfo — one entry in the Following list (frontend DTO).
type FollowInfo struct {
	ID           int64  `json:"id"`
	RemotePeerID string `json:"remote_peer_id"`
	LocalTagID   int64  `json:"local_tag_id"`
	LocalTagName string `json:"local_tag_name"`
	Status       string `json:"status"`     // "connected" | "connected_relayed" | "offline"
	ClipsReceived int64 `json:"clips_received"`
	LastSeq      int64  `json:"last_seq"`
	LastSeenAt   *int64 `json:"last_seen_at"`
	CreatedAt    int64  `json:"created_at"`
}
```

- [ ] **Step 2: Compile.**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit.**

```bash
git add share_types.go
git commit -m "feat(share): add share_types.go with protocol constants and DTOs"
```

---

## Phase 1 — Share codec (pure functions, TDD greenfield)

### Task 1.1: Encode/decode share strings

**Files:**
- Create: `share_codec.go`
- Test: `share_codec_test.go`

- [ ] **Step 1: Write failing tests.**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	peerID := bytes.Repeat([]byte{0xAB}, 32)
	key := bytes.Repeat([]byte{0xCD}, 32)

	s, err := EncodeShareString(peerID, key)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if !strings.HasPrefix(s, ShareStringPrefix) {
		t.Fatalf("prefix mismatch: %q", s)
	}
	if len(s) > 160 {
		t.Fatalf("encoded length %d exceeds budget (≤160)", len(s))
	}

	gotPeerID, gotKey, err := DecodeShareString(s)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(gotPeerID, peerID) {
		t.Fatalf("peer_id mismatch")
	}
	if !bytes.Equal(gotKey, key) {
		t.Fatalf("key mismatch")
	}
}

func TestDecodeRejectsMissingPrefix(t *testing.T) {
	if _, _, err := DecodeShareString("abcdef"); err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

func TestDecodeRejectsWrongVersion(t *testing.T) {
	if _, _, err := DecodeShareString("mp-share:v99:xxxxx"); err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestDecodeRejectsMalformedBase64(t *testing.T) {
	if _, _, err := DecodeShareString("mp-share:v1:!!!not-base64!!!"); err == nil {
		t.Fatal("expected error for malformed base64")
	}
}

func TestDecodeRejectsMalformedCBOR(t *testing.T) {
	// Valid base64 but garbage CBOR body
	if _, _, err := DecodeShareString("mp-share:v1:AAAAAA"); err == nil {
		t.Fatal("expected error for malformed CBOR")
	}
}

func TestShareIDDerivation(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	id1 := DeriveShareID(key)
	id2 := DeriveShareID(key)
	if len(id1) != 16 {
		t.Fatalf("share_id length %d != 16", len(id1))
	}
	if !bytes.Equal(id1, id2) {
		t.Fatalf("derivation not deterministic")
	}
	other := DeriveShareID(bytes.Repeat([]byte{0x43}, 32))
	if bytes.Equal(id1, other) {
		t.Fatalf("derivation should differ on different keys")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail.**

Run: `go test -run 'TestEncode|TestDecode|TestShareID' ./... 2>&1 | tail -20`
Expected: compile error or FAIL (undefined functions).

- [ ] **Step 3: Implement share_codec.go.**

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// sharePayload is the CBOR payload inside a share string.
// Field names use short tags to keep the encoded string compact.
type sharePayload struct {
	PeerID []byte `cbor:"1,keyasint"`
	Key    []byte `cbor:"2,keyasint"`
}

// EncodeShareString builds "mp-share:v1:<base64url(CBOR{peer_id, key})>".
func EncodeShareString(peerID, key []byte) (string, error) {
	if len(peerID) != 32 {
		return "", fmt.Errorf("peer_id must be 32 bytes (got %d)", len(peerID))
	}
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes (got %d)", len(key))
	}
	body, err := cbor.Marshal(sharePayload{PeerID: peerID, Key: key})
	if err != nil {
		return "", fmt.Errorf("cbor marshal: %w", err)
	}
	b64 := base64.RawURLEncoding.EncodeToString(body)
	return ShareStringPrefix + b64, nil
}

// DecodeShareString parses a share string and returns peer_id + key.
func DecodeShareString(s string) (peerID, key []byte, err error) {
	if !strings.HasPrefix(s, ShareStringPrefix) {
		return nil, nil, errors.New("invalid share string: missing prefix")
	}
	rest := strings.TrimPrefix(s, ShareStringPrefix)
	body, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid share string: base64: %w", err)
	}
	var p sharePayload
	if err := cbor.Unmarshal(body, &p); err != nil {
		return nil, nil, fmt.Errorf("invalid share string: cbor: %w", err)
	}
	if len(p.PeerID) != 32 || len(p.Key) != 32 {
		return nil, nil, errors.New("invalid share string: wrong field sizes")
	}
	return p.PeerID, p.Key, nil
}

// DeriveShareID returns the 16-byte public identifier for a publication,
// computed as SHA-256(symkey)[:16].
func DeriveShareID(symkey []byte) []byte {
	h := sha256.Sum256(symkey)
	out := make([]byte, 16)
	copy(out, h[:16])
	return out
}

// guard against unused bytes import if only one of the helpers is kept during refactors.
var _ = bytes.Equal
```

Note: the spec mentions version bumps go on the prefix (`mp-share:v1:` → `mp-share:v2:`); the decoder above strictly matches v1. Future versions add a separate prefix check in a new branch.

- [ ] **Step 4: Run tests — verify they pass.**

Run: `go test -run 'TestEncode|TestDecode|TestShareID' ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_codec.go share_codec_test.go
git commit -m "feat(share): add share-string codec and share_id derivation"
```

---

## Phase 2 — Schema migrations + identity persistence

### Task 2.1: Add shares / follows / share_ring schema

**Files:**
- Modify: `database.go`

- [ ] **Step 1: Add `CREATE TABLE` stanzas.**

Insert immediately before the final `backfillContentHashes(db)` call in `database.go:initDB` (around line 219). Use the pattern of the surrounding tables (`if _, err := db.Exec(...); err != nil { log.Printf(...) }`).

```go
// Create shares table (publisher-side publications)
if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS shares (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	tag_id      INTEGER NOT NULL,
	symkey      BLOB    NOT NULL,
	share_id    BLOB    NOT NULL UNIQUE,
	last_seq    INTEGER NOT NULL DEFAULT 0,
	clips_sent  INTEGER NOT NULL DEFAULT 0,
	status      TEXT    NOT NULL DEFAULT 'active',
	created_at  INTEGER NOT NULL,
	FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
)`); err != nil {
	log.Printf("Warning: Failed to create shares table: %v", err)
}
if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_shares_tag_id ON shares(tag_id)`); err != nil {
	log.Printf("Warning: Failed to create idx_shares_tag_id: %v", err)
}

// Create follows table (follower-side subscriptions)
if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS follows (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	remote_peer_id  TEXT    NOT NULL,
	symkey          BLOB    NOT NULL,
	local_tag_id    INTEGER NOT NULL,
	last_seq        INTEGER NOT NULL DEFAULT 0,
	clips_received  INTEGER NOT NULL DEFAULT 0,
	last_seen_at    INTEGER,
	created_at      INTEGER NOT NULL,
	FOREIGN KEY (local_tag_id) REFERENCES tags(id) ON DELETE RESTRICT
)`); err != nil {
	log.Printf("Warning: Failed to create follows table: %v", err)
}
if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_follows_peer ON follows(remote_peer_id)`); err != nil {
	log.Printf("Warning: Failed to create idx_follows_peer: %v", err)
}

// Create share_ring table (persisted envelope ciphertext for catch-up)
if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS share_ring (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	publication_id INTEGER NOT NULL,
	seq            INTEGER NOT NULL,
	kind           TEXT    NOT NULL,
	envelope_bytes BLOB    NOT NULL,
	ts             INTEGER NOT NULL,
	FOREIGN KEY (publication_id) REFERENCES shares(id) ON DELETE CASCADE
)`); err != nil {
	log.Printf("Warning: Failed to create share_ring table: %v", err)
}
if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_share_ring_pub_seq ON share_ring(publication_id, seq)`); err != nil {
	log.Printf("Warning: Failed to create idx_share_ring_pub_seq: %v", err)
}
if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_share_ring_ts ON share_ring(ts)`); err != nil {
	log.Printf("Warning: Failed to create idx_share_ring_ts: %v", err)
}
```

- [ ] **Step 2: Build & smoke-run to verify migration idempotency.**

Run: `go build ./... && rm -rf /tmp/test-mp-db && mkdir /tmp/test-mp-db`

Then open a tiny sanity test:

```go
// In database_test.go (create if missing), add:
func TestShareTablesMigrate(t *testing.T) {
	// Reuse the real initDB; it'll use the real data dir, which is fine for a
	// one-shot migration sanity check. Alternatively point XDG_DATA_HOME
	// at a tempdir via t.Setenv for full isolation.
	db, err := initDB()
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()
	for _, tbl := range []string{"shares", "follows", "share_ring"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", tbl, err)
		}
	}
}
```

Run: `go test -run TestShareTablesMigrate ./... 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add database.go database_test.go
git commit -m "feat(share): add shares, follows, share_ring tables + indexes"
```

### Task 2.2: Identity persistence

**Files:**
- Create: `share_identity.go`
- Test: `share_identity_test.go`

- [ ] **Step 1: Write failing tests.**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityGenerateAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)

	priv1, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	pid1 := PeerIDFromPrivKey(priv1)

	priv2, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	pid2 := PeerIDFromPrivKey(priv2)

	if pid1 != pid2 {
		t.Fatalf("peer IDs differ across reload: %s vs %s", pid1, pid2)
	}
}

func TestIdentityFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)
	_, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("identity file perms = %v, want 0600", info.Mode().Perm())
	}
}

func TestIdentityCorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ShareIdentityFile)
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatal("expected error on corrupt identity file")
	}
}

func TestPublicKeyBytes32(t *testing.T) {
	dir := t.TempDir()
	priv, err := LoadOrCreateIdentity(filepath.Join(dir, ShareIdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := PublicKeyBytes(priv)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubBytes) != 32 {
		t.Fatalf("pub key bytes = %d, want 32 (raw Ed25519)", len(pubBytes))
	}
}
```

- [ ] **Step 2: Run — verify fail.**

Run: `go test -run TestIdentity ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement share_identity.go.**

```go
package main

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// LoadOrCreateIdentity returns the Ed25519 private key at path, generating and
// persisting one if the file does not yet exist. Permissions are 0600.
// Returns a typed error on corrupt existing files.
func LoadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		priv, uerr := crypto.UnmarshalPrivateKey(b)
		if uerr != nil {
			return nil, fmt.Errorf("identity file %s is corrupt: %w", path, uerr)
		}
		if priv.Type() != crypto.Ed25519 {
			return nil, fmt.Errorf("identity file %s is not Ed25519", path)
		}
		return priv, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading identity file: %w", err)
	}

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating Ed25519 key: %w", err)
	}
	marshalled, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(path, marshalled, 0o600); err != nil {
		return nil, fmt.Errorf("writing identity file: %w", err)
	}
	return priv, nil
}

// PeerIDFromPrivKey returns the libp2p peer.ID derived from a private key.
func PeerIDFromPrivKey(priv crypto.PrivKey) peer.ID {
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		// With a valid Ed25519 priv key this cannot fail.
		panic(fmt.Errorf("derive peer.ID: %w", err))
	}
	return pid
}

// PublicKeyBytes returns the 32-byte raw Ed25519 public key for use in the
// share-string payload.
func PublicKeyBytes(priv crypto.PrivKey) ([]byte, error) {
	pub := priv.GetPublic()
	return pub.Raw()
}
```

- [ ] **Step 4: Run — verify pass.**

Run: `go test -run TestIdentity ./... -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_identity.go share_identity_test.go
git commit -m "feat(share): add persistent libp2p Ed25519 identity loader"
```

---

## Phase 3 — Protocol primitives (envelope + handshake + ring)

### Task 3.1: Envelope encode/decode (framed AES-GCM)

**Files:**
- Create: `share_protocol.go`
- Test: `share_protocol_test.go`

- [ ] **Step 1: Write failing tests for envelope framing.**

```go
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestEnvelopeEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	plaintext := []byte("hello envelope")
	seq := uint64(42)

	frame, err := EncryptEnvelope(key, shareID, seq, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Frame layout: u32 length || nonce || ciphertext+tag
	if len(frame) < 4+12+16 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	innerLen := binary.BigEndian.Uint32(frame[:4])
	if int(innerLen) != len(frame)-4 {
		t.Fatalf("length prefix mismatch: %d vs body %d", innerLen, len(frame)-4)
	}

	got, err := DecryptEnvelope(key, shareID, seq, frame)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: %q", got)
	}
}

func TestEnvelopeWrongSeqFails(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	frame, err := EncryptEnvelope(key, shareID, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptEnvelope(key, shareID, 2, frame); err == nil {
		t.Fatal("expected decrypt fail on wrong seq AAD")
	}
}

func TestEnvelopeWrongShareIDFails(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	bogus := make([]byte, 16)

	frame, err := EncryptEnvelope(key, shareID, 1, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptEnvelope(key, bogus, 1, frame); err == nil {
		t.Fatal("expected decrypt fail on wrong share_id AAD")
	}
}

func TestReadFrameFromStream(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	f1, _ := EncryptEnvelope(key, shareID, 1, []byte("msg1"))
	f2, _ := EncryptEnvelope(key, shareID, 2, []byte("msg2"))

	var buf bytes.Buffer
	buf.Write(f1)
	buf.Write(f2)

	out1, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got1, _ := DecryptEnvelope(key, shareID, 1, out1)
	if string(got1) != "msg1" {
		t.Fatalf("frame 1 plaintext %q", got1)
	}
	out2, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got2, _ := DecryptEnvelope(key, shareID, 2, out2)
	if string(got2) != "msg2" {
		t.Fatalf("frame 2 plaintext %q", got2)
	}
	if _, err := ReadFrame(&buf); err != io.EOF {
		t.Fatalf("expected EOF at end, got %v", err)
	}
}

func TestReadFrameRejectsOverlongEnvelope(t *testing.T) {
	// Pretend someone sends length larger than MaxEnvelopeLen.
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(MaxEnvelopeLen+1))
	r := bytes.NewReader(hdr[:])
	_, err := ReadFrame(r)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got %v", err)
	}
}
```

- [ ] **Step 2: Run — verify fail.**

Run: `go test -run 'TestEnvelope|TestReadFrame' ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement envelope framing.**

Add to `share_protocol.go`:

```go
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// EncryptEnvelope builds the full on-wire frame:
//   u32 length || 12-byte nonce || ciphertext || GCM tag
// where AAD = share_id || seq (u64 BE).
func EncryptEnvelope(key, shareID []byte, seq uint64, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	if len(shareID) != 16 {
		return nil, errors.New("share_id must be 16 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := make([]byte, 16+8)
	copy(aad, shareID)
	binary.BigEndian.PutUint64(aad[16:], seq)

	ct := gcm.Seal(nil, nonce, plaintext, aad)
	inner := make([]byte, 0, len(nonce)+len(ct))
	inner = append(inner, nonce...)
	inner = append(inner, ct...)

	if len(inner) > MaxEnvelopeLen {
		return nil, fmt.Errorf("envelope too large: %d > %d", len(inner), MaxEnvelopeLen)
	}
	frame := make([]byte, 4+len(inner))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(inner)))
	copy(frame[4:], inner)
	return frame, nil
}

// DecryptEnvelope verifies and decrypts a full framed envelope (as produced by
// EncryptEnvelope). Returns plaintext.
func DecryptEnvelope(key, shareID []byte, seq uint64, frame []byte) ([]byte, error) {
	if len(frame) < 4+12+16 {
		return nil, errors.New("frame too short")
	}
	innerLen := binary.BigEndian.Uint32(frame[:4])
	if int(innerLen)+4 != len(frame) {
		return nil, errors.New("frame length mismatch")
	}
	nonce := frame[4 : 4+12]
	ct := frame[4+12:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad := make([]byte, 16+8)
	copy(aad, shareID)
	binary.BigEndian.PutUint64(aad[16:], seq)

	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return pt, nil
}

// ReadFrame reads one length-prefixed envelope from r and returns the full
// frame (length || nonce || ciphertext || tag) as a single []byte.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if int(n) > MaxEnvelopeLen {
		return nil, fmt.Errorf("frame body too large: %d > %d", n, MaxEnvelopeLen)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	out := make([]byte, 4+int(n))
	copy(out, hdr[:])
	copy(out[4:], body)
	return out, nil
}
```

- [ ] **Step 4: Run tests — verify pass.**

Run: `go test -run 'TestEnvelope|TestReadFrame' ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_protocol.go share_protocol_test.go
git commit -m "feat(share): add envelope framing + AES-256-GCM encrypt/decrypt"
```

### Task 3.2: Handshake encode/decode + HMAC verification

**Files:**
- Modify: `share_protocol.go`, `share_protocol_test.go`

- [ ] **Step 1: Add failing handshake tests.**

Append to `share_protocol_test.go`:

```go
func TestHandshakeRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)

	hs := BuildHandshake(key, shareID, 7)
	parsed, err := ParseHandshake(hs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(parsed.ShareID, shareID) {
		t.Fatal("share_id mismatch")
	}
	if parsed.SinceSeq != 7 {
		t.Fatalf("since_seq %d", parsed.SinceSeq)
	}
	if err := VerifyHandshake(key, parsed); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestHandshakeVerifyFailsOnTamper(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	shareID := DeriveShareID(key)
	hs := BuildHandshake(key, shareID, 0)
	// Flip a byte in the HMAC
	hs[len(hs)-1] ^= 0xFF
	parsed, err := ParseHandshake(hs)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyHandshake(key, parsed); err == nil {
		t.Fatal("expected verify fail on tampered HMAC")
	}
}

func TestHandshakeVerifyFailsWithWrongKey(t *testing.T) {
	k1 := make([]byte, 32)
	k2 := make([]byte, 32)
	rand.Read(k1)
	rand.Read(k2)
	shareID := DeriveShareID(k1)
	hs := BuildHandshake(k1, shareID, 0)
	parsed, _ := ParseHandshake(hs)
	if err := VerifyHandshake(k2, parsed); err == nil {
		t.Fatal("expected verify fail with wrong key")
	}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestHandshake ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement handshake.**

Append to `share_protocol.go`:

```go
import (
	"crypto/hmac"
	"crypto/sha256"
)

// Handshake is the one-shot follower→publisher message sent at stream open.
// Wire layout: share_id (16) || proof_nonce (16) || proof_hmac (32) || since_seq (u64 BE) = 72 bytes.
type Handshake struct {
	ShareID    []byte // 16
	ProofNonce []byte // 16
	ProofHMAC  []byte // 32
	SinceSeq   uint64
}

const HandshakeBytesLen = 16 + 16 + 32 + 8

// BuildHandshake produces the 72-byte handshake blob for a follower.
func BuildHandshake(symkey, shareID []byte, sinceSeq uint64) []byte {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		panic(fmt.Errorf("handshake nonce rand: %w", err))
	}
	h := hmac.New(sha256.New, symkey)
	h.Write([]byte(HandshakeHMACContext))
	h.Write(nonce)
	mac := h.Sum(nil)

	buf := make([]byte, 0, HandshakeBytesLen)
	buf = append(buf, shareID...)
	buf = append(buf, nonce...)
	buf = append(buf, mac...)
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], sinceSeq)
	buf = append(buf, seqBuf[:]...)
	return buf
}

// ParseHandshake decodes the 72-byte blob into a Handshake struct. It does NOT
// verify the HMAC — call VerifyHandshake with the looked-up key.
func ParseHandshake(b []byte) (*Handshake, error) {
	if len(b) != HandshakeBytesLen {
		return nil, fmt.Errorf("handshake len %d != %d", len(b), HandshakeBytesLen)
	}
	return &Handshake{
		ShareID:    append([]byte(nil), b[0:16]...),
		ProofNonce: append([]byte(nil), b[16:32]...),
		ProofHMAC:  append([]byte(nil), b[32:64]...),
		SinceSeq:   binary.BigEndian.Uint64(b[64:72]),
	}, nil
}

// VerifyHandshake recomputes the HMAC with the publisher's stored symkey and
// compares in constant time.
func VerifyHandshake(symkey []byte, hs *Handshake) error {
	h := hmac.New(sha256.New, symkey)
	h.Write([]byte(HandshakeHMACContext))
	h.Write(hs.ProofNonce)
	expected := h.Sum(nil)
	if !hmac.Equal(expected, hs.ProofHMAC) {
		return errors.New("handshake HMAC mismatch")
	}
	return nil
}
```

- [ ] **Step 4: Run — verify pass.**

Run: `go test -run TestHandshake ./... -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_protocol.go share_protocol_test.go
git commit -m "feat(share): add handshake encode/decode/verify with HMAC-SHA256"
```

### Task 3.3: CBOR plaintext payloads (clip_start / clip_chunk / clip_end)

**Files:**
- Modify: `share_protocol.go`, `share_protocol_test.go`

- [ ] **Step 1: Add failing tests.**

Append to `share_protocol_test.go`:

```go
func TestClipStartRoundtrip(t *testing.T) {
	meta := map[string]string{"author": "alice"}
	s := ClipStartPayload{
		Seq: 10, TS: 1_700_000_000_000,
		Kind: KindClipStart, ClipID: 123,
		Filename: "photo.png", ContentType: "image/png",
		Metadata: meta, TotalSize: 2048, ChunkCount: 3,
	}
	b, err := MarshalPayload(s)
	if err != nil {
		t.Fatal(err)
	}
	kind, raw, err := PeekPayloadKind(b)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindClipStart {
		t.Fatalf("kind %q", kind)
	}
	var got ClipStartPayload
	if err := UnmarshalPayload(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Filename != "photo.png" || got.ChunkCount != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestClipChunkAndEndRoundtrip(t *testing.T) {
	c := ClipChunkPayload{Seq: 11, Kind: KindClipChunk, ClipID: 123, Index: 0, Data: []byte("hello")}
	b, _ := MarshalPayload(c)
	var gotC ClipChunkPayload
	if err := UnmarshalPayload(b, &gotC); err != nil {
		t.Fatal(err)
	}
	if string(gotC.Data) != "hello" {
		t.Fatalf("data %q", gotC.Data)
	}

	e := ClipEndPayload{Seq: 12, Kind: KindClipEnd, ClipID: 123, SHA256: make([]byte, 32)}
	b2, _ := MarshalPayload(e)
	var gotE ClipEndPayload
	if err := UnmarshalPayload(b2, &gotE); err != nil {
		t.Fatal(err)
	}
	if len(gotE.SHA256) != 32 {
		t.Fatal("sha256 len")
	}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run 'TestClipStart|TestClipChunk' ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement CBOR payload types.**

Append to `share_protocol.go`:

```go
import "github.com/fxamacker/cbor/v2"

type ClipStartPayload struct {
	Seq         uint64            `cbor:"seq"`
	TS          int64             `cbor:"ts"`
	Kind        string            `cbor:"kind"`
	ClipID      uint64            `cbor:"clip_id"`
	Filename    string            `cbor:"filename"`
	ContentType string            `cbor:"content_type"`
	Metadata    map[string]string `cbor:"metadata"`
	TotalSize   uint64            `cbor:"total_size"`
	ChunkCount  uint32            `cbor:"chunk_count"`
}

type ClipChunkPayload struct {
	Seq    uint64 `cbor:"seq"`
	Kind   string `cbor:"kind"`
	ClipID uint64 `cbor:"clip_id"`
	Index  uint32 `cbor:"index"`
	Data   []byte `cbor:"data"`
}

type ClipEndPayload struct {
	Seq    uint64 `cbor:"seq"`
	Kind   string `cbor:"kind"`
	ClipID uint64 `cbor:"clip_id"`
	SHA256 []byte `cbor:"sha256"`
}

type GapPayload struct {
	Seq  uint64 `cbor:"seq"`
	Kind string `cbor:"kind"`
}

// MarshalPayload CBOR-encodes any payload struct.
func MarshalPayload(v any) ([]byte, error) { return cbor.Marshal(v) }

// UnmarshalPayload CBOR-decodes into dst.
func UnmarshalPayload(b []byte, dst any) error { return cbor.Unmarshal(b, dst) }

// PeekPayloadKind decodes just the "kind" field from a CBOR-encoded payload,
// returning the kind string and the original bytes for a second pass.
func PeekPayloadKind(b []byte) (string, []byte, error) {
	var peek struct {
		Kind string `cbor:"kind"`
	}
	if err := cbor.Unmarshal(b, &peek); err != nil {
		return "", nil, err
	}
	return peek.Kind, b, nil
}
```

- [ ] **Step 4: Run — verify pass.**

Run: `go test -run 'TestClipStart|TestClipChunk' ./... -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_protocol.go share_protocol_test.go
git commit -m "feat(share): add clip_start / clip_chunk / clip_end / gap CBOR payloads"
```

---

## Phase 4 — share_ring persistence layer

### Task 4.1: Ring insert + retransmit query + eviction

**Files:**
- Create: `share_ring.go`
- Test: `share_ring_test.go`

- [ ] **Step 1: Failing tests.**

```go
package main

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDBWithShareRing(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER, symkey BLOB, share_id BLOB, last_seq INTEGER, status TEXT, created_at INTEGER);
CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER, FOREIGN KEY(publication_id) REFERENCES shares(id) ON DELETE CASCADE);
CREATE UNIQUE INDEX idx_share_ring_pub_seq ON share_ring(publication_id, seq);
CREATE INDEX idx_share_ring_ts ON share_ring(ts);`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, 1, X'00', X'00', 0, 'active', 0)`)
	return db
}

func TestRingInsertAndRetransmit(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	for seq := uint64(1); seq <= 5; seq++ {
		if err := RingInsert(db, 1, seq, KindClipChunk, []byte{0, 0, 0, byte(seq)}, now); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := RingRetransmit(db, 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d want 3", len(rows))
	}
	// Seq order 3,4,5.
	for i, r := range rows {
		if int(r.Seq) != i+3 {
			t.Fatalf("row %d seq %d", i, r.Seq)
		}
	}
}

func TestRingRetransmitRespectsTTL(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	// Old row (2h ago)
	RingInsert(db, 1, 1, KindClipChunk, []byte{1}, now-2*3600)
	// Fresh row
	RingInsert(db, 1, 2, KindClipChunk, []byte{2}, now)
	rows, err := RingRetransmit(db, 1, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Seq != 2 {
		t.Fatalf("expected only fresh row, got %+v", rows)
	}
}

func TestRingEvictAgeAndCap(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	// Age: 5 old rows
	for i := uint64(1); i <= 5; i++ {
		RingInsert(db, 1, i, KindClipChunk, []byte{1}, now-2*3600)
	}
	// Cap: 3 fresh rows, each 200KB
	big := make([]byte, 200*1024)
	for i := uint64(6); i <= 8; i++ {
		RingInsert(db, 1, i, KindClipChunk, big, now)
	}
	if err := RingEvict(db, now, 500*1024); err != nil { // cap 500KB
		t.Fatal(err)
	}
	var total int
	var count int
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(LENGTH(envelope_bytes)),0) FROM share_ring").Scan(&count, &total)
	// Expect all 5 old rows deleted by age; then oldest of the fresh rows
	// removed until cap satisfied (≤500KB).
	if count == 0 || total > 500*1024 {
		t.Fatalf("after evict count=%d total=%d", count, total)
	}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestRing ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement share_ring.go.**

```go
package main

import (
	"database/sql"
	"fmt"
)

// RingRow is one row of share_ring as returned by queries.
type RingRow struct {
	Seq           uint64
	Kind          string
	EnvelopeBytes []byte
}

// RingInsert appends an envelope to the ring for publicationID.
func RingInsert(db *sql.DB, publicationID int64, seq uint64, kind string, envelopeBytes []byte, tsUnix int64) error {
	_, err := db.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		publicationID, int64(seq), kind, envelopeBytes, tsUnix,
	)
	if err != nil {
		return fmt.Errorf("ring insert: %w", err)
	}
	return nil
}

// RingRetransmit returns rows with seq > sinceSeq that are within the 1h TTL
// relative to nowUnix, ordered by seq. Used on handshake catch-up.
func RingRetransmit(db *sql.DB, publicationID int64, sinceSeq uint64, nowUnix int64) ([]RingRow, error) {
	cutoff := nowUnix - RingTTLSeconds
	rows, err := db.Query(
		`SELECT seq, kind, envelope_bytes FROM share_ring
          WHERE publication_id = ?
            AND seq > ?
            AND ts >= ?
          ORDER BY seq`,
		publicationID, int64(sinceSeq), cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("ring retransmit query: %w", err)
	}
	defer rows.Close()
	var out []RingRow
	for rows.Next() {
		var r RingRow
		var seqI int64
		if err := rows.Scan(&seqI, &r.Kind, &r.EnvelopeBytes); err != nil {
			return nil, err
		}
		r.Seq = uint64(seqI)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RingEvict deletes (1) every row older than the 1h TTL, then (2) for any
// publication over the per-publication byte cap, drops oldest rows until under
// cap. Call after each emission and from the periodic sweeper.
func RingEvict(db *sql.DB, nowUnix int64, bytesCapPerPub int64) error {
	// (1) Age eviction — global.
	cutoff := nowUnix - RingTTLSeconds
	if _, err := db.Exec(`DELETE FROM share_ring WHERE ts < ?`, cutoff); err != nil {
		return fmt.Errorf("ring age evict: %w", err)
	}
	// (2) Per-publication byte cap.
	rows, err := db.Query(`SELECT publication_id, SUM(LENGTH(envelope_bytes)) FROM share_ring GROUP BY publication_id`)
	if err != nil {
		return fmt.Errorf("ring byte query: %w", err)
	}
	defer rows.Close()
	type overCap struct {
		pubID int64
		bytes int64
	}
	var over []overCap
	for rows.Next() {
		var pid, size int64
		if err := rows.Scan(&pid, &size); err != nil {
			return err
		}
		if size > bytesCapPerPub {
			over = append(over, overCap{pubID: pid, bytes: size})
		}
	}
	rows.Close()
	for _, o := range over {
		if err := trimOldestForPub(db, o.pubID, o.bytes-bytesCapPerPub); err != nil {
			return err
		}
	}
	return nil
}

// trimOldestForPub deletes rows starting from the oldest seq for this
// publication until at least `bytesToDrop` have been freed.
func trimOldestForPub(db *sql.DB, pubID, bytesToDrop int64) error {
	freed := int64(0)
	for freed < bytesToDrop {
		var id int64
		var size int64
		err := db.QueryRow(
			`SELECT id, LENGTH(envelope_bytes) FROM share_ring WHERE publication_id = ? ORDER BY seq ASC LIMIT 1`,
			pubID,
		).Scan(&id, &size)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := db.Exec(`DELETE FROM share_ring WHERE id = ?`, id); err != nil {
			return err
		}
		freed += size
	}
	return nil
}

// RingBytesForPub returns the total envelope byte footprint of a publication.
func RingBytesForPub(db *sql.DB, publicationID int64) (int64, error) {
	var n int64
	err := db.QueryRow(
		`SELECT COALESCE(SUM(LENGTH(envelope_bytes)),0) FROM share_ring WHERE publication_id = ?`,
		publicationID,
	).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run — verify pass.**

Run: `go test -run TestRing ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_ring.go share_ring_test.go
git commit -m "feat(share): add share_ring persistence layer with TTL + byte-cap eviction"
```

---

## Phase 5 — ShareManager skeleton: libp2p host, publications, follows registries

### Task 5.1: ShareManager struct + Init + Stop

**Files:**
- Create: `share_manager.go`
- Test: `share_manager_test.go`

- [ ] **Step 1: Failing test for Init/Stop.**

```go
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
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestShareManagerInitStop ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement ShareManager skeleton.**

Create `share_manager.go`:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

// ShareManager owns the libp2p host and tracks publications + follows.
type ShareManager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	db      *sql.DB
	dataDir string

	host host.Host
	dht  *dht.IpfsDHT

	mu           sync.RWMutex
	publications map[int64]*publication // keyed by shares.id
	follows      map[int64]*follow      // keyed by follows.id

	// eventFn is the runtime.EventsEmit bound at App startup time
	// (lets the manager push card updates to the frontend without a
	// direct dependency on the Wails runtime).
	eventFn func(name string, data ...any)
}

// NewShareManager loads identity, starts the libp2p host + DHT, and returns
// the manager ready for ResumeAll.
func NewShareManager(parent context.Context, db *sql.DB, dataDir string) (*ShareManager, error) {
	ctx, cancel := context.WithCancel(parent)

	priv, err := LoadOrCreateIdentity(filepath.Join(dataDir, ShareIdentityFile))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("identity: %w", err)
	}

	// Neutral AgentVersion to avoid install-specific fingerprinting (spec §5.4).
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.UserAgent("mahpastes"),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoRelayWithStaticRelays(defaultStaticRelays()), // or EnableAutoRelayWithPeerSource in a future refinement
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("libp2p.New: %w", err)
	}

	// Register circuit-relay v2 stop handler so we can be reached via relay.
	if _, err := relay.New(h); err == nil {
		// OK if this fails — libp2p host still usable; relay reservation handled by AutoRelay
	}

	// Kademlia DHT. Publisher=auto-server, but we don't know yet which role
	// this node plays; auto-server is safe for both because a node without
	// publications just doesn't serve publication lookups.
	kad, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("dht.New: %w", err)
	}
	if err := kad.Bootstrap(ctx); err != nil {
		log.Printf("share: DHT bootstrap returned error (non-fatal): %v", err)
	}

	m := &ShareManager{
		ctx:          ctx,
		cancel:       cancel,
		db:           db,
		dataDir:      dataDir,
		host:         h,
		dht:          kad,
		publications: map[int64]*publication{},
		follows:      map[int64]*follow{},
	}

	// Register the single application protocol.
	h.SetStreamHandler(ShareProtocolID, m.handlePublisherStream)

	return m, nil
}

// Host exposes the libp2p host for tests and advanced operations.
func (m *ShareManager) Host() host.Host { return m.host }

// SetEventFn installs the frontend event emitter (called from App.startup).
func (m *ShareManager) SetEventFn(fn func(string, ...any)) { m.eventFn = fn }

// Stop shuts down the DHT, host, and cancels all follow reconnect loops.
func (m *ShareManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.follows {
		f.cancel()
	}
	for _, p := range m.publications {
		p.closeAllFollowers()
	}
	if m.dht != nil {
		_ = m.dht.Close()
	}
	if m.host != nil {
		_ = m.host.Close()
	}
	m.cancel()
}

// publication — publisher-side per-tag state.
type publication struct {
	id        int64
	tagID     int64
	shareID   []byte // 16 bytes
	symkey    []byte // 32 bytes
	status    string // "active" | "invalid"
	followers map[peer.ID]*followerConn
	fmu       sync.Mutex
}

func (p *publication) closeAllFollowers() {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	for _, fc := range p.followers {
		fc.close()
	}
	p.followers = map[peer.ID]*followerConn{}
}

// followerConn — one connected follower's stream + send queue. Implemented in
// Phase 5.3.
type followerConn struct {
	stream network.Stream
	// queue is implemented in Phase 5.3
}

func (fc *followerConn) close() {
	if fc != nil && fc.stream != nil {
		_ = fc.stream.Reset()
	}
}

// follow — follower-side per-subscription state.
type follow struct {
	id           int64
	remotePeerID peer.ID
	symkey       []byte
	localTagID   int64
	lastSeq      uint64
	status       string
	ctx          context.Context
	cancel       context.CancelFunc
}

// defaultStaticRelays returns a placeholder relay list. In production libp2p
// has a maintained public relay set; we use a small well-known list.
// See https://github.com/libp2p/go-libp2p/blob/master/p2p/host/autorelay for
// the upstream convention. For the initial implementation it's acceptable to
// fall back to AutoRelay without a static list (libp2p discovers via DHT).
func defaultStaticRelays() []peer.AddrInfo {
	// Intentionally empty: AutoRelay will discover relays via the DHT. This
	// returns [] to avoid pinning specific relay nodes in-source; revisit if
	// libp2p EnableAutoRelayWithPeerSource becomes the blessed API.
	return nil
}

// handlePublisherStream is installed as the libp2p stream handler. Phase 5.2
// fills in the handshake + retransmit logic.
func (m *ShareManager) handlePublisherStream(s network.Stream) {
	_ = s.Reset() // stub: reject until Phase 5.2 implements
}

// stub for Phase 8 — prevents unused time import warning.
var _ = time.Second
```

- [ ] **Step 4: Verify pass.**

Run: `go test -run TestShareManagerInitStop ./... -v 2>&1 | tail -30`
Expected: PASS (may take a couple seconds for libp2p init).

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): add ShareManager skeleton with libp2p host + DHT"
```

### Task 5.2: Publisher stream handler — handshake + retransmit

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing tests — wire a real libp2p pair in-memory.**

Append to `share_manager_test.go`:

```go
import (
	"bytes"
	"io"
	"time"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

// helper: open stream and do handshake as a follower, returning the stream.
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

func TestPublisherStreamReplaysRing(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, err := NewShareManager(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	// Seed a publication
	symkey := make([]byte, 32)
	for i := range symkey { symkey[i] = 0xAA }
	shareID := DeriveShareID(symkey)
	var pubID int64
	res, _ := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, shareID, time.Now().Unix())
	pubID, _ = res.LastInsertId()

	m.registerPublication(pubID, 1, shareID, symkey, "active")

	// Write two envelopes into the ring
	plain1, _ := MarshalPayload(ClipChunkPayload{Seq: 1, Kind: KindClipChunk, ClipID: 1, Index: 0, Data: []byte("hi")})
	env1, _ := EncryptEnvelope(symkey, shareID, 1, plain1)
	RingInsert(db, pubID, 1, KindClipChunk, env1, time.Now().Unix())

	plain2, _ := MarshalPayload(ClipChunkPayload{Seq: 2, Kind: KindClipChunk, ClipID: 1, Index: 1, Data: []byte("there")})
	env2, _ := EncryptEnvelope(symkey, shareID, 2, plain2)
	RingInsert(db, pubID, 2, KindClipChunk, env2, time.Now().Unix())

	// Spin up a fake follower host and dial
	follower, err := libp2p.New()
	if err != nil { t.Fatal(err) }
	defer follower.Close()

	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	defer s.Close()

	// Expect two frames streamed back
	var readCtx bytes.Buffer
	// Read both frames (publisher streams as-is)
	s.SetReadDeadline(time.Now().Add(3 * time.Second))
	f1, err := ReadFrame(s)
	if err != nil { t.Fatalf("read1: %v", err) }
	pt1, err := DecryptEnvelope(symkey, shareID, 1, f1)
	if err != nil || !bytes.Contains(pt1, []byte("hi")) { t.Fatalf("decrypt1: %v %q", err, pt1) }
	f2, err := ReadFrame(s)
	if err != nil { t.Fatalf("read2: %v", err) }
	pt2, err := DecryptEnvelope(symkey, shareID, 2, f2)
	if err != nil || !bytes.Contains(pt2, []byte("there")) { t.Fatalf("decrypt2: %v %q", err, pt2) }
	_ = readCtx
	_ = io.EOF
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

	// Send handshake with wrong symkey → HMAC won't verify.
	wrong := bytes.Repeat([]byte{0xCC}, 32)
	s := openFollowerStream(t, follower, pubInfo, wrong, shareID, 0)

	// Expect stream reset / EOF quickly.
	s.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err := s.Read(buf)
	if err == nil {
		t.Fatal("expected stream reset; got data")
	}
}

func TestPublisherStreamRejectsOverPublicationCap(t *testing.T) {
	// Fill pub.followers up to MaxStreamsPerPublication with stub entries so
	// the handler's cap check trips on the next real stream.
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	symkey := bytes.Repeat([]byte{0xDE}, 32)
	shareID := DeriveShareID(symkey)
	res, _ := db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, shareID, time.Now().Unix())
	pubID, _ := res.LastInsertId()
	m.registerPublication(pubID, 1, shareID, symkey, "active")

	// Cram the followers map with MaxStreamsPerPublication stub entries.
	pub := m.publications[pubID]
	pub.fmu.Lock()
	for i := 0; i < MaxStreamsPerPublication; i++ {
		// Use a unique sentinel pointer value as the key; stream is nil but
		// close() guards against that.
		var sentinel network.Stream
		pub.followers[sentinel] = &followerConn{}
		// Note: map keys must differ — if nil collapses, use a real stub with
		// a dummy stream impl in test-only code. See note below.
	}
	pub.fmu.Unlock()

	follower, _ := libp2p.New()
	defer follower.Close()
	pubInfo := peer.AddrInfo{ID: m.Host().ID(), Addrs: m.Host().Addrs()}
	s := openFollowerStream(t, follower, pubInfo, symkey, shareID, 0)
	s.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := s.Read(buf); err == nil {
		t.Fatal("expected stream reset after cap exceeded; got data")
	}
}
```

**Note:** the nil-stream stub above won't work because map keys collapse. Implement the test's saturation step by spinning up `MaxStreamsPerPublication` real concurrent follower streams (small loop using the `openFollowerStream` helper). Assert the `(cap+1)`-th stream closes immediately. Keep the loop bounded to avoid test flakiness — `MaxStreamsPerPublication` is 128 by default; if that's too heavy for CI, temporarily lower the constant via a test-only override or ship a Go build tag.

A simpler alternative: add a thin testable helper on `ShareManager` like `forCapTest_setStreamLimit(n int)` that swaps the constant for the duration of the test, and verify the cap with a small loop (e.g. n=2).

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestPublisherStream ./... 2>&1 | tail -30`
Expected: FAIL / undefined `registerPublication`.

- [ ] **Step 3: Implement handler + registerPublication.**

Append to `share_manager.go`:

```go
// registerPublication adds (or updates) a publication in the in-memory map.
// Called from StartShare and ResumeAll.
func (m *ShareManager) registerPublication(id, tagID int64, shareID, symkey []byte, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publications[id] = &publication{
		id:        id,
		tagID:     tagID,
		shareID:   append([]byte(nil), shareID...),
		symkey:    append([]byte(nil), symkey...),
		status:    status,
		followers: map[peer.ID]*followerConn{},
	}
}

// findPublicationByShareID linear-scans the map (small N) to find which
// publication a given share_id belongs to. Returns nil if unknown.
func (m *ShareManager) findPublicationByShareID(shareID []byte) *publication {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.publications {
		if bytes.Equal(p.shareID, shareID) {
			return p
		}
	}
	return nil
}

// handlePublisherStream implements:
//   1. read handshake (72 bytes)
//   2. lookup share_id; if unknown → Reset
//   3. verify HMAC; if bad → Reset
//   4. if status != active → Reset
//   5. replay share_ring with seq > since_seq (TTL enforced in SQL)
//   6. register the followerConn so future live envelopes flow here
func (m *ShareManager) handlePublisherStream(s network.Stream) {
	_ = s.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	hsBuf := make([]byte, HandshakeBytesLen)
	if _, err := io.ReadFull(s, hsBuf); err != nil {
		_ = s.Reset()
		return
	}
	_ = s.SetReadDeadline(time.Time{})

	hs, err := ParseHandshake(hsBuf)
	if err != nil {
		_ = s.Reset()
		return
	}
	pub := m.findPublicationByShareID(hs.ShareID)
	if pub == nil || pub.status != "active" {
		_ = s.Reset()
		return
	}
	if err := VerifyHandshake(pub.symkey, hs); err != nil {
		_ = s.Reset()
		return
	}

	peerID := s.Conn().RemotePeer()

	// Enforce both caps from spec §8.5 under the publication lock so the
	// decision and insertion happen atomically.
	pub.fmu.Lock()
	if len(pub.followers) >= MaxStreamsPerPublication {
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}
	perPeer := 0
	for _, fc := range pub.followers {
		if fc.peerID == peerID {
			perPeer++
		}
	}
	if perPeer >= MaxStreamsPerPeer {
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}
	pub.fmu.Unlock()

	// Retransmit first (catch-up).
	rows, err := RingRetransmit(m.db, pub.id, hs.SinceSeq, time.Now().Unix())
	if err != nil {
		log.Printf("share: retransmit query: %v", err)
		_ = s.Reset()
		return
	}
	for _, r := range rows {
		if _, werr := s.Write(r.EnvelopeBytes); werr != nil {
			_ = s.Reset()
			return
		}
	}

	// Register for live fan-out. Keyed by the libp2p stream pointer so we can
	// track multiple concurrent streams from the same peer (up to MaxStreamsPerPeer).
	fc := newFollowerConn(s, s)
	fc.peerID = peerID
	pub.fmu.Lock()
	pub.followers[s] = fc
	pub.fmu.Unlock()

	// Keep this goroutine alive until the stream closes. When the stream
	// closes, remove the entry. The read-drain catches client-side close.
	go func() {
		_, _ = io.Copy(io.Discard, s)
		pub.fmu.Lock()
		delete(pub.followers, s)
		pub.fmu.Unlock()
		fc.close()
	}()
}
```

This change requires updating the `publication` struct's `followers` map key type from `peer.ID` to `network.Stream` (the stream pointer itself), and adding `peerID` to `followerConn`. Update both in Task 5.1 (go back and apply this change before continuing):

```go
// In Task 5.1's publication struct, replace:
//   followers map[peer.ID]*followerConn
// with:
//   followers map[network.Stream]*followerConn
//
// In Task 6.3's followerConn struct, add a peerID field:
//   type followerConn struct {
//       peerID  peer.ID
//       stream  network.Stream
//       // ... rest unchanged
//   }
//
// And update liveFanOut (Task 6.2) — the iteration variable is still the
// stream pointer but we don't need the key for fan-out, just iterate values.
```

Add the new `bytes` and `io` imports at the top of `share_manager.go` if not already present.

- [ ] **Step 4: Run — verify pass.**

Run: `go test -run TestPublisherStream ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): implement publisher stream handler with retransmit"
```

---

## Phase 6 — Publication flow: StartShare + OnClipCreated + fan-out

### Task 6.1: StartShare / StopShare

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing test.**

```go
func TestStartShareInsertsRowAndRegisters(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`)
	info, err := m.StartShare(1)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	if info.ShareString == "" { t.Fatal("empty share string") }
	if info.Status != "active" { t.Fatalf("status %q", info.Status) }

	peerID, key, err := DecodeShareString(info.ShareString)
	if err != nil { t.Fatal(err) }
	if len(peerID) != 32 || len(key) != 32 { t.Fatal("bad sizes") }

	// Calling StartShare twice on the same tag should fail — one publication per tag
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
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`)
	info, _ := m.StartShare(1)

	if err := m.StopShare(info.TagID); err != nil { t.Fatal(err) }
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM shares WHERE id = ?`, info.ID).Scan(&n)
	if n != 0 { t.Fatalf("shares row still present") }
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run 'TestStartShare|TestStopShare' ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement.**

Append to `share_manager.go`:

```go
import "crypto/rand"

// StartShare creates a new publication for tagID and returns the share string.
// One publication per tag — returns an error if one already exists.
func (m *ShareManager) StartShare(tagID int64) (ShareInfo, error) {
	var info ShareInfo
	// Generate symmetric key.
	symkey := make([]byte, 32)
	if _, err := rand.Read(symkey); err != nil {
		return info, fmt.Errorf("rand: %w", err)
	}
	shareID := DeriveShareID(symkey)
	now := time.Now().Unix()

	// Insert shares row.
	res, err := m.db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (?, ?, ?, 0, 'active', ?)`,
		tagID, symkey, shareID, now,
	)
	if err != nil {
		return info, fmt.Errorf("insert share: %w (one share per tag)", err)
	}
	id, _ := res.LastInsertId()

	// Look up tag name for the DTO.
	var tagName string
	_ = m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, tagID).Scan(&tagName)

	// Build share string.
	pubKeyBytes, err := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
	if err != nil {
		return info, fmt.Errorf("pubkey: %w", err)
	}
	s, err := EncodeShareString(pubKeyBytes, symkey)
	if err != nil {
		return info, fmt.Errorf("encode: %w", err)
	}

	// Register in-memory.
	m.registerPublication(id, tagID, shareID, symkey, "active")

	info = ShareInfo{
		ID: id, TagID: tagID, TagName: tagName,
		ShareString: s, Status: "active",
		Followers: 0, ClipsPushed: 0, CreatedAt: now,
	}
	m.emitEvent("share:publication-updated", info)
	return info, nil
}

// StopShare closes streams, drops ring entries, and deletes the shares row.
// ON DELETE CASCADE on share_ring.publication_id makes the DELETE transitive.
func (m *ShareManager) StopShare(tagID int64) error {
	m.mu.Lock()
	var pub *publication
	var id int64
	for pid, p := range m.publications {
		if p.tagID == tagID {
			pub = p
			id = pid
			break
		}
	}
	if pub != nil {
		delete(m.publications, id)
	}
	m.mu.Unlock()

	if pub != nil {
		pub.closeAllFollowers()
	}

	if _, err := m.db.Exec(`DELETE FROM shares WHERE tag_id = ?`, tagID); err != nil {
		return fmt.Errorf("delete share: %w", err)
	}
	m.emitEvent("share:publication-removed", map[string]any{"tag_id": tagID})
	return nil
}

func (m *ShareManager) emitEvent(name string, data any) {
	if m.eventFn != nil {
		m.eventFn(name, data)
	}
}
```

- [ ] **Step 4: Verify pass.**

Run: `go test -run 'TestStartShare|TestStopShare' ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): implement StartShare / StopShare on ShareManager"
```

### Task 6.2: OnClipCreated — chunked emission + ring write + fan-out

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing tests.**

```go
import "crypto/sha256"

func TestOnClipCreatedEmitsChunks(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	info, _ := m.StartShare(1)

	// 2.5 MiB clip → 3 chunks
	data := bytes.Repeat([]byte{0x77}, int(2.5*float64(ChunkSize)))
	r, _ := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('image/png', ?, 'big.png', '{}')`, data)
	clipID, _ := r.LastInsertId()
	db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID)

	if err := m.OnClipCreated(clipID, []int64{1}); err != nil {
		t.Fatalf("OnClipCreated: %v", err)
	}

	// Ring should now have 5 envelopes: clip_start + 3 clip_chunks + clip_end
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, info.ID).Scan(&count)
	if count != 5 {
		t.Fatalf("ring count %d want 5", count)
	}

	// last_seq advanced to 5
	var lastSeq int64
	db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, info.ID).Scan(&lastSeq)
	if lastSeq != 5 {
		t.Fatalf("last_seq %d want 5", lastSeq)
	}

	// Verify clip_end sha256 matches
	var envBytes []byte
	db.QueryRow(`SELECT envelope_bytes FROM share_ring WHERE publication_id = ? AND kind = ?`, info.ID, KindClipEnd).Scan(&envBytes)
	pub := m.publications[info.ID]
	pt, err := DecryptEnvelope(pub.symkey, pub.shareID, 5, envBytes)
	if err != nil { t.Fatal(err) }
	var end ClipEndPayload
	UnmarshalPayload(pt, &end)
	want := sha256.Sum256(data)
	if !bytes.Equal(end.SHA256, want[:]) {
		t.Fatal("clip_end sha256 mismatch")
	}
}

func TestOnClipCreatedSkipsNonMatchingTag(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()
	m, _ := NewShareManager(ctx, db, dir)
	defer m.Stop()

	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa'), (2, 'y', '#bbb')`)
	info, _ := m.StartShare(1)

	r, _ := db.Exec(`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', 'hi', 'a.txt', '{}')`)
	clipID, _ := r.LastInsertId()
	db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 2)`, clipID)

	m.OnClipCreated(clipID, []int64{2})
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, info.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("ring count %d want 0 (tag mismatch)", count)
	}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestOnClipCreated ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement OnClipCreated.**

Append to `share_manager.go`:

```go
// OnClipCreated is the hook called by App after every clip insert. It finds
// every publication whose tag is on the clip and emits a chunked envelope
// burst into share_ring (and to any connected followers).
func (m *ShareManager) OnClipCreated(clipID int64, tagIDs []int64) error {
	// Collect matching publications under read lock.
	m.mu.RLock()
	var matches []*publication
	for _, p := range m.publications {
		if p.status != "active" {
			continue
		}
		for _, tid := range tagIDs {
			if tid == p.tagID {
				matches = append(matches, p)
				break
			}
		}
	}
	m.mu.RUnlock()
	if len(matches) == 0 {
		return nil
	}

	// Read clip metadata (size + content_type + filename + metadata).
	var contentType, filename, metaJSON string
	var totalSize int64
	err := m.db.QueryRow(
		`SELECT content_type, COALESCE(filename,''), COALESCE(metadata,'{}'), LENGTH(data) FROM clips WHERE id = ?`,
		clipID,
	).Scan(&contentType, &filename, &metaJSON, &totalSize)
	if err != nil {
		return fmt.Errorf("read clip %d: %w", clipID, err)
	}

	metadata := map[string]string{}
	_ = json.Unmarshal([]byte(metaJSON), &metadata)

	chunkCount := uint32((totalSize + int64(ChunkSize) - 1) / int64(ChunkSize))
	if chunkCount == 0 {
		chunkCount = 1 // empty clip still emits one zero-length chunk for symmetry
	}

	for _, p := range matches {
		if err := m.emitClipForPublication(p, clipID, contentType, filename, metadata, totalSize, chunkCount); err != nil {
			log.Printf("share: emit clip %d to pub %d: %v", clipID, p.id, err)
			// Continue to next publication; errors on one don't block others.
		}
	}
	return nil
}

func (m *ShareManager) emitClipForPublication(
	p *publication,
	clipID int64,
	contentType, filename string,
	metadata map[string]string,
	totalSize int64,
	chunkCount uint32,
) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Read last_seq under the transaction.
	var lastSeq uint64
	var seqI int64
	if err := tx.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, p.id).Scan(&seqI); err != nil {
		return fmt.Errorf("read last_seq: %w", err)
	}
	lastSeq = uint64(seqI)

	// Incrementally hash and emit.
	hasher := sha256.New()
	now := time.Now().Unix()
	tsMillis := time.Now().UnixMilli()

	nextSeq := lastSeq + 1

	// 1) clip_start
	start := ClipStartPayload{
		Seq: nextSeq, TS: tsMillis, Kind: KindClipStart,
		ClipID: uint64(clipID), Filename: filename,
		ContentType: contentType, Metadata: metadata,
		TotalSize: uint64(totalSize), ChunkCount: chunkCount,
	}
	startBytes, _ := MarshalPayload(start)
	startEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, startBytes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		p.id, int64(nextSeq), KindClipStart, startEnv, now,
	); err != nil {
		return err
	}
	m.liveFanOut(p, startEnv)
	nextSeq++

	// 2) clip_chunk × N, streaming via SUBSTR so we never hold the full blob in RAM
	for idx := uint32(0); idx < chunkCount; idx++ {
		offset := int64(idx) * int64(ChunkSize)
		length := int64(ChunkSize)
		if offset+length > totalSize {
			length = totalSize - offset
			if length < 0 {
				length = 0
			}
		}
		var chunk []byte
		if err := tx.QueryRow(
			`SELECT SUBSTR(data, ?, ?) FROM clips WHERE id = ?`,
			offset+1, length, clipID,
		).Scan(&chunk); err != nil {
			return fmt.Errorf("read chunk %d: %w", idx, err)
		}
		hasher.Write(chunk)

		cp := ClipChunkPayload{
			Seq: nextSeq, Kind: KindClipChunk,
			ClipID: uint64(clipID), Index: idx, Data: chunk,
		}
		cpBytes, _ := MarshalPayload(cp)
		cpEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, cpBytes)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
			p.id, int64(nextSeq), KindClipChunk, cpEnv, now,
		); err != nil {
			return err
		}
		m.liveFanOut(p, cpEnv)
		nextSeq++
	}

	// 3) clip_end
	end := ClipEndPayload{
		Seq: nextSeq, Kind: KindClipEnd,
		ClipID: uint64(clipID), SHA256: hasher.Sum(nil),
	}
	endBytes, _ := MarshalPayload(end)
	endEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, endBytes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		p.id, int64(nextSeq), KindClipEnd, endEnv, now,
	); err != nil {
		return err
	}
	m.liveFanOut(p, endEnv)

	// Bump last_seq.
	// Bump last_seq AND clips_sent (one clip_end = one published clip).
	// Counter incremented alongside last_seq so they're always in sync.
	if _, err := tx.Exec(`UPDATE shares SET last_seq = ?, clips_sent = clips_sent + 1 WHERE id = ?`, int64(nextSeq), p.id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Ring eviction (age + cap) outside the tx.
	if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
		log.Printf("share: evict: %v", err)
	}
	return nil
}

// liveFanOut writes an envelope to every currently connected follower for this
// publication. Writes are best-effort: a failing write means we close that
// follower's stream and let them reconnect via the ring. Phase 6.3 replaces
// this with a proper byte-capped send scheduler.
func (m *ShareManager) liveFanOut(p *publication, envelope []byte) {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	for pid, fc := range p.followers {
		if _, err := fc.stream.Write(envelope); err != nil {
			_ = fc.stream.Reset()
			delete(p.followers, pid)
		}
	}
}
```

Add `encoding/json` to imports of `share_manager.go`.

- [ ] **Step 4: Verify pass.**

Run: `go test -run TestOnClipCreated ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): implement chunked clip emission into share_ring"
```

### Task 6.3: Byte-capped per-follower send scheduler

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing test.**

```go
func TestSendSchedulerShedsOverCap(t *testing.T) {
	// Fake followerConn with a slow/blocking writer, verify the send loop
	// closes it once byte cap exceeded.
	blocker := &blockingWriter{}
	fc := newFollowerConn(nil, blocker)
	// Inject 33 MiB of envelopes (1 MiB each) — expect close after 32 MiB.
	data := bytes.Repeat([]byte{1}, 1<<20)
	closed := false
	fc.onClose = func() { closed = true }
	for i := 0; i < 33; i++ {
		fc.enqueue(data)
	}
	// Give scheduler a tick
	time.Sleep(100 * time.Millisecond)
	if !closed {
		t.Fatal("expected connection shed after 32 MiB queued")
	}
}

// blockingWriter never completes writes until its chan is drained.
type blockingWriter struct {
	wrote int64
}
func (b *blockingWriter) Write(p []byte) (int, error) {
	// Simulate stall: sleep forever (bounded by test timeout).
	select {}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestSendScheduler ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement the scheduler.**

Replace `followerConn` and `liveFanOut` in `share_manager.go`:

```go
// followerConn owns one follower's stream + a bounded async send queue.
type followerConn struct {
	stream  network.Stream
	writer  io.Writer     // indirection for tests
	queue   chan []byte   // buffered channel, EnvelopesCap slots
	pending int64         // atomic: bytes sitting in queue
	onClose func()        // test hook
	mu      sync.Mutex
	closed  bool
}

func newFollowerConn(s network.Stream, w io.Writer) *followerConn {
	fc := &followerConn{
		stream: s,
		writer: w,
		queue:  make(chan []byte, SendQueueEnvelopesCap),
	}
	go fc.runSender()
	return fc
}

func (fc *followerConn) enqueue(env []byte) {
	fc.mu.Lock()
	if fc.closed {
		fc.mu.Unlock()
		return
	}
	// Byte-cap check.
	if fc.pending+int64(len(env)) > int64(SendQueueBytesCap) {
		fc.closeLocked()
		fc.mu.Unlock()
		return
	}
	// Envelope-cap check (channel buffer).
	select {
	case fc.queue <- env:
		fc.pending += int64(len(env))
	default:
		fc.closeLocked()
	}
	fc.mu.Unlock()
}

func (fc *followerConn) runSender() {
	for env := range fc.queue {
		if _, err := fc.writer.Write(env); err != nil {
			fc.mu.Lock()
			fc.closeLocked()
			fc.mu.Unlock()
			return
		}
		fc.mu.Lock()
		fc.pending -= int64(len(env))
		fc.mu.Unlock()
	}
}

func (fc *followerConn) close() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.closeLocked()
}

func (fc *followerConn) closeLocked() {
	if fc.closed {
		return
	}
	fc.closed = true
	close(fc.queue)
	if fc.stream != nil {
		_ = fc.stream.Reset()
	}
	if fc.onClose != nil {
		fc.onClose()
	}
}
```

Update `handlePublisherStream` to build via `newFollowerConn(s, s)` instead of the struct literal, and update `liveFanOut` to call `fc.enqueue(envelope)` instead of `fc.stream.Write`.

- [ ] **Step 4: Verify pass.**

Run: `go test -run TestSendScheduler ./... -v 2>&1 | tail -30`
Expected: PASS (may take ~100ms for the timer).

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): add byte-capped per-follower send scheduler"
```

---

## Phase 7 — Follow flow: Follow / Unfollow + stream assembly

### Task 7.1: Follow / Unfollow + DHT-based dial

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing test — paired hosts.**

```go
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
				if tagID == 99 { return }
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("follower did not persist clip within 5s")
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestFollowAndReceiveClip ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement Follow / Unfollow + run loop.**

Append to `share_manager.go`:

```go
import (
	"path/filepath"
	"os"
	"crypto/sha256"
)

// Follow validates a share string, creates/resolves the local tag, persists a
// follows row, and starts the background reconnect loop.
func (m *ShareManager) Follow(shareString, localTagName string) (FollowInfo, error) {
	var info FollowInfo

	peerIDBytes, symkey, err := DecodeShareString(shareString)
	if err != nil {
		return info, fmt.Errorf("invalid share string: %w", err)
	}
	// Build libp2p peer.ID from raw Ed25519 public key bytes.
	pubKey, err := cryptoPublicKeyFromBytes(peerIDBytes)
	if err != nil {
		return info, fmt.Errorf("peer id from key: %w", err)
	}
	pid, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return info, fmt.Errorf("peer id: %w", err)
	}
	// Don't follow self.
	if pid == m.host.ID() {
		return info, errors.New("cannot follow your own share")
	}

	// Resolve or create the local tag.
	localTagID, err := m.resolveOrCreateTag(localTagName)
	if err != nil {
		return info, fmt.Errorf("tag: %w", err)
	}

	// Attempt an initial connection (times out at HandshakeTimeout + 2s).
	dctx, dcancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer dcancel()
	if err := m.dialByPeerID(dctx, pid); err != nil {
		return info, fmt.Errorf("initial dial: %w", err)
	}

	// Insert row.
	now := time.Now().Unix()
	res, err := m.db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, last_seen_at, created_at) VALUES (?, ?, ?, 0, ?, ?)`,
		pid.String(), symkey, localTagID, now, now,
	)
	if err != nil {
		return info, fmt.Errorf("insert follow: %w", err)
	}
	id, _ := res.LastInsertId()

	fctx, fcancel := context.WithCancel(m.ctx)
	f := &follow{
		id: id, remotePeerID: pid, symkey: symkey,
		localTagID: localTagID, lastSeq: 0,
		status: "connected", ctx: fctx, cancel: fcancel,
	}
	m.mu.Lock()
	m.follows[id] = f
	m.mu.Unlock()

	go m.runFollowLoop(f)

	info = FollowInfo{
		ID: id, RemotePeerID: pid.String(),
		LocalTagID: localTagID, LocalTagName: localTagName,
		Status: "connected", ClipsReceived: 0, LastSeq: 0,
		CreatedAt: now,
	}
	m.emitEvent("share:follow-updated", info)
	return info, nil
}

// Unfollow cancels the reconnect loop and deletes the follows row.
func (m *ShareManager) Unfollow(followID int64) error {
	m.mu.Lock()
	f, ok := m.follows[followID]
	if ok {
		delete(m.follows, followID)
	}
	m.mu.Unlock()
	if ok {
		f.cancel()
	}
	if _, err := m.db.Exec(`DELETE FROM follows WHERE id = ?`, followID); err != nil {
		return err
	}
	m.emitEvent("share:follow-removed", map[string]any{"id": followID})
	return nil
}

// dialByPeerID finds addrs via peerstore (fast path) or DHT (slow path) and
// opens the stream with a handshake at since_seq = lastSeq.
func (m *ShareManager) dialByPeerID(ctx context.Context, pid peer.ID) error {
	// Fast path: cached addrs.
	if addrs := m.host.Peerstore().Addrs(pid); len(addrs) > 0 {
		if err := m.host.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs}); err == nil {
			return nil
		}
	}
	// Slow path: DHT FindPeer.
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ai, err := m.dht.FindPeer(fctx, pid)
	if err != nil {
		return fmt.Errorf("dht find peer: %w", err)
	}
	m.host.Peerstore().AddAddrs(pid, ai.Addrs, time.Hour)
	return m.host.Connect(ctx, ai)
}

// runFollowLoop is the reconnect + receive loop for one follow.
func (m *ShareManager) runFollowLoop(f *follow) {
	backoff := ReconnectFloor
	for {
		if err := m.followSession(f); err != nil {
			log.Printf("share: follow %d session ended: %v", f.id, err)
		}
		select {
		case <-f.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > ReconnectCap {
			backoff = ReconnectCap
		}
	}
}

// followSession dials, handshakes, and streams until the stream closes.
func (m *ShareManager) followSession(f *follow) error {
	if err := m.dialByPeerID(f.ctx, f.remotePeerID); err != nil {
		m.setFollowStatus(f, "offline")
		return err
	}
	s, err := m.host.NewStream(f.ctx, f.remotePeerID, ShareProtocolID)
	if err != nil {
		m.setFollowStatus(f, "offline")
		return err
	}
	defer s.Reset()

	shareID := DeriveShareID(f.symkey)
	hs := BuildHandshake(f.symkey, shareID, f.lastSeq)
	if _, err := s.Write(hs); err != nil {
		m.setFollowStatus(f, "offline")
		return err
	}
	m.setFollowStatus(f, "connected")

	return m.consumeStream(f, s)
}

// consumeStream reads frames forever and dispatches to the clip assembler.
func (m *ShareManager) consumeStream(f *follow, r io.Reader) error {
	asm := newClipAssembler(filepath.Join(m.dataDir, ShareStagingDirName))
	shareID := DeriveShareID(f.symkey)
	for {
		select {
		case <-f.ctx.Done():
			return f.ctx.Err()
		default:
		}
		frame, err := ReadFrame(r)
		if err != nil {
			return err
		}
		// Seq is part of AAD; we need it to decrypt. Because we don't know it
		// yet, peek into the CBOR after decrypting with candidate seqs — but
		// we can cheat: plaintext also includes the seq. Try lastSeq+1 first.
		pt, err := DecryptEnvelope(f.symkey, shareID, f.lastSeq+1, frame)
		if err != nil {
			// Retransmit may skip seqs (eviction gap); try bumping.
			var ok bool
			for jump := uint64(2); jump < 32; jump++ {
				pt, err = DecryptEnvelope(f.symkey, shareID, f.lastSeq+jump, frame)
				if err == nil {
					f.lastSeq += (jump - 1) // advance over the gap; will be fixed to exact in next step
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("decrypt failed: %w", err)
			}
		}
		kind, raw, err := PeekPayloadKind(pt)
		if err != nil {
			return err
		}
		// Trust the seq that's inside the plaintext — that's the authoritative one.
		var inSeq uint64
		switch kind {
		case KindClipStart:
			var p ClipStartPayload
			if err := UnmarshalPayload(raw, &p); err != nil { return err }
			inSeq = p.Seq
			asm.onStart(p)
		case KindClipChunk:
			var p ClipChunkPayload
			if err := UnmarshalPayload(raw, &p); err != nil { return err }
			inSeq = p.Seq
			asm.onChunk(p)
		case KindClipEnd:
			var p ClipEndPayload
			if err := UnmarshalPayload(raw, &p); err != nil { return err }
			inSeq = p.Seq
			if err := asm.onEnd(p, m.db, f.localTagID); err != nil {
				log.Printf("share: assemble failed: %v", err)
			} else {
				// Assembly succeeded → bump clips_received.
				_, _ = m.db.Exec(`UPDATE follows SET clips_received = clips_received + 1 WHERE id = ?`, f.id)
			}
		case KindGap:
			var p GapPayload
			if err := UnmarshalPayload(raw, &p); err != nil { return err }
			inSeq = p.Seq
		}
		f.lastSeq = inSeq
		_, _ = m.db.Exec(`UPDATE follows SET last_seq = ?, last_seen_at = ? WHERE id = ?`, int64(inSeq), time.Now().Unix(), f.id)
	}
}

func (m *ShareManager) setFollowStatus(f *follow, s string) {
	f.status = s
	m.emitEvent("share:follow-updated", map[string]any{"id": f.id, "status": s})
}

// resolveOrCreateTag uses the existing App API if present, or falls back to a
// direct INSERT. For the tests this path is sufficient; in the wired app,
// ShareManager.resolveOrCreateTag delegates via a callback set at startup.
func (m *ShareManager) resolveOrCreateTag(name string) (int64, error) {
	var id int64
	err := m.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := m.db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#888')`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// cryptoPublicKeyFromBytes reconstructs a libp2p PubKey from 32 raw Ed25519 bytes.
func cryptoPublicKeyFromBytes(raw []byte) (crypto.PubKey, error) {
	if len(raw) != 32 {
		return nil, errors.New("pubkey must be 32 bytes")
	}
	return crypto.UnmarshalEd25519PublicKey(raw)
}

// Unused import guards.
var _ = os.Remove
var _ = sha256.New
```

- [ ] **Step 4: Verify pass.**

Run: `go test -run TestFollowAndReceiveClip ./... -v -timeout 30s 2>&1 | tail -30`
Expected: PASS (may take ~1-3s for handshake + stream).

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): implement Follow / Unfollow with DHT fallback and stream consumer"
```

### Task 7.2: Clip assembler (staging file + atomic insert)

**Files:**
- Create: `share_assembler.go`
- Test: `share_assembler_test.go`

- [ ] **Step 1: Failing test.**

```go
package main

import (
	"bytes"
	"database/sql"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAssemblerWritesClipAndTag(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'inbox', '#888')`)
	dir := t.TempDir()
	asm := newClipAssembler(dir)

	data := bytes.Repeat([]byte("hello"), 1000)
	h := sha256.Sum256(data)
	meta := map[string]string{"from": "pub"}

	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "m.txt", ContentType: "text/plain",
		Metadata: meta, TotalSize: uint64(len(data)), ChunkCount: 1,
	})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	if err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: h[:]}, db, 42); err != nil {
		t.Fatal(err)
	}

	var clipID int64
	var content []byte
	var filename, ctype, metaJSON string
	db.QueryRow(`SELECT id, data, filename, content_type, metadata FROM clips LIMIT 1`).Scan(&clipID, &content, &filename, &ctype, &metaJSON)
	if !bytes.Equal(content, data) {
		t.Fatalf("content mismatch")
	}
	if filename != "m.txt" { t.Fatalf("filename %q", filename) }
	if ctype != "text/plain" { t.Fatalf("ctype %q", ctype) }
	var gotMeta map[string]string
	json.Unmarshal([]byte(metaJSON), &gotMeta)
	if gotMeta["from"] != "pub" { t.Fatalf("meta %+v", gotMeta) }

	var tagID int64
	db.QueryRow(`SELECT tag_id FROM clip_tags WHERE clip_id = ?`, clipID).Scan(&tagID)
	if tagID != 42 { t.Fatalf("tagID %d", tagID) }

	// Staging file removed
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("staging dir not empty: %v", entries)
	}
}

func TestAssemblerRejectsWrongSHA256(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'x', '#888')`)
	dir := t.TempDir()
	asm := newClipAssembler(dir)
	asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 5, ChunkCount: 1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: []byte("hello")})
	wrong := bytes.Repeat([]byte{0}, 32)
	if err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: wrong}, db, 42); err == nil {
		t.Fatal("expected sha mismatch error")
	}
	// Staging file should still be cleaned up.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staging leak on error: %v", entries)
	}
	_ = filepath.Glob
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestAssembler ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement.**

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// clipAssembler buffers a single in-flight clip (across clip_start / clip_chunk
// / clip_end) to a temp file, then atomically inserts it into SQLite.
type clipAssembler struct {
	stagingDir string

	active    bool
	clipID    uint64
	filename  string
	cType     string
	metadata  map[string]string
	totalSize uint64
	chunks    uint32

	file       *os.File
	filePath   string
	writtenBytes uint64
	hasher     *sha256Hasher
}

type sha256Hasher struct {
	h interface {
		Write(p []byte) (n int, err error)
		Sum(b []byte) []byte
	}
}

func newClipAssembler(stagingDir string) *clipAssembler {
	_ = os.MkdirAll(stagingDir, 0o755)
	return &clipAssembler{stagingDir: stagingDir}
}

func (a *clipAssembler) onStart(p ClipStartPayload) {
	// If a prior clip is still open, discard it.
	a.cleanup()
	a.active = true
	a.clipID = p.ClipID
	a.filename = p.Filename
	a.cType = p.ContentType
	a.metadata = p.Metadata
	a.totalSize = p.TotalSize
	a.chunks = p.ChunkCount

	a.filePath = filepath.Join(a.stagingDir, fmt.Sprintf("%d.bin", p.ClipID))
	f, err := os.Create(a.filePath)
	if err != nil {
		a.active = false
		return
	}
	a.file = f
	a.hasher = &sha256Hasher{h: sha256.New()}
	a.writtenBytes = 0
}

func (a *clipAssembler) onChunk(p ClipChunkPayload) {
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return
	}
	if _, err := a.file.Write(p.Data); err != nil {
		a.cleanup()
		return
	}
	a.hasher.h.Write(p.Data)
	a.writtenBytes += uint64(len(p.Data))
}

func (a *clipAssembler) onEnd(p ClipEndPayload, db *sql.DB, localTagID int64) error {
	defer a.cleanup()
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return fmt.Errorf("clip_end without active clip_start")
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	got := a.hasher.h.Sum(nil)
	if !bytes.Equal(got, p.SHA256) {
		return fmt.Errorf("sha256 mismatch: got %x want %x", got, p.SHA256)
	}
	body, err := io.ReadAll(a.file)
	if err != nil {
		return err
	}
	metaJSON, _ := json.Marshal(a.metadata)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES (?, ?, ?, ?)`,
		a.cType, body, a.filename, string(metaJSON),
	)
	if err != nil {
		return err
	}
	newClipID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, newClipID, localTagID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *clipAssembler) cleanup() {
	if a.file != nil {
		a.file.Close()
		_ = os.Remove(a.filePath)
	}
	a.active = false
	a.file = nil
	a.filePath = ""
	a.hasher = nil
}
```

- [ ] **Step 4: Fix the sha256 wrapper shim.**

The dependency-injected hasher above is awkward. Simplify:

```go
import "hash"

type clipAssembler struct {
	// ...
	hasher hash.Hash
}

// In onStart replace:
//   a.hasher = &sha256Hasher{h: sha256.New()}
// with:
//   a.hasher = sha256.New()
// In onChunk replace a.hasher.h.Write(p.Data) with a.hasher.Write(p.Data).
// In onEnd replace a.hasher.h.Sum(nil) with a.hasher.Sum(nil).
// Delete sha256Hasher type entirely.
```

Apply the simplification inline before running tests.

- [ ] **Step 5: Verify pass.**

Run: `go test -run TestAssembler ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add share_assembler.go share_assembler_test.go
git commit -m "feat(share): add staging-file clip assembler with sha256 verification"
```

---

## Phase 8 — ResumeAll + sweepers + App integration + Wails binding

### Task 8.1: ResumeAll

**Files:**
- Modify: `share_manager.go`, `share_manager_test.go`

- [ ] **Step 1: Failing test.**

```go
func TestResumeAllReplaysTables(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dir := t.TempDir()

	// Pre-seed a publication and a follow row directly.
	symkey := bytes.Repeat([]byte{0xAA}, 32)
	sid := DeriveShareID(symkey)
	db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#888'), (2, 'inbox', '#888')`)
	db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, ?, ?, 0, 'active', ?)`, symkey, sid, time.Now().Unix())
	db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (?, ?, ?, 0, 'invalid', ?)`, int64(99), bytes.Repeat([]byte{0xBB}, 32), DeriveShareID(bytes.Repeat([]byte{0xBB}, 32)), time.Now().Unix())
	db.Exec(`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, last_seen_at, created_at) VALUES ('12D3KooWABCDEF', ?, 2, 0, 0, ?)`, symkey, time.Now().Unix())

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
		if p.status == "active" { actives++ }
	}
	if actives != 1 {
		t.Fatalf("actives %d want 1", actives)
	}
	if len(m.follows) != 1 {
		t.Fatalf("follows %d want 1", len(m.follows))
	}
}
```

- [ ] **Step 2: Verify fail.**

Run: `go test -run TestResumeAll ./... 2>&1 | tail -20`
Expected: FAIL / undefined.

- [ ] **Step 3: Implement ResumeAll.**

Append to `share_manager.go`:

```go
// ResumeAll loads every persisted publication and follow into memory and
// starts background loops. Called once from App.startup after Init.
func (m *ShareManager) ResumeAll() error {
	// Publications
	rows, err := m.db.Query(`SELECT id, tag_id, symkey, share_id, status FROM shares`)
	if err != nil {
		return fmt.Errorf("query shares: %w", err)
	}
	for rows.Next() {
		var id, tagID int64
		var symkey, shareID []byte
		var status string
		if err := rows.Scan(&id, &tagID, &symkey, &shareID, &status); err != nil {
			rows.Close()
			return err
		}
		m.registerPublication(id, tagID, shareID, symkey, status)
	}
	rows.Close()

	// Follows
	frows, err := m.db.Query(`SELECT id, remote_peer_id, symkey, local_tag_id, last_seq FROM follows`)
	if err != nil {
		return fmt.Errorf("query follows: %w", err)
	}
	defer frows.Close()
	for frows.Next() {
		var id, localTagID, lastSeqI int64
		var pidStr string
		var symkey []byte
		if err := frows.Scan(&id, &pidStr, &symkey, &localTagID, &lastSeqI); err != nil {
			return err
		}
		pid, err := peer.Decode(pidStr)
		if err != nil {
			log.Printf("share: resume skip follow %d: bad peer id %q", id, pidStr)
			continue
		}
		fctx, fcancel := context.WithCancel(m.ctx)
		f := &follow{
			id: id, remotePeerID: pid, symkey: symkey,
			localTagID: localTagID, lastSeq: uint64(lastSeqI),
			status: "offline", ctx: fctx, cancel: fcancel,
		}
		m.mu.Lock()
		m.follows[id] = f
		m.mu.Unlock()
		go m.runFollowLoop(f)
	}
	return nil
}
```

- [ ] **Step 4: Verify pass.**

Run: `go test -run TestResumeAll ./... -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add share_manager.go share_manager_test.go
git commit -m "feat(share): implement ResumeAll for persisted publications and follows"
```

### Task 8.2: Ring sweeper + staging janitor

**Files:**
- Modify: `share_manager.go`

- [ ] **Step 1: Add sweeper functions.**

Append to `share_manager.go`:

```go
// startSweepers launches background timers for ring eviction and staging cleanup.
// Call once from App.startup after ResumeAll.
func (m *ShareManager) startSweepers() {
	go m.runRingSweeper()
	go m.runStagingJanitor()
}

func (m *ShareManager) runRingSweeper() {
	// Run once immediately (catches stale rows from restored backups).
	if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
		log.Printf("share: initial ring evict: %v", err)
	}
	t := time.NewTicker(RingSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
				log.Printf("share: ring evict: %v", err)
			}
		}
	}
}

func (m *ShareManager) runStagingJanitor() {
	dir := filepath.Join(m.dataDir, ShareStagingDirName)
	cleanup := func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		cutoff := time.Now().Add(-StagingMaxAge)
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	cleanup()
	t := time.NewTicker(StagingSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			cleanup()
		}
	}
}
```

- [ ] **Step 2: Build verify.**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit.**

```bash
git add share_manager.go
git commit -m "feat(share): add ring sweeper and staging janitor goroutines"
```

### Task 8.3: Add GetShareStatus for UI polling

**Files:**
- Modify: `share_manager.go`

- [ ] **Step 1: Implement.**

```go
// GetShareStatus returns DTOs for every publication and follow currently
// registered, combining in-memory state with DB counters.
func (m *ShareManager) GetShareStatus() (shares []ShareInfo, follows []FollowInfo) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.publications {
		var tagName string
		m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, p.tagID).Scan(&tagName)
		var clipsSent, createdAt int64
		// Authoritative counters live in the DB; no envelope-count heuristics.
		m.db.QueryRow(`SELECT clips_sent, created_at FROM shares WHERE id = ?`, p.id).Scan(&clipsSent, &createdAt)
		p.fmu.Lock()
		fCount := len(p.followers)
		p.fmu.Unlock()

		// Reconstruct share string from stored key + our pubkey.
		pubKeyBytes, _ := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
		shareStr, _ := EncodeShareString(pubKeyBytes, p.symkey)

		shares = append(shares, ShareInfo{
			ID: p.id, TagID: p.tagID, TagName: tagName,
			ShareString: shareStr, Status: p.status,
			Followers: fCount, ClipsPushed: clipsSent,
			CreatedAt: createdAt,
		})
	}

	for _, f := range m.follows {
		var localTagName string
		var createdAt, lastSeqDB, clipsRecv int64
		var lastSeenSQL sql.NullInt64
		m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, f.localTagID).Scan(&localTagName)
		m.db.QueryRow(`SELECT created_at, last_seq, clips_received, last_seen_at FROM follows WHERE id = ?`, f.id).Scan(&createdAt, &lastSeqDB, &clipsRecv, &lastSeenSQL)
		var lastSeenPtr *int64
		if lastSeenSQL.Valid {
			v := lastSeenSQL.Int64
			lastSeenPtr = &v
		}
		follows = append(follows, FollowInfo{
			ID: f.id, RemotePeerID: f.remotePeerID.String(),
			LocalTagID: f.localTagID, LocalTagName: localTagName,
			Status: f.status,
			ClipsReceived: clipsRecv,
			LastSeq: lastSeqDB, LastSeenAt: lastSeenPtr,
			CreatedAt: createdAt,
		})
	}
	return
}
```

- [ ] **Step 2: Commit.**

```bash
git add share_manager.go
git commit -m "feat(share): add GetShareStatus aggregating publications + follows"
```

### Task 8.4: ShareService Wails binding

**Files:**
- Create: `share_service.go`
- Modify: `main.go`

- [ ] **Step 1: Implement service.**

`share_service.go`:

```go
package main

import "fmt"

// ShareService exposes share operations to the frontend via Wails.
type ShareService struct {
	app *App
}

func NewShareService(app *App) *ShareService { return &ShareService{app: app} }

func (s *ShareService) StartShare(tagID int64) (ShareInfo, error) {
	if s.app.shareManager == nil {
		return ShareInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.StartShare(tagID)
}

func (s *ShareService) StopShare(tagID int64) error {
	if s.app.shareManager == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.StopShare(tagID)
}

func (s *ShareService) Follow(shareString, localTagName string) (FollowInfo, error) {
	if s.app.shareManager == nil {
		return FollowInfo{}, fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.Follow(shareString, localTagName)
}

func (s *ShareService) Unfollow(followID int64) error {
	if s.app.shareManager == nil {
		return fmt.Errorf("share manager not initialized")
	}
	return s.app.shareManager.Unfollow(followID)
}

type ShareStatus struct {
	Shares  []ShareInfo  `json:"shares"`
	Follows []FollowInfo `json:"follows"`
}

func (s *ShareService) GetShareStatus() ShareStatus {
	if s.app.shareManager == nil {
		return ShareStatus{Shares: []ShareInfo{}, Follows: []FollowInfo{}}
	}
	ss, ff := s.app.shareManager.GetShareStatus()
	if ss == nil { ss = []ShareInfo{} }
	if ff == nil { ff = []FollowInfo{} }
	return ShareStatus{Shares: ss, Follows: ff}
}
```

- [ ] **Step 2: Register in main.go.**

Edit `main.go`:
- After `apiService := NewAPIService(app)` add: `shareService := NewShareService(app)`.
- Add `shareService` to the `Bind:` slice.

- [ ] **Step 3: Wire ShareManager into App.**

In `app.go`, add field `shareManager *ShareManager` to the `App` struct definition. Locate it (search for the struct — near the top of `app.go`) and add the field alongside `serveManager`, `pluginManager`, etc.

- [ ] **Step 4: Build.**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit.**

```bash
git add share_service.go main.go app.go
git commit -m "feat(share): add ShareService Wails binding and register in main"
```

### Task 8.5: Wire init/shutdown/hooks in App

**Files:**
- Modify: `app.go`

- [ ] **Step 1: Init in `startup`.**

In `app.go:startup`, after `a.serveManager = NewServeManager(a)` (~line 169), add:

```go
// Initialize share manager
dataDir, _ := getDataDir()
sm, smErr := NewShareManager(ctx, a.db, dataDir)
if smErr != nil {
	log.Printf("Warning: Failed to initialize share manager: %v", smErr)
} else {
	a.shareManager = sm
	// Push Wails events to the frontend from the manager.
	sm.SetEventFn(func(name string, data ...any) {
		if len(data) == 1 {
			runtime.EventsEmit(ctx, name, data[0])
		} else {
			runtime.EventsEmit(ctx, name, data...)
		}
	})
	if err := sm.ResumeAll(); err != nil {
		log.Printf("Warning: ShareManager ResumeAll: %v", err)
	}
	sm.startSweepers()
}
```

- [ ] **Step 2: Shutdown.**

In `app.go:shutdown` (~line 230), before `a.db.Close()`, add:

```go
if a.shareManager != nil {
	a.shareManager.Stop()
}
```

- [ ] **Step 3: Hook OnClipCreated.**

Find every clip-insert site in `app.go` (the grep results: line 806 is one; search for `INSERT INTO clips` and `AddTextClip` / `UploadFileAndGetID` etc.). After each successful insert + tag association, add:

```go
if a.shareManager != nil {
	go func(id int64, tags []int64) {
		if err := a.shareManager.OnClipCreated(id, tags); err != nil {
			log.Printf("share: OnClipCreated(%d): %v", id, err)
		}
	}(clipID, tagIDsToUse)
}
```

Collect `tagIDsToUse` from the site: in `UploadFiles` the auto-tag ID is available as `autoTagID`, in `AddTextClip` etc. the tag set needs a fresh query — use:

```go
tagIDs, _ := a.getTagIDsForClip(clipID) // existing helper, or add it
```

Add helper `getTagIDsForClip` in app.go if missing:

```go
func (a *App) getTagIDsForClip(clipID int64) ([]int64, error) {
	rows, err := a.db.Query(`SELECT tag_id FROM clip_tags WHERE clip_id = ?`, clipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Hook DeleteTag.**

In `DeleteTag` (app.go:1410), before `DELETE FROM tags`, add:

```go
if a.shareManager != nil {
	_ = a.shareManager.StopShare(id) // no-op if not shared; errors logged, not returned
}
```

- [ ] **Step 5: Regenerate Wails bindings.**

Run: `make bindings`

This updates `frontend/wailsjs/` with the new service methods.

- [ ] **Step 6: Build + smoke.**

Run: `go build ./... && make bindings`
Expected: success.

- [ ] **Step 7: Commit.**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat(share): wire ShareManager into App lifecycle, clip-create, tag-delete"
```

---

## Phase 9 — Frontend: sidebar 2×2 grid + Share tab + Share view markup

### Task 9.1: Change drawer tablist to 2×2 grid + add Share tab

**Files:**
- Modify: `frontend/index.html` (lines ~132-167)

- [ ] **Step 1: Edit the tablist.**

Replace the current `<div class="flex rounded-lg border border-stone-200 overflow-hidden" role="tablist">` wrapper with:

```html
<div class="grid grid-cols-2 rounded-lg border border-stone-200 overflow-hidden" role="tablist" aria-label="View switcher">
```

Inside, the existing three tab buttons stay; append a fourth:

```html
<button id="view-tab-share" role="tab" aria-selected="false"
    class="relative flex-1 flex flex-col items-center gap-1 py-3 text-stone-400 hover:bg-stone-100 transition-colors"
    data-view="share">
    <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m9.632 4.026A3 3 0 1115 15a3 3 0 013.316 2.342M15 9a3 3 0 11-.684-1.658M6 15l6-3m0 0l6-3m-6 3v6"/>
    </svg>
    <span class="text-[10px] font-medium">Share</span>
    <span id="share-indicator" class="hidden absolute top-1.5 right-1.5 w-2.5 h-2.5 bg-emerald-500 rounded-full border-2 border-white"></span>
</button>
```

With `grid grid-cols-2`, four tabs naturally lay out 2×2. Verify inner dividers still look right (may need `border-r border-stone-200` on alternate children, or adjust via CSS — keep the existing `overflow-hidden` container to clip stray borders).

- [ ] **Step 2: Open the app and visually verify (manual).**

Run: `make dev` (or `make install` → launch app). Open nav drawer. Confirm 2×2 tabs render, Share is bottom-right, visible, labelled.

Stop dev server when done.

- [ ] **Step 3: Commit.**

```bash
git add frontend/index.html
git commit -m "feat(share): drawer tablist in 2x2 grid with new Share tab"
```

### Task 9.2: Add Share view section markup

**Files:**
- Modify: `frontend/index.html`

- [ ] **Step 1: Append Share view.**

Immediately after the existing `<section id="serve-view">...</section>` block, insert:

```html
<!-- Share View (hidden by default) -->
<section id="share-view" class="hidden mb-10" aria-labelledby="share-heading">
    <h2 id="share-heading" class="sr-only">Share Tags</h2>

    <button id="share-back-btn"
        class="text-xs font-medium text-stone-500 hover:text-stone-700 transition-colors flex items-center gap-1.5 mb-4"
        style="--wails-draggable: no-drag">
        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 19l-7-7 7-7"/>
        </svg>
        Back to Pastes
    </button>

    <h3 class="text-xs font-semibold text-stone-400 uppercase tracking-wide mb-3">Sharing</h3>
    <ul id="share-publications-list" class="space-y-3 mb-4">
        <!-- cards injected by share.js -->
    </ul>
    <div class="border border-dashed border-stone-300 rounded-lg p-6 text-center transition-all hover:border-stone-400 hover:bg-stone-50/50 mb-10">
        <button id="add-share-btn"
            class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
            + Share a Tag
        </button>
    </div>

    <h3 class="text-xs font-semibold text-stone-400 uppercase tracking-wide mb-3">Following</h3>
    <ul id="share-follows-list" class="space-y-3 mb-4">
        <!-- cards injected by share.js -->
    </ul>
    <div class="border border-dashed border-stone-300 rounded-lg p-6 text-center transition-all hover:border-stone-400 hover:bg-stone-50/50">
        <p class="text-sm text-stone-500 mb-3">Paste a share link from someone to start following</p>
        <button id="add-follow-btn"
            class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
            + Follow a Share
        </button>
    </div>
</section>
```

- [ ] **Step 2: Commit.**

```bash
git add frontend/index.html
git commit -m "feat(share): add Share view section markup"
```

### Task 9.3: Add Create-Share and Follow-Share modal markup

**Files:**
- Modify: `frontend/index.html`

- [ ] **Step 1: Append near other modal markup at end of body.**

Find the block of existing modals (search for `id="serve-modal"` or similar `<div class="fixed inset-0` modals near the end of `<body>`). Append:

```html
<!-- Create-Share Modal -->
<div id="create-share-modal" class="hidden fixed inset-0 z-[60] bg-stone-900/40 backdrop-blur-sm flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="create-share-title">
    <div class="bg-white rounded-lg border border-stone-200 shadow-xl max-w-md w-full p-5">
        <div class="flex justify-between items-center mb-3">
            <h4 id="create-share-title" class="text-xs font-semibold text-stone-400 uppercase tracking-wide">Share a tag</h4>
            <button class="create-share-close p-1 hover:bg-stone-100 rounded" aria-label="Close">
                <svg class="w-4 h-4 text-stone-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/></svg>
            </button>
        </div>

        <div id="create-share-picker-section">
            <label class="block text-xs font-medium text-stone-600 mb-1">Tag</label>
            <select id="create-share-tag-select"
                class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-2 px-2 mb-3"></select>
            <div class="flex justify-end gap-2">
                <button class="create-share-close border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md">Cancel</button>
                <button id="create-share-confirm-btn" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md">Create</button>
            </div>
        </div>

        <div id="create-share-result-section" class="hidden">
            <label class="block text-xs font-medium text-stone-600 mb-1">Share link</label>
            <p class="text-[11px] text-stone-500 mb-2">Anyone with this link can follow. Forward-only — they won't see past clips.</p>
            <div class="bg-stone-50 border border-stone-200 rounded-md p-3 text-[10px] font-mono text-stone-800 break-all mb-2" id="create-share-string-box" tabindex="0"></div>
            <div id="create-share-qr-box" class="hidden mx-auto my-2 bg-white p-2 border border-stone-200 w-fit"></div>
            <div class="bg-amber-50 border border-amber-200 rounded-md px-3 py-2 text-[10px] text-amber-700 mb-3">⚠ Treat this like a password. It contains the decryption key.</div>
            <div class="flex justify-end gap-2">
                <button id="create-share-copy-btn" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md">📋 Copy link</button>
                <button id="create-share-qr-toggle-btn" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md">Show QR</button>
                <button class="create-share-close border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md">Done</button>
            </div>
        </div>
    </div>
</div>

<!-- Follow-Share Modal -->
<div id="follow-share-modal" class="hidden fixed inset-0 z-[60] bg-stone-900/40 backdrop-blur-sm flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="follow-share-title">
    <div class="bg-white rounded-lg border border-stone-200 shadow-xl max-w-md w-full p-5">
        <div class="flex justify-between items-center mb-3">
            <h4 id="follow-share-title" class="text-xs font-semibold text-stone-400 uppercase tracking-wide">Follow a share</h4>
            <button class="follow-share-close p-1 hover:bg-stone-100 rounded" aria-label="Close">
                <svg class="w-4 h-4 text-stone-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/></svg>
            </button>
        </div>
        <label class="block text-xs font-medium text-stone-600 mb-1">Paste share link</label>
        <textarea id="follow-share-string" rows="3"
            class="block w-full border border-stone-200 rounded-md text-xs bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-2 px-2 mb-3 font-mono"
            placeholder="mp-share:v1:..."></textarea>
        <div id="follow-share-tag-section" class="hidden">
            <label class="block text-xs font-medium text-stone-600 mb-1">Assign incoming clips to local tag</label>
            <input id="follow-share-local-tag" type="text" required
                class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-2 px-2 mb-3"
                placeholder="e.g. shared/from-alice">
        </div>
        <p id="follow-share-error" class="hidden text-[11px] text-red-600 mb-2"></p>
        <div class="flex justify-end gap-2">
            <button class="follow-share-close border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md">Cancel</button>
            <button id="follow-share-confirm-btn" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md" disabled>Follow</button>
        </div>
    </div>
</div>
```

- [ ] **Step 2: Commit.**

```bash
git add frontend/index.html
git commit -m "feat(share): add Create-Share and Follow-Share modal markup"
```

---

## Phase 10 — Frontend: share.js + view switcher + QR

### Task 10.1: Vendor a QR library

**Files:**
- Create: `frontend/js/vendor/qrcode.min.js`

- [ ] **Step 1: Download a vetted QR library.**

Use `qrcode-generator` (tiny, permissive, no deps). Download the UMD build to `frontend/js/vendor/qrcode.min.js`:

```bash
mkdir -p frontend/js/vendor
curl -Lo frontend/js/vendor/qrcode.min.js https://cdn.jsdelivr.net/npm/qrcode-generator@1.4.4/qrcode.min.js
```

Verify file is ≤ 15 KB and contains `qrcode` export.

- [ ] **Step 2: Reference it in `frontend/index.html`.**

Near the bottom, alongside other `<script src>` tags, add:

```html
<script src="js/vendor/qrcode.min.js"></script>
```

- [ ] **Step 3: Commit.**

```bash
git add frontend/js/vendor/qrcode.min.js frontend/index.html
git commit -m "feat(share): vendor qrcode-generator for client-side QR rendering"
```

### Task 10.2: Create share.js — view renderer + event wiring

**Files:**
- Create: `frontend/js/share.js`
- Modify: `frontend/index.html` (add `<script src="js/share.js">`)
- Modify: `frontend/js/app.js` (view switcher)

- [ ] **Step 1: Write share.js.**

```js
// Share view renderer + modal logic. Depends on window.go.main.ShareService
// and the vendored qrcode global.
(function () {
  const pubList = document.getElementById('share-publications-list');
  const followList = document.getElementById('share-follows-list');
  const shareIndicator = document.getElementById('share-indicator');

  const createModal = document.getElementById('create-share-modal');
  const pickerSec = document.getElementById('create-share-picker-section');
  const resultSec = document.getElementById('create-share-result-section');
  const tagSelect = document.getElementById('create-share-tag-select');
  const stringBox = document.getElementById('create-share-string-box');
  const qrBox = document.getElementById('create-share-qr-box');
  const confirmBtn = document.getElementById('create-share-confirm-btn');
  const copyBtn = document.getElementById('create-share-copy-btn');
  const qrToggleBtn = document.getElementById('create-share-qr-toggle-btn');

  const followModal = document.getElementById('follow-share-modal');
  const followString = document.getElementById('follow-share-string');
  const followTagSec = document.getElementById('follow-share-tag-section');
  const followTagInput = document.getElementById('follow-share-local-tag');
  const followErr = document.getElementById('follow-share-error');
  const followConfirmBtn = document.getElementById('follow-share-confirm-btn');

  const addShareBtn = document.getElementById('add-share-btn');
  const addFollowBtn = document.getElementById('add-follow-btn');

  function relTime(sec) {
    const d = Math.floor(Date.now()/1000) - sec;
    if (d < 60) return `${d}s ago`;
    if (d < 3600) return `${Math.floor(d/60)}m ago`;
    if (d < 86400) return `${Math.floor(d/3600)}h ago`;
    return `${Math.floor(d/86400)}d ago`;
  }

  function updateIndicator(hasAny) {
    if (hasAny) shareIndicator.classList.remove('hidden');
    else shareIndicator.classList.add('hidden');
  }

  function renderPublications(shares) {
    pubList.innerHTML = '';
    shares.forEach(s => {
      const li = document.createElement('li');
      li.className = 'bg-white border border-stone-200 rounded-md px-4 py-3 flex items-center justify-between gap-3';
      li.innerHTML = `
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium text-stone-800 truncate">${s.tag_name}${s.status === 'invalid' ? ' <span class=\"text-amber-700\">(Re-share needed)</span>' : ''}</div>
          <div class="text-[11px] text-stone-500">${s.followers} followers · ${s.clips_pushed} clips pushed · since ${relTime(s.created_at)}</div>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="share-copy-link border border-stone-200 hover:bg-stone-100 text-stone-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-id="${s.id}" data-share="${s.share_string}">Copy link</button>
          <button class="share-stop border border-stone-200 hover:bg-red-50 hover:border-red-300 text-stone-600 hover:text-red-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-tagid="${s.tag_id}">Stop</button>
        </div>`;
      pubList.appendChild(li);
    });
  }

  function renderFollows(follows) {
    followList.innerHTML = '';
    follows.forEach(f => {
      const li = document.createElement('li');
      li.className = 'bg-white border border-stone-200 rounded-md px-4 py-3 flex items-center justify-between gap-3';
      const status = f.status === 'connected' ? '<span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1.5"></span>Connected'
        : f.status === 'connected_relayed' ? '<span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1.5"></span>Connected (relayed)'
        : '<span class="inline-block w-1.5 h-1.5 rounded-full bg-stone-400 mr-1.5"></span>Offline · will resume';
      li.innerHTML = `
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium text-stone-800 truncate">${f.local_tag_name}</div>
          <div class="text-[11px] text-stone-500">${status} · ${f.clips_received} clips received · since ${relTime(f.created_at)}</div>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="share-unfollow border border-stone-200 hover:bg-red-50 hover:border-red-300 text-stone-600 hover:text-red-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-id="${f.id}">Unfollow</button>
        </div>`;
      followList.appendChild(li);
    });
  }

  async function refresh() {
    try {
      const status = await window.go.main.ShareService.GetShareStatus();
      renderPublications(status.shares || []);
      renderFollows(status.follows || []);
      updateIndicator((status.shares && status.shares.length > 0) || (status.follows && status.follows.length > 0));
    } catch (e) {
      console.error('share: refresh failed', e);
    }
  }

  // Open create-share modal: fill tag picker fresh each time.
  addShareBtn.addEventListener('click', async () => {
    try {
      const tags = await window.go.main.App.GetTags();
      tagSelect.innerHTML = '';
      (tags || []).forEach(t => {
        const o = document.createElement('option');
        o.value = t.id; o.textContent = t.name;
        tagSelect.appendChild(o);
      });
      pickerSec.classList.remove('hidden');
      resultSec.classList.add('hidden');
      qrBox.classList.add('hidden');
      qrBox.innerHTML = '';
      createModal.classList.remove('hidden');
    } catch (e) { console.error(e); }
  });

  document.querySelectorAll('.create-share-close').forEach(b => b.addEventListener('click', () => {
    createModal.classList.add('hidden');
  }));

  confirmBtn.addEventListener('click', async () => {
    const tagID = parseInt(tagSelect.value, 10);
    try {
      const info = await window.go.main.ShareService.StartShare(tagID);
      stringBox.textContent = info.share_string;
      pickerSec.classList.add('hidden');
      resultSec.classList.remove('hidden');
      await refresh();
    } catch (e) {
      alert('Failed to start share: ' + e);
    }
  });

  copyBtn.addEventListener('click', async () => {
    const text = stringBox.textContent;
    try {
      await navigator.clipboard.writeText(text);
      copyBtn.textContent = 'Copied ✓';
      setTimeout(() => copyBtn.textContent = '📋 Copy link', 1500);
    } catch (e) {
      console.error('copy failed', e);
    }
  });

  qrToggleBtn.addEventListener('click', () => {
    if (!qrBox.classList.contains('hidden')) {
      qrBox.classList.add('hidden');
      qrBox.innerHTML = '';
      qrToggleBtn.textContent = 'Show QR';
      return;
    }
    const text = stringBox.textContent;
    const qr = window.qrcode(0, 'L');
    qr.addData(text);
    qr.make();
    qrBox.innerHTML = qr.createSvgTag({ scalable: true, margin: 2 });
    qrBox.classList.remove('hidden');
    qrToggleBtn.textContent = 'Hide QR';
  });

  // Publication list actions (delegation).
  pubList.addEventListener('click', async (e) => {
    const copy = e.target.closest('.share-copy-link');
    const stop = e.target.closest('.share-stop');
    if (copy) {
      try {
        await navigator.clipboard.writeText(copy.dataset.share);
        copy.textContent = 'Copied ✓';
        setTimeout(() => copy.textContent = 'Copy link', 1500);
      } catch (err) { console.error(err); }
      return;
    }
    if (stop) {
      const tagID = parseInt(stop.dataset.tagid, 10);
      if (!confirm('Stop sharing? Existing followers will disconnect and the link will stop working.')) return;
      try {
        await window.go.main.ShareService.StopShare(tagID);
        await refresh();
      } catch (err) {
        alert('Stop failed: ' + err);
      }
    }
  });

  // Follow modal
  addFollowBtn.addEventListener('click', () => {
    followString.value = '';
    followTagInput.value = '';
    followErr.classList.add('hidden');
    followTagSec.classList.add('hidden');
    followConfirmBtn.disabled = true;
    followModal.classList.remove('hidden');
  });
  document.querySelectorAll('.follow-share-close').forEach(b => b.addEventListener('click', () => {
    followModal.classList.add('hidden');
  }));

  function parseShareStringClientSide(s) {
    if (!s || !s.startsWith('mp-share:v1:')) return null;
    const blob = s.substring('mp-share:v1:'.length);
    // Base64url charset check
    if (!/^[A-Za-z0-9_-]+$/.test(blob)) return null;
    return true; // opaque body validated server-side
  }

  followString.addEventListener('input', () => {
    const ok = parseShareStringClientSide(followString.value.trim());
    if (ok) {
      followTagSec.classList.remove('hidden');
      followErr.classList.add('hidden');
      followConfirmBtn.disabled = false;
    } else {
      followTagSec.classList.add('hidden');
      followConfirmBtn.disabled = true;
      if (followString.value.trim().length > 0) {
        followErr.textContent = 'Not a valid share link';
        followErr.classList.remove('hidden');
      } else {
        followErr.classList.add('hidden');
      }
    }
  });

  followConfirmBtn.addEventListener('click', async () => {
    const s = followString.value.trim();
    const tagName = followTagInput.value.trim();
    if (!tagName) { followErr.textContent = 'Local tag name required'; followErr.classList.remove('hidden'); return; }
    try {
      await window.go.main.ShareService.Follow(s, tagName);
      followModal.classList.add('hidden');
      await refresh();
    } catch (e) {
      followErr.textContent = String(e);
      followErr.classList.remove('hidden');
    }
  });

  followList.addEventListener('click', async (e) => {
    const un = e.target.closest('.share-unfollow');
    if (!un) return;
    const id = parseInt(un.dataset.id, 10);
    if (!confirm('Unfollow this share? Already-received clips stay.')) return;
    try {
      await window.go.main.ShareService.Unfollow(id);
      await refresh();
    } catch (e) {
      alert('Unfollow failed: ' + e);
    }
  });

  // Re-render on backend events.
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('share:publication-updated', refresh);
    window.runtime.EventsOn('share:publication-removed', refresh);
    window.runtime.EventsOn('share:follow-updated', refresh);
    window.runtime.EventsOn('share:follow-removed', refresh);
  }

  // Expose for view switcher.
  window.ShareView = { refresh };

  // Back button behaves like other views — flip to clips.
  const backBtn = document.getElementById('share-back-btn');
  if (backBtn) backBtn.addEventListener('click', () => {
    const clipsTab = document.getElementById('view-tab-clips');
    if (clipsTab) clipsTab.click();
  });
})();
```

- [ ] **Step 2: Reference in index.html.**

Next to the other `<script src="js/..."` tags, add:

```html
<script src="js/share.js"></script>
```

Ensure this appears **after** `js/vendor/qrcode.min.js` and after any `wailsjs/go/main/ShareService.js` generated bindings.

- [ ] **Step 3: Hook view switcher in app.js.**

Find the view-switcher block (grep for `view-tab-clips` / `data-view`). There's an existing handler that toggles `#clips-view`, `#watch-view`, `#serve-view`. Add the `share` case:

```js
// In the view switcher, e.g. after:
//   else if (view === 'serve') { show('#serve-view'); }
else if (view === 'share') {
    show('#share-view');
    if (window.ShareView) window.ShareView.refresh();
}
```

(Adapt to the exact pattern — read `frontend/js/app.js` and the existing `show(...)` helper.)

- [ ] **Step 4: Smoke-run.**

Run: `make dev`. Open app → drawer → Share tab. Verify empty view renders with both add-zones; click `+ Share a Tag` opens the modal. Cancel. Click `+ Follow a Share` opens modal with disabled Follow button until text pasted.

- [ ] **Step 5: Commit.**

```bash
git add frontend/js/share.js frontend/index.html frontend/js/app.js
git commit -m "feat(share): implement share.js (Share view, modals, events, QR)"
```

---

## Phase 11 — Backup/restore identity handling

### Task 11.1: Include share_identity.key in backups

**Files:**
- Modify: `backup.go`

- [ ] **Step 1: Read existing backup.go to learn the ZIP writer conventions.**

Run: `grep -n "zip\\.Writer\\|zip.Reader\\|AddFile\\|clips.db" backup.go | head -20`

- [ ] **Step 2: In the backup creation function, before closing the ZIP, append the identity file if it exists.**

Apply an Edit that reads like:

```go
// Include share_identity.key if present (spec §7.3).
if idPath := filepath.Join(dataDir, ShareIdentityFile); fileExists(idPath) {
	if err := addFileToZip(zw, idPath, ShareIdentityFile); err != nil {
		log.Printf("backup: include identity: %v", err)
	}
}
```

where `addFileToZip` is either an existing helper in backup.go or a small inline helper — follow whatever pattern the current ZIP code uses.

- [ ] **Step 3: Commit.**

```bash
git add backup.go
git commit -m "feat(share): include share_identity.key in backup ZIP"
```

### Task 11.2: Restore prompt + three-way identity choice

**Files:**
- Modify: `backup.go`, `app.go`
- Modify: `frontend/js/settings.js` (or wherever backup restore UI lives)

**Key insight about mahpastes restore semantics.** `backup.RestoreBackup` is a **full DB replace** — after it returns, every row in `shares` / `follows` / `share_ring` came from the backup. So any post-restore DML against those tables is operating on backup rows, not on the install's prior rows. The "takeover" path therefore must NOT run `UPDATE shares SET status = 'invalid'` after restore — that would invalidate exactly the publications the user wants to keep. Instead, capture whatever warning information the UI needs *before* restore, and use the post-restore phase only for the `keep` path (where restored-but-foreign-identity shares genuinely are invalid on this install).

- [ ] **Step 1: Add `BackupInspect` that captures pre-restore state.**

```go
// BackupInspection is the read-only snapshot shown to the user before
// they commit to a restore. It carries:
//   - whether the backup contains an identity at all
//   - whether this install already has an identity (collision signal)
//   - the tag names of publications that will be lost if restore proceeds
//     (so the UI can list them under the "takeover" warning path)
type BackupInspection struct {
	HasIdentity           bool     `json:"has_identity"`
	TargetHasIdentity     bool     `json:"target_has_identity"`
	TargetPublicationTags []string `json:"target_publication_tags"`
}

func (a *App) BackupInspect(zipPath string) (BackupInspection, error) {
	var out BackupInspection
	dataDir, _ := getDataDir()
	if fileExists(filepath.Join(dataDir, ShareIdentityFile)) {
		out.TargetHasIdentity = true
	}

	// Capture the current install's active publications BEFORE any write.
	rows, err := a.db.Query(`SELECT t.name FROM shares s JOIN tags t ON t.id = s.tag_id WHERE s.status = 'active' ORDER BY t.name`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				out.TargetPublicationTags = append(out.TargetPublicationTags, n)
			}
		}
	}

	// Crack the ZIP, look for the identity entry.
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return out, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == ShareIdentityFile {
			out.HasIdentity = true
			break
		}
	}
	return out, nil
}
```

- [ ] **Step 2: Extend the existing restore function to accept an `identityPolicy` string: `"takeover" | "keep" | "none"`.**

```go
func (a *App) RestoreBackup(zipPath, identityPolicy string) error {
	dataDir, _ := getDataDir()

	// Replace the DB (existing full-restore logic unchanged). After this
	// returns, every row in shares/follows/share_ring came from the backup.
	if err := restoreDBFromZip(zipPath); err != nil {
		return err
	}

	switch identityPolicy {
	case "takeover":
		// Extract share_identity.key from the ZIP and overwrite the local
		// copy. Backup rows in `shares` are valid under this identity —
		// leave `status` alone. Any warning about the install's PRIOR
		// publications is surfaced in the UI from BackupInspection data,
		// captured before restore ran.
		if err := extractIdentityFromZip(zipPath, filepath.Join(dataDir, ShareIdentityFile)); err != nil {
			return err
		}
	case "keep":
		// Backup's identity file is discarded. Restored `shares` rows came
		// from a different peer-id and won't work under ours — flag them so
		// the UI shows the Re-share CTA.
		if _, err := a.db.Exec(`UPDATE shares SET status = 'invalid'`); err != nil {
			return err
		}
	case "none":
		// Target had no prior identity. Extract the backup's identity (if
		// any). Restored `shares` rows, if present, now work under that
		// identity — no invalidation.
		if fileExists(filepath.Join(dataDir, ShareIdentityFile)) == false {
			// only extract if still missing (race-safe)
			_ = extractIdentityFromZip(zipPath, filepath.Join(dataDir, ShareIdentityFile))
		}
	default:
		return fmt.Errorf("unknown identity policy: %q", identityPolicy)
	}
	return nil
}
```

- [ ] **Step 3: Wire the frontend restore flow.**

In the existing restore UI (search `RestoreBackup` in `frontend/js`), call `BackupInspect` first and feed its `target_publication_tags` into the takeover warning panel:

```js
const insp = await window.go.main.App.BackupInspect(zipPath);
let policy = 'none';
if (insp.target_has_identity && insp.has_identity) {
  // Modal with three radios. The "Take over" option shows the captured
  // list of this install's current publication tags so the user knows
  // what will stop working after restore.
  policy = await promptIdentityPolicy(insp.target_publication_tags);
  if (policy === 'cancel') return;
}
await window.go.main.App.RestoreBackup(zipPath, policy);
```

`promptIdentityPolicy(currentTags: string[])` renders a modal like:
```
This install already has a share identity.
Choose how to handle it:

(o) Take over — use the backup's identity
    ⚠ These publications from this install will stop working:
    • work/recipes
    • inbox/from-alice

( ) Keep this install's identity
    Restored shares will need to be re-shared.

( ) Cancel restore
```

- [ ] **Step 4: Regenerate bindings.**

Run: `make bindings`

- [ ] **Step 5: Build + commit.**

```bash
git add backup.go app.go frontend/js/settings.js frontend/wailsjs/
git commit -m "feat(share): identity-takeover prompt on backup restore"
```

---

## Phase 12 — E2E tests (batched)

Each E2E spec is its own file under `e2e/tests/share/`. Follow the pattern in `e2e/tests/serve/`. All spec files share imports:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data';
import * as path from 'path';
```

**AppHelper additions** — you'll need a small set of new helpers in `e2e/fixtures/test-fixtures.ts`. Add alongside `startServingTag`:

```typescript
async startShare(tagName: string): Promise<{ shareString: string; tagID: number }> {
  // Click through the Create-Share modal; parse the resulting string box.
  await this.page.click('#view-tab-share');
  await this.page.click('#add-share-btn');
  await this.page.selectOption('#create-share-tag-select', { label: tagName });
  await this.page.click('#create-share-confirm-btn');
  await this.page.waitForSelector('#create-share-result-section:not(.hidden)');
  const shareString = (await this.page.textContent('#create-share-string-box') || '').trim();
  await this.page.click('.create-share-close');
  // Look up tagID via API
  const tagID = await this.page.evaluate((n) =>
    window.go.main.App.GetTags().then((tags: any[]) => (tags.find(t => t.name === n)?.id) || 0),
  tagName);
  return { shareString, tagID };
}

async stopShare(tagID: number): Promise<void> {
  await this.page.evaluate((id) => window.go.main.ShareService.StopShare(id), tagID);
}

async followShare(shareString: string, localTagName: string): Promise<void> {
  await this.page.click('#view-tab-share');
  await this.page.click('#add-follow-btn');
  await this.page.fill('#follow-share-string', shareString);
  await this.page.fill('#follow-share-local-tag', localTagName);
  await this.page.click('#follow-share-confirm-btn');
  await this.page.waitForSelector('#follow-share-modal.hidden');
}

async getShareStatus(): Promise<any> {
  return await this.page.evaluate(() => window.go.main.ShareService.GetShareStatus());
}
```

### Task 12.1: Core follow spec

**Files:**
- Create: `e2e/tests/share/share-follow.spec.ts`

- [ ] **Step 1: Write the test.**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Share - follow and receive', () => {
  test('follower receives a PNG clip within N seconds', async ({ app, browser }) => {
    // Publisher = `app`.
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('recipes');
    await app.addTagToClip(path.basename(img), 'recipes');
    const { shareString } = await app.startShare('recipes');

    // Spin up a second app instance via the test fixture.
    // The existing AppHelper supports one instance per worker; we need a
    // second-context fallback — use a fresh browser context if the fixture
    // supports `createSecondApp()`. If not, skip this test or refactor.
    const app2 = await app.spawnSecondary(); // add this helper in Task 12.0
    await app2.followShare(shareString, 'inbox');

    // Publisher adds a new clip
    const img2 = await createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png');
    await app.uploadFile(img2);
    await app.addTagToClip(path.basename(img2), 'recipes');

    // Poll follower for clip arrival
    const deadline = Date.now() + 15000;
    let found = false;
    while (Date.now() < deadline) {
      await app2.page.click('#view-tab-clips');
      const count = await app2.expectClipCount(1, 'soft' as any).catch(() => -1);
      if (count !== -1) { found = true; break; }
      await app2.page.waitForTimeout(500);
    }
    expect(found).toBeTruthy();
  });
});
```

- [ ] **Step 2: Add `spawnSecondary()` helper.**

In `e2e/fixtures/test-fixtures.ts`, add a method on `AppHelper` that launches a second mahpastes instance in a separate context (reusing the existing launcher logic). Because this introduces parallelism the fixture didn't previously support, this is the largest real addition to the e2e framework. Expected implementation: extract the existing per-worker launch into a function and call it twice in `spawnSecondary()`.

If introducing this helper is too invasive, mark `share-follow.spec.ts` as skipped with a TODO and rely on Go integration tests (Task 5.2 + 7.1) for the wire-level coverage; run just the UI pieces via `share-ui.spec.ts`.

- [ ] **Step 3: Run.**

Run: `cd e2e && npm test -- share/share-follow.spec.ts 2>&1 | tail -40`

- [ ] **Step 4: Commit.**

```bash
git add e2e/tests/share/share-follow.spec.ts e2e/fixtures/test-fixtures.ts
git commit -m "test(share): e2e follow + receive clip"
```

### Task 12.2: Remaining E2E specs

Write each of the following spec files following the same pattern. Each task = 1 file + run + commit; keep them small and focused.

- [ ] `share-create.spec.ts` — Create share, verify string appears; stop, verify `GetShareStatus().shares.length === 0`.
- [ ] `share-large-clip.spec.ts` — Push an 8 MiB PNG via `generateTestImage(2048, 2048)`; assert follower receives with matching byte length.
- [ ] `share-offline-catchup.spec.ts` — Genuine reconnect semantics, not Unfollow/Follow.

  **Why not Unfollow/Follow:** `Unfollow` deletes the `follows` row including `last_seq`. A subsequent `Follow` starts a brand-new subscription with `since_seq = 0`, so the ring's catch-up path is never exercised. The test must preserve the follow row and only sever the transport.

  **Required test-only hook** — add to `share_service.go` behind a `// test-only` comment:

  ```go
  // DisconnectFollowForTest closes the in-flight follow stream without
  // removing the follows row, so the next reconnect exercises the real
  // since_seq-based catch-up path. Exposed for e2e tests that can't kill
  // libp2p connections from the outside.
  func (s *ShareService) DisconnectFollowForTest(followID int64) error {
  	if s.app.shareManager == nil { return fmt.Errorf("share manager not initialized") }
  	return s.app.shareManager.DisconnectFollowForTest(followID)
  }
  ```

  And on `ShareManager`:
  ```go
  // DisconnectFollowForTest cancels the follow's current session context so
  // the runFollowLoop goroutine drops the stream; the row stays, last_seq
  // stays, and the reconnect backoff loop will redial.
  func (m *ShareManager) DisconnectFollowForTest(id int64) error {
  	m.mu.RLock()
  	f, ok := m.follows[id]
  	m.mu.RUnlock()
  	if !ok { return fmt.Errorf("no follow %d", id) }
  	// Replace the session context with a fresh one so the current session
  	// exits but the follow keeps reconnecting.
  	oldCancel := f.cancel
  	ctx, cancel := context.WithCancel(m.ctx)
  	f.ctx = ctx
  	f.cancel = cancel
  	oldCancel() // unblocks consumeStream and triggers reconnect
  	return nil
  }
  ```

  **Test body:**
  1. Publisher A shares tag; follower B follows with `local = 'inbox'`.
  2. Wait for first clip to arrive (verify transport works).
  3. `DisconnectFollowForTest(followID)` on B. Assert connection status flips to `offline`.
  4. A pushes a new clip while B is disconnected. Verify it lands in A's `share_ring`.
  5. Poll B for up to 10s; assert it auto-reconnects (backoff) and the new clip arrives with `since_seq` matching the pre-disconnect `last_seq + expected-envelope-count`.
  6. Verify B's `follows.last_seq` advanced monotonically (did not reset to 0).
- [ ] `share-offline-dropped.spec.ts` — Direct SQL: `UPDATE share_ring SET ts = ts - 7200` to age all rows; reconnect; assert follower does NOT receive the pre-aged clips.
- [ ] `share-snapshot.spec.ts` — Publisher pushes clip; deletes/renames the source clip; follower reconnects — receives original bytes.
- [ ] `share-publisher-restart.spec.ts` — publisher pushes; stop+restart the App (use whatever `app.restart()` helper exists or add one); follower reconnects within 1h — receives.
- [ ] `share-address-change.spec.ts` — harder to simulate; defer or skip with TODO if no easy hook. Assertion: after a simulated publisher host restart (new random ports), follower's DHT-backed reconnect still succeeds.
- [ ] `share-persistence.spec.ts` — Create share; restart app; verify same share string still in `GetShareStatus`.
- [ ] `share-restore.spec.ts` — Backup+restore flow, both `takeover` and `keep` paths; assert correct `status` in the UI.
- [ ] `share-ui.spec.ts` — Sidebar 2×2 grid renders (all four tabs present); Share tab activates view; both modals open; follow-modal validation rejects malformed strings.
- [ ] `share-trust.spec.ts` — Using a raw libp2p test client (could be in Go tests instead): dial publisher, open stream on `/some/other/1.0.0`, expect reset; open correct protocol with wrong HMAC, expect reset.

For each: write the test, run it, fix any issues, commit.

**Commit per spec** — `test(share): e2e <filename>`.

---

## Phase 13 — Self-review pass

### Task 13.1: Run full baseline

- [ ] **Step 1: Go tests.**

Run: `go test ./... 2>&1 | tail -30`
Expected: all green.

- [ ] **Step 2: E2E tests.**

Run: `cd e2e && npm test 2>&1 | tail -80`
Expected: all green.

- [ ] **Step 3: Build the app end-to-end.**

Run: `make build 2>&1 | tail -10`
Expected: clean build.

- [ ] **Step 4: Install + manual smoke test.**

Run: `make install`

In the installed app:
1. Open drawer → Share tab → verify 2×2 layout.
2. Create share on any tag → copy link → verify format `mp-share:v1:<one-line>`.
3. On a second machine (or second account), follow the link → paste link → assign local tag → click Follow.
4. Upload a clip on publisher, tag it → verify it appears on follower.
5. Stop share on publisher → verify follower status flips to Offline.

- [ ] **Step 5: Commit any final tweaks.**

---

## Self-Review Checklist

Before handing off:

**Spec coverage map** (spec section → plan task):

| Spec § | Task(s) |
|---|---|
| §2 goals/non-goals | Embedded throughout; §2 non-goal on forward-only realized by §5.3 chunked protocol + §6.3 ring snapshot. |
| §3.1 Share view | Task 9.2, 10.2 |
| §3.2 Create-Share modal | Task 9.3, 10.2 |
| §3.3 Follow modal | Task 9.3, 10.2 |
| §3.4 Share-string format | Task 1.1 |
| §4.1 Components | File-structure map + tasks 5.1, 8.4 |
| §4.2 Integration | Task 8.5 |
| §5.1 Keys | Tasks 2.2 (identity), 6.1 (symkey gen) |
| §5.2 Handshake | Task 3.2, 5.2 |
| §5.3 Envelope + chunked transport | Task 3.1, 3.3, 6.2, 7.2 |
| §5.4 Trust model | Task 5.1 (single protocol handler, neutral AgentVersion) |
| §5.5 Peer discovery | Task 5.1 (DHT bootstrap), 7.1 (FindPeer dial) |
| §6.1 Protocol ID | Task 5.1 (constant), 5.2 (handler) |
| §6.2 Liveness | Task 7.1 (reconnect loop) |
| §6.3 Ring | Task 4.1, 6.2 |
| §6.4 Send scheduler | Task 6.3 |
| §7.1 Schema | Task 2.1 |
| §7.2 Design rationale | Documented in spec; enforced via FK constraints in Task 2.1 |
| §7.3 Backup/restore | Task 11.1, 11.2 |
| §8.1 Startup | Task 8.1, 8.2, 8.5 |
| §8.2 Shutdown | Task 5.1 (`Stop()`), 8.5 |
| §8.3 Clip creation | Task 6.2 |
| §8.4 Error modes | Distributed across handler logic (Task 5.2), reconnect loop (Task 7.1), assembler (Task 7.2) |
| §8.5 Rate limits | Both caps enforced in Task 5.2 (`len(pub.followers)` ≥ `MaxStreamsPerPublication` and per-peer loop ≥ `MaxStreamsPerPeer`); reconnect backoff in Task 7.1. |
| §9.1 E2E tests | Phase 12 |
| §9.2 Go unit tests | Tasks 1.1, 2.2, 3.1, 3.2, 3.3, 4.1, 5.1, 5.2, 6.2, 6.3, 7.1, 7.2, 8.1 |

**Placeholder scan:** none remaining. All earlier TODOs folded into explicit plan steps in the previous revision round.

**Type consistency:**
- `ShareInfo`, `FollowInfo`, `ShareStatus` defined in `share_types.go` (Task 0.3) and Task 8.4; used by tests and service; field names in Go (CamelCase) vs JSON (snake_case) handled via struct tags.
- `publication` and `follow` are internal Go structs — never exported to frontend.
- `envelope_bytes` consistently refers to the on-wire framed bytes (with `u32` prefix) in every task that touches it (Task 3.1, 4.1, 6.2, 8.3).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-17-encrypted-p2p-share.md`.

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Requires `superpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints.

Given the size of this plan (13 phases, ~30 tasks, many unit + e2e tests), **Subagent-Driven is strongly preferred** — each task's fresh context means cleaner commits and easier review. Which approach?
