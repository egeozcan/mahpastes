package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cacheDataBytesOnDisk sums the sizes of every .bin file left in dir.
func cacheDataBytesOnDisk(t *testing.T, dir string) int64 {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	var total int64
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".bin" {
			continue
		}
		info, err := file.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", file.Name(), err)
		}
		total += info.Size()
	}
	return total
}

// A Clear that cannot delete a file (Windows, file open in a concurrent Get)
// must keep that file's bytes accounted and retry its delete later, rather than
// forgetting it: otherwise stats, expiry and the byte budget no longer see it.
func TestMarkdownImageCacheFailedClearKeepsSurvivorsAccounted(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cache, err := newMarkdownImageCache(dir, 12, func() time.Time { return now })
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

	locked := true
	oneKey := markdownImageCacheKey("https://example.com/one")
	cache.remove = func(path string) error {
		if locked && strings.Contains(path, oneKey) {
			return errors.New("sharing violation")
		}
		return os.Remove(path)
	}
	if err := cache.Clear(); err == nil {
		t.Fatal("Clear reported success although a file survived")
	}
	dataPath, _ := cache.keyPaths(oneKey)
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("expected undeletable file to remain: %v", err)
	}
	if _, hit, _ := cache.Get("https://example.com/one"); hit {
		t.Fatal("cleared entry still served")
	}
	stats, _ := cache.Stats()
	if stats.Entries != 0 || stats.Bytes != 6 {
		t.Fatalf("stats after failed clear = %+v, want 0 entries / 6 bytes", stats)
	}

	put("https://example.com/three")
	put("https://example.com/four")
	stats, _ = cache.Stats()
	onDisk := cacheDataBytesOnDisk(t, dir)
	if stats.Bytes != onDisk {
		t.Fatalf("stats.Bytes = %d, but %d bytes are on disk", stats.Bytes, onDisk)
	}
	if onDisk > 12 {
		t.Fatalf("%d bytes on disk exceed the 12-byte budget", onDisk)
	}

	locked = false
	stats, _ = cache.Stats() // prunes, which retries the delete
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("surviving file not deleted on retry: %v", err)
	}
	if onDisk := cacheDataBytesOnDisk(t, dir); stats.Bytes != onDisk {
		t.Fatalf("stats.Bytes = %d after retry, but %d bytes are on disk", stats.Bytes, onDisk)
	}
}

// A successful Clear leaves nothing behind and nothing accounted.
func TestMarkdownImageCacheClearEmptiesDirAndAccounting(t *testing.T) {
	dir := t.TempDir()
	cache, err := newMarkdownImageCache(dir, 1<<20, nil)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	if err := cache.Put("https://example.com/a", []byte("abc"), "image/png", time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("Clear left %d files", len(files))
	}
	if stats, _ := cache.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("stats after clear = %+v", stats)
	}
	if err := cache.Put("https://example.com/b", []byte("de"), "image/png", time.Hour); err != nil {
		t.Fatalf("Put after clear: %v", err)
	}
}
