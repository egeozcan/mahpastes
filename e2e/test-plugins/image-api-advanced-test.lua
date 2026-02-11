-- Image API Advanced Test Plugin
-- Tests advanced image API features: diff with different images, resize output
-- verification, overlay with x/y coordinates, all position keywords, multi-color dominant_colors

Plugin = {
    name = "Image API Advanced Test",
    version = "1.0.0",
    description = "Advanced test plugin for image API",
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
storage.set("clip_count", "0")
log("Image API Advanced Test plugin loaded")

function on_clip_created(data)
    if not data or not data.id then
        storage.set("error", "no clip id in event data")
        return
    end

    local count = tonumber(storage.get("clip_count")) or 0
    count = count + 1
    storage.set("clip_count", tostring(count))

    if count == 1 then
        storage.set("first_clip_id", tostring(data.id))
        run_single_image_tests(data.id)
    elseif count == 2 then
        local first_id = tonumber(storage.get("first_clip_id"))
        run_diff_test(first_id, data.id)
        storage.set("all_advanced_tests_done", "true")
    end
end

function run_single_image_tests(clip_id)
    -- Test 1: Resize output verification (50x40)
    local resized, err = image.resize(clip_id, 50, 40)
    if resized then
        storage.set("resize_verify_ok", "true")
        storage.set("resize_verify_has_data", tostring(resized.data ~= nil and #resized.data > 0))
        storage.set("resize_verify_mime", resized.mime_type or "")
        log("resize verification succeeded, data length=" .. tostring(#(resized.data or "")))
    else
        storage.set("resize_verify_ok", "false")
        storage.set("resize_verify_error", err or "unknown error")
        log("resize verification failed: " .. (err or "unknown error"))
    end

    -- Test 2: Overlay text with explicit x/y coordinates (no position keyword)
    local overlay_xy, err = image.overlay_text(clip_id, {
        text = "XY Test",
        x = 10,
        y = 30,
        size = 14,
        color = "#00FF00",
    })
    if overlay_xy then
        storage.set("overlay_xy_ok", "true")
        storage.set("overlay_xy_has_data", tostring(overlay_xy.data ~= nil and #overlay_xy.data > 0))
        storage.set("overlay_xy_mime", overlay_xy.mime_type or "")
        log("overlay with x/y succeeded")
    else
        storage.set("overlay_xy_ok", "false")
        storage.set("overlay_xy_error", err or "unknown error")
        log("overlay with x/y failed: " .. (err or "unknown error"))
    end

    -- Test 3: Overlay text with all position keywords
    local positions = { "center", "top", "bottom", "top-left", "top-right", "bottom-left", "bottom-right" }
    local all_positions_ok = true
    local position_results = {}

    for _, pos in ipairs(positions) do
        local result, err = image.overlay_text(clip_id, {
            text = "Pos " .. pos,
            position = pos,
            size = 12,
            color = "#FFFFFF",
        })
        local key = "overlay_pos_" .. pos:gsub("-", "_")
        if result then
            storage.set(key, "true")
            table.insert(position_results, pos .. "=ok")
        else
            storage.set(key, "false")
            storage.set(key .. "_error", err or "unknown error")
            all_positions_ok = false
            table.insert(position_results, pos .. "=fail")
        end
    end

    storage.set("overlay_all_positions_ok", tostring(all_positions_ok))
    storage.set("overlay_positions_count", tostring(#positions))
    storage.set("overlay_positions_results", table.concat(position_results, ","))
    log("overlay all positions: " .. table.concat(position_results, ", "))

    -- Test 4: Dominant colors (at least 1 color returned)
    local colors, err = image.dominant_colors(clip_id, 5)
    if colors then
        storage.set("colors_multi_ok", "true")
        storage.set("colors_multi_count", tostring(#colors))
        storage.set("colors_multi_result", json.encode(colors))
        log("dominant_colors multi succeeded, count=" .. tostring(#colors))
    else
        storage.set("colors_multi_ok", "false")
        storage.set("colors_multi_error", err or "unknown error")
        log("dominant_colors multi failed: " .. (err or "unknown error"))
    end

    storage.set("single_image_tests_done", "true")
    log("Single image tests completed")
end

function run_diff_test(first_id, second_id)
    log("Running diff test between clip " .. tostring(first_id) .. " and " .. tostring(second_id))

    local diff_result, err = image.diff(first_id, second_id)
    if diff_result then
        storage.set("diff_different_ok", "true")
        storage.set("diff_different_similarity", tostring(diff_result.similarity))
        storage.set("diff_different_has_data", tostring(diff_result.diff_data ~= nil and #diff_result.diff_data > 0))
        log("diff between different images: similarity=" .. tostring(diff_result.similarity))
    else
        storage.set("diff_different_ok", "false")
        storage.set("diff_different_error", err or "unknown error")
        log("diff between different images failed: " .. (err or "unknown error"))
    end
end
