package app

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-clipboard/internal/bridgeiface"
)

// withKey attaches an apiKeyContext to a request, mimicking authMiddleware.
func withKey(req *http.Request, kc *apiKeyContext) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, kc))
}

// --- H1: SSE event stream is closed to tag-scoped keys ---

func TestRequireUnscopedSSERejectsScopedKeys(t *testing.T) {
	manager := NewAPIManager(&App{db: newServerTestDB(t)})
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if bridgeiface.SSEAuthFrom(r.Context()) == nil {
			t.Error("unscoped stream must carry SSEAuth for cap accounting / revalidation")
		}
		w.WriteHeader(http.StatusOK)
	})
	h := manager.requireUnscopedSSE(next)

	scoped := withKey(httptest.NewRequest("GET", "/api/v1/events", nil), &apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: 9})
	rec := httptest.NewRecorder()
	h(rec, scoped)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scoped key: code = %d, want 403", rec.Code)
	}
	if called {
		t.Fatal("scoped key must not reach the broker")
	}

	called = false
	unscoped := withKey(httptest.NewRequest("GET", "/api/v1/events", nil), &apiKeyContext{KeyID: 2, Role: "viewer", ScopedTagID: 0})
	rec = httptest.NewRecorder()
	h(rec, unscoped)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("unscoped key should pass through: code = %d, called = %v", rec.Code, called)
	}
}

// --- H2: folder_tag respects tag scope ---

func TestListClipsViaAppFolderTagEnforcesScope(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1,'scope','#111'),(2,'other','#222')`); err != nil {
		t.Fatalf("seed tags: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/clips?folder_tag=2&archived=false", nil)
	rec := httptest.NewRecorder()
	manager.handleListClipsViaApp(rec, req, 50, 0, &apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: 1})

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"clips":[]`) || !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Fatalf("out-of-scope folder_tag must yield no clips, got %s", rec.Body.String())
	}
}

// --- H3: clip data is served as a hardened download ---

func TestGetClipDataHardenedHeaders(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})
	// Worst case: a text/html clip with an EMPTY filename (the case that used to
	// render inline because Content-Disposition was only set for named clips).
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/html', '<script>alert(1)</script>', '')`); err != nil {
		t.Fatalf("seed clip: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/clips/1/data", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	manager.handleGetClipData(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment even with empty filename", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want a sandbox policy", got)
	}
}

// --- L13: settings reads are scope/role-restricted ---

func TestGetSettingDeniesScopedAndUnprivilegedKeys(t *testing.T) {
	manager := NewAPIManager(&App{db: newServerTestDB(t)})

	scoped := httptest.NewRequest("GET", "/api/v1/settings/share_identity", nil)
	scoped.SetPathValue("key", "share_identity")
	rec := httptest.NewRecorder()
	manager.handleGetSetting(rec, withKey(scoped, &apiKeyContext{KeyID: 1, Role: "admin", ScopedTagID: 4}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scoped key reading settings: code = %d, want 403", rec.Code)
	}

	viewer := httptest.NewRequest("GET", "/api/v1/settings/share_identity", nil)
	viewer.SetPathValue("key", "share_identity")
	rec = httptest.NewRecorder()
	manager.handleGetSetting(rec, withKey(viewer, &apiKeyContext{KeyID: 2, Role: "viewer"}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer reading non-whitelisted setting: code = %d, want 403", rec.Code)
	}
}

// --- L15: bulk download neutralizes attacker filenames (zip-slip) ---

func TestBulkDownloadSanitizesZipEntryNames(t *testing.T) {
	db := newServerTestDB(t)
	manager := NewAPIManager(&App{db: db})
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'x', '../../../etc/evil.sh')`); err != nil {
		t.Fatalf("seed clip: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/v1/clips/bulk/download", strings.NewReader(`{"ids":[1]}`))
	rec := httptest.NewRecorder()
	manager.handleBulkDownload(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(zr.File))
	}
	name := zr.File[0].Name
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		t.Fatalf("zip entry %q still contains traversal characters", name)
	}
	if name != "1_evil.sh" {
		t.Fatalf("zip entry = %q, want 1_evil.sh", name)
	}
}
