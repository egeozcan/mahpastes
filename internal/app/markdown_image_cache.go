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
// mutex: files are only ever replaced by rename, and a size mismatch with the
// index is treated as a miss.
type markdownImageCache struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	now      func() time.Time
	index    map[string]*markdownImageIndexEntry
	total    int64
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
	cache := &markdownImageCache{dir: dir, maxBytes: maxBytes, now: now}
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
	c.dropLocked(key)
	if err := writeAtomicFile(dataPath, data, 0o600); err != nil {
		_ = os.Remove(metadataPath)
		return err
	}
	if err := writeAtomicFile(metadataPath, encodedMetadata, 0o600); err != nil {
		_ = os.Remove(dataPath)
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

func (c *markdownImageCache) Get(rawURL string) (markdownImageCacheEntry, bool, error) {
	key := markdownImageCacheKey(rawURL)
	dataPath, _ := c.keyPaths(key)

	c.mu.Lock()
	entry := c.index[key]
	if entry == nil {
		c.mu.Unlock()
		return markdownImageCacheEntry{}, false, nil
	}
	if !c.now().UTC().Before(entry.expiresAt) {
		c.dropLocked(key)
		c.removeKeyFilesLocked(key)
		c.mu.Unlock()
		return markdownImageCacheEntry{}, false, nil
	}
	want := *entry
	c.mu.Unlock()

	data, err := os.ReadFile(dataPath)
	if err != nil || int64(len(data)) != want.size {
		c.mu.Lock()
		// Only if the entry is still the one this read was checked against: a
		// concurrent Put may have just replaced it with a good one.
		if c.index[key] == entry {
			c.dropLocked(key)
			c.removeKeyFilesLocked(key)
		}
		c.mu.Unlock()
		if err != nil && !os.IsNotExist(err) {
			return markdownImageCacheEntry{}, false, fmt.Errorf("read cached image: %w", err)
		}
		return markdownImageCacheEntry{}, false, nil
	}

	now := c.now().UTC()
	c.mu.Lock()
	if c.index[key] == entry {
		entry.lastAccess = now
	}
	c.mu.Unlock()
	// Persists the access for the next startup's LRU order; best-effort.
	_ = os.Chtimes(dataPath, now, now)
	return markdownImageCacheEntry{Data: data, ContentType: want.contentType}, true, nil
}

func (c *markdownImageCache) Stats() (MarkdownImageCacheStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	return MarkdownImageCacheStats{Entries: len(c.index), Bytes: c.total}, nil
}

func (c *markdownImageCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.index = map[string]*markdownImageIndexEntry{}
	c.total = 0
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

func (c *markdownImageCache) removeKeyFilesLocked(key string) {
	dataPath, metadataPath := c.keyPaths(key)
	c.removeEntryLocked(dataPath, metadataPath)
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
	now := c.now().UTC()
	for key, entry := range c.index {
		if !now.Before(entry.expiresAt) {
			c.dropLocked(key)
			c.removeKeyFilesLocked(key)
		}
	}
	if c.maxBytes <= 0 || c.total <= c.maxBytes {
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
		if c.total <= c.maxBytes {
			break
		}
		c.dropLocked(entry.key)
		c.removeKeyFilesLocked(entry.key)
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

func (c *markdownImageCache) removeEntryLocked(dataPath, metadataPath string) {
	_ = os.Remove(dataPath)
	_ = os.Remove(metadataPath)
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
