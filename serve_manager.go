package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ServeInfo describes a running tag server for the frontend.
type ServeInfo struct {
	TagID        int64  `json:"tag_id"`
	TagName      string `json:"tag_name"`
	Port         int    `json:"port"`
	BindAll      bool   `json:"bind_all"`
	URL          string `json:"url"`
	Running      bool   `json:"running"`
	RequestCount int64  `json:"request_count"`
	ApiAccess    string `json:"api_access"`
}

// tagServer is the internal state for a single running HTTP server.
type tagServer struct {
	tagID        int64
	tagName      string
	port         int
	bindAll      bool
	server       *http.Server
	requestCount int64 // accessed atomically
	apiAccess    string              // "none", "read", "readwrite"
	serveKey     string              // random token for cookie auth
	clipMutexes  map[string]*sync.Mutex // per-clip filename mutex
	clipMu       sync.Mutex            // protects clipMutexes map
}

// virtualFile represents a clip exposed as a file in the HTTP listing.
type virtualFile struct {
	clipID      int64
	filename    string
	contentType string
	size        int64
}

// directoryEntry is the JSON representation of a file or folder in a directory listing.
type directoryEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Type        string `json:"type"` // "file" or "directory"
}

// childTagInfo holds the short name and ID of an immediate child tag.
type childTagInfo struct {
	shortName string
	tagID     int64
}

// ServeManager manages per-tag HTTP servers.
type ServeManager struct {
	app     *App
	servers map[int64]*tagServer
	mu      sync.RWMutex
}

// NewServeManager creates a new ServeManager.
func NewServeManager(app *App) *ServeManager {
	return &ServeManager{
		app:     app,
		servers: make(map[int64]*tagServer),
	}
}

// buildFileList returns the virtual files for a tag, ordered by created_at ASC.
// Duplicate filenames get a " (2)", " (3)" etc. suffix. Archived clips are excluded.
func (sm *ServeManager) buildFileList(tagID int64) ([]virtualFile, error) {
	rows, err := sm.app.db.Query(`
		SELECT c.id, c.filename, c.content_type, LENGTH(c.data)
		FROM clips c
		JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ? AND c.is_archived = 0
		ORDER BY c.created_at ASC
	`, tagID)
	if err != nil {
		return nil, fmt.Errorf("failed to query clips for tag %d: %w", tagID, err)
	}
	defer rows.Close()

	var files []virtualFile
	nameCount := make(map[string]int)

	for rows.Next() {
		var vf virtualFile
		var filename *string
		if err := rows.Scan(&vf.clipID, &filename, &vf.contentType, &vf.size); err != nil {
			return nil, fmt.Errorf("failed to scan clip row: %w", err)
		}

		if filename != nil && *filename != "" {
			vf.filename = *filename
		} else {
			vf.filename = fmt.Sprintf("clip-%d", vf.clipID)
		}

		// Resolve duplicate filenames.
		nameCount[vf.filename]++
		if nameCount[vf.filename] > 1 {
			ext := filepath.Ext(vf.filename)
			base := strings.TrimSuffix(vf.filename, ext)
			vf.filename = fmt.Sprintf("%s (%d)%s", base, nameCount[vf.filename], ext)
		}

		files = append(files, vf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return files, nil
}

// getImmediateChildTags returns the immediate child tags of the given parent tag name.
func (sm *ServeManager) getImmediateChildTags(parentTagName string) ([]childTagInfo, error) {
	rows, err := sm.app.db.Query(`SELECT id, name FROM tags WHERE name LIKE ? || '/%'`, parentTagName)
	if err != nil {
		return nil, fmt.Errorf("failed to query child tags: %w", err)
	}
	defer rows.Close()

	var children []childTagInfo
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("failed to scan tag row: %w", err)
		}
		if isImmediateChildOf(name, parentTagName) {
			children = append(children, childTagInfo{
				shortName: getShortTagName(name),
				tagID:     id,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return children, nil
}

// resolveSubtag looks up a tag by exact name and returns its ID, or 0 if not found.
func (sm *ServeManager) resolveSubtag(tagName string) (int64, error) {
	var id int64
	err := sm.app.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, tagName).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// serveClipData reads the blob from the database and writes it to the HTTP response.
func (sm *ServeManager) serveClipData(w http.ResponseWriter, clipID int64, contentType string) {
	var data []byte
	err := sm.app.db.QueryRow("SELECT data FROM clips WHERE id = ?", clipID).Scan(&data)
	if err != nil {
		http.Error(w, "clip not found", http.StatusNotFound)
		return
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

// makeHandler builds the http.Handler for a tag server.
func (sm *ServeManager) makeHandler(ts *tagServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow cross-origin requests (Wails webview uses wails:// origin)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Route /_api/* requests to the JSON API handler.
		if strings.HasPrefix(r.URL.Path, "/_api/") || r.URL.Path == "/_api" {
			atomic.AddInt64(&ts.requestCount, 1)
			sm.handleJSONAPI(w, r, ts)
			return
		}

		atomic.AddInt64(&ts.requestCount, 1)

		// Set auth cookie on all responses when JSON API is enabled.
		if ts.apiAccess != "none" {
			http.SetCookie(w, &http.Cookie{
				Name:     "_mp_serve_key",
				Value:    ts.serveKey,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}

		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		if decodedPath, err := url.PathUnescape(reqPath); err == nil {
			reqPath = decodedPath
		}

		// Strip trailing slash to determine directory vs file,
		// but remember whether the original path ended with one.
		isDir := reqPath == "" || strings.HasSuffix(reqPath, "/")
		reqPath = strings.TrimSuffix(reqPath, "/")

		// Walk path segments to resolve the deepest subtag.
		// Start from the root tag that this server is serving.
		currentTagName := ts.tagName
		currentTagID := ts.tagID

		var filename string
		if reqPath != "" {
			segments := strings.Split(reqPath, "/")
			for i, seg := range segments {
				// Try to resolve this segment as a child tag.
				childTagName := currentTagName + "/" + seg
				childID, err := sm.resolveSubtag(childTagName)
				if err != nil {
					log.Printf("serve: failed to resolve subtag %q: %v", childTagName, err)
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}

				if childID != 0 {
					// This segment is a subtag — descend into it.
					currentTagName = childTagName
					currentTagID = childID
				} else {
					// Not a subtag — must be a filename. It should be the last segment.
					if i != len(segments)-1 {
						// Middle segment doesn't match a tag — 404.
						http.NotFound(w, r)
						return
					}
					filename = seg
					isDir = false
				}
			}
		}

		// If all segments resolved to tags and no explicit trailing slash,
		// check if the last segment is actually a file in the parent tag.
		// But if the path ended with "/" or was empty, it's a directory request.
		if filename == "" && !isDir && reqPath != "" {
			// The last segment resolved as a subtag. Redirect to add trailing slash.
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}

		// Build the file list for the resolved tag.
		files, err := sm.buildFileList(currentTagID)
		if err != nil {
			log.Printf("serve: failed to build file list for tag %d: %v", currentTagID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if isDir {
			// Directory listing — check for index.html clip first (scoped to this level).
			for _, f := range files {
				if f.filename == "index.html" {
					sm.serveClipData(w, f.clipID, f.contentType)
					return
				}
			}

			// Get child tags for directory listing.
			childTags, err := sm.getImmediateChildTags(currentTagName)
			if err != nil {
				log.Printf("serve: failed to get child tags for %q: %v", currentTagName, err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			// Build the URL prefix for links in this directory.
			urlPrefix := "/"
			if currentTagName != ts.tagName {
				// We're in a subdirectory — compute relative path from root tag.
				relPath := strings.TrimPrefix(currentTagName, ts.tagName+"/")
				urlPrefix = "/" + relPath + "/"
			}

			sm.serveDirectoryListing(w, r, currentTagName, files, childTags, urlPrefix)
			return
		}

		// Serve a specific file.
		for _, f := range files {
			if f.filename == filename {
				sm.serveClipData(w, f.clipID, f.contentType)
				return
			}
		}

		http.NotFound(w, r)
	})
}

// serveDirectoryListing responds with either JSON or an HTML directory listing
// depending on the Accept header.
func (sm *ServeManager) serveDirectoryListing(w http.ResponseWriter, r *http.Request, tagName string, files []virtualFile, childTags []childTagInfo, urlPrefix string) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		var entries []directoryEntry
		// Add folder entries first.
		for _, ct := range childTags {
			entries = append(entries, directoryEntry{
				Name: ct.shortName,
				Type: "directory",
			})
		}
		// Add file entries.
		for _, f := range files {
			entries = append(entries, directoryEntry{
				Name:        f.filename,
				Size:        f.size,
				ContentType: f.contentType,
				Type:        "file",
			})
		}
		if entries == nil {
			entries = []directoryEntry{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
		return
	}

	// HTML directory listing using the app's stone palette and IBM Plex Mono.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mahpastes</title>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'IBM Plex Mono',monospace;background:#fafaf9;color:#292524;padding:2rem}
h1{font-size:.75rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#78716c;margin-bottom:1.5rem}
table{width:100%;border-collapse:collapse}
th{text-align:left;font-size:.625rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#a8a29e;border-bottom:1px solid #e7e5e4;padding:.5rem 0}
td{font-size:.75rem;padding:.5rem 0;border-bottom:1px solid #e7e5e4}
a{color:#292524;text-decoration:none}
a:hover{text-decoration:underline}
.size{color:#78716c;text-align:right;font-size:.6875rem}
.type{color:#a8a29e;font-size:.625rem}
.empty{color:#a8a29e;font-size:.75rem;padding:2rem 0}
.icon{display:inline-block;width:1rem;margin-right:.25rem;vertical-align:middle}
</style>
</head>
<body>
<h1>`)
	b.WriteString(html.EscapeString(tagName))
	b.WriteString(`</h1>
`)

	hasContent := len(files) > 0 || len(childTags) > 0
	if !hasContent {
		b.WriteString(`<p class="empty">No files.</p>`)
	} else {
		b.WriteString(`<table>
<tr><th>Name</th><th>Type</th><th style="text-align:right">Size</th></tr>
`)
		// Render folder entries first.
		for _, ct := range childTags {
			name := html.EscapeString(ct.shortName)
			href := html.EscapeString(urlPrefix + url.PathEscape(ct.shortName) + "/")
			fmt.Fprintf(&b,
				"<tr><td><span class=\"icon\">\U0001F4C1</span><a href=\"%s\">%s/</a></td><td class=\"type\">folder</td><td class=\"size\"></td></tr>\n",
				href, name,
			)
		}
		// Render file entries.
		for _, f := range files {
			name := html.EscapeString(f.filename)
			href := html.EscapeString(urlPrefix + url.PathEscape(f.filename))
			fmt.Fprintf(&b,
				"<tr><td><a href=\"%s\">%s</a></td><td class=\"type\">%s</td><td class=\"size\">%s</td></tr>\n",
				href, name, html.EscapeString(f.contentType), formatSize(f.size),
			)
		}
		b.WriteString("</table>\n")
	}

	b.WriteString("</body>\n</html>\n")
	fmt.Fprint(w, b.String())
}

// formatSize returns a human-readable byte size string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// StartServing starts an HTTP server for the given tag on the specified port.
func (sm *ServeManager) StartServing(tagID int64, port int, bindAll bool, apiAccess string) (ServeInfo, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.servers[tagID]; exists {
		return ServeInfo{}, fmt.Errorf("tag %d is already being served", tagID)
	}

	// Look up tag name.
	var tagName string
	err := sm.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName)
	if err != nil {
		return ServeInfo{}, fmt.Errorf("tag not found: %w", err)
	}

	if apiAccess != "read" && apiAccess != "readwrite" {
		apiAccess = "none"
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return ServeInfo{}, fmt.Errorf("failed to generate serve key: %w", err)
	}

	host := "127.0.0.1"
	if bindAll {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	ts := &tagServer{
		tagID:       tagID,
		tagName:     tagName,
		port:        port,
		bindAll:     bindAll,
		apiAccess:   apiAccess,
		serveKey:    hex.EncodeToString(tokenBytes),
		clipMutexes: make(map[string]*sync.Mutex),
	}

	ts.server = &http.Server{
		Addr:    addr,
		Handler: sm.makeHandler(ts),
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return ServeInfo{}, fmt.Errorf("port %d unavailable: %w", port, err)
	}

	// Resolve actual port (useful if port was 0, though we don't expose that path).
	actualPort := ln.Addr().(*net.TCPAddr).Port
	ts.port = actualPort

	sm.servers[tagID] = ts

	go func() {
		log.Printf("serve: starting server for tag %q (id=%d) on %s", tagName, tagID, ln.Addr())
		if err := ts.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve: server for tag %d stopped with error: %v", tagID, err)
		}
	}()

	return ServeInfo{
		TagID:        tagID,
		TagName:      tagName,
		Port:         actualPort,
		BindAll:      bindAll,
		URL:          displayServerURL(actualPort),
		Running:      true,
		RequestCount: 0,
		ApiAccess:    apiAccess,
	}, nil
}

// StopServing gracefully stops the server for the given tag.
func (sm *ServeManager) StopServing(tagID int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ts, exists := sm.servers[tagID]
	if !exists {
		return fmt.Errorf("no server running for tag %d", tagID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ts.server.Shutdown(ctx); err != nil {
		log.Printf("serve: forced close for tag %d: %v", tagID, err)
		ts.server.Close()
	}

	delete(sm.servers, tagID)
	log.Printf("serve: stopped server for tag %q (id=%d)", ts.tagName, tagID)
	return nil
}

// GetStatus returns the status of all running tag servers.
func (sm *ServeManager) GetStatus() []ServeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	infos := make([]ServeInfo, 0, len(sm.servers))
	for _, ts := range sm.servers {
		infos = append(infos, ServeInfo{
			TagID:        ts.tagID,
			TagName:      ts.tagName,
			Port:         ts.port,
			BindAll:      ts.bindAll,
			URL:          displayServerURL(ts.port),
			Running:      true,
			RequestCount: atomic.LoadInt64(&ts.requestCount),
			ApiAccess:    ts.apiAccess,
		})
	}
	return infos
}

// StopAll gracefully stops every running server. Called during app shutdown.
func (sm *ServeManager) StopAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for tagID, ts := range sm.servers {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := ts.server.Shutdown(ctx); err != nil {
			ts.server.Close()
		}
		cancel()
		log.Printf("serve: stopped server for tag %q (id=%d) during shutdown", ts.tagName, tagID)
	}
	sm.servers = make(map[int64]*tagServer)
}

// GetRandomPort finds an available TCP port on localhost by briefly binding to port 0.
func GetRandomPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
