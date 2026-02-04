---
sidebar_position: 4
---

# Settings & Storage

Plugins can persist data between sessions and expose user-configurable settings. This guide covers the storage API and best practices for managing plugin state.

## Plugin Storage

Every plugin has access to isolated key-value storage. Data is stored in SQLite and persists across app restarts. Each plugin's storage is completely separate from other plugins.

### storage.set(key, value)

Store a value under a key. Both key and value must be strings.

```lua
storage.set("last_run", "2024-01-15T10:30:00Z")
storage.set("clip_count", "42")  -- Numbers must be converted to strings
```

**Returns:** `true` on success, or `false, error_message` on failure.

### storage.get(key)

Retrieve a value by key.

```lua
local last_run = storage.get("last_run")
if last_run then
    log("Last run: " .. last_run)
else
    log("Never run before")
end
```

**Returns:** The stored string value, or `nil` if the key doesn't exist.

### storage.delete(key)

Remove a key from storage.

```lua
storage.delete("temporary_data")
```

**Returns:** `true` on success, `false` on failure.

### storage.list()

Get all keys in storage. Useful for iterating over stored data.

```lua
local keys = storage.list()
for _, key in ipairs(keys) do
    log("Key: " .. key .. " = " .. (storage.get(key) or "nil"))
end
```

**Returns:** An array of key strings, sorted alphabetically.

### Storing Complex Data

Since storage only accepts strings, use `json.encode()` and `json.decode()` for complex data:

```lua
-- Storing a table
local config = {
    enabled = true,
    threshold = 100,
    tags = {"important", "work"},
}
storage.set("config", json.encode(config))

-- Retrieving and parsing
local raw = storage.get("config")
if raw then
    local config = json.decode(raw)
    log("Threshold: " .. config.threshold)
end
```

### Storage Limits

- **Per-plugin limit:** 10MB total storage per plugin
- **Key length:** Maximum 256 characters
- **Value length:** Maximum 1MB per value

If you exceed these limits, `storage.set()` will return `false` with an error message.

## User-Configurable Settings

Settings allow users to configure your plugin through the UI. They appear in the plugin's settings panel when the user clicks the gear icon.

### Declaring Settings in Manifest

Define settings in your `Plugin` table:

```lua
Plugin = {
    name = "My Plugin",
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
        {
            key = "quality",
            type = "select",
            label = "Export Quality",
            options = {"low", "medium", "high"},
            default = "medium",
        },
    },
}
```

### Setting Types

| Type | Description | Value Type | Example |
|------|-------------|------------|---------|
| `text` | Single-line text input | String | Username, folder path |
| `password` | Obscured text input | String | API keys, tokens |
| `checkbox` | Boolean toggle | `"true"` or `"false"` | Enable/disable features |
| `select` | Dropdown menu | String (selected option) | Quality level, mode |

### Setting Fields

| Field | Required | Description |
|-------|----------|-------------|
| `key` | Yes | Unique identifier (used with `storage.get`) |
| `type` | Yes | Input type: `text`, `password`, `checkbox`, `select` |
| `label` | Yes | Display label shown to users |
| `description` | No | Help text displayed below the input |
| `default` | No | Default value if not configured |
| `options` | For `select` | Array of choices |

### Reading Settings

Settings are stored with a `setting:` prefix. Read them with `storage.get()`:

```lua
function on_startup()
    -- Read a text/password setting
    local api_key = storage.get("setting:api_key")
    if not api_key or api_key == "" then
        log("Warning: API key not configured")
        return
    end

    -- Read a checkbox (returns "true" or "false" as string)
    local auto_sync = storage.get("setting:auto_sync")
    if auto_sync == "true" then
        log("Auto-sync is enabled")
    end

    -- Read a select with default fallback
    local quality = storage.get("setting:quality") or "medium"
    log("Quality: " .. quality)
end
```

:::tip
Checkbox values are stored as strings `"true"` or `"false"`, not Lua booleans. Compare with `== "true"` or create a helper function.
:::

### Default Values

If a setting has a `default` value and the user hasn't changed it, `storage.get()` returns `nil`. Handle defaults in your code:

```lua
-- Define defaults in one place
local DEFAULTS = {
    quality = "medium",
    max_retries = "3",
    auto_sync = "true",
}

function get_setting(key)
    local value = storage.get("setting:" .. key)
    return value or DEFAULTS[key]
end

-- Usage
local quality = get_setting("quality")  -- "medium" if not set
```

## Common Patterns

### Initialization State Tracking

Track whether your plugin has been initialized:

```lua
function on_startup()
    local initialized = storage.get("initialized")
    if not initialized then
        -- First run setup
        log("First run - initializing...")
        storage.set("initialized", "true")
        storage.set("version", Plugin.version)
    else
        -- Check for version upgrade
        local stored_version = storage.get("version")
        if stored_version ~= Plugin.version then
            log("Upgrading from " .. (stored_version or "unknown") .. " to " .. Plugin.version)
            migrate_data(stored_version)
            storage.set("version", Plugin.version)
        end
    end
end

function migrate_data(from_version)
    -- Handle data migration between versions
end
```

### Pending Queue for Retries

Queue failed operations for later retry:

```lua
function on_clip_created(clip)
    local success = upload_clip(clip)
    if not success then
        -- Add to pending queue
        local pending = json.decode(storage.get("pending") or "[]")
        table.insert(pending, {
            clip_id = clip.id,
            timestamp = utils.time(),
            attempts = 1,
        })
        storage.set("pending", json.encode(pending))
        log("Queued clip " .. clip.id .. " for retry")
    end
end

-- Scheduled task to retry pending items
Plugin.schedules = {{name = "retry", interval = 300}}

function scheduled_retry()
    local pending = json.decode(storage.get("pending") or "[]")
    local still_pending = {}

    for _, item in ipairs(pending) do
        local clip = clips.get(item.clip_id)
        if clip then
            local success = upload_clip(clip)
            if not success and item.attempts < 5 then
                item.attempts = item.attempts + 1
                table.insert(still_pending, item)
            elseif not success then
                log("Giving up on clip " .. item.clip_id .. " after 5 attempts")
            end
        end
    end

    storage.set("pending", json.encode(still_pending))
end
```

### Tracking Last Run Time

Record when operations were last performed:

```lua
function scheduled_cleanup()
    local last_cleanup = storage.get("last_cleanup")
    local now = utils.time()

    if last_cleanup then
        local elapsed = now - tonumber(last_cleanup)
        log("Last cleanup was " .. elapsed .. " seconds ago")
    end

    -- Perform cleanup
    perform_cleanup()

    -- Update timestamp
    storage.set("last_cleanup", tostring(now))
end
```

### Caching API Responses

Cache expensive API calls with expiration:

```lua
local CACHE_TTL = 3600  -- 1 hour in seconds

function get_remote_config()
    -- Check cache first
    local cached = storage.get("cache:remote_config")
    local cached_at = storage.get("cache:remote_config:time")

    if cached and cached_at then
        local age = utils.time() - tonumber(cached_at)
        if age < CACHE_TTL then
            log("Using cached config (age: " .. age .. "s)")
            return json.decode(cached)
        end
    end

    -- Fetch fresh data
    log("Fetching fresh config from API...")
    local response = http.get("https://api.example.com/config")

    if response.ok then
        storage.set("cache:remote_config", response.body)
        storage.set("cache:remote_config:time", tostring(utils.time()))
        return json.decode(response.body)
    end

    -- Return stale cache if fetch fails
    if cached then
        log("API failed, using stale cache")
        return json.decode(cached)
    end

    return nil
end
```

### Counting and Statistics

Track usage statistics:

```lua
function on_clip_created(clip)
    -- Increment counter
    local count = tonumber(storage.get("stats:clips_processed") or "0")
    storage.set("stats:clips_processed", tostring(count + 1))

    -- Track by content type
    local type_key = "stats:type:" .. clip.content_type
    local type_count = tonumber(storage.get(type_key) or "0")
    storage.set(type_key, tostring(type_count + 1))
end

function on_startup()
    local total = storage.get("stats:clips_processed") or "0"
    log("Total clips processed: " .. total)
end
```

## Debugging Storage

### Viewing Storage Contents

Log all stored keys and values for debugging:

```lua
function debug_storage()
    log("=== Storage Debug ===")
    local keys = storage.list()

    if #keys == 0 then
        log("(empty)")
        return
    end

    for _, key in ipairs(keys) do
        local value = storage.get(key)
        -- Truncate long values
        if #value > 50 then
            value = value:sub(1, 50) .. "..."
        end
        log("  " .. key .. " = " .. value)
    end
    log("=== End Storage ===")
end
```

### Debug Logging Helper

Create a debug mode toggle for verbose logging:

```lua
local DEBUG = false  -- Set to true during development

function debug_log(message)
    if DEBUG then
        log("[DEBUG] " .. message)
    end
end

function on_clip_created(clip)
    debug_log("Received clip: " .. json.encode(clip))

    local api_key = storage.get("setting:api_key")
    debug_log("API key configured: " .. tostring(api_key ~= nil))

    -- Production logic here
end
```

### Using Settings for Debug Mode

Let users enable debug mode through settings:

```lua
Plugin = {
    name = "My Plugin",
    settings = {
        {
            key = "debug_mode",
            type = "checkbox",
            label = "Enable Debug Logging",
            description = "Log detailed information for troubleshooting",
            default = false,
        },
    },
}

function is_debug()
    return storage.get("setting:debug_mode") == "true"
end

function debug_log(message)
    if is_debug() then
        log("[DEBUG] " .. message)
    end
end
```

## Best Practices

### Use Key Prefixes

Organize storage with consistent key prefixes:

```lua
-- Settings (managed by UI)
storage.get("setting:api_key")

-- Plugin state
storage.set("state:initialized", "true")
storage.set("state:version", "1.0.0")

-- Cached data
storage.set("cache:remote_config", data)
storage.set("cache:remote_config:time", timestamp)

-- Pending work
storage.set("pending:clip:123", json.encode(item))

-- Statistics
storage.set("stats:total_processed", count)
```

### Clean Up Unused Data

Remove obsolete keys when they're no longer needed:

```lua
function on_clip_deleted(clip_id)
    -- Clean up any data associated with this clip
    storage.delete("metadata:" .. clip_id)
    storage.delete("pending:clip:" .. clip_id)
end
```

### Handle Missing Data Gracefully

Always provide fallbacks for missing values:

```lua
-- Bad: crashes if key doesn't exist
local count = tonumber(storage.get("count"))  -- nil if missing

-- Good: provide default
local count = tonumber(storage.get("count") or "0")

-- Better: helper function
function get_number(key, default)
    local value = storage.get(key)
    return value and tonumber(value) or default
end

local count = get_number("count", 0)
```

## Next Steps

- [API Reference](../api-reference) - Complete reference for all APIs
- [Example Plugins](../example-plugins) - See storage patterns in real plugins
