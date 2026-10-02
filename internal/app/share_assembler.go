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
// SHA-256 does not match the one the publisher sealed into clip_end, the
// stream broke the shape its own clip_start declared (see clipShapeViolation
// and onChunk), or one of its frames decrypted but could not be decoded (see
// poison). Replaying it would fail identically every time, so the caller
// steps its resume point past the clip instead of asking for it again forever.
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
	nextChunk uint32 // the Index the next clip_chunk must carry

	// refused marks refusedClipID as a clip that broke its declared shape. The
	// staging file is already gone by then; the flag outlives it so that the
	// clip's remaining chunks are dropped without touching disk and its
	// clip_end comes back poisoned. Merely clearing active would answer that
	// clip_end with the retryable no-clip_start error, and the follower would
	// re-pull the same clip on every reconnect.
	refused       bool
	refusedClipID uint64
	refusedWhy    string
	// refusedAny widens a refusal to whichever clip the next clip_end names.
	// Set by poison, for a frame too unreadable to say which clip it belongs
	// to; cleared, like the rest of the refusal, by the next clip_start.
	refusedAny bool

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

// clipShapeViolation checks a clip_start against what an honest publisher can
// emit, and names the problem if there is one. The publisher is untrusted here
// — a share string can come from anyone — and nothing else bounds a clip: a
// frame is capped at MaxEnvelopeLen, but chunks are unlimited in number and
// onEnd reads the whole staging file into memory.
//
// The ceiling is the production MaxShareableClipBytes, never the publisher's
// test-overridable maxShareableClipBytes(): a test that lowers the publisher's
// cap must not also shrink what this side will accept.
func clipShapeViolation(p ClipStartPayload) string {
	if p.TotalSize > MaxShareableClipBytes {
		return fmt.Sprintf("declares %d bytes, over the %d-byte share ceiling", p.TotalSize, MaxShareableClipBytes)
	}
	if want := expectedChunkCount(p.TotalSize); p.ChunkCount != want {
		return fmt.Sprintf("declares %d chunks for %d bytes, want %d", p.ChunkCount, p.TotalSize, want)
	}
	return ""
}

// expectedChunkCount mirrors the publisher's chunking in OnClipCreated:
// ceil(total/ChunkSize) chunks, and one zero-length chunk for an empty clip.
// total must already be under the share ceiling, which keeps the sum below
// from overflowing.
func expectedChunkCount(total uint64) uint32 {
	n := (total + ChunkSize - 1) / ChunkSize
	if n == 0 {
		n = 1
	}
	return uint32(n)
}

// refuse drops the in-flight clip for breaking its declared shape: the staging
// file is closed and removed at once, so a stream that keeps going cannot keep
// filling the disk, and the clip is remembered so its clip_end is poisoned.
func (a *clipAssembler) refuse(clipID uint64, why string) {
	a.cleanup()
	a.refused = true
	a.refusedClipID = clipID
	a.refusedWhy = why
}

// poison drops the in-flight clip because one of its frames decrypted but
// could not be decoded. Such a frame is the publisher's own bytes at the seq
// the follower asked for, so a reconnect replays it unchanged; it is as
// terminal as a SHA mismatch. Unlike refuse it cannot name the clip — an
// undecodable frame has no trustworthy clip_id, and an undecodable clip_start
// leaves no clip in flight at all — so the next clip_end is poisoned whatever
// clip it names. Frames are strictly ordered, so that clip_end is the one
// closing the clip the bad frame sat in; and since the next clip_start clears
// the refusal, a bad frame between clips costs the clip after it nothing.
func (a *clipAssembler) poison(why string) {
	a.cleanup()
	a.refused = true
	a.refusedAny = true
	a.refusedWhy = why
}

func (a *clipAssembler) onStart(p ClipStartPayload) {
	// If a prior clip was mid-stream (missing clip_end), discard it cleanly.
	a.cleanup()
	if why := clipShapeViolation(p); why != "" {
		a.refuse(p.ClipID, why)
		return
	}
	a.active = true
	a.clipID = p.ClipID
	a.filename = p.Filename
	a.cType = p.ContentType
	a.metadata = p.Metadata
	a.totalSize = p.TotalSize
	a.chunks = p.ChunkCount
	a.nextChunk = 0

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

// onChunk appends one chunk, holding it to the clip_start's declaration first:
// chunks in Index order, no more than ChunkCount of them, and never more bytes
// than TotalSize. Frames within a session are strictly ordered (seq is in the
// AAD), so a stream that breaks any of these was sent that way and replays
// identically — it is refused as poison, not treated as a transport fault.
func (a *clipAssembler) onChunk(p ClipChunkPayload) {
	// A refused clip is inactive too, so its chunks stop here.
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return
	}
	switch {
	case p.Index != a.nextChunk:
		a.refuse(p.ClipID, fmt.Sprintf("chunk %d arrived where chunk %d was due", p.Index, a.nextChunk))
		return
	case p.Index >= a.chunks:
		a.refuse(p.ClipID, fmt.Sprintf("chunk %d is past the %d declared", p.Index, a.chunks))
		return
	case a.writtenBytes+uint64(len(p.Data)) > a.totalSize:
		a.refuse(p.ClipID, fmt.Sprintf("chunk %d overruns the declared %d bytes", p.Index, a.totalSize))
		return
	}
	if _, err := a.file.Write(p.Data); err != nil {
		// A local write failure (disk full) is not the clip's fault: leave it
		// unrefused so clip_end stays retryable.
		a.cleanup()
		return
	}
	a.hasher.Write(p.Data)
	a.writtenBytes += uint64(len(p.Data))
	a.nextChunk++
}

// onEnd finalises a clip: verifies the running SHA-256 against p.SHA256,
// writes the assembled bytes to the clips table, and tags the new clip
// with localTagID. Returns the new clip's ID on success so the caller
// can fetch a preview and notify the frontend.
//
// When cur is non-nil the same transaction also advances that follow's
// durable resume point past the clip and counts the delivery, so the clip
// and the cursor that says "already received" can never disagree. A
// SHA-256 mismatch, a clip refused for its shape or poisoned by an
// undecodable frame, or a clip that ends short of its declared chunks or
// bytes returns an error wrapping errClipPoisoned; every other error is a
// local I/O or database failure, where the clip is still worth retrying.
func (a *clipAssembler) onEnd(p ClipEndPayload, db *sql.DB, localTagID int64, cur *followCursor) (int64, error) {
	defer a.cleanup()
	if a.refused && (a.refusedAny || p.ClipID == a.refusedClipID) {
		return 0, fmt.Errorf("%w: %s", errClipPoisoned, a.refusedWhy)
	}
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return 0, fmt.Errorf("clip_end without active clip_start")
	}
	if a.nextChunk != a.chunks || a.writtenBytes != a.totalSize {
		return 0, fmt.Errorf("%w: clip_end after %d of %d chunks and %d of %d bytes",
			errClipPoisoned, a.nextChunk, a.chunks, a.writtenBytes, a.totalSize)
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
	a.refused = false
	a.refusedClipID = 0
	a.refusedWhy = ""
	a.refusedAny = false
}
