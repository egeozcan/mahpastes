package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-clipboard/internal/wailsbridge"
)

// publishLinkSnapshot downloads a fresh large clip through a share link, which
// leaves its snapshot in the temp store, and returns the clip's id.
func publishLinkSnapshot(t *testing.T, am *APIManager, app *App, filename string) int64 {
	t.Helper()
	id, want := insertBigClip(t, app, 3<<20, filename)
	link, err := am.CreateShareLink(id, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, want) {
		t.Fatalf("download: status %d, %d bytes", status, len(body))
	}
	if len(tempDirEntries(t, app)) == 0 {
		t.Fatal("no snapshot was published")
	}
	return id
}

// startTestCleanupJob runs the cleanup job on a fast tick. Defer the returned
// stop after the app's cleanup, so the job stops before the database closes.
func startTestCleanupJob(t *testing.T, app *App) (stop func()) {
	t.Helper()
	setForTest(t, &cleanupJobInterval, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	StartCleanupJob(ctx, app.db, app.tempStore, nil)
	return cancel
}

// The expiry reaper deletes rows behind the App's back. A short-lived clip —
// a secret handed out by link — must not leave a plaintext copy of itself in
// the temp store once it has expired.
func TestCleanupJob_ExpiredClipLeavesNoTempFiles(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	id := publishLinkSnapshot(t, am, app, "secret.bin")
	if _, err := app.tempStore.PrepareClipFile(id); err != nil { // and a drag-out copy
		t.Fatal(err)
	}
	if _, err := app.db.Exec("UPDATE clips SET expires_at = datetime('now', '-1 minute') WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}

	defer startTestCleanupJob(t, app)()
	if !waitFor(t, 3*time.Second, func() bool {
		var n int
		_ = app.db.QueryRow("SELECT COUNT(*) FROM clips WHERE id = ?", id).Scan(&n)
		return n == 0
	}) {
		t.Fatal("the reaper never deleted the expired clip")
	}
	if !waitFor(t, time.Second, func() bool { return len(tempDirEntries(t, app)) == 0 }) {
		t.Fatalf("an expired clip left copies behind: %v", tempDirEntries(t, app))
	}
}

// The headless server never drags out, so pruning only on a new copy let idle
// copies sit on disk indefinitely. The cleanup job prunes on its own.
func TestCleanupJob_PrunesIdleSnapshotsWithoutANewCopy(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	publishLinkSnapshot(t, am, app, "photo.bin")
	later := time.Now().Add(3 * time.Hour)
	app.tempStore.now = func() time.Time { return later }

	defer startTestCleanupJob(t, app)()
	if !waitFor(t, 3*time.Second, func() bool { return len(tempDirEntries(t, app)) == 0 }) {
		t.Fatalf("an idle snapshot outlived its lease: %v", tempDirEntries(t, app))
	}
}

const clipDeletePluginSrc = `Plugin = { name = "Clip Delete Test", version = "1.0.0",
  ui = { global_actions = {
    {id = "delete", label = "Delete"},
    {id = "delete_many", label = "Delete many"},
  } } }

function on_ui_action(action_id, clip_ids, options, context)
  local ok, err
  if action_id == "delete" then
    ok, err = clips.delete(options.a)
  else
    ok, err = clips.delete_many({options.a, options.b})
  end
  if not ok then
    return {success = false, error = tostring(err)}
  end
  return {success = true}
end`

// Plugin deletes run raw SQL in the plugin package. They must still drop the
// clip's temp files, or a plugin like expiring-clips leaves a plaintext copy
// of every large clip it removes.
func TestPluginClipsDeleteDropsTempFiles(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	app.bridge = wailsbridge.NewForTesting()
	pm, pluginID := loadWiredTestPlugin(t, app, clipDeletePluginSrc)
	defer pm.Shutdown()

	run := func(action string, a, b int64) {
		t.Helper()
		res, err := pm.ExecuteUIAction(pluginID, action, nil, map[string]interface{}{"a": a, "b": b}, nil)
		if err != nil {
			t.Fatalf("ExecuteUIAction(%s): %v", action, err)
		}
		if !res.Success {
			t.Fatalf("clips.%s from Lua failed: %s", action, res.Error)
		}
	}

	one := publishLinkSnapshot(t, am, app, "one.bin")
	run("delete", one, 0)
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("clips.delete left copies behind: %v", left)
	}

	a := publishLinkSnapshot(t, am, app, "a.bin")
	b := publishLinkSnapshot(t, am, app, "b.bin")
	if _, err := app.tempStore.PrepareClipFile(b); err != nil { // and a drag-out copy
		t.Fatal(err)
	}
	run("delete_many", a, b)
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("clips.delete_many left copies behind: %v", left)
	}
}

// streamSnapshotEntries lists the link snapshots in the store's directory.
func streamSnapshotEntries(t *testing.T, app *App) []string {
	t.Helper()
	var names []string
	for _, name := range tempDirEntries(t, app) {
		if _, ok := parseStreamSnapshotName(name); ok {
			names = append(names, name)
		}
	}
	return names
}

// At most one link snapshot per clip stays on disk: a new revision's copy
// replaces the record of the old one and removes its file, and one the store
// has no record of — left by an earlier run — is dead, since only the
// in-memory record can lead a stream to it.
func TestTempClipStore_OnlyTheCurrentSnapshotStaysOnDisk(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	id, _ := insertBigClip(t, app, 3<<20, "doc.bin")
	link, err := am.CreateShareLink(id, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()
	if status, _ := fetchShare(t, srv, link.Token); status != http.StatusOK {
		t.Fatalf("download: status %d", status)
	}

	// A writer that changes the bytes without dropping the temp files.
	raw := bytes.Repeat([]byte("v2"), 2<<20)
	if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?", raw, computeContentHash(raw), id); err != nil {
		t.Fatal(err)
	}
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, raw) {
		t.Fatalf("download after raw update: status %d, %d bytes", status, len(body))
	}
	current := streamSnapshotEntries(t, app)
	if len(current) != 1 || current[0] != streamSnapshotName(id, computeContentHash(raw)) {
		t.Fatalf("snapshots on disk = %v, want only the current revision's", current)
	}

	leftover := filepath.Join(app.tempDir, streamSnapshotName(id, strings.Repeat("0", 64)))
	if err := os.WriteFile(leftover, []byte("from an earlier run"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := app.tempStore.Prune(true); err != nil {
		t.Fatal(err)
	}
	if got := streamSnapshotEntries(t, app); len(got) != 1 || got[0] != current[0] {
		t.Fatalf("after prune, snapshots on disk = %v, want just %s", got, current[0])
	}
}

// A link snapshot is on a shorter lease than a drag-out or playback file: it
// only ever saves a later request one copy, and the headless server makes one
// for every large clip its web UI shows.
func TestTempClipStore_SnapshotsOnAShorterLease(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	id := publishLinkSnapshot(t, am, app, "doc.bin")
	prepared, err := app.tempStore.PrepareClipFile(id)
	if err != nil {
		t.Fatal(err)
	}

	later := time.Now().Add(defaultStreamSnapshotTTL + 5*time.Minute) // < defaultTempLeaseTTL
	app.tempStore.now = func() time.Time { return later }
	if err := app.tempStore.Prune(true); err != nil {
		t.Fatal(err)
	}
	if got := streamSnapshotEntries(t, app); len(got) != 0 {
		t.Fatalf("a snapshot idle past its lease survived: %v", got)
	}
	if _, err := os.Stat(prepared.AbsPath); err != nil {
		t.Fatalf("the drag-out file, still under lease, was pruned: %v", err)
	}
}
