package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-clipboard/plugin"
)

// These tests pin that the plugin tags API mutates tags through the same App
// methods the UI uses. The Lua bindings used to run raw SQL on the plugin's own
// handle, so a plugin tagging a clip into a shared folder (auto-tagger does it
// for every new clip) never published it, skipped tree exclusivity and the
// restore lock, and a plugin deleting a shared tag cascaded its share away
// while the publication lived on in memory.

// tagHostPluginSrc drives each mutating tags.* call from a global action, and
// auto-tags new clips into "screenshot" the way the bundled auto-tagger does.
const tagHostPluginSrc = `Plugin = { name = "Tag Host Test", version = "1.0.0",
  events = {"clip:created"},
  ui = { global_actions = {
    {id = "add", label = "Add"},
    {id = "remove", label = "Remove"},
    {id = "delete", label = "Delete"},
  } } }

function on_clip_created(data)
  for _, t in ipairs(tags.list()) do
    if t.name == "screenshot" then
      tags.add_to_clip(t.id, data.id)
    end
  end
end

function on_ui_action(action_id, clip_ids, options, context)
  local ok, err
  if action_id == "add" then
    ok, err = tags.add_to_clip(options.tag, options.clip)
  elseif action_id == "remove" then
    ok, err = tags.remove_from_clip(options.tag, options.clip)
  elseif action_id == "delete" then
    ok, err = tags.delete(options.tag)
  end
  if not ok then
    return {success = false, error = tostring(err)}
  end
  return {success = true}
end`

// loadWiredTestPlugin imports src into a plugin manager wired to app exactly as
// Bootstrap wires it, and returns the plugin id.
func loadWiredTestPlugin(t *testing.T, app *App, src string) (*plugin.Manager, int64) {
	t.Helper()
	pm, _ := newTestPluginManager(t, app)
	app.wirePluginHostFuncs(pm)

	srcPath := filepath.Join(t.TempDir(), "tag-host-test.lua")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	p, err := pm.ImportPlugin(srcPath)
	if err != nil {
		t.Fatalf("ImportPlugin: %v", err)
	}
	return pm, p.ID
}

// runTagAction runs one of tagHostPluginSrc's global actions and fails the test
// unless the Lua call it makes reported success.
func runTagAction(t *testing.T, pm *plugin.Manager, pluginID int64, action string, tagID, clipID int64) {
	t.Helper()
	res, err := pm.ExecuteUIAction(pluginID, action, nil,
		map[string]interface{}{"tag": tagID, "clip": clipID}, nil)
	if err != nil {
		t.Fatalf("ExecuteUIAction(%s): %v", action, err)
	}
	if !res.Success {
		t.Fatalf("tags.%s from Lua failed: %s", action, res.Error)
	}
}

func TestPluginTagsAddToClipPublishesToShare(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	tagID := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(tagID)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	clipID := insertSharedTestClip(t, app, "hello")

	runTagAction(t, pm, pluginID, "add", tagID, clipID)
	app.shareHookWG.Wait()

	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d after tags.add_to_clip into a shared tag, want %d: the clip never reached followers", got, ringRowsPerSmallClip)
	}
}

// TestPluginAutoTagOnClipCreatedPublishesToShare is the auto-tagger shape: the
// tagging happens inside an on_clip_created handler, which also proves the
// re-entry from a running Lua handler into AddTagToClip completes.
func TestPluginAutoTagOnClipCreatedPublishesToShare(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, _ := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	tagID := mustCreateTag(t, app, "screenshot")
	info, err := app.shareManager.StartShare(tagID)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	clipID := insertSharedTestClip(t, app, "hello")

	pm.EmitEvent("clip:created", map[string]interface{}{"id": clipID})
	app.shareHookWG.Wait()

	tags, err := app.GetClipTags(clipID)
	if err != nil {
		t.Fatalf("GetClipTags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tagID {
		t.Fatalf("clip tags = %+v, want just screenshot (the handler did not tag it)", tags)
	}
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d after auto-tagging into a shared tag, want %d", got, ringRowsPerSmallClip)
	}
}

// TestPluginTagsAddToClipEnforcesTreeExclusivity: a clip holds one tag per
// tree, whoever tags it.
func TestPluginTagsAddToClipEnforcesTreeExclusivity(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	oldTag := mustCreateTag(t, app, "a/old")
	newTag := mustCreateTag(t, app, "a/new")
	clipID := tagOneClip(t, app, oldTag)

	runTagAction(t, pm, pluginID, "add", newTag, clipID)

	tags, err := app.GetClipTags(clipID)
	if err != nil {
		t.Fatalf("GetClipTags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != newTag {
		t.Fatalf("clip tags = %+v, want only a/new: two tags from one tree", tags)
	}
}

// TestPluginTagsDeleteStopsShare: deleting a shared tag from Lua must stop the
// publication the way DeleteTag does, not cascade the row out from under it.
func TestPluginTagsDeleteStopsShare(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	tagID := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(tagID)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	runTagAction(t, pm, pluginID, "delete", tagID, 0)

	if tagExists(t, app, tagID) {
		t.Fatal("tags.delete left the tag row in place")
	}
	shares, _ := app.shareManager.GetShareStatus()
	for _, s := range shares {
		if s.ID == info.ID {
			t.Fatalf("publication %d (tag name %q) is still live after its tag was deleted from Lua: StopShare never ran", s.ID, s.TagName)
		}
	}
}

// TestPluginTagsDeleteRefusesFollowTarget: DeleteTag's precondition, not a raw
// foreign-key failure, is what a plugin now hits.
func TestPluginTagsDeleteRefusesFollowTarget(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	tagID := mustCreateTag(t, app, "incoming")
	if _, err := app.db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, created_at) VALUES ('peer', x'00', ?, 0)`, tagID,
	); err != nil {
		t.Fatalf("insert follow: %v", err)
	}

	res, err := pm.ExecuteUIAction(pluginID, "delete", nil, map[string]interface{}{"tag": tagID}, nil)
	if err != nil {
		t.Fatalf("ExecuteUIAction: %v", err)
	}
	if res.Success {
		t.Fatal("tags.delete succeeded on a follow target")
	}
	if !tagExists(t, app, tagID) {
		t.Fatal("follow target was deleted")
	}
}

// TestPluginTagsRemoveFromClipRunsOrphanCleanup: removal from Lua tidies an
// unreferenced empty tag exactly like removal from the UI, and spares a shared
// one exactly like it.
func TestPluginTagsRemoveFromClipRunsOrphanCleanup(t *testing.T) {
	t.Run("unreferenced tag is cleaned up", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
		defer pm.Shutdown()

		tagID := mustCreateTag(t, app, "scratch")
		clipID := tagOneClip(t, app, tagID)

		runTagAction(t, pm, pluginID, "remove", tagID, clipID)

		if tagExists(t, app, tagID) {
			t.Fatal("tags.remove_from_clip left an unreferenced empty tag behind")
		}
	})
	t.Run("shared tag survives", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
		defer pm.Shutdown()

		tagID := mustCreateTag(t, app, "shared")
		if _, err := app.shareManager.StartShare(tagID); err != nil {
			t.Fatalf("StartShare: %v", err)
		}
		clipID := tagOneClip(t, app, tagID)

		runTagAction(t, pm, pluginID, "remove", tagID, clipID)

		if !tagExists(t, app, tagID) {
			t.Fatal("tags.remove_from_clip auto-deleted a shared tag")
		}
		var n int
		if err := app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE clip_id = ? AND tag_id = ?`, clipID, tagID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("tags.remove_from_clip did not remove the tag")
		}
	})
}

// --- Re-entry from a running handler ---------------------------------------
//
// Routing tags.* through the App means a Lua call now emits plugin events
// synchronously from inside the handler that made it. A handler holds its
// sandbox's mutex for its whole run, so an event addressed to the plugin that
// is mid-call — or to any plugin waiting in a host call further up the same
// chain — would block on a mutex its own goroutine holds. tags.create already
// had this shape through App.CreateTag.

// reentryPluginSrc subscribes to every event its own tag calls emit.
const reentryPluginSrc = `Plugin = { name = "Reentry Test", version = "1.0.0",
  events = {"tag:created", "tag:added_to_clip", "tag:removed_from_clip", "tag:deleted"},
  ui = { global_actions = {
    {id = "create", label = "Create"},
    {id = "add", label = "Add"},
    {id = "remove", label = "Remove"},
    {id = "delete", label = "Delete"},
  } } }

function on_tag_created(data) storage.set("seen_tag_created", "1") end
function on_tag_added_to_clip(data) storage.set("seen_tag_added_to_clip", "1") end
function on_tag_removed_from_clip(data) storage.set("seen_tag_removed_from_clip", "1") end
function on_tag_deleted(data) storage.set("seen_tag_deleted", "1") end

function on_ui_action(action_id, clip_ids, options, context)
  local ok, err
  if action_id == "create" then
    ok, err = tags.create("made-by-plugin")
  elseif action_id == "add" then
    ok, err = tags.add_to_clip(options.tag, options.clip)
  elseif action_id == "remove" then
    ok, err = tags.remove_from_clip(options.tag, options.clip)
  elseif action_id == "delete" then
    ok, err = tags.delete(options.tag)
  end
  if not ok then
    return {success = false, error = tostring(err)}
  end
  return {success = true}
end`

// reentryDeadline bounds a call that should take milliseconds. A deadlock
// never finishes, so the exact value only trades test time for slack.
const reentryDeadline = 5 * time.Second

// runWithin runs fn on its own goroutine and reports whether it finished
// before reentryDeadline. A goroutine that does not is deadlocked and is left
// behind; callers must then skip any cleanup that would wait on it.
func runWithin(fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(reentryDeadline):
		return false
	}
}

func pluginStorageValue(t *testing.T, app *App, pluginID int64, key string) string {
	t.Helper()
	var v string
	err := app.db.QueryRow(
		`SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?`, pluginID, key,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read plugin storage %q: %v", key, err)
	}
	return v
}

func TestPluginTagCallsDoNotDeadlockOnOwnEvents(t *testing.T) {
	cases := []struct {
		action string
		event  string
		// setup returns the tag/clip the action operates on.
		setup func(t *testing.T, app *App) (tagID, clipID int64)
	}{
		{"create", "tag_created", func(t *testing.T, app *App) (int64, int64) { return 0, 0 }},
		{"add", "tag_added_to_clip", func(t *testing.T, app *App) (int64, int64) {
			return mustCreateTag(t, app, "target"), insertSharedTestClip(t, app, "hello")
		}},
		{"remove", "tag_removed_from_clip", func(t *testing.T, app *App) (int64, int64) {
			tagID := mustCreateTag(t, app, "target")
			return tagID, tagOneClip(t, app, tagID)
		}},
		{"delete", "tag_deleted", func(t *testing.T, app *App) (int64, int64) {
			return mustCreateTag(t, app, "target"), 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()
			pm, pluginID := loadWiredTestPlugin(t, app, reentryPluginSrc)
			deadlocked := false
			defer func() {
				if !deadlocked {
					pm.Shutdown()
				}
			}()

			tagID, clipID := tc.setup(t, app)
			var res *plugin.ActionResult
			var err error
			if !runWithin(func() {
				res, err = pm.ExecuteUIAction(pluginID, tc.action, nil,
					map[string]interface{}{"tag": tagID, "clip": clipID}, nil)
			}) {
				deadlocked = true
				t.Fatalf("tags.%s from a handler deadlocked: its own event was delivered into the sandbox the call was holding", tc.action)
			}
			if err != nil {
				t.Fatalf("ExecuteUIAction: %v", err)
			}
			if !res.Success {
				t.Fatalf("tags.%s failed: %s", tc.action, res.Error)
			}

			// The event is not dropped: it reaches the plugin once the handler
			// that caused it has returned.
			deadline := time.Now().Add(reentryDeadline)
			for pluginStorageValue(t, app, pluginID, "seen_"+tc.event) != "1" {
				if time.Now().After(deadline) {
					t.Fatalf("on_%s never ran after tags.%s: the event was lost", tc.event, tc.action)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// TestPluginTeardownWhileHandlerInHostCallDoesNotDeadlock: UnloadPlugin and
// Shutdown close sandboxes, which waits for a running handler. Doing that while
// holding the manager lock deadlocks against a handler whose host call is about
// to emit an event, because EmitEvent needs that same lock to find subscribers.
func TestPluginTeardownWhileHandlerInHostCallDoesNotDeadlock(t *testing.T) {
	for _, teardown := range []string{"UnloadPlugin", "Shutdown"} {
		t.Run(teardown, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()
			pm, _ := newTestPluginManager(t, app)
			app.wirePluginHostFuncs(pm)

			// Park the host call after the handler has taken its sandbox and
			// before the App emits its event.
			entered := make(chan struct{})
			release := make(chan struct{})
			pm.SetTagMutationFuncs(func(clipID, tagID int64) error {
				close(entered)
				<-release
				return app.AddTagToClip(clipID, tagID)
			}, app.RemoveTagFromClip, app.DeleteTag)

			srcPath := filepath.Join(t.TempDir(), "tag-host-test.lua")
			if err := os.WriteFile(srcPath, []byte(tagHostPluginSrc), 0o644); err != nil {
				t.Fatalf("write plugin: %v", err)
			}
			p, err := pm.ImportPlugin(srcPath)
			if err != nil {
				t.Fatalf("ImportPlugin: %v", err)
			}
			tagID := mustCreateTag(t, app, "target")
			clipID := insertSharedTestClip(t, app, "hello")

			actionDone := make(chan struct{})
			go func() {
				defer close(actionDone)
				_, _ = pm.ExecuteUIAction(p.ID, "add", nil,
					map[string]interface{}{"tag": tagID, "clip": clipID}, nil)
			}()
			<-entered

			teardownDone := make(chan struct{})
			go func() {
				defer close(teardownDone)
				if teardown == "UnloadPlugin" {
					pm.UnloadPlugin(p.ID)
				} else {
					pm.Shutdown()
				}
			}()
			// Let the teardown reach the sandbox close it will wait in.
			time.Sleep(200 * time.Millisecond)
			close(release)

			for name, ch := range map[string]chan struct{}{"the UI action": actionDone, teardown: teardownDone} {
				select {
				case <-ch:
				case <-time.After(reentryDeadline):
					t.Fatalf("%s never finished: %s held the manager lock while waiting for a handler that needed it", name, teardown)
				}
			}
			if teardown == "UnloadPlugin" {
				pm.Shutdown()
			}
		})
	}
}

// TestPluginAddToClipDuringRestoreReloadDoesNotDeadlock: RestoreBackup reloads
// plugins while it holds backupRestoreMu for writing, and loading a plugin runs
// its top-level code on that goroutine. AddTagToClip reads the same lock, so a
// plugin that tags a clip at load time would wait on the restore that is
// loading it, forever — and every tag mutation in the app behind it.
func TestPluginAddToClipDuringRestoreReloadDoesNotDeadlock(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, _ := newTestPluginManager(t, app)
	app.wirePluginHostFuncs(pm)

	tagID := mustCreateTag(t, app, "target")
	clipID := insertSharedTestClip(t, app, "hello")
	src := fmt.Sprintf(`Plugin = { name = "Load Tagger", version = "1.0.0" }
local ok, err = tags.add_to_clip(%d, %d)
load_result = tostring(ok) .. ":" .. tostring(err)`, tagID, clipID)
	srcPath := filepath.Join(t.TempDir(), "load-tagger.lua")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}

	// Stand in for RestoreBackup: hold the write lock on this goroutine and
	// load the plugin under it.
	app.backupRestoreMu.Lock()
	var importErr error
	finished := runWithin(func() {
		_, importErr = pm.ImportPlugin(srcPath)
	})
	if !finished {
		t.Fatal("loading a plugin that calls tags.add_to_clip deadlocked under the restore write lock")
	}
	app.backupRestoreMu.Unlock()
	defer pm.Shutdown()
	if importErr != nil {
		t.Fatalf("ImportPlugin: %v", importErr)
	}

	// The add is refused rather than waited on: mid-restore the clip id may
	// already name a different clip.
	var n int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE clip_id = ? AND tag_id = ?`, clipID, tagID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("tags.add_to_clip tagged a clip while a restore held the write lock")
	}
}
