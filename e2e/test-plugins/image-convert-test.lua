-- Image Convert Test Plugin
-- Tests image.convert function via clip:created event

Plugin = {
    name = "Image Convert Test",
    version = "1.0.0",
    description = "Test plugin for image.convert API",
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
log("Image Convert Test plugin loaded")

function on_clip_created(data)
    if not data or not data.id then
        storage.set("error", "no clip id in event data")
        return
    end

    local clip_id = data.id

    -- Test 1: Convert to JPEG with default quality (85)
    local result, err = image.convert(clip_id, "jpeg")
    if result then
        storage.set("jpeg_default_ok", "true")
        storage.set("jpeg_default_mime", result.mime_type or "")
        storage.set("jpeg_default_has_data", tostring(result.data ~= nil and #result.data > 0))
        storage.set("jpeg_default_data_len", tostring(#result.data))
        log("convert jpeg default succeeded")
    else
        storage.set("jpeg_default_ok", "false")
        storage.set("jpeg_default_err", err or "unknown error")
        log("convert jpeg default failed: " .. (err or "unknown error"))
    end

    -- Test 2: Convert to JPEG with explicit quality (50)
    local result2, err2 = image.convert(clip_id, "jpeg", { quality = 50 })
    if result2 then
        storage.set("jpeg_q50_ok", "true")
        storage.set("jpeg_q50_mime", result2.mime_type or "")
        storage.set("jpeg_q50_has_data", tostring(result2.data ~= nil and #result2.data > 0))
        storage.set("jpeg_q50_data_len", tostring(#result2.data))
        log("convert jpeg q50 succeeded")
    else
        storage.set("jpeg_q50_ok", "false")
        storage.set("jpeg_q50_err", err2 or "unknown error")
    end

    -- Test 3: Convert to PNG (identity conversion)
    local result3, err3 = image.convert(clip_id, "png")
    if result3 then
        storage.set("png_ok", "true")
        storage.set("png_mime", result3.mime_type or "")
        storage.set("png_has_data", tostring(result3.data ~= nil and #result3.data > 0))
        log("convert png succeeded")
    else
        storage.set("png_ok", "false")
        storage.set("png_err", err3 or "unknown error")
    end

    -- Test 4: "jpg" alias
    local result4, err4 = image.convert(clip_id, "jpg")
    if result4 then
        storage.set("jpg_alias_ok", "true")
        storage.set("jpg_alias_mime", result4.mime_type or "")
        log("convert jpg alias succeeded")
    else
        storage.set("jpg_alias_ok", "false")
        storage.set("jpg_alias_err", err4 or "unknown error")
    end

    -- Test 5: Invalid format
    local result5, err5 = image.convert(clip_id, "bmp")
    if result5 then
        storage.set("invalid_format_ok", "true")
    else
        storage.set("invalid_format_ok", "false")
        storage.set("invalid_format_err", err5 or "unknown error")
        log("convert invalid format correctly failed: " .. (err5 or ""))
    end

    -- Test 6: Non-existent clip
    local result6, err6 = image.convert(999999, "jpeg")
    if result6 then
        storage.set("nonexistent_clip_ok", "true")
    else
        storage.set("nonexistent_clip_ok", "false")
        storage.set("nonexistent_clip_err", err6 or "unknown error")
        log("convert nonexistent clip correctly failed: " .. (err6 or ""))
    end

    -- Test 7: Quality clamping (quality < 1 should clamp to 1)
    local result7, err7 = image.convert(clip_id, "jpeg", { quality = -10 })
    if result7 then
        storage.set("quality_clamp_low_ok", "true")
        storage.set("quality_clamp_low_mime", result7.mime_type or "")
        log("convert quality clamp low succeeded")
    else
        storage.set("quality_clamp_low_ok", "false")
        storage.set("quality_clamp_low_err", err7 or "unknown error")
    end

    -- Test 8: Quality clamping (quality > 100 should clamp to 100)
    local result8, err8 = image.convert(clip_id, "jpeg", { quality = 200 })
    if result8 then
        storage.set("quality_clamp_high_ok", "true")
        storage.set("quality_clamp_high_mime", result8.mime_type or "")
        log("convert quality clamp high succeeded")
    else
        storage.set("quality_clamp_high_ok", "false")
        storage.set("quality_clamp_high_err", err8 or "unknown error")
    end

    storage.set("all_tests_done", "true")
end
