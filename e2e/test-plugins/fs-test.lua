-- Filesystem Test Plugin
-- Tests filesystem API with read/write permissions

Plugin = {
    name = "FS Test",
    version = "1.0.0",
    description = "Test plugin for filesystem API verification",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = true,
        write = true,
    },

    events = {
        "clip:created",
    },

    schedules = {},
}

-- Initialize
storage.set("fs_test_initialized", "true")
storage.set("read_attempts", "0")
storage.set("write_attempts", "0")
storage.set("last_read_result", "")
storage.set("last_write_result", "")
storage.set("last_error", "")
log("FS Test plugin loaded")

-- Helper to record results
local function record_read(success, result, err)
    local attempts = tonumber(storage.get("read_attempts")) or 0
    storage.set("read_attempts", tostring(attempts + 1))

    if success then
        storage.set("last_read_result", result or "")
        storage.set("last_error", "")
    else
        storage.set("last_read_result", "")
        storage.set("last_error", err or "unknown error")
    end
end

local function record_write(success, err)
    local attempts = tonumber(storage.get("write_attempts")) or 0
    storage.set("write_attempts", tostring(attempts + 1))

    if success then
        storage.set("last_write_result", "success")
        storage.set("last_error", "")
    else
        storage.set("last_write_result", "failed")
        storage.set("last_error", err or "unknown error")
    end
end

function on_clip_created(data)
    -- When a clip is created, try to write a log file
    -- The test will set up permissions before uploading
    local test_path = storage.get("test_write_path")
    if test_path and test_path ~= "" then
        local ok, err = fs.write(test_path, "test content: " .. tostring(utils.time()))
        record_write(ok == true, err)
    end

    -- Try to read if path is set
    local read_path = storage.get("test_read_path")
    if read_path and read_path ~= "" then
        local content, err = fs.read(read_path)
        record_read(content ~= nil, content, err)
    end
end

-- Exposed for direct testing via storage commands
-- Test can set test_write_path, test_read_path via storage before triggering events
