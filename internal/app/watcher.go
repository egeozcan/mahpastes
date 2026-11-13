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
	// recreated is watched again on the next pass. Files that arrived while a
	// folder's watch was dead are not picked up, like files already there when
	// a folder is added.
	watchRearmInterval = 30 * time.Second
	// watchBusyRecheckMax caps the backoff between checks of a file another
	// process still has open for writing.
	watchBusyRecheckMax = 5 * time.Second
	// watchBusyReportAfter is how long a file may stay open for writing,
	// unchanged, before the UI is told (once) why it has not been imported.
	// It keeps being rechecked after that.
	watchBusyReportAfter = time.Minute
	// watchStatTimeout bounds how long a re-arm pass waits for one folder's
	// stat: on a dead network mount it can block for minutes.
	watchStatTimeout = 5 * time.Second
	// watchErrorToastInterval rate-limits how often a watcher-level error
	// (such as a kernel event-queue overflow) is reported to the UI.
	watchErrorToastInterval = 30 * time.Second
)

// inProgressSuffixes are the names browsers and download tools give a file
// while they are still writing it. The finished file appears under its final
// name (a rename, which is a Create event), so these are never imported:
// importing one would store a truncated copy and then delete it from under the
// downloader. A tool that writes straight to the final name is covered only by
// the debounce and, where the platform can tell (watchedFileBusy), by not
// touching a file some process still has open for writing.
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

// errWatchStatTimeout is the watch error for a folder whose stat did not
// return within watchStatTimeout.
var errWatchStatTimeout = errors.New("folder is not responding")

// errSameFolderPrefix starts the watch error of a folder entry that names a
// directory another entry already watches (another letter case, a symlink).
const errSameFolderPrefix = "same folder as "

// watchBackend is the part of an fsnotify watcher the manager calls.
type watchBackend interface {
	Add(name string) error
	Remove(name string) error
	WatchList() []string
	Close() error
}

// fsWatch is one fsnotify watcher and its channels.
type fsWatch struct {
	watchBackend
	events <-chan fsnotify.Event
	errors <-chan error
}

// newFsWatch creates the OS watcher. A variable so tests can wrap it.
var newFsWatch = func() (*fsWatch, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsWatch{watchBackend: fw, events: fw.Events, errors: fw.Errors}, nil
}

// dirProbe tells whether the OS still delivers events for a watched directory.
// macOS revokes a watcher's descriptors when the volume is unmounted, without
// any event, and the remounted directory has the same device and inode: only
// a descriptor opened before the unmount shows it. Both methods are safe to
// call concurrently and after close.
type dirProbe interface {
	alive() bool
	close()
}

// openDirProbe opens a probe for a directory just armed; nil where the
// platform needs none. A variable so tests can replace it.
var openDirProbe = openPlatformDirProbe

// statWatchDir stats a watched folder. A variable so tests can block it.
var statWatchDir = os.Stat

// watchedFileBusy reports whether another process still has the file open for
// writing (always false where the platform cannot tell), going by a look at
// the system's open files begun no more than maxAge before the call. Checks
// are shared rather than run per file (writeOpenSnapshots), so a caller may
// wait for one in progress. A variable so tests can replace it.
var watchedFileBusy = fileOpenForWriting

// watchBusySettleMaxAge is how old a look a settling file accepts. It stays a
// fraction of the debounce, so the look always postdates the file's last
// change event, and files settling in a stream share a few looks a second
// rather than running them back to back. The check before a deletion
// (removeImportedSource) accepts none older than itself.
func watchBusySettleMaxAge() time.Duration { return watchDebounceDelay / 5 }

// activeWatch is one armed fsnotify watch.
type activeWatch struct {
	path  string      // cleaned folder path, as fsnotify reports it in events
	info  os.FileInfo // the directory's identity when the watch was added
	probe dirProbe    // nil where the platform needs none
}

// closeProbesExcept closes the probes of from that keep does not hold.
func closeProbesExcept(from, keep map[int64]activeWatch) {
	kept := make(map[dirProbe]bool, len(keep))
	for _, aw := range keep {
		if aw.probe != nil {
			kept[aw.probe] = true
		}
	}
	for _, aw := range from {
		if aw.probe != nil && !kept[aw.probe] {
			aw.probe.close()
		}
	}
}

// pendingFile is a file waiting out its debounce.
type pendingFile struct {
	timer    *time.Timer
	size     int64
	modTime  time.Time
	busyWait time.Duration // current backoff while a writer holds the file open
	// busySince is when a writer was first found holding the file (zero: not
	// held); busyReported says the UI was told.
	busySince    time.Time
	busyReported bool
}

// statFlight is one stat of a watched folder in progress.
type statFlight struct {
	started time.Time
	done    chan struct{}
	info    os.FileInfo
	err     error
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

	// statMu guards statFlights: at most one stat per folder is in flight,
	// however many passes time out waiting for it.
	statMu      sync.Mutex
	statFlights map[string]*statFlight

	// mu guards everything below. It is never held across database access,
	// filesystem calls or fsnotify Close.
	mu      sync.RWMutex
	running bool
	gen     uint64
	watcher *fsWatch
	// refreshMu serialises refreshWatches from reading the configuration to
	// applying it, so a periodic re-arm can never apply a configuration read
	// before a concurrent pause/remove committed. A new one per generation: a
	// pass left behind by Stop (stuck on a slow folder) holds only its own.
	refreshMu     refreshLock
	stopCh        chan struct{} // closed by Stop; ends the re-arm loop
	eventsDone    chan struct{} // closed when this generation's handleEvents returns
	rearmDone     chan struct{} // closed when this generation's re-arm loop returns
	activeWatches map[int64]activeWatch
	watchErrors   map[int64]string // folder id -> why a folder that should be watched is not
	debounceMap   map[string]*pendingFile
	lastErrorEmit time.Time

	// kick asks this generation's re-arm loop for an immediate pass (after a
	// watch was lost). Buffered; never closed. A new one per generation, so a
	// loop left behind by Stop (stuck on a slow folder) cannot swallow a kick
	// meant for the next generation.
	kick chan struct{}
}

// NewWatcherManager creates a new watcher manager. It creates the first
// fsnotify watcher up front so a platform that cannot watch at all is
// reported at startup.
func NewWatcherManager(app *App) (*WatcherManager, error) {
	watcher, err := newFsWatch()
	if err != nil {
		return nil, err
	}

	return &WatcherManager{
		watcher:       watcher,
		app:           app,
		refreshMu:     make(refreshLock, 1),
		statFlights:   make(map[string]*statFlight),
		activeWatches: make(map[int64]activeWatch),
		watchErrors:   make(map[int64]string),
		debounceMap:   make(map[string]*pendingFile),
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
		fw, err = newFsWatch()
		if err != nil {
			w.mu.Unlock()
			return err
		}
		w.watcher = fw
	}
	w.running = true
	w.gen++
	gen := w.gen
	w.refreshMu = make(refreshLock, 1)
	w.activeWatches = make(map[int64]activeWatch)
	w.watchErrors = make(map[int64]string)
	w.debounceMap = make(map[string]*pendingFile)
	stop := make(chan struct{})
	eventsDone := make(chan struct{})
	rearmDone := make(chan struct{})
	kick := make(chan struct{}, 1)
	w.stopCh, w.eventsDone, w.rearmDone, w.kick = stop, eventsDone, rearmDone, kick
	w.mu.Unlock()

	go func() {
		defer close(eventsDone)
		w.handleEvents(fw, gen)
	}()
	go func() {
		defer close(rearmDone)
		w.rearmLoop(stop, kick)
	}()

	// Load and start watching folders
	return w.refreshWatches()
}

// watchStopRearmWait bounds how long Stop waits for the event goroutine and a
// re-arm pass in progress. Both stat files, and a stat on a dead network mount
// can hang; Stop (and so quitting the app) must not hang with it. Whatever
// finishes after Stop applies nothing: its generation has ended, and the pass
// holds only that generation's refresh lock.
var watchStopRearmWait = 2 * time.Second

// Stop stops all watching and waits, briefly, for the event goroutine and a
// re-arm pass in progress. Imports already running finish on their own: they
// may need locks the caller holds (a backup restore holds backupRestoreMu).
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
	stopped := w.activeWatches
	w.activeWatches = make(map[int64]activeWatch)
	w.watchErrors = make(map[int64]string)

	fw := w.watcher
	w.watcher = nil
	eventsDone, rearmDone := w.eventsDone, w.rearmDone
	w.mu.Unlock()

	// Closing the watcher closes its channels, which ends handleEvents.
	if fw != nil {
		fw.Close()
	}
	closeProbesExcept(stopped, nil)
	timeout := time.After(watchStopRearmWait)
	select {
	case <-eventsDone:
	case <-timeout:
		log.Printf("Watch folders: the event loop is still busy (slow folder?); not waiting for it")
		return
	}
	select {
	case <-rearmDone:
	case <-timeout:
		log.Printf("Watch folders: a re-arm pass is still running (slow folder?); not waiting for it")
	}
}

// rearmLoop reconciles the watches with the configuration every
// watchRearmInterval, and at once when a watch was lost.
func (w *WatcherManager) rearmLoop(stop <-chan struct{}, kick <-chan struct{}) {
	ticker := time.NewTicker(watchRearmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		case <-kick:
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

// kickRearm asks the current generation's re-arm loop for an immediate pass.
func (w *WatcherManager) kickRearm() {
	w.mu.RLock()
	kick := w.kick
	w.mu.RUnlock()
	signalRearm(kick)
}

// signalRearm sends a non-blocking kick; repeated kicks coalesce.
func signalRearm(kick chan struct{}) {
	if kick == nil {
		return
	}
	select {
	case kick <- struct{}{}:
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

// pinFileIdentity makes info remember which file it describes. On Windows,
// os.Stat defers reading the file ID until the first os.SameFile call, which
// would then resolve against whatever is at the path by that time and make an
// identity check always pass.
func pinFileIdentity(info os.FileInfo) os.FileInfo {
	os.SameFile(info, info)
	return info
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

// forgetWatch removes path from fw and reports whether fsnotify let go of it.
// It does not when the watch's descriptor is dead (macOS, after the volume was
// unmounted): the entry stays, and every later Add of that path re-registers
// the dead descriptor instead of opening the directory. Such a watcher cannot
// be repaired, only replaced.
func forgetWatch(fw *fsWatch, path string) bool {
	// Errors are expected: fsnotify has usually dropped it already.
	_ = fw.Remove(path)
	return !watchListHas(fw.WatchList(), path)
}

// statDir stats a watched folder, waiting at most watchStatTimeout. A stat
// that outlives the wait keeps running; later calls for the same folder join
// it rather than start another, so a dead mount costs one goroutine.
func (w *WatcherManager) statDir(path string) (os.FileInfo, error) {
	w.statMu.Lock()
	fl := w.statFlights[path]
	if fl == nil {
		fl = &statFlight{started: time.Now(), done: make(chan struct{})}
		w.statFlights[path] = fl
		stat := statWatchDir
		go func() {
			fl.info, fl.err = stat(path)
			w.statMu.Lock()
			delete(w.statFlights, path)
			w.statMu.Unlock()
			close(fl.done)
		}()
	}
	w.statMu.Unlock()

	wait := watchStatTimeout - time.Since(fl.started)
	if wait < 0 {
		wait = 0
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-fl.done:
		return fl.info, fl.err
	case <-timer.C:
		return nil, errWatchStatTimeout
	}
}

// sameDirWatch returns the path of the watch in watches on the directory info
// describes, if there is one.
func sameDirWatch(watches map[int64]activeWatch, info os.FileInfo) (string, bool) {
	for _, aw := range watches {
		if os.SameFile(aw.info, info) {
			return aw.path, true
		}
	}
	return "", false
}

// refreshLock is one generation's refresh lock. A channel, so a caller
// waiting for it can give up when the generation ends.
type refreshLock chan struct{}

func (l refreshLock) Lock()   { l <- struct{}{} }
func (l refreshLock) Unlock() { <-l }

// lockRefresh takes the running generation's refresh lock; nil when the
// manager is stopped. It never waits behind a pass of an ended generation.
func (w *WatcherManager) lockRefresh() refreshLock {
	for {
		w.mu.RLock()
		running, lock, stop := w.running, w.refreshMu, w.stopCh
		w.mu.RUnlock()
		if !running {
			return nil
		}
		select {
		case lock <- struct{}{}:
			w.mu.RLock()
			current := w.running && w.refreshMu == lock
			w.mu.RUnlock()
			if current {
				return lock
			}
			lock.Unlock()
		case <-stop:
			// The generation ended while this call waited.
		}
	}
}

// refreshWatches reloads watched folders from the DB and reconciles the
// active watches with them:
//   - folders that are paused or removed stop being watched;
//   - an armed watch whose folder was deleted, moved, replaced or unmounted
//     (fsnotify dropped it, the path now names a different directory, or the
//     watch's descriptor was revoked) is dropped and re-added if the folder is
//     back;
//   - folders that should be watched but are not are armed, and the reason is
//     recorded when that fails.
//
// A watcher holding a dead descriptor it will not release is replaced by a
// new one within the same generation (see forgetWatch).
//
// Changes are reported to the UI with a "watch:status" event.
func (w *WatcherManager) refreshWatches() error {
	lock := w.lockRefresh()
	if lock == nil {
		return nil
	}
	defer lock.Unlock()

	w.mu.RLock()
	running, gen, fw := w.running && w.refreshMu == lock, w.gen, w.watcher
	current := make(map[int64]activeWatch, len(w.activeWatches))
	for id, aw := range w.activeWatches {
		current[id] = aw
	}
	w.mu.RUnlock()
	if !running || fw == nil {
		// Stopped, or restarted since the lock was taken: the new generation
		// does its own refresh, under its own lock.
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

	next, errs, healthy := w.reconcile(fw, current, shouldWatch)
	var replacement *fsWatch
	var replacementDone chan struct{}
	if !healthy {
		// Arm everything on a new watcher before the old one is closed, so
		// folders that were fine miss nothing. At most once per pass.
		log.Printf("Watch folders: the system watcher holds a dead watch; replacing it")
		replacement, err = newFsWatch()
		if err != nil {
			return fmt.Errorf("replace the file watcher: %w", err)
		}
		replacementDone = make(chan struct{})
		go func(nfw *fsWatch, done chan struct{}) {
			defer close(done)
			w.handleEvents(nfw, gen)
		}(replacement, replacementDone)
		var ok bool
		if next, errs, ok = w.reconcile(replacement, nil, shouldWatch); !ok {
			// Not expected of a new watcher. Report every folder as unwatched;
			// the next pass replaces this watcher too.
			next, errs = map[int64]activeWatch{}, make(map[int64]string, len(shouldWatch))
			for id := range shouldWatch {
				errs[id] = "the system file watcher failed"
			}
		}
	}

	w.mu.Lock()
	if !w.running || w.gen != gen {
		// Stopped (or restarted) meanwhile: this pass worked on a closed
		// watcher, and the new generation does its own refresh.
		w.mu.Unlock()
		if replacement != nil {
			replacement.Close()
		}
		closeProbesExcept(next, nil)
		return nil
	}
	prevActive, prevErrs := w.activeWatches, w.watchErrors
	// A copy: dropLostWatch edits the installed map while next is still read
	// below.
	w.activeWatches = make(map[int64]activeWatch, len(next))
	for id, aw := range next {
		w.activeWatches[id] = aw
	}
	w.watchErrors = errs
	if replacement != nil {
		w.watcher, w.eventsDone = replacement, replacementDone
	}
	w.mu.Unlock()

	if replacement != nil {
		// Ends the old watcher's handleEvents.
		fw.Close()
	}
	closeProbesExcept(current, next)

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

// reconcile brings fw's watches in line with shouldWatch, starting from the
// armed watches in current. It returns the watches now armed and, per folder,
// why one that should be watched is not. healthy is false when fw holds a dead
// watch (see forgetWatch); the caller then replaces fw and the other results
// are not to be used.
//
// Filesystem and fsnotify calls happen here, outside w.mu: a stat on an
// unreachable network volume can block.
func (w *WatcherManager) reconcile(fw *fsWatch, current map[int64]activeWatch, shouldWatch map[int64]string) (next map[int64]activeWatch, errs map[int64]string, healthy bool) {
	next = make(map[int64]activeWatch, len(shouldWatch))
	errs = make(map[int64]string)
	// Probes opened here belong to nobody yet if the pass is abandoned.
	unhealthy := func() (map[int64]activeWatch, map[int64]string, bool) {
		closeProbesExcept(next, current)
		return nil, nil, false
	}

	// Verify the armed watches.
	watchList := fw.WatchList()
	var drop []activeWatch
	for id, aw := range current {
		path, ok := shouldWatch[id]
		if !ok || path != aw.path {
			drop = append(drop, aw)
			continue
		}
		info, statErr := w.statDir(path)
		if errors.Is(statErr, errWatchStatTimeout) {
			// Unknown, not lost: keep the watch rather than tear it down.
			next[id] = aw
			continue
		}
		if statErr != nil || !info.IsDir() || !os.SameFile(info, aw.info) || !watchListHas(watchList, path) {
			log.Printf("Watch on %s was lost; re-arming", path)
			drop = append(drop, aw)
			continue
		}
		if aw.probe != nil && !aw.probe.alive() {
			// Unmounted and mounted again: the folder looks the same, but
			// fsnotify's descriptors for it are dead.
			log.Printf("Watch on %s was revoked (volume remounted?)", path)
			return unhealthy()
		}
		next[id] = aw
	}

	for _, aw := range drop {
		if !forgetWatch(fw, aw.path) {
			return unhealthy()
		}
	}

	// Arm folders that should be watched but are not, lowest id first.
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
		info, statErr := w.statDir(path)
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
		pinFileIdentity(info)
		// One entry per directory, whatever it is called: a second watch under
		// another spelling would deliver, and import, every file twice.
		if owner, ok := sameDirWatch(next, info); ok {
			errs[id] = errSameFolderPrefix + owner
			continue
		}
		// fsnotify must not already hold this path: nothing here owns it.
		if watchListHas(fw.WatchList(), path) && !forgetWatch(fw, path) {
			return unhealthy()
		}
		if err := fw.Add(path); err != nil {
			if watchListHas(fw.WatchList(), path) {
				return unhealthy()
			}
			errs[id] = err.Error()
			continue
		}
		next[id] = activeWatch{path: path, info: info, probe: openDirProbe(path)}
	}
	return next, errs, true
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
// make it read another generation's channels. It must never call into
// fsnotify (Add/Remove): on Windows those wait for fsnotify's reader
// goroutine, which can be blocked handing this goroutine an event.
func (w *WatcherManager) handleEvents(fw *fsWatch, gen uint64) {
	for {
		select {
		case event, ok := <-fw.events:
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

		case err, ok := <-fw.errors:
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
	lost := make(map[int64]activeWatch)
	for id, aw := range w.activeWatches {
		if aw.path == path {
			delete(w.activeWatches, id)
			lost[id] = aw
		}
	}
	kick := w.kick
	w.mu.Unlock()
	if len(lost) == 0 {
		return false
	}
	closeProbesExcept(lost, nil)

	// No fw.Remove here: fsnotify has already dropped a watch whose folder
	// was removed or moved, and on Windows Remove waits for fsnotify's reader
	// goroutine, which may itself be waiting for this goroutine to take the
	// next event. The re-arm pass cleans up from its own goroutine.
	log.Printf("Watched folder %s was moved or deleted; it is watched again once it is back", path)
	signalRearm(kick)
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
// given another round, and so is one a process still has open for writing: a
// writer that merely paused would otherwise get a truncated clip and its file
// deleted from under it. An empty file is left alone, since a downloader may
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
		p.size, p.modTime, p.busyWait, p.busySince = info.Size(), info.ModTime(), 0, time.Time{}
		p.timer = time.AfterFunc(watchDebounceDelay, func() { w.settleFile(gen, path, p) })
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	// Outside w.mu: the check walks other processes' open files, or waits
	// for a walk another file started.
	busy := statErr == nil && !info.IsDir() && info.Size() > 0 && watchedFileBusy(path, info, watchBusySettleMaxAge())

	w.mu.Lock()
	if gen != w.gen || !w.running || w.debounceMap[path] != p {
		w.mu.Unlock()
		return
	}
	if busy {
		if p.busyWait == 0 {
			log.Printf("Still open for writing; waiting for the writer to finish: %s", path)
			p.busyWait = watchDebounceDelay
		} else if p.busyWait *= 2; p.busyWait > watchBusyRecheckMax {
			p.busyWait = watchBusyRecheckMax
		}
		if p.busySince.IsZero() {
			p.busySince = time.Now()
		}
		report := !p.busyReported && time.Since(p.busySince) >= watchBusyReportAfter
		if report {
			p.busyReported = true
		}
		p.timer = time.AfterFunc(p.busyWait, func() { w.settleFile(gen, path, p) })
		w.mu.Unlock()
		if report {
			w.app.emitWatchError(path, "another app still has it open for writing; it is imported once that app closes it")
		}
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
	w.processFile(gen, path, pinFileIdentity(info))
}

// removeImportedSource deletes a watched file once it is safely stored,
// unless it changed while it was being imported (the stored clip would then be
// a partial copy, and deleting the file would lose the rest), a process has
// it open for writing (what it writes next would go to an unlinked file), or a backup
// restore replaced the library since the clip was stored (epoch is the
// restore epoch read before the upload): the clip is gone, and the file is
// then the only copy. A restore stops the watcher but does not wait for
// imports already running, so this is reachable.
//
// Holding backupRestoreMu's read side across the check and the removal keeps a
// restore from landing in between. Nothing here may take that lock again (a
// second RLock can deadlock behind a waiting restore), so restoreEpoch is
// read directly rather than through currentRestoreEpoch.
func (a *App) removeImportedSource(filePath string, before os.FileInfo, epoch uint64) {
	a.backupRestoreMu.RLock()
	defer a.backupRestoreMu.RUnlock()
	if a.restoreEpoch != epoch {
		log.Printf("A backup restore replaced the library while %s was imported; leaving it in place", filePath)
		return
	}
	after, err := os.Stat(filePath)
	if err != nil || !sameFileState(before, after) {
		log.Printf("File changed while it was imported; leaving it in place: %s", filePath)
		return
	}
	// Age 0: a writer that opened the file at any point before this check
	// must be seen, since the next step is the unlink.
	if watchedFileBusy(filePath, after, 0) {
		log.Printf("File is open for writing; leaving it in place: %s", filePath)
		return
	}
	// The walk takes milliseconds: a write that opened and closed during it
	// was not seen as busy, so compare once more right before the unlink.
	if final, err := os.Stat(filePath); err != nil || !sameFileState(before, final) {
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
	clipID, epoch, err := w.importFile(filePath, folder)
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
	w.app.removeImportedSource(filePath, info, epoch)

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

// importFile reads a file and imports it as a clip. It returns the clip ID and
// the restore epoch read before the clip was inserted (see
// removeImportedSource).
func (w *WatcherManager) importFile(filePath string, folder *WatchedFolder) (int64, uint64, error) {
	fileData, err := w.app.ReadFileFromPath(filePath)
	if err != nil {
		return 0, 0, err
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
		return 0, 0, err
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
				return 0, 0, err
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

	return clipID, epoch, nil
}

// ProcessExistingFiles imports all existing files in a watched folder. Hidden
// files, downloads still in progress, empty files and files open for writing
// are left alone, as they are for live events.
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
		pinFileIdentity(info)
		// Usually answered by the look removeImportedSource just took for
		// the previous file.
		if watchedFileBusy(filePath, info, watchBusySettleMaxAge()) {
			log.Printf("Skipping file still open for writing: %s", filePath)
			continue
		}

		clipID, epoch, err := w.importFile(filePath, folder)
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

		w.app.removeImportedSource(filePath, info, epoch)
	}

	return nil
}
