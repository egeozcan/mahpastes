-- QR Code Generator Test Plugin
-- Simplified version for e2e testing
-- Instead of calling the real QR API, creates a simple placeholder image

Plugin = {
    name = "QR Code Test",
    version = "1.0.0",
    description = "Test variant of QR code generator plugin",
    author = "e2e-tests",

    -- No network needed: we create a local placeholder instead of calling API
    network = {},

    ui = {
        lightbox_buttons = {
            {id = "generate_qr", label = "Generate QR", icon = "qrcode", async = true},
        },
        card_actions = {
            {id = "generate_qr", label = "Generate QR", icon = "qrcode", async = true},
        },
    },
}

function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "generate_qr" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    local clip_count = #clip_ids
    local task_id = task.start("Generate QR (" .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local data, mime_type = clips.get_data(clip_id)
            if not data then
                error("Failed to get clip data: " .. (mime_type or "unknown error"))
            end

            -- For text clips, get_data returns plain text (not base64)
            -- For binary clips, it returns base64-encoded data
            local text
            if mime_type and (mime_type:find("^text/") or mime_type == "application/json") then
                text = data
            else
                text = base64.decode(data)
            end
            if not text or text == "" then
                error("Clip has no text content (mime: " .. (mime_type or "nil") .. ")")
            end

            -- Record the text for test verification
            storage.set("last_input_text", text)

            -- Instead of calling a network API, create a simple SVG placeholder
            local encoded_text = text:sub(1, 50) -- truncate for safety
            local svg = '<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300">'
            svg = svg .. '<rect width="300" height="300" fill="white"/>'
            svg = svg .. '<text x="150" y="150" text-anchor="middle" font-size="14">QR: ' .. encoded_text .. '</text>'
            svg = svg .. '</svg>'

            local clip_info = clips.get(clip_id)
            local name = "qr_" .. clip_id .. ".svg"
            if clip_info and clip_info.filename then
                local base = clip_info.filename:match("^(.+)%.[^%.]+$") or clip_info.filename
                name = "qr_" .. base .. ".svg"
            end

            -- Must base64-encode SVG for clips.create since image/svg+xml is not a text/ type
            local encoded_svg = base64.encode(svg)
            local new_clip, create_err = clips.create({
                data = encoded_svg,
                content_type = "image/svg+xml",
                data_encoding = "base64",
                name = name,
            })
            if not new_clip then
                error("Failed to create QR clip: " .. (create_err or "unknown error"))
            end

            last_clip_id = new_clip.id
            storage.set("last_result_clip_id", tostring(new_clip.id))
        end)

        if not ok then
            errors = errors + 1
            storage.set("last_error", tostring(err))
            log("QR Code Test error for clip " .. clip_id .. ": " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to generate QR code(s)")
    else
        task.complete(task_id)
        toast.show("QR code generated", "success")
    end

    local processed = tonumber(storage.get("actions_executed")) or 0
    storage.set("actions_executed", tostring(processed + 1))

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

storage.set("loaded", "true")
storage.set("actions_executed", "0")
log("QR Code Test plugin loaded")
