package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A newer gallery load supersedes a search still in flight: the older call
// fails with context.Canceled (which the frontend's generation guard
// discards) instead of handing back a stale page.
func TestSearchClipsSupersededByNewerLoad(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	for i := 0; i < 5; i++ {
		insertTestClip(t, a, fmt.Sprintf("note-%d.txt", i), "text/plain", []byte("needle"))
	}

	inQuery := make(chan struct{})
	release := make(chan struct{})
	var hooked bool
	clipListingQueryHook = func(context.Context) {
		if hooked {
			return
		}
		hooked = true
		close(inQuery)
		<-release
	}
	t.Cleanup(func() { clipListingQueryHook = nil })

	done := make(chan error, 1)
	go func() {
		_, err := a.SearchClips(false, nil, nil, "needle", true, "created", "desc")
		done <- err
	}()
	select {
	case <-inQuery:
	case <-time.After(5 * time.Second):
		t.Fatal("search never reached its query")
	}
	// The next keystroke's load (a first page) supersedes it. The hook only
	// parks the first query, so this one runs straight through.
	if _, err := a.ListClipsPage(ClipListRequest{Mode: "search", Query: "x", Limit: 1}); err != nil {
		t.Fatalf("newer load: %v", err)
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("superseded search: err = %v, want context.Canceled", err)
	}

	// A continuation page joins the load in progress rather than cancelling it.
	ctx := a.beginGalleryLoad(true)
	if _, err := a.ListClipsPage(ClipListRequest{Mode: "all", Offset: 2, Limit: 2}); err != nil {
		t.Fatalf("continuation page: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("a continuation page cancelled the current load")
	}
}

// Cancelling a load's context interrupts a statement mid-scan rather than
// waiting for it to finish: the driver turns cancellation into
// sqlite3_interrupt. The statement here would run for minutes.
func TestQueryContextInterruptsRunningStatement(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	var n int64
	err := a.db.QueryRowContext(ctx, `
		WITH RECURSIVE r(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM r WHERE i < 10000000000)
		SELECT COUNT(*) FROM r`).Scan(&n)
	if err == nil {
		t.Fatalf("statement ran to completion (%d) despite cancellation", n)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancellation took %v to stop the statement", elapsed)
	}
}

// Gallery loads are ordered by their generation, not by when the bound call
// reaches Go, and two page sessions (the native window and a browser tab
// under `wails dev`, or a reloaded page) never cancel one another.
func TestNumberedGalleryLoads(t *testing.T) {
	a := &App{}

	g2 := a.beginNumberedGalleryLoad("s1", 2)
	// Generation 1's first page arrives after generation 2's.
	if g1 := a.beginNumberedGalleryLoad("s1", 1); g1.Err() == nil {
		t.Fatal("an older generation got a live context")
	}
	if g2.Err() != nil {
		t.Fatal("an older generation arriving late cancelled the newer load")
	}
	// Later pages of the current generation join it.
	if again := a.beginNumberedGalleryLoad("s1", 2); again != g2 {
		t.Fatal("a continuation page of the current load did not join it")
	}

	// Another session's loads leave this one alone, whatever its counter.
	other := a.beginNumberedGalleryLoad("s2", 1)
	if g2.Err() != nil || other.Err() != nil {
		t.Fatal("one page session cancelled another's load")
	}
	// Unnumbered calls (SearchClips) have their own slot too.
	plain := a.beginGalleryLoad(true)
	if g2.Err() != nil || other.Err() != nil || plain.Err() != nil {
		t.Fatal("an unnumbered load cancelled a numbered one")
	}

	// A newer generation supersedes the current load of its own session.
	g3 := a.beginNumberedGalleryLoad("s1", 3)
	if g2.Err() == nil || g3.Err() != nil || other.Err() != nil {
		t.Fatal("generation 3 did not supersede exactly generation 2")
	}

	// Records are bounded; the least recently used session goes first.
	for i := 0; i < maxGalleryLoadSessions; i++ {
		a.beginNumberedGalleryLoad(fmt.Sprintf("extra-%d", i), 1)
	}
	if len(a.galleryLoads) > maxGalleryLoadSessions {
		t.Fatalf("%d load records kept, want at most %d", len(a.galleryLoads), maxGalleryLoadSessions)
	}
}
