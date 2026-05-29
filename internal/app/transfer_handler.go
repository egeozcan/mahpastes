package app

import (
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TransferFileHandler serves prepared temp files over HTTP so that Chromium's
// DownloadURL DataTransfer type can produce CF_HDROP on Windows.
// Each transfer URL includes a random token to prevent local enumeration attacks.
type TransferFileHandler struct {
	app    *App
	mu     sync.RWMutex
	tokens map[string]string // token → filename
}

func NewTransferFileHandler(app *App) *TransferFileHandler {
	return &TransferFileHandler{app: app}
}

// RegisterToken stores a one-time token that authorizes access to a specific temp file.
func (h *TransferFileHandler) RegisterToken(token, filename string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokens == nil {
		h.tokens = make(map[string]string)
	}
	h.tokens[token] = filename
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
	// Only handle GET requests under /transfer/
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	if !ok || authorized != filename {
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
