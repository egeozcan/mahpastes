-- Auto-Archive Old Clips Plugin
-- Archives clips older than 24 hours

Plugin = {
    name = "Auto Archive Old",
    version = "1.0.0",
    description = "Automatically archives clips older than 24 hours",
    author = "mahpastes",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {"app:startup"},

    -- Run every hour
    schedules = {
        {name = "archive_old_clips", interval = 3600},
    },
}

local HOURS_THRESHOLD = 24

function on_startup()
    log("Auto Archive plugin started - will archive clips older than " .. HOURS_THRESHOLD .. " hours")
    -- Run immediately on startup
    archive_old_clips()
end

function archive_old_clips()
    local now = os.time()
    local threshold = now - (HOURS_THRESHOLD * 60 * 60)

    local all_clips = clips.list()
    local archived_count = 0

    for _, clip in ipairs(all_clips) do
        -- Skip already archived clips
        if not clip.is_archived then
            if clip.created_at < threshold then
                local success = clips.archive(clip.id)
                if success then
                    archived_count = archived_count + 1
                end
            end
        end
    end

    if archived_count > 0 then
        log("Archived " .. archived_count .. " old clips")
    end
end
