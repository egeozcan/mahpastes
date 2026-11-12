package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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

// Timings. Variables, not constants, so tests can shrink them.
var (
	// watchDebounceDelay is how long a file must go without a change event
	// (and without changing size or modification time) before it is imported.
	watchDebounceDelay = 500 * time.Millisecond
	// watchRearmInterval is how often the watches are reconciled with the
	// configured folders: a folder that was missing, unmounted, or deleted and
	// recreated is watched again on the next pass.
	watchRearmInterval = 30 * time.Second
	// watchErrorToastInterval rate-limits how often a watcher-level error
	// (such as a kernel event-queue overflow) is reported to the UI.
	watchErrorToastInterval = 30 * time.Second
)

// inProgressSuffixes are the names browsers and download tools give a file
// while they are still writing it. The finished file appears under its final
// name (a rename, which is a Create event), so these are never imported:
// importing one would store a truncated copy and then delete it from under the
// downloader.
var inProgressSuffixes = []string{
	".crdownload", // Chrome, Edge, Brave
	".part",       // Firefox, wget, curl -C, many others
	".partial",    // older Firefox, IE/Edge legacy
	".download",   // Safari (a bundle directory, but some tools use files)
	".opdownload", // Opera
	".tmp",        // generic temporary files
	".!qb",        // qBittorrent incomplete files
	".aria2",      // aria2 control file
	".filepart",   // WinSCP
}

// isInProgressName reports whether a file name looks like a download or save
// that is still being written.
func isInProgressName(name string) bool {
	if strings.HasSuffix(name, "~") {
		return true
	}
	lower := strings.ToLower(name)
	for _, suffix := range inProgressSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// skipWatchedName reports whether a file in a watched folder is never
// imported, whatever the folder's filter: hidden files and files that are
// still being written.
func skipWatchedName(name string) bool {
	return strings.HasPrefix(name, ".") || isInProgressName(name)
}

// errFolderMissing is the watch error recorded for a folder that should be
// watched but is not on disk (deleted, or on a volume that is not mounted).
const errFolderMissing = "folder not found"

// activeWatch is one armed fsnotify watch.
type activeWatch struct {
	path string      // cleaned folder path, as fsnotify reports it in events
	info os.FileInfo // the directory's identity when the watch was added
}

// pendingFile is a file waiting out its debounce.
type pendingFile struct {
	timer   *time.Timer
	size    int64
	modTime time.Time
}

// watchFolderChange describes one folder whose watch state changed. It is the
// payload of the "watch:status" event.
type watchFolderChange struct {
	FolderID int64  `json:"folder_id"`
	Path     string `json:"path"`
	Watching bool   `json:"watching"`
	// Error says why the folder is not being watched; empty while watching.
	Error string `json:"error,omitempty"`
	// Recovered is true when a folder that had an error is watched again.
	Recovered bool `json:"recovered,omitempty"`
}

// WatcherManager handles file system watching for all folders.
//
// Each Start creates a fresh fsnotify watcher and begins a new generation;
// Stop closes it and forgets every watch. Goroutines and timers belong to the
// generation they were started in and do nothing once it has ended, so a
// Stop/Start pair (backup restore) leaves no stale state behind.
type WatcherManager struct {
	app *App

	// refreshMu serialises refreshWatches from reading the configuration to
	// applying it, so a periodic re-arm can never apply a configuration read
	// before a concurrent pause/remove committed.
	refreshMu sync.Mutex

	// mu guards everything below. It is never held across database access,
	// filesystem calls or fsnotify Close.
	mu            sync.RWMutex
	running       bool
	gen           uint64
	watcher       *fsnotify.Watcher
	stopCh        chan struct{}   // closed by Stop; ends the re-arm loop
	loops         *sync.WaitGroup // this generation's event and re-arm goroutines
	activeWatches map[int64]activeWatch
	watchErrors   map[int64]string // folder id -> why a folder that should be watched is not
	debounceMap   map[string]*pendingFile
	lastErrorEmit time.Time

	// kick asks the re-arm loop for an immediate pass (after a watch was
	// lost). Buffered; never closed.
	kick chan struct{}
}

// NewWatcherManager creates a new watcher manager. It creates the first
// fsnotify watcher up front so a platform that cannot watch at all is
// reported at startup.
func NewWatcherManager(app *App) (*WatcherManager, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &WatcherManager{
		watcher:       watcher,
		app:           app,
		activeWatches: make(map[int64]activeWatch),
		watchErrors:   make(map[int64]string),
		debounceMap:   make(map[string]*pendingFile),
		kick:          make(chan struct{}, 1),
	}, nil
}

// Start begins watching all non-paused folders. It may be called again after
// Stop.
func (w *WatcherManager) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	fw := w.watcher
	if fw == nil {
		var err error
		fw, err = fsnotify.NewWatcher()
		if err != nil {
			w.mu.Unlock()
			return err
		}
		w.watcher = fw
	}
	w.running = true
	w.gen++
	gen := w.gen
	w.activeWatches = make(map[int64]activeWatch)
	w.watchErrors = make(map[int64]string)
	w.debounceMap = make(map[string]*pendingFile)
	stop := make(chan struct{})
	w.stopCh = stop
	loops := &sync.WaitGroup{}
	loops.Add(2)
	w.loops = loops
	w.mu.Unlock()

	go func() {
		defer loops.Done()
		w.handleEvents(fw, gen)
	}()
	go func() {
		defer loops.Done()
		w.rearmLoop(stop)
	}()

	// Load and start watching folders
	return w.refreshWatches()
}

// Stop stops all watching and waits for the event and re-arm goroutines to
// exit. Imports already running finish on their own: they may need locks the
// caller holds (a backup restore holds backupRestoreMu).
func (w *WatcherManager) Stop() {
	w.mu.Lock()
	if !w.running {
		fw := w.watcher
		w.watcher = nil
		w.mu.Unlock()
		if fw != nil {
			fw.Close()
		}
		return
	}

	w.running = false
	w.gen++ // timers that already fired see the new generation and do nothing
	close(w.stopCh)

	// Cancel all debounce timers
	for _, p := range w.debounceMap {
		p.timer.Stop()
	}
	w.debounceMap = make(map[string]*pendingFile)
	w.activeWatches = make(map[int64]activeWatch)
	w.watchErrors = make(map[int64]string)

	fw := w.watcher
	w.watcher = nil
	loops := w.loops
	w.mu.Unlock()

	// Closing the watcher closes its channels, which ends handleEvents.
	if fw != nil {
		fw.Close()
	}
	loops.Wait()
}

// rearmLoop reconciles the watches with the configuration every
// watchRearmInterval, and at once when a watch was lost.
func (w *WatcherManager) rearmLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(watchRearmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		case <-w.kick:
		}
		select {
		case <-stop:
			return
		default:
		}
		if err := w.refreshWatches(); err != nil {
			log.Printf("Watch folders: re-arm failed: %v", err)
		}
	}
}

// kickRearm asks the re-arm loop for an immediate pass.
func (w *WatcherManager) kickRearm() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// folderWatchError returns why a folder that should be watched is not, or ""
// when it is watched (or not meant to be).
func (w *WatcherManager) folderWatchError(id int64) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.watchErrors[id]
}

// watchState reports which folders are armed and which have a watch error.
// running is false while the manager is stopped.
func (w *WatcherManager) watchState() (armed map[int64]bool, failed map[int64]bool, running bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	armed = make(map[int64]bool, len(w.activeWatches))
	for id := range w.activeWatches {
		armed[id] = true
	}
	failed = make(map[int64]bool, len(w.watchErrors))
	for id := range w.watchErrors {
		failed[id] = true
	}
	return armed, failed, w.running
}

// watchListHas reports whether fsnotify still holds a watch on path.
func watchListHas(list []string, path string) bool {
	for _, p := range list {
		p = filepath.Clean(p)
		if p == path || (runtime.GOOS == "windows" && strings.EqualFold(p, path)) {
			return true
		}
	}
	return false
}

// refreshWatches reloads watched folders from the DB and reconciles the
// active watches with them:
//   - folders that are paused or removed stop being watched;
//   - an armed watch whose folder was deleted, moved, replaced or unmounted
//     (fsnotify dropped it, or the path now names a different directory) is
//     dropped and re-added if the folder is back;
//   - folders that should be watched but are not are armed, and the reason is
//     recorded when that fails.
//
// Changes are reported to the UI with a "watch:status" event.
func (w *WatcherManager) refreshWatches() error {
	w.refreshMu.Lock()
	defer w.refreshMu.Unlock()

	w.mu.RLock()
	running, gen, fw := w.running, w.gen, w.watcher
	current := make(map[int64]activeWatch, len(w.activeWatches))
	for id, aw := range w.activeWatches {
		current[id] = aw
	}
	w.mu.RUnlock()
	if !running || fw == nil {
		return nil
	}

	globalPaused := w.app.GetGlobalWatchPaused()
	folders, err := w.app.GetWatchedFolders()
	if err != nil {
		return err
	}

	// Build set of folders that should be watched
	shouldWatch := make(map[int64]string)
	for _, f := range folders {
		if !globalPaused && !f.IsPaused {
			shouldWatch[f.ID] = filepath.Clean(f.Path)
		}
	}

	// Verify the armed watches. Filesystem and fsnotify calls happen outside
	// w.mu: a stat on an unreachable network volume can block.
	watchList := fw.WatchList()
	next := make(map[int64]activeWatch, len(shouldWatch))
	var drop []activeWatch
	for id, aw := range current {
		path, ok := shouldWatch[id]
		if !ok || path != aw.path {
			drop = append(drop, aw)
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() || !os.SameFile(info, aw.info) || !watchListHas(watchList, path) {
			log.Printf("Watch on %s was lost; re-arming", path)
			drop = append(drop, aw)
			continue
		}
		next[id] = aw
	}

	inUse := make(map[string]bool, len(next))
	for _, aw := range next {
		inUse[aw.path] = true
	}
	for _, aw := range drop {
		if !inUse[aw.path] {
			// Errors are expected: fsnotify has usually dropped it already.
			_ = fw.Remove(aw.path)
		}
	}

	// Arm folders that should be watched but are not.
	errs := make(map[int64]string)
	ids := make([]int64, 0, len(shouldWatch))
	for id := range shouldWatch {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, ok := next[id]; ok {
			continue
		}
		path := shouldWatch[id]
		info, statErr := os.Stat(path)
		switch {
		case statErr != nil && errors.Is(statErr, os.ErrNotExist):
			errs[id] = errFolderMissing
			continue
		case statErr != nil:
			errs[id] = statErr.Error()
			continue
		case !info.IsDir():
			errs[id] = "not a folder"
			continue
		}
		if inUse[path] {
			// Another folder entry already watches this directory.
			next[id] = activeWatch{path: path, info: info}
			continue
		}
		if err := fw.Add(path); err != nil {
			errs[id] = err.Error()
			continue
		}
		next[id] = activeWatch{path: path, info: info}
		inUse[path] = true
	}

	w.mu.Lock()
	if !w.running || w.gen != gen {
		// Stopped (or restarted) meanwhile: this pass worked on a closed
		// watcher, and the new generation does its own refresh.
		w.mu.Unlock()
		return nil
	}
	prevActive, prevErrs := w.activeWatches, w.watchErrors
	w.activeWatches = next
	w.watchErrors = errs
	w.mu.Unlock()

	changes := diffWatchState(shouldWatch, prevActive, prevErrs, next, errs)
	for _, c := range changes {
		switch {
		case c.Watching:
			log.Printf("Started watching: %s", c.Path)
		case c.Error != "":
			log.Printf("Not watching %s: %s", c.Path, c.Error)
		default:
			log.Printf("Stopped watching: %s", c.Path)
		}
	}
	if len(changes) > 0 {
		w.app.emitWatchStatus(changes, "")
	}
	return nil
}

// diffWatchState lists the folders whose armed/error state differs between two
// reconcile results.
func diffWatchState(shouldWatch map[int64]string, prevActive map[int64]activeWatch, prevErrs map[int64]string, next map[int64]activeWatch, errs map[int64]string) []watchFolderChange {
	ids := make(map[int64]bool)
	for id := range prevActive {
		ids[id] = true
	}
	for id := range prevErrs {
		ids[id] = true
	}
	for id := range next {
		ids[id] = true
	}
	for id := range errs {
		ids[id] = true
	}
	var changes []watchFolderChange
	for id := range ids {
		_, wasWatching := prevActive[id]
		_, watching := next[id]
		if wasWatching == watching && prevErrs[id] == errs[id] {
			continue
		}
		path := shouldWatch[id]
		if path == "" {
			if aw, ok := prevActive[id]; ok {
				path = aw.path
			}
		}
		changes = append(changes, watchFolderChange{
			FolderID:  id,
			Path:      path,
			Watching:  watching,
			Error:     errs[id],
			Recovered: watching && prevErrs[id] != "",
		})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].FolderID < changes[j].FolderID })
	return changes
}

// handleEvents processes one generation's fsnotify events. It reads from the
// watcher it was started with, never from the field, so a restart cannot
// make it read another generation's channels.
func (w *WatcherManager) handleEvents(fw *fsnotify.Watcher, gen uint64) {
	for {
		select {
		case event, ok := <-fw.Events:
			if !ok {
				return
			}

			// A watched folder that is removed or moved loses its watch
			// (fsnotify drops it). Handle this before the file checks below,
			// which skip anything that no longer stats.
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 && w.dropLostWatch(gen, filepath.Clean(event.Name)) {
				continue
			}

			// Only handle Create and Write events
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}

			// Skip hidden files and downloads still being written
			if skipWatchedName(filepath.Base(event.Name)) {
				continue
			}

			// Skip directories
			info, err := os.Stat(event.Name)
			if err != nil || info.IsDir() {
				continue
			}

			// Debounce: wait until the file has stopped changing
			w.debounceFile(gen, event.Name, info)

		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			w.reportWatcherError(gen, err)
		}
	}
}

// reportWatcherError logs a watcher-level error (for example an event-queue
// overflow, which means file events were lost) and tells the UI, at most once
// per watchErrorToastInterval.
func (w *WatcherManager) reportWatcherError(gen uint64, err error) {
	log.Printf("Watcher error: %v", err)
	w.mu.Lock()
	if gen != w.gen || !w.running || time.Since(w.lastErrorEmit) < watchErrorToastInterval {
		w.mu.Unlock()
		return
	}
	w.lastErrorEmit = time.Now()
	w.mu.Unlock()
	msg := err.Error()
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		msg = "too many file changes at once; some new files may not have been imported"
	}
	w.app.emitWatchStatus(nil, msg)
}

// dropLostWatch forgets the watch on path when path is a watched folder that
// was removed or moved, and asks for an immediate re-arm pass. That pass
// records the folder as unavailable (and tells the UI) if it is still gone,
// re-arms it if it is already back, and says nothing if the folder was removed
// from the configuration. It reports whether path was a watched folder.
func (w *WatcherManager) dropLostWatch(gen uint64, path string) bool {
	w.mu.Lock()
	if gen != w.gen || !w.running {
		w.mu.Unlock()
		return false
	}
	found := false
	for id, aw := range w.activeWatches {
		if aw.path == path {
			delete(w.activeWatches, id)
			found = true
		}
	}
	fw := w.watcher
	w.mu.Unlock()
	if !found {
		return false
	}

	if fw != nil {
		_ = fw.Remove(path) // usually already gone
	}
	log.Printf("Watched folder %s was moved or deleted; it is watched again once it is back", path)
	w.kickRearm()
	return true
}

// debounceFile delays processing until the file is stable: no change event
// for watchDebounceDelay and no change in size or modification time.
func (w *WatcherManager) debounceFile(gen uint64, path string, info os.FileInfo) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if gen != w.gen || !w.running {
		return
	}

	// Cancel existing timer if any
	if p, ok := w.debounceMap[path]; ok {
		p.timer.Stop()
	}

	p := &pendingFile{size: info.Size(), modTime: info.ModTime()}
	p.timer = time.AfterFunc(watchDebounceDelay, func() { w.settleFile(gen, path, p) })
	w.debounceMap[path] = p
}

// settleFile runs when a file's debounce expires. A file still changing
// (some writers produce no event per write, e.g. on network filesystems) is
// given another round; an empty file is left alone, since a downloader may
// create an empty placeholder under the final name (Firefox does) and replace
// it when the download completes, which is a new event.
func (w *WatcherManager) settleFile(gen uint64, path string, p *pendingFile) {
	info, statErr := os.Stat(path)

	w.mu.Lock()
	if gen != w.gen || !w.running || w.debounceMap[path] != p {
		// Stopped, restarted, or superseded by a newer event for this file.
		w.mu.Unlock()
		return
	}
	if statErr == nil && !info.IsDir() && (info.Size() != p.size || !info.ModTime().Equal(p.modTime)) {
		p.size, p.modTime = info.Size(), info.ModTime()
		p.timer = time.AfterFunc(watchDebounceDelay, func() { w.settleFile(gen, path, p) })
		w.mu.Unlock()
		return
	}
	delete(w.debounceMap, path)
	w.mu.Unlock()

	if statErr != nil || info.IsDir() {
		return
	}
	if info.Size() == 0 {
		log.Printf("Skipping empty file (it may be a download placeholder): %s", path)
		return
	}
	w.processFile(gen, path, info)
}

// removeImportedSource deletes a watched file once it is safely stored,
// unless it changed while it was being imported: the stored clip would then
// be a partial copy, and deleting the file would lose the rest.
func removeImportedSource(filePath string, before os.FileInfo) {
	after, err := os.Stat(filePath)
	if err != nil || !sameFileState(before, after) {
		log.Printf("File changed while it was imported; leaving it in place: %s", filePath)
		return
	}
	if err := os.Remove(filePath); err != nil {
		log.Printf("Failed to delete original file %s: %v", filePath, err)
	}
}

// processFile handles a new file in a watched folder. info is the file's state
// when its debounce settled.
func (w *WatcherManager) processFile(gen uint64, filePath string, info os.FileInfo) {
	// A watch created before the clips folder was mounted over its path would
	// otherwise import the mount's own change events in a loop.
	if w.app.insideProjectionMount(filePath) {
		return
	}
	dir := filepath.Dir(filePath)

	// Find which folder ID this belongs to
	w.mu.RLock()
	var folderID int64
	if gen == w.gen && w.running {
		for id, aw := range w.activeWatches {
			if aw.path == dir {
				folderID = id
				break
			}
		}
	}
	w.mu.RUnlock()

	if folderID == 0 {
		return
	}

	// Fetch fresh config from DB to avoid stale cache issues
	folder, err := w.app.GetWatchedFolderByID(folderID)
	if err != nil {
		log.Printf("Failed to get folder config for %s: %v", filePath, err)
		return
	}
	if folder == nil {
		log.Printf("Folder %d no longer exists, skipping %s", folderID, filePath)
		return
	}

	// Check filter
	if !w.matchesFilter(filePath, folder) {
		log.Printf("File does not match filter: %s", filePath)
		return
	}

	// Import the file and get the clip ID
	clipID, err := w.importFile(filePath, folder)
	if err != nil {
		log.Printf("Failed to import file %s: %v", filePath, err)
		// Emit event to frontend for toast notification
		w.app.emitWatchError(filePath, err.Error())
		return
	}

	// Verify the clip was actually saved before deleting
	if clipID == 0 {
		log.Printf("Import returned no clip ID for %s, not deleting original", filePath)
		w.app.emitWatchError(filePath, "import failed to return clip ID")
		return
	}

	// Delete original file only after confirmed DB commit
	removeImportedSource(filePath, info)

	log.Printf("Successfully imported (clip ID %d): %s", clipID, filePath)
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

// importFile reads a file and imports it as a clip, returns the clip ID
func (w *WatcherManager) importFile(filePath string, folder *WatchedFolder) (int64, error) {
	fileData, err := w.app.ReadFileFromPath(filePath)
	if err != nil {
		return 0, err
	}

	// Emit watch:file_detected event before import
	if w.app.pluginManager != nil {
		w.app.emitPluginEvent("watch:file_detected", map[string]interface{}{
			"path":      filePath,
			"folder_id": folder.ID,
		})
	}

	// Upload and get the clip ID
	epoch := w.app.currentRestoreEpoch()
	clipID, err := w.app.UploadFileAndGetID(*fileData)
	if err != nil {
		return 0, err
	}

	// Auto-archive if configured - must happen BEFORE emitting event
	// so frontend sees the clip in its final archived state
	if folder.AutoArchive {
		if err := w.app.ToggleArchive(clipID); err != nil {
			log.Printf("Failed to auto-archive clip %d: %v", clipID, err)
		}
	}

	// Auto-tag if configured
	if folder.AutoTagID != nil {
		if err := w.app.addTagToNewClip(clipID, *folder.AutoTagID, epoch); err != nil {
			log.Printf("Failed to auto-tag clip %d with tag %d: %v", clipID, *folder.AutoTagID, err)
			if errors.Is(err, errRestoredSinceInsert) {
				return 0, err
			}
		}
	}

	// Emit import event for UI refresh (after archiving is complete)
	clip, err := w.app.getClipPreview(clipID)
	if err != nil {
		log.Printf("Failed to get clip preview for watch event: %v", err)
	} else {
		w.app.emitWatchImport(*clip)
	}

	// Emit watch:import_complete event
	if w.app.pluginManager != nil {
		w.app.emitPluginEvent("watch:import_complete", map[string]interface{}{
			"clip_id":     clipID,
			"source_path": filePath,
			"folder_id":   folder.ID,
		})
	}

	return clipID, nil
}

// ProcessExistingFiles imports all existing files in a watched folder. Hidden
// files, downloads still in progress and empty files are left alone, as they
// are for live events.
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
	if w.app.insideProjectionMount(folder.Path) {
		return errProjectionMount
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
		if skipWatchedName(name) {
			continue
		}

		filePath := filepath.Join(folder.Path, name)

		if !w.matchesFilter(filePath, folder) {
			continue
		}

		info, err := os.Stat(filePath)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}

		clipID, err := w.importFile(filePath, folder)
		if err != nil {
			log.Printf("Failed to import existing file %s: %v", filePath, err)
			w.app.emitWatchError(filePath, err.Error())
			continue
		}

		// Only delete after confirmed DB commit
		if clipID == 0 {
			log.Printf("Import returned no clip ID for %s, not deleting", filePath)
			continue
		}

		removeImportedSource(filePath, info)
	}

	return nil
}
