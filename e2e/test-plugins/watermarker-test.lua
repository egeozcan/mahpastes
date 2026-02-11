-- Watermarker Test Plugin
-- Simplified version for e2e testing
-- Applies text watermarks using image.overlay_text API

Plugin = {
    name = "Watermarker Test",
    version = "1.0.0",
    description = "Test variant of watermarker plugin",
    author = "e2e-tests",

    ui = {
        lightbox_buttons = {
            {id = "watermark", label = "Watermark", icon = "pencil", async = true,
                options = {
                    {id = "text", type = "text", label = "Watermark Text", required = true},
                    {id = "position", type = "select", label = "Position", default = "center",
                        choices = {
                            {value = "center", label = "Center"},
                            {value = "bottom-right", label = "Bottom Right"},
                            {value = "top-right", label = "Top Right"},
                            {value = "top-left", label = "Top Left"},
                            {value = "bottom-left", label = "Bottom Left"},
                        }
                    },
                    {id = "opacity", type = "range", label = "Opacity", default = 0.5, min = 0.1, max = 1, step = 0.1},
                    {id = "size", type = "range", label = "Font Size", default = 24, min = 12, max = 72, step = 1},
                    {id = "color", type = "text", label = "Color (hex)", default = "#ffffff"},
                }
            },
        },
        card_actions = {
            {id = "watermark", label = "Watermark", icon = "pencil", async = true,
                options = {
                    {id = "text", type = "text", label = "Watermark Text", required = true},
                    {id = "position", type = "select", label = "Position", default = "center",
                        choices = {
                            {value = "center", label = "Center"},
                            {value = "bottom-right", label = "Bottom Right"},
                        }
                    },
                    {id = "opacity", type = "range", label = "Opacity", default = 0.5, min = 0.1, max = 1, step = 0.1},
                    {id = "size", type = "range", label = "Font Size", default = 24, min = 12, max = 72, step = 1},
                    {id = "color", type = "text", label = "Color (hex)", default = "#ffffff"},
                }
            },
        },
    },
}

local function generate_filename(original)
    local name = original:match("^(.+)%.[^%.]+$") or original
    local ext = original:match("%.([^%.]+)$") or "png"
    return name .. "_watermarked." .. ext
end

function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "watermark" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    options = options or {}
    local text = options.text
    if not text or text == "" then
        storage.set("last_error", "Watermark text is required")
        toast.show("Watermark text is required", "error")
        return {success = false, error = "Watermark text is required"}
    end

    local position = options.position or "center"
    local opacity = options.opacity or 0.5
    local size = options.size or 24
    local color = options.color or "#ffffff"

    -- Record options for verification
    storage.set("last_options", json.encode({
        text = text,
        position = position,
        opacity = opacity,
        size = size,
        color = color,
    }))

    local clip_count = #clip_ids
    local task_id = task.start("Watermark (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local result = image.overlay_text(clip_id, {
                text = text,
                position = position,
                opacity = opacity,
                size = size,
                color = color,
            })
            if not result or not result.data then
                error("Failed to apply watermark")
            end

            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id .. ".png")
            local filename = generate_filename(original_name)

            local new_clip, create_err = clips.create({
                data = result.data,
                content_type = result.mime_type,
                name = filename,
            })
            if not new_clip then
                error("Failed to save watermarked image: " .. (create_err or "unknown"))
            end

            last_clip_id = new_clip.id
            storage.set("last_result_clip_id", tostring(new_clip.id))
            storage.set("last_result_filename", filename)
        end)

        if not ok then
            errors = errors + 1
            storage.set("last_error", tostring(err))
            log("Watermarker Test error: " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to apply watermark")
    else
        toast.show("Watermark applied", "success")
        task.complete(task_id)
    end

    local processed = tonumber(storage.get("actions_executed")) or 0
    storage.set("actions_executed", tostring(processed + 1))

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

storage.set("loaded", "true")
storage.set("actions_executed", "0")
log("Watermarker Test plugin loaded")
