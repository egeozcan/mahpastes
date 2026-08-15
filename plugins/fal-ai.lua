-- FAL.AI Image Processing Plugin
-- AI-powered image processing using fal.ai

Plugin = {
    name = "FAL.AI Image Processing",
    version = "2.1.0",
    description = "AI-powered image processing using fal.ai - text-to-image generation, colorization, upscaling, restoration, and AI editing. Results are pulled inline where the model supports it, so nothing lingers on fal's CDN.",
    author = "mahpastes",

    network = {
        ["fal.ai"] = {"GET", "POST"},
        ["fal.run"] = {"GET", "POST"},
        ["*.fal.media"] = {"GET"},
    },

    settings = {
        {key = "api_key", type = "password", label = "FAL.AI API Key", required = true},
        {key = "cdn_retention", type = "select", label = "Keep results on fal's CDN for",
            description = "Most models return the image inline and never touch the CDN. This caps how long the rest survive.",
            default = "5 minutes",
            options = {"5 minutes", "1 hour", "24 hours", "fal account default"}},
        {key = "store_history", type = "checkbox", label = "Keep request history on fal",
            description = "When off, prompts and input images are not retained in fal's 30-day request history.",
            default = false},
        {key = "safety_filter", type = "select", label = "Default content filter",
            description = "Used whenever an action's own Content Filter is left on \"Use plugin setting\". Each model grades content on its own scale, so this one level is translated per model. GPT Image 2 and the upscalers other than Clarity expose no filter setting and always use the provider's own.",
            default = "Off (most permissive)",
            options = {"Off (most permissive)", "Relaxed", "Moderate", "Strict"}},
    },

    ui = {
        lightbox_buttons = {
            {id = "colorize", label = "Colorize", icon = "wand", async = true, file_types = {"image/*"}},
            {id = "upscale", label = "Upscale", icon = "arrows-expand", async = true, file_types = {"image/*"},
                options = {
                    {id = "model", type = "select", label = "Model", default = "clarity",
                        choices = {
                            {value = "clarity", label = "Clarity Upscaler"},
                            {value = "topaz", label = "Topaz"},
                            {value = "seedvr", label = "SeedVR2"},
                            {value = "esrgan", label = "ESRGAN"},
                            {value = "creative", label = "Creative Upscaler"},
                        }
                    },
                    {id = "scale", type = "select", label = "Scale", default = "2",
                        choices = {
                            {value = "2", label = "2x"},
                            {value = "4", label = "4x"},
                        }
                    },
                }
            },
            {id = "restore", label = "Restore", icon = "refresh", async = true, file_types = {"image/*"},
                options = {
                    {id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true},
                    {id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true},
                }
            },
            {id = "edit", label = "AI Edit", icon = "pencil", async = true,
                file_types = {"image/png", "image/jpeg", "image/webp", "image/gif", "image/tiff", "image/bmp"},
                options = {
                    {id = "prompt", type = "text", label = "Edit Prompt", required = true},
                    {id = "model", type = "select", label = "Model", default = "nanobanana2",
                        choices = {
                            {value = "nanobanana2", label = "Nano Banana 2"},
                            {value = "nanobananapro", label = "Nano Banana Pro (Gemini 3 Pro)"},
                            {value = "flux2max", label = "FLUX.2 [max]"},
                            {value = "flux2pro", label = "FLUX.2 [pro]"},
                            {value = "flux2turbo", label = "FLUX.2 [dev] Turbo"},
                            {value = "seedream5", label = "Seedream 5.0 Pro"},
                            {value = "seedream45", label = "Seedream 4.5"},
                            {value = "gptimage2", label = "GPT Image 2"},
                        }
                    },
                    {id = "resolution", type = "select", label = "Resolution", default = "1K",
                        choices = {
                            {value = "0.5K", label = "0.5K"},
                            {value = "1K", label = "1K"},
                            {value = "2K", label = "2K"},
                            {value = "4K", label = "4K"},
                        }
                    },
                    {id = "safety", type = "select", label = "Content Filter", default = "default",
                        choices = {
                            {value = "default", label = "Use plugin setting"},
                            {value = "off", label = "Off (most permissive)"},
                            {value = "relaxed", label = "Relaxed"},
                            {value = "moderate", label = "Moderate"},
                            {value = "strict", label = "Strict"},
                        }
                    },
                }
            },
            {id = "vectorize", label = "Vectorize", icon = "sparkles", async = true, file_types = {"image/*"}},
        },
        card_actions = {
            {id = "colorize", label = "Colorize", icon = "wand", async = true, file_types = {"image/*"}},
            {id = "upscale", label = "Upscale", icon = "arrows-expand", async = true, file_types = {"image/*"},
                options = {
                    {id = "model", type = "select", label = "Model", default = "clarity",
                        choices = {
                            {value = "clarity", label = "Clarity Upscaler"},
                            {value = "topaz", label = "Topaz"},
                            {value = "seedvr", label = "SeedVR2"},
                            {value = "esrgan", label = "ESRGAN"},
                            {value = "creative", label = "Creative Upscaler"},
                        }
                    },
                    {id = "scale", type = "select", label = "Scale", default = "2",
                        choices = {
                            {value = "2", label = "2x"},
                            {value = "4", label = "4x"},
                        }
                    },
                }
            },
            {id = "restore", label = "Restore", icon = "refresh", async = true, file_types = {"image/*"},
                options = {
                    {id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true},
                    {id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true},
                }
            },
        },
        bulk_actions = {
            {id = "edit", label = "AI Edit", icon = "pencil", async = true,
                file_types = {"image/png", "image/jpeg", "image/webp", "image/gif", "image/tiff", "image/bmp"},
                options = {
                    {id = "prompt", type = "text", label = "Edit Prompt", required = true},
                    {id = "model", type = "select", label = "Model", default = "nanobanana2",
                        choices = {
                            {value = "nanobanana2", label = "Nano Banana 2"},
                            {value = "nanobananapro", label = "Nano Banana Pro (Gemini 3 Pro)"},
                            {value = "flux2max", label = "FLUX.2 [max]"},
                            {value = "flux2pro", label = "FLUX.2 [pro]"},
                            {value = "flux2turbo", label = "FLUX.2 [dev] Turbo"},
                            {value = "seedream5", label = "Seedream 5.0 Pro"},
                            {value = "seedream45", label = "Seedream 4.5"},
                            {value = "gptimage2", label = "GPT Image 2"},
                        }
                    },
                    {id = "resolution", type = "select", label = "Resolution", default = "1K",
                        choices = {
                            {value = "0.5K", label = "0.5K"},
                            {value = "1K", label = "1K"},
                            {value = "2K", label = "2K"},
                            {value = "4K", label = "4K"},
                        }
                    },
                    {id = "safety", type = "select", label = "Content Filter", default = "default",
                        choices = {
                            {value = "default", label = "Use plugin setting"},
                            {value = "off", label = "Off (most permissive)"},
                            {value = "relaxed", label = "Relaxed"},
                            {value = "moderate", label = "Moderate"},
                            {value = "strict", label = "Strict"},
                        }
                    },
                }
            },
        },
        global_actions = {
            {id = "generate", label = "Generate Image", icon = "sparkles", async = true,
                options = {
                    {id = "prompt", type = "text", label = "Prompt", required = true},
                    {id = "model", type = "select", label = "Model", default = "nanobanana2",
                        choices = {
                            {value = "nanobanana2", label = "Nano Banana 2"},
                            {value = "nanobananapro", label = "Nano Banana Pro (Gemini 3 Pro)"},
                            {value = "flux2max", label = "FLUX.2 [max]"},
                            {value = "flux2pro", label = "FLUX.2 [pro]"},
                            {value = "flux2turbo", label = "FLUX.2 [dev] Turbo"},
                            {value = "seedream5", label = "Seedream 5.0 Pro"},
                            {value = "seedream45", label = "Seedream 4.5"},
                            {value = "gptimage2", label = "GPT Image 2"},
                            {value = "ideogram4", label = "Ideogram V4"},
                            {value = "recraft4", label = "Recraft V4"},
                            {value = "zimage", label = "Z-Image Turbo"},
                        }
                    },
                    {id = "resolution", type = "select", label = "Resolution", default = "1K",
                        choices = {
                            {value = "0.5K", label = "0.5K"},
                            {value = "1K", label = "1K"},
                            {value = "2K", label = "2K"},
                            {value = "4K", label = "4K"},
                        }
                    },
                    {id = "aspect_ratio", type = "select", label = "Aspect Ratio", default = "1:1",
                        choices = {
                            {value = "1:1", label = "1:1"},
                            {value = "16:9", label = "16:9"},
                            {value = "9:16", label = "9:16"},
                            {value = "4:3", label = "4:3"},
                            {value = "3:4", label = "3:4"},
                            {value = "3:2", label = "3:2"},
                            {value = "2:3", label = "2:3"},
                        }
                    },
                    {id = "safety", type = "select", label = "Content Filter", default = "default",
                        choices = {
                            {value = "default", label = "Use plugin setting"},
                            {value = "off", label = "Off (most permissive)"},
                            {value = "relaxed", label = "Relaxed"},
                            {value = "moderate", label = "Moderate"},
                            {value = "strict", label = "Strict"},
                        }
                    },
                }
            },
        },
    },
}

-- ---------------------------------------------------------------------------
-- Data retention
--
-- fal keeps two things by default: generated media on its CDN (per the account
-- setting, which is "forever" unless configured) and the request's JSON payloads
-- for 30 days. Three mechanisms below keep our results off fal's servers:
--
--   1. sync_mode - the model returns the image inline as a data URI and never
--      uploads it to the CDN. Used for every endpoint whose schema accepts it.
--   2. X-Fal-Object-Lifecycle-Preference - caps CDN retention for the handful
--      of legacy endpoints (DDColor, photo restoration, vectorize, Clarity,
--      ESRGAN, Topaz, Recraft) that have no sync_mode.
--   3. X-Fal-Store-IO: 0 - keeps prompts and input images out of the 30-day
--      request history.
-- ---------------------------------------------------------------------------

local RETENTION_SECONDS = {
    ["5 minutes"] = 300,
    ["1 hour"] = 3600,
    ["24 hours"] = 86400,
    ["fal account default"] = false,
}

local function fal_headers(api_key)
    local headers = {
        Authorization = "Key " .. api_key,
        ["Content-Type"] = "application/json",
    }

    local choice = storage.get("cdn_retention")
    if choice == nil or choice == "" then choice = "5 minutes" end
    local seconds = RETENTION_SECONDS[choice]
    if seconds == nil then seconds = RETENTION_SECONDS["5 minutes"] end
    if seconds then
        headers["X-Fal-Object-Lifecycle-Preference"] =
            json.encode({expiration_duration_seconds = seconds})
    end

    if storage.get("store_history") ~= "true" then
        headers["X-Fal-Store-IO"] = "0"
    end

    return headers
end

-- ---------------------------------------------------------------------------
-- Content filtering
--
-- No two families agree on how to express this. Gemini grades tolerance 1-6,
-- FLUX [pro]/[max] grade it 1-5, and everything else offers a plain on/off
-- checker - in every case a *higher* tolerance means *less* filtering, which
-- inverts the label the user picked. One level maps onto all three so the
-- numbers never leak into the action forms.
--
-- Generate and AI Edit each carry their own Content Filter dropdown; leaving it
-- on "Use plugin setting" defers to the plugin-wide default. Upscale has no
-- dropdown, so Clarity (the only upscaler with the parameter) always follows
-- the setting.
--
-- Endpoints with no filter parameter at all (GPT Image 2, DDColor, photo
-- restoration, vectorize, and every upscaler except Clarity) are unaffected.
-- ---------------------------------------------------------------------------

local SAFETY_LEVELS = {
    off = {gemini = "6", flux = "5", checker = false},
    relaxed = {gemini = "5", flux = "4", checker = false},
    moderate = {gemini = "3", flux = "3", checker = true},
    strict = {gemini = "1", flux = "1", checker = true},
}

-- Plugin settings render a plain string list, so the stored value is the label
-- the user saw rather than a slug.
local SAFETY_SETTING_LEVELS = {
    ["Off (most permissive)"] = "off",
    ["Relaxed"] = "relaxed",
    ["Moderate"] = "moderate",
    ["Strict"] = "strict",
}

local DEFAULT_SAFETY_LEVEL = "off"

-- Resolve the filter for one invocation. The dropdown on the action form wins;
-- "default" (and anything unrecognised) falls through to the plugin setting.
-- Returns the per-family values plus the level's own name, which is what gets
-- recorded on the result.
local function safety(choice)
    if choice and choice ~= "" and choice ~= "default" and SAFETY_LEVELS[choice] then
        return SAFETY_LEVELS[choice], choice
    end
    local level = SAFETY_SETTING_LEVELS[storage.get("safety_filter")] or DEFAULT_SAFETY_LEVEL
    return SAFETY_LEVELS[level], level
end

-- ---------------------------------------------------------------------------
-- Model registry
-- ---------------------------------------------------------------------------

-- Per-model dimension limits. Models disagree on both the accepted range and
-- the required granularity, so each family carries its own.
local FLUX_LIMITS = {min = 512, max = 2048, multiple = 16}
local SEEDREAM5_LIMITS = {min = 1024, max = 2048, multiple = 16}
local SEEDREAM45_LIMITS = {min = 1920, max = 4096, multiple = 16}
-- GPT Image 2 requires multiples of 16 and a total pixel count between 655,360
-- and 8,294,400; a 1024 floor and 3584 ceiling stay inside both ends.
local GPT_IMAGE_LIMITS = {min = 1024, max = 3584, multiple = 16}

local GENERATE_MODELS = {
    nanobanana2 = {
        endpoint = "fal-ai/nano-banana-2",
        family = "gemini",
        resolutions = {["0.5K"] = true, ["1K"] = true, ["2K"] = true, ["4K"] = true},
    },
    nanobananapro = {
        endpoint = "fal-ai/nano-banana-pro",
        family = "gemini",
        resolutions = {["1K"] = true, ["2K"] = true, ["4K"] = true},
    },
    flux2max = {endpoint = "fal-ai/flux-2-max", family = "flux_pro", limits = FLUX_LIMITS},
    flux2pro = {endpoint = "fal-ai/flux-2-pro", family = "flux_pro", limits = FLUX_LIMITS},
    flux2turbo = {endpoint = "fal-ai/flux-2/turbo", family = "flux_dev", limits = FLUX_LIMITS},
    seedream5 = {
        endpoint = "bytedance/seedream/v5/pro/text-to-image",
        family = "seedream",
        limits = SEEDREAM5_LIMITS,
    },
    seedream45 = {
        endpoint = "fal-ai/bytedance/seedream/v4.5/text-to-image",
        family = "seedream45",
        limits = SEEDREAM45_LIMITS,
    },
    gptimage2 = {endpoint = "openai/gpt-image-2", family = "gpt_image", limits = GPT_IMAGE_LIMITS},
    ideogram4 = {endpoint = "ideogram/v4", family = "ideogram", limits = FLUX_LIMITS},
    zimage = {endpoint = "fal-ai/z-image/turbo", family = "z_image", limits = FLUX_LIMITS},
    -- Recraft only accepts its own named sizes and has no sync_mode.
    recraft4 = {endpoint = "fal-ai/recraft/v4/text-to-image", family = "recraft"},
}

local EDIT_MODELS = {
    nanobanana2 = {
        endpoint = "fal-ai/nano-banana-2/edit",
        family = "gemini",
        resolutions = {["0.5K"] = true, ["1K"] = true, ["2K"] = true, ["4K"] = true},
    },
    nanobananapro = {
        endpoint = "fal-ai/nano-banana-pro/edit",
        family = "gemini",
        resolutions = {["1K"] = true, ["2K"] = true, ["4K"] = true},
    },
    flux2max = {endpoint = "fal-ai/flux-2-max/edit", family = "flux_pro"},
    flux2pro = {endpoint = "fal-ai/flux-2-pro/edit", family = "flux_pro"},
    -- FLUX.2 turbo edit defaults to a square canvas, so it needs the source
    -- image's shape passed explicitly to preserve framing.
    flux2turbo = {
        endpoint = "fal-ai/flux-2/turbo/edit",
        family = "flux_dev",
        limits = FLUX_LIMITS,
        size_from_source = true,
    },
    seedream5 = {endpoint = "bytedance/seedream/v5/pro/edit", family = "seedream"},
    seedream45 = {
        endpoint = "fal-ai/bytedance/seedream/v4.5/edit",
        family = "seedream45",
        limits = SEEDREAM45_LIMITS,
        size_from_source = true,
    },
    gptimage2 = {endpoint = "openai/gpt-image-2/edit", family = "gpt_image"},
}

local UPSCALE_ENDPOINTS = {
    clarity = "fal-ai/clarity-upscaler",
    topaz = "fal-ai/topaz/upscale/image",
    seedvr = "fal-ai/seedvr/upscale/image",
    esrgan = "fal-ai/esrgan",
    creative = "fal-ai/creative-upscaler",
}

local COLORIZE_ENDPOINT = "fal-ai/ddcolor"
local RESTORE_ENDPOINT = "fal-ai/image-apps-v2/photo-restoration"
local VECTORIZE_ENDPOINT = "fal-ai/recraft/vectorize"

-- ---------------------------------------------------------------------------
-- Image size helpers
-- ---------------------------------------------------------------------------

local ASPECT_RATIOS = {
    ["1:1"] = {1, 1},
    ["16:9"] = {16, 9},
    ["9:16"] = {9, 16},
    ["4:3"] = {4, 3},
    ["3:4"] = {3, 4},
    ["3:2"] = {3, 2},
    ["2:3"] = {2, 3},
}

local RESOLUTION_LONG_EDGE = {
    ["0.5K"] = 512,
    ["1K"] = 1024,
    ["2K"] = 2048,
    ["4K"] = 4096,
}

-- Closest named fal preset per aspect ratio, for models that reject explicit
-- dimensions.
local ASPECT_PRESETS = {
    ["1:1"] = "square_hd",
    ["16:9"] = "landscape_16_9",
    ["9:16"] = "portrait_16_9",
    ["4:3"] = "landscape_4_3",
    ["3:4"] = "portrait_4_3",
    ["3:2"] = "landscape_4_3",
    ["2:3"] = "portrait_4_3",
}

local function snap(value, multiple)
    local snapped = math.floor(value / multiple + 0.5) * multiple
    if snapped < multiple then snapped = multiple end
    return snapped
end

-- Scale a width/height pair into a model's accepted range and snap it to the
-- required granularity. The aspect ratio is never traded away: models that
-- state a short-side floor also accept a total-pixel budget instead, which a
-- wide image satisfies by filling the long side.
local function fit_dimensions(width, height, limits)
    local longest = math.max(width, height)
    if longest > limits.max then
        local scale = limits.max / longest
        width, height = width * scale, height * scale
    end
    local shortest = math.min(width, height)
    if shortest < limits.min then
        local scale = limits.min / shortest
        if math.max(width, height) * scale > limits.max then
            -- Meeting the floor would blow past the ceiling, so fill the long
            -- side instead and let the short side stay under.
            scale = limits.max / math.max(width, height)
        end
        width, height = width * scale, height * scale
    end

    return {width = snap(width, limits.multiple), height = snap(height, limits.multiple)}
end

local function dimensions_for(aspect, resolution, limits)
    local ratio = ASPECT_RATIOS[aspect] or ASPECT_RATIOS["1:1"]
    local long_edge = RESOLUTION_LONG_EDGE[resolution] or RESOLUTION_LONG_EDGE["1K"]
    local scale = long_edge / math.max(ratio[1], ratio[2])
    return fit_dimensions(ratio[1] * scale, ratio[2] * scale, limits)
end

-- Reuse the source clip's own shape so an edit keeps its framing. Returns nil
-- when the image can't be measured, letting the caller fall back to the model
-- default.
local function dimensions_from_clip(clip_id, limits)
    local info = image.info(clip_id)
    if not info or not info.width or not info.height then return nil end
    if info.width <= 0 or info.height <= 0 then return nil end
    return fit_dimensions(info.width, info.height, limits)
end

-- ---------------------------------------------------------------------------
-- Payload builders
-- ---------------------------------------------------------------------------

local function clamp_resolution(model, resolution)
    if not resolution or resolution == "" then resolution = "1K" end
    if model.resolutions and not model.resolutions[resolution] then
        return "1K"
    end
    return resolution
end

-- Text-to-image. Every family below accepts sync_mode except Recraft.
local function build_generate_payload(model, prompt, resolution, aspect, filter)
    local family = model.family

    if family == "gemini" then
        return {
            prompt = prompt,
            aspect_ratio = aspect,
            resolution = clamp_resolution(model, resolution),
            output_format = "jpeg",
            num_images = 1,
            limit_generations = true,
            safety_tolerance = filter.gemini,
            sync_mode = true,
        }
    end

    if family == "recraft" then
        return {
            prompt = prompt,
            image_size = ASPECT_PRESETS[aspect] or "square_hd",
            enable_safety_checker = filter.checker,
        }
    end

    local size = dimensions_for(aspect, resolution, model.limits)

    if family == "flux_pro" then
        return {
            prompt = prompt,
            image_size = size,
            output_format = "jpeg",
            safety_tolerance = filter.flux,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "flux_dev" then
        return {
            prompt = prompt,
            image_size = size,
            output_format = "jpeg",
            num_images = 1,
            guidance_scale = 2.5,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "seedream" then
        return {
            prompt = prompt,
            image_size = size,
            output_format = "jpeg",
            num_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "seedream45" then
        -- Seedream 4.5 has no output_format; it always returns PNG.
        return {
            prompt = prompt,
            image_size = size,
            num_images = 1,
            max_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "gpt_image" then
        -- GPT Image 2 exposes no filter parameter.
        return {
            prompt = prompt,
            image_size = size,
            quality = "high",
            output_format = "jpeg",
            num_images = 1,
            sync_mode = true,
        }
    elseif family == "ideogram" then
        return {
            prompt = prompt,
            image_size = size,
            output_format = "jpeg",
            num_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "z_image" then
        return {
            prompt = prompt,
            image_size = size,
            output_format = "jpeg",
            num_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    end

    return nil
end

-- Image editing. All selected images go to the model in one call so it can use
-- them together as edit inputs and references.
local function build_edit_payload(model, data_uris, prompt, resolution, source_clip_id, filter)
    local family = model.family

    if family == "gemini" then
        return {
            image_urls = data_uris,
            prompt = prompt,
            aspect_ratio = "auto",
            resolution = clamp_resolution(model, resolution),
            output_format = "jpeg",
            num_images = 1,
            limit_generations = true,
            safety_tolerance = filter.gemini,
            sync_mode = true,
        }
    end

    local size = nil
    if model.size_from_source and source_clip_id then
        size = dimensions_from_clip(source_clip_id, model.limits)
    end

    if family == "flux_pro" then
        return {
            image_urls = data_uris,
            prompt = prompt,
            image_size = "auto",
            output_format = "jpeg",
            safety_tolerance = filter.flux,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "flux_dev" then
        local payload = {
            image_urls = data_uris,
            prompt = prompt,
            output_format = "jpeg",
            num_images = 1,
            guidance_scale = 2.5,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
        if size then payload.image_size = size end
        return payload
    elseif family == "seedream" then
        return {
            image_urls = data_uris,
            prompt = prompt,
            image_size = "auto_2K",
            output_format = "jpeg",
            num_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
    elseif family == "seedream45" then
        local payload = {
            image_urls = data_uris,
            prompt = prompt,
            num_images = 1,
            max_images = 1,
            enable_safety_checker = filter.checker,
            sync_mode = true,
        }
        if size then payload.image_size = size end
        return payload
    elseif family == "gpt_image" then
        -- GPT Image 2 exposes no filter parameter.
        return {
            image_urls = data_uris,
            prompt = prompt,
            image_size = "auto",
            quality = "high",
            output_format = "jpeg",
            num_images = 1,
            sync_mode = true,
        }
    end

    return nil
end

-- Single-image transforms driven from the lightbox and card menus. Returns the
-- endpoint, the payload, and the parameters worth recording on the result.
local function build_request(action_id, data_uri, options)
    if action_id == "colorize" then
        return COLORIZE_ENDPOINT, {image_url = data_uri}, {operation = "colorize"}

    elseif action_id == "upscale" then
        local model = options.model or "clarity"
        local scale = tonumber(options.scale) or 2
        local endpoint = UPSCALE_ENDPOINTS[model] or UPSCALE_ENDPOINTS.clarity

        if model == "esrgan" then
            return endpoint, {
                image_url = data_uri,
                scale = scale,
                model = "RealESRGAN_x4plus",
                output_format = "jpeg",
            }, {operation = "upscale", upscale_factor = scale}
        elseif model == "topaz" then
            return endpoint, {
                image_url = data_uri,
                model = "Standard V2",
                upscale_factor = scale,
                face_enhancement = true,
                output_format = "jpeg",
            }, {operation = "upscale", upscale_factor = scale}
        elseif model == "seedvr" then
            return endpoint, {
                image_url = data_uri,
                upscale_mode = "factor",
                upscale_factor = scale,
                output_format = "jpg",
                sync_mode = true,
            }, {operation = "upscale", upscale_factor = scale}
        elseif model == "creative" then
            -- The creative upscaler takes no scale; it picks its own.
            return endpoint, {image_url = data_uri}, {operation = "upscale"}
        else
            -- Clarity is the only upscaler with a filter parameter.
            local filter, filter_level = safety(nil)
            return endpoint, {
                image_url = data_uri,
                prompt = "masterpiece, best quality, highres",
                negative_prompt = "(worst quality, low quality, normal quality:2)",
                upscale_factor = scale,
                enable_safety_checker = filter.checker,
            }, {operation = "upscale", upscale_factor = scale, safety_filter = filter_level}
        end

    elseif action_id == "restore" then
        local fix_colors = true
        local remove_scratches = true
        if options.fix_colors ~= nil then fix_colors = options.fix_colors end
        if options.remove_scratches ~= nil then remove_scratches = options.remove_scratches end
        return RESTORE_ENDPOINT, {
            image_url = data_uri,
            enhance_resolution = true,
            fix_colors = fix_colors,
            remove_scratches = remove_scratches,
        }, {
            operation = "restore",
            fix_colors = fix_colors,
            remove_scratches = remove_scratches,
        }

    elseif action_id == "vectorize" then
        return VECTORIZE_ENDPOINT, {image_url = data_uri}, {operation = "vectorize"}

    else
        return nil, nil, nil
    end
end

-- ---------------------------------------------------------------------------
-- API plumbing
-- ---------------------------------------------------------------------------

local function call_fal(endpoint, payload, api_key)
    local resp, http_err = http.post(
        "https://fal.run/" .. endpoint,
        {body = json.encode(payload), headers = fal_headers(api_key)}
    )
    if not resp then
        error("HTTP request failed: " .. (http_err or "unknown error"))
    end
    if resp.status ~= 200 then
        error("API error (status " .. resp.status .. "): " .. (resp.body or ""))
    end

    local result = json.decode(resp.body)
    if result.msg and result.msg ~= "" then
        error(result.msg)
    end
    return result
end

-- Extract the result image reference. With sync_mode this is a data URI; the
-- legacy endpoints return a fal CDN URL instead.
local function get_result_url(result)
    if result.image and result.image.url then
        return result.image.url
    end
    if result.images and type(result.images) == "table" then
        if result.images[1] and result.images[1].url then
            return result.images[1].url
        end
    end
    return nil
end

local EXTENSION_BY_MIME = {
    ["image/jpeg"] = "jpg",
    ["image/png"] = "png",
    ["image/webp"] = "webp",
    ["image/svg+xml"] = "svg",
}

local function result_extension(result_url, fallback)
    local mime = result_url:match("^data:([^;]+);base64,")
    if mime then
        return EXTENSION_BY_MIME[mime] or fallback
    end
    local path = result_url:gsub("[?#].*$", "")
    return path:match("%.([%a][%w]*)$") or fallback
end

-- Persist a result. Inline data URIs are decoded straight into a clip, so the
-- image never exists on fal's CDN at all.
local function save_result(result_url, filename)
    local mime, encoded = result_url:match("^data:([^;]+);base64,(.+)$")
    if encoded then
        return clips.create({
            data = encoded,
            content_type = mime,
            filename = filename,
        })
    end
    return clips.create_from_url(result_url, {name = filename})
end

-- Build the output filename from the original name and the result's real format.
local function generate_filename(original, action_id, result_url)
    local name = original:match("^(.+)%.[^%.]+$") or original
    local fallback = original:match("%.([^%.]+)$") or "png"
    local ext = result_extension(result_url, fallback)
    if action_id == "vectorize" then
        ext = "svg"
    end
    return name .. "_" .. action_id .. "." .. ext
end

-- Convert a non-JPEG clip to JPEG, replacing the original clip.
-- Returns the final clip ID (new JPEG clip or original if already JPEG/SVG).
local function ensure_jpeg(clip_id)
    local info = clips.get(clip_id)
    if not info then return clip_id end

    local ct = info.content_type or ""
    -- Skip if already JPEG or if it's an SVG (vectorize output)
    if ct == "image/jpeg" or ct == "image/svg+xml" then
        return clip_id
    end

    local converted, conv_err = image.convert(clip_id, "jpeg")
    if not converted then
        log("JPEG conversion failed: " .. (conv_err or "unknown"))
        return clip_id
    end

    -- Build JPEG filename from original
    local orig_name = info.filename or ("clip_" .. clip_id)
    local base = orig_name:match("^(.+)%.[^%.]+$") or orig_name
    local jpeg_name = base .. ".jpg"

    local new_clip, create_err = clips.create({
        data = converted.data,
        content_type = "image/jpeg",
        filename = jpeg_name,
    })
    if not new_clip then
        log("Failed to create JPEG clip: " .. (create_err or "unknown"))
        return clip_id
    end

    clips.delete(clip_id)
    return new_clip.id
end

-- Place a freshly created clip into the active folder, if the action was invoked
-- from folder view. context.folder_tag_id is absent/0 outside folder mode, so this
-- is a no-op in that case.
local function add_to_folder(clip_id, context)
    if not clip_id or clip_id == 0 then return end
    if not context then return end
    local tag_id = context.folder_tag_id
    if tag_id and tag_id > 0 then
        tags.add_to_clip(tag_id, clip_id)
    end
end

-- fal.ai endpoints reject SVG and other non-raster formats.
local SUPPORTED_INPUT_TYPES = {
    ["image/png"] = true,
    ["image/jpeg"] = true,
    ["image/webp"] = true,
    ["image/gif"] = true,
    ["image/tiff"] = true,
    ["image/bmp"] = true,
}

local function clip_data_uri(clip_id)
    local data, mime_type = clips.get_data(clip_id)
    if not data then
        error("Failed to get clip data for clip " .. clip_id)
    end
    if not SUPPORTED_INPUT_TYPES[mime_type] then
        error("Unsupported image format: " .. (mime_type or "unknown") ..
            ". Only raster images (PNG, JPEG, WebP) are supported.")
    end
    return "data:" .. mime_type .. ";base64," .. data
end

-- ---------------------------------------------------------------------------
-- Generation metadata
--
-- Every clip this plugin creates carries the parameters that produced it, so a
-- result can be traced back to its request long after the task toast is gone.
-- Only what was actually sent is recorded: models that ignore a parameter (the
-- Creative upscaler's scale, GPT Image 2's absent filter) leave its key off
-- rather than claiming a value that had no effect.
--
-- Writing is best-effort. metadata.set reports problems by return value instead
-- of raising, and a rejected key must never fail a generation the user has
-- already paid for.
-- ---------------------------------------------------------------------------

-- The keys this plugin writes. Listing them explicitly keeps a stray field from
-- leaking into a clip's metadata and fixes the write order; the metadata panel
-- sorts alphabetically regardless.
local METADATA_KEYS = {
    "generator",
    "operation",
    "model",
    "prompt",
    "resolution",
    "aspect_ratio",
    "image_size",
    "safety_filter",
    "upscale_factor",
    "fix_colors",
    "remove_scratches",
    "source_clips",
}

-- Matches the backend's per-value cap, which counts characters rather than
-- bytes.
local MAX_METADATA_VALUE = 4096

-- Trim to at most `limit` characters. Counting bytes instead would cut a CJK or
-- emoji prompt to a third of the length the backend actually accepts, and could
-- leave a codepoint split down the middle.
local function trim_to_chars(text, limit)
    -- Byte length is an upper bound on character count.
    if #text <= limit then return text end

    local count = 0
    for pos = 1, #text do
        local byte = text:byte(pos)
        -- Every byte except a UTF-8 continuation (0x80-0xBF) starts a character.
        if byte < 128 or byte > 191 then
            count = count + 1
            if count > limit then return text:sub(1, pos - 1) end
        end
    end
    return text
end

local function metadata_value(value)
    if value == nil then return nil end
    if type(value) == "boolean" then return value and "true" or "false" end
    if type(value) == "number" then
        if value == math.floor(value) then return string.format("%d", value) end
        return tostring(value)
    end

    local text = tostring(value)
    if text == "" then return nil end
    return trim_to_chars(text, MAX_METADATA_VALUE)
end

local function record_generation(clip_id, fields)
    if not clip_id or clip_id == 0 or not fields then return end
    fields.generator = "fal.ai"

    -- Guarded as a whole, not just per key: an older host exposes no metadata
    -- API at all, and one that raises instead of returning an error would
    -- otherwise abort an action whose image is already generated and saved.
    local ok, err = pcall(function()
        for _, key in ipairs(METADATA_KEYS) do
            local value = metadata_value(fields[key])
            if value then
                local set, set_err = metadata.set(clip_id, key, value)
                if not set then
                    log("Failed to record " .. key .. " on clip " .. clip_id .. ": " ..
                        tostring(set_err or "unknown error"))
                end
            end
        end
    end)

    if not ok then
        log("Failed to record generation metadata on clip " .. clip_id .. ": " .. tostring(err))
    end
end

-- image_size is a {width, height} table for models that take explicit
-- dimensions and a named preset ("square_hd", "auto") for the rest.
local function describe_size(size)
    if type(size) == "table" and size.width and size.height then
        return math.floor(size.width) .. "x" .. math.floor(size.height)
    elseif type(size) == "string" then
        return size
    end
    return nil
end

-- Report the resolved filter level only for payloads that actually carry a
-- filter parameter.
local function describe_safety(payload, level)
    if payload.safety_tolerance ~= nil or payload.enable_safety_checker ~= nil then
        return level
    end
    return nil
end

-- ---------------------------------------------------------------------------
-- Actions
-- ---------------------------------------------------------------------------

local ACTION_NAMES = {
    colorize = "Colorize",
    upscale = "Upscale",
    restore = "Restore",
    edit = "AI Edit",
    vectorize = "Vectorize",
    generate = "Generate Image",
}

local function run_generate(options, context, api_key)
    local prompt = options.prompt or ""
    if prompt == "" then
        toast.show("Please enter a prompt.", "error")
        return {success = false, error = "Prompt is required"}
    end

    local model = GENERATE_MODELS[options.model or "nanobanana2"] or GENERATE_MODELS.nanobanana2
    local task_id = task.start("Generate Image", 1)
    local new_clip_id = nil

    local ok, err = pcall(function()
        local aspect = options.aspect_ratio or "1:1"
        local filter, filter_level = safety(options.safety)
        local payload = build_generate_payload(
            model, prompt, options.resolution, aspect, filter)
        if not payload then
            error("Unsupported model: " .. tostring(options.model))
        end

        local result = call_fal(model.endpoint, payload, api_key)
        local result_url = get_result_url(result)
        if not result_url then
            error("No image URL in API response")
        end

        local safe_prompt = prompt:gsub("[^%w%s_-]", ""):gsub("%s+", "_"):sub(1, 40)
        local filename = "generated_" .. safe_prompt .. "." .. result_extension(result_url, "jpg")

        local new_clip, create_err = save_result(result_url, filename)
        if not new_clip then
            error("Failed to save result: " .. (create_err or "unknown error"))
        end

        new_clip_id = ensure_jpeg(new_clip.id)
        record_generation(new_clip_id, {
            operation = "generate",
            model = model.endpoint,
            prompt = prompt,
            resolution = payload.resolution,
            aspect_ratio = aspect,
            image_size = describe_size(payload.image_size),
            safety_filter = describe_safety(payload, filter_level),
        })
        add_to_folder(new_clip_id, context)
    end)

    if not ok then
        task.fail(task_id, tostring(err))
        return {success = false, error = tostring(err)}
    end

    task.progress(task_id, 1)
    task.complete(task_id)
    return {success = true, result_clip_id = new_clip_id or 0}
end

-- A bulk AI edit is one model invocation with all selected images. The result
-- is a single new clip informed by the complete selection.
local function run_multi_edit(clip_ids, options, context, api_key)
    local prompt = options.prompt or ""
    if prompt == "" then
        toast.show("Please enter an edit prompt.", "error")
        return {success = false, error = "Prompt is required"}
    end

    local model = EDIT_MODELS[options.model or "nanobanana2"] or EDIT_MODELS.nanobanana2
    local task_id = task.start("AI Edit (" .. #clip_ids .. " images)", 1)
    local result_clip_id = nil

    local ok, err = pcall(function()
        local data_uris = {}
        for _, clip_id in ipairs(clip_ids) do
            table.insert(data_uris, clip_data_uri(clip_id))
        end

        local filter, filter_level = safety(options.safety)
        local payload = build_edit_payload(
            model, data_uris, prompt, options.resolution, clip_ids[1], filter)
        if not payload then
            error("Unsupported model: " .. tostring(options.model))
        end

        local result = call_fal(model.endpoint, payload, api_key)
        local result_url = get_result_url(result)
        if not result_url then
            error("No image URL in API response")
        end

        local first_info = clips.get(clip_ids[1])
        local original_name = (first_info and first_info.filename) or "multi_image.png"
        local new_clip, create_err = save_result(
            result_url, generate_filename(original_name, "edit", result_url))
        if not new_clip then
            error("Failed to save result: " .. (create_err or "unknown error"))
        end

        result_clip_id = ensure_jpeg(new_clip.id)
        record_generation(result_clip_id, {
            operation = "edit",
            model = model.endpoint,
            prompt = prompt,
            resolution = payload.resolution,
            image_size = describe_size(payload.image_size),
            safety_filter = describe_safety(payload, filter_level),
            source_clips = table.concat(clip_ids, ", "),
        })
        add_to_folder(result_clip_id, context)
    end)

    if not ok then
        task.fail(task_id, tostring(err))
        return {success = false, error = tostring(err)}
    end

    task.progress(task_id, 1)
    task.complete(task_id)
    return {success = true, result_clip_id = result_clip_id or 0}
end

-- Handle UI action from lightbox, card menu, or global action.
-- context carries the active folder's tag when invoked from folder view.
function on_ui_action(action_id, clip_ids, options, context)
    local api_key = storage.get("api_key")
    if not api_key or api_key == "" then
        toast.show("FAL.AI API key not configured. Please set it in plugin settings.", "error")
        return {success = false, error = "API key not configured"}
    end

    options = options or {}

    if action_id == "generate" then
        return run_generate(options, context, api_key)
    end

    if action_id == "edit" and #clip_ids > 1 then
        return run_multi_edit(clip_ids, options, context, api_key)
    end

    local action_name = ACTION_NAMES[action_id] or action_id
    local clip_count = #clip_ids
    local task_id = task.start(
        action_name .. " (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")",
        clip_count)

    local last_clip_id = nil
    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local data_uri = clip_data_uri(clip_id)

            local endpoint, payload, fields
            if action_id == "edit" then
                local prompt = options.prompt or ""
                if prompt == "" then
                    error("Prompt is required")
                end
                local model = EDIT_MODELS[options.model or "nanobanana2"] or EDIT_MODELS.nanobanana2
                endpoint = model.endpoint
                local filter, filter_level = safety(options.safety)
                payload = build_edit_payload(
                    model, {data_uri}, prompt, options.resolution, clip_id, filter)
                if not payload then
                    error("Unsupported model: " .. tostring(options.model))
                end
                fields = {
                    operation = "edit",
                    prompt = prompt,
                    resolution = payload.resolution,
                    image_size = describe_size(payload.image_size),
                    safety_filter = describe_safety(payload, filter_level),
                }
            else
                endpoint, payload, fields = build_request(action_id, data_uri, options)
                if not endpoint then
                    error("Unknown action: " .. action_id)
                end
            end

            local result = call_fal(endpoint, payload, api_key)
            local result_url = get_result_url(result)
            if not result_url then
                error("No image URL in API response")
            end

            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id .. ".png")

            local new_clip, create_err = save_result(
                result_url, generate_filename(original_name, action_id, result_url))
            if not new_clip then
                error("Failed to save result: " .. (create_err or "unknown error"))
            end

            -- Convert to JPEG to reduce file size (some endpoints return large PNGs).
            -- ensure_jpeg handles filename renaming to .jpg
            last_clip_id = ensure_jpeg(new_clip.id)
            fields.model = endpoint
            fields.source_clips = tostring(clip_id)
            record_generation(last_clip_id, fields)
            add_to_folder(last_clip_id, context)
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("FAL.AI error processing clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "All images failed to process"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
    else
        task.complete(task_id)
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

log("FAL.AI Image Processing plugin loaded")
