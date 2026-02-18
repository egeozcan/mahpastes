# Plugin URL Install & Update System

## Overview

Add the ability to install plugins from raw `.lua` file URLs, with periodic background update checking, manual update application, and permission review on all installs and permission-changing updates.

## Requirements

- Install plugins from raw `.lua` file URLs (direct HTTP links)
- Permission review screen for **all** installs (both URL and file)
- Store source URL for URL-installed plugins
- Periodic background update checks (configurable interval in app settings)
- Update badge visible in plugin list when newer version available
- Manual "Update" button to apply updates
- When update changes permissions/network/events: block update, require full re-review of all permissions
- Two separate buttons: existing "Import Plugin" (file) + new "Install from URL"

## Database Changes

### `plugins` table — new column

```sql
ALTER TABLE plugins ADD COLUMN source_url TEXT DEFAULT '';
```

Stores the URL this plugin was installed from. Empty for file-imported plugins. Used by the update checker to fetch the latest version.

### New `app_settings` table

```sql
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

Simple key-value store for app-level settings. Used initially for:
- `plugin_update_interval`: values `"startup"`, `"6h"`, `"24h"`, `"disabled"`. Default: `"24h"`.

## Backend: Two-Phase Install Flow

All plugin installs (URL and file) now go through a two-phase flow:

### Phase 1: Preview

Parse the manifest and return a `PluginPreview` to the frontend for the review modal. No installation occurs.

**New type:**
```go
type PluginPreview struct {
    Name        string              `json:"name"`
    Version     string              `json:"version"`
    Description string              `json:"description"`
    Author      string              `json:"author"`
    Network     map[string][]string `json:"network"`
    Filesystem  FilesystemPerms     `json:"filesystem"`
    Clipboard   bool                `json:"clipboard"`
    Events      []string            `json:"events"`
    Source      string              `json:"source"` // URL or file path
}
```

**New PluginService methods:**
- `PreviewPluginFromURL(url string) (*PluginPreview, error)` — HTTP GET the URL, parse manifest, return preview
- `PreviewPluginFromPath(path string) (*PluginPreview, error)` — read file, parse manifest, return preview. Also used after the file dialog.

**URL validation:**
- Must be HTTP or HTTPS
- Timeout: 30s
- Max response size: 1MB
- Must contain a valid Plugin manifest

### Phase 2: Confirm Install

After user approves in the review modal, actually install the plugin.

**New PluginService method:**
- `ConfirmPluginInstall(source string) (*PluginInfo, error)` — source is a URL or file path (distinguished by `http://`/`https://` prefix)
  - For URLs: re-fetches the content (avoids TOCTOU with stale preview), writes to plugins dir, inserts DB with `source_url` populated
  - For file paths: copies file to plugins dir, inserts DB with empty `source_url`
  - Loads the plugin and returns `PluginInfo`

### Existing methods updated

- `ImportPlugin()` (file dialog): opens dialog, then returns `PluginPreview` instead of directly installing. Frontend shows review modal, then calls `ConfirmPluginInstall(path)`.
- `ImportPluginFromPath(path)`: becomes an alias for `ConfirmPluginInstall(path)` for backwards compatibility (used in tests).

## Backend: Update Checker

### New file: `plugin/update_checker.go`

**Struct: `UpdateChecker`**

Runs as a goroutine started by the Manager after `LoadPlugins()`.

**Behavior:**
1. On startup (and on the configured interval), iterates all plugins with non-empty `source_url`
2. Fetches each URL, parses only the manifest (no code execution)
3. Compares remote version to installed version via semver comparison
4. If remote is newer, stores update info in memory and emits Wails event `plugin:update_available`

**Event payload:**
```go
type PluginUpdateInfo struct {
    PluginID             int64  `json:"plugin_id"`
    CurrentVersion       string `json:"current_version"`
    NewVersion           string `json:"new_version"`
    HasPermissionChanges bool   `json:"has_permission_changes"`
}
```

### Permission Diff Logic

Compares current manifest against remote manifest:
- Network domain maps: added/removed domains, changed HTTP methods
- Filesystem perms: read/write added or removed
- Clipboard flag: changed from false to true
- Events list: new events added

Any difference sets `has_permission_changes = true`.

### Configurable Interval

- Read from `app_settings` table on startup
- Options: `"startup"` (check once on app start), `"6h"`, `"24h"`, `"disabled"`
- Default: `"24h"`
- `SetUpdateCheckInterval()` restarts the timer goroutine

### Update Application

**New PluginService methods:**
- `GetUpdateCheckInterval() (string, error)` — read from app_settings
- `SetUpdateCheckInterval(interval string) error` — write to app_settings, restart timer
- `CheckForUpdates() ([]PluginUpdateInfo, error)` — manual trigger, returns available updates
- `UpdatePlugin(pluginID int64) (*UpdateResult, error)` — fetches latest, compares permissions:
  - No permission changes: overwrites file, updates DB, reloads plugin, returns success
  - Permission changes: returns `UpdateResult` with `needs_review = true` and a `PluginPreview` with all permissions for the review modal
- `ConfirmPluginUpdate(pluginID int64) (*PluginInfo, error)` — applies the update after user approves permission changes in review modal

**UpdateResult type:**
```go
type UpdateResult struct {
    Success     bool           `json:"success"`
    NeedsReview bool           `json:"needs_review"`
    Preview     *PluginPreview `json:"preview,omitempty"`
    PluginInfo  *PluginInfo    `json:"plugin_info,omitempty"`
    Error       string         `json:"error,omitempty"`
}
```

## Frontend: Permission Review Modal

### New file: `frontend/js/plugin-review.js`

### New modal: `plugin-review-modal` (in `index.html`)

Shown for:
1. First install (URL or file) — after preview returns
2. Update with permission changes — after `UpdatePlugin` returns `needs_review`

### Layout

- **Title**: "Review Plugin" (install) or "Review Update" (update)
- **Plugin header**: name, version, author, description
- **For updates**: version badge `v1.0.0 -> v1.1.0`
- **Warning banner** (updates with changes only): "This update requires new permissions. Review all permissions before updating."

### Permission categories displayed

- **Network Access**: domain list with methods, e.g. `fal.ai (GET, POST)`
- **Filesystem**: read/write badges
- **Clipboard**: "Can write to clipboard" if enabled
- **Events**: subscribed events as badges

For updates with permission changes: all permissions shown (full re-review, not just diff).

### Actions

- "Cancel" — secondary button
- "Approve & Install" or "Approve & Update" — primary button

## Frontend: Plugin List Changes

### Update badges

Frontend listens to `plugin:update_available` events. Stores update info in a `pluginUpdates` map keyed by plugin ID.

Each plugin card shows when an update is available:
- Badge next to version: `v1.0.0 . Update available` (stone-400 text)
- "Update" button in card header (secondary button style, next to enable toggle)

### Update button behavior

1. Calls `UpdatePlugin(pluginID)`
2. No permission changes: update applied, toast "Updated to vX.Y.Z", refresh list
3. Permission changes: opens review modal with full permissions
4. After approval: calls `ConfirmPluginUpdate(pluginID)`, refresh list

### Install from URL

New "Install from URL" button next to existing "Import Plugin" button:
- On click: expands to inline URL input + "Install" button
- User pastes URL, clicks Install
- Calls `PreviewPluginFromURL(url)` -> review modal -> approve -> `ConfirmPluginInstall(url)`

### File import updated

Existing "Import Plugin" button now:
- Opens file dialog -> calls `PreviewPluginFromPath(path)` -> review modal -> approve -> `ConfirmPluginInstall(path)`

## Frontend: Settings Addition

New section in settings modal: "Plugin Updates"

Dropdown:
- Label: "Check for updates"
- Options: "On startup only", "Every 6 hours", "Every 24 hours" (default), "Disabled"
- Calls `SetUpdateCheckInterval()` on change
- Reads current value via `GetUpdateCheckInterval()` on settings open

## New PluginService Methods Summary

| Method | Purpose |
|--------|---------|
| `PreviewPluginFromURL(url)` | Fetch URL, parse manifest, return preview |
| `PreviewPluginFromPath(path)` | Read file, parse manifest, return preview |
| `ConfirmPluginInstall(source)` | Install after review approval |
| `GetUpdateCheckInterval()` | Read update check interval setting |
| `SetUpdateCheckInterval(interval)` | Write update check interval, restart timer |
| `CheckForUpdates()` | Manual update check trigger |
| `UpdatePlugin(pluginID)` | Attempt update, return success or needs-review |
| `ConfirmPluginUpdate(pluginID)` | Apply update after permission re-review |

Total new methods: 8. Brings PluginService from ~15 to ~23 methods (well under 49 limit).

## Design Decisions

- **Semver comparison** for version ordering (not content hashing). Authors must bump version for releases.
- **Re-fetch on confirm** to avoid TOCTOU issues between preview and install.
- **Full re-review on permission changes** (not just a diff) — most secure approach.
- **No plugin registry/store** — YAGNI, can add later on top of this.
- **Single `ConfirmPluginInstall`** handles both URL and file sources, distinguished by prefix.
