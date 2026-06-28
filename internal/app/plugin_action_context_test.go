package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go-clipboard/plugin"
)

// TestHandleExecutePluginActionPassesFolderContext exercises the headless
// (mahpastesd) path for plugin folder context: the REST endpoint that
// rest-glue.js posts to must decode the `context` body field and hand it to the
// plugin's on_ui_action as the fourth argument. Desktop e2e covers the Wails
// binding; this covers the REST decode that only the server uses.
func TestHandleExecutePluginActionPassesFolderContext(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir pluginsDir: %v", err)
	}
	pm, err := plugin.NewManager(context.Background(), app.bridge, app.db, pluginsDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	app.pluginManager = pm
	defer pm.Shutdown()

	// A plugin whose global action records the folder context it received and
	// tags the clip it creates into that folder — exactly like fal.ai's generate.
	const src = `Plugin = { name = "Ctx Test", version = "1.0.0",
  ui = { global_actions = { {id = "make", label = "Make"} } } }
function on_ui_action(action_id, clip_ids, options, context)
  context = context or {}
  storage.set("ctx_id", tostring(context.folder_tag_id or 0))
  storage.set("ctx_path", context.folder_tag_path or "")
  local c = clips.create({name = "ctx_result.txt", data = "x", mime_type = "text/plain"})
  if context.folder_tag_id and context.folder_tag_id > 0 then
    tags.add_to_clip(context.folder_tag_id, c.id)
  end
  return {success = true, result_clip_id = c.id}
end`
	srcPath := filepath.Join(t.TempDir(), "ctx-test.lua")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
	p, err := pm.ImportPlugin(srcPath)
	if err != nil {
		t.Fatalf("ImportPlugin: %v", err)
	}

	// A tag standing in for the active folder.
	res, err := app.db.Exec("INSERT INTO tags (name, color) VALUES ('inbox', '#78716C')")
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	tagID, _ := res.LastInsertId()

	// Drive the REST endpoint the way rest-glue.js does in headless mode.
	am := NewAPIManager(app)
	body := fmt.Sprintf(`{"clip_ids":[],"options":{},"context":{"folder_tag_id":%d,"folder_tag_path":"inbox"}}`, tagID)
	req := httptest.NewRequest("POST", "/api/v1/plugins/x/actions/make", strings.NewReader(body))
	req.SetPathValue("id", strconv.FormatInt(p.ID, 10))
	req.SetPathValue("actionId", "make")
	rec := httptest.NewRecorder()
	am.handleExecutePluginAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Both context fields were decoded from JSON and delivered to the plugin.
	var ctxPath, ctxID string
	app.db.QueryRow("SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = 'ctx_path'", p.ID).Scan(&ctxPath)
	app.db.QueryRow("SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = 'ctx_id'", p.ID).Scan(&ctxID)
	if ctxPath != "inbox" {
		t.Errorf("ctx_path = %q, want %q", ctxPath, "inbox")
	}
	if ctxID != strconv.FormatInt(tagID, 10) {
		t.Errorf("ctx_id = %q, want %q", ctxID, strconv.FormatInt(tagID, 10))
	}

	// And the plugin used it to tag the created clip into the folder.
	var tagged int
	app.db.QueryRow("SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?", tagID).Scan(&tagged)
	if tagged != 1 {
		t.Errorf("clip_tags rows for folder tag = %d, want 1", tagged)
	}
}
