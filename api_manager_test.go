package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAPI_MergeTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Create two tags
	src, err := app.CreateTag("api-src")
	if err != nil {
		t.Fatalf("CreateTag src failed: %v", err)
	}
	dst, err := app.CreateTag("api-dst")
	if err != nil {
		t.Fatalf("CreateTag dst failed: %v", err)
	}

	// Create an admin API key for testing
	testKey := "test-admin-key-12345"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err = app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Create an APIManager with the app
	am := &APIManager{app: app}

	// Build the mux with routes (inline the mux building logic)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tags/{id}/merge", am.authMiddleware(am.requireRole("admin", am.handleMergeTag)))

	// Also add the auth and CORS middleware
	handler := am.corsMiddleware(mux)

	// Create the request
	body := map[string]int64{"dest_id": dst.ID}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/tags/%d/merge", src.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))

	// Record the response
	rec := httptest.NewRecorder()

	// Serve through the mux (which will handle routing and auth)
	handler.ServeHTTP(rec, req)

	// Verify response status
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify source tag is deleted
	var count int
	err = app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, src.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("source tag should be deleted, but found %d", count)
	}

	// Verify destination tag still exists
	err = app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, dst.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("destination tag should exist, but found %d", count)
	}
}

func TestAPI_MaintenanceVacuum(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Create an admin API key for testing
	testKey := "test-admin-key-12345"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Create an APIManager with the app
	am := &APIManager{app: app}

	// Build the mux with routes (inline the mux building logic)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/maintenance/vacuum", am.authMiddleware(am.requireRole("admin", am.handleVacuum)))

	// Also add the auth and CORS middleware
	handler := am.corsMiddleware(mux)

	// Create the request
	req := httptest.NewRequest("POST", "/api/v1/maintenance/vacuum", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))

	// Record the response
	rec := httptest.NewRecorder()

	// Serve through the mux (which will handle routing and auth)
	handler.ServeHTTP(rec, req)

	// Verify response status
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify response structure
	var resp struct {
		Before int64 `json:"before"`
		After  int64 `json:"after"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify before/after are nonzero
	if resp.Before == 0 {
		t.Fatalf("expected nonzero before size")
	}
	if resp.After == 0 {
		t.Fatalf("expected nonzero after size")
	}
}

func TestAPI_MaintenanceStaleFiles_ListAndClean(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Create an admin API key for testing
	testKey := "test-admin-key-12345"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Create an APIManager with the app
	am := &APIManager{app: app}

	// Build the mux with routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/maintenance/stale-files", am.authMiddleware(am.requireRole("admin", am.handleListStaleFiles)))
	mux.HandleFunc("POST /api/v1/maintenance/stale-files/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanStaleFiles)))

	// Also add the auth and CORS middleware
	handler := am.corsMiddleware(mux)

	// Seed a stale temp file
	dataDir, err := getDataDir()
	if err != nil {
		t.Fatalf("getDataDir: %v", err)
	}
	clipTempDir := filepath.Join(dataDir, "clip_temp_files")
	if err := os.MkdirAll(clipTempDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(clipTempDir, "stale-api.bin")
	if err := os.WriteFile(p, []byte("xx"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(p, past, past)

	// GET returns the stale file
	req := httptest.NewRequest("GET", "/api/v1/maintenance/stale-files", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d, body %s", rec.Code, rec.Body.String())
	}
	var files []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(files) < 1 {
		t.Fatalf("expected ≥1 stale file, got %d", len(files))
	}

	// POST cleans them
	req2 := httptest.NewRequest("POST", "/api/v1/maintenance/stale-files/clean", nil)
	req2.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST status %d, body %s", rec2.Code, rec2.Body.String())
	}
	var respBody struct {
		Count int   `json:"count"`
		Bytes int64 `json:"bytes"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if respBody.Count < 1 {
		t.Fatalf("expected ≥1 cleaned, got %d", respBody.Count)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be removed")
	}
}

func TestAPI_MaintenanceOrphanRows_ListAndClean(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Create an admin API key for testing
	testKey := "test-admin-key-12345"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Create an APIManager with the app
	am := &APIManager{app: app}

	// Build the mux with routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/maintenance/orphan-rows", am.authMiddleware(am.requireRole("admin", am.handleListOrphanRows)))
	mux.HandleFunc("POST /api/v1/maintenance/orphan-rows/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanOrphanRows)))

	// Also add the auth and CORS middleware
	handler := am.corsMiddleware(mux)

	// Seed an orphan plugin_storage row.
	if _, err := app.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}
	if _, err := app.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/maintenance/orphan-rows", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d, body %s", rec.Code, rec.Body.String())
	}
	var report struct {
		PluginStorage int `json:"plugin_storage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if report.PluginStorage < 1 {
		t.Fatalf("expected ≥1 plugin_storage orphan, got %d", report.PluginStorage)
	}

	req2 := httptest.NewRequest("POST", "/api/v1/maintenance/orphan-rows/clean", nil)
	req2.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST status %d, body %s", rec2.Code, rec2.Body.String())
	}

	var count int
	app.db.QueryRow(`SELECT COUNT(*) FROM plugin_storage WHERE plugin_id = 99999`).Scan(&count)
	if count != 0 {
		t.Fatalf("orphan should be removed, got %d", count)
	}
}

// insertScopedAdminKey inserts a scoped admin API key tied to scopedTagID and
// returns the raw key string.
func insertScopedAdminKey(t *testing.T, app *App, rawKey string, scopedTagID int64) {
	t.Helper()
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id, is_revoked) VALUES (?, ?, ?, ?, ?, ?)`,
		"scoped-test-key", keyHash, "scpd-", "admin", scopedTagID, false,
	)
	if err != nil {
		t.Fatalf("insert scoped key: %v", err)
	}
}

func TestAPI_MergeTag_BlockedForScopedKeyOutsideScope(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Tags under "work" scope and unrelated "personal" tags.
	work, err := app.CreateTag("work")
	if err != nil {
		t.Fatalf("CreateTag work: %v", err)
	}
	src, err := app.CreateTag("personal/src")
	if err != nil {
		t.Fatalf("CreateTag personal/src: %v", err)
	}
	dst, err := app.CreateTag("personal/dst")
	if err != nil {
		t.Fatalf("CreateTag personal/dst: %v", err)
	}

	// Scoped admin key bound to "work".
	scopedKey := "scoped-merge-key-99999"
	insertScopedAdminKey(t, app, scopedKey, work.ID)

	am := &APIManager{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tags/{id}/merge", am.authMiddleware(am.requireRole("admin", am.handleMergeTag)))
	handler := am.corsMiddleware(mux)

	body := map[string]int64{"dest_id": dst.ID}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/tags/%d/merge", src.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", scopedKey))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAPI_Maintenance_RejectsScopedKey(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	scopeTag, err := app.CreateTag("scope-tag")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	scopedKey := "scoped-maint-key-99999"
	insertScopedAdminKey(t, app, scopedKey, scopeTag.ID)

	am := &APIManager{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/maintenance/vacuum", am.authMiddleware(am.requireRole("admin", am.handleVacuum)))
	mux.HandleFunc("GET /api/v1/maintenance/stale-files", am.authMiddleware(am.requireRole("admin", am.handleListStaleFiles)))
	mux.HandleFunc("POST /api/v1/maintenance/stale-files/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanStaleFiles)))
	mux.HandleFunc("GET /api/v1/maintenance/orphan-rows", am.authMiddleware(am.requireRole("admin", am.handleListOrphanRows)))
	mux.HandleFunc("POST /api/v1/maintenance/orphan-rows/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanOrphanRows)))
	handler := am.corsMiddleware(mux)

	cases := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/maintenance/vacuum"},
		{"GET", "/api/v1/maintenance/stale-files"},
		{"POST", "/api/v1/maintenance/stale-files/clean"},
		{"GET", "/api/v1/maintenance/orphan-rows"},
		{"POST", "/api/v1/maintenance/orphan-rows/clean"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", scopedKey))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want %d, body = %s", tc.method, tc.path, rec.Code, http.StatusForbidden, rec.Body.String())
		}
	}
}

func TestAPI_DeleteTag_FollowBlockerReturns409(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("followed-tag")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// Insert a follow row referencing the tag to trigger the conflict.
	if _, err := app.db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, created_at) VALUES ('peer1', X'DEADBEEF', ?, strftime('%s','now'))`,
		tag.ID,
	); err != nil {
		t.Fatalf("insert follow: %v", err)
	}

	testKey := "test-admin-del-follow-key"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	if _, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	am := &APIManager{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/tags/{id}", am.authMiddleware(am.requireRole("admin", am.handleDeleteTag)))
	handler := am.corsMiddleware(mux)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/tags/%d", tag.ID), nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("follow")) {
		t.Fatalf("expected 'follow' in body, got: %s", rec.Body.String())
	}
}

func TestAPI_DeleteTag_NotFoundReturns404(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	testKey := "test-admin-del-notfound-key"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	if _, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	am := &APIManager{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/tags/{id}", am.authMiddleware(am.requireRole("admin", am.handleDeleteTag)))
	handler := am.corsMiddleware(mux)

	req := httptest.NewRequest("DELETE", "/api/v1/tags/999999", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
