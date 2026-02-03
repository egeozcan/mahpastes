-- Event Tracker Test Plugin
-- Records all events to storage for e2e test verification

Plugin = {
    name = "Event Tracker",
    version = "1.0.0",
    description = "Test plugin that tracks events for verification",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {
        "app:startup",
        "app:shutdown",
        "clip:created",
        "clip:deleted",
        "clip:archived",
        "clip:unarchived",
        "tag:created",
        "tag:deleted",
        "tag:added_to_clip",
        "tag:removed_from_clip",
    },

    schedules = {},
}

-- Initialize storage at load time (not in on_startup, since that's only called
-- if the plugin is loaded before the app emits app:startup)
storage.set("event_log", "[]")
storage.set("loaded", "true")
storage.set("load_time", tostring(utils.time()))
log("Event Tracker plugin loaded and initialized")

-- Helper to append to event log
local function log_event(event_name, data)
    -- Get current log
    local current = storage.get("event_log") or "[]"
    local log_data = json.decode(current) or {}

    -- Add new event
    table.insert(log_data, {
        event = event_name,
        data = data,
        time = utils.time()
    })

    -- Save back
    storage.set("event_log", json.encode(log_data))

    -- Also increment counter for this event type
    local count_key = "count_" .. string.gsub(event_name, ":", "_")
    local count = tonumber(storage.get(count_key)) or 0
    storage.set(count_key, tostring(count + 1))

    log("Event tracked: " .. event_name)
end

function on_startup()
    -- This is only called if plugin is loaded before app:startup event
    storage.set("received_startup", "true")
    log("Event Tracker received app:startup event")
end

function on_shutdown()
    log_event("app:shutdown", nil)
end

function on_clip_created(data)
    log_event("clip:created", data)
end

function on_clip_deleted(data)
    log_event("clip:deleted", data)
end

function on_clip_archived(data)
    log_event("clip:archived", data)
end

function on_clip_unarchived(data)
    log_event("clip:unarchived", data)
end

function on_tag_created(data)
    log_event("tag:created", data)
end

function on_tag_deleted(data)
    log_event("tag:deleted", data)
end

function on_tag_added_to_clip(data)
    log_event("tag:added_to_clip", data)
end

function on_tag_removed_from_clip(data)
    log_event("tag:removed_from_clip", data)
end
