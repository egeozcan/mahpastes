package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- Tag events announce changes, not calls ---------------------------------
//
// tags.add_to_clip and tags.remove_from_clip run through the App methods, and
// an event a handler's own call emits is delivered back to it once it returns.
// An event fired for a call that changed nothing therefore turns any handler
// that makes an idempotent tag call ("keep `all` on every tagged clip") into a
// loop that never ends, one write transaction per turn.

// tagEchoPluginSrc counts its handler runs, and each run makes the idempotent
// call the handler's event invites: re-adding `all` after any add, and
// re-removing the tag that was just removed.
const tagEchoPluginSrc = `Plugin = { name = "Tag Echo", version = "1.0.0",
  events = {"tag:added_to_clip", "tag:removed_from_clip"} }

local function bump(key)
  storage.set(key, tostring(tonumber(storage.get(key) or "0") + 1))
end

function on_tag_added_to_clip(data)
  bump("added_runs")
  for _, t in ipairs(tags.list()) do
    if t.name == "all" then
      tags.add_to_clip(t.id, data.clip_id)
    end
  end
end

function on_tag_removed_from_clip(data)
  bump("removed_runs")
  tags.remove_from_clip(data.tag_id, data.clip_id)
end`

// settledCount waits for a plugin storage counter to reach want, then holds
// for a while to prove it stays there. It returns the last value read.
func settledCount(t *testing.T, app *App, pluginID int64, key string, want int) int {
	t.Helper()
	read := func() int {
		n, _ := strconv.Atoi(pluginStorageValue(t, app, pluginID, key))
		return n
	}
	deadline := time.Now().Add(reentryDeadline)
	for read() < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	return read()
}

func TestPluginIdempotentTagHandlerTerminates(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		mustCreateTag(t, app, "all")
		work := mustCreateTag(t, app, "work")
		clipID := insertSharedTestClip(t, app, "hello")
		pm, pluginID := loadWiredTestPlugin(t, app, tagEchoPluginSrc)
		defer pm.Shutdown()

		if err := app.AddTagToClip(clipID, work); err != nil {
			t.Fatalf("AddTagToClip: %v", err)
		}

		// Run 1 is the user's add of work, whose re-add of `all` is real and
		// emits run 2. Run 2's re-add of `all` changes nothing.
		if got := settledCount(t, app, pluginID, "added_runs", 2); got != 2 {
			t.Fatalf("on_tag_added_to_clip ran %d times, want 2: a no-op add re-emitted the event and the handler is looping", got)
		}
	})
	t.Run("remove", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		// A subtag, so orphan cleanup cannot delete it after the removal and
		// end the loop by making the re-removal fail "tag not found".
		work := mustCreateTag(t, app, "projects/work")
		clipID := tagOneClip(t, app, work)
		pm, pluginID := loadWiredTestPlugin(t, app, tagEchoPluginSrc)
		defer pm.Shutdown()

		if err := app.RemoveTagFromClip(clipID, work); err != nil {
			t.Fatalf("RemoveTagFromClip: %v", err)
		}

		// Run 1 is the user's removal; its re-removal deletes nothing.
		if got := settledCount(t, app, pluginID, "removed_runs", 1); got != 1 {
			t.Fatalf("on_tag_removed_from_clip ran %d times, want 1: a no-op removal re-emitted the event and the handler is looping", got)
		}
	})
}

// tagEventLogPluginSrc records the clip id of every tag event it receives.
const tagEventLogPluginSrc = `Plugin = { name = "Tag Event Log", version = "1.0.0",
  events = {"tag:added_to_clip", "tag:removed_from_clip"} }

local function record(key, data)
  local v = storage.get(key)
  local entry = tostring(data.clip_id) .. "@" .. tostring(data.tag_id)
  if v == nil or v == "" then v = entry else v = v .. "," .. entry end
  storage.set(key, v)
end

function on_tag_added_to_clip(data) record("added", data) end
function on_tag_removed_from_clip(data) record("removed", data) end`

// tagEventLog reads what tagEventLogPluginSrc recorded under key, as
// "clip@tag" entries in delivery order.
func tagEventLog(t *testing.T, app *App, pluginID int64, key string) []string {
	t.Helper()
	v := pluginStorageValue(t, app, pluginID, key)
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func eventEntry(clipID, tagID int64) string {
	return strconv.FormatInt(clipID, 10) + "@" + strconv.FormatInt(tagID, 10)
}

// TestTagEventsFireOnlyForRealChanges: each event announces exactly one row
// that was inserted or deleted. The App methods are called from the test
// goroutine, so delivery is synchronous and the log is complete on return.
func TestTagEventsFireOnlyForRealChanges(t *testing.T) {
	t.Run("AddTagToClip", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		tagID := mustCreateTag(t, app, "work")
		clipID := insertSharedTestClip(t, app, "hello")
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		for i := 0; i < 2; i++ {
			if err := app.AddTagToClip(clipID, tagID); err != nil {
				t.Fatalf("AddTagToClip #%d: %v", i+1, err)
			}
		}
		want := []string{eventEntry(clipID, tagID)}
		if got := tagEventLog(t, app, pluginID, "added"); !slices.Equal(got, want) {
			t.Fatalf("tag:added_to_clip events = %v, want %v: the repeat add changed nothing", got, want)
		}
	})
	t.Run("AddTagToClip move within a tree", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		oldTag := mustCreateTag(t, app, "a/old")
		newTag := mustCreateTag(t, app, "a/new")
		clipID := tagOneClip(t, app, oldTag)
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		if err := app.AddTagToClip(clipID, newTag); err != nil {
			t.Fatalf("AddTagToClip: %v", err)
		}
		if got, want := tagEventLog(t, app, pluginID, "added"), []string{eventEntry(clipID, newTag)}; !slices.Equal(got, want) {
			t.Fatalf("tag:added_to_clip events = %v, want %v", got, want)
		}
		// The exclusivity removal of a/old never announced itself, and still
		// does not: the move is one add.
		if got := tagEventLog(t, app, pluginID, "removed"); len(got) != 0 {
			t.Fatalf("tag:removed_from_clip events = %v, want none for a tree move", got)
		}
	})
	t.Run("RemoveTagFromClip", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		tagID := mustCreateTag(t, app, "work")
		clipID := tagOneClip(t, app, tagID)
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		for i := 0; i < 2; i++ {
			if err := app.RemoveTagFromClip(clipID, tagID); err != nil {
				t.Fatalf("RemoveTagFromClip #%d: %v", i+1, err)
			}
		}
		want := []string{eventEntry(clipID, tagID)}
		if got := tagEventLog(t, app, pluginID, "removed"); !slices.Equal(got, want) {
			t.Fatalf("tag:removed_from_clip events = %v, want %v: the repeat removal changed nothing", got, want)
		}
	})
	t.Run("BulkAddTag", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		tagID := mustCreateTag(t, app, "work")
		already := tagOneClip(t, app, tagID)
		fresh := insertSharedTestClip(t, app, "fresh")
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		if err := app.BulkAddTag([]int64{already, fresh, fresh}, tagID); err != nil {
			t.Fatalf("BulkAddTag: %v", err)
		}
		want := []string{eventEntry(fresh, tagID)}
		if got := tagEventLog(t, app, pluginID, "added"); !slices.Equal(got, want) {
			t.Fatalf("tag:added_to_clip events = %v, want %v: only the clip that gained the tag changed", got, want)
		}
	})
	t.Run("BulkRemoveTag", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		tagID := mustCreateTag(t, app, "work")
		first := tagOneClip(t, app, tagID)
		second := tagOneClip(t, app, tagID)
		untagged := insertSharedTestClip(t, app, "untagged")
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		if err := app.BulkRemoveTag([]int64{second, untagged, first, second}, tagID); err != nil {
			t.Fatalf("BulkRemoveTag: %v", err)
		}
		// One event per clip that lost the tag, in the caller's order.
		want := []string{eventEntry(second, tagID), eventEntry(first, tagID)}
		if got := tagEventLog(t, app, pluginID, "removed"); !slices.Equal(got, want) {
			t.Fatalf("tag:removed_from_clip events = %v, want %v", got, want)
		}
	})
}

// --- A plugin add does not unfile a clip the user already filed under it ----
//
// UploadFiles tags a folder upload before it emits clip:created, so an
// auto-tagger adding top-level "screenshot" to a clip just dropped into
// "screenshot/2024" arrives second. Under replace semantics it would strip the
// user's folder tag and leave the clip at the root of the tree.

// ensureTag returns the id of the tag called name, creating it if needed.
// CreateTag refuses an existing name, and creating "a/b/c" already made "a".
func ensureTag(t *testing.T, a *App, name string) int64 {
	t.Helper()
	var id int64
	err := a.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return mustCreateTag(t, a, name)
	}
	if err != nil {
		t.Fatalf("look up tag %q: %v", name, err)
	}
	return id
}

func TestPluginAutoTagKeepsFolderUploadInItsFolder(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, _ := loadWiredTestPlugin(t, app, tagHostPluginSrc)
	defer pm.Shutdown()

	folder := mustCreateTag(t, app, "screenshot/2024") // also creates "screenshot"
	err := app.UploadFiles([]FileData{{
		Name:        "Screenshot 1.png",
		ContentType: "image/png",
		Data:        base64.StdEncoding.EncodeToString([]byte("not really a png")),
	}}, 0, folder)
	if err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	var clipID int64
	if err := app.db.QueryRow(`SELECT id FROM clips WHERE filename = 'Screenshot 1.png'`).Scan(&clipID); err != nil {
		t.Fatalf("find uploaded clip: %v", err)
	}

	if got, want := clipTagNames(t, app, clipID), []string{"screenshot/2024"}; !slices.Equal(got, want) {
		t.Fatalf("clip tags after a folder upload = %v, want %v: the auto-tagger moved it out of the folder", got, want)
	}
}

// TestPluginTagsAddToClipPlacement: a plugin add is a successful no-op when
// the clip already sits at the requested tag or beneath it; every other
// same-tree case moves the clip, and other trees are untouched.
func TestPluginTagsAddToClipPlacement(t *testing.T) {
	cases := []struct {
		name    string
		current string // the clip's tag before the plugin call
		add     string // the tag the plugin adds
		want    []string
	}{
		{"already under the requested tag", "screenshot/2024", "screenshot", []string{"screenshot/2024"}},
		{"several levels under the requested tag", "a/b/c", "a", []string{"a/b/c"}},
		{"already carries the requested tag", "a/b", "a/b", []string{"a/b"}},
		{"sibling moves", "a/old", "a/new", []string{"a/new"}},
		{"deeper than the current tag moves down", "a", "a/b", []string{"a/b"}},
		{"name prefix is not a descendant", "a/bc", "a/b", []string{"a/b"}},
		{"similarly named root is another tree", "screenshots/2024", "screenshot", []string{"screenshot", "screenshots/2024"}},
		{"unrelated tree is added alongside", "work/x", "screenshot", []string{"screenshot", "work/x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()
			pm, pluginID := loadWiredTestPlugin(t, app, tagHostPluginSrc)
			defer pm.Shutdown()

			clipID := tagOneClip(t, app, ensureTag(t, app, tc.current))
			runTagAction(t, pm, pluginID, "add", ensureTag(t, app, tc.add), clipID)

			if got := clipTagNames(t, app, clipID); !slices.Equal(got, tc.want) {
				t.Fatalf("clip tags = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAddTagToClipStillReplacesDeeperPlacement: the yield is the plugin path's
// alone. A user filing a clip at the root of its tree moves it there.
func TestAddTagToClipStillReplacesDeeperPlacement(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	folder := mustCreateTag(t, app, "screenshot/2024")
	clipID := tagOneClip(t, app, folder)
	if err := app.AddTagToClip(clipID, ensureTag(t, app, "screenshot")); err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	if got, want := clipTagNames(t, app, clipID), []string{"screenshot"}; !slices.Equal(got, want) {
		t.Fatalf("clip tags = %v, want %v", got, want)
	}
}
