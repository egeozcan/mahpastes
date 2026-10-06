package app

import (
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultTempLeaseTTL      = 60 * time.Minute
	defaultTempPruneInterval = 10 * time.Minute

	// defaultStreamSnapshotTTL is the lease on a link snapshot
	// (clip_snapshot.go), shorter than a drag-out or playback file's. A
	// snapshot only ever spares a later request one copy out of the database,
	// and the headless server makes one for every large clip its web UI shows,
	// so on the hour lease browsing a big library would keep a full copy of
	// everything viewed on disk for an hour after the last look.
	defaultStreamSnapshotTTL = 15 * time.Minute
)

// ErrClipNotFound indicates a clip ID did not exist in the database.
var ErrClipNotFound = errors.New("clip not found")

// tempPreparedFile is the store-level result for a prepared transfer file.
type tempPreparedFile struct {
	ClipID         int64
	AbsPath        string
	Filename       string
	ContentType    string
	LeaseExpiresAt time.Time
}

// TempClipStore manages leased clip temp files shared by transfer channels.
type TempClipStore struct {
	db            *sql.DB
	dir           string
	leaseTTL      time.Duration
	streamTTL     time.Duration // lease on link snapshots; see defaultStreamSnapshotTTL
	pruneInterval time.Duration
	lastPrune     time.Time
	now           func() time.Time

	// mu guards the directory's bookkeeping — leased, prepFlights, lastPrune
	// — and orders publishing a copy against DeleteForClipIDs. It is held
	// for map updates, renames and the throttled prune, never across reading
	// a clip out of the database or writing it to disk.
	mu sync.Mutex
	// leased indexes the leased transfer files on disk by clip (base names),
	// so FindExistingClipFile stats a clip's own files instead of listing the
	// directory. Built on first use and rebuilt by every prune; a file
	// removed behind the store's back drops out when its stat fails.
	leased      map[int64]map[string]struct{}
	leasedBuilt bool
	// prepFlights lets concurrent PrepareClipFile calls for one clip share
	// one copy, while different clips copy concurrently.
	prepFlights map[int64]*tempPrepareFlight

	// Streaming snapshots (clip_snapshot.go). snapMu guards only these two
	// maps, so checking for a reusable snapshot never waits behind s.mu.
	// Lock order: mu, then snapMu.
	snapMu      sync.Mutex
	snapshots   map[int64]clipSnapshotRecord
	snapFlights map[int64]*clipSnapshotFlight
	// materialized counts snapshot copies out of the database.
	materialized atomic.Int64
}

// NewTempClipStore creates a new temp clip store.
func NewTempClipStore(db *sql.DB, dir string, leaseTTL, pruneInterval time.Duration) *TempClipStore {
	if leaseTTL <= 0 {
		leaseTTL = defaultTempLeaseTTL
	}
	if pruneInterval <= 0 {
		pruneInterval = defaultTempPruneInterval
	}
	return &TempClipStore{
		db:            db,
		dir:           dir,
		leaseTTL:      leaseTTL,
		streamTTL:     min(leaseTTL, defaultStreamSnapshotTTL),
		pruneInterval: pruneInterval,
		now:           time.Now,
	}
}

// tempPrepareMaxConcurrent bounds how many clips PrepareClipFile copies out
// of the database at once. Each copy holds the whole blob in memory while it
// is read and written, so a gallery page of video cards must not multiply
// that by the page size.
const tempPrepareMaxConcurrent = 3

var tempPrepareSem = make(chan struct{}, tempPrepareMaxConcurrent)

// tempPrepareMaxAttempts bounds re-reads of a clip whose files were dropped
// (an edit or a delete) while a copy of it was being made.
const tempPrepareMaxAttempts = 3

// tempPrepareAfterLoad, when set (tests only), runs after PrepareClipFile has
// read a clip out of the database and before it publishes the copy — the
// window the dropped flag guards.
var tempPrepareAfterLoad func(clipID int64)

// tempPrepareFlight is one in-progress PrepareClipFile copy, shared by every
// concurrent request for that clip.
type tempPrepareFlight struct {
	done chan struct{}
	// dropped is set, under s.mu, by DeleteForClipIDs and DeleteAll: the clip
	// changed or went away after the copy started reading it, so the copy may
	// hold bytes the library no longer has and must not be published.
	dropped bool
	result  *tempPreparedFile
	err     error
}

// PrepareClipFile creates or refreshes the leased temp file for a clip.
//
// The copy is made outside s.mu: a request joins a copy of the same clip
// already in progress, and copies of different clips run concurrently (up to
// tempPrepareMaxConcurrent). The fresh file is renamed into place under s.mu
// only if no drop for the clip ran since the copy began reading — every writer
// of clips.data and every deleter drops the clip's temp files after its
// statement, so either the drop comes later and removes the new file, or the
// copy is discarded and the clip read again.
func (s *TempClipStore) PrepareClipFile(clipID int64) (*tempPreparedFile, error) {
	if clipID <= 0 {
		return nil, fmt.Errorf("invalid clip ID: %d", clipID)
	}
	if s.db == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	if s.dir == "" {
		return nil, fmt.Errorf("temp clip store directory is not configured")
	}

	s.mu.Lock()
	if err := s.pruneLocked(false); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if fl := s.prepFlights[clipID]; fl != nil {
		s.mu.Unlock()
		<-fl.done
		if fl.err != nil {
			return nil, fl.err
		}
		res := *fl.result
		return &res, nil
	}
	fl := &tempPrepareFlight{done: make(chan struct{})}
	if s.prepFlights == nil {
		s.prepFlights = make(map[int64]*tempPrepareFlight)
	}
	s.prepFlights[clipID] = fl
	s.mu.Unlock()

	fl.result, fl.err = s.prepareClipFile(clipID, fl)

	s.mu.Lock()
	if s.prepFlights[clipID] == fl {
		delete(s.prepFlights, clipID)
	}
	s.mu.Unlock()
	close(fl.done)
	if fl.err != nil {
		return nil, fl.err
	}
	res := *fl.result
	return &res, nil
}

// prepareClipFile does one flight's copy. fl is registered in prepFlights.
func (s *TempClipStore) prepareClipFile(clipID int64, fl *tempPrepareFlight) (*tempPreparedFile, error) {
	tempPrepareSem <- struct{}{}
	defer func() { <-tempPrepareSem }()

	for attempt := 1; ; attempt++ {
		data, filename, contentType, err := s.loadClipForPrepare(clipID)
		if err != nil {
			return nil, err
		}
		if tempPrepareAfterLoad != nil {
			tempPrepareAfterLoad(clipID)
		}
		safeName := tempFilenameForClip(clipID, filename, contentType)
		tempPath := filepath.Join(s.dir, safeName)
		tmp, err := writePrepareTemp(s.dir, data)
		if err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}

		s.mu.Lock()
		if fl.dropped {
			fl.dropped = false
			s.mu.Unlock()
			_ = os.Remove(tmp)
			if attempt >= tempPrepareMaxAttempts {
				return nil, fmt.Errorf("clip %d kept changing while it was being prepared", clipID)
			}
			continue
		}
		prepared, err := s.publishPreparedLocked(clipID, tmp, tempPath, data, filename, contentType)
		s.mu.Unlock()
		return prepared, err
	}
}

// loadClipForPrepare reads a clip's bytes and naming metadata.
func (s *TempClipStore) loadClipForPrepare(clipID int64) ([]byte, sql.NullString, string, error) {
	var data []byte
	var filename sql.NullString
	var contentType string
	row := s.db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", clipID)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, filename, "", ErrClipNotFound
		}
		return nil, filename, "", fmt.Errorf("failed to load clip %d: %w", clipID, err)
	}
	return data, filename, contentType, nil
}

// writePrepareTemp writes data to a fresh dot-prefixed file in dir (outside
// the clip-ID namespace, so nothing looks it up before it is renamed) and
// returns its path.
func writePrepareTemp(dir string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".prepare-*")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0644)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// publishPreparedLocked moves a finished copy into place, refreshes its lease
// and indexes it. Callers hold s.mu.
func (s *TempClipStore) publishPreparedLocked(clipID int64, tmp, tempPath string, data []byte, filename sql.NullString, contentType string) (*tempPreparedFile, error) {
	// Renaming a fresh file over the old one, never truncating it in place:
	// a leased file can be read by a long-lived stream — a video playing from
	// it over /media/ — and an in-place rewrite would truncate the file under
	// it mid-read. Windows cannot replace an open file; there the in-place
	// write is the fallback.
	if err := os.Rename(tmp, tempPath); err != nil {
		_ = os.Remove(tmp)
		if err := os.WriteFile(tempPath, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
	}

	now := s.now()
	if err := os.Chtimes(tempPath, now, now); err != nil {
		return nil, fmt.Errorf("failed to refresh temp file lease: %w", err)
	}
	s.indexLeasedLocked(clipID, filepath.Base(tempPath))

	absPath, err := filepath.Abs(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve temp file path: %w", err)
	}

	resolvedName := filename.String
	if resolvedName == "" {
		resolvedName = filepath.Base(absPath)
	}

	return &tempPreparedFile{
		ClipID:         clipID,
		AbsPath:        absPath,
		Filename:       resolvedName,
		ContentType:    contentType,
		LeaseExpiresAt: now.Add(s.leaseTTL),
	}, nil
}

// FindExistingClipFile returns a leased descriptor if a non-stale temp file already exists.
// It does not create new files from clip bytes.
func (s *TempClipStore) FindExistingClipFile(clipID int64) (*tempPreparedFile, error) {
	if clipID <= 0 {
		return nil, fmt.Errorf("invalid clip ID: %d", clipID)
	}
	if s.db == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	if s.dir == "" {
		return nil, fmt.Errorf("temp clip store directory is not configured")
	}

	var filename sql.NullString
	var contentType string
	row := s.db.QueryRow("SELECT filename, content_type FROM clips WHERE id = ?", clipID)
	if err := row.Scan(&filename, &contentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClipNotFound
		}
		return nil, fmt.Errorf("failed to load clip metadata %d: %w", clipID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLeasedIndexLocked(); err != nil {
		return nil, err
	}

	now := s.now()
	var candidatePath string
	var candidateMod time.Time
	for name := range s.leased[clipID] {
		fullPath := filepath.Join(s.dir, name)
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			// Removed behind the store's back (maintenance, a user).
			s.unindexLeasedLocked(clipID, name)
			continue
		}
		if now.Sub(info.ModTime()) > s.leaseTTL {
			_ = os.Remove(fullPath)
			s.unindexLeasedLocked(clipID, name)
			continue
		}
		if candidatePath == "" || info.ModTime().After(candidateMod) {
			candidatePath = fullPath
			candidateMod = info.ModTime()
		}
	}

	if candidatePath == "" {
		return nil, nil
	}

	if err := os.Chtimes(candidatePath, now, now); err != nil {
		return nil, fmt.Errorf("failed to refresh temp file lease: %w", err)
	}

	absPath, err := filepath.Abs(candidatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve temp file path: %w", err)
	}

	resolvedName := filename.String
	if strings.TrimSpace(resolvedName) == "" {
		resolvedName = filepath.Base(absPath)
	}

	return &tempPreparedFile{
		ClipID:         clipID,
		AbsPath:        absPath,
		Filename:       resolvedName,
		ContentType:    contentType,
		LeaseExpiresAt: now.Add(s.leaseTTL),
	}, nil
}

// ensureLeasedIndexLocked builds the leased-file index from the directory
// the first time it is needed. Callers hold s.mu.
func (s *TempClipStore) ensureLeasedIndexLocked() error {
	if s.leasedBuilt {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read temp directory: %w", err)
	}
	s.rebuildLeasedIndexLocked(entries)
	return nil
}

// rebuildLeasedIndexLocked replaces the index with the leased transfer files
// among entries. Callers hold s.mu.
func (s *TempClipStore) rebuildLeasedIndexLocked(entries []os.DirEntry) {
	s.leased = make(map[int64]map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if id, ok := parseClipIDFromTempFilename(entry.Name()); ok {
			s.indexLeasedLocked(id, entry.Name())
		}
	}
	s.leasedBuilt = true
}

func (s *TempClipStore) indexLeasedLocked(clipID int64, name string) {
	if s.leased == nil {
		s.leased = make(map[int64]map[string]struct{})
	}
	names := s.leased[clipID]
	if names == nil {
		names = make(map[string]struct{})
		s.leased[clipID] = names
	}
	names[name] = struct{}{}
}

func (s *TempClipStore) unindexLeasedLocked(clipID int64, name string) {
	if names := s.leased[clipID]; names != nil {
		delete(names, name)
		if len(names) == 0 {
			delete(s.leased, clipID)
		}
	}
}

// markPrepareDroppedLocked tells in-progress copies of ids (nil: all) that
// the clip's files were dropped. Callers hold s.mu.
func (s *TempClipStore) markPrepareDroppedLocked(ids map[int64]struct{}) {
	for id, fl := range s.prepFlights {
		if ids == nil {
			fl.dropped = true
			continue
		}
		if _, ok := ids[id]; ok {
			fl.dropped = true
		}
	}
}

// DeleteForClipIDs removes temp files for the provided clip IDs, link
// snapshots included.
func (s *TempClipStore) DeleteForClipIDs(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idSet := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			idSet[id] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return nil
	}
	s.forgetSnapshots(idSet)
	s.markPrepareDroppedLocked(idSet)
	for id := range idSet {
		delete(s.leased, id)
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		clipID, _, ok := clipIDForTempFile(entry.Name())
		if !ok {
			continue
		}
		if _, shouldDelete := idSet[clipID]; !shouldDelete {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return fmt.Errorf("failed to remove one or more temp files: %w", firstErr)
	}
	return nil
}

// DeleteAll removes and recreates the managed temp directory.
func (s *TempClipStore) DeleteAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return nil
	}
	s.forgetSnapshots(nil)
	s.markPrepareDroppedLocked(nil)
	s.leased = nil
	s.leasedBuilt = true // the directory is empty from here on

	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("failed to remove temp dir: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to recreate temp dir: %w", err)
	}
	return nil
}

// Prune removes stale and orphaned files according to the configured policy.
func (s *TempClipStore) Prune(force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneLocked(force)
}

func (s *TempClipStore) pruneLocked(force bool) error {
	now := s.now()
	if !force && !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < s.pruneInterval {
		return nil
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.rebuildLeasedIndexLocked(nil)
			s.lastPrune = now
			return nil
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	// The leased index is rebuilt from what this pass leaves on disk, so a
	// file created or removed behind the store's back is picked up here.
	kept := entries[:0:0]
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(s.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		clipID, snapshot, ok := clipIDForTempFile(entry.Name())
		ttl := s.leaseTTL
		if snapshot {
			ttl = s.streamTTL
		}
		remove := now.Sub(info.ModTime()) > ttl
		if !remove && ok {
			exists, err := s.clipExists(clipID)
			if err != nil {
				return err
			}
			remove = !exists // orphan
		}
		// A link snapshot is reachable only through its in-memory record, so
		// one without it — left by an earlier run, or superseded — is dead.
		if !remove && snapshot {
			remove = !s.isRecordedSnapshot(clipID, fullPath)
		}
		if !remove {
			kept = append(kept, entry)
			continue
		}

		err = os.Remove(fullPath)
		if snapshot {
			// Best effort: on Windows a snapshot a stream still has open
			// cannot be removed yet, and one undeletable file must not fail
			// every drag-out that prunes first. The next prune retries.
			if err == nil || errors.Is(err, os.ErrNotExist) {
				s.forgetSnapshotAt(clipID, fullPath)
			}
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			s.rebuildLeasedIndexLocked(append(kept, entry))
			return fmt.Errorf("failed to remove temp file %q: %w", fullPath, err)
		}
	}

	s.rebuildLeasedIndexLocked(kept)
	s.lastPrune = now
	return nil
}

func (s *TempClipStore) clipExists(clipID int64) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("database is not initialized")
	}
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM clips WHERE id = ?", clipID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check clip %d existence: %w", clipID, err)
}

func tempFilenameForClip(clipID int64, filename sql.NullString, contentType string) string {
	safeName := fmt.Sprintf("%d", clipID)
	if filename.Valid && strings.TrimSpace(filename.String) != "" {
		return fmt.Sprintf("%d_%s", clipID, filepath.Base(filename.String))
	}
	exts, _ := mime.ExtensionsByType(contentType)
	if len(exts) > 0 {
		safeName += exts[0]
	}
	return safeName
}

// clipIDForTempFile returns the clip a file in the store's directory belongs
// to, and whether it is a link snapshot rather than a leased transfer file.
func clipIDForTempFile(name string) (clipID int64, snapshot, ok bool) {
	if clipID, ok = parseClipIDFromTempFilename(name); ok {
		return clipID, false, true
	}
	if clipID, ok = parseStreamSnapshotName(name); ok {
		return clipID, true, true
	}
	return 0, false, false
}

// parseClipIDFromTempFilename returns the clip ID of a leased transfer file —
// the names drag-out, copy-as-file and playback hand out. It rejects every
// dot-prefixed name: in-flight copies and link snapshots.
func parseClipIDFromTempFilename(name string) (int64, bool) {
	if name == "" {
		return 0, false
	}
	i := 0
	for i < len(name) {
		c := name[i]
		if c < '0' || c > '9' {
			break
		}
		i++
	}
	if i == 0 {
		return 0, false
	}
	if i < len(name) {
		switch name[i] {
		case '_', '.':
		default:
			return 0, false
		}
	}
	id, err := strconv.ParseInt(name[:i], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
