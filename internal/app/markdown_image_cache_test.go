package app

import (
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
