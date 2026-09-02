package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go-clipboard/internal/bridgeiface"
	"go-clipboard/internal/cliptype"
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
	// RevokedAt is nil for active keys. For revoked ones it is when the
	// retention clock started — the row is deleted revokedKeyRetentionDays later.
	RevokedAt *string `json:"revoked_at"`
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

const (
	// maxClipUploadBytes bounds a single uploaded clip's bytes.
	maxClipUploadBytes = 100 * 1024 * 1024 // 100 MB
	// multipartOverheadBytes is the slack added on top of the clip limit to
	// cover multipart boundaries/headers when bounding the whole request body.
	multipartOverheadBytes = 1 * 1024 * 1024 // 1 MB
)

func displayServerURL(port int) string {
	// 0.0.0.0 is only a bind address; clients need a concrete endpoint.
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// SSEHandler is the subset of the web-UI event broker the API layer needs: it
// serves the streaming endpoint and can be told to terminate every open stream
// during shutdown. Kept as an interface so package app does not import the
// web-UI asset package (which the desktop build never links).
type SSEHandler interface {
	http.Handler
	Shutdown()
}

// APIManager manages the REST API HTTP server.
type APIManager struct {
	app              *App
	mux              *http.ServeMux
	server           *http.Server
	sse              SSEHandler
	dataDir          string
	signingKey       []byte
	loginLimiter     *loginRateLimiter
	shareLimiter     *loginRateLimiter
	trustProxy       bool
	secureCookies    bool
	routesRegistered bool
	mu               sync.RWMutex
	running          bool
	port             int
	bindAll          bool
	requestCount     int64
}

// NewAPIManager creates a new APIManager.
func NewAPIManager(app *App, dataDir ...string) *APIManager {
	am := &APIManager{
		app:          app,
		mux:          http.NewServeMux(),
		loginLimiter: newLoginRateLimiter(),
		shareLimiter: newLoginRateLimiter(),
	}
	// Reverse-proxy posture (headless server). When the daemon sits behind a
	// trusted TLS-terminating proxy, operators opt in so we can (a) trust
	// X-Forwarded-Proto to mark session cookies Secure and (b) derive the real
	// client IP for the login rate-limiter instead of bucketing everyone under
	// the proxy's address. Default off so a directly-exposed instance never
	// trusts attacker-supplied forwarding headers.
	am.trustProxy = os.Getenv("MAHPASTESD_TRUST_PROXY") == "1"
	if v, err := strconv.ParseBool(os.Getenv("MAHPASTESD_SECURE_COOKIES")); err == nil {
		am.secureCookies = v
	}
	if len(dataDir) > 0 && dataDir[0] != "" {
		am.dataDir = dataDir[0]
		key, err := loadOrCreateSigningKey(dataDir[0])
		if err != nil {
			log.Printf("api: signing key: %v - web UI sessions disabled", err)
		} else {
			am.signingKey = key
		}
	}
	return am
}

func (am *APIManager) MountWebUI(assets fs.FS, sse SSEHandler) {
	am.sse = sse
	// The event stream is broadcast to every subscriber with no per-frame
	// filtering, so it must only be reachable by keys whose REST visibility is
	// already global. Tag-scoped keys (which the REST layer confines to a tag
	// subtree) are rejected here, otherwise they would receive a firehose of
	// clip previews, tag changes and share events for the entire instance.
	// requireUnscopedSSE also attaches per-connection auth metadata used by the
	// broker to cap connections and to keep re-validating the key/session.
	am.mux.Handle("GET /api/v1/events", am.authMiddleware(am.requireUnscopedSSE(sse)))
	am.mux.Handle("/", newSPAHandler(assets, am))
}

// requireUnscopedSSE rejects tag-scoped keys from the broadcast event stream and
// attaches an SSEAuth (per-key cap accounting + periodic re-validation) to the
// request context for the broker.
func (am *APIManager) requireUnscopedSSE(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyCtx := getKeyContext(r)
		if keyCtx.ScopedTagID != 0 {
			am.jsonError(w, http.StatusForbidden, "event stream is not available for tag-scoped keys")
			return
		}
		auth := &bridgeiface.SSEAuth{
			KeyID:      keyCtx.KeyID,
			Revalidate: func() bool { return am.stillAuthorized(r) },
		}
		next.ServeHTTP(w, r.WithContext(bridgeiface.WithSSEAuth(r.Context(), auth)))
	}
}

// stillAuthorized re-runs the same checks as authMiddleware (without mutating
// the request) so an open SSE stream can detect a revoked key or expired
// session and close itself.
func (am *APIManager) stillAuthorized(r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		_, err := am.validateAPIKey(strings.TrimPrefix(auth, "Bearer "))
		return err == nil
	}
	if c, err := r.Cookie("_mp_session"); err == nil && am.signingKey != nil {
		if id, err := verifySessionToken(am.signingKey, c.Value); err == nil {
			_, err := am.loadKeyContextByID(id)
			return err == nil
		}
	}
	return false
}

type spaHandler struct {
	assets   fs.FS
	api      *APIManager
	indexBuf []byte
	modTime  time.Time
}

func newSPAHandler(assets fs.FS, api *APIManager) *spaHandler {
	h := &spaHandler{assets: assets, api: api, modTime: time.Now()}
	if data, err := fs.ReadFile(assets, "index.html"); err == nil {
		data = bytes.Replace(data, []byte("<head>"), []byte("<head>\n    <script>window.mahpastesMode = 'server';</script>"), 1)
		h.indexBuf = data
	}
	return h
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	// The SPA shell — the root, an explicit /index.html, and any unknown
	// deep-link path — requires a valid session and is served via writeIndex
	// (which injects server mode). Every other embedded asset (login.html,
	// CSS, JS, fonts) is public so the login page can render before auth.
	// index.html must NOT fall through to the public-asset branch, or the
	// shell would be reachable unauthenticated.
	isShell := name == "" || name == "." || name == "index.html"
	if !isShell {
		if _, err := fs.Stat(h.assets, name); err == nil {
			h.serveFile(w, r, name)
			return
		}
	}
	if !h.api.hasValidSession(r) {
		http.Redirect(w, r, "/login.html", http.StatusFound)
		return
	}
	h.writeIndex(w, r)
}

// spaCSP backstops the app origin against injected markup. It is intentionally
// permissive about inline script/style (the SPA relies on both) but blocks the
// passive XSS sinks that the clip-data hardening does not cover: <object>/<embed>
// (object-src), <base> hijacking (base-uri), and framing/clickjacking
// (frame-ancestors). Self-hosted assets plus the Google Fonts the shell loads
// are allowlisted; clip images arrive as data:/blob: URLs.
const spaCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"img-src 'self' data: blob:; " +
	"connect-src 'self'; " +
	// The text-editor validator worker is a static same-origin script. This
	// directive is documentation, not a new allowance: worker-src already
	// inherited 'self' from default-src, and no blob: allowance is wanted —
	// the worker is never constructed from blob: or data:.
	"worker-src 'self'; " +
	"object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

func (h *spaHandler) writeIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", spaCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "index.html", h.modTime, bytes.NewReader(h.indexBuf))
}

func (h *spaHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(h.assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, h.modTime, bytes.NewReader(data))
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

	if !am.routesRegistered {
		mux := am.mux
		mux.HandleFunc("GET /api/v1/clips", am.authMiddleware(am.requireRole("viewer", am.handleListClips)))
		mux.HandleFunc("GET /api/v1/clips/hidden-info", am.authMiddleware(am.requireRole("viewer", am.handleHiddenClipInfo)))
		mux.HandleFunc("GET /api/v1/clips/{id}", am.authMiddleware(am.requireRole("viewer", am.handleGetClip)))
		mux.HandleFunc("GET /api/v1/clips/{id}/data", am.authMiddleware(am.requireRole("viewer", am.handleGetClipData)))
		mux.HandleFunc("GET /api/v1/clips/{id}/text", am.authMiddleware(am.requireRole("viewer", am.handleGetClipText)))
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
		mux.HandleFunc("POST /api/v1/tags/{id}/merge-preview", am.authMiddleware(am.requireRole("admin", am.handlePreviewMergeTag)))

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
		mux.HandleFunc("POST /api/v1/watch/refresh", am.authMiddleware(am.requireRole("admin", am.handleRefreshWatches)))

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
		mux.HandleFunc("POST /api/v1/plugins/{id}/search", am.authMiddleware(am.requireRole("editor", am.handlePluginSearch)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/update", am.authMiddleware(am.requireRole("admin", am.handleUpdatePlugin)))
		mux.HandleFunc("GET /api/v1/plugins/{id}/permissions", am.authMiddleware(am.requireRole("admin", am.handleGetPluginPermissions)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/permissions/revoke", am.authMiddleware(am.requireRole("admin", am.handleRevokePluginPermission)))
		mux.HandleFunc("POST /api/v1/plugins/{id}/url-check", am.authMiddleware(am.requireRole("editor", am.handlePluginURLCheck)))
		mux.HandleFunc("POST /api/v1/plugins/upload", am.authMiddleware(am.requireRole("admin", am.handleUploadPlugin)))
		mux.HandleFunc("POST /api/v1/plugins/preview", am.authMiddleware(am.requireRole("admin", am.handlePreviewPluginFromURL)))
		mux.HandleFunc("POST /api/v1/plugins/confirm", am.authMiddleware(am.requireRole("admin", am.handleConfirmPluginInstall)))
		// Server-mode replacement for the desktop "plugin:modal:acked/closed"
		// upstream events the browser cannot send over SSE.
		mux.HandleFunc("POST /api/v1/plugins/modal/ack", am.authMiddleware(am.requireRole("editor", am.handleModalAck)))
		mux.HandleFunc("POST /api/v1/plugins/modal/close", am.authMiddleware(am.requireRole("editor", am.handleModalClose)))

		// Backup
		mux.HandleFunc("GET /api/v1/backup", am.authMiddleware(am.requireRole("admin", am.handleCreateBackup)))
		mux.HandleFunc("POST /api/v1/backup/restore", am.authMiddleware(am.requireRole("admin", am.handleRestoreBackup)))

		// Maintenance
		mux.HandleFunc("POST /api/v1/maintenance/vacuum", am.authMiddleware(am.requireRole("admin", am.handleVacuum)))
		mux.HandleFunc("GET /api/v1/maintenance/stale-files", am.authMiddleware(am.requireRole("admin", am.handleListStaleFiles)))
		mux.HandleFunc("POST /api/v1/maintenance/stale-files/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanStaleFiles)))
		mux.HandleFunc("GET /api/v1/maintenance/orphan-rows", am.authMiddleware(am.requireRole("admin", am.handleListOrphanRows)))
		mux.HandleFunc("POST /api/v1/maintenance/orphan-rows/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanOrphanRows)))
		mux.HandleFunc("GET /api/v1/maintenance/empty-tags", am.authMiddleware(am.requireRole("admin", am.handleListEmptyTags)))
		mux.HandleFunc("POST /api/v1/maintenance/empty-tags", am.authMiddleware(am.requireRole("admin", am.handleRemoveEmptyTags)))
		mux.HandleFunc("GET /api/v1/maintenance/database", am.authMiddleware(am.requireRole("admin", am.handleDatabaseSize)))
		mux.HandleFunc("POST /api/v1/maintenance/database", am.authMiddleware(am.requireRole("admin", am.handleCompactDatabase)))

		// Clipboard
		mux.HandleFunc("POST /api/v1/clipboard/copy", am.authMiddleware(am.requireRole("editor", am.handleCopyToClipboard)))
		mux.HandleFunc("POST /api/v1/clipboard/copy-file", am.authMiddleware(am.requireRole("editor", am.handleCopyFileToClipboard)))

		// Misc App routes used by the browser UI
		mux.HandleFunc("GET /api/v1/settings/{key}", am.authMiddleware(am.requireRole("viewer", am.handleGetSetting)))
		mux.HandleFunc("PUT /api/v1/settings/{key}", am.authMiddleware(am.requireRole("admin", am.handleSetSetting)))
		mux.HandleFunc("GET /api/v1/serve/random-port", am.authMiddleware(am.requireRole("admin", am.handleRandomPort)))
		mux.HandleFunc("PUT /api/v1/clips/{id}/data", am.authMiddleware(am.requireRole("editor", am.handleUpdateClipData)))
		mux.HandleFunc("POST /api/v1/clips/{id}/temp", am.authMiddleware(am.requireRole("editor", am.handleCreateTempFile)))
		mux.HandleFunc("DELETE /api/v1/temp", am.authMiddleware(am.requireRole("editor", am.handleDeleteAllTempFiles)))
		mux.HandleFunc("POST /api/v1/clips/diff", am.authMiddleware(am.requireRole("viewer", am.handleImageDiff)))
		mux.HandleFunc("POST /api/v1/clips/find", am.authMiddleware(am.requireRole("viewer", am.handleFindClipsByFilenameAndTag)))

		// Share
		mux.HandleFunc("GET /api/v1/share", am.authMiddleware(am.requireRole("viewer", am.handleGetShareStatus)))
		mux.HandleFunc("POST /api/v1/share/publish", am.authMiddleware(am.requireRole("admin", am.handleStartShare)))
		mux.HandleFunc("DELETE /api/v1/share/publish/{tagId}", am.authMiddleware(am.requireRole("admin", am.handleStopShare)))
		mux.HandleFunc("PUT /api/v1/share/publish/{tagId}/pause", am.authMiddleware(am.requireRole("admin", am.handlePauseShare)))
		mux.HandleFunc("DELETE /api/v1/share/publish/{tagId}/pause", am.authMiddleware(am.requireRole("admin", am.handleResumeShare)))
		mux.HandleFunc("POST /api/v1/share/follow", am.authMiddleware(am.requireRole("admin", am.handleFollow)))
		mux.HandleFunc("POST /api/v1/share/test-follow", am.authMiddleware(am.requireRole("admin", am.handleTestFollowConnection)))
		mux.HandleFunc("POST /api/v1/share/follow-direct", am.authMiddleware(am.requireRole("admin", am.handleFollowWithoutDial)))
		mux.HandleFunc("DELETE /api/v1/share/follow/{id}", am.authMiddleware(am.requireRole("admin", am.handleUnfollow)))
		mux.HandleFunc("POST /api/v1/share/follow/{id}/reconnect", am.authMiddleware(am.requireRole("admin", am.handleReconnectFollow)))
		mux.HandleFunc("PUT /api/v1/share/follow/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handlePauseFollow)))
		mux.HandleFunc("DELETE /api/v1/share/follow/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handleResumeFollow)))
		mux.HandleFunc("PUT /api/v1/share/follow/{id}/tag", am.authMiddleware(am.requireRole("admin", am.handleUpdateFollowTag)))
		mux.HandleFunc("GET /api/v1/share/logs", am.authMiddleware(am.requireRole("viewer", am.handleGetShareLogs)))

		// API keys and web UI auth
		mux.HandleFunc("GET /api/v1/status", am.handleAPIStatus)
		mux.HandleFunc("POST /api/v1/keys", am.authMiddleware(am.requireRole("admin", am.handleCreateKey)))
		mux.HandleFunc("GET /api/v1/keys", am.authMiddleware(am.requireRole("admin", am.handleListKeys)))
		mux.HandleFunc("DELETE /api/v1/keys/{id}", am.authMiddleware(am.requireRole("admin", am.handleRevokeKey)))
		mux.HandleFunc("POST /api/v1/auth/login", am.rateLimitLogin(am.handleLogin))
		mux.HandleFunc("POST /api/v1/auth/logout", am.handleLogout)

		// Public, revocable per-clip share links. The view route is intentionally
		// unauthenticated — the unguessable token in the path is the capability —
		// and is more specific than the SPA "/" catch-all, so it wins route
		// matching. Management is admin-only. Named /links (not /share) to avoid
		// colliding with the peer-to-peer share feature above.
		mux.HandleFunc("GET /s/{token}", am.handleShareView)
		mux.HandleFunc("POST /api/v1/links", am.authMiddleware(am.requireRole("admin", am.handleCreateShareLink)))
		mux.HandleFunc("GET /api/v1/links", am.authMiddleware(am.requireRole("admin", am.handleListShareLinks)))
		mux.HandleFunc("DELETE /api/v1/links/{id}", am.authMiddleware(am.requireRole("admin", am.handleRevokeShareLink)))
		am.routesRegistered = true
	}

	handler := am.corsMiddleware(am.mux)

	// Bound how long unauthenticated clients can hold a connection while
	// trickling request headers/bodies. Without these, a network-facing
	// (0.0.0.0) daemon is trivially Slowloris-able: header parsing happens
	// before auth, so an anonymous client can pin goroutines/FDs indefinitely.
	// WriteTimeout is deliberately left at 0 — the SSE event stream is a
	// long-lived response and a write deadline would sever it.
	// ReadHeaderTimeout is the real Slowloris guard (headers are parsed before
	// auth). ReadTimeout bounds slow request bodies but must stay generous
	// enough for a legitimate ~100MB clip upload over a slow link; the SSE route
	// clears its own read deadline so this does not sever the event stream.
	am.server = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
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

	// Terminate open SSE streams first so their handler goroutines return
	// promptly; otherwise server.Shutdown blocks for its full timeout waiting on
	// long-lived event streams that only notice teardown when their request
	// context is finally cancelled.
	if am.sse != nil {
		am.sse.Shutdown()
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
		       ak.is_revoked, ak.created_at, ak.last_used_at, ak.revoked_at
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
		var revokedAt sql.NullString

		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.Role, &scopedTagID, &scopedTagName,
			&isRevoked, &k.CreatedAt, &lastUsedAt, &revokedAt); err != nil {
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
		if revokedAt.Valid {
			k.RevokedAt = &revokedAt.String
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []APIKeyInfo{}
	}
	return keys, rows.Err()
}

// RevokeKey soft-deletes an API key. The revoked_at stamp starts the retention
// clock: the cleanup sweep hard-deletes the row revokedKeyRetentionDays later.
// Access is denied immediately either way — auth lookups filter is_revoked = 0.
func (am *APIManager) RevokeKey(id int64) error {
	result, err := am.app.db.Exec(
		"UPDATE api_keys SET is_revoked = 1, revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND is_revoked = 0", id)
	if err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("key not found or already revoked")
	}
	return nil
}

func (am *APIManager) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	am.jsonOK(w, am.GetStatus())
}

func (am *APIManager) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		ScopedTagID int64  `json:"scoped_tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Role == "" {
		body.Role = "viewer"
	}
	result, err := am.CreateKey(body.Name, body.Role, body.ScopedTagID)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	am.jsonOK(w, result)
}

func (am *APIManager) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := am.ListKeys()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, keys)
}

func (am *APIManager) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	if err := am.RevokeKey(id); err != nil {
		am.jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// authMiddleware validates a Bearer token or web UI session cookie and attaches key context.
func (am *APIManager) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&am.requestCount, 1)

		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			keyCtx, err := am.validateAPIKey(strings.TrimPrefix(auth, "Bearer "))
			if err != nil {
				am.jsonError(w, http.StatusUnauthorized, "invalid or revoked API key")
				return
			}
			ctx := context.WithValue(r.Context(), apiKeyContextKey, keyCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if c, err := r.Cookie("_mp_session"); err == nil && am.signingKey != nil {
			if id, err := verifySessionToken(am.signingKey, c.Value); err == nil {
				if keyCtx, err := am.loadKeyContextByID(id); err == nil {
					ctx := context.WithValue(r.Context(), apiKeyContextKey, keyCtx)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}

		am.jsonError(w, http.StatusUnauthorized, "authentication required")
	}
}

func (am *APIManager) validateAPIKey(token string) (*apiKeyContext, error) {
	hash := sha256.Sum256([]byte(token))
	keyHash := hex.EncodeToString(hash[:])
	var keyCtx apiKeyContext
	var scopedTagID sql.NullInt64
	err := am.app.db.QueryRow(
		"SELECT id, role, scoped_tag_id FROM api_keys WHERE key_hash = ? AND is_revoked = 0",
		keyHash,
	).Scan(&keyCtx.KeyID, &keyCtx.Role, &scopedTagID)
	if err != nil {
		return nil, err
	}
	if scopedTagID.Valid {
		keyCtx.ScopedTagID = scopedTagID.Int64
	}
	// Complete bookkeeping before dispatching the authenticated handler. An
	// asynchronous write here races write transactions started by the handler
	// and can surface SQLITE_BUSY to otherwise valid API requests.
	if _, err := am.app.db.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", keyCtx.KeyID); err != nil {
		log.Printf("validateAPIKey: failed to update last_used_at: %v", err)
	}
	return &keyCtx, nil
}

func (am *APIManager) loadKeyContextByID(id int64) (*apiKeyContext, error) {
	var keyCtx apiKeyContext
	var scopedTagID sql.NullInt64
	err := am.app.db.QueryRow(
		"SELECT id, role, scoped_tag_id FROM api_keys WHERE id = ? AND is_revoked = 0",
		id,
	).Scan(&keyCtx.KeyID, &keyCtx.Role, &scopedTagID)
	if err != nil {
		return nil, err
	}
	if scopedTagID.Valid {
		keyCtx.ScopedTagID = scopedTagID.Int64
	}
	return &keyCtx, nil
}

func (am *APIManager) hasValidSession(r *http.Request) bool {
	if am.signingKey == nil {
		return false
	}
	c, err := r.Cookie("_mp_session")
	if err != nil {
		return false
	}
	_, err = verifySessionToken(am.signingKey, c.Value)
	return err == nil
}

func (am *APIManager) rateLimitLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !am.loginLimiter.allow(am.clientIP(r), 5, time.Minute) {
			am.jsonError(w, http.StatusTooManyRequests, "too many login attempts")
			return
		}
		next(w, r)
	}
}

// clientIP returns the address used to bucket the login rate-limiter. Behind a
// trusted proxy (MAHPASTESD_TRUST_PROXY=1) it is the right-most X-Forwarded-For
// hop — the address the trusted proxy observed — so every client gets its own
// bucket instead of all clients collapsing onto the proxy's TCP address (which
// would let 5 bad logins from anyone lock out everyone). Without proxy trust it
// is the raw TCP peer, and forwarding headers (which a direct client could
// forge) are ignored.
func (am *APIManager) clientIP(r *http.Request) string {
	if am.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				if ip := strings.TrimSpace(parts[i]); ip != "" {
					return ip
				}
			}
		}
	}
	return remoteIP(r.RemoteAddr)
}

// cookieSecure reports whether the session cookie should carry the Secure
// attribute. It is true under direct TLS, behind a trusted proxy that reports
// X-Forwarded-Proto: https, or when explicitly forced via
// MAHPASTESD_SECURE_COOKIES=1. It is intentionally left false for a plaintext
// localhost/LAN deployment so the cookie is still sent.
func (am *APIManager) cookieSecure(r *http.Request) bool {
	if am.secureCookies || r.TLS != nil {
		return true
	}
	if am.trustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

func (am *APIManager) handleLogin(w http.ResponseWriter, r *http.Request) {
	if am.signingKey == nil {
		am.jsonError(w, http.StatusServiceUnavailable, "web UI sessions unavailable: signing key not initialized")
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	keyCtx, err := am.validateAPIKey(body.Key)
	if err != nil {
		am.jsonError(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	token := signSessionToken(am.signingKey, keyCtx.KeyID, time.Now().Add(sessionTTL))
	http.SetCookie(w, &http.Cookie{
		Name:     "_mp_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   am.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	am.jsonOK(w, map[string]string{"status": "ok"})
}

func (am *APIManager) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "_mp_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   am.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	am.jsonOK(w, map[string]string{"status": "ok"})
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
	// Check if clip has the scoped tag or any descendant. Escape SQL LIKE
	// wildcards in the scoped name so a tag literally containing _ or % cannot
	// over-match a sibling subtree (mirrors removeSameTreeTags in app.go).
	escapedName := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(scopedName)
	var count int
	am.app.db.QueryRow(`
		SELECT COUNT(*) FROM clip_tags ct
		JOIN tags t ON ct.tag_id = t.id
		WHERE ct.clip_id = ? AND (t.id = ? OR t.name LIKE ? ESCAPE '\')`,
		clipID, keyCtx.ScopedTagID, escapedName+"/%").Scan(&count)
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

	q := r.URL.Query()
	if q.Has("sort") || q.Has("dir") || len(q["tag"]) > 1 || len(q["hidden"]) > 0 || q.Has("folder_tag") || q.Has("untagged") || q.Has("search_content") {
		am.handleListClipsViaApp(w, r, limit, offset, keyCtx)
		return
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

// handleHiddenClipInfo reports how many clips the given filters would match if
// no tags were hidden, so a client can tell the user what it is withholding.
func (am *APIManager) handleHiddenClipInfo(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	q := r.URL.Query()
	archived := q.Get("archived") == "true"

	parseIDs := func(values []string) ([]int64, error) {
		out := make([]int64, 0, len(values))
		for _, v := range values {
			if v == "" {
				continue
			}
			id, err := parseIntParam(v)
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, nil
	}
	tagIDs, err := parseIDs(q["tag"])
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	hiddenIDs, err := parseIDs(q["hidden"])
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid hidden tag id")
		return
	}

	// Mirror handleListClipsViaApp: a tag-scoped key may only ask about its own subtree.
	empty := HiddenClipInfo{Tags: []string{}}
	if keyCtx.ScopedTagID > 0 {
		if len(tagIDs) == 0 {
			tagIDs = []int64{keyCtx.ScopedTagID}
		}
		for _, id := range tagIDs {
			var name string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&name); err != nil || !am.isTagInScope(name, keyCtx.ScopedTagID) {
				am.jsonOK(w, empty)
				return
			}
		}
	}

	info, err := am.app.GetHiddenClipInfo(archived, tagIDs, hiddenIDs)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, info)
}

func (am *APIManager) handleListClipsViaApp(w http.ResponseWriter, r *http.Request, limit, offset int, keyCtx *apiKeyContext) {
	q := r.URL.Query()
	archived := q.Get("archived") == "true"
	sortField := q.Get("sort")
	if sortField == "filename" {
		sortField = "name" // documented alias
	}
	if sortField != "name" && sortField != "size" && sortField != "type" && sortField != "created_at" {
		sortField = "created_at"
	}
	sortDir := q.Get("dir")
	if sortDir != "asc" {
		sortDir = "desc"
	}
	parseIDs := func(values []string) ([]int64, error) {
		out := make([]int64, 0, len(values))
		for _, v := range values {
			if v == "" {
				continue
			}
			id, err := parseIntParam(v)
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, nil
	}
	tagIDs, err := parseIDs(q["tag"])
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	hiddenIDs, err := parseIDs(q["hidden"])
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid hidden tag id")
		return
	}
	if keyCtx.ScopedTagID > 0 {
		if len(tagIDs) == 0 {
			tagIDs = []int64{keyCtx.ScopedTagID}
		}
		for _, id := range tagIDs {
			var name string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&name); err != nil || !am.isTagInScope(name, keyCtx.ScopedTagID) {
				am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: limit, Offset: offset})
				return
			}
		}
	}

	var previews []ClipPreview
	usedDBSearch := false
	if q.Get("untagged") == "true" {
		if keyCtx.ScopedTagID > 0 {
			am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: limit, Offset: offset})
			return
		}
		previews, err = am.app.GetUntaggedClips(archived, hiddenIDs, sortField, sortDir)
	} else if v := q.Get("folder_tag"); v != "" {
		folderID, parseErr := parseIntParam(v)
		if parseErr != nil {
			am.jsonError(w, http.StatusBadRequest, "invalid folder_tag")
			return
		}
		// GetFolderClips does no key-scope filtering, so a tag-scoped key must be
		// confined to its own subtree here (mirroring handleGetTagClips); without
		// this it could enumerate clips for any tag tree via folder_tag.
		if keyCtx.ScopedTagID > 0 {
			var name string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", folderID).Scan(&name); err != nil || !am.isTagInScope(name, keyCtx.ScopedTagID) {
				am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: limit, Offset: offset})
				return
			}
		}
		previews, err = am.app.GetFolderClips(archived, folderID, sortField, sortDir)
	} else if q.Has("search_content") {
		// The presence of search_content — not its value — is what asks for a
		// database-side search. `search` on its own keeps the older preview-only
		// post-filter below, which the mp CLI relies on.
		usedDBSearch = true
		previews, err = am.app.SearchClips(archived, tagIDs, hiddenIDs, q.Get("search"), q.Get("search_content") == "true", sortField, sortDir)
	} else {
		previews, err = am.app.GetClips(archived, tagIDs, hiddenIDs, sortField, sortDir)
	}
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	contentType := q.Get("content_type")
	search := strings.ToLower(q.Get("search"))
	if usedDBSearch {
		// Already applied in SQL, over the whole clip — re-running it against the
		// 500-byte preview here would throw away the deep matches.
		search = ""
	}
	clips := make([]apiClipResponse, 0, len(previews))
	for _, p := range previews {
		if contentType != "" && p.ContentType != contentType {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(p.Filename), search) && !strings.Contains(strings.ToLower(p.Preview), search) {
			continue
		}
		clips = append(clips, apiClipResponse{
			ID:          p.ID,
			Filename:    p.Filename,
			ContentType: p.ContentType,
			Size:        p.Size,
			IsArchived:  p.IsArchived,
			CreatedAt:   p.CreatedAt.Format(time.RFC3339),
			Tags:        p.Tags,
		})
	}
	total := len(clips)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	am.jsonOK(w, apiClipListResponse{Clips: clips[offset:end], Total: total, Limit: limit, Offset: offset})
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

	if !am.writeClipBytes(w, r, id) {
		am.jsonError(w, http.StatusNotFound, "clip not found")
	}
}

// apiClipTextResponse is deliberately the exact shape TextCodec already consumes
// from the desktop GetClipData binding, so one frontend decoder serves both
// surfaces. `data` is always base64: the frontend decides text-ness after
// classification and must never infer it from the transport.
type apiClipTextResponse struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
	ValidUTF8   bool   `json:"valid_utf8"`
	// Always "base64" — present so the payload is self-describing and matches
	// ClipData field-for-field.
	DataEncoding string `json:"data_encoding"`
	Size         int    `json:"size"`
}

// maxInlineTextBytes is an out-of-memory guard, NOT the editable cap. It matches
// the 64 MiB inline ceiling server mode already applied in rest-glue.js, so this
// endpoint is never more restrictive than the path it replaces.
//
// The real editable cap — 16 MiB — deliberately lives in one place, TextCodec, so
// desktop and served mode enforce it identically. Putting it here as well would
// make the server stricter than the desktop binding and split one rule across two
// languages.
const maxInlineTextBytes = 64 * 1024 * 1024

// handleGetClipText returns filename, content type, bytes, and UTF-8 validity in
// ONE query.
//
// The two-request composition it replaces — metadata from GET /clips/{id} plus
// bytes from GET /clips/{id}/data — is not merely slower: a concurrent update can
// pair one clip's metadata with another revision's bytes, e.g. config.json
// metadata over PNG bytes. Desktop reads all three columns in a single row scan;
// this is how server mode gets the same guarantee rather than papering over it.
func (am *APIManager) handleGetClipText(w http.ResponseWriter, r *http.Request) {
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
	if err := am.app.db.QueryRow("SELECT data, content_type, filename FROM clips WHERE id = ?", id).
		Scan(&data, &contentType, &filename); err != nil {
		am.jsonError(w, http.StatusNotFound, "clip not found")
		return
	}

	if len(data) > maxInlineTextBytes {
		am.jsonError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("clip is too large to load inline (%d bytes, limit %d) — download it instead", len(data), maxInlineTextBytes))
		return
	}

	am.jsonOK(w, apiClipTextResponse{
		ID:           id,
		Filename:     filename.String,
		ContentType:  contentType,
		Data:         base64.StdEncoding.EncodeToString(data),
		ValidUTF8:    utf8.Valid(data),
		DataEncoding: "base64",
		Size:         len(data),
	})
}

// writeClipBytes streams a clip's raw bytes to w with the full set of
// stored-XSS defenses. It performs NO authorization — every caller must gate
// access first (Bearer/session for the REST route, an unguessable token for the
// public /s/{token} share route). It returns false without writing anything when
// the clip row is missing, so the caller can emit its own not-found response.
//
// In web-UI (server) mode the SPA and this endpoint share one origin, so a
// stored text/html (or SVG) clip rendered inline would execute script with the
// user's session — effectively account takeover. The same risk applies to a
// public /s/{token} link on the app origin. Defend in depth:
//   - nosniff: stop content-type sniffing into an executable type.
//   - Content-Disposition: attachment (always, even with an empty filename):
//     direct navigation downloads instead of rendering inline. The web UI
//     reads clip bytes via fetch()+Blob, which ignores this header, so image
//     previews/editor are unaffected.
//   - CSP sandbox: even if a browser were coerced into rendering this
//     response, it runs in an opaque origin with scripts disabled.
func (am *APIManager) writeClipBytes(w http.ResponseWriter, r *http.Request, clipID int64) bool {
	return serveStoredClip(w, r, am.app.db, clipID, "attachment", true)
}

// writeWholeClipBytes streams a clip with range support switched off. Public
// share links use it: a link's download counter is claimed before the body is
// written, so honouring Range would let "bytes=0-0", or a browser's ordinary
// resume probe, spend a max_downloads=1 link on a single byte. A share link is
// a whole-file download, so the simplest correct answer is not to offer ranges.
func (am *APIManager) writeWholeClipBytes(w http.ResponseWriter, r *http.Request, clipID int64) bool {
	return serveStoredClip(w, r, am.app.db, clipID, "attachment", false)
}

// sanitizeDownloadName returns a safe, separator-free filename for use in a
// Content-Disposition header or a ZIP entry. Attacker-controlled clip filenames
// can contain path separators, "..", or control characters; collapse them to
// the base name and fall back to a generated name when nothing safe remains.
func sanitizeDownloadName(name string, id int64) string {
	name = strings.TrimSpace(name)
	// Reject anything with a path separator by keeping only the final element.
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	// Strip control characters and quotes that could break the header / archive.
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("clip_%d", id)
	}
	return name
}

func attachmentDisposition(name string) string {
	return fmt.Sprintf("attachment; filename=%q", name)
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

	// Cap the whole request body, not just the file part, so a network-facing
	// daemon cannot be made to buffer unbounded data into RAM (the part is read
	// via io.ReadAll). The ceiling is the 100MB clip limit plus a small margin
	// for multipart framing.
	r.Body = http.MaxBytesReader(w, r.Body, maxClipUploadBytes+multipartOverheadBytes)

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read multipart data")
		return
	}

	data, err := io.ReadAll(io.LimitReader(part, maxClipUploadBytes))
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
	partContentType = cliptype.PromoteMarkdown(filename, partContentType)

	// Compute content hash for dedup
	contentHash := computeContentHash(data)

	// Check for duplicate
	var existingID int64
	err = am.app.db.QueryRow("SELECT id FROM clips WHERE content_hash = ?", contentHash).Scan(&existingID)
	if err == nil {
		// Duplicate exists - if tag-scoped, ensure tag is applied to existing clip
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

	if preview, err := am.app.getClipPreview(clipID); err == nil {
		am.app.emitWatchImport(*preview)

		// Emit plugin event
		if am.app.pluginManager != nil {
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
		msg := err.Error()
		// Map app-level error categories to HTTP status codes.
		if strings.Contains(msg, "tag not found") {
			am.jsonError(w, http.StatusNotFound, msg)
			return
		}
		if strings.HasPrefix(msg, "cannot delete tag:") {
			am.jsonError(w, http.StatusConflict, msg)
			return
		}
		am.jsonError(w, http.StatusInternalServerError, msg)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleMergeTag(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)

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

	// Enforce tag scope: both source and destination must be within the
	// scoped subtree. Mirror the single-tag pattern used by handleDeleteTag.
	if keyCtx.ScopedTagID > 0 {
		for _, id := range []int64{sourceID, body.DestID} {
			var tagName string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&tagName); err != nil {
				am.jsonError(w, http.StatusNotFound, "tag not found")
				return
			}
			if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
				am.jsonError(w, http.StatusForbidden, "tag-scoped key can only merge tags within its scope")
				return
			}
		}
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
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "maintenance operations are not available for tag-scoped keys")
		return
	}
	result, err := am.app.CompactDatabase()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, map[string]int64{"before": result.Before, "after": result.After})
}

func (am *APIManager) handleListStaleFiles(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "maintenance operations are not available for tag-scoped keys")
		return
	}
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
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "maintenance operations are not available for tag-scoped keys")
		return
	}
	result, err := am.app.CleanStaleFiles()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, map[string]any{"count": result.Count, "bytes": result.Bytes})
}

func (am *APIManager) handleListOrphanRows(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "maintenance operations are not available for tag-scoped keys")
		return
	}
	report, err := am.app.GetOrphanDBRows()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, report)
}

func (am *APIManager) handleCleanOrphanRows(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		am.jsonError(w, http.StatusForbidden, "maintenance operations are not available for tag-scoped keys")
		return
	}
	report, err := am.app.CleanOrphanDBRows()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	am.jsonOK(w, report)
}

func (am *APIManager) handleListEmptyTags(w http.ResponseWriter, r *http.Request) {
	tags, err := am.app.GetRemovableEmptyTags()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tags == nil {
		tags = []Tag{}
	}
	am.jsonOK(w, tags)
}

func (am *APIManager) handleRemoveEmptyTags(w http.ResponseWriter, r *http.Request) {
	count, err := am.app.RemoveEmptyTags()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"removed": count})
}

func (am *APIManager) handleDatabaseSize(w http.ResponseWriter, r *http.Request) {
	size, err := am.app.GetDatabaseSize()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int64{"size": size})
}

func (am *APIManager) handleCompactDatabase(w http.ResponseWriter, r *http.Request) {
	result, err := am.app.CompactDatabase()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, result)
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

		// Clip filenames are attacker-controlled at upload time; neutralize path
		// separators / ".." before embedding them so the archive cannot carry a
		// zip-slip entry that writes outside an extractor's target directory.
		name := fmt.Sprintf("%d_%s", id, sanitizeDownloadName(filename.String, id))

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
		am.jsonError(w, http.StatusNotImplemented, "clipboard not available on this server")
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
		Context map[string]interface{} `json:"context"`
	}
	// Body is optional for actions that don't need clip IDs or options
	json.NewDecoder(r.Body).Decode(&body)

	result, err := am.app.executePluginAction(pluginID, actionID, body.ClipIDs, body.Options, body.Context)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	am.jsonOK(w, result)
}

// handlePluginSearch invokes a plugin's on_search hook for a picker field.
// Server-mode counterpart of the desktop SearchPluginOptions binding.
func (am *APIManager) handlePluginSearch(w http.ResponseWriter, r *http.Request) {
	pluginID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}

	var body struct {
		Source string `json:"source"`
		Query  string `json:"query"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Source == "" {
		am.jsonError(w, http.StatusBadRequest, "source is required")
		return
	}

	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusServiceUnavailable, "plugin manager not initialized")
		return
	}

	choices, err := am.app.pluginManager.SearchOptions(pluginID, body.Source, body.Query)
	if err != nil {
		switch {
		case errors.Is(err, plugin.ErrPluginBusy):
			am.jsonError(w, http.StatusConflict, err.Error())
		case errors.Is(err, plugin.ErrUnknownSearchSource):
			am.jsonError(w, http.StatusBadRequest, err.Error())
		default:
			am.jsonError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if choices == nil {
		choices = []plugin.Choice{}
	}
	am.jsonOK(w, map[string]interface{}{"choices": choices})
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

func (am *APIManager) handleGetPluginPermissions(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	rows, err := am.app.db.Query(`SELECT permission_type, path, granted_at FROM plugin_permissions WHERE plugin_id = ?`, id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	perms := []map[string]string{}
	for rows.Next() {
		var typ, p, granted string
		if err := rows.Scan(&typ, &p, &granted); err != nil {
			continue
		}
		perms = append(perms, map[string]string{"type": typ, "path": p, "granted_at": granted})
	}
	am.jsonOK(w, perms)
}

func (am *APIManager) handleRevokePluginPermission(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	var body struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := am.app.db.Exec(`DELETE FROM plugin_permissions WHERE plugin_id = ? AND permission_type = ? AND path = ?`, id, body.Type, body.Path); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handlePluginURLCheck(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	var body struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	allowed := am.app.pluginManager != nil && am.app.pluginManager.IsPluginURLAllowed(id, body.URL, body.Method)
	am.jsonOK(w, map[string]bool{"allowed": allowed})
}

// handleUploadPlugin accepts a Lua plugin file via multipart upload and returns a
// permission preview keyed by an opaque token. This is the headless equivalent of
// the desktop "open file dialog" import: the browser reads the local file and POSTs
// it here. The frontend reviews the returned preview, then POSTs the token to
// /plugins/confirm to install the exact reviewed content (no re-fetch, no TOCTOU).
func (am *APIManager) handleUploadPlugin(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusServiceUnavailable, "plugin manager not initialized")
		return
	}

	const maxPluginUploadBytes = 1 * 1024 * 1024 // 1 MB; Lua plugins are small

	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !strings.HasPrefix(mediaType, "multipart/") {
		am.jsonError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPluginUploadBytes+multipartOverheadBytes)

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read multipart data")
		return
	}

	data, err := io.ReadAll(io.LimitReader(part, maxPluginUploadBytes+1))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read file data")
		return
	}
	if len(data) > maxPluginUploadBytes {
		am.jsonError(w, http.StatusRequestEntityTooLarge, "plugin file too large (max 1 MB)")
		return
	}

	preview, err := plugin.PreviewFromSource(string(data), "")
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := newPluginUploadToken()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to generate upload token")
		return
	}
	am.app.pluginManager.StorePendingInstall(token, string(data))
	preview.Source = token

	am.jsonOK(w, preview)
}

// newPluginUploadToken returns an opaque token used to key a pending plugin upload.
// The "upload:" prefix guarantees it is neither an http(s) URL nor a resolvable
// filesystem path, so installPluginFromSource only ever resolves it from the
// pending-install cache.
func newPluginUploadToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "upload:" + hex.EncodeToString(buf), nil
}

func (am *APIManager) handlePreviewPluginFromURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	source := body.Source
	if source == "" {
		source = body.URL
	}
	pluginSource, err := plugin.FetchPluginSource(source)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := plugin.PreviewFromSource(pluginSource, source)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if am.app.pluginManager != nil {
		am.app.pluginManager.StorePendingInstall(source, pluginSource)
	}
	am.jsonOK(w, preview)
}

func (am *APIManager) handleConfirmPluginInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	info, err := am.app.installPluginFromSource(body.Source)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	am.jsonOK(w, info)
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

// --- Misc App Handlers ---

// viewerReadableSettings are the UI-preference keys the web client legitimately
// needs to read with a non-admin key. Every other setting (which may hold global
// config or share identity material) is admin-only, matching the admin-gated PUT.
var viewerReadableSettings = map[string]bool{
	"sort_field":             true,
	"sort_dir":               true,
	"tooltips_enabled":       true,
	"keyboard_shortcuts":     true,
	"plugin_update_interval": true,
}

func (am *APIManager) handleGetSetting(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	key := r.PathValue("key")
	// Settings are global, not tag-scoped — deny scoped keys outright, and limit
	// non-admin keys to the small UI-preference whitelist.
	if keyCtx.ScopedTagID != 0 {
		am.jsonError(w, http.StatusForbidden, "settings are not available for tag-scoped keys")
		return
	}
	if keyCtx.Role != "admin" && !viewerReadableSettings[key] {
		am.jsonError(w, http.StatusForbidden, "insufficient permissions for this setting")
		return
	}
	value, err := am.app.GetSetting(key)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]string{"value": value})
}

func (am *APIManager) handleModalAck(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager != nil {
		am.app.pluginManager.NotifyModalAcked()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleModalClose(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager != nil {
		am.app.pluginManager.NotifyModalClosed()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleSetSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := am.app.SetSetting(r.PathValue("key"), body.Value); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleRandomPort(w http.ResponseWriter, r *http.Request) {
	port, err := GetRandomPort()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"port": port})
}

func (am *APIManager) handleRefreshWatches(w http.ResponseWriter, r *http.Request) {
	if err := am.app.RefreshWatches(); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleCreateTempFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(getKeyContext(r), id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	path, err := am.app.CreateTempFile(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]string{"path": path})
}

func (am *APIManager) handleDeleteAllTempFiles(w http.ResponseWriter, r *http.Request) {
	if err := am.app.DeleteAllTempFiles(); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleImageDiff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClipIDA   int64 `json:"clip_id_a"`
		ClipIDB   int64 `json:"clip_id_b"`
		Threshold int   `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	keyCtx := getKeyContext(r)
	if err := am.enforceTagScope(keyCtx, body.ClipIDA); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := am.enforceTagScope(keyCtx, body.ClipIDB); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	diff, err := am.app.GetImageDiff(body.ClipIDA, body.ClipIDB, body.Threshold)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	am.jsonOK(w, diff)
}

func (am *APIManager) handleUpdateClipData(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(getKeyContext(r), id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 150*1024*1024)
	var body struct {
		ContentType string `json:"content_type"`
		Data        string `json:"data"`
		Filename    string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid base64 data")
		return
	}
	if len(decoded) > 100*1024*1024 {
		am.jsonError(w, http.StatusRequestEntityTooLarge, "clip exceeds 100MB limit")
		return
	}
	if err := am.app.UpdateClipData(id, body.ContentType, body.Data, body.Filename); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handlePreviewMergeTag(w http.ResponseWriter, r *http.Request) {
	sourceID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid source id")
		return
	}
	var body struct {
		DestID int64 `json:"dest_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if getKeyContext(r).ScopedTagID > 0 {
		for _, id := range []int64{sourceID, body.DestID} {
			var name string
			if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&name); err != nil || !am.isTagInScope(name, getKeyContext(r).ScopedTagID) {
				am.jsonError(w, http.StatusForbidden, "tag-scoped key can only preview merges within its scope")
				return
			}
		}
	}
	preview, err := am.app.PreviewMergeTag(sourceID, body.DestID)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	am.jsonOK(w, preview)
}

func (am *APIManager) handleFindClipsByFilenameAndTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Filenames []string `json:"filenames"`
		TagID     int64    `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.TagID <= 0 {
		am.jsonError(w, http.StatusBadRequest, "tag_id is required")
		return
	}
	keyCtx := getKeyContext(r)
	if keyCtx.ScopedTagID > 0 {
		var name string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", body.TagID).Scan(&name); err != nil || !am.isTagInScope(name, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag_id is outside key scope")
			return
		}
	}
	matches, err := am.app.FindClipsByFilenameAndTag(body.Filenames, body.TagID)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if matches == nil {
		matches = []ClipMatch{}
	}
	am.jsonOK(w, matches)
}

// --- Share Handlers ---

func (am *APIManager) handleGetShareStatus(w http.ResponseWriter, r *http.Request) {
	if am.app.shareManager == nil {
		am.jsonOK(w, map[string]any{"shares": []ShareInfo{}, "follows": []FollowInfo{}})
		return
	}
	shares, follows := am.app.shareManager.GetShareStatus()
	if shares == nil {
		shares = []ShareInfo{}
	}
	if follows == nil {
		follows = []FollowInfo{}
	}
	am.jsonOK(w, map[string]any{"shares": shares, "follows": follows})
}

func (am *APIManager) handleStartShare(w http.ResponseWriter, r *http.Request) {
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	var body struct {
		TagID int64 `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	info, err := am.app.shareManager.StartShare(body.TagID)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	am.jsonOK(w, info)
}

func (am *APIManager) handleStopShare(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseIntParam(r.PathValue("tagId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	if err := am.app.shareManager.StopShare(tagID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handlePauseShare(w http.ResponseWriter, r *http.Request) {
	am.handleSharePauseState(w, r, true)
}

func (am *APIManager) handleResumeShare(w http.ResponseWriter, r *http.Request) {
	am.handleSharePauseState(w, r, false)
}

func (am *APIManager) handleSharePauseState(w http.ResponseWriter, r *http.Request, paused bool) {
	tagID, err := parseIntParam(r.PathValue("tagId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	if paused {
		err = am.app.shareManager.PauseShare(tagID)
	} else {
		err = am.app.shareManager.ResumeShare(tagID)
	}
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleFollow(w http.ResponseWriter, r *http.Request) {
	am.handleFollowCreate(w, r, false)
}

func (am *APIManager) handleFollowWithoutDial(w http.ResponseWriter, r *http.Request) {
	am.handleFollowCreate(w, r, true)
}

func (am *APIManager) handleFollowCreate(w http.ResponseWriter, r *http.Request, direct bool) {
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	var body struct {
		ShareString  string `json:"share_string"`
		LocalTagName string `json:"local_tag_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var info FollowInfo
	var err error
	if direct {
		info, err = am.app.shareManager.FollowWithoutDial(body.ShareString, body.LocalTagName)
	} else {
		info, err = am.app.shareManager.Follow(body.ShareString, body.LocalTagName)
	}
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	am.jsonOK(w, info)
}

func (am *APIManager) handleTestFollowConnection(w http.ResponseWriter, r *http.Request) {
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	var body struct {
		ShareString string `json:"share_string"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := am.app.shareManager.TestFollowConnection(body.ShareString); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	am.handleFollowIDOnly(w, r, func(id int64) error { return am.app.shareManager.Unfollow(id) })
}

func (am *APIManager) handleReconnectFollow(w http.ResponseWriter, r *http.Request) {
	am.handleFollowIDOnly(w, r, func(id int64) error { return am.app.shareManager.ReconnectFollow(id) })
}

func (am *APIManager) handlePauseFollow(w http.ResponseWriter, r *http.Request) {
	am.handleFollowIDOnly(w, r, func(id int64) error { return am.app.shareManager.PauseFollow(id) })
}

func (am *APIManager) handleResumeFollow(w http.ResponseWriter, r *http.Request) {
	am.handleFollowIDOnly(w, r, func(id int64) error { return am.app.shareManager.ResumeFollow(id) })
}

func (am *APIManager) handleFollowIDOnly(w http.ResponseWriter, r *http.Request, fn func(int64) error) {
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid follow id")
		return
	}
	if err := fn(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleUpdateFollowTag(w http.ResponseWriter, r *http.Request) {
	if am.app.shareManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "share manager not initialized")
		return
	}
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid follow id")
		return
	}
	var body struct {
		LocalTagName string `json:"local_tag_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	info, err := am.app.shareManager.UpdateFollowTag(id, body.LocalTagName)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	am.jsonOK(w, info)
}

func (am *APIManager) handleGetShareLogs(w http.ResponseWriter, r *http.Request) {
	if am.app.shareManager == nil {
		am.jsonOK(w, []ShareLogEntry{})
		return
	}
	followID, _ := strconv.ParseInt(r.URL.Query().Get("follow_id"), 10, 64)
	pubID, _ := strconv.ParseInt(r.URL.Query().Get("publication_id"), 10, 64)
	logs := am.app.shareManager.GetShareLogs(followID, pubID)
	if logs == nil {
		logs = []ShareLogEntry{}
	}
	am.jsonOK(w, logs)
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
		am.jsonError(w, http.StatusNotImplemented, "clipboard not available on this server")
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
		am.jsonError(w, http.StatusNotImplemented, "clipboard not available on this server")
		return
	}

	if err := am.app.clipboardService.CopyFileToClipboard(body.ClipID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to copy file to clipboard: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
