package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TransferFileHandler serves prepared temp files over HTTP so that Chromium's
// DownloadURL DataTransfer type can produce CF_HDROP on Windows.
// Each transfer URL includes a random token to prevent local enumeration attacks.
type TransferFileHandler struct {
	app         *App
	mu          sync.RWMutex
	tokens      map[string]transferToken
	mediaTokens map[string]mediaToken
	thumbKey    string           // see ThumbnailURLBase; set once, guarded by mu
	now         func() time.Time // tests only; nil means time.Now
}

// transferToken authorizes one temp file for drag-out (DownloadURL) reads.
// It expires with the file's lease: every hover and drag mints one, so an
// unexpiring map grew for the life of the process.
type transferToken struct {
	filename  string
	expiresAt time.Time
}

type mediaToken struct {
	absPath     string
	filename    string
	contentType string
	expiresAt   time.Time
}

func NewTransferFileHandler(app *App) *TransferFileHandler {
	return &TransferFileHandler{app: app}
}

// RegisterToken stores a token that authorizes access to a specific temp file
// for the length of a temp file lease. Expired tokens are dropped here.
func (h *TransferFileHandler) RegisterToken(token, filename string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokens == nil {
		h.tokens = make(map[string]transferToken)
	}
	now := h.clock()
	for existing, item := range h.tokens {
		if !item.expiresAt.After(now) {
			delete(h.tokens, existing)
		}
	}
	h.tokens[token] = transferToken{filename: filename, expiresAt: now.Add(defaultTempLeaseTTL)}
}

func (h *TransferFileHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// RegisterMediaToken authorizes range-based reads of one leased temp file for
// in-app media playback. Separate from the drag-out token map so playback URLs
// can carry their own expiry and an inline disposition.
func (h *TransferFileHandler) RegisterMediaToken(token, absPath, filename, contentType string, expiresAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mediaTokens == nil {
		h.mediaTokens = make(map[string]mediaToken)
	}
	now := time.Now()
	for existing, item := range h.mediaTokens {
		if !item.expiresAt.After(now) {
			delete(h.mediaTokens, existing)
		}
	}
	h.mediaTokens[token] = mediaToken{absPath: absPath, filename: filename, contentType: contentType, expiresAt: expiresAt}
}

// generateTransferToken creates a random hex token for transfer URL authorization.
func generateTransferToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *TransferFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/media/") {
		h.serveMedia(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/thumb/") {
		h.serveThumb(w, r)
		return
	}

	const prefix = "/transfer/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}

	// URL format: /transfer/{token}/{filename}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 1 {
		http.NotFound(w, r)
		return
	}
	token := rest[:slashIdx]
	filename := rest[slashIdx+1:]

	// Sanitize: only allow the base name (no path traversal)
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
		http.NotFound(w, r)
		return
	}

	// Validate the token
	h.mu.RLock()
	authorized, ok := h.tokens[token]
	h.mu.RUnlock()
	if !ok || authorized.filename != filename || !authorized.expiresAt.After(h.clock()) {
		http.NotFound(w, r)
		return
	}

	tempDir := h.app.tempDir
	if tempDir == "" {
		http.Error(w, "transfer service not ready", http.StatusServiceUnavailable)
		return
	}

	fullPath := filepath.Join(tempDir, filename)

	// Verify the resolved path is still inside tempDir (defense in depth)
	absPath, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(tempDir)) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Set Content-Type from extension
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))

	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func (h *TransferFileHandler) serveMedia(w http.ResponseWriter, r *http.Request) {
	const prefix = "/media/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 1 {
		http.NotFound(w, r)
		return
	}
	token := rest[:slashIdx]
	filename := filepath.Base(rest[slashIdx+1:])
	if filename == "." || filename == ".." || filename == "" {
		http.NotFound(w, r)
		return
	}

	h.mu.RLock()
	item, ok := h.mediaTokens[token]
	h.mu.RUnlock()
	if !ok || item.filename != filename || !item.expiresAt.After(time.Now()) {
		http.NotFound(w, r)
		return
	}

	// Defense in depth: the token already names the file, but keep it inside
	// the temp directory even if a stale token outlives a tempDir change.
	tempDir := h.app.tempDir
	if tempDir == "" || !strings.HasPrefix(item.absPath, filepath.Clean(tempDir)) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(item.absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Playing a clip keeps it alive. Unlike a drag-out, which is over in a
	// moment, playback is long-lived: watching a video for longer than the
	// lease would otherwise let the pruner delete the file (and the token
	// expire) mid-stream. Slide both while the clip is actually being read.
	h.touchMediaLease(token, item, info.ModTime())

	// The clip's stored content type, never one guessed from the temp file's
	// name: tempFilenameForClip keeps the clip's own filename, so a video/mp4
	// clip called "recording.txt" would be served as text/plain and, under
	// nosniff, refuse to play.
	ct := item.contentType
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(filename))
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
	// ServeContent handles Range, If-Range and HEAD, seeking the file directly.
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

// touchMediaLease extends a media capability and its backing file's lease when
// they are past halfway, so an in-progress playback is never pruned out from
// under the browser. Throttled to avoid an mtime write per range request.
func (h *TransferFileHandler) touchMediaLease(token string, item mediaToken, modTime time.Time) {
	now := time.Now()
	if item.expiresAt.Sub(now) > defaultTempLeaseTTL/2 && now.Sub(modTime) < defaultTempLeaseTTL/2 {
		return
	}
	if err := os.Chtimes(item.absPath, now, now); err != nil {
		return
	}
	item.expiresAt = now.Add(defaultTempLeaseTTL)
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.mediaTokens[token]; ok && current.absPath == item.absPath {
		h.mediaTokens[token] = item
	}
}

// ThumbnailURLBase returns the prefix of gallery thumbnail URLs,
// "/thumb/{key}/", to which the frontend appends "{clipID}/{contentHash}".
//
// The key is random per process and handed out only through a bound method,
// so reaching a thumbnail takes the same thing as calling GetClipData: code
// running in the app's WebView. Unlike /media/ it is not minted per clip — a
// gallery page would otherwise make one bridge call per card just to learn a
// URL, which is the round trip thumbnails exist to remove.
func (h *TransferFileHandler) ThumbnailURLBase() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.thumbKey == "" {
		key, err := generateTransferToken()
		if err != nil {
			return "", err
		}
		h.thumbKey = key
	}
	return "/thumb/" + h.thumbKey + "/", nil
}

// serveThumb serves /thumb/{key}/{clipID}[/{contentHash}].
func (h *TransferFileHandler) serveThumb(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/thumb/"), "/")
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	h.mu.RLock()
	key := h.thumbKey
	h.mu.RUnlock()
	if key == "" || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(key)) != 1 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	hash := ""
	if len(parts) == 3 {
		hash = parts[2]
	}
	if h.app == nil || h.app.thumbCache == nil || !h.app.thumbCache.Serve(w, r, id, hash) {
		http.NotFound(w, r)
	}
}
