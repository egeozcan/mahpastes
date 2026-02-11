-- Utils Extensions Omitted Clipboard Test Plugin
-- Tests that clipboard_write is denied when clipboard field is omitted entirely

Plugin = {
    name = "Utils Omitted Clipboard Test",
    version = "1.0.0",
    description = "Test plugin with clipboard field omitted from manifest",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },
    -- NOTE: No clipboard field at all! Should default to false.

    events = {},
    schedules = {},
}

-- Run tests at load time and store results
storage.set("loaded", "true")

-- clipboard_write should be denied since clipboard field is omitted (defaults to false)
local ok, err = utils.clipboard_write("should fail - no clipboard field")
storage.set("clipboard_write_ok", tostring(ok))
storage.set("clipboard_write_err", err or "no_error")

log("Utils Omitted Clipboard Test plugin loaded")
