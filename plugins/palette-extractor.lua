-- Palette Extractor Plugin
-- Extracts dominant colors from images and creates SVG swatch clips

Plugin = {
    name = "Palette Extractor",
    version = "1.0.0",
    description = "Extract dominant colors from images and generate color palette swatches.",
    author = "mahpastes",

    events = {"clip:created"},

    ui = {
        lightbox_buttons = {
            {id = "extract_palette", label = "Extract Palette", icon = "swatch", async = true,
                options = {
                    {id = "count", type = "range", label = "Number of Colors", default = 6, min = 3, max = 10, step = 1},
                    {id = "tag_colors", type = "checkbox", label = "Tag with hex colors", default = false},
                }
            },
        },
        card_actions = {
            {id = "extract_palette", label = "Extract Palette", icon = "swatch", async = true,
                options = {
                    {id = "count", type = "range", label = "Number of Colors", default = 6, min = 3, max = 10, step = 1},
                    {id = "tag_colors", type = "checkbox", label = "Tag with hex colors", default = false},
                }
            },
        },
    },
}

-- Build an SVG swatch from an array of hex color strings
local function build_swatch_svg(colors)
    local count = #colors
    local swatch_width = math.floor(600 / count)
    local total_width = swatch_width * count
    local svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' .. total_width .. '" height="100">'
    for i, color in ipairs(colors) do
        local x = (i - 1) * swatch_width
        svg = svg .. '<rect x="' .. x .. '" y="0" width="' .. swatch_width .. '" height="100" fill="' .. color .. '"/>'
    end
    svg = svg .. '</svg>'
    return svg
end

-- Find or create a tag by name
local function find_or_create_tag(name)
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
        log("Palette Extractor: failed to create tag '" .. name .. "': " .. (err or "unknown"))
        return nil
    end
    return new_tag.id
end

-- Handle UI action from lightbox or card menu
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "extract_palette" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    options = options or {}
    local count = options.count or 6
    local tag_colors = options.tag_colors or false
    local clip_count = #clip_ids
    local task_id = task.start("Extract Palette (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            -- Extract dominant colors
            local colors = image.dominant_colors(clip_id, count)
            if not colors or #colors == 0 then
                error("Failed to extract colors")
            end

            -- Build SVG swatch
            local svg = build_swatch_svg(colors)

            -- Get original clip info for naming
            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
            local name = original_name:match("^(.+)%.[^%.]+$") or original_name
            local palette_name = name .. "_palette.svg"

            -- Create palette clip (base64 encode SVG since image/* types are auto-detected as binary)
            local new_clip, create_err = clips.create({
                data = base64.encode(svg),
                content_type = "image/svg+xml",
                name = palette_name,
            })
            if not new_clip then
                error("Failed to create palette clip: " .. (create_err or "unknown"))
            end

            last_clip_id = new_clip.id

            -- Optionally tag the original clip with hex colors
            if tag_colors then
                for _, color in ipairs(colors) do
                    local tag_id = find_or_create_tag(color)
                    if tag_id then
                        tags.add_to_clip(tag_id, clip_id)
                    end
                end
            end
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("Palette Extractor error on clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "Failed to extract palette"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
    else
        local extracted = clip_count - errors
        toast.show("Extracted " .. count .. " colors from " .. extracted .. " image" .. (extracted > 1 and "s" or ""), "success")
        task.complete(task_id)
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

log("Palette Extractor plugin loaded")
