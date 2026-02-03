-- Toast Spam Test Plugin
-- Tests toast rate limiting by sending 10 toasts rapidly

Plugin = {
  name = "Toast Spam Test",
  version = "1.0.0",
  description = "Tests toast rate limiting",
  author = "e2e-tests",
  events = {"clip:created"}
}

function on_clip_created(data)
  local success_count = 0
  for i = 1, 10 do
    local result = toast.show("Toast " .. i, "info")
    if result then
      success_count = success_count + 1
    end
  end
  -- Store the count so we can verify it from the test
  storage.set("toast_success_count", tostring(success_count))
  log("Toast spam test: " .. success_count .. " of 10 toasts succeeded")
end
