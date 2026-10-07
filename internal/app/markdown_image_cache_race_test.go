package app

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A Put that replaces an entry while a Get is between its index snapshot and
// its unlocked read must not hand back the new bytes under the old entry's
// content type.
func TestMarkdownImageCacheGetNeverMixesRevisions(t *testing.T) {
	cache, err := newMarkdownImageCache(t.TempDir(), 1<<20, nil)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	const url = "https://example.com/pic"
	png := []byte("PNGPNGPNG")
	jpeg := []byte("JPGJPGJPG") // same length: the size check cannot catch it
	if err := cache.Put(url, png, "image/png", time.Hour); err != nil {
		t.Fatalf("Put png: %v", err)
	}
	replaced := false
	cache.beforeRead = func() {
		if replaced {
			return
		}
		replaced = true
		if err := cache.Put(url, jpeg, "image/jpeg", time.Hour); err != nil {
			t.Errorf("Put jpeg: %v", err)
		}
	}
	entry, hit, err := cache.Get(url)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !replaced {
		t.Fatal("hook did not run")
	}
	if hit {
		matches := (bytes.Equal(entry.Data, png) && entry.ContentType == "image/png") ||
			(bytes.Equal(entry.Data, jpeg) && entry.ContentType == "image/jpeg")
		if !matches {
			t.Fatalf("Get returned %q labelled %q", entry.Data, entry.ContentType)
		}
	}
}

// An eviction whose delete fails (Windows, file open in a concurrent Get) must
// keep the bytes accounted and retry the delete on a later prune.
func TestMarkdownImageCacheRetriesFailedEvictionDeletes(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cache, err := newMarkdownImageCache(dir, 12, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	locked := true
	oneKey := markdownImageCacheKey("https://example.com/one")
	cache.remove = func(path string) error {
		if locked && strings.Contains(path, oneKey) {
			return errors.New("sharing violation")
		}
		return os.Remove(path)
	}
	put := func(url string) {
		t.Helper()
		if err := cache.Put(url, []byte("123456"), "image/png", time.Hour); err != nil {
			t.Fatalf("Put %s: %v", url, err)
		}
		now = now.Add(time.Second)
	}
	put("https://example.com/one")
	put("https://example.com/two")
	put("https://example.com/three") // evicts one; its delete fails

	if _, hit, _ := cache.Get("https://example.com/one"); hit {
		t.Fatal("evicted entry still served")
	}
	dataPath, _ := cache.keyPaths(oneKey)
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("expected undeletable file to remain: %v", err)
	}
	// The undeleted file's 6 bytes still count, on top of the live entries'
	// (which is why the budget may evict more live entries to compensate).
	stats, _ := cache.Stats()
	if want := int64(stats.Entries*6 + 6); stats.Bytes != want {
		t.Fatalf("stats = %+v, want Bytes %d (live + undeleted)", stats, want)
	}
	if stats.Bytes > 12 {
		t.Fatalf("stats.Bytes = %d over budget", stats.Bytes)
	}

	locked = false
	stats, _ = cache.Stats() // prunes, which retries the delete
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("stale file not deleted on retry: %v", err)
	}
	if want := int64(stats.Entries * 6); stats.Bytes != want {
		t.Fatalf("stats = %+v after retry, want Bytes %d", stats, want)
	}
}
