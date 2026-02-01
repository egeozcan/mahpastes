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
	watcher       *fsnotify.Watcher
	app           *App
	activeWatches map[int64]string        // folderID -> path
	debounceMap   map[string]*time.Timer  // path -> debounce timer
	mu            sync.RWMutex
	running       bool
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

// refreshWatches reloads watched folders from DB and updates active watches
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

	// Emit import event for UI refresh
	w.app.emitWatchImport(fileData.Name)

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
