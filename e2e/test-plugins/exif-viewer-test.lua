-- EXIF/Metadata Viewer Test Plugin
-- Simplified version for e2e testing
-- Extracts image metadata and creates text reports

Plugin = {
    name = "EXIF Viewer Test",
    version = "1.0.0",
    description = "Test variant of EXIF/metadata viewer plugin",
    author = "e2e-tests",

    events = {"clip:created"},

    ui = {
        lightbox_buttons = {
            {id = "view_exif", label = "View EXIF", icon = "info", async = true},
        },
        card_actions = {
            {id = "view_exif", label = "View EXIF", icon = "info", async = true},
        },
    },
}

-- Find or create a tag by name
local function find_or_create_tag(name)
    local all_tags = tags.list()
    if all_tags then
        for _, t in ipairs(all_tags) do
            if t.name == name then
                return t.id
            end
        end
    end
    local new_tag, err = tags.create(name)
    if not new_tag then
        log("EXIF Viewer Test: failed to create tag '" .. name .. "': " .. (err or "unknown"))
        return nil
    end
    return new_tag.id
end

-- Handle UI action: extract and display EXIF data
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "view_exif" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    local clip_count = #clip_ids
    local task_id = task.start("View EXIF (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local clip_info = clips.get(clip_id)
            local info = image.info(clip_id)
            local meta = image.metadata(clip_id)

            -- Record metadata for test verification
            if info then
                storage.set("last_info", json.encode({
                    width = info.width,
                    height = info.height,
                    format = info.format,
                    size = info.size,
                }))
            end

            if meta then
                storage.set("last_metadata", json.encode(meta))
            end

            -- Build report text
            local lines = {"=== Image Metadata ===", ""}
            if clip_info and clip_info.filename then
                table.insert(lines, "File: " .. clip_info.filename)
            end
            if info then
                if info.width and info.height then
                    table.insert(lines, "Dimensions: " .. info.width .. " x " .. info.height)
                end
                if info.format then
                    table.insert(lines, "Format: " .. info.format)
                end
            end
            if meta and meta.camera_model and meta.camera_model ~= "" then
                table.insert(lines, "Camera: " .. meta.camera_model)
            else
                table.insert(lines, "")
                table.insert(lines, "No EXIF data found.")
            end
            local report = table.concat(lines, "\n")

            -- Create text clip with report
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
            local name = original_name:match("^(.+)%.[^%.]+$") or original_name
            local report_name = name .. "_exif.txt"

            local new_clip, create_err = clips.create({
                data = report,
                content_type = "text/plain",
                name = report_name,
            })
            if not new_clip then
                error("Failed to create EXIF report: " .. (create_err or "unknown"))
            end

            last_clip_id = new_clip.id
            storage.set("last_result_clip_id", tostring(new_clip.id))
        end)

        if not ok then
            errors = errors + 1
            storage.set("last_error", tostring(err))
            log("EXIF Viewer Test error: " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "Failed to extract EXIF data")
    else
        task.complete(task_id)
    end

    local processed = tonumber(storage.get("actions_executed")) or 0
    storage.set("actions_executed", tostring(processed + 1))

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

-- Auto-tag new image clips (simplified: just record that event fired)
function on_clip_created(data)
    if not data or not data.id then
        return
    end

    local clip_info = clips.get(data.id)
    if not clip_info or not clip_info.content_type then
        return
    end

    -- Record clip:created event
    local count = tonumber(storage.get("clips_seen")) or 0
    storage.set("clips_seen", tostring(count + 1))

    if not clip_info.content_type:match("^image/") then
        storage.set("last_clip_is_image", "false")
        return
    end

    storage.set("last_clip_is_image", "true")

    -- Try to read EXIF and auto-tag with camera model
    local ok, meta = pcall(image.metadata, data.id)
    if not ok or not meta then
        storage.set("last_auto_tag", "none")
        return
    end

    if meta.camera_model and meta.camera_model ~= "" then
        local tag_id = find_or_create_tag(meta.camera_model)
        if tag_id then
            tags.add_to_clip(data.id, tag_id)
            storage.set("last_auto_tag", meta.camera_model)
        end
    else
        storage.set("last_auto_tag", "none")
    end
end

storage.set("loaded", "true")
storage.set("actions_executed", "0")
storage.set("clips_seen", "0")
log("EXIF Viewer Test plugin loaded")
