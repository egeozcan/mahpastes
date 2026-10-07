package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	markdownImageCacheTTL      = time.Hour
	markdownImageCacheMaxBytes = 250 * 1024 * 1024
)

type MarkdownImageCacheStats struct {
	Entries int   `json:"entries"`
	Bytes   int64 `json:"bytes"`
}

type markdownImageCacheEntry struct {
	Data        []byte
	ContentType string
}

type markdownImageCacheMetadata struct {
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ExpiresAt   time.Time `json:"expires_at"`
	LastAccess  time.Time `json:"last_access"`
}

// markdownImageCache is a disk cache of remote Markdown images with an
// in-memory index of every entry. The index is built from the directory once,
// at construction; after that Put, Get, Stats and pruning consult only the
// index, so a Put no longer rescans the directory and a hit touches no
// metadata file. A hit records its access time in the index and, best-effort,
// as the data file's mtime (no rewrite, no fsync), which the next startup's
// scan folds back into the LRU order. A hit reads the data file outside the
// mutex: files are only ever replaced by rename while Put holds the mutex and
// has already taken the old entry out of the index, so a read is trusted only
// if the index still holds the very entry it started from afterwards (else
// it retries); a size mismatch is a miss.
//
// Evicting an entry can fail to delete its files — Windows refuses to delete a
// file another handle (an unlocked Get's read) has open. Such keys stay in
// `stale` with their bytes still counted against maxBytes, and every prune
// retries them, so a failed delete is never forgotten until the next restart.
type markdownImageCache struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	now      func() time.Time
	index    map[string]*markdownImageIndexEntry
	total    int64 // bytes of live index entries

	stale      map[string]int64 // key -> bytes of files whose delete failed
	staleBytes int64

	// Test seams: remove deletes a cache file (os.Remove); beforeRead runs
	// between Get's index snapshot and its unlocked read.
	remove     func(string) error
	beforeRead func()
}

type markdownImageIndexEntry struct {
	key         string
	contentType string
	size        int64
	expiresAt   time.Time
	lastAccess  time.Time
}

func newMarkdownImageCache(dir string, maxBytes int64, now func() time.Time) (*markdownImageCache, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Markdown image cache: %w", err)
	}
	cache := &markdownImageCache{
		dir:      dir,
		maxBytes: maxBytes,
		now:      now,
		stale:    map[string]int64{},
		remove:   os.Remove,
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := cache.rebuildIndexLocked(); err != nil {
		return nil, err
	}
	cache.pruneLocked()
	return cache, nil
}

func markdownImageCacheKey(rawURL string) string {
	hash := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(hash[:])
}

func (c *markdownImageCache) keyPaths(key string) (string, string) {
	return filepath.Join(c.dir, key+".bin"), filepath.Join(c.dir, key+".json")
}

func (c *markdownImageCache) paths(rawURL string) (string, string) {
	return c.keyPaths(markdownImageCacheKey(rawURL))
}

func (c *markdownImageCache) Put(rawURL string, data []byte, contentType string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	if ttl > markdownImageCacheTTL {
		ttl = markdownImageCacheTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	key := markdownImageCacheKey(rawURL)
	dataPath, metadataPath := c.keyPaths(key)
	now := c.now().UTC()
	metadata := markdownImageCacheMetadata{
		ContentType: contentType,
		Size:        int64(len(data)),
		ExpiresAt:   now.Add(ttl),
		LastAccess:  now,
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode cache metadata: %w", err)
	}
	// The old entry is gone from the index before its files are replaced, so
	// a failed write never leaves the index describing bytes that are not there.
	// A pending stale delete of this key is superseded by the replacement; if
	// the write fails, whatever is left on disk is discarded (and retried) again.
	oldSize := c.staleSizeLocked(key)
	if entry := c.index[key]; entry != nil && entry.size > oldSize {
		oldSize = entry.size
	}
	c.dropLocked(key)
	c.clearStaleLocked(key)
	if err := writeAtomicFile(dataPath, data, 0o600); err != nil {
		c.discardKeyLocked(key, oldSize)
		return err
	}
	if err := writeAtomicFile(metadataPath, encodedMetadata, 0o600); err != nil {
		c.discardKeyLocked(key, metadata.Size)
		return err
	}
	c.index[key] = &markdownImageIndexEntry{
		key:         key,
		contentType: contentType,
		size:        metadata.Size,
		expiresAt:   metadata.ExpiresAt,
		lastAccess:  now,
	}
	c.total += metadata.Size
	c.pruneLocked()
	return nil
}

// markdownImageCacheGetAttempts bounds Get's retries when a concurrent Put
// replaces the entry mid-read; past it the read is reported as a miss.
const markdownImageCacheGetAttempts = 3

func (c *markdownImageCache) Get(rawURL string) (markdownImageCacheEntry, bool, error) {
	key := markdownImageCacheKey(rawURL)
	for attempt := 0; attempt < markdownImageCacheGetAttempts; attempt++ {
		result, hit, replaced, err := c.getOnce(key)
		if !replaced {
			return result, hit, err
		}
	}
	return markdownImageCacheEntry{}, false, nil
}

// getOnce reports replaced when the index entry the read started from was no
// longer the indexed one once the read finished: the bytes may belong to a
// different revision than the content type snapshotted with the entry (a Put
// of an equally sized JPEG over a PNG), so they are not returned.
func (c *markdownImageCache) getOnce(key string) (result markdownImageCacheEntry, hit, replaced bool, err error) {
	dataPath, _ := c.keyPaths(key)

	c.mu.Lock()
	entry := c.index[key]
	if entry == nil {
		c.mu.Unlock()
		return markdownImageCacheEntry{}, false, false, nil
	}
	if !c.now().UTC().Before(entry.expiresAt) {
		c.evictLocked(key)
		c.mu.Unlock()
		return markdownImageCacheEntry{}, false, false, nil
	}
	want := *entry
	c.mu.Unlock()

	if c.beforeRead != nil {
		c.beforeRead()
	}
	data, readErr := os.ReadFile(dataPath)

	now := c.now().UTC()
	c.mu.Lock()
	if c.index[key] != entry {
		// Replaced, evicted or cleared during the read: whatever was read
		// cannot be paired with want's content type.
		c.mu.Unlock()
		return markdownImageCacheEntry{}, false, true, nil
	}
	if readErr != nil || int64(len(data)) != want.size {
		c.evictLocked(key)
		c.mu.Unlock()
		if readErr != nil && !os.IsNotExist(readErr) {
			return markdownImageCacheEntry{}, false, false, fmt.Errorf("read cached image: %w", readErr)
		}
		return markdownImageCacheEntry{}, false, false, nil
	}
	entry.lastAccess = now
	c.mu.Unlock()
	// Persists the access for the next startup's LRU order; best-effort.
	_ = os.Chtimes(dataPath, now, now)
	return markdownImageCacheEntry{Data: data, ContentType: want.contentType}, true, false, nil
}

func (c *markdownImageCache) Stats() (MarkdownImageCacheStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	return MarkdownImageCacheStats{Entries: len(c.index), Bytes: c.total + c.staleBytes}, nil
}

func (c *markdownImageCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.index = map[string]*markdownImageIndexEntry{}
	c.total = 0
	c.stale = map[string]int64{}
	c.staleBytes = 0
	if err := os.RemoveAll(c.dir); err != nil {
		return fmt.Errorf("clear Markdown image cache: %w", err)
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("recreate Markdown image cache: %w", err)
	}
	return nil
}

type cacheMetadataEntry struct {
	dataPath     string
	metadataPath string
	metadata     markdownImageCacheMetadata
}

func (c *markdownImageCache) dropLocked(key string) {
	if entry := c.index[key]; entry != nil {
		c.total -= entry.size
		delete(c.index, key)
	}
}

// evictLocked drops a live entry from the index and deletes its files,
// keeping its bytes accounted as stale if the delete fails.
func (c *markdownImageCache) evictLocked(key string) {
	var size int64
	if entry := c.index[key]; entry != nil {
		size = entry.size
	}
	c.dropLocked(key)
	c.discardKeyLocked(key, size)
}

// discardKeyLocked deletes key's files. If either delete fails for any reason
// other than the file being absent, the key is recorded as stale with size
// bytes, so the budget still counts them and pruning retries the delete.
func (c *markdownImageCache) discardKeyLocked(key string, size int64) {
	if c.removeKeyFilesLocked(key) {
		c.clearStaleLocked(key)
		return
	}
	if prev, ok := c.stale[key]; ok {
		if size < prev {
			size = prev
		}
		c.staleBytes -= prev
	}
	c.stale[key] = size
	c.staleBytes += size
}

func (c *markdownImageCache) staleSizeLocked(key string) int64 {
	return c.stale[key]
}

func (c *markdownImageCache) clearStaleLocked(key string) {
	if size, ok := c.stale[key]; ok {
		c.staleBytes -= size
		delete(c.stale, key)
	}
}

// retryStaleLocked re-attempts every delete that failed earlier.
func (c *markdownImageCache) retryStaleLocked() {
	for key := range c.stale {
		if c.removeKeyFilesLocked(key) {
			c.clearStaleLocked(key)
		}
	}
}

// removeKeyFilesLocked deletes key's data and metadata files and reports
// whether neither is left behind.
func (c *markdownImageCache) removeKeyFilesLocked(key string) bool {
	dataPath, metadataPath := c.keyPaths(key)
	return c.removeEntryLocked(dataPath, metadataPath)
}

// rebuildIndexLocked scans the directory: the only full scan, done at
// construction. Entries whose data file is missing or the wrong size are
// removed. A data file's mtime newer than the recorded last access is a hit
// recorded by Get since the metadata was written.
func (c *markdownImageCache) rebuildIndexLocked() error {
	c.index = map[string]*markdownImageIndexEntry{}
	c.total = 0
	entries, err := c.metadataEntriesLocked()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := os.Stat(entry.dataPath)
		if err != nil || info.Size() != entry.metadata.Size {
			c.removeEntryLocked(entry.dataPath, entry.metadataPath)
			continue
		}
		lastAccess := entry.metadata.LastAccess
		if mtime := info.ModTime().UTC(); mtime.After(lastAccess) {
			lastAccess = mtime
		}
		key := strings.TrimSuffix(filepath.Base(entry.metadataPath), ".json")
		c.index[key] = &markdownImageIndexEntry{
			key:         key,
			contentType: entry.metadata.ContentType,
			size:        entry.metadata.Size,
			expiresAt:   entry.metadata.ExpiresAt,
			lastAccess:  lastAccess,
		}
		c.total += entry.metadata.Size
	}
	return nil
}

// pruneLocked drops expired entries, then the least recently used until the
// cache fits maxBytes — from the index alone.
func (c *markdownImageCache) pruneLocked() {
	c.retryStaleLocked()
	now := c.now().UTC()
	for key, entry := range c.index {
		if !now.Before(entry.expiresAt) {
			c.evictLocked(key)
		}
	}
	if c.maxBytes <= 0 || c.total+c.staleBytes <= c.maxBytes {
		return
	}
	retained := make([]*markdownImageIndexEntry, 0, len(c.index))
	for _, entry := range c.index {
		retained = append(retained, entry)
	}
	sort.Slice(retained, func(i, j int) bool {
		return retained[i].lastAccess.Before(retained[j].lastAccess)
	})
	for _, entry := range retained {
		if c.total+c.staleBytes <= c.maxBytes {
			break
		}
		c.evictLocked(entry.key)
	}
}

func (c *markdownImageCache) metadataEntriesLocked() ([]cacheMetadataEntry, error) {
	files, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Markdown image cache: %w", err)
	}
	var entries []cacheMetadataEntry
	metadataKeys := map[string]bool{}
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			metadataKeys[file.Name()[:len(file.Name())-len(".json")]] = true
		}
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if filepath.Ext(file.Name()) == ".bin" {
			base := file.Name()[:len(file.Name())-len(".bin")]
			if !metadataKeys[base] {
				_ = os.Remove(filepath.Join(c.dir, file.Name()))
			}
			continue
		}
		if filepath.Ext(file.Name()) != ".json" {
			if strings.Contains(file.Name(), ".tmp-") {
				_ = os.Remove(filepath.Join(c.dir, file.Name()))
			}
			continue
		}
		metadataPath := filepath.Join(c.dir, file.Name())
		metadata, err := readMarkdownImageCacheMetadata(metadataPath)
		if err != nil {
			_ = os.Remove(metadataPath)
			continue
		}
		base := file.Name()[:len(file.Name())-len(".json")]
		entries = append(entries, cacheMetadataEntry{
			dataPath:     filepath.Join(c.dir, base+".bin"),
			metadataPath: metadataPath,
			metadata:     metadata,
		})
	}
	return entries, nil
}

func readMarkdownImageCacheMetadata(path string) (markdownImageCacheMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return markdownImageCacheMetadata{}, err
	}
	var metadata markdownImageCacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return markdownImageCacheMetadata{}, err
	}
	return metadata, nil
}

func (c *markdownImageCache) removeEntryLocked(dataPath, metadataPath string) bool {
	ok := true
	for _, path := range []string{dataPath, metadataPath} {
		if err := c.remove(path); err != nil && !os.IsNotExist(err) {
			ok = false
		}
	}
	return ok
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write cache temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows does not replace an existing destination with Rename. The
		// temporary file is already complete and synced, so remove then retry.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace cache file: %w", err)
		}
		if retryErr := os.Rename(tmpName, path); retryErr != nil {
			return fmt.Errorf("commit cache file: %w", retryErr)
		}
	}
	return nil
}
