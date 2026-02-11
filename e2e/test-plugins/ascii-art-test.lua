-- ASCII Art Converter Test Plugin
-- Simplified version for e2e testing
-- Converts images to ASCII art text using grayscale pixels

Plugin = {
    name = "ASCII Art Test",
    version = "1.0.0",
    description = "Test variant of ASCII art converter plugin",
    author = "e2e-tests",

    ui = {
        lightbox_buttons = {
            {id = "to_ascii", label = "To ASCII", icon = "code", async = true,
                options = {
                    {id = "width", type = "range", label = "Width (chars)", default = 80, min = 40, max = 120, step = 1},
                }
            },
        },
        card_actions = {
            {id = "to_ascii", label = "To ASCII", icon = "code", async = true,
                options = {
                    {id = "width", type = "range", label = "Width (chars)", default = 80, min = 40, max = 120, step = 1},
                }
            },
        },
    },
}

local charset = " .:-=+*#%@"

function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "to_ascii" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    options = options or {}
    local width = options.width or 80
    local height = math.floor(width / 2)

    local clip_count = #clip_ids
    local task_id = task.start("ASCII Art (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local pixels, pixel_err = image.grayscale_pixels(clip_id, width, height)
            if not pixels then
                error("Failed to get grayscale pixels: " .. (pixel_err or "unknown"))
            end

            -- Record pixel data info for verification
            storage.set("last_pixel_count", tostring(#pixels))
            storage.set("last_width", tostring(width))
            storage.set("last_height", tostring(height))

            local lines = {}
            for y = 0, height - 1 do
                local row = ""
                for x = 0, width - 1 do
                    local lum = pixels[y * width + x + 1] or 0
                    local inverted = 255 - lum
                    local idx = math.floor(inverted / 256 * #charset) + 1
                    if idx > #charset then idx = #charset end
                    if idx < 1 then idx = 1 end
                    row = row .. charset:sub(idx, idx)
                end
                lines[#lines + 1] = row
            end
            local art = table.concat(lines, "\n")

            -- Record output info for verification
            storage.set("last_art_lines", tostring(#lines))
            storage.set("last_art_length", tostring(#art))

            local clip_info = clips.get(clip_id)
            local name = "ascii_" .. clip_id .. ".txt"
            if clip_info and clip_info.filename then
                local base = clip_info.filename:match("^(.+)%.[^%.]+$") or clip_info.filename
                name = "ascii_" .. base .. ".txt"
            end

            local new_clip, create_err = clips.create({
                data = art,
                content_type = "text/plain",
                name = name,
            })
            if not new_clip then
                error("Failed to create ASCII clip: " .. (create_err or "unknown error"))
            end

            last_clip_id = new_clip.id
            storage.set("last_result_clip_id", tostring(new_clip.id))
        end)

        if not ok then
            errors = errors + 1
            storage.set("last_error", tostring(err))
            log("ASCII Art Test error: " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to convert to ASCII art")
    else
        task.complete(task_id)
        toast.show("ASCII art created", "success")
    end

    local processed = tonumber(storage.get("actions_executed")) or 0
    storage.set("actions_executed", tostring(processed + 1))

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

storage.set("loaded", "true")
storage.set("actions_executed", "0")
log("ASCII Art Test plugin loaded")
