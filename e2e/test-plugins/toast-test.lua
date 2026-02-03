Plugin = {
  name = "Toast Test",
  version = "1.0.0",
  description = "Tests toast API",
  author = "Test",
  events = {"clip:created"}
}

function on_clip_created(data)
  -- Store that we received the event
  storage.set("last_event", "clip:created")
  storage.set("last_filename", data.filename or "unknown")
  -- Show toast
  local success = toast.show("Clip created: " .. (data.filename or "unknown"), "success")
  storage.set("toast_result", tostring(success))
end
