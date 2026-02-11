-- Image API Error Cases Test Plugin
-- Tests error handling for invalid inputs

Plugin = {
    name = "Image API Error Test",
    version = "1.0.0",
    description = "Test plugin for image API error cases",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {
        "clip:created",
    },

    schedules = {},
}

storage.set("loaded", "true")
log("Image API Error Test plugin loaded")

function on_clip_created(data)
    if not data or not data.id then
        return
    end

    local clip_id = data.id

    -- Test resize with invalid fit mode
    local res, err = image.resize(clip_id, 50, 50, { fit = "invalid_mode" })
    storage.set("resize_invalid_fit_ok", tostring(res ~= nil))
    storage.set("resize_invalid_fit_err", err or "no_error")

    -- Test resize with contain mode
    local contain_res, err = image.resize(clip_id, 50, 50, { fit = "contain" })
    storage.set("resize_contain_ok", tostring(contain_res ~= nil))
    storage.set("resize_contain_err", err or "no_error")

    -- Test resize with cover mode
    local cover_res, err = image.resize(clip_id, 50, 50, { fit = "cover" })
    storage.set("resize_cover_ok", tostring(cover_res ~= nil))
    storage.set("resize_cover_err", err or "no_error")

    -- Test overlay_text with invalid position
    local overlay_res, err = image.overlay_text(clip_id, {
        text = "Hello",
        position = "invalid_position",
        size = 16,
    })
    storage.set("overlay_invalid_pos_ok", tostring(overlay_res ~= nil))
    storage.set("overlay_invalid_pos_err", err or "no_error")

    -- Test image.info with non-existent clip ID
    local info, err = image.info(999999)
    storage.set("info_nonexistent_ok", tostring(info ~= nil))
    storage.set("info_nonexistent_err", err or "no_error")

    -- Test resize with non-existent clip ID
    local res2, err = image.resize(999999, 50, 50)
    storage.set("resize_nonexistent_ok", tostring(res2 ~= nil))
    storage.set("resize_nonexistent_err", err or "no_error")

    storage.set("error_tests_done", "true")
end
