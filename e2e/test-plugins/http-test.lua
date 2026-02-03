-- HTTP API Test Plugin
-- Tests HTTP API availability and error handling

Plugin = {
    name = "HTTP Test",
    version = "1.0.0",
    description = "Test plugin for HTTP API verification",
    author = "e2e-tests",

    -- Request permission for httpbin.org for testing
    network = {
        ["httpbin.org"] = {"GET", "POST"},
    },
    filesystem = {
        read = false,
        write = false,
    },

    events = {
        "clip:created",
    },

    schedules = {},
}

-- Initialize
storage.set("http_test_initialized", "true")
storage.set("http_get_available", "false")
storage.set("http_post_available", "false")
storage.set("last_http_status", "")
storage.set("last_http_error", "")
storage.set("unauthorized_domain_error", "")
log("HTTP Test plugin loaded")

-- Check API availability at load
if http and http.get then
    storage.set("http_get_available", "true")
end
if http and http.post then
    storage.set("http_post_available", "true")
end

function on_clip_created(data)
    -- Test 1: Try to access unauthorized domain
    local resp, err = http.get("https://example.com/test")
    if err then
        storage.set("unauthorized_domain_error", err)
    else
        storage.set("unauthorized_domain_error", "no_error_unexpected")
    end

    -- Test 2: Try valid domain (httpbin.org) - this would need actual network
    -- For testing, we just verify the API exists and returns proper error format
    local test_url = storage.get("test_http_url")
    if test_url and test_url ~= "" then
        local resp2, err2 = http.get(test_url)
        if resp2 then
            storage.set("last_http_status", tostring(resp2.status))
            storage.set("last_http_error", "")
        else
            storage.set("last_http_status", "")
            storage.set("last_http_error", err2 or "unknown")
        end
    end
end
