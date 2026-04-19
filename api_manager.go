package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"os"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-clipboard/plugin"
)

// APIStatus describes the current state of the API server.
type APIStatus struct {
	Running      bool   `json:"running"`
	Port         int    `json:"port"`
	BindAll      bool   `json:"bind_all"`
	URL          string `json:"url"`
	RequestCount int64  `json:"request_count"`
}

// APIKeyInfo is the public info about an API key (never includes the secret).
type APIKeyInfo struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	KeyPrefix     string  `json:"key_prefix"`
	Role          string  `json:"role"`
	ScopedTagID   *int64  `json:"scoped_tag_id"`
	ScopedTagName string  `json:"scoped_tag_name"`
	IsRevoked     bool    `json:"is_revoked"`
	CreatedAt     string  `json:"created_at"`
	LastUsedAt    *string `json:"last_used_at"`
}

// APIKeyCreateResult is returned when a new key is created, with the plaintext key shown once.
type APIKeyCreateResult struct {
	Key  string     `json:"key"`
	Info APIKeyInfo `json:"info"`
}

// apiKeyContext is attached to each authenticated request via context.
type apiKeyContext struct {
	KeyID       int64
	Role        string
	ScopedTagID int64 // 0 means no scope
}

type contextKey string

const apiKeyContextKey contextKey = "apiKey"

func displayServerURL(port int) string {
	// 0.0.0.0 is only a bind address; clients need a concrete endpoint.
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// APIManager manages the REST API HTTP server.
type APIManager struct {
	app          *App
	server       *http.Server
	mu           sync.RWMutex
	running      bool
	port         int
	bindAll      bool
	requestCount int64
}

// NewAPIManager creates a new APIManager.
func NewAPIManager(app *App) *APIManager {
	return &APIManager{app: app}
}

// Start starts the API server on the given port.
func (am *APIManager) Start(port int, bindAll bool) (APIStatus, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.running {
		return APIStatus{}, fmt.Errorf("API server is already running")
	}

	host := "127.0.0.1"
	if bindAll {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clips", am.authMiddleware(am.requireRole("viewer", am.handleListClips)))
	mux.HandleFunc("GET /api/v1/clips/{id}", am.authMiddleware(am.requireRole("viewer", am.handleGetClip)))
	mux.HandleFunc("GET /api/v1/clips/{id}/data", am.authMiddleware(am.requireRole("viewer", am.handleGetClipData)))
	mux.HandleFunc("POST /api/v1/clips", am.authMiddleware(am.requireRole("editor", am.handleCreateClip)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}", am.authMiddleware(am.requireRole("editor", am.handleDeleteClip)))
	mux.HandleFunc("PUT /api/v1/clips/{id}/archive", am.authMiddleware(am.requireRole("editor", am.handleArchiveClip)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}/archive", am.authMiddleware(am.requireRole("editor", am.handleUnarchiveClip)))
	mux.HandleFunc("GET /api/v1/tags", am.authMiddleware(am.requireRole("viewer", am.handleListTags)))
	mux.HandleFunc("POST /api/v1/tags", am.authMiddleware(am.requireRole("admin", am.handleCreateTag)))
	mux.HandleFunc("PUT /api/v1/tags/{id}", am.authMiddleware(am.requireRole("admin", am.handleUpdateTag)))
	mux.HandleFunc("DELETE /api/v1/tags/{id}", am.authMiddleware(am.requireRole("admin", am.handleDeleteTag)))
	mux.HandleFunc("POST /api/v1/tags/{id}/merge", am.authMiddleware(am.requireRole("admin", am.handleMergeTag)))
	mux.HandleFunc("PUT /api/v1/clips/{id}/tags/{tagId}", am.authMiddleware(am.requireRole("editor", am.handleAddTagToClip)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}/tags/{tagId}", am.authMiddleware(am.requireRole("editor", am.handleRemoveTagFromClip)))
	mux.HandleFunc("GET /api/v1/serve", am.authMiddleware(am.requireRole("viewer", am.handleListServers)))
	mux.HandleFunc("POST /api/v1/serve", am.authMiddleware(am.requireRole("admin", am.handleStartServer)))
	mux.HandleFunc("DELETE /api/v1/serve/{tagId}", am.authMiddleware(am.requireRole("admin", am.handleStopServer)))

	// Clip extended operations
	mux.HandleFunc("PATCH /api/v1/clips/{id}", am.authMiddleware(am.requireRole("editor", am.handleRenameClip)))
	mux.HandleFunc("PUT /api/v1/clips/{id}/expiration", am.authMiddleware(am.requireRole("editor", am.handleSetExpiration)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}/expiration", am.authMiddleware(am.requireRole("editor", am.handleCancelExpiration)))
	mux.HandleFunc("POST /api/v1/clips/bulk/delete", am.authMiddleware(am.requireRole("editor", am.handleBulkDelete)))
	mux.HandleFunc("POST /api/v1/clips/bulk/archive", am.authMiddleware(am.requireRole("editor", am.handleBulkArchive)))
	mux.HandleFunc("POST /api/v1/clips/bulk/unarchive", am.authMiddleware(am.requireRole("editor", am.handleBulkUnarchive)))
	mux.HandleFunc("POST /api/v1/clips/bulk/expire", am.authMiddleware(am.requireRole("editor", am.handleBulkSetExpiration)))
	mux.HandleFunc("POST /api/v1/clips/bulk/cancel-expire", am.authMiddleware(am.requireRole("editor", am.handleBulkCancelExpiration)))
	mux.HandleFunc("POST /api/v1/clips/bulk/tag", am.authMiddleware(am.requireRole("editor", am.handleBulkAddTag)))
	mux.HandleFunc("POST /api/v1/clips/bulk/untag", am.authMiddleware(am.requireRole("editor", am.handleBulkRemoveTag)))
	mux.HandleFunc("POST /api/v1/clips/bulk/download", am.authMiddleware(am.requireRole("viewer", am.handleBulkDownload)))
	mux.HandleFunc("POST /api/v1/clips/bulk/copy", am.authMiddleware(am.requireRole("editor", am.handleBulkCopyToClipboard)))

	// Clip metadata
	mux.HandleFunc("GET /api/v1/clips/{id}/metadata", am.authMiddleware(am.requireRole("viewer", am.handleGetMetadata)))
	mux.HandleFunc("PUT /api/v1/clips/{id}/metadata", am.authMiddleware(am.requireRole("editor", am.handleSetMetadataBulk)))
	mux.HandleFunc("PUT /api/v1/clips/{id}/metadata/{key}", am.authMiddleware(am.requireRole("editor", am.handleSetMetadata)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}/metadata/{key}", am.authMiddleware(am.requireRole("editor", am.handleDeleteMetadata)))

	// Tags extended
	mux.HandleFunc("GET /api/v1/tags/{id}/children", am.authMiddleware(am.requireRole("viewer", am.handleGetChildTags)))
	mux.HandleFunc("GET /api/v1/tags/{id}/clips", am.authMiddleware(am.requireRole("viewer", am.handleGetTagClips)))
	mux.HandleFunc("GET /api/v1/tags/hidden", am.authMiddleware(am.requireRole("viewer", am.handleGetHiddenTags)))
	mux.HandleFunc("PUT /api/v1/tags/hidden", am.authMiddleware(am.requireRole("admin", am.handleSetHiddenTags)))

	// Deduplication
	mux.HandleFunc("GET /api/v1/dedup", am.authMiddleware(am.requireRole("viewer", am.handleListDuplicates)))
	mux.HandleFunc("POST /api/v1/dedup/{clipId}/merge", am.authMiddleware(am.requireRole("editor", am.handleMergeDuplicates)))
	mux.HandleFunc("POST /api/v1/dedup/all", am.authMiddleware(am.requireRole("admin", am.handleDeduplicateAll)))

	// Watch folders
	mux.HandleFunc("GET /api/v1/watch", am.authMiddleware(am.requireRole("viewer", am.handleListWatchFolders)))
	mux.HandleFunc("GET /api/v1/watch/status", am.authMiddleware(am.requireRole("viewer", am.handleWatchStatus)))
	mux.HandleFunc("PUT /api/v1/watch/global-pause", am.authMiddleware(am.requireRole("admin", am.handleGlobalPause)))
	mux.HandleFunc("DELETE /api/v1/watch/global-pause", am.authMiddleware(am.requireRole("admin", am.handleGlobalResume)))
	mux.HandleFunc("GET /api/v1/watch/{id}", am.authMiddleware(am.requireRole("viewer", am.handleGetWatchFolder)))
	mux.HandleFunc("POST /api/v1/watch", am.authMiddleware(am.requireRole("admin", am.handleAddWatchFolder)))
	mux.HandleFunc("PUT /api/v1/watch/{id}", am.authMiddleware(am.requireRole("admin", am.handleUpdateWatchFolder)))
	mux.HandleFunc("DELETE /api/v1/watch/{id}", am.authMiddleware(am.requireRole("admin", am.handleRemoveWatchFolder)))
	mux.HandleFunc("PUT /api/v1/watch/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handlePauseWatchFolder)))
	mux.HandleFunc("DELETE /api/v1/watch/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handleResumeWatchFolder)))
	mux.HandleFunc("POST /api/v1/watch/{id}/process", am.authMiddleware(am.requireRole("admin", am.handleProcessExisting)))

	// Plugins
	mux.HandleFunc("GET /api/v1/plugins", am.authMiddleware(am.requireRole("viewer", am.handleListPlugins)))
	mux.HandleFunc("GET /api/v1/plugins/actions", am.authMiddleware(am.requireRole("viewer", am.handleListPluginActions)))
	mux.HandleFunc("POST /api/v1/plugins", am.authMiddleware(am.requireRole("admin", am.handleInstallPlugin)))
	mux.HandleFunc("POST /api/v1/plugins/check-updates", am.authMiddleware(am.requireRole("admin", am.handleCheckPluginUpdates)))
	mux.HandleFunc("DELETE /api/v1/plugins/{id}", am.authMiddleware(am.requireRole("admin", am.handleRemovePlugin)))
	mux.HandleFunc("PUT /api/v1/plugins/{id}/enable", am.authMiddleware(am.requireRole("admin", am.handleEnablePlugin)))
	mux.HandleFunc("PUT /api/v1/plugins/{id}/disable", am.authMiddleware(am.requireRole("admin", am.handleDisablePlugin)))
	mux.HandleFunc("GET /api/v1/plugins/{id}/storage", am.authMiddleware(am.requireRole("admin", am.handleGetPluginStorageAll)))
	mux.HandleFunc("GET /api/v1/plugins/{id}/storage/{key}", am.authMiddleware(am.requireRole("admin", am.handleGetPluginStorage)))
	mux.HandleFunc("PUT /api/v1/plugins/{id}/storage/{key}", am.authMiddleware(am.requireRole("admin", am.handleSetPluginStorage)))
	mux.HandleFunc("POST /api/v1/plugins/{id}/actions/{actionId}", am.authMiddleware(am.requireRole("editor", am.handleExecutePluginAction)))
	mux.HandleFunc("POST /api/v1/plugins/{id}/update", am.authMiddleware(am.requireRole("admin", am.handleUpdatePlugin)))

	// Backup
	mux.HandleFunc("GET /api/v1/backup", am.authMiddleware(am.requireRole("admin", am.handleCreateBackup)))
	mux.HandleFunc("POST /api/v1/backup/restore", am.authMiddleware(am.requireRole("admin", am.handleRestoreBackup)))

	// Maintenance
	mux.HandleFunc("POST /api/v1/maintenance/vacuum", am.authMiddleware(am.requireRole("admin", am.handleVacuum)))
	mux.HandleFunc("GET /api/v1/maintenance/stale-files", am.authMiddleware(am.requireRole("admin", am.handleListStaleFiles)))
	mux.HandleFunc("POST /api/v1/maintenance/stale-files/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanStaleFiles)))

	// Clipboard
	mux.HandleFunc("POST /api/v1/clipboard/copy", am.authMiddleware(am.requireRole("editor", am.handleCopyToClipboard)))
	mux.HandleFunc("POST /api/v1/clipboard/copy-file", am.authMiddleware(am.requireRole("editor", am.handleCopyFileToClipboard)))

	handler := am.corsMiddleware(mux)

	am.server = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return APIStatus{}, fmt.Errorf("port %d unavailable: %w", port, err)
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port
	am.port = actualPort
	am.bindAll = bindAll
	am.running = true
	atomic.StoreInt64(&am.requestCount, 0)

	go func() {
		log.Printf("api: starting API server on %s", ln.Addr())
		if err := am.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("api: server stopped with error: %v", err)
		}
	}()

	return APIStatus{
		Running:      true,
		Port:         actualPort,
		BindAll:      bindAll,
		URL:          displayServerURL(actualPort),
		RequestCount: 0,
	}, nil
}

// Stop gracefully shuts down the API server.
func (am *APIManager) Stop() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if !am.running {
		return fmt.Errorf("API server is not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := am.server.Shutdown(ctx); err != nil {
		log.Printf("api: forced close: %v", err)
		am.server.Close()
	}

	am.running = false
	log.Printf("api: server stopped")
	return nil
}

// GetStatus returns the current API server status.
func (am *APIManager) GetStatus() APIStatus {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if !am.running {
		return APIStatus{}
	}

	return APIStatus{
		Running:      true,
		Port:         am.port,
		BindAll:      am.bindAll,
		URL:          displayServerURL(am.port),
		RequestCount: atomic.LoadInt64(&am.requestCount),
	}
}

// CreateKey generates a new API key.
func (am *APIManager) CreateKey(name, role string, scopedTagID int64) (*APIKeyCreateResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("key name cannot be empty")
	}

	validRoles := map[string]bool{"viewer": true, "editor": true, "admin": true}
	if !validRoles[role] {
		return nil, fmt.Errorf("invalid role: %s (must be viewer, editor, or admin)", role)
	}

	// Validate scoped tag exists if provided
	if scopedTagID > 0 {
		var count int
		if err := am.app.db.QueryRow("SELECT COUNT(*) FROM tags WHERE id = ?", scopedTagID).Scan(&count); err != nil || count == 0 {
			return nil, fmt.Errorf("scoped tag %d not found", scopedTagID)
		}
	}

	// Generate 16 random bytes → hex → prefix with mp_
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	plaintext := "mp_" + hex.EncodeToString(randomBytes)
	prefix := plaintext[:8] + "..."

	hash := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hash[:])

	var scopedTagIDPtr *int64
	if scopedTagID > 0 {
		scopedTagIDPtr = &scopedTagID
	}

	result, err := am.app.db.Exec(
		"INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES (?, ?, ?, ?, ?)",
		name, keyHash, prefix, role, scopedTagIDPtr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create key: %w", err)
	}

	id, _ := result.LastInsertId()

	info := APIKeyInfo{
		ID:        id,
		Name:      name,
		KeyPrefix: prefix,
		Role:      role,
		IsRevoked: false,
	}
	if scopedTagID > 0 {
		info.ScopedTagID = &scopedTagID
		am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", scopedTagID).Scan(&info.ScopedTagName)
	}

	return &APIKeyCreateResult{
		Key:  plaintext,
		Info: info,
	}, nil
}

// ListKeys returns all API keys (active and revoked).
func (am *APIManager) ListKeys() ([]APIKeyInfo, error) {
	rows, err := am.app.db.Query(`
		SELECT ak.id, ak.name, ak.key_prefix, ak.role, ak.scoped_tag_id, t.name,
		       ak.is_revoked, ak.created_at, ak.last_used_at
		FROM api_keys ak
		LEFT JOIN tags t ON ak.scoped_tag_id = t.id
		ORDER BY ak.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKeyInfo
	for rows.Next() {
		var k APIKeyInfo
		var scopedTagID sql.NullInt64
		var scopedTagName sql.NullString
		var isRevoked int
		var lastUsedAt sql.NullString

		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Role, &scopedTagID, &scopedTagName,
			&isRevoked, &k.CreatedAt, &lastUsedAt); err != nil {
			return nil, fmt.Errorf("failed to scan key: %w", err)
		}

		k.IsRevoked = isRevoked == 1
		if scopedTagID.Valid {
			v := scopedTagID.Int64
			k.ScopedTagID = &v
		}
		if scopedTagName.Valid {
			k.ScopedTagName = scopedTagName.String
		}
		if lastUsedAt.Valid {
			k.LastUsedAt = &lastUsedAt.String
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []APIKeyInfo{}
	}
	return keys, rows.Err()
}

// RevokeKey soft-deletes an API key.
func (am *APIManager) RevokeKey(id int64) error {
	result, err := am.app.db.Exec("UPDATE api_keys SET is_revoked = 1 WHERE id = ? AND is_revoked = 0", id)
	if err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("key not found or already revoked")
	}
	return nil
}

// --- Middleware ---

// corsMiddleware adds CORS headers to all responses.
func (am *APIManager) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware validates the Bearer token and attaches key context.
func (am *APIManager) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&am.requestCount, 1)

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			am.jsonError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		hash := sha256.Sum256([]byte(token))
		keyHash := hex.EncodeToString(hash[:])

		var keyCtx apiKeyContext
		var scopedTagID sql.NullInt64

		err := am.app.db.QueryRow(
			"SELECT id, role, scoped_tag_id FROM api_keys WHERE key_hash = ? AND is_revoked = 0",
			keyHash,
		).Scan(&keyCtx.KeyID, &keyCtx.Role, &scopedTagID)

		if err != nil {
			am.jsonError(w, http.StatusUnauthorized, "invalid or revoked API key")
			return
		}

		if scopedTagID.Valid {
			keyCtx.ScopedTagID = scopedTagID.Int64
		}

		// Update last_used_at (fire and forget)
		go am.app.db.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", keyCtx.KeyID)

		ctx := context.WithValue(r.Context(), apiKeyContextKey, &keyCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// requireRole wraps a handler with a minimum role check.
func (am *APIManager) requireRole(minRole string, next http.HandlerFunc) http.HandlerFunc {
	roleLevel := map[string]int{"viewer": 0, "editor": 1, "admin": 2}
	return func(w http.ResponseWriter, r *http.Request) {
		keyCtx := r.Context().Value(apiKeyContextKey).(*apiKeyContext)
		if roleLevel[keyCtx.Role] < roleLevel[minRole] {
			am.jsonError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	}
}

// getKeyContext extracts the API key context from the request.
func getKeyContext(r *http.Request) *apiKeyContext {
	return r.Context().Value(apiKeyContextKey).(*apiKeyContext)
}

// --- Helpers ---

func (am *APIManager) jsonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (am *APIManager) jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func parseIntParam(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// enforceTagScope returns an error if the key is tag-scoped and the clip doesn't have that tag
// or any descendant tag.
func (am *APIManager) enforceTagScope(keyCtx *apiKeyContext, clipID int64) error {
	if keyCtx.ScopedTagID == 0 {
		return nil
	}
	// Get the scoped tag name
	var scopedName string
	if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName); err != nil {
		return fmt.Errorf("forbidden: scoped tag not found")
	}
	// Check if clip has the scoped tag or any descendant
	var count int
	am.app.db.QueryRow(`
		SELECT COUNT(*) FROM clip_tags ct
		JOIN tags t ON ct.tag_id = t.id
		WHERE ct.clip_id = ? AND (t.id = ? OR t.name LIKE ?)`,
		clipID, keyCtx.ScopedTagID, scopedName+"/%").Scan(&count)
	if count == 0 {
		return fmt.Errorf("forbidden: clip not in scoped tag")
	}
	return nil
}

// isTagInScope returns true if the given tag name is the scoped tag itself or a descendant of it.
func (am *APIManager) isTagInScope(tagName string, scopedTagID int64) bool {
	var scopedName string
	if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", scopedTagID).Scan(&scopedName); err != nil {
		return false
	}
	return tagName == scopedName || isDescendantOf(tagName, scopedName)
}

// --- Clip Handlers ---

type apiClipResponse struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	IsArchived  bool   `json:"is_archived"`
	CreatedAt   string `json:"created_at"`
	Tags        []Tag  `json:"tags"`
}

type apiClipListResponse struct {
	Clips  []apiClipResponse `json:"clips"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

func (am *APIManager) handleListClips(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	// Parse query params
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Build query
	var conditions []string
	var args []interface{}

	// Tag filter
	tagFilter := int64(0)
	if v := r.URL.Query().Get("tag"); v != "" {
		if n, err := parseIntParam(v); err == nil {
			tagFilter = n
		}
	}

	// Tag-scoped key: expand filter to include subtree
	if keyCtx.ScopedTagID > 0 {
		if tagFilter > 0 {
			// Verify requested tag is within scope
			var tagName string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagFilter).Scan(&tagName); err != nil || !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
				am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: limit, Offset: offset})
				return
			}
			// tagFilter stays as-is, the existing query will handle it
		} else {
			tagFilter = keyCtx.ScopedTagID
		}
	}

	// Content type filter
	if v := r.URL.Query().Get("content_type"); v != "" {
		conditions = append(conditions, "c.content_type = ?")
		args = append(args, v)
	}

	// Archived filter
	if v := r.URL.Query().Get("archived"); v != "" {
		if v == "true" {
			conditions = append(conditions, "c.is_archived = 1")
		} else {
			conditions = append(conditions, "c.is_archived = 0")
		}
	}

	// Search
	if v := r.URL.Query().Get("search"); v != "" {
		conditions = append(conditions, "(c.filename LIKE ? OR CAST(c.data AS TEXT) LIKE ?)")
		pattern := "%" + v + "%"
		args = append(args, pattern, pattern)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " AND " + strings.Join(conditions, " AND ")
	}

	var totalCount int
	var rows *sql.Rows
	var err error

	if tagFilter > 0 {
		// For scoped keys using their scoped tag as the filter, expand to include subtree
		expandSubtree := keyCtx.ScopedTagID > 0 && tagFilter == keyCtx.ScopedTagID
		if expandSubtree {
			var scopedName string
			am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName)

			// Count
			countQuery := fmt.Sprintf(`SELECT COUNT(DISTINCT c.id) FROM clips c
				JOIN clip_tags ct ON c.id = ct.clip_id
				JOIN tags t ON ct.tag_id = t.id
				WHERE (t.id = ? OR t.name LIKE ?)%s`, whereClause)
			countArgs := append([]interface{}{tagFilter, scopedName + "/%"}, args...)
			am.app.db.QueryRow(countQuery, countArgs...).Scan(&totalCount)

			// Fetch
			query := fmt.Sprintf(`SELECT DISTINCT c.id, c.content_type, c.filename, LENGTH(c.data), c.is_archived, c.created_at
				FROM clips c
				JOIN clip_tags ct ON c.id = ct.clip_id
				JOIN tags t ON ct.tag_id = t.id
				WHERE (t.id = ? OR t.name LIKE ?)%s
				ORDER BY c.created_at DESC
				LIMIT ? OFFSET ?`, whereClause)
			fetchArgs := append([]interface{}{tagFilter, scopedName + "/%"}, args...)
			fetchArgs = append(fetchArgs, limit, offset)
			rows, err = am.app.db.Query(query, fetchArgs...)
		} else {
			// Count
			countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM clips c
				JOIN clip_tags ct ON c.id = ct.clip_id
				WHERE ct.tag_id = ?%s`, whereClause)
			countArgs := append([]interface{}{tagFilter}, args...)
			am.app.db.QueryRow(countQuery, countArgs...).Scan(&totalCount)

			// Fetch
			query := fmt.Sprintf(`SELECT c.id, c.content_type, c.filename, LENGTH(c.data), c.is_archived, c.created_at
				FROM clips c
				JOIN clip_tags ct ON c.id = ct.clip_id
				WHERE ct.tag_id = ?%s
				ORDER BY c.created_at DESC
				LIMIT ? OFFSET ?`, whereClause)
			fetchArgs := append([]interface{}{tagFilter}, args...)
			fetchArgs = append(fetchArgs, limit, offset)
			rows, err = am.app.db.Query(query, fetchArgs...)
		}
	} else {
		// Count
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM clips c WHERE 1=1%s", whereClause)
		am.app.db.QueryRow(countQuery, args...).Scan(&totalCount)

		// Fetch
		query := fmt.Sprintf(`SELECT c.id, c.content_type, c.filename, LENGTH(c.data), c.is_archived, c.created_at
			FROM clips c WHERE 1=1%s
			ORDER BY c.created_at DESC
			LIMIT ? OFFSET ?`, whereClause)
		fetchArgs := append(args, limit, offset)
		rows, err = am.app.db.Query(query, fetchArgs...)
	}

	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to query clips")
		return
	}
	defer rows.Close()

	clips := []apiClipResponse{}
	for rows.Next() {
		var c apiClipResponse
		var filename sql.NullString
		var isArchived int

		if err := rows.Scan(&c.ID, &c.ContentType, &filename, &c.Size, &isArchived, &c.CreatedAt); err != nil {
			continue
		}
		c.Filename = filename.String
		c.IsArchived = isArchived == 1

		c.Tags, _ = am.app.GetClipTags(c.ID)
		if c.Tags == nil {
			c.Tags = []Tag{}
		}
		clips = append(clips, c)
	}

	am.jsonOK(w, apiClipListResponse{
		Clips:  clips,
		Total:  totalCount,
		Limit:  limit,
		Offset: offset,
	})
}

func (am *APIManager) handleGetClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	var c apiClipResponse
	var filename sql.NullString
	var isArchived int

	err = am.app.db.QueryRow(
		"SELECT id, content_type, filename, LENGTH(data), is_archived, created_at FROM clips WHERE id = ?", id,
	).Scan(&c.ID, &c.ContentType, &filename, &c.Size, &isArchived, &c.CreatedAt)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, "clip not found")
		return
	}

	c.Filename = filename.String
	c.IsArchived = isArchived == 1
	c.Tags, _ = am.app.GetClipTags(id)
	if c.Tags == nil {
		c.Tags = []Tag{}
	}

	am.jsonOK(w, c)
}

func (am *APIManager) handleGetClipData(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	var data []byte
	var contentType string
	var filename sql.NullString

	err = am.app.db.QueryRow("SELECT data, content_type, filename FROM clips WHERE id = ?", id).
		Scan(&data, &contentType, &filename)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, "clip not found")
		return
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if filename.String != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename.String))
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

func (am *APIManager) handleCreateClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	// Tag-scoped keys cannot create clips for other tags (enforced after creation via auto-tag)

	ct := r.Header.Get("Content-Type")
	if ct == "" {
		am.jsonError(w, http.StatusBadRequest, "missing Content-Type header")
		return
	}

	mediaType, params, _ := mime.ParseMediaType(ct)
	if !strings.HasPrefix(mediaType, "multipart/") {
		am.jsonError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read multipart data")
		return
	}

	data, err := io.ReadAll(io.LimitReader(part, 100*1024*1024)) // 100MB limit
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read file data")
		return
	}

	filename := part.FileName()
	if override := r.URL.Query().Get("filename"); override != "" {
		filename = override
	}

	// Detect content type
	partContentType := part.Header.Get("Content-Type")
	if partContentType == "" || partContentType == "application/octet-stream" {
		partContentType = mime.TypeByExtension(filepath.Ext(filename))
		if partContentType == "" {
			partContentType = http.DetectContentType(data)
		}
	}

	// Compute content hash for dedup
	contentHash := computeContentHash(data)

	// Check for duplicate
	var existingID int64
	err = am.app.db.QueryRow("SELECT id FROM clips WHERE content_hash = ?", contentHash).Scan(&existingID)
	if err == nil {
		// Duplicate exists — if tag-scoped, ensure tag is applied to existing clip
		if keyCtx.ScopedTagID > 0 {
			am.app.AddTagToClip(existingID, keyCtx.ScopedTagID)
		}
		// Return existing clip
		am.handleGetClipByID(w, existingID)
		return
	}

	// Insert new clip
	result, err := am.app.db.Exec(
		"INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		partContentType, data, filename, contentHash,
	)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create clip")
		return
	}

	clipID, _ := result.LastInsertId()

	// Auto-apply scoped tag
	if keyCtx.ScopedTagID > 0 {
		am.app.AddTagToClip(clipID, keyCtx.ScopedTagID)
	}

	// Emit plugin event
	if am.app.pluginManager != nil {
		preview, err := am.app.getClipPreview(clipID)
		if err == nil {
			am.app.pluginManager.EmitEvent("clip:created", map[string]interface{}{
				"id":           preview.ID,
				"content_type": preview.ContentType,
				"filename":     preview.Filename,
				"size":         preview.Size,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	am.handleGetClipByID(w, clipID)
}

// handleGetClipByID is a helper to write a clip JSON response by ID.
func (am *APIManager) handleGetClipByID(w http.ResponseWriter, id int64) {
	var c apiClipResponse
	var filename sql.NullString
	var isArchived int

	err := am.app.db.QueryRow(
		"SELECT id, content_type, filename, LENGTH(data), is_archived, created_at FROM clips WHERE id = ?", id,
	).Scan(&c.ID, &c.ContentType, &filename, &c.Size, &isArchived, &c.CreatedAt)
	if err != nil {
		return
	}

	c.Filename = filename.String
	c.IsArchived = isArchived == 1
	c.Tags, _ = am.app.GetClipTags(id)
	if c.Tags == nil {
		c.Tags = []Tag{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

func (am *APIManager) handleDeleteClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	if err := am.app.DeleteClip(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to delete clip")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleArchiveClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	_, err = am.app.db.Exec("UPDATE clips SET is_archived = 1 WHERE id = ?", id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to archive clip")
		return
	}

	if am.app.pluginManager != nil {
		am.app.pluginManager.EmitEvent("clip:archived", map[string]interface{}{"id": id})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleUnarchiveClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	_, err = am.app.db.Exec("UPDATE clips SET is_archived = 0 WHERE id = ?", id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to unarchive clip")
		return
	}

	if am.app.pluginManager != nil {
		am.app.pluginManager.EmitEvent("clip:unarchived", map[string]interface{}{"id": id})
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Tag Handlers ---

func (am *APIManager) handleListTags(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	if keyCtx.ScopedTagID > 0 {
		// Return the scoped tag and all its descendants
		var scopedName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", keyCtx.ScopedTagID).Scan(&scopedName); err != nil {
			am.jsonOK(w, []Tag{})
			return
		}

		rows, err := am.app.db.Query(`
			SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
			FROM tags t
			LEFT JOIN clip_tags ct ON t.id = ct.tag_id
			WHERE t.id = ? OR t.name LIKE ?
			GROUP BY t.id
			ORDER BY t.name`, keyCtx.ScopedTagID, scopedName+"/%")
		if err != nil {
			am.jsonOK(w, []Tag{})
			return
		}
		defer rows.Close()

		tags := []Tag{}
		for rows.Next() {
			var t Tag
			if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Count); err != nil {
				continue
			}
			tags = append(tags, t)
		}
		am.jsonOK(w, tags)
		return
	}

	tags, err := am.app.GetTags()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to list tags")
		return
	}
	am.jsonOK(w, tags)
}

func (am *APIManager) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if keyCtx.ScopedTagID > 0 {
		if !am.isTagInScope(body.Name, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag-scoped key can only create subtags under its scope")
			return
		}
	}

	tag, err := am.app.CreateTag(body.Name)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	am.jsonOK(w, tag)
}

func (am *APIManager) handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusNotFound, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage tags within its scope")
			return
		}
	}

	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if keyCtx.ScopedTagID > 0 && !am.isTagInScope(body.Name, keyCtx.ScopedTagID) {
		am.jsonError(w, http.StatusForbidden, "tag-scoped key can only rename tags within its scope")
		return
	}

	if err := am.app.UpdateTag(id, body.Name, body.Color); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusNotFound, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage tags within its scope")
			return
		}
	}

	if err := am.app.DeleteTag(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to delete tag")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleMergeTag(w http.ResponseWriter, r *http.Request) {
	sourceID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid source id")
		return
	}
	var body struct {
		DestID int64 `json:"dest_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := am.app.MergeTag(sourceID, body.DestID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, map[string]bool{"ok": true})
}

// --- Maintenance Handlers ---

func (am *APIManager) handleVacuum(w http.ResponseWriter, r *http.Request) {
	before, after, err := am.app.CompactDatabase()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, map[string]int64{"before": before, "after": after})
}

func (am *APIManager) handleListStaleFiles(w http.ResponseWriter, r *http.Request) {
	files, err := am.app.GetStaleFiles()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, files)
}

func (am *APIManager) handleCleanStaleFiles(w http.ResponseWriter, r *http.Request) {
	count, bytes, err := am.app.CleanStaleFiles()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, map[string]any{"count": count, "bytes": bytes})
}

// --- Clip-Tag Handlers ---

func (am *APIManager) handleAddTagToClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	clipID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	tagID, err := parseIntParam(r.PathValue("tagId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Tag-scoped keys can only add tags within scope
	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusBadRequest, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage tags within its scope")
			return
		}
		if err := am.enforceTagScope(keyCtx, clipID); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.AddTagToClip(clipID, tagID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleRemoveTagFromClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	clipID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	tagID, err := parseIntParam(r.PathValue("tagId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Tag-scoped keys can only remove tags within scope
	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusBadRequest, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage tags within its scope")
			return
		}
	}

	if err := am.app.RemoveTagFromClip(clipID, tagID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Serve Handlers ---

func (am *APIManager) handleListServers(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	if am.app.serveManager == nil {
		am.jsonOK(w, map[string][]ServeInfo{"servers": {}})
		return
	}

	servers := am.app.serveManager.GetStatus()

	// Tag-scoped keys only see servers for their scoped tag.
	if keyCtx.ScopedTagID > 0 {
		filtered := []ServeInfo{}
		for _, s := range servers {
			if s.TagID == keyCtx.ScopedTagID {
				filtered = append(filtered, s)
			}
		}
		servers = filtered
	}

	if servers == nil {
		servers = []ServeInfo{}
	}
	am.jsonOK(w, map[string][]ServeInfo{"servers": servers})
}

func (am *APIManager) handleStartServer(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		TagID     int64  `json:"tag_id"`
		Port      int    `json:"port"`
		BindAll   bool   `json:"bind_all"`
		ApiAccess string `json:"api_access"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.TagID == 0 {
		am.jsonError(w, http.StatusBadRequest, "tag_id is required")
		return
	}

	// Tag-scoped keys can only start servers for their scoped tag.
	if keyCtx.ScopedTagID > 0 && body.TagID != keyCtx.ScopedTagID {
		am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage serve for its scoped tag")
		return
	}

	if am.app.serveManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "serve manager not initialized")
		return
	}

	// Auto-assign port if 0.
	port := body.Port
	if port == 0 {
		p, err := GetRandomPort()
		if err != nil {
			am.jsonError(w, http.StatusInternalServerError, "failed to find available port")
			return
		}
		port = p
	}

	info, err := am.app.serveManager.StartServing(body.TagID, port, body.BindAll, body.ApiAccess)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "already being served") {
			am.jsonError(w, http.StatusConflict, msg)
			return
		}
		if strings.Contains(msg, "unavailable") {
			am.jsonError(w, http.StatusConflict, msg)
			return
		}
		am.jsonError(w, http.StatusBadRequest, msg)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(info)
}

func (am *APIManager) handleStopServer(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	tagID, err := parseIntParam(r.PathValue("tagId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Tag-scoped keys can only stop servers for their scoped tag.
	if keyCtx.ScopedTagID > 0 && tagID != keyCtx.ScopedTagID {
		am.jsonError(w, http.StatusForbidden, "tag-scoped key can only manage serve for its scoped tag")
		return
	}

	if am.app.serveManager == nil {
		am.jsonError(w, http.StatusNotFound, "no server running for this tag")
		return
	}

	if err := am.app.serveManager.StopServing(tagID); err != nil {
		if strings.Contains(err.Error(), "no server running") {
			am.jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Clip Extended Handlers ---

func (am *APIManager) handleRenameClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	var body struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Filename == "" {
		am.jsonError(w, http.StatusBadRequest, "filename is required")
		return
	}

	if err := am.app.RenameClip(id, body.Filename); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to rename clip")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleSetExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	var body struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Minutes <= 0 {
		am.jsonError(w, http.StatusBadRequest, "minutes must be positive")
		return
	}

	if err := am.app.SetExpiration(id, body.Minutes); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to set expiration")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleCancelExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	if err := am.app.CancelExpiration(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to cancel expiration")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Bulk Handlers ---

func (am *APIManager) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkDelete(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk delete")
		return
	}

	am.jsonOK(w, map[string]int{"deleted": len(body.IDs)})
}

func (am *APIManager) handleBulkArchive(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkArchive(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk archive")
		return
	}

	am.jsonOK(w, map[string]int{"archived": len(body.IDs)})
}

func (am *APIManager) handleBulkUnarchive(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkUnarchive(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk unarchive")
		return
	}

	am.jsonOK(w, map[string]int{"unarchived": len(body.IDs)})
}

func (am *APIManager) handleBulkSetExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs     []int64 `json:"ids"`
		Minutes int     `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	if body.Minutes <= 0 {
		am.jsonError(w, http.StatusBadRequest, "minutes must be positive")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkSetExpiration(body.IDs, body.Minutes); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk set expiration")
		return
	}

	am.jsonOK(w, map[string]int{"updated": len(body.IDs)})
}

func (am *APIManager) handleBulkCancelExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkCancelExpiration(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk cancel expiration")
		return
	}

	am.jsonOK(w, map[string]int{"updated": len(body.IDs)})
}

func (am *APIManager) handleBulkAddTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs   []int64 `json:"ids"`
		TagID int64   `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	if body.TagID == 0 {
		am.jsonError(w, http.StatusBadRequest, "tag_id is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkAddTag(body.IDs, body.TagID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk add tag")
		return
	}

	am.jsonOK(w, map[string]int{"tagged": len(body.IDs)})
}

func (am *APIManager) handleBulkRemoveTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs   []int64 `json:"ids"`
		TagID int64   `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	if body.TagID == 0 {
		am.jsonError(w, http.StatusBadRequest, "tag_id is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if err := am.app.BulkRemoveTag(body.IDs, body.TagID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to bulk remove tag")
		return
	}

	am.jsonOK(w, map[string]int{"untagged": len(body.IDs)})
}

func (am *APIManager) handleBulkDownload(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"clips.zip\"")

	zw := zip.NewWriter(w)

	for _, id := range body.IDs {
		var data []byte
		var filename sql.NullString
		err := am.app.db.QueryRow("SELECT data, filename FROM clips WHERE id = ?", id).Scan(&data, &filename)
		if err != nil {
			continue
		}

		name := filename.String
		if name == "" {
			name = fmt.Sprintf("clip_%d", id)
		} else {
			name = fmt.Sprintf("%d_%s", id, name)
		}

		fw, err := zw.Create(name)
		if err != nil {
			continue
		}
		fw.Write(data)
	}

	zw.Close()
}

func (am *APIManager) handleBulkCopyToClipboard(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range body.IDs {
		if err := am.enforceTagScope(keyCtx, id); err != nil {
			am.jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}

	if err := am.app.clipboardService.BulkCopyFilesToClipboard(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to copy to clipboard")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Metadata Handlers ---

func (am *APIManager) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	meta, err := am.app.GetClipMetadata(id)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, "clip not found")
		return
	}
	if meta == nil {
		meta = map[string]string{}
	}

	am.jsonOK(w, meta)
}

func (am *APIManager) handleSetMetadataBulk(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	var metadata map[string]string
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.app.SetClipMetadataBulk(id, metadata); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleSetMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	key := r.PathValue("key")
	if key == "" {
		am.jsonError(w, http.StatusBadRequest, "metadata key is required")
		return
	}

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.app.SetClipMetadata(id, key, body.Value); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDeleteMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	key := r.PathValue("key")
	if key == "" {
		am.jsonError(w, http.StatusBadRequest, "metadata key is required")
		return
	}

	if err := am.app.DeleteClipMetadata(id, key); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to delete metadata")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Tag Extended Handlers ---

func (am *APIManager) handleGetChildTags(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Tag-scoped keys can only view children within their scope
	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusNotFound, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonOK(w, []Tag{})
			return
		}
	}

	children, err := am.app.GetChildTags(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get child tags")
		return
	}
	if children == nil {
		children = []Tag{}
	}

	am.jsonOK(w, children)
}

func (am *APIManager) handleGetTagClips(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	tagID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Tag-scoped keys can only view clips for tags within their scope
	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName); err != nil {
			am.jsonError(w, http.StatusNotFound, "tag not found")
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: 50, Offset: 0})
			return
		}
	}

	// Parse pagination
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Count
	var totalCount int
	am.app.db.QueryRow(`
		SELECT COUNT(*) FROM clips c
		JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ?`, tagID).Scan(&totalCount)

	// Fetch
	rows, err := am.app.db.Query(`
		SELECT c.id, c.content_type, c.filename, LENGTH(c.data), c.is_archived, c.created_at
		FROM clips c
		JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ?
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?`, tagID, limit, offset)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to query clips")
		return
	}
	defer rows.Close()

	clips := []apiClipResponse{}
	for rows.Next() {
		var c apiClipResponse
		var filename sql.NullString
		var isArchived int

		if err := rows.Scan(&c.ID, &c.ContentType, &filename, &c.Size, &isArchived, &c.CreatedAt); err != nil {
			continue
		}
		c.Filename = filename.String
		c.IsArchived = isArchived == 1
		c.Tags, _ = am.app.GetClipTags(c.ID)
		if c.Tags == nil {
			c.Tags = []Tag{}
		}
		clips = append(clips, c)
	}

	am.jsonOK(w, apiClipListResponse{
		Clips:  clips,
		Total:  totalCount,
		Limit:  limit,
		Offset: offset,
	})
}

func (am *APIManager) handleGetHiddenTags(w http.ResponseWriter, r *http.Request) {
	ids, err := am.app.GetHiddenTags()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get hidden tags")
		return
	}
	if ids == nil {
		ids = []int64{}
	}

	am.jsonOK(w, map[string][]int64{"ids": ids})
}

func (am *APIManager) handleSetHiddenTags(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.app.SetHiddenTags(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to set hidden tags")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Dedup Handlers ---

func (am *APIManager) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "dedup operations are not available for tag-scoped keys")
		return
	}

	groups, err := am.app.GetDuplicateGroups()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get duplicate groups")
		return
	}
	if groups == nil {
		groups = []DuplicateGroup{}
	}

	am.jsonOK(w, map[string]interface{}{
		"groups": groups,
		"total":  len(groups),
	})
}

func (am *APIManager) handleMergeDuplicates(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	clipID, err := parseIntParam(r.PathValue("clipId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}

	if err := am.enforceTagScope(keyCtx, clipID); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	if err := am.app.MergeDuplicates(clipID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDeduplicateAll(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "dedup operations are not available for tag-scoped keys")
		return
	}

	removed, err := am.app.DeduplicateAll()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to deduplicate")
		return
	}

	am.jsonOK(w, map[string]int{"removed": removed})
}

// --- Watch Folder Handlers ---

func (am *APIManager) handleListWatchFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := am.app.GetWatchedFolders()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to list watch folders")
		return
	}
	if folders == nil {
		folders = []WatchedFolder{}
	}

	am.jsonOK(w, map[string]interface{}{
		"folders": folders,
		"total":   len(folders),
	})
}

func (am *APIManager) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	status := am.app.GetWatchStatus()
	am.jsonOK(w, status)
}

func (am *APIManager) handleGetWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	folder, err := am.app.GetWatchedFolderByID(id)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, "watch folder not found")
		return
	}

	am.jsonOK(w, folder)
}

func (am *APIManager) handleAddWatchFolder(w http.ResponseWriter, r *http.Request) {
	var config WatchedFolderConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if config.Path == "" {
		am.jsonError(w, http.StatusBadRequest, "path is required")
		return
	}

	folder, err := am.app.AddWatchedFolder(config)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	am.app.RefreshWatches()

	if config.ProcessExisting && folder != nil {
		go am.app.ProcessExistingFilesInFolder(folder.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(folder)
}

func (am *APIManager) handleUpdateWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	// Decode into raw map so we know which fields were actually provided
	var rawFields map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawFields); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.app.UpdateWatchedFolderPartial(id, rawFields); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleRemoveWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	if err := am.app.RemoveWatchedFolder(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to remove watch folder")
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handlePauseWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	if err := am.app.SetFolderPaused(id, true); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleResumeWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	if err := am.app.SetFolderPaused(id, false); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGlobalPause(w http.ResponseWriter, r *http.Request) {
	if err := am.app.SetGlobalWatchPaused(true); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to set global pause")
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGlobalResume(w http.ResponseWriter, r *http.Request) {
	if err := am.app.SetGlobalWatchPaused(false); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to resume global watch")
		return
	}

	am.app.RefreshWatches()
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleProcessExisting(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid watch folder id")
		return
	}

	if err := am.app.ProcessExistingFilesInFolder(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Plugin Handlers ---

func (am *APIManager) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if am.app.db == nil {
		am.jsonOK(w, map[string]interface{}{
			"plugins": []PluginInfo{},
			"total":   0,
		})
		return
	}

	rows, err := am.app.db.Query(`
		SELECT id, name, version, enabled, status
		FROM plugins ORDER BY name
	`)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to query plugins")
		return
	}
	defer rows.Close()

	var plugins []PluginInfo
	for rows.Next() {
		var p PluginInfo
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &enabled, &p.Status); err != nil {
			log.Printf("handleListPlugins: failed to scan row: %v", err)
			continue
		}
		p.Enabled = enabled == 1

		// Augment with manifest info from loaded plugin
		if am.app.pluginManager != nil {
			for _, loaded := range am.app.pluginManager.GetPlugins() {
				if loaded.ID == p.ID && loaded.Manifest != nil {
					p.Description = loaded.Manifest.Description
					p.Author = loaded.Manifest.Author
					p.Events = loaded.Manifest.Events
					p.Settings = loaded.Manifest.Settings
					break
				}
			}
		}

		plugins = append(plugins, p)
	}

	if plugins == nil {
		plugins = []PluginInfo{}
	}

	am.jsonOK(w, map[string]interface{}{
		"plugins": plugins,
		"total":   len(plugins),
	})
}

func (am *APIManager) handleListPluginActions(w http.ResponseWriter, r *http.Request) {
	response, err := am.app.getPluginUIActions()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get plugin actions")
		return
	}

	am.jsonOK(w, response)
}

func (am *APIManager) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Source == "" {
		am.jsonError(w, http.StatusBadRequest, "source is required")
		return
	}

	info, err := am.app.installPluginFromSource(body.Source)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(info)
}

func (am *APIManager) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}

	if err := am.app.pluginManager.RemovePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}

	if err := am.app.pluginManager.EnablePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}

	if err := am.app.pluginManager.DisablePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGetPluginStorageAll(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	storage, err := am.app.getAllPluginStorage(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get plugin storage")
		return
	}

	am.jsonOK(w, storage)
}

func (am *APIManager) handleGetPluginStorage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		am.jsonError(w, http.StatusBadRequest, "key is required")
		return
	}

	value, err := am.app.getPluginStorage(id, key)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to get plugin storage value")
		return
	}

	am.jsonOK(w, map[string]string{
		"key":   key,
		"value": value,
	})
}

func (am *APIManager) handleSetPluginStorage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	key := r.PathValue("key")
	if key == "" {
		am.jsonError(w, http.StatusBadRequest, "key is required")
		return
	}

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.app.setPluginStorage(id, key, body.Value); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to set plugin storage value")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleExecutePluginAction(w http.ResponseWriter, r *http.Request) {
	pluginID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	actionID := r.PathValue("actionId")
	if actionID == "" {
		am.jsonError(w, http.StatusBadRequest, "action id is required")
		return
	}

	var body struct {
		ClipIDs []int64                `json:"clip_ids"`
		Options map[string]interface{} `json:"options"`
	}
	// Body is optional for actions that don't need clip IDs or options
	json.NewDecoder(r.Body).Decode(&body)

	result, err := am.app.executePluginAction(pluginID, actionID, body.ClipIDs, body.Options)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	am.jsonOK(w, result)
}

func (am *APIManager) handleCheckPluginUpdates(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager == nil {
		am.jsonOK(w, map[string]interface{}{
			"updates": []*plugin.PluginUpdateInfo{},
		})
		return
	}

	uc := am.app.pluginManager.GetUpdateChecker()
	if uc == nil {
		am.jsonOK(w, map[string]interface{}{
			"updates": []*plugin.PluginUpdateInfo{},
		})
		return
	}

	uc.CheckAll()
	updates := uc.GetUpdates()
	if updates == nil {
		updates = []*plugin.PluginUpdateInfo{}
	}

	am.jsonOK(w, map[string]interface{}{
		"updates": updates,
	})
}

func (am *APIManager) handleUpdatePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	result, err := am.app.updatePlugin(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	am.jsonOK(w, result)
}

// --- Backup Handlers ---

func (am *APIManager) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	tmpFile, err := os.CreateTemp("", "mahpastes-backup-*.zip")
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := am.app.CreateBackup(tmpPath); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create backup: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="mahpastes-backup.zip"`)
	http.ServeFile(w, r, tmpPath)
}

func (am *APIManager) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		am.jsonError(w, http.StatusBadRequest, "missing Content-Type header")
		return
	}

	mediaType, params, _ := mime.ParseMediaType(ct)
	if !strings.HasPrefix(mediaType, "multipart/") {
		am.jsonError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read multipart data")
		return
	}

	tmpFile, err := os.CreateTemp("", "mahpastes-restore-*.zip")
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, part); err != nil {
		tmpFile.Close()
		am.jsonError(w, http.StatusBadRequest, "failed to read upload data")
		return
	}
	tmpFile.Close()

	// REST API (headless CLI) defaults to "keep": preserve local identity and
	// mark restored shares invalid rather than silently adopting a remote identity.
	if err := am.app.ConfirmRestoreBackup(tmpPath, "keep"); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to restore backup: "+err.Error())
		return
	}

	am.jsonOK(w, map[string]string{"status": "restored"})
}

// --- Clipboard Handlers ---

func (am *APIManager) handleCopyToClipboard(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		ClipID int64 `json:"clip_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.enforceTagScope(keyCtx, body.ClipID); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}

	if err := am.app.clipboardService.CopyClipContents(body.ClipID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to copy to clipboard: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleCopyFileToClipboard(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

	var body struct {
		ClipID int64 `json:"clip_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := am.enforceTagScope(keyCtx, body.ClipID); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}

	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}

	if err := am.app.clipboardService.CopyFileToClipboard(body.ClipID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to copy file to clipboard: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
