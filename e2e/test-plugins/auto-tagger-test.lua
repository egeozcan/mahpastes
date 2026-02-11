-- Auto-Tagger Test Plugin
-- Simplified version of auto-tagger for e2e testing
-- Same logic: tags clips based on content type and filename patterns

Plugin = {
    name = "Auto-Tagger Test",
    version = "1.0.0",
    description = "Test variant of auto-tagger plugin",
    author = "e2e-tests",

    events = {"clip:created"},
}

-- Tag rules: match on content type and filename
local rules = {
    {
        tag = "image",
        match = function(clip)
            return clip.content_type and clip.content_type:match("^image/") ~= nil
        end,
    },
    {
        tag = "text",
        match = function(clip)
            return clip.content_type and clip.content_type:match("^text/") ~= nil
        end,
    },
    {
        tag = "screenshot",
        match = function(clip)
            if not clip.filename then return false end
            local lower = clip.filename:lower()
            return lower:match("screenshot") ~= nil or clip.filename:match("Screen Shot") ~= nil
        end,
    },
    {
        tag = "document",
        match = function(clip)
            if not clip.filename then return false end
            return clip.filename:match("%.pdf$") ~= nil
        end,
    },
}

-- Find or create a tag by name, returns tag id
local function ensure_tag(name)
    local all_tags = tags.list()
    if all_tags then
        for _, t in ipairs(all_tags) do
            if t.name == name then
                return t.id
            end
        end
    end
    local new_tag, err = tags.create(name)
    if not new_tag then
        log("Auto-Tagger Test: failed to create tag '" .. name .. "': " .. (err or "unknown error"))
        return nil
    end
    return new_tag.id
end

function on_clip_created(data)
    if not data or not data.id then
        storage.set("last_error", "clip:created event missing clip id")
        return
    end

    local clip = clips.get(data.id)
    if not clip then
        storage.set("last_error", "could not get clip " .. tostring(data.id))
        return
    end

    local applied_tags = {}

    for _, rule in ipairs(rules) do
        if rule.match(clip) then
            local tag_id = ensure_tag(rule.tag)
            if tag_id then
                local _, err = tags.add_to_clip(tag_id, data.id)
                if err then
                    log("Auto-Tagger Test: failed to add tag '" .. rule.tag .. "': " .. err)
                else
                    table.insert(applied_tags, rule.tag)
                end
            end
        end
    end

    -- Record what happened for test verification
    local count = tonumber(storage.get("clips_processed")) or 0
    storage.set("clips_processed", tostring(count + 1))
    storage.set("last_clip_id", tostring(data.id))
    storage.set("last_applied_tags", json.encode(applied_tags))

    -- Append to log
    local log_data = json.decode(storage.get("tag_log") or "[]") or {}
    table.insert(log_data, {
        clip_id = data.id,
        content_type = clip.content_type,
        filename = clip.filename,
        tags_applied = applied_tags,
        time = utils.time(),
    })
    storage.set("tag_log", json.encode(log_data))
end

storage.set("loaded", "true")
storage.set("clips_processed", "0")
storage.set("tag_log", "[]")
log("Auto-Tagger Test plugin loaded")
