-- Settings Test Plugin
-- Tests settings API in e2e tests

Plugin = {
  name = "Settings Test",
  version = "1.0.0",
  description = "Tests settings API",
  author = "Test",
  events = {"app:startup"},
  settings = {
    {key = "api_key", type = "password", label = "API Key", description = "Your API key"},
    {key = "endpoint", type = "text", label = "Endpoint URL", default = "https://api.example.com"},
    {key = "enabled", type = "checkbox", label = "Enable feature", default = true},
    {key = "mode", type = "select", label = "Mode", options = {"fast", "balanced", "thorough"}, default = "balanced"}
  }
}

function on_startup()
  -- Read settings and store to indicate they're accessible
  local api_key = storage.get("api_key")
  local endpoint = storage.get("endpoint")
  local enabled = storage.get("enabled")
  local mode = storage.get("mode")

  storage.set("settings_read", "true")
  storage.set("api_key_value", api_key or "nil")
  storage.set("endpoint_value", endpoint or "nil")
  storage.set("enabled_value", enabled or "nil")
  storage.set("mode_value", mode or "nil")
end
