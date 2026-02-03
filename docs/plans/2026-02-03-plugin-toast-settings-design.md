# Plugin Toast Notifications & Settings UI

## Overview

Extend the plugin system with two capabilities:
1. **Toast API** - Plugins can display toast notifications to users
2. **Settings UI** - Plugins can declare configuration fields that appear in the Plugin Manager

## Toast API

### Lua Interface

```lua
toast.show("Import complete!", "success")
toast.show("API key missing", "error")
toast.show("Processing...", "info")  -- info is default if type omitted
```

### Types and Colors

| Type | Color | Use Case |
|------|-------|----------|
| `info` | stone-800 (default) | General notifications |
| `success` | emerald-600 | Completed operations |
| `error` | red-600 | Failures, missing config |

### Implementation

**Backend** (`plugin/api_toast.go`):
- New API module registered when plugin loads
- Single `show(message, type)` function
- Validates type is one of: `success`, `error`, `info`
- Emits Wails event: `runtime.EventsEmit(ctx, "plugin:toast", data)`
- Rate limited: 5 toasts per minute per plugin
- Max message length: 200 characters
- Messages HTML-escaped before emission

**Frontend**:
- `app.js`: Listen for `plugin:toast` event, call `showToast()`
- `utils.js`: Extend `showToast(message, type)` with type parameter and color mapping

## Plugin Settings

### Manifest Declaration

```lua
Plugin = {
  name = "my_plugin",
  version = "1.0.0",
  -- ... existing fields ...

  settings = {
    {key = "api_key", type = "password", label = "API Key", description = "Your service API key"},
    {key = "endpoint", type = "text", label = "Endpoint URL", default = "https://api.example.com"},
    {key = "enabled", type = "checkbox", label = "Enable sync", default = true},
    {key = "mode", type = "select", label = "Mode", options = {"fast", "balanced", "thorough"}, default = "balanced"}
  }
}
```

### Field Schema

| Property | Required | Description |
|----------|----------|-------------|
| `key` | Yes | Storage key name (used with `storage.get()`) |
| `type` | Yes | One of: `text`, `password`, `checkbox`, `select` |
| `label` | Yes | Display label in UI |
| `description` | No | Help text shown below field |
| `default` | No | Default value (shown in UI, not auto-saved) |
| `options` | For select | Array of string choices |

### Storage Integration

Settings reuse the existing plugin storage system:
- Frontend reads/writes via `PluginService.GetPluginStorage()` / `SetPluginStorage()`
- Plugins read via `storage.get("key")` as they do today
- No new database tables needed
- Defaults shown in UI but only written to storage when user explicitly changes value

### UI Location

Settings appear in the Plugin Manager, inside each plugin's expandable details panel:

```
┌─────────────────────────────────────────┐
│ ● My Plugin                        v1.0 │
│   Author Name                           │
├─────────────────────────────────────────┤
│ Description text here...                │
│                                         │
│ ─── Settings ───────────────────────── │
│ API Key                                 │
│ ┌─────────────────────────────────────┐ │
│ │ ••••••••••••                        │ │
│ └─────────────────────────────────────┘ │
│ Your service API key                    │
│                                         │
│ Mode                                    │
│ ┌─────────────────────────────────────┐ │
│ │ balanced                          ▼ │ │
│ └─────────────────────────────────────┘ │
│                                         │
│ ☑ Enable sync                          │
│                                         │
│ ─── Events ─────────────────────────── │
│ clip:created, tag:added_to_clip        │
└─────────────────────────────────────────┘
```

### Behavior

- Settings section only appears if plugin declares `settings` in manifest
- Each field change saves immediately (debounced 300ms)
- Password fields have show/hide toggle
- Styling matches app design system
- Settings editable even when plugin is disabled (configure before enabling)

## Backend Changes

### New File: `plugin/api_toast.go`

```go
type ToastAPI struct {
    ctx      context.Context
    pluginID string
    limiter  *rate.Limiter  // 5 per minute
}

func (t *ToastAPI) Register(L *lua.LState) {
    // Register toast.show(message, type)
}
```

### Changes to `plugin/manifest.go`

Add to `Manifest` struct:

```go
type SettingField struct {
    Key         string   `json:"key"`
    Type        string   `json:"type"`
    Label       string   `json:"label"`
    Description string   `json:"description,omitempty"`
    Default     any      `json:"default,omitempty"`
    Options     []string `json:"options,omitempty"`
}
```

Extend `parseManifest()` to extract `settings` array using text-based parsing.

### Validation

Manifest parser validates:
- Required fields (`key`, `type`, `label`) present
- `type` is one of: `text`, `password`, `checkbox`, `select`
- `select` type has non-empty `options` array

## Frontend Changes

### `frontend/js/utils.js`

Extend `showToast()`:

```javascript
function showToast(message, type = 'info') {
    const colors = {
        info: 'bg-stone-800',
        success: 'bg-emerald-600',
        error: 'bg-red-600'
    };
    // Apply color class based on type
}
```

### `frontend/js/app.js`

Add event listener:

```javascript
runtime.EventsOn("plugin:toast", (data) => {
    showToast(data.message, data.type);
});
```

### `frontend/js/plugins.js`

In `renderPluginDetails()`:
1. Check if `plugin.manifest.settings` exists
2. Render "Settings" section header
3. For each field, call `renderSettingField(field, currentValue, pluginID)`
4. Load current values via `PluginService.GetPluginStorage(pluginID)`
5. On change, debounce then call `PluginService.SetPluginStorage()`

## Files Summary

**Modify:**
- `plugin/manifest.go` - Parse settings from manifest
- `plugin/manager.go` - Register toast API when loading plugins
- `frontend/js/utils.js` - Extend `showToast()` with types
- `frontend/js/app.js` - Listen for `plugin:toast` events
- `frontend/js/plugins.js` - Render settings UI in plugin details

**Create:**
- `plugin/api_toast.go` - Toast API module

**Tests:**
- `e2e/tests/plugins/settings.spec.ts`
- `e2e/tests/plugins/toast.spec.ts`

## Edge Cases

| Case | Handling |
|------|----------|
| Plugin disabled | Settings still visible and editable |
| Invalid type in manifest | Parser rejects plugin with error |
| Missing required field | Parser rejects with specific error |
| XSS in toast message | HTML escaped before display |
| Toast spam | Rate limited, excess dropped silently |
| Plugin deleted | Storage cascade-deletes settings |

## Testing Checklist

- [ ] Import plugin with settings, verify UI renders
- [ ] Change each field type, verify storage updates
- [ ] Plugin reads saved value via `storage.get()`
- [ ] Default values show in UI but don't auto-save
- [ ] Call `toast.show()` from plugin, verify display
- [ ] Verify each toast type shows correct color
- [ ] Spam toasts, verify rate limiting works
- [ ] Plugin without settings shows no Settings section
