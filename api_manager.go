package main

import (
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
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	mux.HandleFunc("PUT /api/v1/clips/{id}/tags/{tagId}", am.authMiddleware(am.requireRole("editor", am.handleAddTagToClip)))
	mux.HandleFunc("DELETE /api/v1/clips/{id}/tags/{tagId}", am.authMiddleware(am.requireRole("editor", am.handleRemoveTagFromClip)))
	mux.HandleFunc("GET /api/v1/serve", am.authMiddleware(am.requireRole("viewer", am.handleListServers)))
	mux.HandleFunc("POST /api/v1/serve", am.authMiddleware(am.requireRole("admin", am.handleStartServer)))
	mux.HandleFunc("DELETE /api/v1/serve/{tagId}", am.authMiddleware(am.requireRole("admin", am.handleStopServer)))

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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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
