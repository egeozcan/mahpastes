-- URL grant test plugin
-- Exercises the `url` setting grant model: saving the server_url setting
-- grants network access to its host; storage.set on the url key must fail;
-- a denied-host error passes through on_search as a plugin-supplied
-- message. Driven by e2e/tests/plugins/url-grant.spec.ts.

Plugin = {
    name = "URL Grant Test",
    version = "1.0.0",
    description = "Exercises the url setting grant model",
    author = "mahpastes",

    -- Hostless: hosts come from the url setting of the user, not the manifest.
    settings = {
        {key = "server_url", type = "url", label = "Server URL",
         grants_network = {"GET", "POST"},
         description = "Saving grants GET and POST to the host.",
         default = ""},
        -- Probes the picker drives: "fetch" issues a GET to the query URL;
        -- "write" attempts storage.set on the url key with the query value.
        {key = "probe", type = "search", source = "fetch", label = "Probe URL"},
        {key = "write_probe", type = "search", source = "write", label = "Write probe"},
    },
}

function on_search(source, query)
    query = tostring(query or "")

    if source == "fetch" then
        local resp, err = http.get(query)
        if not resp then
            return nil, tostring(err)
        end
        return { {value = tostring(resp.status), label = "HTTP " .. tostring(resp.status)} }
    end

    if source == "write" then
        local ok, err = storage.set("server_url", query)
        if ok then
            return nil, "WRITE-ALLOWED (grant model broken)"
        end
        return nil, "WRITE-BLOCKED: " .. tostring(err)
    end

    return {}
end

log("url grant test plugin loaded")
