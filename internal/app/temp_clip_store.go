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
	mu            sync.Mutex

	// Streaming snapshots (clip_snapshot.go). snapMu guards only these two
	// maps, so checking for a reusable snapshot never waits behind s.mu, which
	// PrepareClipFile holds across a whole clip read. Lock order: mu, then
	// snapMu.
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

// PrepareClipFile creates or refreshes the leased temp file for a clip.
func (s *TempClipStore) PrepareClipFile(clipID int64) (*tempPreparedFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if clipID <= 0 {
		return nil, fmt.Errorf("invalid clip ID: %d", clipID)
	}
	if s.db == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	if s.dir == "" {
		return nil, fmt.Errorf("temp clip store directory is not configured")
	}

	if err := s.pruneLocked(false); err != nil {
		return nil, err
	}

	var data []byte
	var filename sql.NullString
	var contentType string
	row := s.db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", clipID)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClipNotFound
		}
		return nil, fmt.Errorf("failed to load clip %d: %w", clipID, err)
	}

	safeName := tempFilenameForClip(clipID, filename, contentType)
	tempPath := filepath.Join(s.dir, safeName)

	if err := replaceTempFile(tempPath, data); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	now := s.now()
	if err := os.Chtimes(tempPath, now, now); err != nil {
		return nil, fmt.Errorf("failed to refresh temp file lease: %w", err)
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()

	if clipID <= 0 {
		return nil, fmt.Errorf("invalid clip ID: %d", clipID)
	}
	if s.db == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	if s.dir == "" {
		return nil, fmt.Errorf("temp clip store directory is not configured")
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read temp directory: %w", err)
	}

	exists, err := s.clipExists(clipID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrClipNotFound
	}

	now := s.now()
	var candidatePath string
	var candidateMod time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		id, ok := parseClipIDFromTempFilename(entry.Name())
		if !ok || id != clipID {
			continue
		}

		fullPath := filepath.Join(s.dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > s.leaseTTL {
			_ = os.Remove(fullPath)
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

	var filename sql.NullString
	var contentType string
	row := s.db.QueryRow("SELECT filename, content_type FROM clips WHERE id = ?", clipID)
	if err := row.Scan(&filename, &contentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClipNotFound
		}
		return nil, fmt.Errorf("failed to load clip metadata %d: %w", clipID, err)
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
			s.lastPrune = now
			return nil
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

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
			return fmt.Errorf("failed to remove temp file %q: %w", fullPath, err)
		}
	}

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

// replaceTempFile writes data to path by renaming a fresh file over it rather
// than truncating it in place. A leased file can be read by a long-lived
// stream — a video playing from it over /media/ — and an in-place rewrite
// would truncate the file under it mid-read. Windows cannot replace a file
// that is open, so there the in-place write is the fallback.
func replaceTempFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".prepare-*")
	if err == nil {
		_, err = tmp.Write(data)
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Chmod(tmp.Name(), 0644)
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		}
		if err == nil {
			return nil
		}
		_ = os.Remove(tmp.Name())
	}
	return os.WriteFile(path, data, 0644)
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
