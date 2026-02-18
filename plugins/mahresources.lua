-- mahresources Upload Plugin
-- Uploads clips to a mahresources instance

Plugin = {
    name = "mahresources",
    version = "1.0.0",
    description = "Upload clips to a mahresources instance",
    author = "mahpastes",

    events = {"clip:created"},

    -- EDIT THIS to match your mahresources server domain/IP
    network = {
        ["localhost"] = {"POST"},
    },

    settings = {
        {key = "server_url", type = "text", label = "Server URL", default = "localhost:8181"},
        {key = "owner_id", type = "text", label = "Owner Group ID", default = "1"},
        {key = "auto_upload", type = "checkbox", label = "Auto-upload new clips", default = false},
        {key = "content_filter", type = "select", label = "Content types to upload",
         default = "all",
         choices = {
             {value = "all", label = "All files"},
             {value = "image", label = "Images only"},
             {value = "text", label = "Text only"},
         }},
    },

    ui = {
        card_actions = {
            {
                id = "upload",
                label = "Upload to mahresources",
                icon = "upload",
                async = true,
            },
        },
    },
}

-- Build a multipart/form-data body
local function build_multipart(filename, content_type, raw_data, owner_id)
    local boundary = "----MahPastesBoundary" .. tostring(utils.time())
    local parts = {}

    -- File field
    parts[#parts + 1] = "--" .. boundary
    parts[#parts + 1] = 'Content-Disposition: form-data; name="resource"; filename="' .. filename .. '"'
    parts[#parts + 1] = "Content-Type: " .. content_type
    parts[#parts + 1] = ""
    parts[#parts + 1] = raw_data

    -- Owner ID field
    parts[#parts + 1] = "--" .. boundary
    parts[#parts + 1] = 'Content-Disposition: form-data; name="ownerId"'
    parts[#parts + 1] = ""
    parts[#parts + 1] = owner_id

    -- End boundary
    parts[#parts + 1] = "--" .. boundary .. "--"

    local body = table.concat(parts, "\r\n")
    local header = "multipart/form-data; boundary=" .. boundary
    return body, header
end

-- Check if a clip's content type matches the configured filter
local function matches_filter(content_type, filter)
    if filter == "all" then return true end
    if filter == "image" then return content_type:match("^image/") ~= nil end
    if filter == "text" then return content_type:match("^text/") ~= nil end
    return true
end

-- Upload a single clip to mahresources. Returns true on success, false + error on failure.
local function upload_clip(clip_id, silent)
    local server_url = storage.get("server_url") or "localhost:8181"
    local owner_id = storage.get("owner_id") or "1"
    local content_filter = storage.get("content_filter") or "all"

    -- Get clip metadata
    local clip = clips.get(clip_id)
    if not clip then
        if not silent then toast.show("Clip not found", "error") end
        return false, "Clip not found"
    end

    -- Check content filter
    if not matches_filter(clip.content_type, content_filter) then
        if not silent then toast.show("Skipped (content type filtered)", "info") end
        return false, "filtered"
    end

    -- Get clip data (base64 for binary, plain for text)
    local data, mime_type = clips.get_data(clip_id)
    if not data then
        if not silent then toast.show("Failed to read clip data", "error") end
        return false, "Failed to read clip data"
    end

    -- Decode base64 to raw bytes (binary clips are base64-encoded)
    local raw_data
    if mime_type:match("^text/") then
        raw_data = data
    else
        raw_data = base64.decode(data)
    end

    -- Build multipart body
    local filename = clip.filename or ("clip_" .. clip_id)
    local body, content_header = build_multipart(filename, mime_type, raw_data, owner_id)

    -- Upload
    local url = "http://" .. server_url .. "/v1/resource"
    local resp, http_err = http.post(url, {
        body = body,
        headers = {
            ["Content-Type"] = content_header,
        },
    })

    if not resp then
        local msg = "Upload failed: " .. (http_err or "unknown error")
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if resp.status < 200 or resp.status >= 300 then
        local msg = "Upload failed (HTTP " .. resp.status .. ")"
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if not silent then toast.show("Uploaded to mahresources", "success") end
    return true, nil
end

-- Auto-upload on clip:created (gated by setting)
function on_clip_created(data)
    local auto_upload = storage.get("auto_upload")
    if auto_upload ~= "true" then return end
    local ok, err = pcall(upload_clip, data.id, true)
    if not ok then
        log("mahresources auto-upload failed: " .. tostring(err))
    end
end

-- Manual upload via card action
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "upload" then
        return {success = false, error = "Unknown action: " .. tostring(action_id)}
    end

    local clip_count = #clip_ids
    local task_id = task.start("Upload to mahresources (" .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local success, upload_err = upload_clip(clip_id, true)
            if not success and upload_err ~= "filtered" then
                error(upload_err or "Unknown error")
            end
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("mahresources upload error for clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "All uploads failed"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
        return {success = false, error = msg}
    else
        task.complete(task_id)
        if errors > 0 then
            toast.show(errors .. " of " .. clip_count .. " uploads failed", "error")
        else
            toast.show("Uploaded " .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. " to mahresources", "success")
        end
        return {success = true}
    end
end

log("mahresources plugin loaded")
