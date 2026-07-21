package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMarkdownRemoteImageIPPolicyRejectsSpecialUseNetworks(t *testing.T) {
	rejected := []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
		"172.16.0.1", "192.0.0.1", "192.0.2.1", "192.31.196.1", "192.52.193.1",
		"192.88.99.1", "192.168.1.1", "192.175.48.1", "198.18.0.1", "198.51.100.1",
		"203.0.113.1", "224.0.0.1", "240.0.0.1", "::1", "64:ff9b::1",
		"2001:db8::1", "2620:4f:8000::1", "3fff::1", "5f00::1", "fc00::1",
		"fec0::1", "fe80::1", "ff00::1",
	}
	for _, raw := range rejected {
		if isPublicMarkdownImageIP(net.ParseIP(raw)) {
			t.Errorf("special-use address %s was allowed", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicMarkdownImageIP(net.ParseIP(raw)) {
			t.Errorf("public address %s was rejected", raw)
		}
	}
}

func TestMarkdownResponseCacheTTLHonorsRestrictiveHeadersAndCurrentAge(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{name: "no store field form", header: http.Header{"Cache-Control": {`no-store="Set-Cookie"`}}, want: 0},
		{name: "no cache field form", header: http.Header{"Cache-Control": {`no-cache="Set-Cookie"`}}, want: 0},
		{name: "vary star", header: http.Header{"Vary": {"*"}}, want: 0},
		{name: "subtract age", header: http.Header{"Cache-Control": {"max-age=3600"}, "Age": {"1800"}}, want: 30 * time.Minute},
		{name: "maximum one hour", header: http.Header{"Cache-Control": {"max-age=7200"}}, want: time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownResponseCacheTTL(tt.header, now); got != tt.want {
				t.Fatalf("TTL = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarkdownRemoteImageLoaderFetchesCachesAndReportsProgress(t *testing.T) {
	pngData := encodeTestPNG(t, 2, 2)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(pngData)
	}))
	defer server.Close()

	cache, err := newMarkdownImageCache(t.TempDir(), markdownImageCacheMaxBytes, time.Now)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	var events []MarkdownImageProgress
	loader := newMarkdownRemoteImageLoader(cache, func(progress MarkdownImageProgress) {
		events = append(events, progress)
	}, server.Client(), true)

	result, err := loader.Load(context.Background(), "request-1", server.URL+"/image.png")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Cached || result.ContentType != "image/png" || result.Width != 2 || result.Height != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(events) == 0 || events[len(events)-1].State != "complete" || events[len(events)-1].Bytes != int64(len(pngData)) {
		t.Fatalf("progress events = %+v", events)
	}

	cached, err := loader.Load(context.Background(), "request-2", server.URL+"/image.png")
	if err != nil {
		t.Fatalf("cached Load: %v", err)
	}
	if !cached.Cached {
		t.Fatal("second result was not a cache hit")
	}
	if requests.Load() != 1 {
		t.Fatalf("server requests = %d, want 1", requests.Load())
	}
}

func TestMarkdownRemoteImageLoaderCancellationDoesNotCachePartialData(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		for i := 0; i < 100; i++ {
			if _, err := w.Write(make([]byte, 32*1024)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	cache, _ := newMarkdownImageCache(t.TempDir(), markdownImageCacheMaxBytes, time.Now)
	loader := newMarkdownRemoteImageLoader(cache, nil, server.Client(), true)
	errCh := make(chan error, 1)
	go func() {
		_, err := loader.Load(context.Background(), "cancel-me", server.URL)
		errCh <- err
	}()
	<-started
	if !loader.Cancel("cancel-me") {
		t.Fatal("active request was not cancelled")
	}
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Load error = %v, want context canceled", err)
	}
	if _, hit, err := cache.Get(server.URL); err != nil || hit {
		t.Fatalf("partial cache hit=%v err=%v", hit, err)
	}
}

func TestMarkdownRemoteImageLoaderClearCancelsWithoutRepopulatingCache(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		for i := 0; i < 100; i++ {
			if _, err := w.Write(make([]byte, 32*1024)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	cache, _ := newMarkdownImageCache(t.TempDir(), markdownImageCacheMaxBytes, time.Now)
	loader := newMarkdownRemoteImageLoader(cache, nil, server.Client(), true)
	errCh := make(chan error, 1)
	go func() {
		_, err := loader.Load(context.Background(), "clear-me", server.URL)
		errCh <- err
	}()
	<-started
	if err := loader.ClearCache(); err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Load error = %v, want context canceled", err)
	}
	if _, hit, err := cache.Get(server.URL); err != nil || hit {
		t.Fatalf("cache repopulated hit=%v err=%v", hit, err)
	}
}

func TestMarkdownRemoteImageLoaderHonorsNoStoreAndRejectsUnsafeURLs(t *testing.T) {
	pngData := encodeTestPNG(t, 1, 1)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(pngData)
	}))
	defer server.Close()

	cache, _ := newMarkdownImageCache(t.TempDir(), markdownImageCacheMaxBytes, time.Now)
	loader := newMarkdownRemoteImageLoader(cache, nil, server.Client(), true)
	for _, requestID := range []string{"one", "two"} {
		if _, err := loader.Load(context.Background(), requestID, server.URL); err != nil {
			t.Fatalf("Load no-store: %v", err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("no-store requests = %d, want 2", requests.Load())
	}

	if _, err := loader.Load(context.Background(), "http", "http://example.com/image.png"); err == nil {
		t.Fatal("expected HTTP URL rejection")
	}
	if _, err := loader.Load(context.Background(), "credentials", "https://user:pass@example.com/image.png"); err == nil {
		t.Fatal("expected credential URL rejection")
	}

	productionLoader := newMarkdownRemoteImageLoader(cache, nil, nil, false)
	_, err := productionLoader.Load(context.Background(), "private", server.URL)
	if err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("private-address error = %v", err)
	}
}
