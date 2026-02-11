-- Image API Edge Cases Test Plugin
-- Tests edge cases: pixel limits, invalid colors, composite errors, non-image clips

Plugin = {
    name = "Image API Edge Cases Test",
    version = "1.0.0",
    description = "Test plugin for image API edge cases",
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
log("Image API Edge Cases Test plugin loaded")

function on_clip_created(data)
    if not data or not data.id then
        return
    end

    local clip_id = data.id

    -- Try image.info to determine if this is an image
    local info, err = image.info(clip_id)

    if info then
        -- This is an image clip - run image-specific edge cases
        run_image_edge_cases(clip_id)
    else
        -- This is NOT an image - run non-image tests
        run_non_image_tests(clip_id)
    end
end

function run_image_edge_cases(clip_id)
    -- Test grayscale_pixels exceeding limit (1001 * 1000 = 1,001,000 > 1M)
    local pixels, err = image.grayscale_pixels(clip_id, 1001, 1000)
    storage.set("grayscale_limit_ok", tostring(pixels ~= nil))
    storage.set("grayscale_limit_err", err or "no_error")

    -- Test overlay_text with invalid color
    local res, err = image.overlay_text(clip_id, {
        text = "test",
        position = "center",
        size = 16,
        color = "#ZZZZZZ",
    })
    storage.set("overlay_bad_color_ok", tostring(res ~= nil))
    storage.set("overlay_bad_color_err", err or "no_error")

    -- Test composite with missing width/height
    local res, err = image.composite({ layers = {} })
    storage.set("composite_no_dims_ok", tostring(res ~= nil))
    storage.set("composite_no_dims_err", err or "no_error")

    -- Test composite with missing layers
    local res, err = image.composite({ width = 100, height = 100 })
    storage.set("composite_no_layers_ok", tostring(res ~= nil))
    storage.set("composite_no_layers_err", err or "no_error")

    -- Test composite with invalid layer (missing clip_id)
    local res, err = image.composite({
        width = 100,
        height = 100,
        layers = { { x = 0, y = 0 } },
    })
    storage.set("composite_bad_layer_ok", tostring(res ~= nil))
    storage.set("composite_bad_layer_err", err or "no_error")

    -- Test resize with zero width
    local res, err = image.resize(clip_id, 0, 50)
    storage.set("resize_zero_w_ok", tostring(res ~= nil))
    storage.set("resize_zero_w_err", err or "no_error")

    -- Test resize with negative dimensions
    local res, err = image.resize(clip_id, -1, 50)
    storage.set("resize_neg_ok", tostring(res ~= nil))
    storage.set("resize_neg_err", err or "no_error")

    storage.set("image_edge_tests_done", "true")
    log("Image edge case tests completed")
end

function run_non_image_tests(clip_id)
    -- Test resize on non-image
    local res, err = image.resize(clip_id, 50, 50)
    storage.set("resize_text_ok", tostring(res ~= nil))
    storage.set("resize_text_err", err or "no_error")

    -- Test overlay_text on non-image
    local res, err = image.overlay_text(clip_id, {
        text = "test",
        position = "center",
        size = 16,
    })
    storage.set("overlay_text_clip_ok", tostring(res ~= nil))
    storage.set("overlay_text_clip_err", err or "no_error")

    -- Test dominant_colors on non-image
    local res, err = image.dominant_colors(clip_id)
    storage.set("colors_text_ok", tostring(res ~= nil))
    storage.set("colors_text_err", err or "no_error")

    storage.set("non_image_tests_done", "true")
    log("Non-image edge case tests completed")
end
