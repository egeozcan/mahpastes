package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkdownImageCacheExpiresAndUsesExactURLKeys(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cache, err := newMarkdownImageCache(t.TempDir(), 250*1024*1024, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	data := encodeTestPNG(t, 1, 1)
	if err := cache.Put("https://example.com/image.png?token=one", data, "image/png", time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, hit, err := cache.Get("https://example.com/image.png?token=two"); err != nil || hit {
		t.Fatalf("different exact URL hit=%v err=%v", hit, err)
	}
	entry, hit, err := cache.Get("https://example.com/image.png?token=one")
	if err != nil || !hit {
		t.Fatalf("cache hit=%v err=%v", hit, err)
	}
	if entry.ContentType != "image/png" || len(entry.Data) != len(data) {
		t.Fatalf("entry = %+v", entry)
	}

	now = now.Add(time.Hour + time.Second)
	if _, hit, err := cache.Get("https://example.com/image.png?token=one"); err != nil || hit {
		t.Fatalf("expired hit=%v err=%v", hit, err)
	}
}

func TestMarkdownImageCacheEvictsLeastRecentlyUsedAndClears(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cache, err := newMarkdownImageCache(t.TempDir(), 12, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new cache: %v", err)
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
	if _, _, err := cache.Get("https://example.com/one"); err != nil {
		t.Fatalf("touch one: %v", err)
	}
	now = now.Add(time.Second)
	put("https://example.com/three")

	if _, hit, _ := cache.Get("https://example.com/one"); !hit {
		t.Fatal("recently used entry was evicted")
	}
	if _, hit, _ := cache.Get("https://example.com/two"); hit {
		t.Fatal("least recently used entry was retained")
	}
	stats, err := cache.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != 2 || stats.Bytes != 12 {
		t.Fatalf("stats = %+v", stats)
	}
	if err := cache.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	stats, err = cache.Stats()
	if err != nil {
		t.Fatalf("Stats after clear: %v", err)
	}
	if stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("stats after clear = %+v", stats)
	}
}

// A hit used to rewrite (and fsync) the metadata file under the global mutex
// to record its access time. It now records the access in the in-memory index
// and as the data file's mtime only.
func TestMarkdownImageCacheHitDoesNotRewriteMetadata(t *testing.T) {
	now := time.Now().Add(time.Hour).UTC()
	dir := t.TempDir()
	cache, err := newMarkdownImageCache(dir, 250*1024*1024, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	url := "https://example.com/hit.png"
	if err := cache.Put(url, []byte("abcdef"), "image/png", time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	dataPath, metadataPath := cache.paths(url)
	before, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	now = now.Add(5 * time.Minute)
	if _, hit, err := cache.Get(url); err != nil || !hit {
		t.Fatalf("hit=%v err=%v", hit, err)
	}
	after, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata after hit: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a cache hit rewrote the metadata file")
	}
	info, err := os.Stat(dataPath)
	if err != nil {
		t.Fatalf("stat data: %v", err)
	}
	if !info.ModTime().Equal(now) {
		t.Fatalf("data mtime = %v, want the access time %v", info.ModTime(), now)
	}
}

// Put prunes from the in-memory index; it no longer rescans the directory.
func TestMarkdownImageCachePutDoesNotRescanDirectory(t *testing.T) {
	dir := t.TempDir()
	cache, err := newMarkdownImageCache(dir, 250*1024*1024, nil)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	// A directory scan deletes leftover temp files; an index prune never sees one.
	stray := filepath.Join(dir, "leftover.json.tmp-123")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	if err := cache.Put("https://example.com/a.png", []byte("abc"), "image/png", time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("Put rescanned the directory (stray temp file removed): %v", err)
	}
	// The startup scan still cleans it up.
	if _, err := newMarkdownImageCache(dir, 250*1024*1024, nil); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("startup scan left the stray temp file: %v", err)
	}
}

// LRU order survives a restart: the access a hit recorded as the data file's
// mtime is folded back in by the startup scan.
func TestMarkdownImageCacheLRUSurvivesRestart(t *testing.T) {
	// Ahead of the wall clock, so files written by Put (real mtime) never look
	// more recent than an access recorded through the fake clock.
	now := time.Now().Add(time.Hour).UTC()
	clock := func() time.Time { return now }
	dir := t.TempDir()
	cache, err := newMarkdownImageCache(dir, 12, clock)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	for _, url := range []string{"https://example.com/one", "https://example.com/two"} {
		if err := cache.Put(url, []byte("123456"), "image/png", time.Hour); err != nil {
			t.Fatalf("Put: %v", err)
		}
		now = now.Add(time.Second)
	}
	if _, hit, _ := cache.Get("https://example.com/one"); !hit {
		t.Fatal("one missing")
	}
	now = now.Add(time.Second)

	reopened, err := newMarkdownImageCache(dir, 12, clock)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if stats, _ := reopened.Stats(); stats.Entries != 2 || stats.Bytes != 12 {
		t.Fatalf("reopened stats = %+v", stats)
	}
	if err := reopened.Put("https://example.com/three", []byte("123456"), "image/png", time.Hour); err != nil {
		t.Fatalf("Put three: %v", err)
	}
	if _, hit, _ := reopened.Get("https://example.com/one"); !hit {
		t.Fatal("the entry read before the restart was evicted")
	}
	if _, hit, _ := reopened.Get("https://example.com/two"); hit {
		t.Fatal("the least recently used entry survived")
	}
}
