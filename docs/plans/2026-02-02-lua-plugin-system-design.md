# Lua Plugin System Design

A headless plugin system for mahpastes enabling automation, integration, and transformation workflows via Lua scripts.

## Overview

- **Single-file plugins** with embedded Lua manifest
- **In-app import** via settings panel, stored locally
- **No UI modifications** - plugins operate on data only
- **Sandboxed execution** with declared permissions for network and filesystem

## Plugin Structure

A plugin is a single `.lua` file with a manifest table at the top:

```lua
Plugin = {
    name = "Dropbox Sync",
    version = "1.0.0",
    description = "Auto-sync clips to Dropbox",
    author = "Your Name",

    -- Network permissions: domain -> allowed HTTP methods
    network = {
        ["api.dropboxapi.com"] = {"GET", "POST"},
        ["content.dropboxapi.com"] = {"POST"},
    },

    -- Filesystem: declares intent, user approves specific paths at runtime
    filesystem = {
        read = true,
        write = true,
    },

    -- Events to subscribe to
    events = {"clip:created", "app:startup"},

    -- Scheduled tasks (optional)
    schedules = {
        {name = "sync_check", interval = 300}, -- seconds
    },
}
```

The manifest is parsed by Go before executing any Lua code. Invalid manifests reject the plugin entirely.

## Event System

### Available Events

**Lifecycle events:**
- `app:startup` - App finished initializing
- `app:shutdown` - App is closing

**Clip events:**
- `clip:created` - New clip added
- `clip:deleted` - Clip removed
- `clip:archived` - Archive status changed

**Watch events:**
- `watch:file_detected` - File detected in watched folder
- `watch:import_complete` - File imported as clip

**Scheduled tasks:**
- Defined in manifest with interval in seconds
- Function name must match schedule name

### Handler Functions

```lua
function on_startup()
    log("Plugin started")
end

function on_shutdown()
    -- Cleanup, final sync
end

function on_clip_created(clip)
    -- clip = {id, content_type, filename, created_at, is_archived}
    if clip.content_type:match("^image/") then
        sync_to_dropbox(clip.id)
    end
end

function on_clip_deleted(clip_id)
    -- Only ID available since data is gone
end

function on_clip_archived(clip)
    -- Called when archive status changes
end

function on_watch_import(clip, source_path)
    -- Clip just imported from watched folder
end

-- Scheduled task - name matches manifest
function sync_check()
    local pending = storage.get("pending_syncs") or {}
    -- Process pending items
end
```

Handlers have a 30-second timeout. Scheduled tasks run sequentially (no overlapping).

## Plugin API

### Clips Module

```lua
-- Query
local all_clips = clips.list()
local images = clips.list({content_type = "image/%"})
local clip = clips.get(id)  -- Full clip with data

-- Create
local new_id = clips.create({
    data = binary_data,
    content_type = "image/png",
    filename = "processed.png",  -- optional
})

-- Update metadata (not content)
clips.update(id, {is_archived = true})

-- Delete
clips.delete(id)
clips.delete_many({id1, id2, id3})

-- Archive helpers
clips.archive(id)
clips.unarchive(id)
```

Large clips (>10MB) return a reference; use `clips.read_chunk(id, offset, size)` to stream.

### Storage Module

Plugin-local key-value storage persisted to SQLite:

```lua
storage.set("last_sync", os.time())
storage.get("last_sync")  -- nil if not set
storage.delete("last_sync")
```

### HTTP Module

Restricted to domains and methods declared in manifest:

```lua
local response = http.get("https://api.dropboxapi.com/2/users/get_current_account", {
    headers = {["Authorization"] = "Bearer " .. token},
})
-- response = {status, headers, body}

local response = http.post(url, {
    headers = {["Content-Type"] = "application/json"},
    body = json.encode(data),
})

-- Violations throw errors:
-- http.get("https://evil.com")  --> Error: domain not in allowlist
-- http.delete(allowed_url)      --> Error: DELETE not allowed for this domain
```

### Filesystem Module

First access to a path triggers native folder picker. User approves once, permission remembered:

```lua
-- Triggers prompt: "Plugin 'X' wants to read from: [Choose Folder]"
local content = fs.read("/path/to/file.txt")

-- Write (separate permission)
fs.write("/path/to/output.txt", content)

-- List directory
local files = fs.list("/path/to/dir/")  -- {name, is_dir, size, modified}

-- Existence check (no prompt)
if fs.exists(path) then ... end
```

Users can revoke permissions via settings panel.

### Utility Functions

```lua
log(message)           -- Write to plugin log
json.encode(table)     -- Table to JSON string
json.decode(string)    -- JSON string to table
base64.encode(data)    -- Binary to base64
base64.decode(string)  -- Base64 to binary
```

## Resource Limits

| Resource | Limit | On Violation |
|----------|-------|--------------|
| Execution time | 30s per handler | Terminate, notify user |
| Memory | 50MB per plugin | Terminate, notify user |
| HTTP requests | 100/minute | Throttle, log warning |
| File operations | 50/minute | Throttle, log warning |
| Storage | 10MB per plugin | Reject writes, notify |

## Error Handling

Plugins can handle errors gracefully:

```lua
local ok, err = pcall(function()
    http.post(url, {body = data})
end)

if not ok then
    log("Sync failed: " .. err)
    storage.set("pending_syncs", pending)
end
```

**Plugin states:**
- `enabled` - Running normally
- `disabled` - User disabled
- `error` - After 3 consecutive handler failures

Plugins in `error` state prompt user to disable or retry.

## Implementation Architecture

### Package Structure

```
plugin/
├── manager.go      # PluginManager - load, run, manage plugins
├── lua_vm.go       # Lua VM wrapper, sandbox setup
├── api_clips.go    # clips.* functions exposed to Lua
├── api_http.go     # http.* with domain/method enforcement
├── api_fs.go       # fs.* with permission prompts
├── api_storage.go  # storage.* backed by SQLite
├── scheduler.go    # Scheduled tasks, timers
└── manifest.go     # Parse/validate plugin manifest
```

### App Integration

```go
type App struct {
    // ... existing fields
    pluginManager *plugin.PluginManager
}

// In startup():
pm, err := plugin.NewPluginManager(a.ctx, a.db)
a.pluginManager = pm
a.pluginManager.EmitEvent("app:startup", nil)

// Hook into existing operations:
func (a *App) UploadFiles(...) error {
    // ... existing upload logic
    for _, clipID := range newClipIDs {
        a.pluginManager.EmitEvent("clip:created", clipID)
    }
}
```

### Lua Runtime

Use `gopher-lua` (pure Go implementation) for the VM. Each plugin gets its own `*lua.LState` for isolation.

### Database Schema

```sql
CREATE TABLE plugins (
    id INTEGER PRIMARY KEY,
    filename TEXT UNIQUE,
    name TEXT,
    version TEXT,
    enabled INTEGER DEFAULT 1,
    status TEXT DEFAULT 'enabled',  -- enabled, disabled, error
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE plugin_permissions (
    plugin_id INTEGER,
    permission_type TEXT,  -- 'fs_read', 'fs_write'
    path TEXT,
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (plugin_id) REFERENCES plugins(id)
);

CREATE TABLE plugin_storage (
    plugin_id INTEGER,
    key TEXT,
    value BLOB,
    PRIMARY KEY (plugin_id, key),
    FOREIGN KEY (plugin_id) REFERENCES plugins(id)
);
```

## Plugin Management UI

Settings panel includes:
- List of installed plugins with status indicators
- Enable/disable toggle per plugin
- View granted permissions with revoke option
- Remove plugin button
- Import plugin button (file picker for `.lua` files)
