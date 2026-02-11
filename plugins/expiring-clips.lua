-- Expiring Clips Plugin
-- Set time-based expiry on clips; expired clips are automatically archived.

Plugin = {
    name = "Expiring Clips",
    version = "1.0.0",
    description = "Set expiry timers on clips. Expired clips are automatically archived after the chosen duration.",
    author = "mahpastes",

    schedules = {
        {name = "check_expiry", interval = 300},
    },

    ui = {
        card_actions = {
            {id = "set_expiry", label = "Set Expiry", icon = "clock",
                options = {
                    {id = "duration", type = "select", label = "Expires In", default = "86400",
                        choices = {
                            {value = "3600", label = "1 Hour"},
                            {value = "86400", label = "1 Day"},
                            {value = "604800", label = "1 Week"},
                            {value = "2592000", label = "30 Days"},
                        }
                    },
                }
            },
            {id = "clear_expiry", label = "Clear Expiry", icon = "x-circle"},
        },
    },
}

-- Human-readable duration for toast messages
local function format_duration(seconds)
    if seconds >= 2592000 then
        local days = math.floor(seconds / 86400)
        return days .. " days"
    elseif seconds >= 604800 then
        local weeks = math.floor(seconds / 604800)
        return weeks .. " week" .. (weeks > 1 and "s" or "")
    elseif seconds >= 86400 then
        local days = math.floor(seconds / 86400)
        return days .. " day" .. (days > 1 and "s" or "")
    elseif seconds >= 3600 then
        local hours = math.floor(seconds / 3600)
        return hours .. " hour" .. (hours > 1 and "s" or "")
    else
        local minutes = math.floor(seconds / 60)
        return minutes .. " minute" .. (minutes > 1 and "s" or "")
    end
end

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
        local duration = tonumber(options.duration) or 86400
        local expiry_time = utils.time() + duration
        local map = load_expiry_map()

        for _, clip_id in ipairs(clip_ids) do
            map[tostring(clip_id)] = expiry_time
        end

        save_expiry_map(map)

        local label = format_duration(duration)
        local count = #clip_ids
        if count == 1 then
            toast.show("Expiry set: clip will be archived in " .. label, "info")
        else
            toast.show("Expiry set: " .. count .. " clips will be archived in " .. label, "info")
        end

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

        if cleared > 0 then
            if cleared == 1 then
                toast.show("Expiry cleared for clip", "info")
            else
                toast.show("Expiry cleared for " .. cleared .. " clips", "info")
            end
        else
            toast.show("No expiry was set on selected clips", "info")
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

    if archived > 0 then
        toast.show("Archived " .. archived .. " expired clip" .. (archived > 1 and "s" or ""), "success")
    end
end

log("Expiring Clips plugin loaded")
