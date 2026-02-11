-- Image API Test Plugin
-- Tests image module functions via clip:created event

Plugin = {
    name = "Image API Test",
    version = "1.0.0",
    description = "Test plugin for image API",
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
log("Image API Test plugin loaded")

function on_clip_created(data)
    if not data or not data.id then
        storage.set("error", "no clip id in event data")
        return
    end

    local clip_id = data.id

    -- Test image.info
    local info, err = image.info(clip_id)
    if info then
        storage.set("info_result", json.encode(info))
        log("image.info succeeded: " .. json.encode(info))
    else
        storage.set("info_error", err or "unknown error")
        log("image.info failed: " .. (err or "unknown error"))
        return
    end

    -- Test image.resize
    local resized, err = image.resize(clip_id, 50, 50)
    if resized then
        storage.set("resize_ok", "true")
        storage.set("resize_has_data", tostring(resized.data ~= nil and #resized.data > 0))
        storage.set("resize_mime", resized.mime_type or "")
        log("image.resize succeeded")
    else
        storage.set("resize_error", err or "unknown error")
    end

    -- Test image.dominant_colors
    local colors, err = image.dominant_colors(clip_id, 3)
    if colors then
        storage.set("colors_result", json.encode(colors))
        log("image.dominant_colors succeeded: " .. json.encode(colors))
    else
        storage.set("colors_error", err or "unknown error")
    end

    -- Test image.grayscale_pixels (small size)
    local pixels, err = image.grayscale_pixels(clip_id, 2, 2)
    if pixels then
        storage.set("grayscale_count", tostring(#pixels))
        storage.set("grayscale_ok", "true")
        log("image.grayscale_pixels succeeded, count=" .. #pixels)
    else
        storage.set("grayscale_error", err or "unknown error")
    end

    -- Test image.diff with same image
    local diff_result, err = image.diff(clip_id, clip_id)
    if diff_result then
        storage.set("diff_similarity", tostring(diff_result.similarity))
        storage.set("diff_has_data", tostring(diff_result.diff_data ~= nil and #diff_result.diff_data > 0))
        log("image.diff succeeded, similarity=" .. diff_result.similarity)
    else
        storage.set("diff_error", err or "unknown error")
    end

    -- Test image.overlay_text
    local overlay_result, err = image.overlay_text(clip_id, {
        text = "Hello Test",
        position = "center",
        size = 16,
        color = "#FF0000",
    })
    if overlay_result then
        storage.set("overlay_ok", "true")
        storage.set("overlay_has_data", tostring(overlay_result.data ~= nil and #overlay_result.data > 0))
        storage.set("overlay_mime", overlay_result.mime_type or "")
        log("image.overlay_text succeeded")
    else
        storage.set("overlay_error", err or "unknown error")
    end

    -- Test image.composite
    local composite_result, err = image.composite({
        width = 100,
        height = 100,
        background = "#FFFFFF",
        layers = {
            { clip_id = clip_id, x = 0, y = 0, width = 50, height = 50 },
            { clip_id = clip_id, x = 50, y = 50, width = 50, height = 50 },
        },
    })
    if composite_result then
        storage.set("composite_ok", "true")
        storage.set("composite_has_data", tostring(composite_result.data ~= nil and #composite_result.data > 0))
        storage.set("composite_mime", composite_result.mime_type or "")
        log("image.composite succeeded")
    else
        storage.set("composite_error", err or "unknown error")
    end

    -- Test image.metadata
    local meta, err = image.metadata(clip_id)
    if meta then
        storage.set("metadata_ok", "true")
        storage.set("metadata_type", type(meta))
        log("image.metadata succeeded, type=" .. type(meta))
    else
        storage.set("metadata_error", err or "unknown error")
    end

    storage.set("all_tests_done", "true")
end
