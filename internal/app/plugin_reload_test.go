package app

import (
	"testing"
	"time"

	"go-clipboard/plugin"
)

// tagCreatedCounterSrc counts its on_tag_created deliveries in storage.
const tagCreatedCounterSrc = `Plugin = { name = "Created Counter", version = "1.0.0",
  events = {"tag:created"} }

function on_tag_created(data)
  local n = tonumber(storage.get("created_runs") or "0") or 0
  storage.set("created_runs", tostring(n + 1))
end`

// RestoreBackup reloads plugins with LoadPlugins while the ones loaded at
// startup are still registered. LoadPlugins used to add each plugin again —
// a second entry in the subscriber list and a second sandbox — so after any
// restore every event reached every plugin twice (mahresources' auto-upload
// uploaded each new clip twice) until the app restarted.
func TestLoadPluginsAgainDeliversEachEventOnce(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagCreatedCounterSrc)
	defer pm.Shutdown()

	if err := pm.LoadPlugins(); err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if n := len(pm.GetPlugins()); n != 1 {
		t.Fatalf("%d plugins loaded after a reload, want 1", n)
	}
	mustCreateTag(t, app, "after-reload")

	if got := settledCount(t, app, pluginID, "created_runs", 1); got != 1 {
		t.Fatalf("on_tag_created ran %d times for one tag, want 1: the reload left the old copy subscribed", got)
	}
}

// The reload replaces plugins while RestoreBackup holds backupRestoreMu for
// writing, and an old copy may be busy: an async UI action runs for up to
// MaxUIActionTime, and a handler can be parked in a tags.* host call waiting
// for that very lock. Replacing a copy must not wait for its sandbox to close,
// or the restore stalls behind the old handler — or deadlocks with it.
func TestLoadPluginsDoesNotWaitForOldHandlers(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, slowFailingActionSrc)
	defer pm.Shutdown()

	if _, err := pm.ExecuteUIAction(pluginID, "slow", nil, nil, nil); err != nil {
		t.Fatalf("ExecuteUIAction: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // the old copy is now busy for 0.9-1.9s more

	app.backupRestoreMu.Lock()
	defer app.backupRestoreMu.Unlock()
	start := time.Now()
	if err := pm.LoadPlugins(); err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("LoadPlugins took %v: it waited for the old copy's running action", took)
	}
	if n := len(pm.GetPlugins()); n != 1 {
		t.Fatalf("%d plugins loaded after the reload, want 1", n)
	}
}

// slowFailingActionSrc has an async action that runs for 1-2 s (utils.time
// has whole-second resolution) and then fails.
const slowFailingActionSrc = `Plugin = { name = "Slow Failer", version = "1.0.0",
  ui = { global_actions = { {id = "slow", label = "Slow", async = true} } } }

function on_ui_action(action_id, clip_ids, options, context)
  local stop = utils.time() + 2
  while utils.time() < stop do end
  error("boom")
end`

// A run that started on a copy a reload has since replaced finishes against
// the plugin's id. Charged to that id, its failure counted against — and, with
// a restored error_count of 2, unloaded and marked 'error' — the copy that had
// just been loaded in its place.
func TestStaleCopyFailureDoesNotDisableTheReloadedPlugin(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, slowFailingActionSrc)
	defer pm.Shutdown()

	if _, err := pm.ExecuteUIAction(pluginID, "slow", nil, nil, nil); err != nil {
		t.Fatalf("ExecuteUIAction: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // the old copy is now running the action
	if _, err := app.db.Exec(`UPDATE plugins SET error_count = 2 WHERE id = ?`, pluginID); err != nil {
		t.Fatal(err)
	}
	if err := pm.LoadPlugins(); err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	time.Sleep(2500 * time.Millisecond) // the old copy's action has failed by now (≤2s)
	if n := len(pm.GetPlugins()); n != 1 {
		t.Fatalf("%d plugins loaded after a stale copy's failure, want the reloaded one", n)
	}
	var status string
	var errs int
	if err := app.db.QueryRow(`SELECT status, error_count FROM plugins WHERE id = ?`, pluginID).Scan(&status, &errs); err != nil {
		t.Fatal(err)
	}
	if status == "error" || errs != 2 {
		t.Fatalf("plugin status %q, error_count %d after a replaced copy failed — want it untouched", status, errs)
	}
}

// LoadPlugins replaces what is loaded only once it knows what to load: if the
// query fails, the copies already running stay, rather than leaving the app
// with no plugins until it restarts.
func TestLoadPluginsKeepsLoadedCopiesWhenItCannotRead(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, _ := loadWiredTestPlugin(t, app, tagCreatedCounterSrc)
	defer pm.Shutdown()

	if _, err := app.db.Exec(`ALTER TABLE plugins RENAME TO plugins_unreadable`); err != nil {
		t.Fatal(err)
	}
	if err := pm.LoadPlugins(); err == nil {
		t.Fatal("LoadPlugins succeeded without a plugins table")
	}
	if n := len(pm.GetPlugins()); n != 1 {
		t.Fatalf("%d plugins loaded after a failed reload, want the original 1", n)
	}
}

// The error count is checked against the copy that failed, but reaching the
// limit unloads by id. If the plugin was replaced between the two (an update,
// a re-enable), the unload must not take the replacement with it.
func TestErrorLimitUnloadsOnlyTheCopyThatFailed(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagCreatedCounterSrc)
	defer pm.Shutdown()
	if _, err := app.db.Exec(`UPDATE plugins SET error_count = 2 WHERE id = ?`, pluginID); err != nil {
		t.Fatal(err)
	}
	stale := plugin.NewSandbox(&plugin.Manifest{Name: "replaced"}, pluginID)
	defer stale.Close()

	pm.RecordFailureForTest(pluginID, stale)

	if n := len(pm.GetPlugins()); n != 1 {
		t.Fatal("a failure charged to a replaced copy unloaded the current one")
	}
	var errs int
	var status string
	if err := app.db.QueryRow(`SELECT error_count, status FROM plugins WHERE id = ?`, pluginID).Scan(&errs, &status); err != nil {
		t.Fatal(err)
	}
	if errs != 2 || status == "error" {
		t.Fatalf("a replaced copy's failure left error_count %d, status %q — want it not counted at all", errs, status)
	}

	// The current copy's own failure still disables it.
	pm.RecordFailureForTest(pluginID, pm.GetPlugins()[0].Sandbox)
	if n := len(pm.GetPlugins()); n != 0 {
		t.Fatal("the current copy reached the error limit and stayed loaded")
	}
	if err := app.db.QueryRow(`SELECT status FROM plugins WHERE id = ?`, pluginID).Scan(&status); err != nil || status != "error" {
		t.Fatalf("status %q after the current copy hit the error limit, want error (%v)", status, err)
	}
}
