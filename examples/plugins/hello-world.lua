-- Hello World Plugin for mahpastes
-- Demonstrates the plugin API

Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "A simple example plugin that logs clip events",
    author = "mahpastes",

    -- No network access needed
    network = {},

    -- No filesystem access needed
    filesystem = {
        read = false,
        write = false,
    },

    -- Subscribe to clip events
    events = {"app:startup", "app:shutdown", "clip:created", "clip:deleted"},

    -- No scheduled tasks
    schedules = {},
}

-- Called when the app starts
function on_startup()
    log("Hello World plugin started!")

    -- Count existing clips
    local all_clips = clips.list()
    log("Found " .. #all_clips .. " existing clips")
end

-- Called when the app is shutting down
function on_shutdown()
    log("Hello World plugin shutting down. Goodbye!")
end

-- Called when a new clip is created
function on_clip_created(clip)
    log("New clip created: " .. (clip.filename or "unnamed") .. " (" .. clip.content_type .. ")")

    -- Store count in plugin storage
    local count = storage.get("clip_count") or "0"
    count = tonumber(count) + 1
    storage.set("clip_count", tostring(count))
    log("Total clips created this session: " .. count)
end

-- Called when a clip is deleted
function on_clip_deleted(clip_id)
    log("Clip deleted: ID " .. clip_id)
end
