-- Error Test Plugin
-- Tests error handling in event handlers

Plugin = {
    name = "Error Test",
    version = "1.0.0",
    description = "Test plugin that throws errors for error handling verification",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {
        "clip:created",
    },

    schedules = {},
}

-- Initialize
storage.set("error_count", "0")
storage.set("calls_before_error", "0")
log("Error Test plugin loaded")

function on_clip_created(data)
    local calls = tonumber(storage.get("calls_before_error")) or 0
    calls = calls + 1
    storage.set("calls_before_error", tostring(calls))

    -- Always throw an error after recording the call
    local errors = tonumber(storage.get("error_count")) or 0
    storage.set("error_count", tostring(errors + 1))

    log("Error Test: About to throw error on call #" .. calls)
    error("Intentional test error on clip:created")
end
