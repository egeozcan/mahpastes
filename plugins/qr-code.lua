-- QR Code Generator Plugin
-- Generates QR codes from text clip content

Plugin = {
    name = "QR Code Generator",
    version = "1.0.0",
    description = "Generate QR codes from text clips using the goqr.me API.",
    author = "mahpastes",

    network = {
        ["api.qrserver.com"] = {"GET"},
    },

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
                error("Failed to get clip data")
            end

            -- For text/* and application/json, get_data returns plain text.
            -- For binary content types, it returns base64-encoded data.
            local text = data
            if not mime_type:match("^text/") and mime_type ~= "application/json" then
                text = base64.decode(data)
            end
            if not text or text == "" then
                error("Clip has no text content")
            end

            -- URL-encode the text for the API
            local encoded = utils.url_encode(text)
            local url = "https://api.qrserver.com/v1/create-qr-code/?size=300x300&data=" .. encoded

            -- Get original clip info for naming
            local clip_info = clips.get(clip_id)
            local name = "qr_" .. clip_id .. ".png"
            if clip_info and clip_info.filename then
                local base = clip_info.filename:match("^(.+)%.[^%.]+$") or clip_info.filename
                name = "qr_" .. base .. ".png"
            end

            local new_clip, create_err = clips.create_from_url(url, {name = name})
            if not new_clip then
                error("Failed to create QR clip: " .. (create_err or "unknown error"))
            end

            last_clip_id = new_clip.id
        end)

        if not ok then
            errors = errors + 1
            log("QR Code error for clip " .. clip_id .. ": " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to generate QR code(s)")
    else
        task.complete(task_id)
        toast.show("QR code generated", "success")
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

log("QR Code Generator plugin loaded")
