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
