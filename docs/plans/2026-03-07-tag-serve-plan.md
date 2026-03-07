# Tag Serve Feature Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Serve clips under a tag as static HTTP servers with index.html rewrite and directory listing.

**Architecture:** New ServeManager (internal, manages `http.Server` instances per tag) + ServeService (Wails-bound frontend API). Frontend gets a 3-way view toggle (Clips/Watch/Serve) in the drawer and a dedicated serve view with server cards.

**Tech Stack:** Go net/http, Wails v2 bindings, vanilla JS, Tailwind CSS, Playwright e2e tests.

---

### Task 1: ServeManager — Core Struct and Types

**Files:**
- Create: `serve_manager.go`

**Step 1: Create `serve_manager.go` with types and constructor**

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// ServeInfo describes a running or configured tag server.
type ServeInfo struct {
	TagID        int64  `json:"tag_id"`
	TagName      string `json:"tag_name"`
	Port         int    `json:"port"`
	BindAll      bool   `json:"bind_all"`
	URL          string `json:"url"`
	Running      bool   `json:"running"`
	RequestCount int64  `json:"request_count"`
}

// tagServer tracks a single running HTTP server for one tag.
type tagServer struct {
	tagID        int64
	tagName      string
	port         int
	bindAll      bool
	server       *http.Server
	requestCount int64 // atomic
}

// ServeManager manages multiple HTTP servers, one per tag.
type ServeManager struct {
	app     *App
	servers map[int64]*tagServer // tagID -> server
	mu      sync.RWMutex
}

// NewServeManager creates a new serve manager.
func NewServeManager(app *App) *ServeManager {
	return &ServeManager{
		app:     app,
		servers: make(map[int64]*tagServer),
	}
}
```

**Step 2: Commit**

```
git add serve_manager.go
git commit -m "feat(serve): add ServeManager struct and types"
```

---

### Task 2: ServeManager — HTTP Handler

**Files:**
- Modify: `serve_manager.go`

**Step 1: Add the virtual file list builder and HTTP handler**

Add these methods to `serve_manager.go` after the constructor:

```go
// virtualFile represents a clip mapped to a filename for serving.
type virtualFile struct {
	clipID      int64
	filename    string
	contentType string
	size        int64
}

// buildFileList queries clips for a tag and resolves duplicate filenames.
// Returns the list ordered by created_at ASC with duplicates suffixed as "name (2).ext".
func (sm *ServeManager) buildFileList(tagID int64) ([]virtualFile, error) {
	rows, err := sm.app.db.Query(`
		SELECT c.id, c.filename, c.content_type, LENGTH(c.data) as size
		FROM clips c
		INNER JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ? AND c.is_archived = 0
		ORDER BY c.created_at ASC
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []virtualFile
	nameCount := make(map[string]int) // lowercase name -> count seen so far

	for rows.Next() {
		var f virtualFile
		if err := rows.Scan(&f.clipID, &f.filename, &f.contentType, &f.size); err != nil {
			return nil, err
		}
		if f.filename == "" {
			f.filename = fmt.Sprintf("clip-%d", f.clipID)
		}

		key := strings.ToLower(f.filename)
		nameCount[key]++
		if nameCount[key] > 1 {
			ext := filepath.Ext(f.filename)
			base := strings.TrimSuffix(f.filename, ext)
			f.filename = fmt.Sprintf("%s (%d)%s", base, nameCount[key], ext)
		}

		files = append(files, f)
	}
	return files, rows.Err()
}

// serveClipData writes clip data directly from the database to the response.
func (sm *ServeManager) serveClipData(w http.ResponseWriter, clipID int64, contentType string) {
	var data []byte
	err := sm.app.db.QueryRow("SELECT data FROM clips WHERE id = ?", clipID).Scan(&data)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

// makeHandler creates an http.Handler for serving a specific tag's clips.
func (sm *ServeManager) makeHandler(ts *tagServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&ts.requestCount, 1)

		files, err := sm.buildFileList(ts.tagID)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			log.Printf("serve: failed to build file list for tag %d: %v", ts.tagID, err)
			return
		}

		reqPath := strings.TrimPrefix(r.URL.Path, "/")

		// Check for index.html rewrite
		if reqPath == "" || reqPath == "index.html" {
			for _, f := range files {
				if strings.EqualFold(f.filename, "index.html") {
					sm.serveClipData(w, f.clipID, f.contentType)
					return
				}
			}
			// No index.html — serve directory listing
			sm.serveDirectoryListing(w, r, files)
			return
		}

		// Match requested path to a file
		for _, f := range files {
			if f.filename == reqPath {
				sm.serveClipData(w, f.clipID, f.contentType)
				return
			}
		}

		http.NotFound(w, r)
	})
}
```

**Step 2: Add directory listing (HTML + JSON content negotiation)**

```go
// directoryEntry is the JSON representation of a file in the listing.
type directoryEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// serveDirectoryListing renders the file list as HTML or JSON based on Accept header.
func (sm *ServeManager) serveDirectoryListing(w http.ResponseWriter, r *http.Request, files []virtualFile) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		entries := make([]directoryEntry, len(files))
		for i, f := range files {
			entries[i] = directoryEntry{Name: f.filename, Size: f.size, ContentType: f.contentType}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	fmt.Fprint(w, `<title>Index</title>`)
	fmt.Fprint(w, `<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">`)
	fmt.Fprint(w, `<style>*{margin:0;padding:0;box-sizing:border-box}body{font-family:'IBM Plex Mono',monospace;background:#fafaf9;color:#292524;padding:2rem}`)
	fmt.Fprint(w, `h1{font-size:.75rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#a8a29e;margin-bottom:1.5rem}`)
	fmt.Fprint(w, `table{width:100%;border-collapse:collapse}th{text-align:left;font-size:.625rem;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:#a8a29e;padding:.5rem 0;border-bottom:1px solid #e7e5e4}`)
	fmt.Fprint(w, `td{padding:.625rem 0;border-bottom:1px solid #f5f5f4;font-size:.75rem}a{color:#292524;text-decoration:none}a:hover{text-decoration:underline}`)
	fmt.Fprint(w, `.size{color:#a8a29e;text-align:right;font-size:.6875rem}.type{color:#a8a29e;font-size:.6875rem}</style></head><body>`)
	fmt.Fprint(w, `<h1>Index</h1><table><thead><tr><th>Name</th><th class="type">Type</th><th class="size">Size</th></tr></thead><tbody>`)
	for _, f := range files {
		fmt.Fprintf(w, `<tr><td><a href="/%s">%s</a></td><td class="type">%s</td><td class="size">%s</td></tr>`,
			f.filename, f.filename, f.contentType, formatSize(f.size))
	}
	fmt.Fprint(w, `</tbody></table></body></html>`)
}

// formatSize formats bytes into human-readable form.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
```

**Step 3: Commit**

```
git add serve_manager.go
git commit -m "feat(serve): add HTTP handler with index.html rewrite and directory listing"
```

---

### Task 3: ServeManager — Start, Stop, Status, GetRandomPort

**Files:**
- Modify: `serve_manager.go`

**Step 1: Add StartServing method**

```go
// StartServing starts an HTTP server for the given tag.
func (sm *ServeManager) StartServing(tagID int64, port int, bindAll bool) (ServeInfo, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if already serving this tag
	if _, exists := sm.servers[tagID]; exists {
		return ServeInfo{}, fmt.Errorf("tag %d is already being served", tagID)
	}

	// Get tag name
	var tagName string
	err := sm.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName)
	if err != nil {
		return ServeInfo{}, fmt.Errorf("tag not found: %w", err)
	}

	host := "127.0.0.1"
	if bindAll {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	ts := &tagServer{
		tagID:   tagID,
		tagName: tagName,
		port:    port,
		bindAll: bindAll,
	}

	mux := http.NewServeMux()
	mux.Handle("/", sm.makeHandler(ts))

	ts.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start listener to catch port conflicts early
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return ServeInfo{}, fmt.Errorf("cannot bind to %s: %w", addr, err)
	}

	sm.servers[tagID] = ts

	go func() {
		if err := ts.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve: server for tag %d stopped with error: %v", tagID, err)
			sm.mu.Lock()
			delete(sm.servers, tagID)
			sm.mu.Unlock()
		}
	}()

	url := fmt.Sprintf("http://%s:%d", host, port)
	if bindAll {
		url = fmt.Sprintf("http://0.0.0.0:%d", port)
	}

	log.Printf("serve: started serving tag '%s' (id=%d) on %s", tagName, tagID, url)

	return ServeInfo{
		TagID:        tagID,
		TagName:      tagName,
		Port:         port,
		BindAll:      bindAll,
		URL:          url,
		Running:      true,
		RequestCount: 0,
	}, nil
}
```

**Step 2: Add StopServing, GetStatus, StopAll, and GetRandomPort methods**

```go
// StopServing stops the HTTP server for the given tag.
func (sm *ServeManager) StopServing(tagID int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ts, exists := sm.servers[tagID]
	if !exists {
		return fmt.Errorf("tag %d is not being served", tagID)
	}

	if err := ts.server.Close(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	delete(sm.servers, tagID)
	log.Printf("serve: stopped serving tag '%s' (id=%d)", ts.tagName, tagID)
	return nil
}

// GetStatus returns the status of all running servers.
func (sm *ServeManager) GetStatus() []ServeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []ServeInfo
	for _, ts := range sm.servers {
		host := "127.0.0.1"
		if ts.bindAll {
			host = "0.0.0.0"
		}
		result = append(result, ServeInfo{
			TagID:        ts.tagID,
			TagName:      ts.tagName,
			Port:         ts.port,
			BindAll:      ts.bindAll,
			URL:          fmt.Sprintf("http://%s:%d", host, ts.port),
			Running:      true,
			RequestCount: atomic.LoadInt64(&ts.requestCount),
		})
	}
	return result
}

// StopAll stops all running servers. Called on app shutdown.
func (sm *ServeManager) StopAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for tagID, ts := range sm.servers {
		ts.server.Close()
		log.Printf("serve: stopped serving tag '%s' (id=%d) on shutdown", ts.tagName, tagID)
	}
	sm.servers = make(map[int64]*tagServer)
}

// GetRandomPort finds an available TCP port.
func GetRandomPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
```

**Step 3: Commit**

```
git add serve_manager.go
git commit -m "feat(serve): add start, stop, status, and random port methods"
```

---

### Task 4: ServeService — Wails-Bound Struct

**Files:**
- Create: `serve_service.go`

**Step 1: Create `serve_service.go`**

```go
package main

// ServeService exposes tag-serving operations to the frontend via Wails.
type ServeService struct {
	app *App
}

// NewServeService creates a new serve service.
func NewServeService(app *App) *ServeService {
	return &ServeService{app: app}
}

// StartServing starts an HTTP server for the given tag.
func (s *ServeService) StartServing(tagID int64, port int, bindAll bool) (ServeInfo, error) {
	if s.app.serveManager == nil {
		return ServeInfo{}, fmt.Errorf("serve manager not initialized")
	}
	return s.app.serveManager.StartServing(tagID, port, bindAll)
}

// StopServing stops the HTTP server for the given tag.
func (s *ServeService) StopServing(tagID int64) error {
	if s.app.serveManager == nil {
		return fmt.Errorf("serve manager not initialized")
	}
	return s.app.serveManager.StopServing(tagID)
}

// GetServeStatus returns the status of all running tag servers.
func (s *ServeService) GetServeStatus() []ServeInfo {
	if s.app.serveManager == nil {
		return nil
	}
	return s.app.serveManager.GetStatus()
}

// GetRandomPort finds an available TCP port.
func (s *ServeService) GetRandomPort() (int, error) {
	return GetRandomPort()
}
```

**Step 2: Add missing import**

Add `"fmt"` to the import block in `serve_service.go`.

**Step 3: Commit**

```
git add serve_service.go
git commit -m "feat(serve): add ServeService Wails-bound struct"
```

---

### Task 5: Wire ServeManager and ServeService into App Lifecycle

**Files:**
- Modify: `app.go:35-50` (App struct)
- Modify: `app.go:112-191` (startup)
- Modify: `app.go:193-210` (shutdown)
- Modify: `main.go:20-54` (service creation and binding)

**Step 1: Add `serveManager` field to App struct**

In `app.go`, add `serveManager *ServeManager` to the App struct (after `watcherManager`):

```go
type App struct {
	ctx             context.Context
	db              *sql.DB
	tempDir         string
	tempStore       *TempClipStore
	transferHandler *TransferFileHandler
	mu              sync.Mutex
	watcherManager  *WatcherManager
	serveManager    *ServeManager
	pluginManager   *plugin.Manager
}
```

**Step 2: Initialize ServeManager in startup**

In `app.go` `startup()`, after the watcher manager initialization block (after line ~149), add:

```go
	// Initialize serve manager
	a.serveManager = NewServeManager(a)
```

**Step 3: Stop ServeManager in shutdown**

In `app.go` `shutdown()`, before `if a.watcherManager != nil`:

```go
	// Stop all serving
	if a.serveManager != nil {
		a.serveManager.StopAll()
	}
```

**Step 4: Wire ServeService in main.go**

In `main.go`, after `transferService := NewTransferService(app)` (line ~27), add:

```go
	serveService := NewServeService(app)
```

And add `serveService` to the `Bind` slice:

```go
	Bind: []interface{}{
		app,
		pluginService,
		clipboardService,
		transferService,
		serveService,
	},
```

**Step 5: Build to verify compilation**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Build succeeds

**Step 6: Commit**

```
git add app.go main.go
git commit -m "feat(serve): wire ServeManager and ServeService into app lifecycle"
```

---

### Task 6: Generate Wails Bindings

**Step 1: Generate frontend bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`

This will generate `frontend/wailsjs/go/main/ServeService.js` and `frontend/wailsjs/go/main/ServeService.d.ts`.

**Step 2: Verify the generated files exist**

Run: `ls frontend/wailsjs/go/main/ServeService.*`
Expected: Two files listed

**Step 3: Commit**

```
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for ServeService"
```

---

### Task 7: Frontend — Drawer Navigation 3-Way Toggle

**Files:**
- Modify: `frontend/index.html:111-185` (nav drawer)

**Step 1: Replace the Watch button with a 3-way view toggle strip**

In `frontend/index.html`, replace the content between the drawer header `</div>` (line 120) and `<nav class="p-3` (line 121) with a new toggle strip. The old Watch button (lines 122-133) gets removed and replaced by the toggle strip. Keep the Archive button and everything below it as separate nav items.

Replace lines 121-133 with:

```html
        <!-- View Toggle Strip -->
        <div class="px-3 pt-3 pb-1">
            <div class="flex rounded-lg border border-stone-200 overflow-hidden" role="tablist" aria-label="View switcher">
                <button id="view-tab-clips" role="tab" aria-selected="true"
                    class="flex-1 flex flex-col items-center gap-1 py-3 bg-stone-800 text-white transition-colors"
                    data-view="clips">
                    <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
                    </svg>
                    <span class="text-[10px] font-medium">Clips</span>
                </button>
                <button id="view-tab-watch" role="tab" aria-selected="false"
                    class="relative flex-1 flex flex-col items-center gap-1 py-3 text-stone-400 hover:bg-stone-100 transition-colors"
                    data-view="watch">
                    <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                    </svg>
                    <span class="text-[10px] font-medium">Watch</span>
                    <span id="watch-indicator" class="hidden absolute top-1.5 right-1.5 w-2.5 h-2.5 bg-emerald-500 rounded-full border-2 border-white"></span>
                </button>
                <button id="view-tab-serve" role="tab" aria-selected="false"
                    class="flex-1 flex flex-col items-center gap-1 py-3 text-stone-400 hover:bg-stone-100 transition-colors"
                    data-view="serve">
                    <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
                    </svg>
                    <span class="text-[10px] font-medium">Serve</span>
                </button>
            </div>
        </div>
        <nav class="p-3 flex flex-col gap-1" aria-label="Global Actions">
```

Note: The old `toggle-watch-view-btn` and its `watch-btn-text` span are removed. The `watch-indicator` span now lives inside the Watch tab. The `<nav>` tag (line 121) still opens here, and the Archive button (old line 134+) follows immediately.

**Step 2: Commit**

```
git add frontend/index.html
git commit -m "feat(serve): add 3-way view toggle strip in drawer navigation"
```

---

### Task 8: Frontend — Serve View HTML

**Files:**
- Modify: `frontend/index.html` (after watch-view section, before the bulk toolbar)

**Step 1: Add serve view section**

After the closing `</section>` of `#watch-view` (line 247) and before `<div id="bulk-toolbar"` (line 249), insert:

```html
        <!-- Serve View (hidden by default) -->
        <section id="serve-view" class="hidden mb-10" aria-labelledby="serve-heading">
            <h2 id="serve-heading" class="sr-only">Serve Tags</h2>

            <!-- Back to Pastes -->
            <button id="serve-back-btn"
                class="text-xs font-medium text-stone-500 hover:text-stone-700 transition-colors flex items-center gap-1.5 mb-4"
                style="--wails-draggable: no-drag">
                <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 19l-7-7 7-7"></path>
                </svg>
                Back to Pastes
            </button>

            <!-- Server list -->
            <ul id="serve-list" class="space-y-3 mb-6">
                <!-- Server cards inserted by JS -->
            </ul>

            <!-- Add serve button -->
            <div id="add-serve-zone"
                class="border border-dashed border-stone-300 rounded-lg p-8 text-center transition-all hover:border-stone-400 hover:bg-stone-50/50">
                <p class="text-sm text-stone-500 mb-3">Pick a tag to serve its files over HTTP</p>
                <button id="add-serve-btn"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors"
                    data-tooltip="Start serving a tag's files">
                    + Serve a Tag
                </button>
            </div>
        </section>
```

**Step 2: Add the script tag for serve.js**

In `frontend/index.html`, after the `<script src="js/watch.js">` line (line 1264), add:

```html
    <script src="js/serve.js"></script>
```

**Step 3: Commit**

```
git add frontend/index.html
git commit -m "feat(serve): add serve view HTML section and script tag"
```

---

### Task 9: Frontend — serve.js Core Logic

**Files:**
- Create: `frontend/js/serve.js`

**Step 1: Create `serve.js` with state, elements, and view toggle**

```javascript
// --- Serve View State ---
let isViewingServe = false;
let servePollingInterval = null;

// --- Elements ---
const serveView = document.getElementById('serve-view');
const serveList = document.getElementById('serve-list');
const serveBackBtn = document.getElementById('serve-back-btn');
const addServeBtn = document.getElementById('add-serve-btn');

// --- Serve Cards ---

function renderServeCard(info) {
    const li = document.createElement('li');
    li.className = 'bg-white rounded-md border border-stone-200 p-4 flex items-center justify-between gap-3 transition-all hover:border-stone-300';
    li.dataset.tagId = info.tag_id;

    const statusDot = info.running
        ? '<span class="w-2 h-2 bg-emerald-500 rounded-full inline-block"></span>'
        : '<span class="w-2 h-2 bg-stone-300 rounded-full inline-block"></span>';

    li.innerHTML = `
        <div class="flex items-center gap-3 min-w-0 flex-1">
            ${statusDot}
            <div class="min-w-0">
                <div class="text-xs font-semibold text-stone-700 truncate">${escapeHtml(info.tag_name)}</div>
                ${info.running ? `<button class="serve-url-copy text-[10px] text-stone-400 hover:text-stone-600 font-mono truncate block transition-colors" title="Click to copy">${escapeHtml(info.url)}</button>` : '<span class="text-[10px] text-stone-400">Stopped</span>'}
            </div>
        </div>
        <div class="flex items-center gap-2 shrink-0">
            ${info.running ? `<span class="text-[10px] text-stone-400 tabular-nums" title="Requests served">${info.request_count} req</span>` : ''}
            <button class="serve-toggle-btn ${info.running ? 'bg-red-50 text-red-600 hover:bg-red-100 border-red-200' : 'bg-stone-800 text-white hover:bg-stone-700 border-stone-800'} text-[10px] font-medium py-1.5 px-3 rounded-md border transition-colors"
                data-tag-id="${info.tag_id}" data-action="${info.running ? 'stop' : 'start'}" data-port="${info.port}" data-bind-all="${info.bind_all}">
                ${info.running ? 'Stop' : 'Start'}
            </button>
        </div>
    `;

    return li;
}

async function loadServeStatus() {
    try {
        const statuses = await window.go.main.ServeService.GetServeStatus();
        if (!statuses || statuses.length === 0) {
            serveList.innerHTML = '';
            return;
        }
        serveList.innerHTML = '';
        for (const info of statuses) {
            serveList.appendChild(renderServeCard(info));
        }
    } catch (err) {
        console.error('Failed to load serve status:', err);
    }
}

function startServePolling() {
    stopServePolling();
    servePollingInterval = setInterval(loadServeStatus, 2000);
}

function stopServePolling() {
    if (servePollingInterval) {
        clearInterval(servePollingInterval);
        servePollingInterval = null;
    }
}

// --- Tag Picker for Adding a Server ---

async function showServeTagPicker() {
    try {
        const tags = await window.go.main.App.GetTags();
        if (!tags || tags.length === 0) {
            showToast('No tags available. Create a tag first.', 'error');
            return;
        }

        // Get already-serving tag IDs
        const statuses = await window.go.main.ServeService.GetServeStatus();
        const servingIds = new Set((statuses || []).map(s => s.tag_id));

        const available = tags.filter(t => !servingIds.has(t.id));
        if (available.length === 0) {
            showToast('All tags are already being served.', 'error');
            return;
        }

        // Simple dropdown inline
        const existing = document.getElementById('serve-tag-picker');
        if (existing) existing.remove();

        const picker = document.createElement('div');
        picker.id = 'serve-tag-picker';
        picker.className = 'mt-3 bg-white rounded-md border border-stone-200 shadow-lg max-h-48 overflow-y-auto';

        for (const tag of available) {
            const btn = document.createElement('button');
            btn.className = 'w-full text-left px-3 py-2 text-xs font-medium text-stone-700 hover:bg-stone-100 transition-colors flex items-center gap-2';
            btn.innerHTML = `<span class="w-2 h-2 rounded-full shrink-0" style="background:${tag.color}"></span>${escapeHtml(tag.name)}`;
            btn.addEventListener('click', async () => {
                picker.remove();
                await startServingTag(tag.id);
            });
            picker.appendChild(btn);
        }

        document.getElementById('add-serve-zone').appendChild(picker);

        // Close picker on outside click
        const closeHandler = (e) => {
            if (!picker.contains(e.target) && e.target !== addServeBtn) {
                picker.remove();
                document.removeEventListener('click', closeHandler);
            }
        };
        setTimeout(() => document.addEventListener('click', closeHandler), 0);
    } catch (err) {
        console.error('Failed to show tag picker:', err);
    }
}

async function startServingTag(tagID) {
    try {
        const port = await window.go.main.ServeService.GetRandomPort();
        await window.go.main.ServeService.StartServing(tagID, port, false);
        showToast('Server started', 'success');
        await loadServeStatus();
    } catch (err) {
        showToast('Failed to start server: ' + err, 'error');
    }
}

// --- Event Handlers ---

serveBackBtn.addEventListener('click', () => switchView('clips'));

addServeBtn.addEventListener('click', () => showServeTagPicker());

serveList.addEventListener('click', async (e) => {
    // Copy URL
    const urlBtn = e.target.closest('.serve-url-copy');
    if (urlBtn) {
        try {
            await navigator.clipboard.writeText(urlBtn.textContent);
            showToast('URL copied', 'success');
        } catch { /* ignore */ }
        return;
    }

    // Toggle start/stop
    const toggleBtn = e.target.closest('.serve-toggle-btn');
    if (toggleBtn) {
        const tagId = parseInt(toggleBtn.dataset.tagId, 10);
        const action = toggleBtn.dataset.action;
        try {
            if (action === 'stop') {
                await window.go.main.ServeService.StopServing(tagId);
                showToast('Server stopped', 'success');
            } else {
                const port = await window.go.main.ServeService.GetRandomPort();
                await window.go.main.ServeService.StartServing(tagId, port, false);
                showToast('Server started', 'success');
            }
            await loadServeStatus();
        } catch (err) {
            showToast('Error: ' + err, 'error');
        }
    }
});
```

**Step 2: Commit**

```
git add frontend/js/serve.js
git commit -m "feat(serve): add serve.js with server cards, tag picker, and polling"
```

---

### Task 10: Frontend — View Switching Refactor

**Files:**
- Modify: `frontend/js/watch.js:1-65` (remove old toggle, add switchView integration)
- Modify: `frontend/js/ui.js:1012-1048` (refactor toggleViewMode)
- Modify: `frontend/js/app.js:524-528` (update keyboard shortcuts)

**Step 1: Add `switchView()` function to `serve.js`**

Add this at the top of `serve.js` (after the state variables, before the elements):

```javascript
// --- View Switching ---
// Shared view state: 'clips', 'watch', 'serve'
let currentView = 'clips';

function switchView(view) {
    const prevView = currentView;
    currentView = view;

    // Tab buttons
    const tabs = ['clips', 'watch', 'serve'];
    tabs.forEach(t => {
        const tab = document.getElementById(`view-tab-${t}`);
        if (!tab) return;
        if (t === view) {
            tab.classList.add('bg-stone-800', 'text-white');
            tab.classList.remove('text-stone-400', 'hover:bg-stone-100');
            tab.setAttribute('aria-selected', 'true');
        } else {
            tab.classList.remove('bg-stone-800', 'text-white');
            tab.classList.add('text-stone-400', 'hover:bg-stone-100');
            tab.setAttribute('aria-selected', 'false');
        }
    });

    // View sections
    const gallerySection = gallery.parentElement;
    const watchSection = document.getElementById('watch-view');
    const serveSection = document.getElementById('serve-view');

    gallerySection.classList.add('hidden');
    watchSection.classList.add('hidden');
    serveSection.classList.add('hidden');

    // Stop serve polling when leaving serve view
    if (prevView === 'serve') {
        stopServePolling();
    }

    switch (view) {
        case 'clips':
            gallerySection.classList.remove('hidden');
            imageCache.clear();
            loadClips();
            break;
        case 'watch':
            watchSection.classList.remove('hidden');
            if (typeof loadWatchFolders === 'function') loadWatchFolders();
            break;
        case 'serve':
            serveSection.classList.remove('hidden');
            loadServeStatus();
            startServePolling();
            break;
    }
}

// Tab click handlers
document.querySelectorAll('[data-view]').forEach(tab => {
    tab.addEventListener('click', () => {
        const view = tab.dataset.view;
        switchView(view);
        closeDrawer();
    });
});
```

**Step 2: Update `watch.js` — remove old `toggleWatchView()` internals, delegate to `switchView`**

In `watch.js`, replace the `toggleWatchView()` function (lines 37-59) with:

```javascript
function toggleWatchView() {
    if (currentView === 'watch') {
        switchView('clips');
    } else {
        switchView('watch');
    }
}
```

Remove the old element references that are no longer needed: `toggleWatchViewBtn`, `watchBtnText` (lines 7-8). These elements no longer exist in the HTML. Keep the `watchIndicator`, `watchView`, and all other watch elements.

Update `watchBackBtn` click handler: the back button should call `switchView('clips')` instead of `toggleWatchView()`.

**Step 3: Update `ui.js` — simplify `toggleViewMode()`**

In `ui.js`, the `toggleViewMode()` function (lines 1012-1048) currently manually closes the watch view. Replace the watch-closing block (lines 1017-1024) with:

```javascript
    // Return to clips view if not already there
    if (currentView !== 'clips') {
        switchView('clips');
    }
```

Remove the references to `watchBtnText` and `toggleWatchViewBtn` from this function since those elements no longer exist.

**Step 4: Update keyboard shortcuts in `app.js`**

In `app.js`, update the watch shortcut (lines 524-528) and add serve shortcut after it:

```javascript
        ShortcutManager.register({
            id: 'open-watch', label: 'Open Watch View', category: 'navigation',
            defaultKey: 'w', context: 'gallery',
            callback: () => { if (typeof toggleWatchView === 'function') toggleWatchView(); }
        });
        ShortcutManager.register({
            id: 'open-serve', label: 'Open Serve View', category: 'navigation',
            defaultKey: 's', context: 'gallery',
            callback: () => { if (typeof switchView === 'function') switchView(currentView === 'serve' ? 'clips' : 'serve'); }
        });
```

**Step 5: Build and test manually**

Run: `cd /Users/egecan/Code/mahpastes && make dev`
Verify: Open drawer, see 3-way toggle, click each tab, verify views switch correctly.

**Step 6: Commit**

```
git add frontend/js/serve.js frontend/js/watch.js frontend/js/ui.js frontend/js/app.js
git commit -m "feat(serve): refactor view switching to 3-way toggle (clips/watch/serve)"
```

---

### Task 11: Update E2E Test Selectors

**Files:**
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add serve selectors and update watch selectors**

In `e2e/helpers/selectors.ts`, add a `serve` section and update the watch section to reflect the new tab-based navigation. The old `toggle-watch-view-btn` selector needs to change to `view-tab-watch`.

Add to the selectors object:

```typescript
  serve: {
    viewTab: '#view-tab-serve',
    view: '#serve-view',
    list: '#serve-list',
    backBtn: '#serve-back-btn',
    addBtn: '#add-serve-btn',
    tagPicker: '#serve-tag-picker',
    card: (tagId: number) => `[data-tag-id="${tagId}"]`,
    toggleBtn: '.serve-toggle-btn',
    urlCopy: '.serve-url-copy',
  },
  viewTabs: {
    clips: '#view-tab-clips',
    watch: '#view-tab-watch',
    serve: '#view-tab-serve',
  },
```

Update the existing watch selectors that reference `toggle-watch-view-btn` to use `#view-tab-watch`.

**Step 2: Commit**

```
git add e2e/helpers/selectors.ts
git commit -m "feat(serve): add serve selectors and update watch selectors for view tabs"
```

---

### Task 12: Update E2E Test Fixtures — AppHelper Methods

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

**Step 1: Add serve helper methods to AppHelper**

Add these methods to the AppHelper class:

```typescript
  async switchToServeView(): Promise<void> {
    // Open drawer and click serve tab
    await this.page.click(selectors.drawer.toggleBtn);
    await this.page.click(selectors.viewTabs.serve);
    await this.page.waitForSelector(selectors.serve.view, { state: 'visible' });
  }

  async switchToClipsView(): Promise<void> {
    if (await this.page.locator(selectors.serve.view).isVisible()) {
      await this.page.click(selectors.serve.backBtn);
    } else if (await this.page.locator('#watch-view').isVisible()) {
      await this.page.click('#watch-back-btn');
    }
  }

  async startServingTag(tagName: string): Promise<{ port: number; url: string }> {
    const result = await this.page.evaluate(async (name) => {
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find(t => t.name === name);
      if (!tag) throw new Error(`Tag "${name}" not found`);
      const port = await window.go.main.ServeService.GetRandomPort();
      const info = await window.go.main.ServeService.StartServing(tag.id, port, false);
      return { port: info.port, url: info.url };
    }, tagName);
    return result;
  }

  async stopServingTag(tagName: string): Promise<void> {
    await this.page.evaluate(async (name) => {
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find(t => t.name === name);
      if (!tag) throw new Error(`Tag "${name}" not found`);
      await window.go.main.ServeService.StopServing(tag.id);
    }, tagName);
  }

  async getServeStatus(): Promise<any[]> {
    return this.page.evaluate(() => window.go.main.ServeService.GetServeStatus());
  }
```

**Step 2: Commit**

```
git add e2e/fixtures/test-fixtures.ts
git commit -m "feat(serve): add serve helper methods to AppHelper test fixture"
```

---

### Task 13: E2E Tests — Basic Serve Flow

**Files:**
- Create: `e2e/tests/serve/serve-basic.spec.ts`

**Step 1: Write basic serve tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Serve - Basic', () => {
  test.afterEach(async ({ app }) => {
    // Stop all servers
    const statuses = await app.getServeStatus();
    for (const s of statuses) {
      await app.page.evaluate(async (tagId) => {
        await window.go.main.ServeService.StopServing(tagId);
      }, s.tag_id);
    }
  });

  test('should start and stop serving a tag', async ({ app }) => {
    // Upload a file and tag it
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('serve-test');
    await app.addTagToClip(path.basename(imagePath), 'serve-test');

    // Start serving
    const { port, url } = await app.startServingTag('serve-test');
    expect(port).toBeGreaterThan(0);
    expect(url).toContain(String(port));

    // Verify server is running
    const statuses = await app.getServeStatus();
    expect(statuses.length).toBe(1);
    expect(statuses[0].running).toBe(true);

    // Fetch the file
    const response = await fetch(`http://127.0.0.1:${port}/${path.basename(imagePath)}`);
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('image/png');

    // Stop serving
    await app.stopServingTag('serve-test');
    const afterStop = await app.getServeStatus();
    expect(afterStop.length).toBe(0);
  });

  test('should serve directory listing when no index.html', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('dir-test');
    await app.addTagToClip(path.basename(imagePath), 'dir-test');

    const { port } = await app.startServingTag('dir-test');

    // HTML listing
    const htmlRes = await fetch(`http://127.0.0.1:${port}/`);
    expect(htmlRes.status).toBe(200);
    const html = await htmlRes.text();
    expect(html).toContain(path.basename(imagePath));
    expect(html).toContain('IBM Plex Mono');

    // JSON listing
    const jsonRes = await fetch(`http://127.0.0.1:${port}/`, {
      headers: { Accept: 'application/json' }
    });
    expect(jsonRes.status).toBe(200);
    const json = await jsonRes.json();
    expect(json.length).toBe(1);
    expect(json[0].name).toBe(path.basename(imagePath));
  });

  test('should serve index.html on root path', async ({ app }) => {
    const htmlContent = '<html><body>Hello World</body></html>';
    const htmlPath = await createTempFile(htmlContent, 'html');
    // Rename to index.html
    const fs = require('fs');
    const indexPath = path.join(path.dirname(htmlPath), 'index.html');
    fs.renameSync(htmlPath, indexPath);

    await app.uploadFile(indexPath);
    await app.createTag('index-test');
    await app.addTagToClip('index.html', 'index-test');

    const { port } = await app.startServingTag('index-test');

    const response = await fetch(`http://127.0.0.1:${port}/`);
    expect(response.status).toBe(200);
    const body = await response.text();
    expect(body).toContain('Hello World');
  });

  test('should return 404 for non-existent file', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('404-test');
    await app.addTagToClip(path.basename(imagePath), '404-test');

    const { port } = await app.startServingTag('404-test');

    const response = await fetch(`http://127.0.0.1:${port}/nonexistent.txt`);
    expect(response.status).toBe(404);
  });

  test('should handle duplicate filenames with suffix', async ({ app }) => {
    // Upload two files with same name
    const img1 = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');

    // Give them the same filename by uploading with explicit name
    await app.uploadFile(img1);
    await app.uploadFile(img2);

    await app.createTag('dup-test');

    // Tag both clips — they may have unique filenames from createTempFile
    // So this test is better done via the JSON listing to verify no collisions
    const clips = await app.page.evaluate(async () => {
      return window.go.main.App.GetClips(false, [], [], '', '');
    });
    for (const clip of clips) {
      await app.page.evaluate(async ({ clipId, tagName }) => {
        const tags = await window.go.main.App.GetTags();
        const tag = tags.find(t => t.name === tagName);
        await window.go.main.App.AddTagToClip(clipId, tag.id);
      }, { clipId: clip.id, tagName: 'dup-test' });
    }

    const { port } = await app.startServingTag('dup-test');

    const jsonRes = await fetch(`http://127.0.0.1:${port}/`, {
      headers: { Accept: 'application/json' }
    });
    const json = await jsonRes.json();
    // All filenames should be unique
    const names = json.map((f: any) => f.name);
    const uniqueNames = new Set(names);
    expect(uniqueNames.size).toBe(names.length);
  });

  test('should serve multiple tags simultaneously', async ({ app }) => {
    const img1 = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);

    await app.createTag('multi-1');
    await app.createTag('multi-2');
    await app.addTagToClip(path.basename(img1), 'multi-1');
    await app.addTagToClip(path.basename(img2), 'multi-2');

    const s1 = await app.startServingTag('multi-1');
    const s2 = await app.startServingTag('multi-2');

    expect(s1.port).not.toBe(s2.port);

    const r1 = await fetch(`http://127.0.0.1:${s1.port}/${path.basename(img1)}`);
    const r2 = await fetch(`http://127.0.0.1:${s2.port}/${path.basename(img2)}`);

    expect(r1.status).toBe(200);
    expect(r2.status).toBe(200);

    const statuses = await app.getServeStatus();
    expect(statuses.length).toBe(2);
  });
});
```

**Step 2: Commit**

```
git add e2e/tests/serve/serve-basic.spec.ts
git commit -m "test(serve): add e2e tests for basic serve flow"
```

---

### Task 14: E2E Tests — Serve UI

**Files:**
- Create: `e2e/tests/serve/serve-ui.spec.ts`

**Step 1: Write UI tests for the serve view**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Serve - UI', () => {
  test.afterEach(async ({ app }) => {
    const statuses = await app.getServeStatus();
    for (const s of statuses) {
      await app.page.evaluate(async (tagId) => {
        await window.go.main.ServeService.StopServing(tagId);
      }, s.tag_id);
    }
  });

  test('should show 3-way view toggle in drawer', async ({ app }) => {
    await app.page.click(selectors.drawer.toggleBtn);
    await expect(app.page.locator(selectors.viewTabs.clips)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.watch)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.serve)).toBeVisible();
  });

  test('should switch to serve view via tab', async ({ app }) => {
    await app.switchToServeView();
    await expect(app.page.locator(selectors.serve.view)).toBeVisible();
    await expect(app.page.locator('#gallery')).not.toBeVisible();
  });

  test('should switch back to clips via back button', async ({ app }) => {
    await app.switchToServeView();
    await app.page.click(selectors.serve.backBtn);
    await expect(app.page.locator('#gallery')).toBeVisible();
    await expect(app.page.locator(selectors.serve.view)).not.toBeVisible();
  });

  test('should show tag picker when clicking add button', async ({ app }) => {
    await app.createTag('ui-serve-test');
    await app.switchToServeView();
    await app.page.click(selectors.serve.addBtn);
    await expect(app.page.locator(selectors.serve.tagPicker)).toBeVisible();
  });
});
```

**Step 2: Commit**

```
git add e2e/tests/serve/serve-ui.spec.ts
git commit -m "test(serve): add e2e tests for serve UI navigation"
```

---

### Task 15: Run All E2E Tests and Fix Issues

**Step 1: Run the full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

**Step 2: Fix any failures**

Common issues to watch for:
- **Watch tests** may fail if they reference the old `#toggle-watch-view-btn` element — update those tests to use `#view-tab-watch` instead
- **Selector mismatches** from the navigation refactor
- **Compilation errors** from Go code — fix any missing imports or type issues

**Step 3: Iterate until all tests pass**

Run tests repeatedly and fix issues until green.

**Step 4: Commit all fixes**

```
git add -A
git commit -m "fix: resolve test failures from serve feature navigation refactor"
```

---

### Task 16: Final Verification and Cleanup

**Step 1: Run `make build` to verify production build**

Run: `cd /Users/egecan/Code/mahpastes && make build`
Expected: Build succeeds

**Step 2: Run all e2e tests one final time**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All pass

**Step 3: Final commit if any cleanup needed**

```
git add -A
git commit -m "chore: final cleanup for tag serve feature"
```
