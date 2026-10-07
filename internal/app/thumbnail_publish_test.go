package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// thumbFilesFor lists every file in the cache dir belonging to hash, plus any
// leftover temporaries.
func thumbFilesFor(t *testing.T, tc *ThumbnailCache, hash string) []string {
	t.Helper()
	entries, err := os.ReadDir(tc.dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), hash) || strings.HasPrefix(e.Name(), ".tmp-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// A generation that read a clip before it was deleted, and finishes after the
// delete's orphan sweep already ran, must not publish a thumbnail of the
// deleted bytes.
func TestThumbnailNotPublishedAfterClipDeletedMidGeneration(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)
	tc.dropDelay = time.Hour // the hook below runs the sweep itself

	id, hash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1600, 1200, false)), "image/jpeg")
	tc.beforePublish = func() {
		if err := app.DeleteClip(id); err != nil {
			t.Error(err)
		}
		// The DropOrphansSoon sweep the delete scheduled, run to completion
		// while the decode is still in flight.
		if err := tc.Prune(true); err != nil {
			t.Error(err)
		}
	}

	if rec := serveThumb(t, tc, id, hash, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("serve of a clip deleted mid-generation: got %d, want 404", rec.Code)
	}
	if files := thumbFilesFor(t, tc, hash); len(files) != 0 {
		t.Fatalf("deleted clip's content left on disk after its sweep: %v", files)
	}
}

// The same holds for an edit: the old revision's hash is gone, so its
// thumbnail is not published, and the response is not marked immutable.
func TestThumbnailNotPublishedAfterClipEditedMidGeneration(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)
	tc.dropDelay = time.Hour

	id, hash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1600, 1200, false)), "image/jpeg")
	newData := encodeJPEG(t, noisyImage(1500, 1100, false))
	edited := false
	tc.beforePublish = func() {
		if edited {
			return
		}
		edited = true
		if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?",
			newData, computeContentHash(newData), id); err != nil {
			t.Error(err)
		}
		if err := tc.Prune(true); err != nil {
			t.Error(err)
		}
	}

	rec := serveThumb(t, tc, id, hash, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve: %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Fatalf("stale revision served immutable: %q", cc)
	}
	if files := thumbFilesFor(t, tc, hash); len(files) != 0 {
		t.Fatalf("old revision's thumbnail published after edit: %v", files)
	}
}

// A generation whose clip is still live publishes normally.
func TestThumbnailPublishedWhileHashLive(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)

	id, hash := insertImageClip(t, app, encodeJPEG(t, noisyImage(1600, 1200, false)), "image/jpeg")
	if rec := serveThumb(t, tc, id, hash, nil); rec.Code != http.StatusOK {
		t.Fatalf("serve: %d", rec.Code)
	}
	if _, ok := tc.lookup(hash); !ok {
		t.Fatal("live clip's thumbnail was not published")
	}
}
