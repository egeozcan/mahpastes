-- EXIF / Metadata Viewer Plugin
-- Extracts and displays image metadata, auto-tags with camera model on import

Plugin = {
    name = "EXIF/Metadata Viewer",
    version = "1.0.0",
    description = "View EXIF and image metadata. Auto-tags images with camera model when imported.",
    author = "mahpastes",

    events = {"clip:created"},

    ui = {
        lightbox_buttons = {
            {id = "view_exif", label = "View EXIF", icon = "info", async = true, file_types = {"image/*"}},
        },
        card_actions = {
            {id = "view_exif", label = "View EXIF", icon = "info", async = true, file_types = {"image/*"}},
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
        log("EXIF Viewer: failed to create tag '" .. name .. "': " .. (err or "unknown"))
        return nil
    end
    return new_tag.id
end

-- Format EXIF metadata into a readable plain text report (used for copy/paste)
local function format_metadata_plain(info, meta, clip_info)
    local lines = {}
    table.insert(lines, "=== Image Metadata ===")
    table.insert(lines, "")

    -- File info
    if clip_info and clip_info.filename then
        table.insert(lines, "File: " .. clip_info.filename)
    end

    -- Basic image info from image.info()
    if info then
        if info.width and info.height then
            table.insert(lines, "Dimensions: " .. info.width .. " x " .. info.height)
        end
        if info.format then
            table.insert(lines, "Format: " .. info.format)
        end
        if info.size then
            local size_kb = math.floor(info.size / 1024)
            if size_kb > 1024 then
                table.insert(lines, "Size: " .. string.format("%.1f", size_kb / 1024) .. " MB")
            else
                table.insert(lines, "Size: " .. size_kb .. " KB")
            end
        end
    end

    -- EXIF data from image.metadata()
    if meta then
        local has_exif = false

        if meta.camera_make and meta.camera_make ~= "" then
            has_exif = true
            table.insert(lines, "")
            table.insert(lines, "--- Camera ---")
            table.insert(lines, "Make: " .. meta.camera_make)
        end
        if meta.camera_model and meta.camera_model ~= "" then
            has_exif = true
            table.insert(lines, "Model: " .. meta.camera_model)
        end
        if meta.lens and meta.lens ~= "" then
            has_exif = true
            table.insert(lines, "Lens: " .. meta.lens)
        end

        local has_settings = false
        if meta.iso then
            has_settings = true
            if not has_exif then
                table.insert(lines, "")
                table.insert(lines, "--- Camera ---")
                has_exif = true
            end
            table.insert(lines, "")
            table.insert(lines, "--- Settings ---")
            table.insert(lines, "ISO: " .. tostring(meta.iso))
        end
        if meta.aperture then
            if not has_settings then
                table.insert(lines, "")
                table.insert(lines, "--- Settings ---")
                has_settings = true
            end
            table.insert(lines, "Aperture: " .. tostring(meta.aperture))
        end
        if meta.shutter_speed then
            if not has_settings then
                table.insert(lines, "")
                table.insert(lines, "--- Settings ---")
                has_settings = true
            end
            table.insert(lines, "Shutter: " .. tostring(meta.shutter_speed))
        end
        if meta.focal_length then
            if not has_settings then
                table.insert(lines, "")
                table.insert(lines, "--- Settings ---")
                has_settings = true
            end
            table.insert(lines, "Focal Length: " .. tostring(meta.focal_length))
        end

        if meta.date and meta.date ~= "" then
            table.insert(lines, "")
            table.insert(lines, "--- Date ---")
            table.insert(lines, "Taken: " .. meta.date)
        end

        if meta.gps and type(meta.gps) == "table" then
            table.insert(lines, "")
            table.insert(lines, "--- Location ---")
            table.insert(lines, "Latitude: " .. tostring(meta.gps.latitude))
            table.insert(lines, "Longitude: " .. tostring(meta.gps.longitude))
        end

        if not has_exif then
            table.insert(lines, "")
            table.insert(lines, "No EXIF data found.")
        end
    else
        table.insert(lines, "")
        table.insert(lines, "No EXIF data found.")
    end

    return table.concat(lines, "\n")
end

-- Format EXIF metadata as markdown with tables (used for modal display)
local function format_metadata_markdown(info, meta, clip_info)
    local lines = {}

    -- File info table
    table.insert(lines, "## File Info")
    table.insert(lines, "| Field | Value |")
    table.insert(lines, "|-------|-------|")
    if clip_info and clip_info.filename then
        table.insert(lines, "| File | " .. clip_info.filename .. " |")
    end
    if info then
        if info.width and info.height then
            table.insert(lines, "| Dimensions | " .. info.width .. " x " .. info.height .. " |")
        end
        if info.format then
            table.insert(lines, "| Format | " .. info.format .. " |")
        end
        if info.size then
            local size_kb = math.floor(info.size / 1024)
            if size_kb > 1024 then
                table.insert(lines, "| Size | " .. string.format("%.1f", size_kb / 1024) .. " MB |")
            else
                table.insert(lines, "| Size | " .. size_kb .. " KB |")
            end
        end
    end

    if not meta then
        table.insert(lines, "")
        table.insert(lines, "*No EXIF data found.*")
        return table.concat(lines, "\n")
    end

    local has_exif = false

    -- Camera table
    local has_camera = (meta.camera_make and meta.camera_make ~= "") or
                       (meta.camera_model and meta.camera_model ~= "") or
                       (meta.lens and meta.lens ~= "")
    if has_camera then
        has_exif = true
        table.insert(lines, "")
        table.insert(lines, "## Camera")
        table.insert(lines, "| Field | Value |")
        table.insert(lines, "|-------|-------|")
        if meta.camera_make and meta.camera_make ~= "" then
            table.insert(lines, "| Make | " .. meta.camera_make .. " |")
        end
        if meta.camera_model and meta.camera_model ~= "" then
            table.insert(lines, "| Model | " .. meta.camera_model .. " |")
        end
        if meta.lens and meta.lens ~= "" then
            table.insert(lines, "| Lens | " .. meta.lens .. " |")
        end
    end

    -- Settings table
    local has_settings = meta.iso or meta.aperture or meta.shutter_speed or meta.focal_length
    if has_settings then
        has_exif = true
        table.insert(lines, "")
        table.insert(lines, "## Settings")
        table.insert(lines, "| Field | Value |")
        table.insert(lines, "|-------|-------|")
        if meta.iso then
            table.insert(lines, "| ISO | " .. tostring(meta.iso) .. " |")
        end
        if meta.aperture then
            table.insert(lines, "| Aperture | " .. tostring(meta.aperture) .. " |")
        end
        if meta.shutter_speed then
            table.insert(lines, "| Shutter | " .. tostring(meta.shutter_speed) .. " |")
        end
        if meta.focal_length then
            table.insert(lines, "| Focal Length | " .. tostring(meta.focal_length) .. " |")
        end
    end

    -- Date section
    if meta.date and meta.date ~= "" then
        has_exif = true
        table.insert(lines, "")
        table.insert(lines, "## Date")
        table.insert(lines, "**" .. meta.date .. "**")
    end

    -- Location table
    if meta.gps and type(meta.gps) == "table" then
        has_exif = true
        table.insert(lines, "")
        table.insert(lines, "## Location")
        table.insert(lines, "| Field | Value |")
        table.insert(lines, "|-------|-------|")
        table.insert(lines, "| Latitude | " .. tostring(meta.gps.latitude) .. " |")
        table.insert(lines, "| Longitude | " .. tostring(meta.gps.longitude) .. " |")
    end

    if not has_exif then
        table.insert(lines, "")
        table.insert(lines, "*No EXIF data found.*")
    end

    return table.concat(lines, "\n")
end

-- Handle UI action from lightbox or card menu
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "view_exif" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    -- Single clip: show modal
    if #clip_ids == 1 then
        local clip_id = clip_ids[1]
        local clip_info = clips.get(clip_id)
        local info = image.info(clip_id)
        local meta = image.metadata(clip_id)

        local md = format_metadata_markdown(info, meta, clip_info)
        local plain = format_metadata_plain(info, meta, clip_info)

        local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
        local name = original_name:match("^(.+)%.[^%.]+$") or original_name

        return {
            success = true,
            modal = {
                title = "Image Metadata",
                content = md,
                format = "markdown",
                copy_data = plain,
                paste_data = plain,
                paste_name = name .. "_exif.txt",
                paste_content_type = "text/plain",
            },
        }
    end

    -- Multiple clips: batch create text clips
    local clip_count = #clip_ids
    local task_id = task.start("View EXIF (" .. clip_count .. " images)", clip_count)

    local last_clip_id = nil
    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local clip_info = clips.get(clip_id)
            local info = image.info(clip_id)
            local meta = image.metadata(clip_id)
            local report = format_metadata_plain(info, meta, clip_info)

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
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("EXIF Viewer error on clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "Failed to extract EXIF data"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
    else
        task.complete(task_id)
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

-- Auto-tag new image clips with camera model if EXIF exists
function on_clip_created(data)
    if not data or not data.id then
        return
    end

    -- Only process image clips
    local clip_info = clips.get(data.id)
    if not clip_info or not clip_info.content_type then
        return
    end
    if not clip_info.content_type:match("^image/") then
        return
    end

    -- Try to read EXIF metadata
    local ok, meta = pcall(image.metadata, data.id)
    if not ok or not meta then
        return
    end

    -- Auto-tag with camera model if present
    if meta.camera_model and meta.camera_model ~= "" then
        local tag_id = find_or_create_tag(meta.camera_model)
        if tag_id then
            tags.add_to_clip(tag_id, data.id)
        end
    end
end

log("EXIF/Metadata Viewer plugin loaded")
