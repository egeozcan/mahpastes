-- Expiring Clips Test Plugin
-- Simplified version for e2e testing
-- Uses very short intervals for testability

Plugin = {
    name = "Expiring Clips Test",
    version = "1.0.0",
    description = "Test variant of expiring clips plugin",
    author = "e2e-tests",

    network = {},

    events = {},

    schedules = {
        {name = "check_expiry", interval = 2},
    },

    ui = {
        card_actions = {
            {id = "set_expiry", label = "Set Expiry", icon = "clock",
                options = {
                    {id = "duration", type = "select", label = "Expires In", default = "3",
                        choices = {
                            {value = "3", label = "3 Seconds (test)"},
                            {value = "86400", label = "1 Day"},
                        }
                    },
                }
            },
            {id = "clear_expiry", label = "Clear Expiry", icon = "x-circle"},
        },
    },
}

-- Load the expiry map from plugin storage
local function load_expiry_map()
    local raw = storage.get("expiry_map")
    if not raw or raw == "" then
        return {}
    end
    local ok, map = pcall(json.decode, raw)
    if not ok or type(map) ~= "table" then
        return {}
    end
    return map
end

-- Save the expiry map to plugin storage
local function save_expiry_map(map)
    storage.set("expiry_map", json.encode(map))
end

-- Handle card actions
function on_ui_action(action_id, clip_ids, options)
    options = options or {}

    if action_id == "set_expiry" then
        local duration = tonumber(options.duration) or 3
        local expiry_time = utils.time() + duration
        local map = load_expiry_map()

        for _, clip_id in ipairs(clip_ids) do
            map[tostring(clip_id)] = expiry_time
        end

        save_expiry_map(map)
        storage.set("last_set_duration", tostring(duration))
        storage.set("last_set_count", tostring(#clip_ids))

        local set_count = tonumber(storage.get("total_set")) or 0
        storage.set("total_set", tostring(set_count + #clip_ids))

        toast.show("Expiry set", "info")
        return {success = true}

    elseif action_id == "clear_expiry" then
        local map = load_expiry_map()
        local cleared = 0

        for _, clip_id in ipairs(clip_ids) do
            local key = tostring(clip_id)
            if map[key] then
                map[key] = nil
                cleared = cleared + 1
            end
        end

        save_expiry_map(map)
        storage.set("last_cleared_count", tostring(cleared))

        if cleared > 0 then
            toast.show("Expiry cleared", "info")
        else
            toast.show("No expiry was set", "info")
        end

        return {success = true}
    end

    return {success = false, error = "Unknown action: " .. tostring(action_id)}
end

-- Scheduled task: check for expired clips and archive them
function check_expiry()
    local now = utils.time()
    local map = load_expiry_map()
    local archived = 0
    local updated = false

    local check_count = tonumber(storage.get("check_count")) or 0
    storage.set("check_count", tostring(check_count + 1))

    for clip_id_str, expiry_time in pairs(map) do
        if now >= expiry_time then
            local clip_id = tonumber(clip_id_str)
            if clip_id then
                local ok, err = pcall(clips.archive, clip_id)
                if ok then
                    archived = archived + 1
                    log("Archived expired clip " .. clip_id_str)
                else
                    log("Failed to archive clip " .. clip_id_str .. ": " .. tostring(err))
                end
            end
            map[clip_id_str] = nil
            updated = true
        end
    end

    if updated then
        save_expiry_map(map)
    end

    local total_archived = tonumber(storage.get("total_archived")) or 0
    storage.set("total_archived", tostring(total_archived + archived))

    if archived > 0 then
        toast.show("Archived " .. archived .. " expired clip(s)", "success")
    end
end

storage.set("loaded", "true")
storage.set("check_count", "0")
storage.set("total_archived", "0")
storage.set("total_set", "0")
storage.set("expiry_map", "{}")
log("Expiring Clips Test plugin loaded")
