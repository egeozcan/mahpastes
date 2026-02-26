# Plugin Global Actions in Hamburger Menu

## Problem

Plugin actions (`lightbox_buttons`, `card_actions`) always operate on existing clips. There's no way for plugins to expose resource-free actions — things like "Generate Image" that take a prompt but don't need a clip as input.

## Solution

Add `global_actions` as a third action type in the plugin UI manifest. These actions appear at the bottom of the hamburger menu and execute with an empty clip IDs list.

## Design

### Manifest

New `global_actions` field in the `ui` section, parallel to `lightbox_buttons` and `card_actions`:

```lua
ui = {
  global_actions = {
    {
      id = "generate_image",
      label = "Generate Image",
      icon = "sparkles",
      async = true,
      options = {
        { id = "prompt", type = "text", label = "Prompt", required = true },
        { id = "model", type = "select", label = "Model", choices = {...} }
      }
    }
  }
}
```

Reuses the existing `UIAction` struct. `FileTypes` and `MaxSize` are ignored (no clip context).

### Backend

- **`plugin/manifest.go`**: Add `GlobalActions []UIAction` to `UIManifest`.
- **`plugin_service.go`**: `GetPluginUIActions()` collects `global_actions` into a third array in the returned map.
- **Execution**: `ExecutePluginAction()` unchanged — global actions receive empty `clipIDs`. The Lua `on_ui_action(action_id, clip_ids, options)` handler sees `clip_ids` as an empty table.

### Frontend

- **Hamburger menu**: After existing items, add a divider and `<div id="drawer-plugin-actions">` container.
- **Rendering**: `loadGlobalActions()` fetches global actions and renders menu items. Each item shows plugin icon + action label. Re-invoked on plugin changes.
- **Styling**: Matches existing drawer items — `text-xs font-medium text-stone-700 hover:bg-stone-100 px-4 py-2.5 rounded-md flex items-center gap-2`.
- **Click handling**:
  - With options: close drawer, show `openPluginOptionsDialog()`, then `executePluginAction(pluginId, actionId, [], options)`.
  - Without options: close drawer, `executePluginAction(pluginId, actionId, [], {})` directly.
- **Async**: Same toast/modal behavior as existing actions.
- **Hidden when empty**: Plugin section in drawer hidden when no plugins have global actions.

### Testing

E2e tests in `e2e/tests/plugins/`:
- Global action appears in hamburger menu after plugin install
- Global action with options shows dialog, executes with form values
- Global action without options executes directly
- Global action can create a clip (result appears in gallery)
- Plugin section hidden when no global actions exist
