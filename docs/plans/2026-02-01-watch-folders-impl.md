# Watch Folders Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add watched folder support to mahpastes - monitor folders for new files, import them as clips, and delete originals.

**Architecture:** Go backend handles file system watching via fsnotify, SQLite stores folder configs and settings. Frontend adds Watch button in header (before Archive), toggles a watch management view. File processing reuses existing UploadFiles logic with auto-archive option.

**Tech Stack:** Go 1.24, fsnotify, SQLite, Wails v2, Vanilla JavaScript, Tailwind CSS

---

## Task 1: Add fsnotify dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the fsnotify dependency**

Run: `go get github.com/fsnotify/fsnotify`

**Step 2: Verify go.mod updated**

Run: `grep fsnotify go.mod`
Expected: `github.com/fsnotify/fsnotify v1.x.x`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "Add fsnotify dependency for folder watching"
```

---

## Task 2: Create watched_folders table and settings

**Files:**
- Modify: `database.go`

**Step 1: Add table creation and migrations to initDB**

In `database.go`, after the existing migrations (around line 80), add:

```go
	// Create settings table
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)

	// Create watched_folders table
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS watched_folders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		filter_mode TEXT NOT NULL DEFAULT 'all',
		filter_presets TEXT,
		filter_regex TEXT,
		process_existing INTEGER DEFAULT 0,
		auto_archive INTEGER DEFAULT 0,
		is_paused INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Initialize global watch pause setting if not exists
	_, _ = db.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('global_watch_paused', 'false')`)
```

**Step 2: Verify the app builds**

Run: `go build`
Expected: No errors

**Step 3: Run app briefly to trigger migration**

Run: `wails dev &` then `sleep 5 && pkill -f wails`

Verify tables exist:
Run: `sqlite3 ~/Library/Application\ Support/mahpastes/clips.db ".tables"`
Expected: Output includes `watched_folders` and `settings`

**Step 4: Commit**

```bash
git add database.go
git commit -m "Add watched_folders and settings tables"
```

---

## Task 3: Create WatchedFolder type and CRUD methods

**Files:**
- Modify: `app.go`

**Step 1: Add WatchedFolder struct after existing types (around line 112)**

```go
// WatchedFolder represents a folder being watched for new files
type WatchedFolder struct {
	ID              int64     `json:"id"`
	Path            string    `json:"path"`
	FilterMode      string    `json:"filter_mode"`      // "all", "presets", "custom"
	FilterPresets   []string  `json:"filter_presets"`   // ["images", "videos", "documents"]
	FilterRegex     string    `json:"filter_regex"`     // regex pattern for custom mode
	ProcessExisting bool      `json:"process_existing"` // import existing files when added
	AutoArchive     bool      `json:"auto_archive"`     // archive imports immediately
	IsPaused        bool      `json:"is_paused"`        // per-folder pause
	CreatedAt       time.Time `json:"created_at"`
	Exists          bool      `json:"exists"` // whether folder path exists on disk
}

// WatchedFolderConfig for creating/updating watched folders
type WatchedFolderConfig struct {
	Path            string   `json:"path"`
	FilterMode      string   `json:"filter_mode"`
	FilterPresets   []string `json:"filter_presets"`
	FilterRegex     string   `json:"filter_regex"`
	ProcessExisting bool     `json:"process_existing"`
	AutoArchive     bool     `json:"auto_archive"`
}
```

**Step 2: Add GetWatchedFolders method**

```go
// GetWatchedFolders retrieves all watched folders
func (a *App) GetWatchedFolders() ([]WatchedFolder, error) {
	rows, err := a.db.Query(`
		SELECT id, path, filter_mode, filter_presets, filter_regex,
		       process_existing, auto_archive, is_paused, created_at
		FROM watched_folders
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query watched folders: %w", err)
	}
	defer rows.Close()

	var folders []WatchedFolder
	for rows.Next() {
		var f WatchedFolder
		var filterPresets sql.NullString
		var filterRegex sql.NullString
		var processExisting, autoArchive, isPaused int

		if err := rows.Scan(&f.ID, &f.Path, &f.FilterMode, &filterPresets, &filterRegex,
			&processExisting, &autoArchive, &isPaused, &f.CreatedAt); err != nil {
			log.Printf("Failed to scan watched folder: %v", err)
			continue
		}

		f.ProcessExisting = processExisting == 1
		f.AutoArchive = autoArchive == 1
		f.IsPaused = isPaused == 1
		f.FilterRegex = filterRegex.String

		// Parse filter presets JSON
		if filterPresets.Valid && filterPresets.String != "" {
			_ = json.Unmarshal([]byte(filterPresets.String), &f.FilterPresets)
		}
		if f.FilterPresets == nil {
			f.FilterPresets = []string{}
		}

		// Check if folder exists
		if _, err := os.Stat(f.Path); err == nil {
			f.Exists = true
		}

		folders = append(folders, f)
	}

	if folders == nil {
		folders = []WatchedFolder{}
	}
	return folders, nil
}
```

**Step 3: Add AddWatchedFolder method**

```go
// AddWatchedFolder adds a new folder to watch
func (a *App) AddWatchedFolder(config WatchedFolderConfig) (*WatchedFolder, error) {
	// Validate path exists
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		return nil, fmt.Errorf("folder does not exist: %s", config.Path)
	}

	// Default filter mode
	if config.FilterMode == "" {
		config.FilterMode = "all"
	}

	// Serialize presets to JSON
	var presetsJSON []byte
	if len(config.FilterPresets) > 0 {
		presetsJSON, _ = json.Marshal(config.FilterPresets)
	}

	result, err := a.db.Exec(`
		INSERT INTO watched_folders (path, filter_mode, filter_presets, filter_regex, process_existing, auto_archive)
		VALUES (?, ?, ?, ?, ?, ?)
	`, config.Path, config.FilterMode, string(presetsJSON), config.FilterRegex,
		boolToInt(config.ProcessExisting), boolToInt(config.AutoArchive))
	if err != nil {
		return nil, fmt.Errorf("failed to add watched folder: %w", err)
	}

	id, _ := result.LastInsertId()

	return &WatchedFolder{
		ID:              id,
		Path:            config.Path,
		FilterMode:      config.FilterMode,
		FilterPresets:   config.FilterPresets,
		FilterRegex:     config.FilterRegex,
		ProcessExisting: config.ProcessExisting,
		AutoArchive:     config.AutoArchive,
		IsPaused:        false,
		Exists:          true,
	}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

**Step 4: Add UpdateWatchedFolder method**

```go
// UpdateWatchedFolder updates an existing watched folder config
func (a *App) UpdateWatchedFolder(id int64, config WatchedFolderConfig) error {
	var presetsJSON []byte
	if len(config.FilterPresets) > 0 {
		presetsJSON, _ = json.Marshal(config.FilterPresets)
	}

	_, err := a.db.Exec(`
		UPDATE watched_folders
		SET filter_mode = ?, filter_presets = ?, filter_regex = ?, auto_archive = ?
		WHERE id = ?
	`, config.FilterMode, string(presetsJSON), config.FilterRegex,
		boolToInt(config.AutoArchive), id)
	if err != nil {
		return fmt.Errorf("failed to update watched folder: %w", err)
	}
	return nil
}
```

**Step 5: Add RemoveWatchedFolder method**

```go
// RemoveWatchedFolder removes a watched folder
func (a *App) RemoveWatchedFolder(id int64) error {
	_, err := a.db.Exec("DELETE FROM watched_folders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove watched folder: %w", err)
	}
	return nil
}
```

**Step 6: Add pause control methods**

```go
// GetGlobalWatchPaused returns whether global watching is paused
func (a *App) GetGlobalWatchPaused() bool {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = 'global_watch_paused'").Scan(&value)
	if err != nil {
		return false
	}
	return value == "true"
}

// SetGlobalWatchPaused sets the global watch pause state
func (a *App) SetGlobalWatchPaused(paused bool) error {
	value := "false"
	if paused {
		value = "true"
	}
	_, err := a.db.Exec("UPDATE settings SET value = ? WHERE key = 'global_watch_paused'", value)
	return err
}

// SetFolderPaused sets the pause state for a specific folder
func (a *App) SetFolderPaused(id int64, paused bool) error {
	_, err := a.db.Exec("UPDATE watched_folders SET is_paused = ? WHERE id = ?", boolToInt(paused), id)
	return err
}
```

**Step 7: Verify build**

Run: `go build`
Expected: No errors

**Step 8: Commit**

```bash
git add app.go
git commit -m "Add WatchedFolder CRUD and pause control methods"
```

---

## Task 4: Add SelectFolder dialog method

**Files:**
- Modify: `app.go`

**Step 1: Add SelectFolder method to show native folder picker**

```go
// SelectFolder opens a native folder picker dialog
func (a *App) SelectFolder() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Folder to Watch",
	})
	if err != nil {
		return "", fmt.Errorf("failed to open folder dialog: %w", err)
	}
	return path, nil
}
```

**Step 2: Verify build**

Run: `go build`
Expected: No errors

**Step 3: Commit**

```bash
git add app.go
git commit -m "Add SelectFolder dialog method"
```

---

## Task 5: Create watcher.go with WatcherManager

**Files:**
- Create: `watcher.go`

**Step 1: Create watcher.go with WatcherManager struct and preset definitions**

```go
package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// File extension presets
var presetExtensions = map[string][]string{
	"images":    {".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".bmp", ".tiff", ".svg"},
	"documents": {".pdf", ".doc", ".docx", ".txt", ".md", ".rtf", ".odt", ".xls", ".xlsx"},
	"videos":    {".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v", ".wmv"},
}

// WatcherManager handles file system watching for all folders
type WatcherManager struct {
	watcher      *fsnotify.Watcher
	app          *App
	activeWatches map[int64]string // folderID -> path
	debounceMap  map[string]*time.Timer // path -> debounce timer
	mu           sync.RWMutex
	running      bool
}

// NewWatcherManager creates a new watcher manager
func NewWatcherManager(app *App) (*WatcherManager, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &WatcherManager{
		watcher:       watcher,
		app:           app,
		activeWatches: make(map[int64]string),
		debounceMap:   make(map[string]*time.Timer),
	}, nil
}

// Start begins watching all non-paused folders
func (w *WatcherManager) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	// Start event handler goroutine
	go w.handleEvents()

	// Load and start watching folders
	return w.refreshWatches()
}

// Stop stops all watching
func (w *WatcherManager) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.running = false

	// Cancel all debounce timers
	for _, timer := range w.debounceMap {
		timer.Stop()
	}
	w.debounceMap = make(map[string]*time.Timer)

	// Close watcher
	if w.watcher != nil {
		w.watcher.Close()
	}
}

// RefreshWatches reloads watched folders from DB and updates active watches
func (w *WatcherManager) refreshWatches() error {
	globalPaused := w.app.GetGlobalWatchPaused()

	folders, err := w.app.GetWatchedFolders()
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Build set of folders that should be watched
	shouldWatch := make(map[int64]string)
	for _, f := range folders {
		if !globalPaused && !f.IsPaused && f.Exists {
			shouldWatch[f.ID] = f.Path
		}
	}

	// Remove watches that should no longer be active
	for id, path := range w.activeWatches {
		if _, ok := shouldWatch[id]; !ok {
			w.watcher.Remove(path)
			delete(w.activeWatches, id)
			log.Printf("Stopped watching: %s", path)
		}
	}

	// Add watches for new folders
	for id, path := range shouldWatch {
		if _, ok := w.activeWatches[id]; !ok {
			if err := w.watcher.Add(path); err != nil {
				log.Printf("Failed to watch %s: %v", path, err)
				continue
			}
			w.activeWatches[id] = path
			log.Printf("Started watching: %s", path)
		}
	}

	return nil
}

// handleEvents processes fsnotify events
func (w *WatcherManager) handleEvents() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Only handle Create and Write events
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}

			// Skip directories
			info, err := os.Stat(event.Name)
			if err != nil || info.IsDir() {
				continue
			}

			// Skip hidden files
			if strings.HasPrefix(filepath.Base(event.Name), ".") {
				continue
			}

			// Debounce: wait 500ms after last event for this file
			w.debounceFile(event.Name)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// debounceFile delays processing until file is stable
func (w *WatcherManager) debounceFile(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Cancel existing timer if any
	if timer, ok := w.debounceMap[path]; ok {
		timer.Stop()
	}

	// Set new timer
	w.debounceMap[path] = time.AfterFunc(500*time.Millisecond, func() {
		w.mu.Lock()
		delete(w.debounceMap, path)
		w.mu.Unlock()

		w.processFile(path)
	})
}

// processFile handles a new file in a watched folder
func (w *WatcherManager) processFile(filePath string) {
	dir := filepath.Dir(filePath)

	// Find which folder config this belongs to
	w.mu.RLock()
	var folderID int64
	for id, path := range w.activeWatches {
		if path == dir {
			folderID = id
			break
		}
	}
	w.mu.RUnlock()

	if folderID == 0 {
		return
	}

	// Get folder config
	folders, err := w.app.GetWatchedFolders()
	if err != nil {
		log.Printf("Failed to get folder config: %v", err)
		return
	}

	var folder *WatchedFolder
	for _, f := range folders {
		if f.ID == folderID {
			folder = &f
			break
		}
	}

	if folder == nil {
		return
	}

	// Check filter
	if !w.matchesFilter(filePath, folder) {
		log.Printf("File does not match filter: %s", filePath)
		return
	}

	// Import the file
	if err := w.importFile(filePath, folder); err != nil {
		log.Printf("Failed to import file %s: %v", filePath, err)
		// Emit event to frontend for toast notification
		w.app.emitWatchError(filePath, err.Error())
		return
	}

	// Delete original file
	if err := os.Remove(filePath); err != nil {
		log.Printf("Failed to delete original file %s: %v", filePath, err)
	}

	log.Printf("Successfully imported and removed: %s", filePath)
}

// matchesFilter checks if a file matches the folder's filter settings
func (w *WatcherManager) matchesFilter(filePath string, folder *WatchedFolder) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch folder.FilterMode {
	case "all":
		return true

	case "presets":
		for _, preset := range folder.FilterPresets {
			if extensions, ok := presetExtensions[preset]; ok {
				for _, e := range extensions {
					if ext == e {
						return true
					}
				}
			}
		}
		return false

	case "custom":
		if folder.FilterRegex == "" {
			return true
		}
		re, err := regexp.Compile(folder.FilterRegex)
		if err != nil {
			log.Printf("Invalid regex %s: %v", folder.FilterRegex, err)
			return false
		}
		return re.MatchString(filepath.Base(filePath))

	default:
		return true
	}
}

// importFile reads a file and imports it as a clip
func (w *WatcherManager) importFile(filePath string, folder *WatchedFolder) error {
	fileData, err := w.app.ReadFileFromPath(filePath)
	if err != nil {
		return err
	}

	// Upload with no expiration
	if err := w.app.UploadFiles([]FileData{*fileData}, 0); err != nil {
		return err
	}

	// Auto-archive if configured
	if folder.AutoArchive {
		// Get the most recently added clip and archive it
		clips, err := w.app.GetClips(false)
		if err == nil && len(clips) > 0 {
			w.app.ToggleArchive(clips[0].ID)
		}
	}

	return nil
}
```

**Step 2: Verify build**

Run: `go build`
Expected: No errors

**Step 3: Commit**

```bash
git add watcher.go
git commit -m "Add WatcherManager with fsnotify integration"
```

---

## Task 6: Add emitWatchError and integrate WatcherManager into App

**Files:**
- Modify: `app.go`

**Step 1: Add watcherManager field to App struct**

Update the App struct (around line 24):

```go
type App struct {
	ctx            context.Context
	db             *sql.DB
	tempDir        string
	mu             sync.Mutex
	watcherManager *WatcherManager
}
```

**Step 2: Add emitWatchError method**

```go
// emitWatchError sends an error event to the frontend
func (a *App) emitWatchError(filePath string, errMsg string) {
	runtime.EventsEmit(a.ctx, "watch:error", map[string]string{
		"file":  filepath.Base(filePath),
		"error": errMsg,
	})
}

// emitWatchImport sends an import event to the frontend
func (a *App) emitWatchImport(filename string) {
	runtime.EventsEmit(a.ctx, "watch:import", filename)
}
```

**Step 3: Initialize WatcherManager in startup (update startup function)**

In the `startup` function, after clipboard.Init(), add:

```go
	// Initialize watcher manager
	wm, err := NewWatcherManager(a)
	if err != nil {
		log.Printf("Warning: Failed to initialize watcher manager: %v", err)
	} else {
		a.watcherManager = wm
		if err := wm.Start(); err != nil {
			log.Printf("Warning: Failed to start watcher: %v", err)
		}
	}
```

**Step 4: Stop WatcherManager in shutdown (update shutdown function)**

In the `shutdown` function, before closing db:

```go
	// Stop watcher
	if a.watcherManager != nil {
		a.watcherManager.Stop()
	}
```

**Step 5: Add RefreshWatches method to expose to frontend**

```go
// RefreshWatches reloads the watcher configuration
func (a *App) RefreshWatches() error {
	if a.watcherManager != nil {
		return a.watcherManager.refreshWatches()
	}
	return nil
}
```

**Step 6: Add import for filepath at top of app.go if not present**

Verify imports include `"path/filepath"`

**Step 7: Verify build**

Run: `go build`
Expected: No errors

**Step 8: Commit**

```bash
git add app.go
git commit -m "Integrate WatcherManager into App lifecycle"
```

---

## Task 7: Update watcher.go to emit import events

**Files:**
- Modify: `watcher.go`

**Step 1: Update importFile to emit event after successful import**

In the `importFile` method, after the UploadFiles call succeeds and before auto-archive logic:

```go
	// Emit import event for UI refresh
	w.app.emitWatchImport(fileData.Name)
```

**Step 2: Verify build**

Run: `go build`
Expected: No errors

**Step 3: Commit**

```bash
git add watcher.go
git commit -m "Emit import events from watcher for UI refresh"
```

---

## Task 8: Add process existing files method

**Files:**
- Modify: `watcher.go`

**Step 1: Add ProcessExistingFiles method**

```go
// ProcessExistingFiles imports all existing files in a watched folder
func (w *WatcherManager) ProcessExistingFiles(folderID int64) error {
	folders, err := w.app.GetWatchedFolders()
	if err != nil {
		return err
	}

	var folder *WatchedFolder
	for _, f := range folders {
		if f.ID == folderID {
			folder = &f
			break
		}
	}

	if folder == nil {
		return fmt.Errorf("folder not found")
	}

	entries, err := os.ReadDir(folder.Path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		filePath := filepath.Join(folder.Path, name)

		if !w.matchesFilter(filePath, folder) {
			continue
		}

		if err := w.importFile(filePath, folder); err != nil {
			log.Printf("Failed to import existing file %s: %v", filePath, err)
			w.app.emitWatchError(filePath, err.Error())
			continue
		}

		if err := os.Remove(filePath); err != nil {
			log.Printf("Failed to delete file %s: %v", filePath, err)
		}
	}

	return nil
}
```

**Step 2: Add import for fmt at top if not present**

**Step 3: Add app method to call ProcessExistingFiles**

In `app.go`, add:

```go
// ProcessExistingFilesInFolder processes existing files in a watched folder
func (a *App) ProcessExistingFilesInFolder(folderID int64) error {
	if a.watcherManager != nil {
		return a.watcherManager.ProcessExistingFiles(folderID)
	}
	return nil
}
```

**Step 4: Verify build**

Run: `go build`
Expected: No errors

**Step 5: Commit**

```bash
git add watcher.go app.go
git commit -m "Add ProcessExistingFiles method for watched folders"
```

---

## Task 9: Add GetWatchStatus method

**Files:**
- Modify: `app.go`

**Step 1: Add WatchStatus type and GetWatchStatus method**

```go
// WatchStatus represents the current watching state
type WatchStatus struct {
	GlobalPaused  bool `json:"global_paused"`
	ActiveCount   int  `json:"active_count"`
	TotalCount    int  `json:"total_count"`
	IsWatching    bool `json:"is_watching"` // true if any folder is actively being watched
}

// GetWatchStatus returns the current watch status
func (a *App) GetWatchStatus() WatchStatus {
	globalPaused := a.GetGlobalWatchPaused()
	folders, _ := a.GetWatchedFolders()

	activeCount := 0
	for _, f := range folders {
		if !f.IsPaused && f.Exists {
			activeCount++
		}
	}

	return WatchStatus{
		GlobalPaused: globalPaused,
		ActiveCount:  activeCount,
		TotalCount:   len(folders),
		IsWatching:   !globalPaused && activeCount > 0,
	}
}
```

**Step 2: Verify build**

Run: `go build`
Expected: No errors

**Step 3: Commit**

```bash
git add app.go
git commit -m "Add GetWatchStatus method for UI status indicator"
```

---

## Task 10: Add Watch button to header HTML

**Files:**
- Modify: `frontend/index.html`

**Step 1: Add Watch button before Archive button (around line 36)**

Replace the nav element with:

```html
            <nav class="flex gap-2" aria-label="Global Actions">
                <button id="toggle-watch-view-btn"
                    class="relative border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors flex items-center"
                    aria-pressed="false">
                    <svg class="w-4 h-4 mr-1.5 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"
                        xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"></path>
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"></path>
                    </svg>
                    <span id="watch-btn-text">Watch</span>
                    <span id="watch-indicator" class="hidden absolute -top-1 -right-1 w-2.5 h-2.5 bg-emerald-500 rounded-full border-2 border-stone-50"></span>
                </button>
                <button id="toggle-archive-view-btn"
                    class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors flex items-center"
                    aria-pressed="false">
                    <svg class="w-4 h-4 mr-1.5 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"
                        xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4">
                        </path>
                    </svg>
                    <span id="archive-btn-text">Archive</span>
                </button>
                <button id="delete-all-temp-btn"
                    class="border border-stone-200 hover:border-red-300 hover:bg-red-50 text-stone-500 hover:text-red-600 text-xs font-medium py-2 px-3 rounded-md transition-colors flex items-center">
                    <svg class="w-4 h-4 mr-1.5 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                    Clear All
                </button>
            </nav>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "Add Watch button to header with status indicator"
```

---

## Task 11: Add Watch view section to HTML

**Files:**
- Modify: `frontend/index.html`

**Step 1: Add watch view section after upload section (around line 106)**

After the closing `</section>` of upload-section and before bulk-toolbar div:

```html
        <!-- Watch View (hidden by default) -->
        <section id="watch-view" class="hidden mb-10" aria-labelledby="watch-heading">
            <h2 id="watch-heading" class="sr-only">Watched Folders</h2>

            <!-- Global controls -->
            <div class="flex items-center justify-between mb-6 pb-4 border-b border-stone-200">
                <div class="flex items-center gap-3">
                    <label class="relative inline-flex items-center cursor-pointer">
                        <input type="checkbox" id="global-watch-toggle" class="sr-only peer">
                        <div class="w-9 h-5 bg-stone-300 peer-focus:ring-2 peer-focus:ring-stone-400/30 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-emerald-500"></div>
                    </label>
                    <span id="global-watch-label" class="text-sm font-medium text-stone-600">Watching paused</span>
                </div>
                <span id="watch-folder-count" class="text-xs text-stone-400">0 folders</span>
            </div>

            <!-- Folder list -->
            <ul id="watch-folder-list" class="space-y-3 mb-6">
                <!-- Folder cards inserted by JS -->
            </ul>

            <!-- Add folder zone -->
            <div id="add-folder-zone" tabindex="0" role="button"
                aria-label="Add folder to watch by dragging or clicking"
                class="border border-dashed border-stone-300 rounded-lg p-8 text-center cursor-pointer transition-all hover:border-stone-400 hover:bg-stone-50/50 focus:outline-none focus:ring-2 focus:ring-stone-400/30">
                <p class="text-sm text-stone-500 mb-3">Drop a folder here or</p>
                <button id="add-folder-btn"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    + Add Folder
                </button>
                <p class="text-[10px] text-stone-400 mt-3">Files in subfolders are not watched</p>
            </div>
        </section>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "Add Watch view section HTML"
```

---

## Task 12: Add watched folder modal to HTML

**Files:**
- Modify: `frontend/index.html`

**Step 1: Add folder config modal (before the Scripts section, around line 455)**

```html
    <!-- Add/Edit Folder Modal -->
    <div id="folder-modal" role="dialog" aria-modal="true" aria-labelledby="folder-modal-title"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-stone-900/40 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none">
        <div class="bg-white rounded-lg shadow-xl max-w-md w-full overflow-hidden transform transition-transform duration-200 scale-95">
            <div class="p-5 border-b border-stone-100">
                <h2 id="folder-modal-title" class="text-sm font-semibold text-stone-800">Add Watched Folder</h2>
            </div>

            <div class="p-5 space-y-5">
                <!-- Folder path -->
                <div>
                    <label class="block text-[10px] font-medium text-stone-500 uppercase tracking-wider mb-1.5">Folder</label>
                    <p id="folder-modal-path" class="text-sm text-stone-700 font-mono bg-stone-50 px-3 py-2 rounded-md truncate"></p>
                </div>

                <!-- Filter type -->
                <div>
                    <label class="block text-[10px] font-medium text-stone-500 uppercase tracking-wider mb-2">File Types</label>
                    <div class="space-y-2">
                        <label class="flex items-center gap-2 cursor-pointer">
                            <input type="checkbox" id="filter-all" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                            <span class="text-sm text-stone-600">All files</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                            <input type="checkbox" id="filter-images" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                            <span class="text-sm text-stone-600">Images</span>
                            <span class="text-[10px] text-stone-400">(jpg, png, gif, webp...)</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                            <input type="checkbox" id="filter-documents" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                            <span class="text-sm text-stone-600">Documents</span>
                            <span class="text-[10px] text-stone-400">(pdf, doc, txt, md...)</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                            <input type="checkbox" id="filter-videos" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                            <span class="text-sm text-stone-600">Videos</span>
                            <span class="text-[10px] text-stone-400">(mp4, mov, avi, mkv...)</span>
                        </label>
                    </div>
                </div>

                <!-- Custom regex -->
                <div id="custom-regex-section">
                    <label class="block text-[10px] font-medium text-stone-500 uppercase tracking-wider mb-1.5">Custom Filter (regex)</label>
                    <input type="text" id="filter-regex"
                        class="w-full px-3 py-2 border border-stone-200 rounded-md text-sm focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20"
                        placeholder="e.g., .*\\.png$">
                </div>

                <!-- Options -->
                <div class="space-y-3 pt-2 border-t border-stone-100">
                    <label class="flex items-center gap-3 cursor-pointer">
                        <input type="checkbox" id="process-existing" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                        <div>
                            <span class="text-sm text-stone-600">Process existing files now</span>
                            <p class="text-[10px] text-stone-400">Import files already in this folder</p>
                        </div>
                    </label>
                    <label class="flex items-center gap-3 cursor-pointer">
                        <input type="checkbox" id="auto-archive" class="w-4 h-4 rounded border-stone-300 text-stone-700 focus:ring-stone-500">
                        <div>
                            <span class="text-sm text-stone-600">Auto-archive imported files</span>
                            <p class="text-[10px] text-stone-400">Move imports directly to archive</p>
                        </div>
                    </label>
                </div>
            </div>

            <div class="bg-stone-50 px-5 py-3 flex gap-2 justify-end border-t border-stone-100">
                <button id="folder-modal-cancel"
                    class="bg-white border border-stone-200 hover:bg-stone-50 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Cancel
                </button>
                <button id="folder-modal-save"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Add Folder
                </button>
            </div>
        </div>
    </div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "Add watched folder configuration modal"
```

---

## Task 13: Create watch.js with core functionality

**Files:**
- Create: `frontend/js/watch.js`

**Step 1: Create watch.js with state and view management**

```javascript
// --- Watch View State ---
let isViewingWatch = false;
let watchFolders = [];
let editingFolderId = null;

// --- Elements ---
const toggleWatchViewBtn = document.getElementById('toggle-watch-view-btn');
const watchBtnText = document.getElementById('watch-btn-text');
const watchIndicator = document.getElementById('watch-indicator');
const watchView = document.getElementById('watch-view');
const watchFolderList = document.getElementById('watch-folder-list');
const globalWatchToggle = document.getElementById('global-watch-toggle');
const globalWatchLabel = document.getElementById('global-watch-label');
const watchFolderCount = document.getElementById('watch-folder-count');
const addFolderZone = document.getElementById('add-folder-zone');
const addFolderBtn = document.getElementById('add-folder-btn');

// Folder modal elements
const folderModal = document.getElementById('folder-modal');
const folderModalTitle = document.getElementById('folder-modal-title');
const folderModalPath = document.getElementById('folder-modal-path');
const filterAll = document.getElementById('filter-all');
const filterImages = document.getElementById('filter-images');
const filterDocuments = document.getElementById('filter-documents');
const filterVideos = document.getElementById('filter-videos');
const filterRegex = document.getElementById('filter-regex');
const processExisting = document.getElementById('process-existing');
const autoArchive = document.getElementById('auto-archive');
const folderModalCancel = document.getElementById('folder-modal-cancel');
const folderModalSave = document.getElementById('folder-modal-save');

// --- View Toggle ---
function toggleWatchView() {
    isViewingWatch = !isViewingWatch;
    toggleWatchViewBtn.setAttribute('aria-pressed', isViewingWatch);

    if (isViewingWatch) {
        // Switch to watch view
        watchBtnText.textContent = 'Clips';
        toggleWatchViewBtn.classList.add('bg-stone-800', 'text-white', 'border-stone-800');
        toggleWatchViewBtn.classList.remove('border-stone-200', 'text-stone-600');

        uploadSection.classList.add('hidden');
        gallery.parentElement.classList.add('hidden');
        watchView.classList.remove('hidden');

        loadWatchFolders();
    } else {
        // Switch back to clips view
        watchBtnText.textContent = 'Watch';
        toggleWatchViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleWatchViewBtn.classList.add('border-stone-200', 'text-stone-600');

        uploadSection.classList.remove('hidden');
        gallery.parentElement.classList.remove('hidden');
        watchView.classList.add('hidden');
    }
}

// --- Load Watch Status ---
async function updateWatchIndicator() {
    try {
        const status = await window.go.main.App.GetWatchStatus();
        if (status.is_watching) {
            watchIndicator.classList.remove('hidden');
        } else {
            watchIndicator.classList.add('hidden');
        }
    } catch (error) {
        console.error('Failed to get watch status:', error);
    }
}

// --- Load Folders ---
async function loadWatchFolders() {
    try {
        const globalPaused = await window.go.main.App.GetGlobalWatchPaused();
        watchFolders = await window.go.main.App.GetWatchedFolders();

        // Update global toggle
        globalWatchToggle.checked = !globalPaused;
        globalWatchLabel.textContent = globalPaused ? 'Watching paused' : 'Watching active';

        // Update count
        const activeCount = watchFolders.filter(f => !f.is_paused && f.exists).length;
        watchFolderCount.textContent = `${watchFolders.length} folder${watchFolders.length !== 1 ? 's' : ''}`;

        // Render folder cards
        renderWatchFolderList();

        // Update indicator
        updateWatchIndicator();
    } catch (error) {
        console.error('Failed to load watch folders:', error);
    }
}

// --- Render Folder List ---
function renderWatchFolderList() {
    watchFolderList.innerHTML = '';

    if (watchFolders.length === 0) {
        watchFolderList.innerHTML = '<li class="text-center text-sm text-stone-400 py-8">No watched folders yet</li>';
        return;
    }

    for (const folder of watchFolders) {
        const card = createWatchFolderCard(folder);
        watchFolderList.appendChild(card);
    }
}

// --- Create Folder Card ---
function createWatchFolderCard(folder) {
    const li = document.createElement('li');
    li.className = 'bg-white border border-stone-200 rounded-lg p-4 flex items-center justify-between gap-4';
    li.dataset.id = folder.id;

    // Filter description
    let filterDesc = 'All files';
    if (folder.filter_mode === 'presets' && folder.filter_presets?.length > 0) {
        filterDesc = folder.filter_presets.map(p => p.charAt(0).toUpperCase() + p.slice(1)).join(', ');
    } else if (folder.filter_mode === 'custom' && folder.filter_regex) {
        filterDesc = `Regex: ${folder.filter_regex}`;
    }

    const pausedClass = folder.is_paused ? 'opacity-50' : '';
    const notExistsWarning = !folder.exists
        ? '<span class="text-[10px] text-amber-600 bg-amber-50 px-1.5 py-0.5 rounded">Folder not found</span>'
        : '';

    li.innerHTML = `
        <div class="flex-1 min-w-0 ${pausedClass}">
            <div class="flex items-center gap-2 mb-1">
                <p class="text-sm font-medium text-stone-700 truncate">${escapeHTML(folder.path)}</p>
                ${notExistsWarning}
            </div>
            <p class="text-[11px] text-stone-400">
                ${filterDesc}
                ${folder.auto_archive ? ' • Auto-archive' : ''}
                ${folder.is_paused ? ' • <span class="text-amber-500">Paused</span>' : ''}
            </p>
        </div>
        <div class="flex items-center gap-1">
            <button class="p-2 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded-md transition-colors"
                    data-action="toggle-pause" title="${folder.is_paused ? 'Resume' : 'Pause'}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    ${folder.is_paused
                        ? '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
                        : '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
                    }
                </svg>
            </button>
            <button class="p-2 text-stone-400 hover:text-red-500 hover:bg-red-50 rounded-md transition-colors"
                    data-action="remove" title="Remove">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
    `;

    // Event listeners
    li.querySelector('[data-action="toggle-pause"]').addEventListener('click', () => toggleFolderPause(folder.id, !folder.is_paused));
    li.querySelector('[data-action="remove"]').addEventListener('click', () => removeWatchFolder(folder.id));

    return li;
}

// --- Toggle Folder Pause ---
async function toggleFolderPause(id, paused) {
    try {
        await window.go.main.App.SetFolderPaused(id, paused);
        await window.go.main.App.RefreshWatches();
        loadWatchFolders();
    } catch (error) {
        console.error('Failed to toggle folder pause:', error);
        showToast('Failed to update folder');
    }
}

// --- Remove Folder ---
async function removeWatchFolder(id) {
    showConfirmDialog('Remove Folder', 'Stop watching this folder?', async () => {
        try {
            await window.go.main.App.RemoveWatchedFolder(id);
            await window.go.main.App.RefreshWatches();
            loadWatchFolders();
            showToast('Folder removed');
        } catch (error) {
            console.error('Failed to remove folder:', error);
            showToast('Failed to remove folder');
        }
    });
}

// --- Global Pause Toggle ---
async function toggleGlobalPause() {
    const paused = !globalWatchToggle.checked;
    try {
        await window.go.main.App.SetGlobalWatchPaused(paused);
        await window.go.main.App.RefreshWatches();
        globalWatchLabel.textContent = paused ? 'Watching paused' : 'Watching active';
        updateWatchIndicator();
    } catch (error) {
        console.error('Failed to toggle global pause:', error);
        showToast('Failed to update watch status');
        globalWatchToggle.checked = !globalWatchToggle.checked; // Revert
    }
}

// --- Add Folder ---
async function openAddFolderDialog() {
    try {
        const path = await window.go.main.App.SelectFolder();
        if (!path) return; // User cancelled

        openFolderModal(path);
    } catch (error) {
        console.error('Failed to select folder:', error);
    }
}

function openFolderModal(path) {
    editingFolderId = null;
    folderModalTitle.textContent = 'Add Watched Folder';
    folderModalPath.textContent = path;
    folderModalPath.dataset.path = path;
    folderModalSave.textContent = 'Add Folder';

    // Reset form
    filterAll.checked = true;
    filterImages.checked = false;
    filterDocuments.checked = false;
    filterVideos.checked = false;
    filterRegex.value = '';
    processExisting.checked = false;
    autoArchive.checked = false;

    updateFilterState();

    // Show modal
    folderModal.classList.remove('opacity-0', 'pointer-events-none');
    folderModal.classList.add('opacity-100');
    folderModal.querySelector('.scale-95').classList.remove('scale-95');
    folderModal.querySelector('.scale-95, div > div').classList.add('scale-100');
}

function closeFolderModal() {
    folderModal.classList.add('opacity-0', 'pointer-events-none');
    folderModal.classList.remove('opacity-100');
}

function updateFilterState() {
    const allChecked = filterAll.checked;
    filterImages.disabled = allChecked;
    filterDocuments.disabled = allChecked;
    filterVideos.disabled = allChecked;

    if (allChecked) {
        filterImages.checked = false;
        filterDocuments.checked = false;
        filterVideos.checked = false;
    }
}

async function saveFolderConfig() {
    const path = folderModalPath.dataset.path;

    let filterMode = 'all';
    let filterPresets = [];

    if (!filterAll.checked) {
        if (filterImages.checked) filterPresets.push('images');
        if (filterDocuments.checked) filterPresets.push('documents');
        if (filterVideos.checked) filterPresets.push('videos');

        if (filterPresets.length > 0) {
            filterMode = 'presets';
        } else if (filterRegex.value.trim()) {
            filterMode = 'custom';
        }
    }

    const config = {
        path: path,
        filter_mode: filterMode,
        filter_presets: filterPresets,
        filter_regex: filterRegex.value.trim(),
        process_existing: processExisting.checked,
        auto_archive: autoArchive.checked
    };

    try {
        const folder = await window.go.main.App.AddWatchedFolder(config);
        await window.go.main.App.RefreshWatches();

        // Process existing if requested
        if (config.process_existing && folder) {
            await window.go.main.App.ProcessExistingFilesInFolder(folder.id);
        }

        closeFolderModal();
        loadWatchFolders();
        showToast('Folder added');
    } catch (error) {
        console.error('Failed to add folder:', error);
        showToast('Failed to add folder: ' + error.message);
    }
}

// --- Event Listeners ---
toggleWatchViewBtn.addEventListener('click', toggleWatchView);
globalWatchToggle.addEventListener('change', toggleGlobalPause);
addFolderBtn.addEventListener('click', openAddFolderDialog);
addFolderZone.addEventListener('click', (e) => {
    if (e.target !== addFolderBtn) openAddFolderDialog();
});

// Filter checkbox logic
filterAll.addEventListener('change', updateFilterState);
filterImages.addEventListener('change', () => { if (filterImages.checked) filterAll.checked = false; });
filterDocuments.addEventListener('change', () => { if (filterDocuments.checked) filterAll.checked = false; });
filterVideos.addEventListener('change', () => { if (filterVideos.checked) filterAll.checked = false; });

// Modal buttons
folderModalCancel.addEventListener('click', closeFolderModal);
folderModalSave.addEventListener('click', saveFolderConfig);
folderModal.addEventListener('click', (e) => {
    if (e.target === folderModal) closeFolderModal();
});

// Drag and drop for folders
addFolderZone.addEventListener('dragover', (e) => {
    e.preventDefault();
    addFolderZone.classList.add('border-stone-400', 'bg-stone-50');
});

addFolderZone.addEventListener('dragleave', () => {
    addFolderZone.classList.remove('border-stone-400', 'bg-stone-50');
});

addFolderZone.addEventListener('drop', async (e) => {
    e.preventDefault();
    addFolderZone.classList.remove('border-stone-400', 'bg-stone-50');

    // Check if it's a folder (Wails should provide path via drop)
    const items = e.dataTransfer.items;
    if (items && items.length > 0) {
        // In Wails, dropped folders come through as file paths
        // We need to handle this via Wails events
        showToast('Use the Add Folder button to select folders');
    }
});

// Wails events for watch notifications
window.runtime.EventsOn('watch:error', (data) => {
    showToast(`Failed to import ${data.file}: ${data.error}`);
});

window.runtime.EventsOn('watch:import', (filename) => {
    // Refresh clips if not viewing watch or archive
    if (!isViewingWatch && !isViewingArchive) {
        loadClips();
    }
    showToast(`Imported: ${filename}`);
});

// Initial status check
updateWatchIndicator();
```

**Step 2: Add watch.js to index.html (before app.js)**

In `frontend/index.html`, add before the app.js script tag:

```html
    <script src="js/watch.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/js/watch.js frontend/index.html
git commit -m "Add watch.js with folder management UI"
```

---

## Task 14: Update app.js to handle watch view state

**Files:**
- Modify: `frontend/js/app.js`

**Step 1: Update toggleViewMode to handle watch view**

Find the `toggleViewMode` function and update it to also hide watch view when switching to archive:

After `isViewingArchive = !isViewingArchive;` add:

```javascript
    // Hide watch view if open
    if (isViewingWatch) {
        isViewingWatch = false;
        watchBtnText.textContent = 'Watch';
        toggleWatchViewBtn.classList.remove('bg-stone-800', 'text-white', 'border-stone-800');
        toggleWatchViewBtn.classList.add('border-stone-200', 'text-stone-600');
        toggleWatchViewBtn.setAttribute('aria-pressed', 'false');
        watchView.classList.add('hidden');
    }
```

**Step 2: Update view visibility in toggleViewMode**

After the archive button style changes, ensure gallery and upload section visibility is restored:

```javascript
    // Ensure main view is visible
    gallery.parentElement.classList.remove('hidden');
```

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "Update app.js to handle watch view state transitions"
```

---

## Task 15: Test the complete flow

**Step 1: Build and run the application**

Run: `wails dev`

**Step 2: Manual testing checklist**

1. Click Watch button - view should toggle
2. Click Add Folder - native picker should open
3. Configure filters and add folder
4. Toggle global pause
5. Toggle individual folder pause
6. Remove a folder
7. Verify indicator dot shows when watching is active
8. Add a file to watched folder - should be imported and show toast
9. Verify file is deleted from watched folder after import
10. Switch between Watch, Archive, and Active views

**Step 3: Commit final working state**

```bash
git add -A
git commit -m "Complete watch folders feature implementation"
```

---

## Summary of Files Changed

**Go Backend:**
- `go.mod` - Added fsnotify dependency
- `database.go` - Added watched_folders and settings tables
- `app.go` - Added WatchedFolder CRUD, pause controls, WatcherManager integration
- `watcher.go` (new) - File system watching with fsnotify

**Frontend:**
- `frontend/index.html` - Added Watch button, watch view section, folder modal
- `frontend/js/watch.js` (new) - Watch view management, folder cards, modal logic
- `frontend/js/app.js` - Updated view state handling
