package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The gallery patches a single card in place after an archive, delete,
// rename or tag change instead of reloading. A plugin handler runs inside the
// App call that emitted its event and can change other clips; the patch only
// knows about the one it was asked about. GetPluginLibraryWrites is how the
// gallery finds out: it must move when a handler changed the library, and
// stay put when no handler did, or every patch degrades to a reload.

// archiveOtherPluginSrc archives clip storage["other"] whenever any clip is
// archived — a side effect on a clip the caller never named.
const archiveOtherPluginSrc = `Plugin = { name = "Archive Other", version = "1.0.0",
  events = {"clip:archived", "clip:deleted", "clip:renamed", "tag:added_to_clip"} }

local function touch_other()
  local other = tonumber(storage.get("other") or "0")
  if other and other > 0 then clips.archive(other) end
end

function on_clip_archived(data) touch_other() end
function on_clip_deleted(data) touch_other() end
function on_clip_renamed(data) touch_other() end
function on_tag_added_to_clip(data) touch_other() end`

func pluginWrites(t *testing.T, app *App) int64 {
	t.Helper()
	n, err := app.GetPluginLibraryWrites()
	if err != nil {
		t.Fatalf("GetPluginLibraryWrites: %v", err)
	}
	return n
}

func setArchiveOtherTarget(t *testing.T, app *App, pluginID, clipID int64) {
	t.Helper()
	if _, err := app.db.Exec(`INSERT OR REPLACE INTO plugin_storage (plugin_id, key, value) VALUES (?, 'other', ?)`,
		pluginID, strconv.FormatInt(clipID, 10)); err != nil {
		t.Fatalf("seed plugin storage: %v", err)
	}
}

func clipArchived(t *testing.T, app *App, id int64) bool {
	t.Helper()
	var v int
	if err := app.db.QueryRow(`SELECT is_archived FROM clips WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("read is_archived: %v", err)
	}
	return v == 1
}

func TestPluginLibraryWritesMovesWhenHandlerChangesAnotherClip(t *testing.T) {
	cases := []struct {
		name string
		do   func(t *testing.T, app *App, a int64)
	}{
		{"archive", func(t *testing.T, app *App, a int64) {
			if err := app.ToggleArchive(a); err != nil {
				t.Fatalf("ToggleArchive: %v", err)
			}
		}},
		{"delete", func(t *testing.T, app *App, a int64) {
			if err := app.DeleteClip(a); err != nil {
				t.Fatalf("DeleteClip: %v", err)
			}
		}},
		{"rename", func(t *testing.T, app *App, a int64) {
			if err := app.RenameClip(a, "renamed.txt"); err != nil {
				t.Fatalf("RenameClip: %v", err)
			}
		}},
		{"tag", func(t *testing.T, app *App, a int64) {
			if err := app.AddTagToClip(a, mustCreateTag(t, app, "work")); err != nil {
				t.Fatalf("AddTagToClip: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()
			a := insertSharedTestClip(t, app, "a")
			b := insertSharedTestClip(t, app, "b")
			pm, pluginID := loadWiredTestPlugin(t, app, archiveOtherPluginSrc)
			defer pm.Shutdown()
			setArchiveOtherTarget(t, app, pluginID, b)

			before := pluginWrites(t, app)
			tc.do(t, app, a)
			// The handler ran synchronously inside the call.
			if !clipArchived(t, app, b) {
				t.Fatalf("handler did not archive clip %d inside the call", b)
			}
			if after := pluginWrites(t, app); after == before {
				t.Fatalf("GetPluginLibraryWrites stayed %d after a handler archived another clip: the gallery would patch only the clip it changed and leave clip %d's card on screen", after, b)
			}
		})
	}
}

func TestPluginLibraryWritesStaysWhenHandlerWritesNothing(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	a := insertSharedTestClip(t, app, "a")
	pm, _ := loadWiredTestPlugin(t, app, archiveOtherPluginSrc) // no target: handler is a no-op
	defer pm.Shutdown()

	before := pluginWrites(t, app)
	if err := app.ToggleArchive(a); err != nil {
		t.Fatalf("ToggleArchive: %v", err)
	}
	if err := app.RenameClip(a, "x.txt"); err != nil {
		t.Fatalf("RenameClip: %v", err)
	}
	if after := pluginWrites(t, app); after != before {
		t.Fatalf("GetPluginLibraryWrites moved %d -> %d though no handler wrote: every in-place patch would reload", before, after)
	}
}

func TestPluginLibraryWritesRESTRefusesScopedKeys(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	am := NewAPIManager(app)
	app.pluginLibraryWrites.Add(3)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/plugin-writes", nil)
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{Role: "viewer"}))
	am.handlePluginLibraryWrites(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"count":3`) {
		t.Fatalf("unscoped: got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/library/plugin-writes", nil)
	req = req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, &apiKeyContext{Role: "viewer", ScopedTagID: 7}))
	am.handlePluginLibraryWrites(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scoped key: got %d, want 403", rec.Code)
	}
}
