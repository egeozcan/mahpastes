---
sidebar_position: 3
---

# Event Handling

Events are the primary way plugins interact with mahpastes. When something happens in the app, plugins that subscribe to that event receive a notification with relevant data.

## How Events Work

1. Declare events you want in your manifest's `events` array
2. Implement handler functions with the naming convention `on_<event_name>`
3. Your handler receives event-specific data as its argument

```lua
Plugin = {
    name = "Event Demo",
    events = {"clip:created", "clip:deleted"},
}

function on_clip_created(clip)
    log("New clip: " .. clip.filename)
end

function on_clip_deleted(clip_id)
    log("Clip deleted: " .. tostring(clip_id))
end
```

## Handler Naming Convention

Event names map to handler function names by replacing `:` with `_` and prefixing with `on_`:

| Event | Handler Function |
|-------|------------------|
| `app:startup` | `on_startup()` |
| `clip:created` | `on_clip_created(clip)` |
| `watch:file_detected` | `on_watch_file_detected(data)` |
| `tag:added_to_clip` | `on_tag_added_to_clip(data)` |

## Event Reference

### App Lifecycle Events

#### app:startup

Fired when mahpastes starts and plugins are loaded.

**Payload:** None

```lua
function on_startup()
    log("Plugin initialized!")

    -- Good place for setup tasks
    local count = #clips.list()
    log("Current clip count: " .. count)
end
```

#### app:shutdown

Fired when mahpastes is closing.

**Payload:** None

```lua
function on_shutdown()
    log("Plugin shutting down, cleaning up...")

    -- Perform cleanup, save state, etc.
    storage.set("last_shutdown", utils.time())
end
```

### Clip Events

#### clip:created

Fired when a new clip is added to the library.

**Payload:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Unique clip identifier |
| `content_type` | string | MIME type (e.g., `"image/png"`, `"text/plain"`) |
| `filename` | string | Original filename |
| `created_at` | string | ISO 8601 timestamp |
| `is_archived` | boolean | Archive status (always `false` for new clips) |

```lua
function on_clip_created(clip)
    log("New clip created:")
    log("  ID: " .. clip.id)
    log("  File: " .. clip.filename)
    log("  Type: " .. clip.content_type)
    log("  Created: " .. clip.created_at)

    -- Example: Auto-tag images
    if clip.content_type:match("^image/") then
        tags.add_to_clip(clip.id, get_or_create_tag("images"))
    end
end
```

#### clip:deleted

Fired when a clip is permanently deleted.

**Payload:** `clip_id` (number)

:::note
Only the clip ID is provided because the clip data no longer exists at this point.
:::

```lua
function on_clip_deleted(clip_id)
    log("Clip deleted: " .. tostring(clip_id))

    -- Clean up any plugin data associated with this clip
    storage.delete("clip_metadata:" .. clip_id)
end
```

#### clip:archived

Fired when a clip is moved to the archive.

**Payload:** Same as `clip:created`

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Unique clip identifier |
| `content_type` | string | MIME type |
| `filename` | string | Original filename |
| `created_at` | string | ISO 8601 timestamp |
| `is_archived` | boolean | Archive status (always `true`) |

```lua
function on_clip_archived(clip)
    log("Clip archived: " .. clip.filename)
    storage.set("archived:" .. clip.id, utils.time())
end
```

#### clip:unarchived

Fired when a clip is restored from the archive.

**Payload:** Same as `clip:created`

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Unique clip identifier |
| `content_type` | string | MIME type |
| `filename` | string | Original filename |
| `created_at` | string | ISO 8601 timestamp |
| `is_archived` | boolean | Archive status (always `false`) |

```lua
function on_clip_unarchived(clip)
    log("Clip restored: " .. clip.filename)
    storage.delete("archived:" .. clip.id)
end
```

### Watch Folder Events

#### watch:file_detected

Fired when a new file is detected in a watch folder, before import.

**Payload:**

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Full path to the detected file |
| `folder_id` | number | ID of the watch folder |

```lua
function on_watch_file_detected(data)
    log("File detected: " .. data.path)
    log("From watch folder: " .. tostring(data.folder_id))
end
```

#### watch:import_complete

Fired after a file from a watch folder has been imported as a clip.

**Payload:**

| Field | Type | Description |
|-------|------|-------------|
| `clip_id` | number | ID of the newly created clip |
| `source_path` | string | Original file path |
| `folder_id` | number | ID of the watch folder |

```lua
function on_watch_import_complete(data)
    log("Imported clip " .. data.clip_id .. " from " .. data.source_path)

    -- Example: Tag clips from specific folders
    local folder_tags = storage.get("folder_tags:" .. data.folder_id)
    if folder_tags then
        for _, tag_id in ipairs(folder_tags) do
            tags.add_to_clip(data.clip_id, tag_id)
        end
    end
end
```

### Tag Events

#### tag:created

Fired when a new tag is created.

**Payload:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Unique tag identifier |
| `name` | string | Tag name |
| `color` | string | Hex color code (e.g., `"#ff5733"`) |

```lua
function on_tag_created(tag)
    log("Tag created: " .. tag.name .. " (" .. tag.color .. ")")
end
```

#### tag:updated

Fired when a tag's name or color is changed.

**Payload:** Same as `tag:created`

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Unique tag identifier |
| `name` | string | Updated tag name |
| `color` | string | Updated hex color code |

```lua
function on_tag_updated(tag)
    log("Tag updated: " .. tag.name)
end
```

#### tag:deleted

Fired when a tag is deleted.

**Payload:** `tag_id` (number)

```lua
function on_tag_deleted(tag_id)
    log("Tag deleted: " .. tostring(tag_id))
end
```

#### tag:added_to_clip

Fired when a tag is applied to a clip.

**Payload:**

| Field | Type | Description |
|-------|------|-------------|
| `tag_id` | number | ID of the tag |
| `clip_id` | number | ID of the clip |

```lua
function on_tag_added_to_clip(data)
    log("Tag " .. data.tag_id .. " added to clip " .. data.clip_id)
end
```

#### tag:removed_from_clip

Fired when a tag is removed from a clip.

**Payload:** Same as `tag:added_to_clip`

| Field | Type | Description |
|-------|------|-------------|
| `tag_id` | number | ID of the tag |
| `clip_id` | number | ID of the clip |

```lua
function on_tag_removed_from_clip(data)
    log("Tag " .. data.tag_id .. " removed from clip " .. data.clip_id)
end
```

## Handler Timeouts

Each event handler has a **30-second timeout**. If your handler takes longer, it will be terminated and an error will be logged.

```lua
-- BAD: This will timeout
function on_clip_created(clip)
    -- Don't do long-running operations synchronously
    for i = 1, 1000000 do
        http.get("https://slow-api.example.com/process")
    end
end

-- GOOD: Keep handlers fast
function on_clip_created(clip)
    -- Quick operations only
    storage.set("pending:" .. clip.id, "true")
    log("Queued clip for processing")
end
```

## Error Handling

Use `pcall` to safely handle errors without crashing your plugin:

```lua
function on_clip_created(clip)
    local success, err = pcall(function()
        -- Code that might fail
        local response = http.post("https://api.example.com/notify", {
            body = json.encode({clip_id = clip.id}),
        })

        if not response.ok then
            error("API returned " .. response.status)
        end
    end)

    if not success then
        log("Error processing clip: " .. tostring(err))
        -- Gracefully handle the failure
        storage.set("failed:" .. clip.id, tostring(err))
    end
end
```

### Plugin Error State

If a plugin's handlers fail **3 consecutive times**, the plugin enters an error state:

- The plugin is marked with an error indicator in the UI
- No further events are delivered to the plugin
- The user must manually re-enable the plugin

To avoid this:
- Always use `pcall` for operations that might fail
- Handle `nil` values defensively
- Log errors for debugging

```lua
function on_clip_created(clip)
    -- Defensive nil checking
    if not clip then
        log("Warning: received nil clip data")
        return
    end

    if not clip.filename then
        log("Warning: clip has no filename")
        return
    end

    -- Safe to proceed
    log("Processing: " .. clip.filename)
end
```

## Best Practices

### Keep Handlers Fast

Event handlers block the main thread. Keep them under 100ms when possible.

```lua
-- BAD: Blocking network call
function on_clip_created(clip)
    http.post("https://api.example.com/upload", {
        body = fs.read(clip.path),  -- Could be large
    })
end

-- GOOD: Queue for later processing
function on_clip_created(clip)
    -- Just record that processing is needed
    storage.set("pending:" .. clip.id, utils.time())
    log("Queued clip " .. clip.id)
end

-- Process in scheduled task
schedules = {{name = "process", interval = 60}}

function scheduled_process()
    local pending = storage.list("pending:")
    for _, key in ipairs(pending) do
        local clip_id = key:match("pending:(%d+)")
        process_clip(clip_id)
        storage.delete(key)
    end
end
```

### Handle Nil Values

Event data might be missing fields. Always check before accessing.

```lua
function on_clip_created(clip)
    -- Safe access pattern
    local filename = clip and clip.filename or "unknown"
    local content_type = clip and clip.content_type or ""

    log("File: " .. filename)

    if content_type:match("^image/") then
        -- Process image
    end
end
```

### Don't Block on Network

Network requests can fail or be slow. Handle failures gracefully.

```lua
function on_clip_created(clip)
    local success, result = pcall(function()
        return http.post("https://api.example.com/webhook", {
            body = json.encode({event = "clip_created", clip_id = clip.id}),
            timeout = 5000,  -- 5 second timeout
        })
    end)

    if not success then
        log("Network error: " .. tostring(result))
        -- Don't fail the handler, just log and continue
        return
    end

    if not result.ok then
        log("API error: " .. result.status)
    end
end
```

### Log Strategically

Use logging for debugging, but don't spam the log.

```lua
function on_clip_created(clip)
    -- Good: Informative but concise
    log("Processing clip " .. clip.id .. " (" .. clip.content_type .. ")")

    -- Bad: Too verbose for production
    -- log("Entering on_clip_created handler")
    -- log("Clip ID is: " .. clip.id)
    -- log("Clip filename is: " .. clip.filename)
    -- log("Clip content type is: " .. clip.content_type)
    -- log("Exiting on_clip_created handler")
end
```

## Complete Example

Here's a plugin that demonstrates multiple event handlers:

```lua
Plugin = {
    name = "Activity Logger",
    version = "1.0.0",
    description = "Logs all clip and tag activity",

    events = {
        "app:startup",
        "app:shutdown",
        "clip:created",
        "clip:deleted",
        "clip:archived",
        "tag:added_to_clip",
    },
}

local session_start

function on_startup()
    session_start = utils.time()
    storage.set("stats:sessions", (storage.get("stats:sessions") or 0) + 1)
    log("Activity Logger started (session " .. storage.get("stats:sessions") .. ")")
end

function on_shutdown()
    local duration = utils.time() - session_start
    log("Session lasted " .. duration .. " seconds")
end

function on_clip_created(clip)
    if not clip then return end

    local count = (storage.get("stats:clips_created") or 0) + 1
    storage.set("stats:clips_created", count)
    log("Clip #" .. count .. " created: " .. (clip.filename or "unknown"))
end

function on_clip_deleted(clip_id)
    local count = (storage.get("stats:clips_deleted") or 0) + 1
    storage.set("stats:clips_deleted", count)
    log("Clip deleted (total: " .. count .. ")")
end

function on_clip_archived(clip)
    if not clip then return end
    log("Archived: " .. (clip.filename or "unknown"))
end

function on_tag_added_to_clip(data)
    if not data then return end

    local success, tag = pcall(function()
        return tags.get(data.tag_id)
    end)

    local tag_name = success and tag and tag.name or tostring(data.tag_id)
    log("Tag '" .. tag_name .. "' added to clip " .. data.clip_id)
end
```

## Next Steps

- [Settings & Storage](./settings-storage) — Persist data between sessions
- [API Reference](../api-reference) — All available functions
