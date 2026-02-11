-- Utils Extensions No Clipboard Test Plugin
-- Tests that clipboard_write is denied without permission

Plugin = {
    name = "Utils No Clipboard Test",
    version = "1.0.0",
    description = "Test plugin without clipboard permission",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },
    clipboard = false,

    events = {},
    schedules = {},
}

-- Run tests at load time and store results
storage.set("loaded", "true")

-- Test clipboard_write without permission - should return nil, error
local ok, err = utils.clipboard_write("should fail")
storage.set("clipboard_write_ok", tostring(ok))
storage.set("clipboard_write_err", err or "no_error")

log("Utils No Clipboard Test plugin loaded")
