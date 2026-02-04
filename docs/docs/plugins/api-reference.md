---
sidebar_position: 4
---

# Plugin API Reference

Complete reference for all APIs available to mahpastes plugins.

## clips

Manage clipboard entries.

### clips.list(filter?)

Returns an array of clips without the `data` field (for performance).

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| filter | table | No | Optional filter criteria |
| filter.content_type | string | No | Filter by MIME type (e.g., "image/png") |
| filter.limit | number | No | Max results (default: 100, max: 1000) |
| filter.offset | number | No | Skip first N results (default: 0) |

**Returns:** Array of clip objects or `nil, error_message`

**Clip object (list):**
```lua
{
  id = 123,
  content_type = "image/png",
  filename = "screenshot.png",
  created_at = 1704067200,  -- Unix timestamp
  is_archived = false
}
```

**Example:**
```lua
-- Get all clips
local clips = clips.list()

-- Get only images
local images = clips.list({ content_type = "image/png" })

-- Paginate results
local page2 = clips.list({ limit = 10, offset = 10 })
```

---

### clips.get(id)

Returns a single clip with its data.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Clip ID |

**Returns:** Clip object with data, or `nil` if not found, or `nil, error_message`

**Clip object (get):**
```lua
{
  id = 123,
  content_type = "text/plain",
  filename = "note.txt",
  created_at = 1704067200,
  is_archived = false,
  data = "Hello, world!",        -- Text content as string
  data_encoding = nil            -- nil for text content
}

-- For binary content (images, etc.):
{
  id = 124,
  content_type = "image/png",
  filename = "screenshot.png",
  created_at = 1704067200,
  is_archived = false,
  data = "iVBORw0KGgo...",       -- Base64-encoded binary
  data_encoding = "base64"       -- Indicates encoding
}
```

**Example:**
```lua
local clip = clips.get(123)
if clip then
  if clip.data_encoding == "base64" then
    local binary = base64.decode(clip.data)
    -- Process binary data
  else
    -- Text content, use directly
    log("Content: " .. clip.data)
  end
end
```

---

### clips.create(options)

Creates a new clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| options | table | Yes | Clip creation options |
| options.data | string | Yes | Content (text or base64 for binary) |
| options.content_type | string | No | MIME type (default: "application/octet-stream") |
| options.filename | string | No | Optional filename |
| options.data_encoding | string | No | Set to "base64" for binary data |

**Returns:** New clip ID (number), or `nil, error_message`

**Size limit:** 10MB maximum for clip data.

**Example:**
```lua
-- Create text clip
local id = clips.create({
  data = "Hello, world!",
  content_type = "text/plain",
  filename = "greeting.txt"
})

-- Create image clip from base64
local id = clips.create({
  data = base64_image_data,
  data_encoding = "base64",
  content_type = "image/png",
  filename = "generated.png"
})
```

---

### clips.update(id, options)

Updates a clip's properties.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Clip ID |
| options | table | Yes | Fields to update |
| options.is_archived | boolean | No | Archive status |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
-- Archive a clip
clips.update(123, { is_archived = true })
```

---

### clips.delete(id)

Permanently deletes a clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Clip ID |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
clips.delete(123)
```

---

### clips.delete_many(ids)

Deletes multiple clips at once.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| ids | table | Yes | Array of clip IDs |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
clips.delete_many({123, 124, 125})
```

---

### clips.archive(id)

Archives a clip (shorthand for update with is_archived = true).

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Clip ID |

**Returns:** `true` on success, or `false, error_message`

---

### clips.unarchive(id)

Unarchives a clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Clip ID |

**Returns:** `true` on success, or `false, error_message`

---

## tags

Manage tags and clip-tag associations.

### tags.list()

Returns all tags with usage counts.

**Returns:** Array of tag objects, or `nil, error_message`

**Tag object:**
```lua
{
  id = 1,
  name = "screenshot",
  color = "#3B82F6",
  count = 15  -- Number of clips with this tag
}
```

**Example:**
```lua
local all_tags = tags.list()
for _, tag in ipairs(all_tags) do
  log(tag.name .. ": " .. tag.count .. " clips")
end
```

---

### tags.get(id)

Returns a single tag by ID.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Tag ID |

**Returns:** Tag object, or `nil` if not found, or `nil, error_message`

---

### tags.create(name)

Creates a new tag. Color is automatically assigned from a rotating palette.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| name | string | Yes | Tag name (max 50 characters) |

**Returns:** New tag object, or `nil, error_message`

**Example:**
```lua
local tag, err = tags.create("important")
if tag then
  log("Created tag with ID: " .. tag.id)
else
  log("Error: " .. err)
end
```

---

### tags.update(id, options)

Updates a tag's name or color.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Tag ID |
| options | table | Yes | Fields to update |
| options.name | string | No | New tag name |
| options.color | string | No | New color (hex format, e.g., "#FF0000") |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
tags.update(1, { name = "urgent", color = "#EF4444" })
```

---

### tags.delete(id)

Deletes a tag. Removes the tag from all clips.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | number | Yes | Tag ID |

**Returns:** `true` on success, or `false, error_message`

---

### tags.add_to_clip(tag_id, clip_id)

Adds a tag to a clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| tag_id | number | Yes | Tag ID |
| clip_id | number | Yes | Clip ID |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
-- Auto-tag a clip based on content type
function on_clip_created(data)
  local clip = clips.get(data.id)
  if clip.content_type:find("image/") then
    local tag = find_or_create_tag("screenshot")
    tags.add_to_clip(tag.id, clip.id)
  end
end
```

---

### tags.remove_from_clip(tag_id, clip_id)

Removes a tag from a clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| tag_id | number | Yes | Tag ID |
| clip_id | number | Yes | Clip ID |

**Returns:** `true` on success, or `false, error_message`

---

### tags.get_for_clip(clip_id)

Returns all tags for a specific clip.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| clip_id | number | Yes | Clip ID |

**Returns:** Array of tag objects, or `nil, error_message`

**Example:**
```lua
local clip_tags = tags.get_for_clip(123)
for _, tag in ipairs(clip_tags) do
  log("Tag: " .. tag.name)
end
```

---

## storage

Plugin-scoped key-value storage. Each plugin has isolated storage that persists across restarts.

### storage.get(key)

Retrieves a stored value.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| key | string | Yes | Storage key |

**Returns:** Stored value as string, or `nil` if not found

**Example:**
```lua
local last_sync = storage.get("last_sync_time")
if last_sync then
  log("Last synced: " .. last_sync)
end
```

---

### storage.set(key, value)

Stores a value. Overwrites existing value if key exists.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| key | string | Yes | Storage key |
| value | string | Yes | Value to store |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
-- Store a simple value
storage.set("counter", "42")

-- Store complex data as JSON
local config = { enabled = true, threshold = 100 }
storage.set("config", json.encode(config))
```

---

### storage.delete(key)

Deletes a stored value.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| key | string | Yes | Storage key |

**Returns:** `true` on success, `false` on error

---

### storage.list()

Lists all storage keys for this plugin.

**Returns:** Array of key strings, or `nil, error_message`

**Example:**
```lua
local keys = storage.list()
for _, key in ipairs(keys) do
  log("Key: " .. key)
end
```

---

## http

Make HTTP requests to allowed domains. Plugins must declare domains in their manifest.

### Domain Restrictions

HTTP requests are only allowed to domains listed in the plugin manifest:

```lua
--[[
manifest = {
  permissions = {
    http = {
      ["api.example.com"] = {"GET", "POST"},
      ["webhook.site"] = {"POST"}
    }
  }
}
]]
```

- Each domain must be explicitly declared
- Allowed HTTP methods must be specified per domain
- Redirects to unauthorized domains are blocked
- HTTPS is enforced (HTTP requests are rejected for redirects)

### http.get(url, options?)

Performs a GET request.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | Yes | Full URL (must be to allowed domain) |
| options | table | No | Request options |
| options.headers | table | No | Request headers as key-value pairs |

**Returns:** Response object, or `nil, error_message`

**Response object:**
```lua
{
  status = 200,              -- HTTP status code
  headers = {                -- Response headers
    ["Content-Type"] = "application/json",
    ["X-Custom"] = "value"
  },
  body = "..."               -- Response body as string
}
```

**Example:**
```lua
local resp, err = http.get("https://api.example.com/data", {
  headers = {
    ["Authorization"] = "Bearer " .. api_key
  }
})

if resp then
  if resp.status == 200 then
    local data = json.decode(resp.body)
    -- Process data
  else
    log("Error: HTTP " .. resp.status)
  end
else
  log("Request failed: " .. err)
end
```

---

### http.post(url, options?)

Performs a POST request.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | Yes | Full URL |
| options | table | No | Request options |
| options.headers | table | No | Request headers |
| options.body | string | No | Request body |

**Returns:** Response object, or `nil, error_message`

**Example:**
```lua
local resp = http.post("https://api.example.com/upload", {
  headers = {
    ["Content-Type"] = "application/json"
  },
  body = json.encode({ message = "Hello" })
})
```

---

### http.put(url, options?)

Performs a PUT request. Same signature as `http.post`.

---

### http.patch(url, options?)

Performs a PATCH request. Same signature as `http.post`.

---

### http.delete(url, options?)

Performs a DELETE request.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| url | string | Yes | Full URL |
| options | table | No | Request options |
| options.headers | table | No | Request headers |

**Returns:** Response object, or `nil, error_message`

---

## fs

Filesystem access with user permission prompts.

### Permission Model

- Plugins must declare filesystem intent in their manifest
- First access to a path triggers a user approval dialog
- User can approve a directory (covers all files within)
- Approvals are persisted and remembered
- `fs.exists` only works within already-approved directories (no prompt, returns false for unapproved paths)

**Manifest declaration:**
```lua
--[[
manifest = {
  permissions = {
    filesystem = {
      read = true,   -- Request read access
      write = true   -- Request write access
    }
  }
}
]]
```

### fs.read(path)

Reads a file's contents. Triggers permission prompt on first access to a directory.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| path | string | Yes | Absolute file path |

**Returns:** File contents as string, or `nil, error_message`

**Size limit:** 50MB maximum file size.

**Example:**
```lua
local content, err = fs.read("/Users/me/Documents/notes.txt")
if content then
  log("File contents: " .. content)
else
  log("Error: " .. err)
end
```

---

### fs.write(path, content)

Writes content to a file. Creates parent directories if needed.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| path | string | Yes | Absolute file path |
| content | string | Yes | Content to write |

**Returns:** `true` on success, or `false, error_message`

**Example:**
```lua
local success = fs.write("/Users/me/Documents/backup.txt", "Backup content")
if success then
  log("File written successfully")
end
```

---

### fs.list(path)

Lists directory contents.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| path | string | Yes | Absolute directory path |

**Returns:** Array of file info objects, or `nil, error_message`

**File info object:**
```lua
{
  name = "document.txt",
  is_dir = false,
  size = 1024,              -- Size in bytes
  modified = 1704067200     -- Unix timestamp
}
```

**Example:**
```lua
local files = fs.list("/Users/me/Documents")
if files then
  for _, file in ipairs(files) do
    if file.is_dir then
      log("[DIR] " .. file.name)
    else
      log(file.name .. " (" .. file.size .. " bytes)")
    end
  end
end
```

---

### fs.exists(path)

Checks if a path exists. Only works within already-approved directories (does not trigger a permission prompt).

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| path | string | Yes | Absolute path |

**Returns:** `true` if exists within approved path, `false` otherwise

**Note:** Returns `false` for paths outside approved directories to avoid leaking filesystem information.

---

## toast

Display toast notifications to the user.

### toast.show(message, type?)

Shows a toast notification.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| message | string | Yes | Notification message (max 200 characters) |
| type | string | No | Toast type: "info" (default), "success", or "error" |

**Returns:** `true` if shown, `false` if rate-limited

**Rate limit:** 5 toasts per minute per plugin.

**Example:**
```lua
toast.show("Sync complete!", "success")
toast.show("Warning: File not found", "error")
toast.show("Processing...")  -- defaults to "info"
```

---

## Utility Functions

### log(message)

Logs a message to the application log. Useful for debugging.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| message | string | Yes | Message to log |

**Returns:** Nothing

**Example:**
```lua
log("Plugin initialized")
log("Processing clip ID: " .. clip.id)
```

Logs appear as: `[plugin:your-plugin-name] Your message`

---

### json.encode(table)

Encodes a Lua table to JSON string.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| table | table | Yes | Lua table to encode |

**Returns:** JSON string, or `nil, error_message`

**Example:**
```lua
local data = { name = "test", count = 42, enabled = true }
local json_str = json.encode(data)
-- Result: '{"name":"test","count":42,"enabled":true}'
```

**Note:** Arrays (tables with consecutive integer keys starting at 1) are encoded as JSON arrays. Other tables are encoded as JSON objects.

---

### json.decode(string)

Decodes a JSON string to a Lua table.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| string | string | Yes | JSON string to decode |

**Returns:** Lua table, or `nil, error_message`

**Example:**
```lua
local data = json.decode('{"name":"test","count":42}')
log(data.name)  -- "test"
log(data.count) -- 42
```

---

### base64.encode(data)

Encodes a string to base64.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| data | string | Yes | Data to encode |

**Returns:** Base64-encoded string

**Example:**
```lua
local encoded = base64.encode("Hello, world!")
-- Result: "SGVsbG8sIHdvcmxkIQ=="
```

---

### base64.decode(string)

Decodes a base64 string.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| string | string | Yes | Base64-encoded string |

**Returns:** Decoded string, or `nil, error_message`

**Example:**
```lua
local decoded = base64.decode("SGVsbG8sIHdvcmxkIQ==")
-- Result: "Hello, world!"
```

---

### utils.time()

Returns the current Unix timestamp (seconds since epoch).

**Returns:** Number (Unix timestamp)

**Example:**
```lua
local now = utils.time()
log("Current timestamp: " .. now)

-- Store for later comparison
storage.set("last_run", tostring(now))
```

---

## Resource Limits

Plugins operate within strict resource limits to ensure system stability:

| Resource | Limit |
|----------|-------|
| Execution time | 30 seconds per handler |
| Memory | 50 MB per plugin |
| HTTP requests | 100 per minute |
| File operations | 50 per minute |
| Storage | 10 MB per plugin |
| Toast notifications | 5 per minute |
| Clip data size | 10 MB maximum |
| File read size | 50 MB maximum |
| HTTP response size | 10 MB maximum |

**Behavior when limits are exceeded:**

- **Execution time:** Handler is terminated with a timeout error
- **Rate limits:** Operation returns an error message
- **Size limits:** Operation is rejected with an error message
- **Toast rate limit:** Notification is silently dropped (returns false)

---

## Error Handling

Most API functions return errors as a second value:

```lua
local result, err = some_api_function()
if result == nil and err then
  log("Error: " .. err)
  return
end
```

**Common patterns:**

```lua
-- Check for nil result
local clip = clips.get(id)
if not clip then
  log("Clip not found")
  return
end

-- Check boolean success with error
local success, err = clips.delete(id)
if not success then
  log("Delete failed: " .. (err or "unknown error"))
end

-- Handle HTTP errors
local resp, err = http.get(url)
if not resp then
  log("Request failed: " .. err)
  return
end
if resp.status ~= 200 then
  log("HTTP error: " .. resp.status)
  return
end
```
