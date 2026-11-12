package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

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

// startTestWatcher wires a started WatcherManager into a test app.
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
	defer cleanup()
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
