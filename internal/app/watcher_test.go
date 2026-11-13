package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The busy and stat seams are read by timer and stat goroutines that can
// outlive a test's watcher, so tests never reassign the variables: they are
// pointed once at hooks that consult an override.
var (
	testFileBusy atomic.Pointer[func(string, os.FileInfo) bool]
	testStatDir  atomic.Pointer[func(string) (os.FileInfo, error)]
)

func init() {
	watchedFileBusy = func(path string, info os.FileInfo, maxAge time.Duration) bool {
		if f := testFileBusy.Load(); f != nil {
			return (*f)(path, info)
		}
		return fileOpenForWriting(path, info, maxAge)
	}
	statWatchDir = func(path string) (os.FileInfo, error) {
		if f := testStatDir.Load(); f != nil {
			return (*f)(path)
		}
		return os.Stat(path)
	}
}

func setTestStatDir(t *testing.T, f func(string) (os.FileInfo, error)) {
	t.Helper()
	testStatDir.Store(&f)
	t.Cleanup(func() { testStatDir.Store(nil) })
}

// watchEventRecorder is a bridge that records the events the watcher emits.
type watchEventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name string
	data []any
}

func (r *watchEventRecorder) Emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name: name, data: data})
}

func (r *watchEventRecorder) On(string, func(...any)) {}

// count returns how many events called name were emitted.
func (r *watchEventRecorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev.name == name {
			n++
		}
	}
	return n
}

// statusChanges returns every folder change reported by "watch:status".
func (r *watchEventRecorder) statusChanges() []watchFolderChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []watchFolderChange
	for _, ev := range r.events {
		if ev.name != "watch:status" || len(ev.data) == 0 {
			continue
		}
		payload, ok := ev.data[0].(map[string]interface{})
		if !ok {
			continue
		}
		if changes, ok := payload["changes"].([]watchFolderChange); ok {
			out = append(out, changes...)
		}
	}
	return out
}

// shrinkWatchTimings makes the debounce short and, unless rearm is set, the
// periodic re-arm effectively off so tests drive it explicitly.
func shrinkWatchTimings(t *testing.T, rearm time.Duration) {
	t.Helper()
	oldDebounce, oldRearm := watchDebounceDelay, watchRearmInterval
	watchDebounceDelay = 60 * time.Millisecond
	if rearm == 0 {
		rearm = time.Hour
	}
	watchRearmInterval = rearm
	t.Cleanup(func() {
		watchDebounceDelay, watchRearmInterval = oldDebounce, oldRearm
	})
}

// startTestWatcher wires a started WatcherManager into a test app. Tests
// register the app's cleanup with t.Cleanup before calling it, so the watcher
// is stopped (LIFO) before the database closes.
func startTestWatcher(t *testing.T, a *App) (*WatcherManager, *watchEventRecorder) {
	t.Helper()
	rec := &watchEventRecorder{}
	a.SetBridge(rec)
	wm, err := NewWatcherManager(a)
	if err != nil {
		t.Fatalf("NewWatcherManager: %v", err)
	}
	a.watcherManager = wm
	if err := wm.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(wm.Stop)
	return wm, rec
}

// addWatchDir registers dir as a watch folder importing everything.
func addWatchDir(t *testing.T, a *App, dir string) *WatchedFolder {
	t.Helper()
	f, err := a.AddWatchedFolder(WatchedFolderConfig{Path: dir, FilterMode: "all", FilterPresets: []string{}})
	if err != nil {
		t.Fatalf("AddWatchedFolder: %v", err)
	}
	if err := a.RefreshWatches(); err != nil {
		t.Fatalf("RefreshWatches: %v", err)
	}
	return f
}

func watchClipCount(t *testing.T, a *App) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n); err != nil {
		t.Fatalf("count clips: %v", err)
	}
	return n
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// expectImported writes a file into dir and waits until it is stored and the
// source is removed.
func expectImported(t *testing.T, a *App, dir, name string) {
	t.Helper()
	before := watchClipCount(t, a)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("content of "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, name+" to be imported", func() bool {
		_, err := os.Stat(p)
		return watchClipCount(t, a) == before+1 && os.IsNotExist(err)
	})
}

// expectLeftAlone waits several debounce periods and checks nothing was
// imported and the file is still there.
func expectLeftAlone(t *testing.T, a *App, path string, wantClips int) {
	t.Helper()
	time.Sleep(6 * watchDebounceDelay)
	if got := watchClipCount(t, a); got != wantClips {
		t.Fatalf("clip count = %d, want %d (file %s was imported)", got, wantClips, filepath.Base(path))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s was removed: %v", filepath.Base(path), err)
	}
}

func TestWatcherImportsAndRemovesNewFile(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)
	expectImported(t, a, dir, "note.txt")
}

// A backup restore calls Stop then Start on the same manager. Stop used to
// close the fsnotify watcher and keep the active-watch map, so Start ran on a
// closed watcher and skipped every folder: watching was dead until the app
// restarted.
func TestWatcherKeepsWatchingAcrossStopStart(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)
	expectImported(t, a, dir, "before.txt")

	for i := 0; i < 3; i++ {
		wm.Stop()
		if err := wm.Start(); err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		expectImported(t, a, dir, "after-restart-"+string(rune('a'+i))+".txt")
	}
}

// The real restore path: RestoreBackup stops and restarts the watcher and
// pauses restored folders; resuming one must watch it again.
func TestWatcherWatchesAgainAfterBackupRestore(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	dataDir := os.Getenv("MAHPASTES_DATA_DIR")
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	startTestWatcher(t, a)

	dir := t.TempDir()
	f := addWatchDir(t, a, dir)
	expectImported(t, a, dir, "before.txt")

	backup := filepath.Join(t.TempDir(), "backup.zip")
	if err := a.CreateBackup(backup); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if err := a.RestoreBackup(backup, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	// Restored folders come back paused.
	if err := a.SetFolderPaused(f.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshWatches(); err != nil {
		t.Fatal(err)
	}
	expectImported(t, a, dir, "after-restore.txt")
}

func TestWatcherSkipsInProgressDownloads(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)

	partial := filepath.Join(dir, "report.pdf.crdownload")
	if err := os.WriteFile(partial, []byte("half a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"movie.mkv.part", "Setup.exe.PARTIAL", "draft.txt~", "x.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expectLeftAlone(t, a, partial, 0)

	// The browser renames the finished download to its final name.
	final := filepath.Join(dir, "report.pdf")
	if err := os.Rename(partial, final); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the finished download to be imported", func() bool {
		_, err := os.Stat(final)
		return watchClipCount(t, a) == 1 && os.IsNotExist(err)
	})
}

// Firefox creates an empty placeholder under the final name next to the
// .part file, then renames the .part over it.
func TestWatcherSkipsEmptyPlaceholderUntilItHasContent(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)

	final := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(final, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, "photo.png.part")
	if err := os.WriteFile(part, []byte("png bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectLeftAlone(t, a, final, 0)

	if err := os.Rename(part, final); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the completed file to be imported", func() bool {
		_, err := os.Stat(final)
		return watchClipCount(t, a) == 1 && os.IsNotExist(err)
	})
}

// A file still growing when its debounce expires (some writers produce no
// event per write) is given another round instead of being imported short.
func TestWatcherWaitsForFileToStopChanging(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	// The file exists before the folder is watched, so no fsnotify event
	// competes with the debounce armed by hand below.
	dir := t.TempDir()
	p := filepath.Join(dir, "growing.bin")
	if err := os.WriteFile(p, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	addWatchDir(t, a, dir)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an event-less writer: arm the debounce with a snapshot that
	// no longer matches the file, as if it grew after its last event.
	wm.mu.RLock()
	gen := wm.gen
	wm.mu.RUnlock()
	wm.debounceFile(gen, p, info)
	wm.mu.Lock()
	if pf := wm.debounceMap[p]; pf != nil {
		pf.size = 1
	}
	wm.mu.Unlock()

	time.Sleep(watchDebounceDelay + watchDebounceDelay/2)
	wm.mu.RLock()
	_, stillPending := wm.debounceMap[p]
	wm.mu.RUnlock()
	if !stillPending || watchClipCount(t, a) != 0 {
		t.Fatal("a file whose size changed during the debounce was not given another round")
	}
	waitUntil(t, 5*time.Second, "the settled file to be imported", func() bool {
		_, err := os.Stat(p)
		return watchClipCount(t, a) == 1 && os.IsNotExist(err)
	})
}

// A watched folder that is deleted and recreated loses its fsnotify watch.
// It used to stay in activeWatches, so no refresh ever re-added it.
func TestWatcherRearmsDeletedAndRecreatedFolder(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	_, rec := startTestWatcher(t, a)

	dir := filepath.Join(t.TempDir(), "inbox")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := addWatchDir(t, a, dir)
	expectImported(t, a, dir, "first.txt")

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	// The removal is reported: the folder shows why it is not watched.
	waitUntil(t, 5*time.Second, "the missing folder to be reported", func() bool {
		return a.watcherManager.folderWatchError(f.ID) == errFolderMissing
	})
	if st := a.GetWatchStatus(); st.UnavailableCount != 1 || st.ActiveCount != 0 {
		t.Fatalf("status = %+v, want 1 unavailable, 0 active", st)
	}
	foundMissing := false
	for _, c := range rec.statusChanges() {
		if c.FolderID == f.ID && c.Error == errFolderMissing && !c.Watching {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("no watch:status change reported the missing folder: %+v", rec.statusChanges())
	}

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The periodic pass re-arms it.
	if err := a.watcherManager.refreshWatches(); err != nil {
		t.Fatal(err)
	}
	if got := a.watcherManager.folderWatchError(f.ID); got != "" {
		t.Fatalf("watch error after re-arm = %q, want none", got)
	}
	recovered := false
	for _, c := range rec.statusChanges() {
		if c.FolderID == f.ID && c.Watching && c.Recovered {
			recovered = true
		}
	}
	if !recovered {
		t.Fatalf("no watch:status change reported the recovery: %+v", rec.statusChanges())
	}
	expectImported(t, a, dir, "second.txt")
}

// A folder missing when watching starts (an unmounted volume) is watched as
// soon as it appears, by the periodic re-arm alone.
func TestWatcherRearmLoopPicksUpFolderThatAppears(t *testing.T) {
	shrinkWatchTimings(t, 100*time.Millisecond)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	_, _ = startTestWatcher(t, a)

	dir := filepath.Join(t.TempDir(), "volume")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := addWatchDir(t, a, dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the folder to be reported missing", func() bool {
		return a.watcherManager.folderWatchError(f.ID) == errFolderMissing
	})
	folders, err := a.GetWatchedFolders()
	if err != nil || len(folders) != 1 || folders[0].WatchError != errFolderMissing {
		t.Fatalf("GetWatchedFolders = %+v, %v; want watch_error %q", folders, err, errFolderMissing)
	}

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the re-arm loop to watch the folder again", func() bool {
		armed, _, _ := a.watcherManager.watchState()
		return armed[f.ID]
	})
	expectImported(t, a, dir, "arrived.txt")
}

// A directory replaced under the same path without a removal event reaching
// the watcher (another inode now) is detected by the identity check.
func TestWatcherRearmsReplacedDirectory(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dir := t.TempDir()
	f := addWatchDir(t, a, dir)

	// Pretend the armed watch belongs to another directory.
	other := t.TempDir()
	otherInfo, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	wm.mu.Lock()
	aw := wm.activeWatches[f.ID]
	aw.info = otherInfo
	wm.activeWatches[f.ID] = aw
	wm.mu.Unlock()

	if err := wm.refreshWatches(); err != nil {
		t.Fatal(err)
	}
	wm.mu.RLock()
	got := wm.activeWatches[f.ID].info
	wm.mu.RUnlock()
	dirInfo, _ := os.Stat(dir)
	if !os.SameFile(got, dirInfo) {
		t.Fatal("the watch was not re-armed on the directory now at the path")
	}
	expectImported(t, a, dir, "after-replace.txt")
}

func TestWatcherRefreshAfterStopIsHarmless(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)
	addWatchDir(t, a, t.TempDir())

	wm.Stop()
	if err := a.RefreshWatches(); err != nil {
		t.Fatalf("RefreshWatches after Stop: %v", err)
	}
	wm.Stop() // a second Stop is a no-op
	if st := a.GetWatchStatus(); st.TotalCount != 1 {
		t.Fatalf("status after Stop = %+v", st)
	}
}

// Stop must cancel pending imports: a debounce armed before Stop must not
// import (and delete) a file once watching has stopped.
func TestWatcherStopCancelsPendingImports(t *testing.T) {
	shrinkWatchTimings(t, 0)
	watchDebounceDelay = 300 * time.Millisecond
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)
	p := filepath.Join(dir, "late.txt")
	if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the file to be pending", func() bool {
		wm.mu.RLock()
		defer wm.mu.RUnlock()
		return wm.debounceMap[p] != nil
	})
	wm.Stop()
	expectLeftAlone(t, a, p, 0)
}

func TestProcessExistingFilesSkipsPartialAndEmptyFiles(t *testing.T) {
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	keep := []string{
		write("big.iso.crdownload", []byte("partial")),
		write("placeholder.pdf", nil),
		write(".hidden", []byte("x")),
	}
	done := write("done.txt", []byte("complete"))

	f, err := a.AddWatchedFolder(WatchedFolderConfig{Path: dir, FilterMode: "all", FilterPresets: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := wm.ProcessExistingFiles(f.ID); err != nil {
		t.Fatalf("ProcessExistingFiles: %v", err)
	}
	if got := watchClipCount(t, a); got != 1 {
		t.Fatalf("clip count = %d, want 1", got)
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Fatalf("done.txt was not removed after import: %v", err)
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(p), err)
		}
	}
}

// A restore stops the watcher without waiting for an import already running.
// If it lands after that import stored its clip, the clip is gone with the old
// library, so the source file must stay: it is the only copy left.
func TestRemoveImportedSourceKeepsFileAfterRestore(t *testing.T) {
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)

	dir := t.TempDir()
	write := func(name string) (string, os.FileInfo) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("data of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return p, pinFileIdentity(info)
	}

	epoch := a.currentRestoreEpoch()
	kept, keptInfo := write("imported-before-restore.txt")
	a.backupRestoreMu.Lock()
	a.restoreEpoch++ // what a committed RestoreBackup does
	a.backupRestoreMu.Unlock()
	a.removeImportedSource(kept, keptInfo, epoch)
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("source removed although a restore replaced its clip: %v", err)
	}

	removed, removedInfo := write("imported-after-restore.txt")
	a.removeImportedSource(removed, removedInfo, a.currentRestoreEpoch())
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Fatalf("source not removed after an import with no restore in between: %v", err)
	}

	// A file that changed after it was read stays too.
	changed, changedInfo := write("changed.txt")
	if err := os.WriteFile(changed, []byte("longer data written after the read"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.removeImportedSource(changed, changedInfo, a.currentRestoreEpoch())
	if _, err := os.Stat(changed); err != nil {
		t.Fatalf("source removed although it changed during the import: %v", err)
	}
}

func TestIsInProgressName(t *testing.T) {
	for name, want := range map[string]bool{
		"report.pdf.crdownload":       true,
		"Unconfirmed 1234.crdownload": true,
		"movie.mkv.part":              true,
		"MOVIE.MKV.PART":              true,
		"file.partial":                true,
		"archive.zip.download":        true,
		"x.opdownload":                true,
		"save.tmp":                    true,
		"notes.txt~":                  true,
		"report.pdf":                  false,
		"partition.png":               false,
		"tmp.txt":                     false,
		"apart":                       false,
	} {
		if got := isInProgressName(name); got != want {
			t.Errorf("isInProgressName(%q) = %v, want %v", name, got, want)
		}
	}
}

// Keep the recorder honest: the payload shape the frontend reads.
func TestWatchStatusEventPayload(t *testing.T) {
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	rec := &watchEventRecorder{}
	a.SetBridge(rec)
	a.emitWatchStatus(nil, "boom")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 1 || rec.events[0].name != "watch:status" {
		t.Fatalf("events = %+v", rec.events)
	}
	payload := rec.events[0].data[0].(map[string]interface{})
	if payload["error"] != "boom" {
		t.Fatalf("error = %v", payload["error"])
	}
	if changes, ok := payload["changes"].([]watchFolderChange); !ok || changes == nil {
		t.Fatalf("changes = %#v, want an empty list (never null)", payload["changes"])
	}
}

// A re-arm pass stuck on a slow folder (a stat on a dead network mount) must
// not hang Stop, and so quitting the app; when it finishes it applies nothing.
func TestWatcherStopDoesNotWaitForStuckRearm(t *testing.T) {
	shrinkWatchTimings(t, 0)
	oldWait := watchStopRearmWait
	watchStopRearmWait = 100 * time.Millisecond
	t.Cleanup(func() { watchStopRearmWait = oldWait })
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)
	addWatchDir(t, a, t.TempDir())

	// Hold the refresh lock so the kicked pass blocks inside refreshWatches.
	wm.refreshMu.Lock()
	wm.kickRearm()
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		wm.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		wm.refreshMu.Unlock()
		t.Fatal("Stop waited for a stuck re-arm pass")
	}
	wm.refreshMu.Unlock()

	time.Sleep(100 * time.Millisecond)
	armed, _, running := wm.watchState()
	if running || len(armed) != 0 {
		t.Fatalf("after Stop: running=%v armed=%v; the late pass must apply nothing", running, armed)
	}
}

// The restore sequence while a re-arm pass is stuck: the next generation
// watches again, and its own lost-watch kicks reach its own re-arm loop (the
// left-behind loop must not take them).
func TestWatcherRestartAfterStuckRearmStillRearms(t *testing.T) {
	shrinkWatchTimings(t, 0) // periodic re-arm off: only kicks re-arm
	oldWait := watchStopRearmWait
	watchStopRearmWait = 50 * time.Millisecond
	t.Cleanup(func() { watchStopRearmWait = oldWait })
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)
	dir := filepath.Join(t.TempDir(), "inbox")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := addWatchDir(t, a, dir)

	wm.refreshMu.Lock()
	wm.kickRearm()
	time.Sleep(30 * time.Millisecond)
	wm.Stop()
	wm.refreshMu.Unlock()
	if err := wm.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	expectImported(t, a, dir, "after-restart.txt")

	// Deleting the folder kicks the new generation's loop, which records it
	// as missing even though the periodic pass is effectively off.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the kicked pass to report the folder missing", func() bool {
		return wm.folderWatchError(f.ID) == errFolderMissing
	})
}

// The restore sequence while a re-arm pass of the old generation is still
// stuck: Start (which a restore runs holding backupRestoreMu) and later
// refreshes must not queue behind it.
func TestWatcherStartNotBlockedByStuckRearmOfEndedGeneration(t *testing.T) {
	shrinkWatchTimings(t, 0)
	oldWait := watchStopRearmWait
	watchStopRearmWait = 50 * time.Millisecond
	t.Cleanup(func() { watchStopRearmWait = oldWait })
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)
	dir := t.TempDir()
	addWatchDir(t, a, dir)

	// Stand-in for a pass stuck in a stat: it holds its generation's lock.
	stuck := wm.refreshMu
	stuck.Lock()
	released := false
	release := func() {
		if !released {
			released = true
			stuck.Unlock()
		}
	}
	defer release()
	wm.kickRearm()
	time.Sleep(30 * time.Millisecond)
	wm.Stop()

	done := make(chan error, 2)
	go func() { done <- wm.Start() }()
	go func() { done <- a.RefreshWatches() }()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Start/RefreshWatches blocked behind the ended generation's re-arm pass")
		}
	}
	expectImported(t, a, dir, "after-restart.txt")

	// The old pass, once it gets going, applies nothing.
	release()
	time.Sleep(100 * time.Millisecond)
	if armed, failed, running := wm.watchState(); !running || len(armed) != 1 || len(failed) != 0 {
		t.Fatalf("after the old pass finished: running=%v armed=%v failed=%v", running, armed, failed)
	}
	expectImported(t, a, dir, "after-old-pass.txt")
}

// A stat that hangs (a dead network mount) costs a pass watchStatTimeout, not
// forever, and only one stat of that folder is ever in flight.
func TestWatcherRefreshNotStuckBehindHangingStat(t *testing.T) {
	shrinkWatchTimings(t, 0)
	oldTimeout := watchStatTimeout
	watchStatTimeout = 100 * time.Millisecond
	t.Cleanup(func() { watchStatTimeout = oldTimeout })
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dead := filepath.Join(t.TempDir(), "dead")
	if err := os.Mkdir(dead, 0o755); err != nil {
		t.Fatal(err)
	}
	unblock := make(chan struct{})
	var deadStats atomic.Int32
	setTestStatDir(t, func(path string) (os.FileInfo, error) {
		if path == dead {
			deadStats.Add(1)
			<-unblock
		}
		return os.Stat(path)
	})
	deadFolder := addWatchDir(t, a, dead)
	live := t.TempDir()
	addWatchDir(t, a, live)

	done := make(chan error, 1)
	go func() { done <- wm.refreshWatches() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a refresh waited for a hanging stat")
	}
	if got := wm.folderWatchError(deadFolder.ID); got != errWatchStatTimeout.Error() {
		t.Fatalf("watch error = %q, want %q", got, errWatchStatTimeout)
	}
	if n := deadStats.Load(); n != 1 {
		t.Fatalf("%d stats of the hanging folder in flight, want 1", n)
	}
	expectImported(t, a, live, "live.txt")

	close(unblock)
	waitUntil(t, 5*time.Second, "the folder to be watched once its stat returns", func() bool {
		if err := wm.refreshWatches(); err != nil {
			t.Fatal(err)
		}
		armed, _, _ := wm.watchState()
		return armed[deadFolder.ID]
	})
	expectImported(t, a, dead, "back.txt")
}

// deadWatchBackend wraps a real watcher and can make one path behave the way
// fsnotify's kqueue backend does after the volume was unmounted: no events,
// Remove fails and leaves the path listed, Add re-registers the dead
// descriptor and fails.
type deadWatchBackend struct {
	watchBackend
	mu   sync.Mutex
	dead map[string]bool
}

func (b *deadWatchBackend) kill(path string) {
	_ = b.watchBackend.Remove(path)
	b.mu.Lock()
	b.dead[path] = true
	b.mu.Unlock()
}

func (b *deadWatchBackend) isDead(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dead[path]
}

func (b *deadWatchBackend) Add(name string) error {
	if b.isDead(name) {
		return syscall.EBADF
	}
	return b.watchBackend.Add(name)
}

func (b *deadWatchBackend) Remove(name string) error {
	if b.isDead(name) {
		return syscall.ENOENT
	}
	return b.watchBackend.Remove(name)
}

func (b *deadWatchBackend) WatchList() []string {
	list := b.watchBackend.WatchList()
	b.mu.Lock()
	defer b.mu.Unlock()
	for p := range b.dead {
		list = append(list, p)
	}
	return list
}

// useDeadWatchBackends makes every watcher the manager creates killable and
// returns them in creation order.
func useDeadWatchBackends(t *testing.T) func() []*deadWatchBackend {
	t.Helper()
	orig := newFsWatch
	var mu sync.Mutex
	var made []*deadWatchBackend
	newFsWatch = func() (*fsWatch, error) {
		fw, err := orig()
		if err != nil {
			return nil, err
		}
		b := &deadWatchBackend{watchBackend: fw.watchBackend, dead: map[string]bool{}}
		fw.watchBackend = b
		mu.Lock()
		made = append(made, b)
		mu.Unlock()
		return fw, nil
	}
	t.Cleanup(func() { newFsWatch = orig })
	return func() []*deadWatchBackend {
		mu.Lock()
		defer mu.Unlock()
		return append([]*deadWatchBackend(nil), made...)
	}
}

// A pass that runs while the volume is away: fsnotify will not release the
// dead watch, and re-adding the path later only re-registers it. The watcher
// has to be replaced, or the folder is dead until the app restarts.
func TestWatcherReplacesWatcherThatKeepsDeadWatch(t *testing.T) {
	shrinkWatchTimings(t, 0)
	backends := useDeadWatchBackends(t)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, rec := startTestWatcher(t, a)

	dir := filepath.Join(t.TempDir(), "volume", "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := addWatchDir(t, a, dir)
	other := t.TempDir()
	addWatchDir(t, a, other)
	expectImported(t, a, dir, "first.txt")

	// Unmount: no event, dead descriptors, and the folder is gone.
	var away atomic.Bool
	away.Store(true)
	setTestStatDir(t, func(path string) (os.FileInfo, error) {
		if away.Load() && path == dir {
			return nil, &os.PathError{Op: "stat", Path: path, Err: syscall.ENOENT}
		}
		return os.Stat(path)
	})
	made := backends()
	made[len(made)-1].kill(dir)
	if err := wm.refreshWatches(); err != nil {
		t.Fatal(err)
	}
	if got := wm.folderWatchError(f.ID); got != errFolderMissing {
		t.Fatalf("while away: watch error = %q, want %q", got, errFolderMissing)
	}
	if got := len(backends()); got != len(made)+1 {
		t.Fatalf("%d watchers created, want %d: the one holding the dead watch must be replaced", got, len(made)+1)
	}
	// A folder that was fine keeps importing through the replacement.
	expectImported(t, a, other, "meanwhile.txt")

	// Remount.
	away.Store(false)
	for i := 0; i < 2; i++ {
		if err := wm.refreshWatches(); err != nil {
			t.Fatal(err)
		}
	}
	if got := wm.folderWatchError(f.ID); got != "" {
		t.Fatalf("after remount: watch error = %q, want none", got)
	}
	if got := len(backends()); got != len(made)+1 {
		t.Fatalf("%d watchers created, want %d: a healthy watcher must not be replaced again", got, len(made)+1)
	}
	expectImported(t, a, dir, "second.txt")

	// One "not found", one recovery: no new report per pass.
	var reports int
	for _, c := range rec.statusChanges() {
		if c.FolderID == f.ID && c.Error != "" {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("%d error reports for the folder, want 1: %+v", reports, rec.statusChanges())
	}

	// Stop still ends the replacement's event loop.
	wm.Stop()
	if _, _, running := wm.watchState(); running {
		t.Fatal("still running after Stop")
	}
}

type fakeDirProbe struct{ dead, closed atomic.Bool }

func (p *fakeDirProbe) alive() bool { return !p.dead.Load() && !p.closed.Load() }
func (p *fakeDirProbe) close()      { p.closed.Store(true) }

// Unmounted and mounted again between two passes: the folder stats the same
// and fsnotify still lists it, but no event will ever come. Only the probe
// shows it. The folder used to be reported as watched and import nothing.
func TestWatcherReplacesWatcherWhenWatchWasRevoked(t *testing.T) {
	shrinkWatchTimings(t, 0)
	backends := useDeadWatchBackends(t)
	oldProbe := openDirProbe
	var mu sync.Mutex
	probes := map[string][]*fakeDirProbe{}
	openDirProbe = func(path string) dirProbe {
		p := &fakeDirProbe{}
		mu.Lock()
		probes[path] = append(probes[path], p)
		mu.Unlock()
		return p
	}
	t.Cleanup(func() { openDirProbe = oldProbe })
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, _ := startTestWatcher(t, a)

	dir := t.TempDir()
	f := addWatchDir(t, a, dir)
	expectImported(t, a, dir, "first.txt")

	made := backends()
	made[len(made)-1].kill(dir)
	mu.Lock()
	first := probes[dir][0]
	mu.Unlock()
	first.dead.Store(true)

	if err := wm.refreshWatches(); err != nil {
		t.Fatal(err)
	}
	if armed, failed, _ := wm.watchState(); !armed[f.ID] || len(failed) != 0 {
		t.Fatalf("after the pass: armed=%v failed=%v", armed, failed)
	}
	expectImported(t, a, dir, "second.txt")

	if !first.closed.Load() {
		t.Fatal("the dead watch's probe was not closed")
	}
	wm.Stop()
	mu.Lock()
	defer mu.Unlock()
	for path, ps := range probes {
		for i, p := range ps {
			if !p.closed.Load() {
				t.Fatalf("probe %d of %s still open after Stop", i, path)
			}
		}
	}
}

// The real thing, on a disk image. Opt-in: it mounts a volume.
func TestWatcherRearmsAfterVolumeUnmountedAndRemounted(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("MAHPASTES_TEST_DISKIMAGE") == "" {
		t.Skip("set MAHPASTES_TEST_DISKIMAGE=1 on macOS to run")
	}
	for _, passWhileAway := range []bool{true, false} {
		name := "remount before the next pass"
		if passWhileAway {
			name = "pass while away"
		}
		t.Run(name, func(t *testing.T) {
			shrinkWatchTimings(t, 0)
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			img, mnt := filepath.Join(base, "v.dmg"), filepath.Join(base, "mnt")
			if err := os.Mkdir(mnt, 0o755); err != nil {
				t.Fatal(err)
			}
			hdiutil := func(args ...string) {
				t.Helper()
				if out, err := exec.Command("hdiutil", args...).CombinedOutput(); err != nil {
					t.Fatalf("hdiutil %v: %v\n%s", args, err, out)
				}
			}
			hdiutil("create", "-size", "20m", "-fs", "APFS", "-volname", "mpwatchtest", img)
			attach := func() { hdiutil("attach", img, "-mountpoint", mnt, "-nobrowse") }
			attach()
			t.Cleanup(func() { exec.Command("hdiutil", "detach", mnt, "-force").Run() })

			a, cleanup := setupTestApp(t)
			t.Cleanup(cleanup)
			wm, _ := startTestWatcher(t, a)
			dir := filepath.Join(mnt, "inbox")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			f := addWatchDir(t, a, dir)
			expectImported(t, a, dir, "first.txt")

			hdiutil("detach", mnt)
			if passWhileAway {
				if err := wm.refreshWatches(); err != nil {
					t.Fatal(err)
				}
				if got := wm.folderWatchError(f.ID); got != errFolderMissing {
					t.Fatalf("while away: watch error = %q, want %q", got, errFolderMissing)
				}
			}
			attach()
			if err := wm.refreshWatches(); err != nil {
				t.Fatal(err)
			}
			if got := wm.folderWatchError(f.ID); got != "" {
				t.Fatalf("after remount: watch error = %q, want none", got)
			}
			expectImported(t, a, dir, "second.txt")
			wm.Stop() // release the volume before the detach in cleanup
		})
	}
}

// One directory listed twice (another letter case on a case-insensitive
// volume, or through a symlink) used to get two watches, and every file was
// imported twice.
func TestWatcherSameDirectoryUnderTwoNamesImportsOnce(t *testing.T) {
	alias := map[string]func(t *testing.T, dir string) string{
		"symlink": func(t *testing.T, dir string) string {
			link := filepath.Join(filepath.Dir(dir), "link")
			if err := os.Symlink(dir, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return link
		},
		"letter case": func(t *testing.T, dir string) string {
			other := filepath.Join(filepath.Dir(dir), "INBOX")
			a, _ := os.Stat(dir)
			b, err := os.Stat(other)
			if err != nil || !os.SameFile(a, b) {
				t.Skip("case-sensitive filesystem")
			}
			return other
		},
	}
	for name, makeAlias := range alias {
		t.Run(name, func(t *testing.T) {
			shrinkWatchTimings(t, 0)
			a, cleanup := setupTestApp(t)
			t.Cleanup(cleanup)
			wm, _ := startTestWatcher(t, a)

			dir := filepath.Join(t.TempDir(), "inbox")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			second := makeAlias(t, dir)
			f1 := addWatchDir(t, a, dir)
			f2 := addWatchDir(t, a, second)

			if list := wm.watcher.WatchList(); len(list) != 1 {
				t.Fatalf("fsnotify watches %v, want one", list)
			}
			if got := wm.folderWatchError(f2.ID); got != errSameFolderPrefix+dir {
				t.Fatalf("second entry's watch error = %q, want %q", got, errSameFolderPrefix+dir)
			}
			expectImported(t, a, dir, "once.txt")
			time.Sleep(6 * watchDebounceDelay)
			if got := watchClipCount(t, a); got != 1 {
				t.Fatalf("clip count = %d, want 1", got)
			}

			// With the first entry paused, the second one takes over.
			if err := a.SetFolderPaused(f1.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := a.RefreshWatches(); err != nil {
				t.Fatal(err)
			}
			if armed, _, _ := wm.watchState(); !armed[f2.ID] || armed[f1.ID] {
				t.Fatalf("armed = %v, want only the second entry", armed)
			}
			expectImported(t, a, second, "via-second.txt")
			time.Sleep(6 * watchDebounceDelay)
			if got := watchClipCount(t, a); got != 2 {
				t.Fatalf("clip count = %d, want 2", got)
			}
		})
	}
}

// A file some process still has open for writing is neither imported nor
// removed, however long the writer pauses; the UI is told why, once.
func TestWatcherLeavesFileAloneWhileAWriterHoldsIt(t *testing.T) {
	shrinkWatchTimings(t, 0)
	oldMax, oldReport := watchBusyRecheckMax, watchBusyReportAfter
	watchBusyRecheckMax = 100 * time.Millisecond
	watchBusyReportAfter = 150 * time.Millisecond
	var busy atomic.Bool
	busy.Store(true)
	busyFn := func(path string, _ os.FileInfo) bool {
		return busy.Load() && strings.HasSuffix(path, ".bin")
	}
	testFileBusy.Store(&busyFn)
	t.Cleanup(func() {
		testFileBusy.Store(nil)
		watchBusyRecheckMax, watchBusyReportAfter = oldMax, oldReport
	})
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	wm, rec := startTestWatcher(t, a)

	dir := t.TempDir()
	f := addWatchDir(t, a, dir)
	p := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(p, []byte("first half"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectLeftAlone(t, a, p, 0)

	// Neither does the scan of existing files touch it...
	if err := wm.ProcessExistingFiles(f.ID); err != nil {
		t.Fatal(err)
	}
	expectLeftAlone(t, a, p, 0)
	// ...nor the removal after an import that raced the writer's open.
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	a.removeImportedSource(p, pinFileIdentity(info), a.currentRestoreEpoch())
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("a file open for writing was removed: %v", err)
	}

	// Held for longer than watchBusyReportAfter: reported, and only once.
	waitUntil(t, 5*time.Second, "the held file to be reported", func() bool {
		return rec.count("watch:error") > 0
	})
	time.Sleep(4 * watchBusyRecheckMax)
	if n := rec.count("watch:error"); n != 1 {
		t.Fatalf("%d reports of the held file, want 1", n)
	}
	if got := watchClipCount(t, a); got != 0 {
		t.Fatalf("clip count = %d, want 0", got)
	}

	// The writer closes without writing again: no event, the recheck finds it.
	busy.Store(false)
	waitUntil(t, 5*time.Second, "the file to be imported once the writer is done", func() bool {
		_, err := os.Stat(p)
		return watchClipCount(t, a) == 1 && os.IsNotExist(err)
	})
}

// The real check: a writer that pauses for longer than the debounce with the
// file open used to get a truncated clip and its file deleted from under it.
func TestWatcherWaitsForStalledWriter(t *testing.T) {
	if !fileBusyCheckSupported {
		t.Skip("this platform cannot tell whether a file is open for writing")
	}
	shrinkWatchTimings(t, 0)
	a, cleanup := setupTestApp(t)
	t.Cleanup(cleanup)
	startTestWatcher(t, a)

	dir := t.TempDir()
	addWatchDir(t, a, dir)
	p := filepath.Join(dir, "big.bin")
	w, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.WriteString("first half"); err != nil {
		t.Fatal(err)
	}
	expectLeftAlone(t, a, p, 0)
	if _, err := w.WriteString(" second half"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "the finished file to be imported", func() bool {
		_, err := os.Stat(p)
		return watchClipCount(t, a) == 1 && os.IsNotExist(err)
	})
	var size int64
	if err := a.db.QueryRow(`SELECT octet_length(data) FROM clips`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != int64(len("first half second half")) {
		t.Fatalf("stored %d bytes, want %d", size, len("first half second half"))
	}
}

func TestFileOpenForWriting(t *testing.T) {
	if !fileBusyCheckSupported {
		t.Skip("this platform cannot tell whether a file is open for writing")
	}
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fileOpenForWriting(p, info, 0) {
		t.Fatal("reported open for writing with no handle open")
	}
	r, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if fileOpenForWriting(p, info, 0) {
		t.Fatal("a reader counted as a writer")
	}
	w, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !fileOpenForWriting(p, info, 0) {
		w.Close()
		t.Fatal("a writer was not seen")
	}
	w.Close()
	if fileOpenForWriting(p, info, 0) {
		t.Fatal("still reported open for writing after the writer closed")
	}
}

// A write that opens and closes while the busy walk runs is never seen as a
// writer; the source must still be kept.
func TestRemoveImportedSourceKeepsFileWrittenDuringBusyCheck(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	p := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(p, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	busyFn := func(path string, _ os.FileInfo) bool {
		if err := os.WriteFile(path, []byte("first and more"), 0o644); err != nil {
			t.Errorf("write during the check: %v", err)
		}
		return false
	}
	testFileBusy.Store(&busyFn)
	defer testFileBusy.Store(nil)

	a.removeImportedSource(p, pinFileIdentity(info), a.currentRestoreEpoch())

	if got, err := os.ReadFile(p); err != nil || string(got) != "first and more" {
		t.Fatalf("source after removal = %q, %v; want it kept with the new content", got, err)
	}
}
