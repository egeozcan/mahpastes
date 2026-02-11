-- Palette Extractor Test Plugin
-- Simplified version for e2e testing
-- Extracts dominant colors using image API and creates SVG swatch clips

Plugin = {
    name = "Palette Extractor Test",
    version = "1.0.0",
    description = "Test variant of palette extractor plugin",
    author = "e2e-tests",

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

-- Build an SVG swatch from hex color strings
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
        log("Palette Extractor Test: failed to create tag '" .. name .. "': " .. (err or "unknown"))
        return nil
    end
    return new_tag.id
end

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

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local colors, color_err = image.dominant_colors(clip_id, count)
            if not colors then
                error("Failed to extract colors: " .. (color_err or "unknown"))
            end
            if #colors == 0 then
                error("No colors extracted from image")
            end

            -- Record extracted colors for test verification
            storage.set("last_colors", json.encode(colors))
            storage.set("last_color_count", tostring(#colors))

            local svg = build_swatch_svg(colors)

            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
            local name = original_name:match("^(.+)%.[^%.]+$") or original_name
            local palette_name = name .. "_palette.svg"

            -- Must base64-encode SVG for clips.create since image/svg+xml is not a text/ type
            local encoded_svg = base64.encode(svg)
            local new_clip, create_err = clips.create({
                data = encoded_svg,
                content_type = "image/svg+xml",
                data_encoding = "base64",
                name = palette_name,
            })
            if not new_clip then
                error("Failed to create palette clip: " .. (create_err or "unknown"))
            end

            last_clip_id = new_clip.id
            storage.set("last_result_clip_id", tostring(new_clip.id))

            if tag_colors then
                local tagged = {}
                for _, color in ipairs(colors) do
                    local tag_id = find_or_create_tag(color)
                    if tag_id then
                        tags.add_to_clip(clip_id, tag_id)
                        table.insert(tagged, color)
                    end
                end
                storage.set("last_tagged_colors", json.encode(tagged))
            end
        end)

        if not ok then
            errors = errors + 1
            storage.set("last_error", tostring(err))
            log("Palette Extractor Test error: " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to extract palette")
    else
        task.complete(task_id)
        toast.show("Extracted palette", "success")
    end

    local processed = tonumber(storage.get("actions_executed")) or 0
    storage.set("actions_executed", tostring(processed + 1))

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

storage.set("loaded", "true")
storage.set("actions_executed", "0")
log("Palette Extractor Test plugin loaded")
