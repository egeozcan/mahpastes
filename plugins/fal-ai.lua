-- FAL.AI Image Processing Plugin
-- AI-powered image processing using fal.ai

Plugin = {
    name = "FAL.AI Image Processing",
    version = "1.0.0",
    description = "AI-powered image processing using fal.ai - colorization, upscaling, restoration, and AI editing.",
    author = "mahpastes",

    network = {
        ["fal.ai"] = {"GET", "POST"},
        ["fal.run"] = {"GET", "POST"},
        ["fal.media"] = {"GET"},
        ["v3.fal.media"] = {"GET"},
    },

    settings = {
        {key = "api_key", type = "password", label = "FAL.AI API Key", required = true},
    },

    ui = {
        lightbox_buttons = {
            {id = "colorize", label = "Colorize", icon = "wand", async = true},
            {id = "upscale", label = "Upscale", icon = "arrows-expand", async = true,
                options = {
                    {id = "model", type = "select", label = "Model", default = "clarity",
                        choices = {
                            {value = "clarity", label = "Clarity Upscaler"},
                            {value = "esrgan", label = "ESRGAN"},
                            {value = "creative", label = "Creative Upscaler"},
                        }
                    },
                }
            },
            {id = "restore", label = "Restore", icon = "refresh", async = true,
                options = {
                    {id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true},
                    {id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true},
                }
            },
            {id = "edit", label = "AI Edit", icon = "pencil", async = true,
                options = {
                    {id = "prompt", type = "text", label = "Edit Prompt", required = true},
                    {id = "model", type = "select", label = "Model", default = "flux2",
                        choices = {
                            {value = "flux2", label = "FLUX.2 Turbo"},
                            {value = "flux2pro", label = "FLUX.2 Pro"},
                            {value = "flux1dev", label = "FLUX.1 Dev"},
                        }
                    },
                    {id = "strength", type = "range", label = "Strength", default = 0.75, min = 0.1, max = 1, step = 0.05},
                }
            },
            {id = "vectorize", label = "Vectorize", icon = "sparkles", async = true},
        },
        card_actions = {
            {id = "colorize", label = "Colorize", icon = "wand", async = true},
            {id = "upscale", label = "Upscale", icon = "arrows-expand", async = true,
                options = {
                    {id = "model", type = "select", label = "Model", default = "clarity",
                        choices = {
                            {value = "clarity", label = "Clarity Upscaler"},
                            {value = "esrgan", label = "ESRGAN"},
                            {value = "creative", label = "Creative Upscaler"},
                        }
                    },
                }
            },
            {id = "restore", label = "Restore", icon = "refresh", async = true,
                options = {
                    {id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true},
                    {id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true},
                }
            },
        },
    },
}

-- FAL.AI endpoints
local FAL_ENDPOINTS = {
    colorize = "fal-ai/ddcolor",
    clarity = "fal-ai/clarity-upscaler",
    esrgan = "fal-ai/esrgan",
    creative = "fal-ai/creative-upscaler",
    restore = "fal-ai/image-apps-v2/photo-restoration",
    flux2 = "fal-ai/flux-2/turbo/edit",
    flux2pro = "fal-ai/flux-2-pro/edit",
    flux1dev = "fal-ai/flux/dev/image-to-image",
    vectorize = "fal-ai/recraft/vectorize",
}

-- Build API request payload based on action and options
local function build_request(action_id, data_uri, options)
    if action_id == "colorize" then
        return FAL_ENDPOINTS.colorize, {image_url = data_uri}

    elseif action_id == "upscale" then
        local model = options.model or "clarity"
        if model == "esrgan" then
            return FAL_ENDPOINTS.esrgan, {
                image_url = data_uri,
                scale = 4,
                model = "RealESRGAN_x4plus",
            }
        elseif model == "creative" then
            return FAL_ENDPOINTS.creative, {
                image_url = data_uri,
            }
        else
            return FAL_ENDPOINTS.clarity, {
                image_url = data_uri,
                prompt = "masterpiece, best quality, highres",
                negative_prompt = "(worst quality, low quality, normal quality:2)",
                enable_safety_checker = false,
            }
        end

    elseif action_id == "restore" then
        local fix_colors = true
        local remove_scratches = true
        if options.fix_colors ~= nil then fix_colors = options.fix_colors end
        if options.remove_scratches ~= nil then remove_scratches = options.remove_scratches end
        return FAL_ENDPOINTS.restore, {
            image_url = data_uri,
            enhance_resolution = true,
            fix_colors = fix_colors,
            remove_scratches = remove_scratches,
            enable_safety_checker = false,
        }

    elseif action_id == "edit" then
        local model = options.model or "flux2"
        local prompt = options.prompt or ""
        if model == "flux1dev" then
            local strength = options.strength or 0.75
            return FAL_ENDPOINTS.flux1dev, {
                image_url = data_uri,
                prompt = prompt,
                strength = strength,
                num_inference_steps = 40,
                guidance_scale = 3.5,
                safety_tolerance = 6,
            }
        else
            local endpoint = FAL_ENDPOINTS[model] or FAL_ENDPOINTS.flux2
            return endpoint, {
                image_urls = {data_uri},
                prompt = prompt,
                guidance_scale = 2.5,
                safety_tolerance = 6,
            }
        end

    elseif action_id == "vectorize" then
        return FAL_ENDPOINTS.vectorize, {image_url = data_uri}

    else
        return nil, nil
    end
end

-- Extract result image URL from API response
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

-- Generate output filename
local function generate_filename(original, action_id)
    local name = original:match("^(.+)%.[^%.]+$") or original
    local ext = original:match("%.([^%.]+)$") or "png"
    if action_id == "vectorize" then
        ext = "svg"
    end
    return name .. "_" .. action_id .. "." .. ext
end

-- Handle UI action from lightbox or card menu
function on_ui_action(action_id, clip_ids, options)
    local api_key = storage.get("api_key")
    if not api_key or api_key == "" then
        toast.error("FAL.AI API key not configured. Please set it in plugin settings.")
        return {success = false, error = "API key not configured"}
    end

    options = options or {}
    local action_names = {
        colorize = "Colorize",
        upscale = "Upscale",
        restore = "Restore",
        edit = "AI Edit",
        vectorize = "Vectorize",
    }
    local action_name = action_names[action_id] or action_id
    local clip_count = #clip_ids
    local task_id = task.start(action_name .. " (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            -- Get clip data (returns base64 + mime_type)
            local data, mime_type = clips.get_data(clip_id)
            if not data then
                error("Failed to get clip data")
            end

            -- Build data URI for the API
            local data_uri = "data:" .. mime_type .. ";base64," .. data

            -- Build request
            local endpoint, payload = build_request(action_id, data_uri, options)
            if not endpoint then
                error("Unknown action: " .. action_id)
            end

            -- Call fal.ai API
            local resp, http_err = http.post(
                "https://fal.run/" .. endpoint,
                {
                    body = json.encode(payload),
                    headers = {
                        Authorization = "Key " .. api_key,
                        ["Content-Type"] = "application/json",
                    },
                }
            )
            if not resp then
                error("HTTP request failed: " .. (http_err or "unknown error"))
            end
            if resp.status ~= 200 then
                error("API error (status " .. resp.status .. "): " .. (resp.body or ""))
            end

            -- Parse response
            local result = json.decode(resp.body)
            if result.msg and result.msg ~= "" then
                error(result.msg)
            end

            -- Get result URL
            local result_url = get_result_url(result)
            if not result_url then
                error("No image URL in API response")
            end

            -- Generate filename from original clip
            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id .. ".png")
            local filename = generate_filename(original_name, action_id)

            -- Download result and create new clip
            local new_clip, create_err = clips.create_from_url(result_url, {name = filename})
            if not new_clip then
                error("Failed to save result: " .. (create_err or "unknown error"))
            end

            last_clip_id = new_clip.id
        end)

        if not ok then
            errors = errors + 1
            log("FAL.AI error processing clip " .. clip_id .. ": " .. tostring(err))
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        task.fail(task_id, "All images failed to process")
    else
        task.complete(task_id)
    end

    return {success = errors < clip_count, result_clip_id = last_clip_id or 0}
end

log("FAL.AI Image Processing plugin loaded")
