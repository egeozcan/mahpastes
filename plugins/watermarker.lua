-- Watermarker Plugin
-- Applies text watermarks to images with configurable position, opacity, size, and color

Plugin = {
    name = "Watermarker",
    version = "1.0.0",
    description = "Apply customizable text watermarks to images with control over position, opacity, size, and color.",
    author = "mahpastes",

    ui = {
        lightbox_buttons = {
            {id = "watermark", label = "Watermark", icon = "pencil", async = true, file_types = {"image/*"},
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
            {id = "watermark", label = "Watermark", icon = "pencil", async = true, file_types = {"image/*"},
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
    },
}

-- Generate output filename
local function generate_filename(original)
    local name = original:match("^(.+)%.[^%.]+$") or original
    local ext = original:match("%.([^%.]+)$") or "png"
    return name .. "_watermarked." .. ext
end

-- Handle UI action from lightbox or card menu
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "watermark" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    options = options or {}
    local text = options.text
    if not text or text == "" then
        toast.show("Watermark text is required", "error")
        return {success = false, error = "Watermark text is required"}
    end

    local position = options.position or "center"
    local opacity = options.opacity or 0.5
    local size = options.size or 24
    local color = options.color or "#ffffff"

    local clip_count = #clip_ids
    local task_id = task.start("Watermark (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            -- Apply watermark via image API
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

            -- Get original clip info for naming
            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id .. ".png")
            local filename = generate_filename(original_name)

            -- Create new clip with watermarked image
            local new_clip, create_err = clips.create({
                data = result.data,
                content_type = result.mime_type,
                name = filename,
            })
            if not new_clip then
                error("Failed to save watermarked image: " .. (create_err or "unknown"))
            end

            last_clip_id = new_clip.id
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("Watermarker error on clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "Failed to apply watermark"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
    else
        toast.show("Watermark applied", "success")
        task.complete(task_id)
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

log("Watermarker plugin loaded")
