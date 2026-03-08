package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newServerTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "server-test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys%3Don")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	for _, stmt := range []string{
		`CREATE TABLE clips (
			id INTEGER PRIMARY KEY,
			content_type TEXT NOT NULL,
			data BLOB NOT NULL,
			filename TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_archived INTEGER DEFAULT 0,
			content_hash TEXT DEFAULT ''
		)`,
		`CREATE TABLE tags (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL
		)`,
		`CREATE TABLE clip_tags (
			clip_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (clip_id, tag_id),
			FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			scoped_tag_id INTEGER,
			is_revoked INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to initialize schema: %v", err)
		}
	}

	return db
}

func TestHandleAddTagToClipRejectsScopedKeyForOutOfScopeClip(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scope', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (41, 'text/plain', 'a', 'a.txt')`); err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (41, 2)`); err != nil {
		t.Fatalf("failed to insert clip tag: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/clips/41/tags/1", nil)
	req.SetPathValue("id", "41")
	req.SetPathValue("tagId", "1")
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       7,
		Role:        "editor",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.handleAddTagToClip(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d with body %q", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE clip_id = 41 AND tag_id = 1`).Scan(&count); err != nil {
		t.Fatalf("failed to count clip tags: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected scoped tag not to be added, found %d rows", count)
	}
}

func TestAPIManagerStatusUsesReachableLoopbackURLWhenBoundToAllInterfaces(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewAPIManager(app)

	status, err := manager.Start(0, true)
	if err != nil {
		t.Fatalf("failed to start API manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Stop()
	})

	if !status.Running {
		t.Fatalf("expected running status")
	}
	if strings.Contains(status.URL, "0.0.0.0") {
		t.Fatalf("expected reachable URL, got %q", status.URL)
	}

	current := manager.GetStatus()
	if strings.Contains(current.URL, "0.0.0.0") {
		t.Fatalf("expected reachable URL from GetStatus, got %q", current.URL)
	}
}

func TestServeManagerStatusUsesReachableLoopbackURLWhenBoundToAllInterfaces(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewServeManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (5, 'served', '#333333')`); err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	info, err := manager.StartServing(5, 0, true)
	if err != nil {
		t.Fatalf("failed to start serve manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.StopServing(5)
	})

	if strings.Contains(info.URL, "0.0.0.0") {
		t.Fatalf("expected reachable URL, got %q", info.URL)
	}

	statuses := manager.GetStatus()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 running server, got %d", len(statuses))
	}
	if strings.Contains(statuses[0].URL, "0.0.0.0") {
		t.Fatalf("expected reachable URL from GetStatus, got %q", statuses[0].URL)
	}
}

func TestHandleCreateClipReturnsJSONOnCreated(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewAPIManager(app)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "clip.txt")
	if err != nil {
		t.Fatalf("failed to create multipart file part: %v", err)
	}
	if _, err := part.Write([]byte("hello from api")); err != nil {
		t.Fatalf("failed to write multipart body: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clips", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "editor",
	}))
	rec := httptest.NewRecorder()

	manager.handleCreateClip(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d with body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var clip apiClipResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &clip); err != nil {
		t.Fatalf("expected JSON response body, got error: %v", err)
	}
	if clip.Filename != "clip.txt" {
		t.Fatalf("expected clip filename to round-trip, got %q", clip.Filename)
	}
}

func TestHandleCreateTagReturnsJSONOnCreated(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewAPIManager(app)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(`{"name":"api-tag"}`))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec := httptest.NewRecorder()

	manager.handleCreateTag(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d with body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var tag Tag
	if err := json.Unmarshal(rec.Body.Bytes(), &tag); err != nil {
		t.Fatalf("expected JSON response body, got error: %v", err)
	}
	if tag.Name != "api-tag" {
		t.Fatalf("expected created tag name to round-trip, got %q", tag.Name)
	}
}

func TestServeManagerDirectoryListingEncodesReservedCharactersInLinks(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	manager := NewServeManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (9, 'served', '#444444')`); err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (91, 'application/javascript', 'console.log(1)', 'app#v2.js')`); err != nil {
		t.Fatalf("failed to insert clip: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (91, 9)`); err != nil {
		t.Fatalf("failed to insert clip tag: %v", err)
	}

	handler := manager.makeHandler(&tagServer{tagID: 9, tagName: "served"})

	listingReq := httptest.NewRequest(http.MethodGet, "/", nil)
	listingRec := httptest.NewRecorder()
	handler.ServeHTTP(listingRec, listingReq)

	if listingRec.Code != http.StatusOK {
		t.Fatalf("expected directory listing to succeed, got %d", listingRec.Code)
	}
	if !strings.Contains(listingRec.Body.String(), `href="/app%23v2.js"`) {
		t.Fatalf("expected reserved characters in href to be URL-encoded, got body %q", listingRec.Body.String())
	}

	fileReq := httptest.NewRequest(http.MethodGet, "/app%23v2.js", nil)
	fileRec := httptest.NewRecorder()
	handler.ServeHTTP(fileRec, fileReq)

	if fileRec.Code != http.StatusOK {
		t.Fatalf("expected encoded file path to resolve, got %d", fileRec.Code)
	}
	if body := fileRec.Body.String(); body != "console.log(1)" {
		t.Fatalf("expected served file body to round-trip, got %q", body)
	}
}

func TestHandleStartServerRequiresAdmin(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'test', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Tag-scoped editor key gets 403 from requireRole.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":1}`))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       1,
		Role:        "editor",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.requireRole("admin", manager.handleStartServer)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for scoped editor, got %d with body %q", rec.Code, rec.Body.String())
	}

	// Admin can start successfully.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":2}`))
	req2 = req2.WithContext(context.WithValue(req2.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 2,
		Role:  "admin",
	}))
	rec2 := httptest.NewRecorder()

	manager.handleStartServer(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d with body %q", rec2.Code, rec2.Body.String())
	}

	var info ServeInfo
	if err := json.Unmarshal(rec2.Body.Bytes(), &info); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if info.TagID != 2 {
		t.Fatalf("expected tag_id 2, got %d", info.TagID)
	}

	t.Cleanup(func() {
		_ = app.serveManager.StopServing(2)
	})
}

func TestHandleStartServerScopedAdminCanStartOwnTag(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Tag-scoped admin can start server for their scoped tag.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":1}`))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       1,
		Role:        "admin",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.handleStartServer(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for scoped admin starting own tag, got %d with body %q", rec.Code, rec.Body.String())
	}

	t.Cleanup(func() {
		_ = app.serveManager.StopServing(1)
	})
}

func TestHandleStartServerScopedAdminCannotStartOtherTag(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Tag-scoped admin cannot start server for a different tag.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":2}`))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       1,
		Role:        "admin",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.handleStartServer(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for scoped admin starting other tag, got %d with body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleStopServerScopedAdminCanStopOwnTag(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#111111')`); err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Start a server first.
	if _, err := app.serveManager.StartServing(1, 0, false); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Tag-scoped admin can stop server for their scoped tag.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/serve/1", nil)
	req.SetPathValue("tagId", "1")
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       1,
		Role:        "admin",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.handleStopServer(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for scoped admin stopping own tag, got %d with body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleStopServerScopedAdminCannotStopOtherTag(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#111111'), (2, 'other', '#222222')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Start a server for tag 2.
	if _, err := app.serveManager.StartServing(2, 0, false); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() {
		_ = app.serveManager.StopServing(2)
	})

	// Tag-scoped admin cannot stop server for a different tag.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/serve/2", nil)
	req.SetPathValue("tagId", "2")
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       1,
		Role:        "admin",
		ScopedTagID: 1,
	}))
	rec := httptest.NewRecorder()

	manager.handleStopServer(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for scoped admin stopping other tag, got %d with body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleStartServerAutoPort(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (2, 'auto', '#222222')`); err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// port 0 or omitted → auto-assign
	req := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":2,"port":0}`))
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec := httptest.NewRecorder()

	manager.handleStartServer(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d with body %q", rec.Code, rec.Body.String())
	}

	var info ServeInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("expected JSON response, got error: %v", err)
	}
	if info.Port == 0 {
		t.Fatalf("expected auto-assigned port, got 0")
	}

	t.Cleanup(func() {
		_ = app.serveManager.StopServing(2)
	})
}

func TestHandleStartServerAlreadyServing(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (3, 'dup', '#333333')`); err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Start first
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":3}`))
	req1 = req1.WithContext(context.WithValue(req1.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec1 := httptest.NewRecorder()
	manager.handleStartServer(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec1.Code)
	}
	t.Cleanup(func() {
		_ = app.serveManager.StopServing(3)
	})

	// Start again → 409
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/serve", strings.NewReader(`{"tag_id":3}`))
	req2 = req2.WithContext(context.WithValue(req2.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec2 := httptest.NewRecorder()
	manager.handleStartServer(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d with body %q", rec2.Code, rec2.Body.String())
	}
}

func TestHandleStopServerNotRunning(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/serve/99", nil)
	req.SetPathValue("tagId", "99")
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec := httptest.NewRecorder()

	manager.handleStopServer(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d with body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleListServersTagScoped(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	app.serveManager = NewServeManager(app)
	manager := NewAPIManager(app)

	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (10, 'a', '#aaaaaa'), (11, 'b', '#bbbbbb')`); err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Start two servers
	if _, err := app.serveManager.StartServing(10, 0, false); err != nil {
		t.Fatalf("failed to start server for tag 10: %v", err)
	}
	if _, err := app.serveManager.StartServing(11, 0, false); err != nil {
		t.Fatalf("failed to start server for tag 11: %v", err)
	}
	t.Cleanup(func() {
		_ = app.serveManager.StopServing(10)
		_ = app.serveManager.StopServing(11)
	})

	// Unscoped admin sees both
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/serve", nil)
	req1 = req1.WithContext(context.WithValue(req1.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID: 1,
		Role:  "admin",
	}))
	rec1 := httptest.NewRecorder()
	manager.handleListServers(rec1, req1)

	var result1 struct {
		Servers []ServeInfo `json:"servers"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &result1); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result1.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(result1.Servers))
	}

	// Tag-scoped key (tag 10) sees only tag 10
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/serve", nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), apiKeyContextKey, &apiKeyContext{
		KeyID:       2,
		Role:        "viewer",
		ScopedTagID: 10,
	}))
	rec2 := httptest.NewRecorder()
	manager.handleListServers(rec2, req2)

	var result2 struct {
		Servers []ServeInfo `json:"servers"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &result2); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result2.Servers) != 1 {
		t.Fatalf("expected 1 server for scoped key, got %d", len(result2.Servers))
	}
	if result2.Servers[0].TagID != 10 {
		t.Fatalf("expected scoped server for tag 10, got tag %d", result2.Servers[0].TagID)
	}
}
