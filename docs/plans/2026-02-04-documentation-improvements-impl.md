# Documentation Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add comprehensive documentation for Tags feature and Plugin system, plus update existing docs with missing details.

**Architecture:** Create 9 new Markdown files in Docusaurus format, update 4 existing files. All work in `docs/` directory. Follow existing doc style (Docusaurus frontmatter, Tailwind class references for UI, code blocks for examples).

**Tech Stack:** Docusaurus, Markdown, Lua (for plugin examples)

---

## Task 1: Tags Feature Page

**Files:**
- Create: `docs/docs/features/tags.md`

**Step 1: Create the tags feature documentation**

```markdown
---
sidebar_position: 5
---

# Tags

Organize your clips with color-coded tags for quick filtering and categorization.

## What are Tags?

Tags are labels you can attach to any clip. Each tag has:
- A **name** (e.g., "work", "screenshots", "reference")
- A **color** automatically assigned from 8 options

Use tags to:
- Group related clips across content types
- Filter the gallery to find clips quickly
- Mark clips for specific projects or purposes

## Creating Tags

### From the Tag Manager

1. Click the **Tags** button in the header
2. Click **New Tag**
3. Enter a name and press Enter

Tags are assigned colors automatically in rotation: stone, red, amber, green, blue, violet, pink, cyan.

### While Assigning to a Clip

1. Click the tag icon on any clip card
2. Type a new tag name in the input field
3. Press Enter to create and assign in one step

## Assigning Tags to Clips

1. Click the **tag icon** on a clip card
2. Select existing tags from the dropdown
3. Click a tag to toggle it on/off

Tags appear as colored badges on the clip card.

## Filtering by Tags

Click any tag badge in the **filter bar** (below the search) to show only clips with that tag.

- Click a tag to enable the filter
- Click again to disable
- Multiple tags: shows clips matching **any** selected tag

## Managing Tags

### Rename a Tag

1. Open the Tag Manager
2. Click the tag name
3. Edit and press Enter

### Change Tag Color

1. Open the Tag Manager
2. Click the color circle next to a tag
3. Select a new color from the palette

### Delete a Tag

1. Open the Tag Manager
2. Click the delete icon next to a tag
3. Confirm deletion

:::warning
Deleting a tag removes it from all clips. This cannot be undone.
:::

## Bulk Tagging

Apply or remove tags from multiple clips at once:

1. Enter **selection mode** (click "Select" or press <kbd>Shift</kbd>)
2. Click clips to select them
3. Click **Tag** in the bulk actions bar
4. Choose tags to add or remove

## Tag Colors

Tags cycle through 8 colors in order:

| Color | Hex |
|-------|-----|
| Stone | `#78716C` |
| Red | `#EF4444` |
| Amber | `#F59E0B` |
| Green | `#22C55E` |
| Blue | `#3B82F6` |
| Violet | `#8B5CF6` |
| Pink | `#EC4899` |
| Cyan | `#06B6D4` |

The color is assigned based on how many tags exist when you create one.
```

**Step 2: Commit**

```bash
git add docs/docs/features/tags.md
git commit -m "docs: add Tags feature page"
```

---

## Task 2: Plugins Overview Page

**Files:**
- Create: `docs/docs/plugins/overview.md`

**Step 1: Create the plugins overview**

```markdown
---
sidebar_position: 1
---

# Plugins Overview

Extend mahpastes with Lua plugins that automate workflows, integrate with external services, and customize behavior.

## What Plugins Can Do

### Automate Workflows

- Auto-tag clips based on content or source
- Move clips to archive after processing
- Clean up old clips on a schedule

### Integrate with Services

- Sync clips to cloud storage (Dropbox, S3)
- Send notifications via webhooks
- Post to APIs when clips are created

### Transform Content

- Compress images automatically
- Format or validate JSON/code
- Extract text from images (via external APIs)

### React to Events

Plugins respond to app events:
- Clip created, deleted, archived
- File detected in watch folder
- Tags added or removed

### Run Scheduled Tasks

Periodic tasks run in the background:
- Hourly cleanup of old clips
- Daily sync to backup location
- Periodic health checks

## How Plugins Work

### Single-File Architecture

Each plugin is a single `.lua` file containing:
- A **manifest** declaring metadata and permissions
- **Handler functions** responding to events
- **Scheduled tasks** running periodically

### Sandboxed Execution

Plugins run in a sandboxed Lua environment:
- No access to system commands
- Network requests restricted to declared domains
- Filesystem access requires user approval

### Permission Model

Before a plugin can:
- **Make HTTP requests** — Must declare allowed domains in manifest
- **Read/write files** — Must declare intent; user approves specific folders
- **Access clips/tags** — Always allowed (core functionality)

## Example Use Cases

| Use Case | Events | APIs Used |
|----------|--------|-----------|
| Auto-tag screenshots | `clip:created` | `tags`, `clips` |
| Webhook notifications | `clip:created` | `http`, `storage` |
| Periodic cleanup | Scheduled (hourly) | `clips` |
| Watch folder organizer | `watch:import_complete` | `tags`, `clips` |
| Cloud backup | `clip:created` | `http`, `clips` |

## Getting Started

- **Users:** See [Installing Plugins](./installing-plugins) to add plugins
- **Developers:** See [Writing Plugins](./writing-plugins/getting-started) to create your own
```

**Step 2: Create the plugins directory**

```bash
mkdir -p docs/docs/plugins
```

**Step 3: Commit**

```bash
git add docs/docs/plugins/overview.md
git commit -m "docs: add Plugins overview page"
```

---

## Task 3: Installing Plugins Page

**Files:**
- Create: `docs/docs/plugins/installing-plugins.md`

**Step 1: Create the installation guide**

```markdown
---
sidebar_position: 2
---

# Installing Plugins

Add, configure, and manage plugins through the Settings panel.

## Adding a Plugin

1. Open **Settings** (gear icon in header)
2. Navigate to the **Plugins** tab
3. Click **Import Plugin**
4. Select a `.lua` file from your computer
5. Review the permissions requested
6. Click **Install**

The plugin activates immediately after installation.

## Reviewing Permissions

Before installing, review what the plugin requests:

### Network Permissions

Shows which domains the plugin can contact:

```
Network Access:
  api.dropbox.com — GET, POST
  hooks.slack.com — POST
```

Only listed domains with listed methods are allowed.

### Filesystem Permissions

Shows if the plugin wants to read or write files:

```
Filesystem Access:
  Read: Yes
  Write: Yes
```

You'll approve specific folders the first time the plugin tries to access them.

## Configuring Plugin Settings

Some plugins have configurable settings:

1. Click the **gear icon** next to a plugin
2. Fill in the settings form
3. Click **Save**

Settings types include:
- **Text** — Free-form input
- **Password** — Hidden input (for API keys)
- **Checkbox** — On/off toggle
- **Select** — Dropdown choices

## Enabling and Disabling

Toggle a plugin on/off without removing it:

1. Find the plugin in the list
2. Click the **toggle switch**

Disabled plugins:
- Don't respond to events
- Don't run scheduled tasks
- Keep their settings and permissions

## Viewing Plugin Logs

See what a plugin is doing:

1. Click the **log icon** next to a plugin
2. View recent log entries

Logs show:
- Handler executions with timestamps
- Errors and warnings
- Custom `log()` messages from the plugin

## Revoking Permissions

Remove filesystem permissions granted to a plugin:

1. Click the **permissions icon** next to a plugin
2. View granted folder permissions
3. Click **Revoke** next to any permission

The plugin will need to request access again.

## Removing a Plugin

1. Click the **delete icon** next to a plugin
2. Confirm removal

This removes:
- The plugin code
- All granted permissions
- Plugin storage data

:::note
Plugin settings and storage are deleted when you remove a plugin. Export any important data first.
:::

## Troubleshooting

### Plugin shows "Error" status

The plugin failed 3 times in a row. Options:
- View logs to diagnose the issue
- Disable and re-enable to retry
- Remove and reinstall if the plugin was updated

### Plugin not responding to events

Check that:
- The plugin is enabled (toggle is on)
- The plugin subscribes to that event (check plugin docs)
- No errors in the plugin log

### Filesystem prompts appearing repeatedly

The plugin is accessing new folders. Either:
- Approve the folders it needs
- Check if the plugin is misconfigured
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/installing-plugins.md
git commit -m "docs: add Installing Plugins guide"
```

---

## Task 4: Writing Plugins - Getting Started

**Files:**
- Create: `docs/docs/plugins/writing-plugins/getting-started.md`

**Step 1: Create the getting started guide**

```markdown
---
sidebar_position: 1
---

# Getting Started with Plugin Development

Create your first mahpastes plugin in 5 minutes.

## Plugin Structure

A plugin is a single `.lua` file with two parts:

1. **Manifest** — A `Plugin` table declaring metadata and permissions
2. **Handlers** — Functions that respond to events

## Your First Plugin

Create a file called `hello.lua`:

```lua
-- Manifest: declares plugin metadata
Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "Logs a message when the app starts",
    author = "Your Name",

    -- Subscribe to app startup event
    events = {"app:startup"},
}

-- Handler: runs when app starts
function on_startup()
    log("Hello from my first plugin!")
end
```

## Installing Your Plugin

1. Open **Settings** → **Plugins**
2. Click **Import Plugin**
3. Select your `hello.lua` file
4. Click **Install**

## Verifying It Works

1. Restart mahpastes (or disable/enable the plugin)
2. Click the **log icon** next to your plugin
3. You should see: `Hello from my first plugin!`

## Adding Event Handlers

Let's extend the plugin to react when clips are created:

```lua
Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "Logs messages for app and clip events",
    author = "Your Name",

    events = {"app:startup", "clip:created"},
}

function on_startup()
    log("Hello from my first plugin!")
end

function on_clip_created(clip)
    log("New clip created: " .. clip.filename)
    log("Content type: " .. clip.content_type)
end
```

## Handler Naming Convention

Event names map to handler functions:

| Event | Handler Function |
|-------|------------------|
| `app:startup` | `on_startup()` |
| `clip:created` | `on_clip_created(clip)` |
| `clip:deleted` | `on_clip_deleted(clip_id)` |
| `tag:added_to_clip` | `on_tag_added_to_clip(data)` |

Pattern: Replace `:` with `_` and prefix with `on_`.

## Using the Clips API

Access and modify clips from your handlers:

```lua
Plugin = {
    name = "Clip Counter",
    version = "1.0.0",
    events = {"app:startup"},
}

function on_startup()
    local all_clips = clips.list()
    log("Total clips: " .. #all_clips)

    local images = clips.list({content_type = "image/%"})
    log("Image clips: " .. #images)
end
```

## Next Steps

- [Plugin Manifest](./plugin-manifest) — All configuration options
- [Event Handling](./event-handling) — Complete event reference
- [Settings & Storage](./settings-storage) — Persist data and user config
- [API Reference](../api-reference) — All available functions
```

**Step 2: Create the writing-plugins directory**

```bash
mkdir -p docs/docs/plugins/writing-plugins
```

**Step 3: Commit**

```bash
git add docs/docs/plugins/writing-plugins/getting-started.md
git commit -m "docs: add Writing Plugins getting started guide"
```

---

## Task 5: Writing Plugins - Plugin Manifest

**Files:**
- Create: `docs/docs/plugins/writing-plugins/plugin-manifest.md`

**Step 1: Create the manifest reference**

```markdown
---
sidebar_position: 2
---

# Plugin Manifest

The `Plugin` table at the top of your plugin file declares metadata, permissions, and capabilities.

## Basic Structure

```lua
Plugin = {
    -- Required
    name = "My Plugin",

    -- Optional metadata
    version = "1.0.0",
    description = "What this plugin does",
    author = "Your Name",

    -- Capabilities (all optional)
    events = {},
    network = {},
    filesystem = {},
    schedules = {},
    settings = {},
}
```

## Required Fields

### name

```lua
name = "My Plugin"
```

Display name shown in the plugins list. Must be unique.

## Optional Metadata

### version

```lua
version = "1.0.0"
```

Semantic version string. Shown in plugin details.

### description

```lua
description = "Syncs clips to cloud storage"
```

Brief explanation shown during install and in plugin list.

### author

```lua
author = "Jane Developer"
```

Creator attribution.

## Events

Subscribe to app events:

```lua
events = {"app:startup", "clip:created", "tag:added_to_clip"}
```

See [Event Handling](./event-handling) for all available events.

## Network Permissions

Declare which domains your plugin can contact:

```lua
network = {
    ["api.example.com"] = {"GET", "POST"},
    ["hooks.slack.com"] = {"POST"},
}
```

**Format:** `["domain"] = {"METHOD1", "METHOD2"}`

- Only listed domains are accessible
- Only listed HTTP methods are allowed
- Requests to other domains throw errors

**Users see this during install:**
```
Network Access:
  api.example.com — GET, POST
  hooks.slack.com — POST
```

## Filesystem Permissions

Declare intent to access the filesystem:

```lua
filesystem = {
    read = true,
    write = true,
}
```

**What this means:**
- `read = true` — Plugin may read files (user approves folders)
- `write = true` — Plugin may write files (user approves folders)

**First access triggers a folder picker.** The user selects which folder to grant access to. Permissions are remembered.

## Scheduled Tasks

Run functions periodically:

```lua
schedules = {
    {name = "cleanup", interval = 3600},      -- Every hour
    {name = "sync_check", interval = 300},    -- Every 5 minutes
}
```

**Fields:**
- `name` — Function name to call (e.g., `cleanup` calls `function cleanup()`)
- `interval` — Seconds between runs

**Example handler:**

```lua
function cleanup()
    log("Running scheduled cleanup...")
    -- Your cleanup logic
end
```

Scheduled tasks:
- Start running after plugin loads
- Don't overlap (next run waits for current to finish)
- Have the same 30-second timeout as event handlers

## Settings

Declare user-configurable settings:

```lua
settings = {
    {
        key = "api_key",
        type = "password",
        label = "API Key",
        description = "Your service API key",
    },
    {
        key = "enabled",
        type = "checkbox",
        label = "Enable Sync",
        default = true,
    },
    {
        key = "quality",
        type = "select",
        label = "Image Quality",
        options = {"low", "medium", "high"},
        default = "medium",
    },
}
```

### Setting Fields

| Field | Required | Description |
|-------|----------|-------------|
| `key` | Yes | Storage key (accessed via `storage.get("setting:key")`) |
| `type` | Yes | Input type: `text`, `password`, `checkbox`, `select` |
| `label` | Yes | Display label in settings UI |
| `description` | No | Help text below the input |
| `default` | No | Default value |
| `options` | For select | Array of choices |

### Reading Settings

```lua
local api_key = storage.get("setting:api_key")
local enabled = storage.get("setting:enabled")
local quality = storage.get("setting:quality")
```

Settings are prefixed with `setting:` in storage.

## Complete Example

```lua
Plugin = {
    name = "Cloud Sync",
    version = "2.1.0",
    description = "Automatically sync new clips to cloud storage",
    author = "Jane Developer",

    events = {"clip:created", "app:startup"},

    network = {
        ["api.cloudstorage.com"] = {"GET", "POST", "PUT"},
    },

    filesystem = {
        read = true,
    },

    schedules = {
        {name = "retry_failed", interval = 300},
    },

    settings = {
        {key = "api_key", type = "password", label = "API Key"},
        {key = "auto_sync", type = "checkbox", label = "Auto-sync new clips", default = true},
    },
}
```
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/writing-plugins/plugin-manifest.md
git commit -m "docs: add Plugin Manifest reference"
```

---

## Task 6: Writing Plugins - Event Handling

**Files:**
- Create: `docs/docs/plugins/writing-plugins/event-handling.md`

**Step 1: Create the event handling guide**

```markdown
---
sidebar_position: 3
---

# Event Handling

Plugins respond to events by defining handler functions.

## How Events Work

1. Something happens in the app (clip created, tag added, etc.)
2. mahpastes checks which plugins subscribe to that event
3. Each subscribed plugin's handler is called with event data
4. Handlers run in sequence (not parallel)

## Subscribing to Events

List events in your manifest:

```lua
Plugin = {
    name = "My Plugin",
    events = {"clip:created", "clip:deleted", "app:startup"},
}
```

## Handler Naming Convention

Convert event name to function name:
- Replace `:` with `_`
- Prefix with `on_`

| Event | Handler |
|-------|---------|
| `app:startup` | `on_startup()` |
| `clip:created` | `on_clip_created(clip)` |
| `watch:import_complete` | `on_watch_import_complete(data)` |

## Available Events

### App Lifecycle

#### app:startup

Fired when mahpastes finishes initializing.

```lua
function on_startup()
    log("Plugin initialized")
    -- Load saved state, initialize connections
end
```

**Payload:** None

#### app:shutdown

Fired when mahpastes is closing.

```lua
function on_shutdown()
    log("Plugin shutting down")
    -- Save state, close connections
end
```

**Payload:** None

### Clip Events

#### clip:created

Fired when a new clip is added (paste, drag-drop, or watch import).

```lua
function on_clip_created(clip)
    log("New clip: " .. clip.filename)
    log("Type: " .. clip.content_type)
    log("ID: " .. clip.id)
end
```

**Payload:**
```lua
{
    id = 42,
    content_type = "image/png",
    filename = "screenshot.png",
    created_at = "2024-01-15T10:30:00Z",
    is_archived = false,
}
```

#### clip:deleted

Fired when a clip is deleted.

```lua
function on_clip_deleted(clip_id)
    log("Clip deleted: " .. clip_id)
    -- Clean up any external references
end
```

**Payload:** `clip_id` (number) — The ID of the deleted clip. Full data is unavailable since the clip is gone.

#### clip:archived

Fired when a clip is moved to archive.

```lua
function on_clip_archived(clip)
    log("Clip archived: " .. clip.filename)
end
```

**Payload:** Same as `clip:created`

#### clip:unarchived

Fired when a clip is restored from archive.

```lua
function on_clip_unarchived(clip)
    log("Clip unarchived: " .. clip.filename)
end
```

**Payload:** Same as `clip:created`

### Watch Folder Events

#### watch:file_detected

Fired when a file is detected in a watched folder (before import).

```lua
function on_watch_file_detected(data)
    log("File detected: " .. data.path)
    log("In folder: " .. data.folder_id)
end
```

**Payload:**
```lua
{
    path = "/Users/you/Screenshots/shot.png",
    folder_id = 1,
}
```

#### watch:import_complete

Fired after a watched file is successfully imported as a clip.

```lua
function on_watch_import_complete(data)
    log("Imported: " .. data.source_path)
    log("As clip: " .. data.clip_id)
end
```

**Payload:**
```lua
{
    clip_id = 42,
    source_path = "/Users/you/Screenshots/shot.png",
    folder_id = 1,
}
```

### Tag Events

#### tag:created

Fired when a new tag is created.

```lua
function on_tag_created(tag)
    log("New tag: " .. tag.name)
end
```

**Payload:**
```lua
{
    id = 5,
    name = "important",
    color = "#EF4444",
}
```

#### tag:updated

Fired when a tag's name or color changes.

```lua
function on_tag_updated(tag)
    log("Tag updated: " .. tag.name)
end
```

**Payload:** Same as `tag:created`

#### tag:deleted

Fired when a tag is deleted.

```lua
function on_tag_deleted(tag_id)
    log("Tag deleted: " .. tag_id)
end
```

**Payload:** `tag_id` (number)

#### tag:added_to_clip

Fired when a tag is assigned to a clip.

```lua
function on_tag_added_to_clip(data)
    log("Tag " .. data.tag_id .. " added to clip " .. data.clip_id)
end
```

**Payload:**
```lua
{
    tag_id = 5,
    clip_id = 42,
}
```

#### tag:removed_from_clip

Fired when a tag is removed from a clip.

```lua
function on_tag_removed_from_clip(data)
    log("Tag " .. data.tag_id .. " removed from clip " .. data.clip_id)
end
```

**Payload:** Same as `tag:added_to_clip`

## Timeouts and Errors

### 30-Second Timeout

Each handler has 30 seconds to complete. If exceeded:
- Handler is terminated
- Error is logged
- Plugin continues running (but error count increases)

### Error Handling

Use `pcall` for operations that might fail:

```lua
function on_clip_created(clip)
    local ok, err = pcall(function()
        http.post(webhook_url, {body = json.encode(clip)})
    end)

    if not ok then
        log("Webhook failed: " .. err)
        -- Save for retry
        storage.set("pending:" .. clip.id, json.encode(clip))
    end
end
```

### Plugin Error State

After 3 consecutive handler failures:
- Plugin enters "error" state
- User is notified
- Plugin must be manually re-enabled

## Best Practices

### Keep Handlers Fast

Do minimal work in handlers. For heavy processing:
- Save data to storage
- Process in a scheduled task

### Handle Missing Data

Event payloads may have nil fields:

```lua
function on_clip_created(clip)
    local filename = clip.filename or "unnamed"
    log("New clip: " .. filename)
end
```

### Don't Block on Network

Network requests can be slow. Consider:
- Queuing requests for batch processing
- Using scheduled tasks for retries
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/writing-plugins/event-handling.md
git commit -m "docs: add Event Handling guide"
```

---

## Task 7: Writing Plugins - Settings & Storage

**Files:**
- Create: `docs/docs/plugins/writing-plugins/settings-storage.md`

**Step 1: Create the settings and storage guide**

```markdown
---
sidebar_position: 4
---

# Settings & Storage

Persist configuration and state between plugin restarts.

## Plugin Storage

Every plugin has isolated key-value storage backed by SQLite.

### Basic Operations

```lua
-- Save a value
storage.set("last_sync", os.time())

-- Read a value (nil if not set)
local last_sync = storage.get("last_sync")

-- Delete a value
storage.delete("last_sync")

-- List all keys
local keys = storage.list()
for _, key in ipairs(keys) do
    log("Key: " .. key)
end
```

### Storing Complex Data

Use JSON for tables and complex values:

```lua
-- Save a table
local pending = {123, 456, 789}
storage.set("pending_ids", json.encode(pending))

-- Load a table
local data = storage.get("pending_ids")
if data then
    pending = json.decode(data)
end
```

### Storage Limits

- **10 MB** total storage per plugin
- Writes rejected when limit exceeded
- User notified when approaching limit

## User-Configurable Settings

Let users configure your plugin without editing code.

### Declaring Settings

Add a `settings` array to your manifest:

```lua
Plugin = {
    name = "My Plugin",
    settings = {
        {
            key = "webhook_url",
            type = "text",
            label = "Webhook URL",
            description = "URL to send notifications to",
        },
        {
            key = "api_key",
            type = "password",
            label = "API Key",
        },
        {
            key = "auto_sync",
            type = "checkbox",
            label = "Auto-sync new clips",
            default = true,
        },
        {
            key = "quality",
            type = "select",
            label = "Image Quality",
            options = {"low", "medium", "high"},
            default = "medium",
        },
    },
}
```

### Setting Types

| Type | UI Element | Value Type |
|------|------------|------------|
| `text` | Text input | String |
| `password` | Hidden input | String |
| `checkbox` | Toggle switch | Boolean |
| `select` | Dropdown | String (from options) |

### Reading Settings

Settings are stored with the `setting:` prefix:

```lua
function on_startup()
    local webhook_url = storage.get("setting:webhook_url")
    local api_key = storage.get("setting:api_key")
    local auto_sync = storage.get("setting:auto_sync")
    local quality = storage.get("setting:quality")

    if not webhook_url then
        log("Warning: Webhook URL not configured")
        return
    end

    log("Plugin ready with quality: " .. quality)
end
```

### Default Values

Defaults from the manifest are used when:
- Plugin is first installed
- User clears a setting

```lua
{
    key = "retry_count",
    type = "text",
    label = "Retry Count",
    default = "3",
}
```

## Common Patterns

### Initialization State

Track whether initialization completed:

```lua
function on_startup()
    -- Check if we already initialized
    if storage.get("initialized") then
        log("Already initialized")
        return
    end

    -- Do one-time setup
    setup_external_connection()

    storage.set("initialized", "true")
    log("Initialization complete")
end
```

### Pending Queue

Queue items for retry:

```lua
function on_clip_created(clip)
    local ok, err = pcall(function()
        sync_clip(clip.id)
    end)

    if not ok then
        -- Add to pending queue
        local pending = json.decode(storage.get("pending") or "[]")
        table.insert(pending, clip.id)
        storage.set("pending", json.encode(pending))
        log("Queued for retry: " .. clip.id)
    end
end

-- Scheduled task to process pending
function retry_pending()
    local pending = json.decode(storage.get("pending") or "[]")
    local still_pending = {}

    for _, id in ipairs(pending) do
        local ok = pcall(function() sync_clip(id) end)
        if not ok then
            table.insert(still_pending, id)
        end
    end

    storage.set("pending", json.encode(still_pending))
end
```

### Tracking Last Run

Remember when a task last ran:

```lua
function cleanup()
    local last_run = tonumber(storage.get("cleanup_last_run") or "0")
    local now = utils.time()

    -- Skip if ran recently (within 1 hour)
    if now - last_run < 3600 then
        log("Cleanup ran recently, skipping")
        return
    end

    -- Do cleanup...

    storage.set("cleanup_last_run", tostring(now))
end
```

### Caching API Responses

Cache expensive API calls:

```lua
function get_user_info()
    local cached = storage.get("user_info")
    local cache_time = tonumber(storage.get("user_info_time") or "0")

    -- Return cached if fresh (within 1 hour)
    if cached and (utils.time() - cache_time) < 3600 then
        return json.decode(cached)
    end

    -- Fetch fresh data
    local response = http.get("https://api.example.com/user", {
        headers = {Authorization = "Bearer " .. storage.get("setting:api_key")}
    })

    if response.status == 200 then
        storage.set("user_info", response.body)
        storage.set("user_info_time", tostring(utils.time()))
        return json.decode(response.body)
    end

    -- Return stale cache if fetch failed
    if cached then
        return json.decode(cached)
    end

    return nil
end
```

## Debugging Storage

View your plugin's storage:

1. Click the **log icon** next to your plugin
2. Storage operations are logged with their keys and values

Or add debug logging:

```lua
function debug_storage()
    local keys = storage.list()
    log("Storage keys: " .. #keys)
    for _, key in ipairs(keys) do
        local value = storage.get(key)
        log("  " .. key .. " = " .. tostring(value))
    end
end
```
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/writing-plugins/settings-storage.md
git commit -m "docs: add Settings & Storage guide"
```

---

## Task 8: Plugin API Reference

**Files:**
- Create: `docs/docs/plugins/api-reference.md`

**Step 1: Create the API reference**

```markdown
---
sidebar_position: 4
---

# Plugin API Reference

Complete reference for all Lua APIs available to plugins.

## clips

Manage clipboard content.

### clips.list

Query clips with optional filtering.

```lua
-- All clips
local all = clips.list()

-- Filter by content type (SQL LIKE pattern)
local images = clips.list({content_type = "image/%"})
local pngs = clips.list({content_type = "image/png"})
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `filter` | table (optional) | Filter criteria |
| `filter.content_type` | string | SQL LIKE pattern for content type |

**Returns:** Array of clip objects (without data field)

```lua
{
    {id = 1, content_type = "image/png", filename = "shot.png", created_at = "...", is_archived = false},
    {id = 2, content_type = "text/plain", filename = "note.txt", created_at = "...", is_archived = false},
}
```

### clips.get

Get a single clip with its data.

```lua
local clip = clips.get(42)
if clip then
    log("Content type: " .. clip.content_type)
    log("Data length: " .. #clip.data)
end
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | number | Clip ID |

**Returns:** Clip object with data, or `nil` if not found

```lua
{
    id = 42,
    content_type = "text/plain",
    filename = "note.txt",
    data = "Hello, world!",
    created_at = "2024-01-15T10:30:00Z",
    is_archived = false,
}
```

### clips.create

Create a new clip.

```lua
local id = clips.create({
    data = "Hello, world!",
    content_type = "text/plain",
    filename = "greeting.txt",  -- optional
})
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `data` | string | Clip content (text or binary) |
| `content_type` | string | MIME type |
| `filename` | string (optional) | Display filename |

**Returns:** New clip ID (number)

### clips.update

Update clip metadata.

```lua
clips.update(42, {is_archived = true})
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | number | Clip ID |
| `updates` | table | Fields to update |
| `updates.is_archived` | boolean | Archive status |

**Returns:** `true` on success

### clips.delete

Delete a single clip.

```lua
clips.delete(42)
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | number | Clip ID |

**Returns:** `true` on success

### clips.delete_many

Delete multiple clips.

```lua
clips.delete_many({1, 2, 3})
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `ids` | array | Array of clip IDs |

**Returns:** `true` on success

### clips.archive / clips.unarchive

Convenience methods for archiving.

```lua
clips.archive(42)    -- Set is_archived = true
clips.unarchive(42)  -- Set is_archived = false
```

---

## tags

Manage tags and clip-tag associations.

### tags.list

Get all tags with usage counts.

```lua
local all_tags = tags.list()
for _, tag in ipairs(all_tags) do
    log(tag.name .. ": " .. tag.count .. " clips")
end
```

**Returns:** Array of tag objects

```lua
{
    {id = 1, name = "work", color = "#3B82F6", count = 15},
    {id = 2, name = "screenshots", color = "#EF4444", count = 42},
}
```

### tags.get

Get a single tag by ID.

```lua
local tag = tags.get(1)
```

**Returns:** Tag object or `nil`

### tags.create

Create a new tag (color auto-assigned).

```lua
local tag, err = tags.create("important")
if tag then
    log("Created tag: " .. tag.name .. " with color " .. tag.color)
else
    log("Error: " .. err)
end
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `name` | string | Tag name (max 50 chars) |

**Returns:** Tag object, or `nil, error_message`

### tags.update

Update a tag's name or color.

```lua
tags.update(1, {name = "urgent", color = "#EF4444"})
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `id` | number | Tag ID |
| `updates.name` | string (optional) | New name |
| `updates.color` | string (optional) | New color (hex) |

**Returns:** `true` on success, or `false, error_message`

### tags.delete

Delete a tag (removes from all clips).

```lua
tags.delete(1)
```

**Returns:** `true` on success

### tags.add_to_clip

Assign a tag to a clip.

```lua
local ok, err = tags.add_to_clip(1, 42)  -- tag_id, clip_id
```

**Returns:** `true` on success, or `false, error_message`

### tags.remove_from_clip

Remove a tag from a clip.

```lua
tags.remove_from_clip(1, 42)  -- tag_id, clip_id
```

**Returns:** `true` on success

### tags.get_for_clip

Get all tags assigned to a clip.

```lua
local clip_tags = tags.get_for_clip(42)
```

**Returns:** Array of tag objects

---

## storage

Plugin-scoped key-value storage.

### storage.get

Read a value.

```lua
local value = storage.get("my_key")
if value then
    log("Found: " .. value)
else
    log("Key not set")
end
```

**Returns:** Value as string, or `nil` if not set

### storage.set

Write a value.

```lua
storage.set("my_key", "my_value")
storage.set("count", tostring(42))
storage.set("data", json.encode({a = 1, b = 2}))
```

**Note:** Values are stored as strings. Use `json.encode` for tables.

### storage.delete

Remove a key.

```lua
storage.delete("my_key")
```

### storage.list

Get all keys for this plugin.

```lua
local keys = storage.list()
for _, key in ipairs(keys) do
    log("Key: " .. key)
end
```

**Returns:** Array of key strings

---

## http

Make HTTP requests (restricted to declared domains).

### http.get

```lua
local response = http.get("https://api.example.com/data", {
    headers = {
        ["Authorization"] = "Bearer token123",
        ["Accept"] = "application/json",
    },
})
```

### http.post

```lua
local response = http.post("https://api.example.com/items", {
    headers = {
        ["Content-Type"] = "application/json",
    },
    body = json.encode({name = "test"}),
})
```

### http.put / http.patch / http.delete

Same signature as `http.post`.

### Response Format

All HTTP methods return:

```lua
{
    status = 200,
    headers = {
        ["Content-Type"] = "application/json",
        -- ...
    },
    body = '{"result": "success"}',
}
```

### Domain Restrictions

Requests to domains not in your manifest throw errors:

```lua
-- If api.example.com not declared:
http.get("https://api.example.com/data")
-- Error: domain not in allowlist

-- If only GET declared for domain:
http.post("https://api.example.com/data", {})
-- Error: POST not allowed for this domain
```

---

## fs

Filesystem operations (requires permission prompts).

### fs.read

Read a file's contents.

```lua
local content, err = fs.read("/path/to/file.txt")
if content then
    log("Read " .. #content .. " bytes")
else
    log("Error: " .. err)
end
```

**First access to a path triggers a folder picker.** User must approve.

### fs.write

Write content to a file.

```lua
local ok, err = fs.write("/path/to/output.txt", "Hello, world!")
```

### fs.list

List directory contents.

```lua
local entries, err = fs.list("/path/to/dir")
if entries then
    for _, entry in ipairs(entries) do
        log(entry.name .. (entry.is_dir and "/" or ""))
    end
end
```

**Returns:** Array of entries

```lua
{
    {name = "file.txt", is_dir = false, size = 1024, modified = 1705312200},
    {name = "subdir", is_dir = true, size = 0, modified = 1705312100},
}
```

### fs.exists

Check if a path exists (no permission prompt).

```lua
if fs.exists("/path/to/file.txt") then
    log("File exists")
end
```

---

## toast

Show notifications to the user.

### toast.show

```lua
toast.show("Sync complete!")
toast.show("Upload failed", "error")
toast.show("Processing...", "info")
```

**Parameters:**
| Name | Type | Description |
|------|------|-------------|
| `message` | string | Notification text |
| `type` | string (optional) | "info" (default), "success", or "error" |

---

## Utility Functions

### log

Write to the plugin log.

```lua
log("Something happened")
log("Count: " .. tostring(count))
```

### json.encode / json.decode

Convert between Lua tables and JSON strings.

```lua
local str = json.encode({a = 1, b = "hello"})
-- '{"a":1,"b":"hello"}'

local tbl = json.decode('{"x": 10}')
-- {x = 10}
```

### base64.encode / base64.decode

Convert between binary data and base64 strings.

```lua
local encoded = base64.encode("Hello")
-- "SGVsbG8="

local decoded = base64.decode("SGVsbG8=")
-- "Hello"
```

### utils.time

Get current Unix timestamp.

```lua
local now = utils.time()
log("Current time: " .. now)
```

---

## Resource Limits

| Resource | Limit | On Violation |
|----------|-------|--------------|
| Execution time | 30s per handler | Terminate, notify user |
| Memory | 50MB per plugin | Terminate, notify user |
| HTTP requests | 100/minute | Throttle, log warning |
| File operations | 50/minute | Throttle, log warning |
| Storage | 10MB per plugin | Reject writes, notify |
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/api-reference.md
git commit -m "docs: add Plugin API Reference"
```

---

## Task 9: Example Plugins

**Files:**
- Create: `docs/docs/plugins/example-plugins.md`

**Step 1: Create the examples page**

```markdown
---
sidebar_position: 5
---

# Example Plugins

Complete, working plugins demonstrating different capabilities.

## Example 1: Auto-Tagger

**Level:** Beginner
**Demonstrates:** Event handling, tags API

Automatically tags clips based on content:
- Images with "screenshot" in filename → tagged "screenshot"
- Clips from watch folders → tagged "imported"

```lua
Plugin = {
    name = "Auto-Tagger",
    version = "1.0.0",
    description = "Automatically tags clips based on source and content",
    author = "mahpastes",

    events = {"clip:created", "watch:import_complete"},
}

-- Ensure a tag exists, create if needed
local function ensure_tag(name)
    local all_tags = tags.list()
    for _, tag in ipairs(all_tags) do
        if tag.name == name then
            return tag.id
        end
    end

    local new_tag, err = tags.create(name)
    if new_tag then
        log("Created tag: " .. name)
        return new_tag.id
    else
        log("Failed to create tag: " .. err)
        return nil
    end
end

function on_clip_created(clip)
    -- Tag screenshots
    if clip.content_type:match("^image/") then
        local filename = (clip.filename or ""):lower()
        if filename:match("screenshot") or filename:match("screen shot") then
            local tag_id = ensure_tag("screenshot")
            if tag_id then
                tags.add_to_clip(tag_id, clip.id)
                log("Tagged clip " .. clip.id .. " as screenshot")
            end
        end
    end
end

function on_watch_import_complete(data)
    -- Tag all watch folder imports
    local tag_id = ensure_tag("imported")
    if tag_id then
        tags.add_to_clip(tag_id, data.clip_id)
        log("Tagged clip " .. data.clip_id .. " as imported")
    end
end
```

### How It Works

1. **on_clip_created**: Checks if the clip is an image with "screenshot" in the filename. If so, ensures the "screenshot" tag exists and applies it.

2. **on_watch_import_complete**: Runs after any watch folder import. Tags the clip as "imported".

3. **ensure_tag**: Helper function that finds an existing tag by name or creates it.

---

## Example 2: Webhook Notifier

**Level:** Intermediate
**Demonstrates:** HTTP requests, plugin settings, error handling

Sends a POST request to a webhook URL whenever a clip is created.

```lua
Plugin = {
    name = "Webhook Notifier",
    version = "1.0.0",
    description = "Sends notifications to a webhook when clips are created",
    author = "mahpastes",

    events = {"clip:created"},

    network = {
        -- Add your webhook domain here
        ["hooks.slack.com"] = {"POST"},
        ["discord.com"] = {"POST"},
        ["webhook.site"] = {"POST"},
    },

    settings = {
        {
            key = "webhook_url",
            type = "text",
            label = "Webhook URL",
            description = "Full URL to send POST requests to",
        },
        {
            key = "include_preview",
            type = "checkbox",
            label = "Include text preview",
            description = "Include first 100 chars of text clips",
            default = false,
        },
    },
}

function on_clip_created(clip)
    local webhook_url = storage.get("setting:webhook_url")
    if not webhook_url or webhook_url == "" then
        log("Webhook URL not configured, skipping")
        return
    end

    -- Build notification payload
    local payload = {
        event = "clip_created",
        clip = {
            id = clip.id,
            content_type = clip.content_type,
            filename = clip.filename,
            created_at = clip.created_at,
        },
    }

    -- Optionally include text preview
    local include_preview = storage.get("setting:include_preview")
    if include_preview == "true" and clip.content_type:match("^text/") then
        local full_clip = clips.get(clip.id)
        if full_clip and full_clip.data then
            payload.clip.preview = full_clip.data:sub(1, 100)
        end
    end

    -- Send webhook
    local ok, err = pcall(function()
        local response = http.post(webhook_url, {
            headers = {
                ["Content-Type"] = "application/json",
            },
            body = json.encode(payload),
        })

        if response.status >= 200 and response.status < 300 then
            log("Webhook sent for clip " .. clip.id)
        else
            log("Webhook failed with status " .. response.status)
        end
    end)

    if not ok then
        log("Webhook error: " .. tostring(err))
    end
end
```

### How It Works

1. **Settings**: User configures the webhook URL and whether to include text previews.

2. **on_clip_created**: Builds a JSON payload with clip metadata and sends it to the webhook.

3. **Error handling**: Uses `pcall` to catch network errors gracefully.

### Configuration

1. Install the plugin
2. Click the gear icon next to it
3. Enter your webhook URL (e.g., `https://hooks.slack.com/services/...`)
4. Optionally enable text preview

**Note:** Add your webhook domain to the `network` table before using.

---

## Example 3: Periodic Cleanup

**Level:** Intermediate
**Demonstrates:** Scheduled tasks, clip deletion, settings

Automatically deletes old unarchived clips based on configurable age.

```lua
Plugin = {
    name = "Periodic Cleanup",
    version = "1.0.0",
    description = "Automatically deletes old clips on a schedule",
    author = "mahpastes",

    events = {"app:startup"},

    schedules = {
        {name = "cleanup", interval = 3600},  -- Run every hour
    },

    settings = {
        {
            key = "max_age_days",
            type = "select",
            label = "Delete clips older than",
            options = {"7", "14", "30", "60", "90"},
            default = "30",
        },
        {
            key = "dry_run",
            type = "checkbox",
            label = "Dry run (log only, don't delete)",
            description = "Test what would be deleted without actually deleting",
            default = true,
        },
    },
}

function on_startup()
    log("Periodic Cleanup plugin loaded")
    local max_age = storage.get("setting:max_age_days") or "30"
    log("Will clean clips older than " .. max_age .. " days")
end

function cleanup()
    local max_age_days = tonumber(storage.get("setting:max_age_days") or "30")
    local dry_run = storage.get("setting:dry_run") == "true"
    local cutoff_time = utils.time() - (max_age_days * 24 * 60 * 60)

    log("Running cleanup, cutoff: " .. max_age_days .. " days ago")

    -- Get all non-archived clips
    local all_clips = clips.list()
    local to_delete = {}

    for _, clip in ipairs(all_clips) do
        -- Skip archived clips
        if clip.is_archived then
            goto continue
        end

        -- Parse created_at timestamp
        -- Format: 2024-01-15T10:30:00Z
        local year, month, day = clip.created_at:match("(%d+)-(%d+)-(%d+)")
        if year then
            local clip_time = os.time({
                year = tonumber(year),
                month = tonumber(month),
                day = tonumber(day),
            })

            if clip_time < cutoff_time then
                table.insert(to_delete, clip.id)
                log("Will delete: " .. (clip.filename or "clip " .. clip.id))
            end
        end

        ::continue::
    end

    if #to_delete == 0 then
        log("No old clips to delete")
        return
    end

    log("Found " .. #to_delete .. " clips to delete")

    if dry_run then
        log("DRY RUN: Would have deleted " .. #to_delete .. " clips")
        return
    end

    -- Delete in batches
    clips.delete_many(to_delete)
    log("Deleted " .. #to_delete .. " old clips")
    toast.show("Cleaned up " .. #to_delete .. " old clips", "success")
end
```

### How It Works

1. **Scheduled task**: `cleanup` runs every hour (3600 seconds).

2. **Age calculation**: Compares each clip's `created_at` to the configured max age.

3. **Safety features**:
   - Archived clips are never deleted
   - Dry run mode lets you test before enabling real deletion
   - Batch deletion for efficiency

### Configuration

1. Install the plugin
2. Set maximum age (default: 30 days)
3. **Important:** Disable "Dry run" when ready to actually delete

The plugin logs what it would/did delete, so check logs to verify behavior.

---

## Tips for Writing Plugins

### Start with Logging

```lua
function on_clip_created(clip)
    log("on_clip_created called")
    log("Clip ID: " .. clip.id)
    log("Type: " .. clip.content_type)
    -- Now add your logic
end
```

### Test with Dry Run

Add a dry run setting during development:

```lua
settings = {
    {key = "dry_run", type = "checkbox", label = "Dry run", default = true},
}
```

### Handle Nil Values

Event payloads may have nil fields:

```lua
local filename = clip.filename or "unnamed"
```

### Use pcall for External Calls

```lua
local ok, err = pcall(function()
    http.post(url, {body = data})
end)
if not ok then
    log("Failed: " .. err)
end
```
```

**Step 2: Commit**

```bash
git add docs/docs/plugins/example-plugins.md
git commit -m "docs: add Example Plugins page"
```

---

## Task 10: Update Existing Files

### Step 1: Update intro.md

**File:** `docs/docs/intro.md`

Add Tags and Plugins to the feature table (around line 46-56).

Find this table:
```markdown
| Feature | Description |
|---------|-------------|
| **Paste Anything** | Images, text, code, JSON, HTML, and files |
...
| **Bulk Actions** | Select multiple clips to archive, download, or delete |
```

Add after Bulk Actions:
```markdown
| **Tags** | Color-coded labels to organize and filter clips |
| **Plugins** | Extend functionality with Lua scripts |
```

### Step 2: Update database-schema.md

**File:** `docs/docs/developers/database-schema.md`

Add new tables section after the `settings` table section.

Add:
```markdown
### tags

Stores tag definitions.

\`\`\`sql
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    color TEXT NOT NULL
);
\`\`\`

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `name` | TEXT | Tag name (unique) |
| `color` | TEXT | Hex color code (e.g., "#EF4444") |

### clip_tags

Junction table for clip-tag relationships.

\`\`\`sql
CREATE TABLE clip_tags (
    clip_id INTEGER,
    tag_id INTEGER,
    PRIMARY KEY (clip_id, tag_id),
    FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
\`\`\`

| Column | Type | Description |
|--------|------|-------------|
| `clip_id` | INTEGER | Reference to clips.id |
| `tag_id` | INTEGER | Reference to tags.id |

### plugins

Stores installed plugin metadata.

\`\`\`sql
CREATE TABLE plugins (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    version TEXT,
    enabled INTEGER DEFAULT 1,
    status TEXT DEFAULT 'enabled',
    error_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
\`\`\`

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `filename` | TEXT | Plugin filename (unique) |
| `name` | TEXT | Display name from manifest |
| `version` | TEXT | Version from manifest |
| `enabled` | INTEGER | 0 = disabled, 1 = enabled |
| `status` | TEXT | "enabled", "disabled", or "error" |
| `error_count` | INTEGER | Consecutive handler failures |
| `created_at` | DATETIME | When plugin was installed |

### plugin_permissions

Stores granted filesystem permissions for plugins.

\`\`\`sql
CREATE TABLE plugin_permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plugin_id INTEGER NOT NULL,
    permission_type TEXT NOT NULL,
    path TEXT NOT NULL,
    granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
);
\`\`\`

| Column | Type | Description |
|--------|------|-------------|
| `plugin_id` | INTEGER | Reference to plugins.id |
| `permission_type` | TEXT | "fs_read" or "fs_write" |
| `path` | TEXT | Approved folder path |
| `granted_at` | DATETIME | When permission was granted |

### plugin_storage

Plugin-scoped key-value storage.

\`\`\`sql
CREATE TABLE plugin_storage (
    plugin_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    value BLOB,
    PRIMARY KEY (plugin_id, key),
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
);
\`\`\`

| Column | Type | Description |
|--------|------|-------------|
| `plugin_id` | INTEGER | Reference to plugins.id |
| `key` | TEXT | Storage key |
| `value` | BLOB | Stored value |
```

### Step 3: Update api-reference.md

**File:** `docs/docs/developers/api-reference.md`

Add Tag Operations section before the Events section.

Add:
```markdown
## Tag Operations

### CreateTag

Create a new tag.

\`\`\`go
func (a *App) CreateTag(name string) (*Tag, error)
\`\`\`

**Tag structure:**
\`\`\`go
type Tag struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Color string `json:"color"`
}
\`\`\`

---

### DeleteTag

Delete a tag by ID.

\`\`\`go
func (a *App) DeleteTag(id int64) error
\`\`\`

---

### GetTags

Get all tags with usage counts.

\`\`\`go
func (a *App) GetTags() ([]TagWithCount, error)
\`\`\`

**TagWithCount structure:**
\`\`\`go
type TagWithCount struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Color string `json:"color"`
    Count int    `json:"count"`
}
\`\`\`

---

### UpdateTag

Update a tag's name or color.

\`\`\`go
func (a *App) UpdateTag(id int64, name string, color string) error
\`\`\`

---

### AddTagToClip

Assign a tag to a clip.

\`\`\`go
func (a *App) AddTagToClip(clipID int64, tagID int64) error
\`\`\`

---

### RemoveTagFromClip

Remove a tag from a clip.

\`\`\`go
func (a *App) RemoveTagFromClip(clipID int64, tagID int64) error
\`\`\`

---

### GetClipTags

Get all tags assigned to a clip.

\`\`\`go
func (a *App) GetClipTags(clipID int64) ([]Tag, error)
\`\`\`

---

### BulkAddTag

Add a tag to multiple clips.

\`\`\`go
func (a *App) BulkAddTag(clipIDs []int64, tagID int64) error
\`\`\`

---

### BulkRemoveTag

Remove a tag from multiple clips.

\`\`\`go
func (a *App) BulkRemoveTag(clipIDs []int64, tagID int64) error
\`\`\`

---
```

### Step 4: Update sidebars.js

**File:** `docs/sidebars.js`

Replace entire content with:

```javascript
/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        'getting-started/installation',
        'getting-started/quick-start',
        'getting-started/keyboard-shortcuts',
      ],
    },
    {
      type: 'category',
      label: 'Features',
      collapsed: false,
      items: [
        'features/clipboard-management',
        'features/image-editor',
        'features/image-comparison',
        'features/text-editor',
        'features/tags',
        'features/auto-delete',
        'features/archive',
        'features/watch-folders',
        'features/bulk-actions',
      ],
    },
    {
      type: 'category',
      label: 'Plugins',
      collapsed: false,
      items: [
        'plugins/overview',
        'plugins/installing-plugins',
        {
          type: 'category',
          label: 'Writing Plugins',
          collapsed: true,
          items: [
            'plugins/writing-plugins/getting-started',
            'plugins/writing-plugins/plugin-manifest',
            'plugins/writing-plugins/event-handling',
            'plugins/writing-plugins/settings-storage',
          ],
        },
        'plugins/api-reference',
        'plugins/example-plugins',
      ],
    },
    {
      type: 'category',
      label: 'Tutorials',
      collapsed: true,
      items: [
        'tutorials/screenshot-workflow',
        'tutorials/code-snippets',
        'tutorials/automated-imports',
      ],
    },
    {
      type: 'category',
      label: 'Developers',
      collapsed: true,
      items: [
        'developers/architecture',
        'developers/frontend',
        'developers/backend',
        'developers/database-schema',
        'developers/api-reference',
        'developers/contributing',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: true,
      items: [
        'reference/data-storage',
        'reference/troubleshooting',
      ],
    },
  ],
};

export default sidebars;
```

### Step 5: Commit all updates

```bash
git add docs/docs/intro.md docs/docs/developers/database-schema.md docs/docs/developers/api-reference.md docs/sidebars.js
git commit -m "docs: update existing files with tags and plugins references"
```

---

## Summary

| Task | Files | Description |
|------|-------|-------------|
| 1 | `features/tags.md` | Tags feature page |
| 2 | `plugins/overview.md` | Plugins overview |
| 3 | `plugins/installing-plugins.md` | Plugin installation guide |
| 4 | `plugins/writing-plugins/getting-started.md` | First plugin tutorial |
| 5 | `plugins/writing-plugins/plugin-manifest.md` | Manifest reference |
| 6 | `plugins/writing-plugins/event-handling.md` | Event handling guide |
| 7 | `plugins/writing-plugins/settings-storage.md` | Settings & storage guide |
| 8 | `plugins/api-reference.md` | Complete API reference |
| 9 | `plugins/example-plugins.md` | 3 working examples |
| 10 | Multiple existing files | Updates to intro, schema, api-ref, sidebars |
