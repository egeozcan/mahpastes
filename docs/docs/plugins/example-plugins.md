---
sidebar_position: 5
---

# Example Plugins

Complete, working plugin examples you can use as starting points for your own plugins.

## Auto-Tagger

**Level:** Beginner

Automatically tags clips based on their filename or source. Screenshots are tagged with "screenshot" and watch folder imports are tagged with "imported".

```lua
--[[
manifest = {
    name = "Auto-Tagger",
    version = "1.0.0",
    description = "Automatically tag clips based on filename and source",
    author = "mahpastes",
    events = {"clip:created", "watch:import_complete"},
}
]]

Plugin = {
    name = "Auto-Tagger",
    version = "1.0.0",
    description = "Automatically tag clips based on filename and source",
    author = "mahpastes",
    events = {"clip:created", "watch:import_complete"},
}

-- Helper function: get or create a tag by name
local function ensure_tag(name)
    -- Check if tag already exists
    local all_tags = tags.list()
    if all_tags then
        for _, tag in ipairs(all_tags) do
            if tag.name == name then
                return tag
            end
        end
    end

    -- Create the tag if it doesn't exist
    local new_tag, err = tags.create(name)
    if new_tag then
        log("Created tag: " .. name)
        return new_tag
    else
        log("Failed to create tag: " .. (err or "unknown error"))
        return nil
    end
end

-- Handler: tag screenshots when clips are created
function on_clip_created(clip)
    if not clip or not clip.filename then
        return
    end

    -- Check if filename contains "screenshot" (case-insensitive)
    local filename_lower = clip.filename:lower()
    if filename_lower:find("screenshot") then
        local tag = ensure_tag("screenshot")
        if tag then
            local success, err = tags.add_to_clip(tag.id, clip.id)
            if success then
                log("Tagged clip " .. clip.id .. " as screenshot")
            else
                log("Failed to tag clip: " .. (err or "unknown error"))
            end
        end
    end
end

-- Handler: tag clips imported from watch folders
function on_watch_import_complete(data)
    if not data or not data.clip_id then
        return
    end

    local tag = ensure_tag("imported")
    if tag then
        local success, err = tags.add_to_clip(tag.id, data.clip_id)
        if success then
            log("Tagged clip " .. data.clip_id .. " as imported from watch folder")
        else
            log("Failed to tag clip: " .. (err or "unknown error"))
        end
    end
end
```

### How It Works

1. **`ensure_tag()` helper** - Checks if a tag exists by name. If not, creates it. This prevents duplicate tags.

2. **`on_clip_created`** - Fires when any clip is added. Checks if the filename contains "screenshot" and applies the tag.

3. **`on_watch_import_complete`** - Fires after a file from a watch folder is imported. Tags the new clip with "imported".

### Customization Ideas

- Add more filename patterns (e.g., "receipt", "invoice", "photo")
- Tag based on content type (`clip.content_type:match("^image/")`)
- Tag based on which watch folder the file came from

---

## Webhook Notifier

**Level:** Intermediate

Sends a notification to a webhook URL when clips are created. Works with Slack, Discord, or any webhook-compatible service.

```lua
--[[
manifest = {
    name = "Webhook Notifier",
    version = "1.0.0",
    description = "Send notifications to webhooks when clips are created",
    author = "mahpastes",
    events = {"clip:created"},
    network = {
        ["hooks.slack.com"] = {"POST"},
        ["discord.com"] = {"POST"},
        ["webhook.site"] = {"POST"},
    },
}
]]

Plugin = {
    name = "Webhook Notifier",
    version = "1.0.0",
    description = "Send notifications to webhooks when clips are created",
    author = "mahpastes",
    events = {"clip:created"},

    network = {
        ["hooks.slack.com"] = {"POST"},
        ["discord.com"] = {"POST"},
        ["webhook.site"] = {"POST"},
    },

    settings = {
        {
            key = "webhook_url",
            type = "text",
            label = "Webhook URL",
            description = "Full URL of your webhook endpoint (Slack, Discord, or webhook.site)",
        },
        {
            key = "include_preview",
            type = "checkbox",
            label = "Include Preview",
            description = "Include clip details in the notification",
            default = true,
        },
    },
}

-- Handler: send webhook when clip is created
function on_clip_created(clip)
    if not clip then
        log("Received nil clip data")
        return
    end

    -- Get settings
    local webhook_url = storage.get("setting:webhook_url")
    if not webhook_url or webhook_url == "" then
        log("Webhook URL not configured - skipping notification")
        return
    end

    local include_preview = storage.get("setting:include_preview") ~= "false"

    -- Build the payload
    local payload = {
        text = "New clip added to mahpastes",
    }

    if include_preview then
        payload.text = string.format(
            "New clip: %s (%s)",
            clip.filename or "unnamed",
            clip.content_type or "unknown type"
        )
    end

    -- Send the webhook with error handling
    local success, result = pcall(function()
        local response, err = http.post(webhook_url, {
            headers = {
                ["Content-Type"] = "application/json",
            },
            body = json.encode(payload),
        })

        if not response then
            error("Request failed: " .. (err or "unknown error"))
        end

        if response.status < 200 or response.status >= 300 then
            error("HTTP " .. response.status)
        end

        return response
    end)

    if success then
        log("Webhook notification sent for clip " .. clip.id)
    else
        log("Webhook failed: " .. tostring(result))
        -- Optionally show a toast to the user
        toast.show("Webhook notification failed", "error")
    end
end
```

### How It Works

1. **Settings** - User configures their webhook URL and notification preferences through the plugin settings UI.

2. **Permission declaration** - The `network` table whitelists domains the plugin can access. Requests to other domains are blocked.

3. **`pcall` error handling** - Wraps the HTTP request in `pcall` to catch network errors, timeouts, or bad responses without crashing the plugin.

4. **Toast notification** - Uses `toast.show()` to alert the user if something goes wrong.

### Configuration

1. Install the plugin
2. Open the Plugins modal and expand the plugin card
3. Enter your webhook URL:
   - **Slack:** `Get your webhook URL from Slack API`
   - **Discord:** `Get your webhook URL from Discord server settings`
   - **Testing:** Use `https://webhook.site` to get a test URL

### Adding More Webhook Services

To add support for additional services, add their domains to the `network` table:

```lua
network = {
    ["hooks.slack.com"] = {"POST"},
    ["discord.com"] = {"POST"},
    ["your-service.com"] = {"POST"},
},
```

---

## Periodic Cleanup

**Level:** Intermediate

Automatically deletes old, unarchived clips to keep your library manageable. Runs hourly with configurable age threshold and a safety dry-run mode.

```lua
--[[
manifest = {
    name = "Periodic Cleanup",
    version = "1.0.0",
    description = "Automatically delete old clips on a schedule",
    author = "mahpastes",
    events = {"app:startup"},
    schedules = {
        {name = "cleanup", interval = 3600},
    },
}
]]

Plugin = {
    name = "Periodic Cleanup",
    version = "1.0.0",
    description = "Automatically delete old clips on a schedule",
    author = "mahpastes",
    events = {"app:startup"},

    schedules = {
        {name = "cleanup", interval = 3600},  -- Run every hour (3600 seconds)
    },

    settings = {
        {
            key = "max_age_days",
            type = "select",
            label = "Delete clips older than",
            description = "Clips older than this will be deleted (archived clips are never deleted)",
            options = {"7", "14", "30", "60", "90"},
            default = "30",
        },
        {
            key = "dry_run",
            type = "checkbox",
            label = "Dry Run Mode",
            description = "When enabled, only logs what would be deleted without actually deleting",
            default = true,
        },
    },
}

-- Constants
local SECONDS_PER_DAY = 86400

-- Helper: get setting with default
local function get_setting(key, default)
    local value = storage.get("setting:" .. key)
    return value or default
end

-- Helper: check if dry run mode is enabled
local function is_dry_run()
    return get_setting("dry_run", "true") == "true"
end

-- Startup handler: log configuration
function on_startup()
    local max_age = get_setting("max_age_days", "30")
    local dry_run = is_dry_run()

    log("Periodic Cleanup initialized")
    log("  Max age: " .. max_age .. " days")
    log("  Dry run: " .. tostring(dry_run))

    if dry_run then
        log("  [!] Dry run mode is ON - no clips will be deleted")
        log("  [!] Disable dry run in settings when ready to delete")
    end
end

-- Scheduled task: function name matches the schedule's "name" field
function cleanup()
    local max_age_days = tonumber(get_setting("max_age_days", "30"))
    local dry_run = is_dry_run()
    local now = utils.time()
    local cutoff = now - (max_age_days * SECONDS_PER_DAY)

    log("Running cleanup (max age: " .. max_age_days .. " days, dry run: " .. tostring(dry_run) .. ")")

    -- Get all clips
    local all_clips, err = clips.list({limit = 1000})
    if not all_clips then
        log("Failed to list clips: " .. (err or "unknown error"))
        return
    end

    -- Find clips to delete
    local to_delete = {}
    local archived_skipped = 0
    local too_new = 0

    for _, clip in ipairs(all_clips) do
        -- SAFETY: Never delete archived clips
        if clip.is_archived then
            archived_skipped = archived_skipped + 1
        elseif clip.created_at < cutoff then
            table.insert(to_delete, clip.id)
        else
            too_new = too_new + 1
        end
    end

    log("Scan results:")
    log("  Total clips: " .. #all_clips)
    log("  Archived (protected): " .. archived_skipped)
    log("  Too new to delete: " .. too_new)
    log("  Eligible for deletion: " .. #to_delete)

    if #to_delete == 0 then
        log("No clips to delete")
        return
    end

    -- Perform deletion (or simulate in dry run mode)
    if dry_run then
        log("[DRY RUN] Would delete " .. #to_delete .. " clips")
        for i, id in ipairs(to_delete) do
            if i <= 10 then  -- Only log first 10
                log("[DRY RUN]   - Clip ID: " .. id)
            end
        end
        if #to_delete > 10 then
            log("[DRY RUN]   ... and " .. (#to_delete - 10) .. " more")
        end
        toast.show("Dry run: would delete " .. #to_delete .. " clips", "info")
    else
        local success, delete_err = clips.delete_many(to_delete)
        if success then
            log("Deleted " .. #to_delete .. " old clips")
            toast.show("Cleaned up " .. #to_delete .. " old clips", "success")

            -- Track statistics
            local total_deleted = tonumber(storage.get("stats:total_deleted") or "0")
            storage.set("stats:total_deleted", tostring(total_deleted + #to_delete))
            storage.set("stats:last_cleanup", tostring(now))
        else
            log("Failed to delete clips: " .. (delete_err or "unknown error"))
            toast.show("Cleanup failed", "error")
        end
    end
end
```

### How It Works

1. **Scheduled execution** - The `schedules` array defines a task named "cleanup" that runs every 3600 seconds (1 hour). The corresponding handler function is `cleanup()` (the function name must match the schedule's `name` field).

2. **Safety first** - Archived clips are never deleted. The `is_archived` check ensures users can protect important clips by archiving them.

3. **Dry run mode** - Enabled by default. Logs what would be deleted without actually deleting anything. Disable in settings when you're confident the configuration is correct.

4. **Batch deletion** - Uses `clips.delete_many()` for efficient deletion of multiple clips in one operation.

5. **Statistics tracking** - Stores the total number of deleted clips and last cleanup time in plugin storage.

### Configuration

1. Install the plugin
2. Open the Plugins modal and expand the plugin card
3. Configure:
   - **Delete clips older than:** Choose 7, 14, 30, 60, or 90 days
   - **Dry Run Mode:** Leave enabled initially to see what would be deleted
4. Watch the plugin logs to verify the configuration
5. When satisfied, disable dry run mode to enable actual deletion

### Safety Features

- **Archived clips are never deleted** - Archive important clips to protect them
- **Dry run by default** - Nothing is deleted until you explicitly disable dry run
- **Logging** - All actions are logged for transparency
- **Toast notifications** - User is notified of cleanup results

---

## Bundled Plugins

mahpastes ships with several ready-to-use plugins in the `plugins/` directory. These demonstrate a range of capabilities and can be installed directly.

### FAL.AI Image Processing

AI-powered image processing via the fal.ai API. Provides lightbox and card actions for colorizing, upscaling (2x/4x), restoring, AI editing, and vectorizing images, plus a global text-to-image action.

Text-to-image covers Nano Banana 2 and Pro, FLUX.2 (max/pro/turbo), Seedream 5.0 Pro and 4.5, GPT Image 2, Ideogram V4, Recraft V4, and Z-Image Turbo; AI Edit covers the editing endpoints of the same families. Aspect ratio and resolution are translated per model, since each states its accepted dimensions differently.

Results are not left on fal's servers. Where the endpoint supports `sync_mode`, the image comes back inline as a data URI and is never uploaded to fal's CDN at all. The rest are capped by the `X-Fal-Object-Lifecycle-Preference` header (5 minutes by default, configurable in plugin settings), and `X-Fal-Store-IO: 0` keeps prompts and input images out of fal's 30-day request history unless the user opts in.

**Content Filter** (Off / Relaxed / Moderate / Strict) appears on the Generate Image and AI Edit forms, alongside a plugin-wide default in settings that those dropdowns fall back to. Models grade content on incompatible scales — Gemini takes a 1-6 tolerance, FLUX [pro]/[max] a 1-5 tolerance, and the rest a plain on/off checker, all of them inverted relative to the label — so the plugin translates one level onto each. Upscale has no dropdown, so Clarity follows the setting; GPT Image 2 and the other upscalers expose no filter parameter and are unaffected.

Demonstrates async UI actions, bulk actions, task progress tracking, settings (password, select, checkbox), `image.info`, `clips.create`, and `clips.create_from_url`.

### mahresources Upload

Automatically uploads new clips to a mahresources server instance. Supports manual upload via card action and auto-upload on `clip:created` with content type filtering. Demonstrates event handling, settings, and multipart HTTP upload.

### Watermarker

Adds text watermark overlays on images. Demonstrates `image.overlay_text` with configurable position, opacity, and font size via UI action options.

### QR Code Generator

Generates QR codes from text clips using an external API. Demonstrates `clips.create_from_url`, `utils.url_encode`, and `file_types`/`max_size` action filters.

### Palette Extractor

Extracts dominant colors from images and displays them as an SVG swatch in a modal. Can optionally create tags for each color. Demonstrates `image.dominant_colors`, `modal.show` with markdown format, and batch processing of multiple clips.

### EXIF Viewer

Displays EXIF metadata from images in a modal and auto-tags photos by camera model on `clip:created`. Demonstrates `image.info`, `image.metadata`, and modal with markdown tables.

### ASCII Art Converter

Converts images to ASCII art using `image.grayscale_pixels`. Displays results in a text-format modal with configurable output width.

### Expiring Clips

Adds time-based expiry to clips. Users can set expiry durations (1h, 1d, 1w, 30d) via card actions. A scheduled task (`check_expiry`, every 5 minutes) archives expired clips. Demonstrates schedules, storage for state persistence, `utils.time`, and multiple card actions.

### Auto-Tagger

Automatically tags clips based on content type and filename patterns. Demonstrates basic event handling with `clip:created` and the tags API.

---

## Tips for Writing Plugins

### Start with Logging

Add `log()` calls throughout your plugin during development. It's the easiest way to understand what's happening.

```lua
function on_clip_created(clip)
    log("on_clip_created called")
    log("clip: " .. json.encode(clip))

    -- Your logic here

    log("on_clip_created finished")
end
```

### Test with Dry Run

For any plugin that modifies or deletes data, add a dry run setting:

```lua
settings = {
    {
        key = "dry_run",
        type = "checkbox",
        label = "Dry Run Mode",
        default = true,
    },
}

function my_task()
    local dry_run = storage.get("setting:dry_run") == "true"

    if dry_run then
        log("[DRY RUN] Would perform action...")
    else
        -- Actually do it
    end
end
```

### Handle Nil Values

Event data may be missing fields. Always check before accessing:

```lua
function on_clip_created(clip)
    -- Check the clip exists
    if not clip then
        log("Warning: received nil clip")
        return
    end

    -- Check individual fields
    local filename = clip.filename or "unknown"
    local content_type = clip.content_type or ""

    -- Now safe to use
    log("Processing: " .. filename)
end
```

### Use pcall for External Calls

Wrap HTTP requests and other operations that might fail:

```lua
function send_notification(message)
    local success, result = pcall(function()
        local response, err = http.post(webhook_url, {
            body = json.encode({text = message}),
        })

        if not response then
            error("Request failed: " .. (err or "unknown"))
        end

        if response.status >= 400 then
            error("HTTP " .. response.status)
        end

        return response
    end)

    if not success then
        log("Notification failed: " .. tostring(result))
        return false
    end

    return true
end
```

This prevents network errors from crashing your plugin and putting it into an error state.
