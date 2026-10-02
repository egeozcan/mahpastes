package app

import (
	"testing"

	"go-clipboard/plugin"
)

// tagUpdatePluginSrc renames or recolors a tag from a global action.
const tagUpdatePluginSrc = `Plugin = { name = "Tag Update Test", version = "1.0.0",
  ui = { global_actions = { {id = "update", label = "Update"} } } }

function on_ui_action(action_id, clip_ids, options, context)
  local ok, err = tags.update(options.tag, {name = options.name, color = options.color})
  if not ok then return {success = false, error = tostring(err)} end
  return {success = true}
end`

func runTagUpdate(t *testing.T, pm *plugin.Manager, pluginID, tagID int64, name, color string) (bool, string) {
	t.Helper()
	opts := map[string]interface{}{"tag": tagID}
	if name != "" {
		opts["name"] = name
	}
	if color != "" {
		opts["color"] = color
	}
	res, err := pm.ExecuteUIAction(pluginID, "update", nil, opts, nil)
	if err != nil {
		t.Fatalf("ExecuteUIAction: %v", err)
	}
	return res.Success, res.Error
}

// tags.update renamed with a bare UPDATE on the plugin's own handle, skipping
// everything App.UpdateTag does: the descendant cascade (a/b → x left a/b/c
// behind, no longer under its parent), the reserved _api segment, the
// served-subtree guard, and the tag:updated events the UI and other plugins
// rely on. It now goes through UpdateTag like the other tags.* mutations.
func TestPluginTagsUpdateGoesThroughUpdateTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginID := loadWiredTestPlugin(t, app, tagUpdatePluginSrc)
	defer pm.Shutdown()

	parent := mustCreateTag(t, app, "a/b")
	mustCreateTag(t, app, "a/b/c")

	if ok, msg := runTagUpdate(t, pm, pluginID, parent, "x", ""); !ok {
		t.Fatalf("rename: %s", msg)
	}
	if n := countRows(t, app.db, `SELECT COUNT(*) FROM tags WHERE name = 'x/c'`); n != 1 {
		t.Fatal("renaming a tag through the plugin API left its descendants behind")
	}

	if ok, _ := runTagUpdate(t, pm, pluginID, parent, "x/_api", ""); ok {
		t.Fatal("the plugin API renamed a tag to a reserved _api path")
	}

	if ok, msg := runTagUpdate(t, pm, pluginID, parent, "", "#00ff00"); !ok {
		t.Fatalf("recolor: %s", msg)
	}
	if name, color := tagRow(t, app, parent); name != "x" || color != "#00ff00" {
		t.Fatalf("after a color-only plugin update: %q %q", name, color)
	}
}
