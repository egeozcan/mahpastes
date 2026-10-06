package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A newer gallery load cancels a content search still scanning for an older
// keystroke, instead of letting it read every text clip to the end.
func TestSearchClipsSupersededByNewerLoad(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	body := bytes.Repeat([]byte("abcdefghij"), 200_000) // 2 MB per clip
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		if _, err := tx.Exec(`INSERT INTO clips (filename, content_type, data) VALUES (?, 'text/plain', ?)`,
			fmt.Sprintf("big-%d.txt", i), body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Baseline: how long the full scan takes when nothing supersedes it.
	start := time.Now()
	if _, err := a.SearchClips(false, nil, nil, "no-such-needle", true, "created", "desc"); err != nil {
		t.Fatalf("uncancelled search: %v", err)
	}
	full := time.Since(start)

	type result struct {
		err     error
		elapsed time.Duration
	}
	before := a.beginGalleryLoad(false)
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		_, err := a.SearchClips(false, nil, nil, "no-such-needle", true, "created", "desc")
		done <- result{err, time.Since(start)}
	}()
	// Wait until the search has started its load, then let it get scanning.
	for {
		a.galleryLoadMu.Lock()
		started := a.galleryLoadCtx != before
		a.galleryLoadMu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(full / 20)
	// The next keystroke's load (a first page) supersedes it.
	if _, err := a.ListClipsPage(ClipListRequest{Mode: "search", Query: "x", Limit: 1}); err != nil {
		t.Fatalf("newer load: %v", err)
	}
	r := <-done
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("superseded search: err = %v, want context.Canceled (full scan %v, took %v)", r.err, full, r.elapsed)
	}

	// A continuation page joins the load in progress rather than cancelling it.
	ctx := a.beginGalleryLoad(true)
	if _, err := a.ListClipsPage(ClipListRequest{Mode: "all", Offset: 10, Limit: 5}); err != nil {
		t.Fatalf("continuation page: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("a continuation page cancelled the current load")
	}
}
