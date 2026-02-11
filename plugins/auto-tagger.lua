-- Auto-Tagger Plugin
-- Automatically tags new clips based on content type and filename patterns

Plugin = {
    name = "Auto-Tagger",
    version = "1.0.0",
    description = "Automatically tags new clips based on content type and filename patterns.",
    author = "mahpastes",

    events = {"clip:created"},
}

-- Tag rules: each rule has a match function and a tag name
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
        log("Auto-Tagger: failed to create tag '" .. name .. "': " .. (err or "unknown error"))
        return nil
    end
    return new_tag.id
end

function on_clip_created(data)
    if not data or not data.id then
        log("Auto-Tagger: clip:created event missing clip id")
        return
    end

    local clip = clips.get(data.id)
    if not clip then
        log("Auto-Tagger: could not get clip " .. tostring(data.id))
        return
    end

    for _, rule in ipairs(rules) do
        if rule.match(clip) then
            local tag_id = ensure_tag(rule.tag)
            if tag_id then
                local _, err = tags.add_to_clip(tag_id, data.id)
                if err then
                    log("Auto-Tagger: failed to add tag '" .. rule.tag .. "' to clip " .. tostring(data.id) .. ": " .. err)
                end
            end
        end
    end
end

storage.set("loaded", "true")
log("Auto-Tagger plugin loaded")
