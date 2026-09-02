-- mahresources Upload Plugin
-- Uploads clips to a mahresources instance. The Server URL setting is a url
-- setting: saving it grants the plugin network access (GET, POST) to that
-- host, revocable from the plugin card. No allowlist editing needed.

Plugin = {
    name = "mahresources",
    version = "2.1.0",
    description = "Upload clips to a mahresources instance",
    author = "mahpastes",

    events = {"clip:created"},

    settings = {
        {key = "server_url", type = "url", label = "Server URL",
         grants_network = {"GET", "POST"},
         description = "Full base URL including scheme, e.g. http://localhost:8181. Saving it grants this plugin network access to that host.",
         default = "http://localhost:8181"},
        {key = "api_token", type = "password", label = "API Token",
         description = "Bearer token from Account -> API tokens; leave empty for an instance with auth disabled. Never sent over plain HTTP to a non-loopback host."},
        {key = "owner_id", type = "search", source = "groups", label = "Parent group",
         description = "Type to search groups; the group a resource's owner points at"},
        {key = "auto_upload", type = "checkbox", label = "Auto-upload new clips", default = false},
        {key = "content_filter", type = "select", label = "Content types to upload",
         default = "all",
         options = {"all", "images", "text"}},
    },

    ui = {
        card_actions = {
            {
                id = "upload",
                label = "Upload to mahresources",
                icon = "upload",
                async = true,
                options = {
                    {id = "owner_id", type = "search", source = "groups", label = "Parent group"},
                },
            },
        },
    },
}

-- Normalize the configured server_url into a full base URL (scheme + host,
-- no trailing slash). A missing scheme defaults to http; the token guard below
-- still decides whether the token may travel over it.
local function base_url()
    local raw = storage.get("server_url")
    if not raw or tostring(raw) == "" then raw = "http://localhost:8181" end
    raw = tostring(raw):gsub("%s+$", ""):gsub("/+$", "")
    if not raw:match("^%a[%w+.-]*://") then
        raw = "http://" .. raw
    end
    return raw
end

-- Parse a base URL into (lowercased scheme, hostname). Pattern-matching the
-- raw string is not enough: "http://localhost@evil.example" has hostname
-- "evil.example", and "localhost." and "[::1]" must be handled.
local function parse_base_url(url)
    url = tostring(url or "")
    local scheme, rest = url:match("^(%a[%w+.-]*)://(.*)$")
    if not rest then
        scheme = "http"
        rest = url
    end
    -- Strip anything after the authority.
    local authority = rest:match("^([^/%?#]*)") or ""
    -- Strip userinfo: everything up to the last '@' belongs to it.
    local hostport = authority:match("([^@]*)$") or ""
    local host
    if hostport:sub(1, 1) == "[" then
        host = hostport:match("^%[(.-)%]") -- IPv6 literal, brackets stripped
    else
        host = hostport:match("^([^:]*)")  -- drop :port
    end
    host = (host or ""):lower()
    host = host:gsub("%.+$", "") -- "localhost." is still localhost
    return scheme:lower(), host
end

-- True when host is a complete IPv4 literal inside 127.0.0.0/8. A bare
-- "^127%." prefix match would also accept DNS names like "127.attacker.example".
local function is_loopback_ipv4(host)
    local a, b, c, d = host:match("^(%d+)%.(%d+)%.(%d+)%.(%d+)$")
    if not a then return false end
    for _, octet in ipairs({ a, b, c, d }) do
        if tonumber(octet) > 255 then return false end
    end
    return tonumber(a) == 127
end

-- Treat exactly localhost, 127.0.0.0/8 and ::1 as loopback.
local function is_loopback(host)
    return host == "localhost"
        or is_loopback_ipv4(host)
        or host == "::1"
end

-- Build auth headers for a request to base_url. Returns nil + reason when the
-- token exists but the target is plain HTTP to a non-loopback host: mahpastes
-- only forces https on redirects, so the guard lives here.
local function auth_headers(base)
    local token = storage.get("api_token")
    if not token or tostring(token) == "" then
        return {}
    end
    local scheme, host = parse_base_url(base)
    if scheme == "http" and not is_loopback(host) then
        return nil, "refusing to send your API token over plain HTTP"
    end
    return { ["Authorization"] = "Bearer " .. tostring(token) }
end

-- Strip control characters (notably CR/LF) and escape quotes so a clip
-- filename cannot inject headers into the multipart body.
local function sanitize_filename(name)
    name = tostring(name or "")
    name = name:gsub("%c", "")
    name = name:gsub('"', '\\"')
    if name == "" then name = "clip" end
    return name
end

-- Build a multipart/form-data body. owner_id may be nil, in which case the
-- ownerId field is omitted entirely (mahresources then applies its own
-- scoping rules).
local function build_multipart(filename, content_type, raw_data, owner_id)
    local boundary = "----MahPastesBoundary" .. tostring(utils.time())
    local parts = {}

    -- File field
    parts[#parts + 1] = "--" .. boundary
    parts[#parts + 1] = 'Content-Disposition: form-data; name="resource"; filename="' .. filename .. '"'
    parts[#parts + 1] = "Content-Type: " .. content_type
    parts[#parts + 1] = ""
    parts[#parts + 1] = raw_data

    if owner_id and owner_id ~= "" then
        parts[#parts + 1] = "--" .. boundary
        parts[#parts + 1] = 'Content-Disposition: form-data; name="ownerId"'
        parts[#parts + 1] = ""
        parts[#parts + 1] = owner_id
    end

    -- End boundary
    parts[#parts + 1] = "--" .. boundary .. "--"

    local body = table.concat(parts, "\r\n")
    local header = "multipart/form-data; boundary=" .. boundary
    return body, header
end

-- Check if a clip's content type matches the configured filter. The filter
-- values are the select options: all / images / text.
local function matches_filter(content_type, filter)
    if filter == "all" then return true end
    if filter == "images" then return content_type:match("^image/") ~= nil end
    if filter == "text" then return content_type:match("^text/") ~= nil end
    return true
end

-- Extract the server's error message from a JSON error body like
-- {"error": "...", "details": [...]}.
local function server_error_message(body)
    if not body or body == "" then return nil end
    local ok, decoded = pcall(json.decode, body)
    if ok and type(decoded) == "table" and decoded.error then
        return tostring(decoded.error)
    end
    return nil
end

-- Turns a plugin HTTP error into a user-facing message. A denied host is a
-- permission problem, not a transport failure, and gets its own wording.
local function http_error_message(http_err)
    local err = tostring(http_err or "unknown error")
    local host = err:match("domain not in allowlist: (.+)$")
    if host then
        return "Server '" .. host .. "' is not granted network access yet — re-save the Server URL setting (or use its Grant button) in the plugin settings"
    end
    return "Request failed: " .. err
end

-- Upload a single clip to mahresources. Returns true on success, false + error
-- on failure. owner_override (from the upload dialog) takes precedence over
-- the owner_id setting; nil/empty means "no override".
local function upload_clip(clip_id, silent, owner_override)
    local base = base_url()
    local content_filter = storage.get("content_filter") or "all"

    -- Owner precedence: per-upload option -> setting -> omit the field.
    local owner_id
    if owner_override and tostring(owner_override) ~= "" then
        owner_id = tostring(owner_override)
    else
        local configured = storage.get("owner_id")
        if configured and tostring(configured) ~= "" then
            owner_id = tostring(configured)
        end
    end

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

    -- Auth headers (refuse to leak the token over plain HTTP off-loopback)
    local headers, auth_err = auth_headers(base)
    if headers == nil then
        if not silent then toast.show(auth_err, "error") end
        return false, auth_err
    end

    -- Build multipart body
    local filename = sanitize_filename(clip.filename or ("clip_" .. clip_id))
    local body, content_header = build_multipart(filename, mime_type, raw_data, owner_id)
    headers["Content-Type"] = content_header
    -- Ask for JSON so the handler returns a body we can parse instead of a 303
    -- redirect to the HTML view.
    headers["Accept"] = "application/json"

    -- Upload
    local url = base .. "/v1/resource"
    local resp, http_err = http.post(url, {
        body = body,
        headers = headers,
    })

    if not resp then
        local msg = "Upload failed: " .. http_error_message(http_err)
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if resp.status < 200 or resp.status >= 300 then
        local detail = server_error_message(resp.body)
        local msg
        if resp.status == 401 then
            -- Missing or invalid token.
            msg = "Upload failed: authentication required — set a valid API token in the plugin settings (401)"
        elseif resp.status == 403 then
            -- Valid token, but the role cannot write or the group is outside
            -- the user's scope.
            msg = "Upload failed: your account cannot upload to this group (403)"
        else
            msg = "Upload failed (HTTP " .. resp.status .. ")"
        end
        if detail then msg = msg .. ": " .. detail end
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if not silent then toast.show("Uploaded to mahresources", "success") end
    return true, nil
end

-- Search for entities referenced by the plugin's search fields.
-- source "groups" queries mahresources' group search API. Failures return
-- nil, "message" so the picker shows the reason instead of "No results".
function on_search(source, query)
    if source ~= "groups" then return {} end
    query = tostring(query or "")

    local base = base_url()
    local headers = { ["Accept"] = "application/json" }
    local auth, auth_err = auth_headers(base)
    if auth == nil then
        log("mahresources group search: " .. auth_err)
        return nil, auth_err
    end
    for k, v in pairs(auth) do headers[k] = v end

    local url = base .. "/v1/groups?Name=" .. utils.url_encode(query) .. "&page=1"
    local resp, http_err = http.get(url, { headers = headers })
    if not resp then
        local msg = http_error_message(http_err)
        log("mahresources group search failed: " .. tostring(http_err))
        return nil, msg
    end
    if resp.status < 200 or resp.status >= 300 then
        local detail = server_error_message(resp.body)
        local msg = "Group search failed (HTTP " .. resp.status .. ")"
        if detail then msg = msg .. ": " .. detail end
        log("mahresources group search failed (HTTP " .. resp.status .. ")")
        return nil, msg
    end

    local ok, groups = pcall(json.decode, resp.body or "[]")
    if not ok or type(groups) ~= "table" then
        log("mahresources group search: unexpected response body")
        return nil, "Server returned an unexpected response (expected a JSON group list)"
    end

    local rows = {}
    for _, g in ipairs(groups) do
        local id = g.ID or g.id
        if id ~= nil then
            rows[#rows + 1] = {
                value = tostring(id),
                label = tostring(g.Name or g.name or id),
            }
        end
    end
    return rows
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

-- Manual upload via card action. options.owner_id (from the dialog's picker)
-- overrides the parent-group setting for this upload only.
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "upload" then
        return {success = false, error = "Unknown action: " .. tostring(action_id)}
    end

    local override = nil
    if options and options.owner_id and tostring(options.owner_id) ~= "" then
        override = tostring(options.owner_id)
    end

    local clip_count = #clip_ids
    local task_id = task.start("Upload to mahresources (" .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local success, upload_err = upload_clip(clip_id, true, override)
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
