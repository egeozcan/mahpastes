-- URL grant example
-- Demonstrates the `url` setting type: the user types their server's URL,
-- and saving it grants the plugin network access (the declared methods) to
-- that host. The grant is revocable from the plugin's card, and retargeting
-- the URL moves it.

Plugin = {
    name = "URL Grant Example",
    version = "1.0.0",
    description = "Shows how a url setting grants network access to a user-chosen host",
    author = "mahpastes",

    -- No `network` table: hosts come from the user's url setting, not the
    -- manifest. The plugin ships hostless.
    settings = {
        {key = "server_url", type = "url", label = "Server URL",
         grants_network = {"GET", "POST"},
         description = "Saving this grants the plugin GET and POST access to the host. Revoke from the plugin card.",
         default = "http://localhost:8080"},
    },

    ui = {
        global_actions = {
            {
                id = "ping",
                label = "Ping Server",
            },
        },
    },
}

-- Read the url setting like any other: storage.get works, only storage.set
-- is refused for url-typed keys.
local function base_url()
    local raw = storage.get("server_url")
    if not raw or tostring(raw) == "" then raw = "http://localhost:8080" end
    raw = tostring(raw):gsub("/+$", "")
    if not raw:match("^%a[%w+.-]*://") then
        raw = "http://" .. raw
    end
    return raw
end

function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "ping" then
        return {success = false, error = "Unknown action: " .. tostring(action_id)}
    end

    local resp, err = http.get(base_url() .. "/health")
    if not resp then
        -- A denied host surfaces "domain not in allowlist: <host>" — a
        -- permission problem, so say so instead of a generic failure.
        if tostring(err):find("not in allowlist") then
            return {success = false, error = "Host not granted network access — re-save the Server URL setting"}
        end
        return {success = false, error = "Request failed: " .. tostring(err)}
    end

    return {
        success = true,
        modal = {
            title = "Server status",
            content = "HTTP " .. tostring(resp.status) .. " from " .. base_url(),
            format = "text",
        },
    }
end

log("url grant example plugin loaded")
