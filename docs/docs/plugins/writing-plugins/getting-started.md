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
    description = "Shows a toast when the app starts",
    author = "Your Name",

    -- Subscribe to app startup event
    events = {"app:startup"},
}

-- Handler: runs when app starts
function on_startup()
    toast.show("Hello from my first plugin!", "success")
end
```

## Installing Your Plugin

1. Open the menu drawer
2. Click **Plugins**
3. Click **Import**
4. Select your `hello.lua` file
5. Click **Approve & Install**

## Verifying It Works

1. Restart mahpastes (or disable/enable the plugin)
2. You should see a toast: `Hello from my first plugin!`

## Adding Event Handlers

Let's extend the plugin to react when clips are created:

```lua
Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "Shows toasts for app and clip events",
    author = "Your Name",

    events = {"app:startup", "clip:created"},
}

function on_startup()
    toast.show("Hello from my first plugin!", "success")
end

function on_clip_created(clip)
    toast.show("New clip created: " .. clip.filename, "info")
end
```

## Handler Naming Convention

Event names map to handler functions:

| Event | Handler Function |
|-------|------------------|
| `app:startup` | `on_startup()` |
| `app:shutdown` | `on_shutdown()` |
| `clip:created` | `on_clip_created(clip)` |
| `clip:deleted` | `on_clip_deleted(clip_id)` |
| `tag:added_to_clip` | `on_tag_added_to_clip(data)` |

Pattern: Replace `:` with `_` and prefix with `on_`. For `app:` events, the `app:` prefix is stripped (e.g., `app:startup` becomes `on_startup`, not `on_app_startup`).

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

    local images = clips.list({content_type = "image/png"})
    log("PNG clips: " .. #images)
end
```

## Next Steps

- [Plugin Manifest](./plugin-manifest) — All configuration options
- [Event Handling](./event-handling) — Complete event reference
- [Settings & Storage](./settings-storage) — Persist data and user config
- [API Reference](../api-reference) — All available functions
