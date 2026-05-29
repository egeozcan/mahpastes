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
	"time"
)

const (
	defaultTempLeaseTTL      = 60 * time.Minute
	defaultTempPruneInterval = 10 * time.Minute
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
	pruneInterval time.Duration
	lastPrune     time.Time
	now           func() time.Time
	mu            sync.Mutex
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

	if err := os.WriteFile(tempPath, data, 0644); err != nil {
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

// DeleteForClipIDs removes temp files for the provided clip IDs.
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
		clipID, ok := parseClipIDFromTempFilename(entry.Name())
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

		stale := now.Sub(info.ModTime()) > s.leaseTTL
		orphan := false
		if clipID, ok := parseClipIDFromTempFilename(entry.Name()); ok {
			exists, err := s.clipExists(clipID)
			if err != nil {
				return err
			}
			orphan = !exists
		}

		if stale || orphan {
			if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to remove temp file %q: %w", fullPath, err)
			}
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
