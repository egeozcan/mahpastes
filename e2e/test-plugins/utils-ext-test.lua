-- Utils Extensions Test Plugin
-- Tests sha256, hmac_sha256, url_encode, clipboard_write

Plugin = {
    name = "Utils Ext Test",
    version = "1.0.0",
    description = "Test plugin for utils API extensions",
    author = "e2e-tests",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },
    clipboard = true,

    events = {},
    schedules = {},
}

-- Run tests at load time and store results
storage.set("loaded", "true")

-- Test sha256
local sha256_result = utils.sha256("hello world")
storage.set("sha256_result", sha256_result)

-- Test hmac_sha256
local hmac_result = utils.hmac_sha256("secret", "message")
storage.set("hmac_sha256_result", hmac_result)

-- Test url_encode
local url_result = utils.url_encode("hello world&foo=bar")
storage.set("url_encode_result", url_result)

-- Test url_decode (roundtrip with url_encode result)
local decoded = utils.url_decode("hello+world%26foo%3Dbar")
storage.set("url_decode_result", decoded)

-- Test url_decode error case
local bad_decoded, decode_err = utils.url_decode("%ZZ")
storage.set("url_decode_error", decode_err or "no_error")

-- Test clipboard_write
local clip_ok = utils.clipboard_write("test clipboard text")
storage.set("clipboard_write_result", tostring(clip_ok))

-- Edge cases: empty strings
storage.set("sha256_empty", utils.sha256(""))
storage.set("url_encode_empty", utils.url_encode(""))
storage.set("url_decode_empty", utils.url_decode(""))

log("Utils Ext Test plugin loaded and all tests executed")
