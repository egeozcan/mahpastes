package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Server mode's inline-preview ceiling (serverInlinePreviewMaxBytes) must hold
// for every route the web UI decodes inline: the thumb route's passthrough
// originals and /data?preview=1. Explicit downloads stay unrestricted.

func withPreviewCap(t *testing.T, limit int64) {
	t.Helper()
	old := serverInlinePreviewMaxBytes
	serverInlinePreviewMaxBytes = limit
	t.Cleanup(func() { serverInlinePreviewMaxBytes = old })
}

func previewGet(t *testing.T, handler func(http.ResponseWriter, *http.Request), method string, id int64, route string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/clips/"+strconv.FormatInt(id, 10)+route, nil)
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	handler(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	return rec
}

func TestServerThumbRefusesPassthroughOriginalOverPreviewCap(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)
	manager := NewAPIManager(app)

	// A small PNG is passed through as its original bytes.
	small := encodePNG(t, noisyImage(64, 48, false))
	id, hash := insertImageClip(t, app, small, "image/png")
	withPreviewCap(t, int64(len(small))-1)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := previewGet(t, manager.handleGetClipThumb, method, id, "/thumb?h="+hash)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s over-cap passthrough: status %d, want 413", method, rec.Code)
		}
		if bytes.Contains(rec.Body.Bytes(), small) || rec.Header().Get("Content-Type") == "image/png" {
			t.Fatalf("%s over-cap passthrough leaked the original", method)
		}
	}

	// The desktop route (uncapped Serve) is unchanged.
	if rec := serveThumb(t, tc, id, hash, nil); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("desktop Serve: status %d", rec.Code)
	}

	// At the cap it is served.
	serverInlinePreviewMaxBytes = int64(len(small))
	if rec := previewGet(t, manager.handleGetClipThumb, http.MethodGet, id, "/thumb?h="+hash); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("at-cap passthrough: status %d", rec.Code)
	}
}

// A generation that reads a small revision while the clip is overwritten with
// a larger one falls back to serving the original — and the cap must be
// checked against that new revision, not the one the decision was made for.
func TestServerThumbCapChecksRevisionActuallyServed(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	tc := newTestThumbCache(t, app)
	manager := NewAPIManager(app)

	small := encodePNG(t, noisyImage(64, 48, false))
	big := encodePNG(t, noisyImage(400, 300, false))
	id, hash := insertImageClip(t, app, small, "image/png")
	withPreviewCap(t, int64(len(small)))

	tc.beforePublish = func() {
		tc.beforePublish = nil
		if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?", big, computeContentHash(big), id); err != nil {
			t.Error(err)
		}
	}
	rec := previewGet(t, manager.handleGetClipThumb, http.MethodGet, id, "/thumb?h="+hash)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("enlarged mid-generation: status %d, want 413", rec.Code)
	}
}

func TestServerDataPreviewEnforcesCapOnCurrentRevision(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	manager := NewAPIManager(app)

	small := encodePNG(t, noisyImage(64, 48, false))
	id, _ := insertImageClip(t, app, small, "image/png")
	withPreviewCap(t, int64(len(small)))

	if rec := previewGet(t, manager.handleGetClipData, http.MethodGet, id, "/data?preview=1"); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("under-cap preview: status %d", rec.Code)
	}

	// Enlarge the clip after the UI recorded its size: the server measures
	// the revision it serves, so a stale card size cannot get it through.
	big := append(append([]byte{}, small...), make([]byte, 2<<20)...)
	if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?", big, computeContentHash(big), id); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := previewGet(t, manager.handleGetClipData, method, id, "/data?preview=1")
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s over-cap preview: status %d, want 413", method, rec.Code)
		}
		if rec.Body.Len() > 1024 {
			t.Fatalf("%s over-cap preview wrote %d bytes", method, rec.Body.Len())
		}
	}

	// Explicit downloads are unrestricted.
	if rec := previewGet(t, manager.handleGetClipData, http.MethodGet, id, "/data"); rec.Code != http.StatusOK || rec.Body.Len() != len(big) {
		t.Fatalf("download: status %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

// previewCapWriter is the backstop for a clip enlarged between the
// measurement and serveStoredClip's own read: it judges the size the response
// declares, which comes from the body actually opened.
func TestPreviewCapWriterRefusesDeclaredOversize(t *testing.T) {
	manager := NewAPIManager(&App{db: newServerTestDB(t)})
	serve := func(contentLength, contentRange string, status int) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		pw := &previewCapWriter{ResponseWriter: rec, limit: 10, refuse: func(size int64) {
			manager.previewTooLarge(rec, size, 10)
		}}
		pw.Header().Set("Content-Type", "image/png")
		if contentLength != "" {
			pw.Header().Set("Content-Length", contentLength)
		}
		if contentRange != "" {
			pw.Header().Set("Content-Range", contentRange)
		}
		if status != http.StatusOK {
			pw.WriteHeader(status)
		}
		_, _ = pw.Write([]byte("0123456789ABCDEF"[:5]))
		pw.finish()
		return rec
	}
	if rec := serve("11", "", http.StatusOK); rec.Code != http.StatusRequestEntityTooLarge || bytes.Contains(rec.Body.Bytes(), []byte("01234")) {
		t.Fatalf("200 over cap: status %d body %q", rec.Code, rec.Body.String())
	}
	if rec := serve("5", "bytes 0-4/11", http.StatusPartialContent); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("206 of an over-cap clip: status %d", rec.Code)
	}
	if rec := serve("", "", http.StatusOK); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("undeclared size: status %d, want refusal", rec.Code)
	}
	if rec := serve("10", "", http.StatusOK); rec.Code != http.StatusOK || rec.Body.String() != "01234" {
		t.Fatalf("at cap: status %d body %q", rec.Code, rec.Body.String())
	}
}
