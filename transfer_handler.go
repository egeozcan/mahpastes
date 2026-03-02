package main

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// TransferFileHandler serves prepared temp files over HTTP so that Chromium's
// DownloadURL DataTransfer type can produce CF_HDROP on Windows.
type TransferFileHandler struct {
	app *App
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

	filename := strings.TrimPrefix(r.URL.Path, prefix)
	// Sanitize: only allow the base name (no path traversal)
	filename = filepath.Base(filename)
	if filename == "." || filename == ".." || filename == "" {
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
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	http.ServeContent(w, r, filename, info.ModTime(), f)
}
