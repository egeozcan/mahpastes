-- Utils Extensions Edge Cases Test Plugin
-- Tests HMAC edge cases, unicode URL encode/decode, longer sha256

Plugin = {
    name = "Utils Edge Cases Test",
    version = "1.0.0",
    description = "Edge case tests for utils API extensions",
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

-- HMAC with empty key
local hmac_empty_key = utils.hmac_sha256("", "message")
storage.set("hmac_empty_key", hmac_empty_key)

-- HMAC with empty data
local hmac_empty_data = utils.hmac_sha256("key", "")
storage.set("hmac_empty_data", hmac_empty_data)

-- URL encode with unicode characters
local unicode_encoded = utils.url_encode("héllo wörld")
storage.set("url_encode_unicode", unicode_encoded)

-- URL decode roundtrip with unicode
local unicode_decoded = utils.url_decode(unicode_encoded)
storage.set("url_decode_unicode", unicode_decoded)

-- URL encode with special URL characters
local special_encoded = utils.url_encode("/?=&#")
storage.set("url_encode_special", special_encoded)

-- URL decode roundtrip with special chars
local special_decoded = utils.url_decode(special_encoded)
storage.set("url_decode_special", special_decoded)

-- sha256 of longer multi-line string (well-known pangram)
local long_str = "The quick brown fox jumps over the lazy dog"
local sha256_long = utils.sha256(long_str)
storage.set("sha256_long", sha256_long)

log("Utils Edge Cases Test plugin loaded")
