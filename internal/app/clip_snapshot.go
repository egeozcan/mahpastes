package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// Clip snapshots back every large-clip HTTP stream: public share links
// (/s/{token}) and the authenticated GET /api/v1/clips/{id}/data. A snapshot is
// the clip's bytes copied out of the database once per revision into the
// TempClipStore directory, after which streams read the file and the database
// is out of the picture. Why not stream from SQLite is in openClipBody.
//
// A published snapshot belongs to the streams alone. It lives in the store's
// directory under a dot-prefixed, per-revision name (streamSnapshotName),
// read-only, outside the clip-ID namespace that drag-out, copy-as-file and
// media playback look files up in (parseClipIDFromTempFilename rejects it).
// Reuse checks a snapshot's identity and size, not its bytes, so a snapshot
// that doubled as the drag-out file would let an app the clip was dropped into
// — an editor saving in place at the same length — change what every link
// serves while the library still held the original. The store's hygiene still
// covers it: DeleteForClipIDs on edit and delete, the lease/orphan pruner (on a
// shorter lease, defaultStreamSnapshotTTL), DeleteAll on restore.
//
// Reuse is keyed on the stored content hash, never on the file merely
// existing. A snapshot records the SHA-256 of the bytes it was written with and
// is served only while clips.content_hash still equals it, so a writer that
// forgets to drop the temp file cannot get an old revision served. A clip whose
// content_hash is empty is never reused, only copied per request.

// clipSnapshotMaxMaterializations bounds how many clips are copied out of the
// database at once, process-wide. Each copy holds the whole blob in memory
// twice while it is read (SQLite's buffer and the driver's copy) — this, not
// the number of downloads, is what bounds peak memory, since concurrent
// requests for one clip share a single copy. Two keeps a burst of distinct
// large clips from multiplying memory while still overlapping one copy's disk
// write with the next one's read.
const clipSnapshotMaxMaterializations = 2

var clipMaterializeSem = make(chan struct{}, clipSnapshotMaxMaterializations)

// clipSnapshotBeforePublish, when set (tests only), runs between copying a
// clip out and publishing the copy — the window publishSnapshot guards.
var clipSnapshotBeforePublish func(clipID int64)

// clipSnapshotRecord is a published snapshot: the file at path, identified by
// info (file identity, so a file put at path by anything but publishSnapshot
// fails os.SameFile), holds size bytes hashing to hash.
type clipSnapshotRecord struct {
	path string
	hash string
	size int64
	info os.FileInfo
}

// clipSnapshotFlight lets concurrent requests for the same clip wait for one
// copy instead of each reading the blob.
type clipSnapshotFlight struct {
	done chan struct{}
}

// openSnapshot returns an open snapshot of the clip's current revision,
// copying it out of the database only if no valid snapshot exists. meta and
// hash are what the caller just read for the clip.
func (s *TempClipStore) openSnapshot(ctx context.Context, clipID int64, meta clipStreamMetadata, hash string) (*clipBody, error) {
	for attempt := 0; ; attempt++ {
		if body := s.reuseSnapshot(clipID, meta, hash); body != nil {
			return body, nil
		}

		s.snapMu.Lock()
		// Join a copy already in progress. Bounded, so a clip edited faster
		// than it can be copied still gets served — by a copy of its own.
		if fl := s.snapFlights[clipID]; fl != nil && attempt < 2 {
			s.snapMu.Unlock()
			select {
			case <-fl.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			// The copy just made may be of a newer revision than this request
			// first saw; match against the current one.
			var err error
			if meta, hash, err = loadClipBodyMetadata(ctx, s.db, clipID); err != nil {
				return nil, err
			}
			continue
		}
		fl := &clipSnapshotFlight{done: make(chan struct{})}
		if s.snapFlights == nil {
			s.snapFlights = make(map[int64]*clipSnapshotFlight)
		}
		s.snapFlights[clipID] = fl
		s.snapMu.Unlock()

		body, err := s.materializeSnapshot(ctx, clipID)

		s.snapMu.Lock()
		if s.snapFlights[clipID] == fl {
			delete(s.snapFlights, clipID)
		}
		s.snapMu.Unlock()
		close(fl.done)
		return body, err
	}
}

// reuseSnapshot opens the clip's published snapshot if it is still the
// revision described by meta and hash, or returns nil.
func (s *TempClipStore) reuseSnapshot(clipID int64, meta clipStreamMetadata, hash string) *clipBody {
	if hash == "" {
		return nil
	}
	s.snapMu.Lock()
	rec, ok := s.snapshots[clipID]
	s.snapMu.Unlock()
	if !ok || rec.hash != hash || rec.size != meta.size {
		return nil
	}
	f, err := os.Open(rec.path)
	if err != nil {
		return nil // pruned or dropped since; copy again
	}
	info, err := f.Stat()
	if err != nil || !os.SameFile(info, rec.info) || info.Size() != rec.size {
		f.Close()
		return nil
	}
	// A link in active use keeps its snapshot leased, as playback does.
	if now := s.now(); now.Sub(info.ModTime()) > s.streamTTL/2 {
		_ = os.Chtimes(rec.path, now, now)
	}
	return &clipBody{clipStreamMetadata: meta, data: f, closer: func() { f.Close() }, hash: hash}
}

// materializeSnapshot copies the clip out of the database and publishes the
// copy for reuse. If it cannot be published — the clip changed while it was
// being read, or its content_hash does not describe its bytes — this request
// is still served from the copy, which is then removed.
func (s *TempClipStore) materializeSnapshot(ctx context.Context, clipID int64) (*clipBody, error) {
	f, meta, sum, err := materializeClipFile(ctx, s.db, s.dir, clipID, &s.materialized)
	if err != nil {
		return nil, err
	}
	if clipSnapshotBeforePublish != nil {
		clipSnapshotBeforePublish(clipID)
	}
	if s.publishSnapshot(f, clipID, meta, sum) {
		return &clipBody{clipStreamMetadata: meta, data: f, closer: func() { f.Close() }, hash: sum}, nil
	}
	return privateClipBody(f, meta, sum), nil
}

// publishSnapshot renames a fresh copy to the clip's stream snapshot name and
// records it. It declines when clips.content_hash no longer matches the bytes
// copied.
//
// That re-check runs under s.mu, which DeleteForClipIDs also takes, and every
// writer of clips.data sets content_hash in the same statement and then drops
// the clip's temp files; deleting a clip removes the row, then drops them. So
// either the check sees the new hash (or no row) and declines, or the writer's
// drop comes after the rename and removes the copy. Reuse would never serve
// such a copy — its recorded hash no longer matches — but without the check a
// copy read just before an edit or a delete could be renamed into place just
// after the drop, leaving a plaintext copy of a revision, or of a clip, the
// library no longer holds until the pruner came across it.
func (s *TempClipStore) publishSnapshot(f *os.File, clipID int64, meta clipStreamMetadata, sum string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cheap when throttled, which is nearly always; the periodic prune
	// (StartCleanupJob) is what bounds an idle server.
	if err := s.pruneLocked(false); err != nil {
		log.Printf("clip snapshot: prune: %v", err)
	}

	var current string
	if err := s.db.QueryRow("SELECT COALESCE(content_hash, '') FROM clips WHERE id = ?", clipID).Scan(&current); err != nil || current != sum {
		return false
	}
	final := filepath.Join(s.dir, streamSnapshotName(clipID, sum))
	// Renaming, never rewriting, keeps any stream still reading an earlier
	// copy whole. Windows refuses to replace a file that is open; the copy
	// then simply stays private to this request.
	if err := os.Rename(f.Name(), final); err != nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return true // published, just not recorded: the next stream copies again
	}
	s.snapMu.Lock()
	if s.snapshots == nil {
		s.snapshots = make(map[int64]clipSnapshotRecord)
	}
	prev, hadPrev := s.snapshots[clipID]
	s.snapshots[clipID] = clipSnapshotRecord{path: final, hash: sum, size: meta.size, info: info}
	s.snapMu.Unlock()
	// A superseded revision can never be reused. Best effort: a stream still
	// reading it keeps it open on Windows, and the pruner retries.
	if hadPrev && prev.path != final {
		_ = os.Remove(prev.path)
	}
	return true
}

// isRecordedSnapshot reports whether path is the snapshot on record for
// clipID. Callers hold s.mu.
func (s *TempClipStore) isRecordedSnapshot(clipID int64, path string) bool {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	rec, ok := s.snapshots[clipID]
	return ok && rec.path == path
}

// forgetSnapshotAt drops clipID's record if it is for path. Callers hold s.mu.
func (s *TempClipStore) forgetSnapshotAt(clipID int64, path string) {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	if rec, ok := s.snapshots[clipID]; ok && rec.path == path {
		delete(s.snapshots, clipID)
	}
}

// forgetSnapshots drops the records for ids; nil drops all. Callers hold s.mu.
func (s *TempClipStore) forgetSnapshots(ids map[int64]struct{}) {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	if ids == nil {
		s.snapshots = nil
		return
	}
	for id := range ids {
		delete(s.snapshots, id)
	}
}

// openPrivateClipSnapshot serves one request from its own copy, for setups
// without a TempClipStore.
func openPrivateClipSnapshot(ctx context.Context, db *sql.DB, clipID int64) (*clipBody, error) {
	f, meta, sum, err := materializeClipFile(ctx, db, "", clipID, nil)
	if err != nil {
		return nil, err
	}
	return privateClipBody(f, meta, sum), nil
}

func privateClipBody(f *os.File, meta clipStreamMetadata, sum string) *clipBody {
	return &clipBody{clipStreamMetadata: meta, data: f, hash: sum, closer: func() {
		f.Close()
		_ = os.Remove(f.Name())
	}}
}

// materializeClipFile copies one revision of a clip — bytes and the metadata
// read with them — into a new file in dir ("" for the system temp dir) and
// returns it open, with the SHA-256 of what it wrote.
func materializeClipFile(ctx context.Context, db *sql.DB, dir string, clipID int64, counter *atomic.Int64) (*os.File, clipStreamMetadata, string, error) {
	var meta clipStreamMetadata
	select {
	case clipMaterializeSem <- struct{}{}:
	case <-ctx.Done():
		return nil, meta, "", ctx.Err()
	}
	defer func() { <-clipMaterializeSem }()

	// Deliberately not bound to the request context: the copy is shared with
	// every request waiting on it and with later ones, and it is bounded work.
	var data []byte
	err := db.QueryRow("SELECT content_type, filename, data FROM clips WHERE id = ?", clipID).
		Scan(&meta.contentType, &meta.filename, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, meta, "", ErrClipNotFound
	}
	if err != nil {
		return nil, meta, "", fmt.Errorf("read clip %d: %w", clipID, err)
	}
	if counter != nil {
		counter.Add(1)
	}
	meta.size = int64(len(data))
	digest := sha256.Sum256(data)

	// The leading dot keeps an unpublished copy out of the store's clip-ID
	// namespace (parseClipIDFromTempFilename rejects it), so drag-out and
	// playback never pick it up; the pruner still removes a leftover by age.
	// The store's dir is made once at startup; recreate it if it has been
	// removed since, or every clip too large to serve from memory fails.
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, meta, "", fmt.Errorf("create snapshot dir: %w", err)
		}
	}
	f, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return nil, meta, "", fmt.Errorf("create snapshot: %w", err)
	}
	fail := func(err error) (*os.File, clipStreamMetadata, string, error) {
		f.Close()
		_ = os.Remove(f.Name())
		return nil, meta, "", err
	}
	if _, err := f.Write(data); err != nil {
		return fail(fmt.Errorf("write snapshot: %w", err))
	}
	// Read-only: no one writes a snapshot after this, and nothing outside this
	// process is ever handed one.
	if err := f.Chmod(0444); err != nil {
		return fail(fmt.Errorf("chmod snapshot: %w", err))
	}
	return f, meta, hex.EncodeToString(digest[:]), nil
}

// streamSnapshotPrefix starts every published snapshot's name. The leading dot
// keeps it out of parseClipIDFromTempFilename's namespace; see the top of this
// file.
const streamSnapshotPrefix = ".stream-"

// streamSnapshotName is the published name of one revision of a clip:
// ".stream-<clipID>-<hash prefix>". Per revision, so publishing a new one
// never has to replace a file an older stream may still have open. The prefix
// only keeps names apart; reuse compares the whole recorded hash.
func streamSnapshotName(clipID int64, hash string) string {
	return fmt.Sprintf("%s%d-%s", streamSnapshotPrefix, clipID, hash[:16])
}

// parseStreamSnapshotName returns the clip ID in a streamSnapshotName.
func parseStreamSnapshotName(name string) (int64, bool) {
	rest, ok := strings.CutPrefix(name, streamSnapshotPrefix)
	if !ok {
		return 0, false
	}
	idPart, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
