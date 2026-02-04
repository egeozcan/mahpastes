---
sidebar_position: 2
---

# Plugin Manifest

The `Plugin` table at the top of your Lua file declares metadata, permissions, and capabilities.

## Basic Structure

```lua
Plugin = {
    -- Required
    name = "My Plugin",

    -- Optional metadata
    version = "1.0.0",
    description = "What this plugin does",
    author = "Your Name",

    -- Event subscriptions
    events = {"app:startup", "clip:created"},

    -- Network permissions
    network = {
        ["api.example.com"] = {"GET", "POST"},
    },

    -- Filesystem permissions
    filesystem = {
        read = true,
        write = true,
    },

    -- Scheduled tasks
    schedules = {
        {name = "cleanup", interval = 3600},
    },

    -- User-configurable settings
    settings = {
        {key = "api_key", type = "password", label = "API Key"},
    },
}
```

## Required Fields

### name

The only required field. Displayed in the plugin list and logs.

```lua
Plugin = {
    name = "My Awesome Plugin",
}
```

## Optional Metadata

### version

Semantic version string for tracking updates.

```lua
version = "1.0.0",
```

### description

Brief explanation shown in the plugin list.

```lua
description = "Automatically backs up clips to the cloud",
```

### author

Your name or organization.

```lua
author = "Jane Developer",
```

## Events

Subscribe to app events by listing them in the `events` array. Your plugin will only receive events it explicitly subscribes to.

```lua
events = {"app:startup", "clip:created", "clip:deleted"},
```

### Available Events

| Event | Handler Function | Data Passed |
|-------|------------------|-------------|
| `app:startup` | `on_startup()` | None |
| `app:shutdown` | `on_shutdown()` | None |
| `clip:created` | `on_clip_created(clip)` | Clip object |
| `clip:deleted` | `on_clip_deleted(clip_id)` | Clip ID (number) |
| `clip:archived` | `on_clip_archived(clip)` | Clip object |
| `clip:unarchived` | `on_clip_unarchived(clip)` | Clip object |
| `watch:file_detected` | `on_watch_file_detected(data)` | File info |
| `watch:import_complete` | `on_watch_import_complete(data)` | Import result |
| `tag:created` | `on_tag_created(tag)` | Tag object |
| `tag:updated` | `on_tag_updated(tag)` | Tag object |
| `tag:deleted` | `on_tag_deleted(tag_id)` | Tag ID (number) |
| `tag:added_to_clip` | `on_tag_added_to_clip(data)` | `{clip_id, tag_id}` |
| `tag:removed_from_clip` | `on_tag_removed_from_clip(data)` | `{clip_id, tag_id}` |

See [Event Handling](./event-handling) for detailed event documentation.

## Network Permissions

Declare which domains your plugin can access. Users see these permissions before installation.

```lua
network = {
    ["api.example.com"] = {"GET", "POST"},
    ["cdn.example.com"] = {"GET"},
},
```

- **Domain**: The exact domain (no wildcards)
- **Methods**: Array of allowed HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`)

Requests to undeclared domains will fail with a permission error.

### Example: Multiple APIs

```lua
network = {
    ["api.openai.com"] = {"POST"},
    ["api.anthropic.com"] = {"POST"},
    ["storage.googleapis.com"] = {"GET", "PUT"},
},
```

## Filesystem Permissions

Request access to read or write files outside the plugin's sandbox.

```lua
filesystem = {
    read = true,   -- Can read files
    write = true,  -- Can write files
},
```

Both default to `false`. Users are prompted to approve filesystem access on first use.

:::warning
Filesystem access is powerful. Only request what you need, and document why in your description.
:::

## Scheduled Tasks

Run functions at regular intervals.

```lua
schedules = {
    {name = "cleanup", interval = 3600},      -- Every hour
    {name = "sync", interval = 300},          -- Every 5 minutes
},
```

- **name**: Matches a handler function `scheduled_<name>()`
- **interval**: Seconds between executions

### Handler Example

```lua
schedules = {
    {name = "backup", interval = 1800},  -- Every 30 minutes
},

function scheduled_backup()
    log("Running scheduled backup...")
    -- Backup logic here
end
```

## Settings

Define user-configurable options that appear in the plugin's settings panel.

```lua
settings = {
    {
        key = "api_key",
        type = "password",
        label = "API Key",
        description = "Your API key from example.com",
    },
    {
        key = "auto_sync",
        type = "checkbox",
        label = "Enable Auto-Sync",
        default = true,
    },
},
```

### Setting Fields

| Field | Required | Description |
|-------|----------|-------------|
| `key` | Yes | Unique identifier for storage |
| `type` | Yes | Input type (see below) |
| `label` | Yes | Display label in settings UI |
| `description` | No | Help text shown below the input |
| `default` | No | Default value if not set |
| `options` | For `select` | Array of choices |

### Setting Types

#### text

Single-line text input.

```lua
{
    key = "username",
    type = "text",
    label = "Username",
    default = "",
},
```

#### password

Obscured text input for sensitive data.

```lua
{
    key = "api_key",
    type = "password",
    label = "API Key",
    description = "Get your key at example.com/api",
},
```

#### checkbox

Boolean toggle.

```lua
{
    key = "enabled",
    type = "checkbox",
    label = "Enable Feature",
    default = true,
},
```

#### select

Dropdown with predefined options.

```lua
{
    key = "quality",
    type = "select",
    label = "Export Quality",
    options = {"low", "medium", "high"},
    default = "medium",
},
```

### Reading Settings

Settings are stored with a `setting:` prefix. Use `storage.get()` to read them:

```lua
function on_startup()
    local api_key = storage.get("setting:api_key")
    local auto_sync = storage.get("setting:auto_sync")

    if not api_key then
        log("Warning: API key not configured")
        return
    end

    if auto_sync then
        log("Auto-sync is enabled")
    end
end
```

## Complete Example

Here's a full manifest for a cloud sync plugin:

```lua
Plugin = {
    name = "Cloud Sync",
    version = "2.1.0",
    description = "Automatically sync clips to your cloud storage",
    author = "Jane Developer",

    -- React to clip changes and app lifecycle
    events = {
        "app:startup",
        "app:shutdown",
        "clip:created",
        "clip:deleted",
    },

    -- API access for cloud provider
    network = {
        ["api.cloudstorage.com"] = {"GET", "POST", "PUT", "DELETE"},
        ["auth.cloudstorage.com"] = {"POST"},
    },

    -- Need to read clip files for upload
    filesystem = {
        read = true,
        write = false,
    },

    -- Periodic sync check
    schedules = {
        {name = "sync_check", interval = 300},  -- Every 5 minutes
    },

    -- User configuration
    settings = {
        {
            key = "api_key",
            type = "password",
            label = "API Key",
            description = "Your Cloud Storage API key",
        },
        {
            key = "auto_sync",
            type = "checkbox",
            label = "Auto-Sync New Clips",
            description = "Automatically upload new clips",
            default = true,
        },
        {
            key = "sync_quality",
            type = "select",
            label = "Image Quality",
            options = {"original", "high", "medium", "low"},
            default = "high",
        },
        {
            key = "folder_path",
            type = "text",
            label = "Remote Folder",
            description = "Path in cloud storage (e.g., /mahpastes/clips)",
            default = "/mahpastes",
        },
    },
}

-- Handler implementations
function on_startup()
    local api_key = storage.get("setting:api_key")
    if api_key then
        log("Cloud Sync initialized")
    else
        log("Cloud Sync: Please configure your API key")
    end
end

function on_clip_created(clip)
    local auto_sync = storage.get("setting:auto_sync")
    if auto_sync then
        sync_clip(clip)
    end
end

function scheduled_sync_check()
    log("Checking for sync conflicts...")
    -- Sync logic here
end

function sync_clip(clip)
    local api_key = storage.get("setting:api_key")
    local quality = storage.get("setting:sync_quality") or "high"
    local folder = storage.get("setting:folder_path") or "/mahpastes"

    -- Upload implementation
    log("Syncing clip: " .. clip.filename)
end
```

## Next Steps

- [Event Handling](./event-handling) — Detailed event reference
- [Settings & Storage](./settings-storage) — Working with persistent data
- [API Reference](../api-reference) — All available functions
