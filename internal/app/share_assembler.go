package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"

	"go-clipboard/internal/cliptype"
)

// errClipPoisoned marks a clip whose bytes are terminally wrong: the assembled
// SHA-256 does not match the one the publisher sealed into clip_end. Replaying
// it would fail identically every time, so the caller steps its resume point
// past the clip instead of asking for it again forever.
var errClipPoisoned = errors.New("clip failed integrity check")

// followCursor names the follows row whose durable resume point must move in
// the same transaction as the clip insert. clip_end is a clip boundary, and
// committing "clip stored" together with "resume point past that clip" is what
// keeps a crash between the two from duplicating an already-delivered clip.
type followCursor struct {
	followID int64
	seq      uint64
}

// clipAssembler buffers one in-flight clip (across clip_start / clip_chunk /
// clip_end) to a staging file, then atomically inserts the finished clip
// into SQLite if the SHA-256 matches. A staging file is always removed on
// cleanup — success, SHA mismatch, or mid-stream drop — so the directory
// never leaks bytes.
type clipAssembler struct {
	stagingDir string
	followID   int64

	active    bool
	clipID    uint64
	filename  string
	cType     string
	metadata  map[string]string
	totalSize uint64
	chunks    uint32

	file         *os.File
	filePath     string
	writtenBytes uint64
	hasher       hash.Hash
}

// newClipAssembler binds an assembler to one follow. followID is part of every
// staging filename because the staging directory is shared by every follow, and
// the clip IDs written into those names come from the remote publisher — two
// follows almost certainly both see a clip 1.
func newClipAssembler(stagingDir string, followID int64) *clipAssembler {
	_ = os.MkdirAll(stagingDir, 0o755)
	return &clipAssembler{stagingDir: stagingDir, followID: followID}
}

func (a *clipAssembler) onStart(p ClipStartPayload) {
	// If a prior clip was mid-stream (missing clip_end), discard it cleanly.
	a.cleanup()
	a.active = true
	a.clipID = p.ClipID
	a.filename = p.Filename
	a.cType = p.ContentType
	a.metadata = p.Metadata
	a.totalSize = p.TotalSize
	a.chunks = p.ChunkCount

	// Staging names must be unique per in-flight clip, not per remote clip ID:
	// share-staging/ is flat and shared by all follows, so two follows staging
	// their own clip 1 under one name would truncate, interleave and unlink
	// each other's bytes — silently, since the integrity check hashes the wire
	// data rather than the file. The follow ID scopes the name and CreateTemp's
	// random suffix keeps even repeat visits to the same clip disjoint.
	f, err := os.CreateTemp(a.stagingDir, fmt.Sprintf("f%d-c%d-*.bin", a.followID, p.ClipID))
	if err != nil {
		a.active = false
		return
	}
	a.filePath = f.Name()
	a.file = f
	a.hasher = sha256.New()
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
	a.hasher.Write(p.Data)
	a.writtenBytes += uint64(len(p.Data))
}

// onEnd finalises a clip: verifies the running SHA-256 against p.SHA256,
// writes the assembled bytes to the clips table, and tags the new clip
// with localTagID. Returns the new clip's ID on success so the caller
// can fetch a preview and notify the frontend.
//
// When cur is non-nil the same transaction also advances that follow's
// durable resume point past the clip and counts the delivery, so the clip
// and the cursor that says "already received" can never disagree. A
// SHA-256 mismatch returns an error wrapping errClipPoisoned; every other
// error is a local I/O or database failure, where the clip is still worth
// retrying.
func (a *clipAssembler) onEnd(p ClipEndPayload, db *sql.DB, localTagID int64, cur *followCursor) (int64, error) {
	defer a.cleanup()
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return 0, fmt.Errorf("clip_end without active clip_start")
	}
	if err := a.file.Sync(); err != nil {
		return 0, err
	}
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	got := a.hasher.Sum(nil)
	if !bytes.Equal(got, p.SHA256) {
		return 0, fmt.Errorf("%w: sha256 got %x want %x", errClipPoisoned, got, p.SHA256)
	}
	body, err := io.ReadAll(a.file)
	if err != nil {
		return 0, err
	}
	metaJSON, _ := json.Marshal(a.metadata)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	contentType := cliptype.PromoteMarkdown(a.filename, a.cType)
	// Same value computeContentHash would produce for these bytes (lowercase
	// hex of the SHA-256), so received clips participate in dedup exactly like
	// uploaded ones. The verified running hash is already that digest — the
	// envelope check above hashes the same body bytes — so no second pass.
	contentHash := hex.EncodeToString(got)
	res, err := tx.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata, content_hash) VALUES (?, ?, ?, ?, ?)`,
		contentType, body, a.filename, string(metaJSON), contentHash,
	)
	if err != nil {
		return 0, err
	}
	newClipID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, newClipID, localTagID); err != nil {
		return 0, err
	}
	if cur != nil {
		if _, err := tx.Exec(
			`UPDATE follows SET last_seq = ?, clips_received = clips_received + 1 WHERE id = ?`,
			int64(cur.seq), cur.followID,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newClipID, nil
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
