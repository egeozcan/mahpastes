package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-clipboard/plugin"
)

// newTestPluginManager builds a plugin manager wired to the test app, mirroring
// the headless server.
func newTestPluginManager(t *testing.T, app *App) (*plugin.Manager, string) {
	t.Helper()
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir pluginsDir: %v", err)
	}
	pm, err := plugin.NewManager(context.Background(), app.bridge, app.db, pluginsDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	app.pluginManager = pm
	return pm, pluginsDir
}

// createAPIKeyForTest inserts an API key with the given role and returns the
// plaintext key.
func createAPIKeyForTest(t *testing.T, app *App, role string) string {
	t.Helper()
	key := "test-key-" + role + "-12345678"
	hash := sha256.Sum256([]byte(key))
	_, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-"+role, hex.EncodeToString(hash[:]), "test-", role, false,
	)
	if err != nil {
		t.Fatalf("create %s key: %v", role, err)
	}
	return key
}

// setupPluginSearchTest stands up a test app with a plugin manager and an
// imported plugin whose on_search echoes the query length it received, plus
// API keys for the roles under test. Returns the app, the plugin ID, and a
// handler serving the search route with full auth middleware.
func setupPluginSearchTest(t *testing.T) (*App, int64, http.Handler, func()) {
	t.Helper()
	app, cleanup := setupTestApp(t)

	pm, _ := newTestPluginManager(t, app)

	src := `Plugin = { name = "Search Test", version = "1.0.0",
  settings = {
    {key = "entity", type = "search", source = "things", label = "Entity"}
  } }
function on_search(source, query)
    return {{value = tostring(#query), label = "len"}}
end`
	srcPath := filepath.Join(t.TempDir(), "search-test.lua")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	p, err := pm.ImportPlugin(srcPath)
	if err != nil {
		t.Fatalf("ImportPlugin: %v", err)
	}

	am := NewAPIManager(app)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/plugins/{id}/search", am.authMiddleware(am.requireRole("editor", am.handlePluginSearch)))
	handler := am.corsMiddleware(mux)

	return app, p.ID, handler, func() {
		pm.Shutdown()
		cleanup()
	}
}

// postPluginSearch issues the REST call the way rest-glue.js does.
func postPluginSearch(handler http.Handler, pluginID int64, key, source, query string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"source":%q,"query":%q}`, source, query)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/plugins/%d/search", pluginID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAPI_PluginSearch(t *testing.T) {
	app, pluginID, handler, cleanup := setupPluginSearchTest(t)
	defer cleanup()

	editorKey := createAPIKeyForTest(t, app, "editor")
	viewerKey := createAPIKeyForTest(t, app, "viewer")

	t.Run("editor gets choices", func(t *testing.T) {
		rec := postPluginSearch(handler, pluginID, editorKey, "things", "abc")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"value":"3"`) {
			t.Errorf("expected on_search to receive the full query (len 3), got %s", rec.Body.String())
		}
	})

	t.Run("unknown source rejected", func(t *testing.T) {
		rec := postPluginSearch(handler, pluginID, editorKey, "undeclared", "abc")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("viewer role forbidden", func(t *testing.T) {
		rec := postPluginSearch(handler, pluginID, viewerKey, "things", "abc")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("query capped at MaxSearchQueryLength", func(t *testing.T) {
		long := strings.Repeat("x", 400)
		rec := postPluginSearch(handler, pluginID, editorKey, "things", long)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		want := fmt.Sprintf(`"value":"%d"`, plugin.MaxSearchQueryLength)
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("expected on_search to receive at most %d chars, got %s", plugin.MaxSearchQueryLength, rec.Body.String())
		}
	})
}
