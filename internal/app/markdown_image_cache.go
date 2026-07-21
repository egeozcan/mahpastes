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

type markdownImageCache struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	now      func() time.Time
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
	err := cache.pruneLocked()
	cache.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return cache, nil
}

func markdownImageCacheKey(rawURL string) string {
	hash := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(hash[:])
}

func (c *markdownImageCache) paths(rawURL string) (string, string) {
	key := markdownImageCacheKey(rawURL)
	return filepath.Join(c.dir, key+".bin"), filepath.Join(c.dir, key+".json")
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

	dataPath, metadataPath := c.paths(rawURL)
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
	if err := writeAtomicFile(dataPath, data, 0o600); err != nil {
		return err
	}
	if err := writeAtomicFile(metadataPath, encodedMetadata, 0o600); err != nil {
		_ = os.Remove(dataPath)
		return err
	}
	return c.pruneLocked()
}

func (c *markdownImageCache) Get(rawURL string) (markdownImageCacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	dataPath, metadataPath := c.paths(rawURL)
	metadata, err := readMarkdownImageCacheMetadata(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return markdownImageCacheEntry{}, false, nil
		}
		c.removeEntryLocked(dataPath, metadataPath)
		return markdownImageCacheEntry{}, false, nil
	}
	if !c.now().UTC().Before(metadata.ExpiresAt) {
		c.removeEntryLocked(dataPath, metadataPath)
		return markdownImageCacheEntry{}, false, nil
	}
	data, err := os.ReadFile(dataPath)
	if err != nil || int64(len(data)) != metadata.Size {
		c.removeEntryLocked(dataPath, metadataPath)
		if err != nil && !os.IsNotExist(err) {
			return markdownImageCacheEntry{}, false, fmt.Errorf("read cached image: %w", err)
		}
		return markdownImageCacheEntry{}, false, nil
	}
	metadata.LastAccess = c.now().UTC()
	if encoded, err := json.Marshal(metadata); err == nil {
		_ = writeAtomicFile(metadataPath, encoded, 0o600)
	}
	return markdownImageCacheEntry{Data: data, ContentType: metadata.ContentType}, true, nil
}

func (c *markdownImageCache) Stats() (MarkdownImageCacheStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.pruneLocked(); err != nil {
		return MarkdownImageCacheStats{}, err
	}
	entries, err := c.metadataEntriesLocked()
	if err != nil {
		return MarkdownImageCacheStats{}, err
	}
	stats := MarkdownImageCacheStats{Entries: len(entries)}
	for _, entry := range entries {
		stats.Bytes += entry.metadata.Size
	}
	return stats, nil
}

func (c *markdownImageCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
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

func (c *markdownImageCache) pruneLocked() error {
	entries, err := c.metadataEntriesLocked()
	if err != nil {
		return err
	}
	now := c.now().UTC()
	var retained []cacheMetadataEntry
	var total int64
	for _, entry := range entries {
		if !now.Before(entry.metadata.ExpiresAt) {
			c.removeEntryLocked(entry.dataPath, entry.metadataPath)
			continue
		}
		if info, err := os.Stat(entry.dataPath); err != nil || info.Size() != entry.metadata.Size {
			c.removeEntryLocked(entry.dataPath, entry.metadataPath)
			continue
		}
		retained = append(retained, entry)
		total += entry.metadata.Size
	}
	if c.maxBytes <= 0 || total <= c.maxBytes {
		return nil
	}
	sort.Slice(retained, func(i, j int) bool {
		return retained[i].metadata.LastAccess.Before(retained[j].metadata.LastAccess)
	})
	for _, entry := range retained {
		if total <= c.maxBytes {
			break
		}
		c.removeEntryLocked(entry.dataPath, entry.metadataPath)
		total -= entry.metadata.Size
	}
	return nil
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
