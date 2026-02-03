-- Scheduler Test Plugin
-- Tests scheduled task execution

Plugin = {
    name = "Scheduler Test",
    version = "1.0.0",
    description = "Test plugin for scheduled task verification",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {},

    schedules = {
        {name = "counter_tick", interval = 2},
    },
}

-- Initialize on load
storage.set("tick_count", "0")
storage.set("started_at", tostring(utils.time()))
storage.set("last_tick", "0")
log("Scheduler Test plugin loaded")

-- Scheduled task handler - name must match the schedule key
function counter_tick()
    local count = tonumber(storage.get("tick_count")) or 0
    count = count + 1
    storage.set("tick_count", tostring(count))
    storage.set("last_tick", tostring(utils.time()))
    log("Scheduler tick #" .. count)
end
