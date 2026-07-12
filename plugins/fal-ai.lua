-- FAL.AI Image Processing Plugin
-- AI-powered image processing using fal.ai

Plugin = {
    name = "FAL.AI Image Processing",
    version = "1.1.0",
    description = "AI-powered image processing using fal.ai - text-to-image generation, colorization, upscaling, restoration, and AI editing.",
    author = "mahpastes",

    network = {
        ["fal.ai"] = {"GET", "POST"},
        ["fal.run"] = {"GET", "POST"},
        ["*.fal.media"] = {"GET"},
    },

    settings = {
        {key = "api_key", type = "password", label = "FAL.AI API Key", required = true},
    },

    ui = {
        lightbox_buttons = {
            {id = "colorize", label = "Colorize", icon = "wand", async = true, file_types = {"image/*"}},
            {id = "upscale", label = "Upscale", icon = "arrows-expand", async = true, file_types = {"image/*"},
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
                    {id = "model", type = "select", label = "Model", default = "flux2",
                        choices = {
                            {value = "flux2", label = "FLUX.2 Turbo"},
                            {value = "flux2pro", label = "FLUX.2 Pro"},
                            {value = "nanobanana2", label = "Nano Banana 2"},
                            {value = "flux1dev", label = "FLUX.1 Dev"},
                        }
                    },
                    {id = "strength", type = "range", label = "Strength", default = 0.75, min = 0.1, max = 1, step = 0.05},
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
                            {value = "esrgan", label = "ESRGAN"},
                            {value = "creative", label = "Creative Upscaler"},
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
                    {id = "model", type = "select", label = "Model", default = "flux2",
                        choices = {
                            {value = "flux2", label = "FLUX.2 Turbo"},
                            {value = "flux2pro", label = "FLUX.2 Pro"},
                            {value = "nanobanana2", label = "Nano Banana 2"},
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
                            {value = "imagen4", label = "Imagen 4"},
                            {value = "imagen4_fast", label = "Imagen 4 Fast"},
                            {value = "imagen4_ultra", label = "Imagen 4 Ultra"},
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
    nanobanana2 = "fal-ai/nano-banana-2/edit",
    vectorize = "fal-ai/recraft/vectorize",
    nanobanana2_generate = "fal-ai/nano-banana-2",
    imagen4 = "fal-ai/imagen4/preview",
    imagen4_fast = "fal-ai/imagen4/preview/fast",
    imagen4_ultra = "fal-ai/imagen4/preview/ultra",
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
                safety_tolerance = 5,
            }
        elseif model == "nanobanana2" then
            return FAL_ENDPOINTS.nanobanana2, {
                image_urls = {data_uri},
                prompt = prompt,
                safety_tolerance = 6,
            }
        else
            local endpoint = FAL_ENDPOINTS[model] or FAL_ENDPOINTS.flux2
            return endpoint, {
                image_urls = {data_uri},
                prompt = prompt,
                guidance_scale = 2.5,
                safety_tolerance = 5,
            }
        end

    elseif action_id == "vectorize" then
        return FAL_ENDPOINTS.vectorize, {image_url = data_uri}

    else
        return nil, nil
    end
end

-- Build a single multi-image edit request. Unlike applying a card action to
-- each image independently, the selected images are sent together so the
-- model can use all of them as edit inputs/references.
local function build_multi_edit_request(data_uris, options)
    local model = options.model or "flux2"
    local endpoint = FAL_ENDPOINTS[model] or FAL_ENDPOINTS.flux2
    local payload = {
        image_urls = data_uris,
        prompt = options.prompt or "",
        safety_tolerance = model == "nanobanana2" and 6 or 5,
    }
    if model ~= "nanobanana2" then
        payload.guidance_scale = 2.5
    end
    return endpoint, payload
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

-- Generate output filename, preserving original extension
-- (ensure_jpeg will rename to .jpg after successful conversion)
local function generate_filename(original, action_id)
    local name = original:match("^(.+)%.[^%.]+$") or original
    local ext = original:match("%.([^%.]+)$") or "png"
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

-- Handle UI action from lightbox, card menu, or global action.
-- context carries the active folder's tag when invoked from folder view.
function on_ui_action(action_id, clip_ids, options, context)
    local api_key = storage.get("api_key")
    if not api_key or api_key == "" then
        toast.show("FAL.AI API key not configured. Please set it in plugin settings.", "error")
        return {success = false, error = "API key not configured"}
    end

    options = options or {}
    local action_names = {
        colorize = "Colorize",
        upscale = "Upscale",
        restore = "Restore",
        edit = "AI Edit",
        vectorize = "Vectorize",
        generate = "Generate Image",
    }
    local action_name = action_names[action_id] or action_id

    -- Handle text-to-image generation (no input clips required)
    if action_id == "generate" then
        local prompt = options.prompt or ""
        if prompt == "" then
            toast.show("Please enter a prompt.", "error")
            return {success = false, error = "Prompt is required"}
        end

        local task_id = task.start("Generate Image", 1)
        local last_clip_id = nil
        local model = options.model or "nanobanana2"

        local ok, err = pcall(function()
            local endpoint = FAL_ENDPOINTS[model] or FAL_ENDPOINTS.nanobanana2_generate
            local payload = {
                prompt = prompt,
                aspect_ratio = options.aspect_ratio or "1:1",
                output_format = "jpeg",
                safety_tolerance = 6,
            }

            -- Nano Banana 2 uses its own endpoint key and supports all resolutions
            if model == "nanobanana2" then
                endpoint = FAL_ENDPOINTS.nanobanana2_generate
                payload.resolution = options.resolution or "1K"
            -- Imagen 4 Fast has no resolution parameter
            elseif model == "imagen4_fast" then
                -- no resolution param
            -- Imagen 4 Standard/Ultra support 1K and 2K only
            else
                local res = options.resolution or "1K"
                if res == "1K" or res == "2K" then
                    payload.resolution = res
                else
                    payload.resolution = "1K"
                end
            end

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

            local result = json.decode(resp.body)
            if result.msg and result.msg ~= "" then
                error(result.msg)
            end

            local result_url = get_result_url(result)
            if not result_url then
                error("No image URL in API response")
            end

            -- Build filename from truncated prompt
            local safe_prompt = prompt:gsub("[^%w%s_-]", ""):gsub("%s+", "_"):sub(1, 40)
            local filename = "generated_" .. safe_prompt .. ".jpg"

            local new_clip, create_err = clips.create_from_url(result_url, {name = filename})
            if not new_clip then
                error("Failed to save result: " .. (create_err or "unknown error"))
            end

            last_clip_id = ensure_jpeg(new_clip.id)
            add_to_folder(last_clip_id, context)
        end)

        if ok then
            task.progress(task_id, 1)
            task.complete(task_id)
            return {success = true, result_clip_id = last_clip_id or 0}
        else
            task.fail(task_id, tostring(err))
            return {success = false, error = tostring(err)}
        end
    end

    -- A bulk AI edit is one model invocation with all selected images. The
    -- result is a single new clip informed by the complete selection.
    if action_id == "edit" and #clip_ids > 1 then
        local prompt = options.prompt or ""
        if prompt == "" then
            toast.show("Please enter an edit prompt.", "error")
            return {success = false, error = "Prompt is required"}
        end

        local task_id = task.start("AI Edit (" .. #clip_ids .. " images)", 1)
        local result_clip_id = nil
        local ok, err = pcall(function()
            local data_uris = {}
            local first_info = nil
            local supported_types = {
                ["image/png"] = true,
                ["image/jpeg"] = true,
                ["image/webp"] = true,
                ["image/gif"] = true,
                ["image/tiff"] = true,
                ["image/bmp"] = true,
            }

            for _, clip_id in ipairs(clip_ids) do
                local data, mime_type = clips.get_data(clip_id)
                if not data then error("Failed to get clip data for clip " .. clip_id) end
                if not supported_types[mime_type] then
                    error("Unsupported image format: " .. (mime_type or "unknown"))
                end
                table.insert(data_uris, "data:" .. mime_type .. ";base64," .. data)
                if not first_info then first_info = clips.get(clip_id) end
            end

            local endpoint, payload = build_multi_edit_request(data_uris, options)
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
            if not resp then error("HTTP request failed: " .. (http_err or "unknown error")) end
            if resp.status ~= 200 then
                error("API error (status " .. resp.status .. "): " .. (resp.body or ""))
            end

            local result = json.decode(resp.body)
            if result.msg and result.msg ~= "" then error(result.msg) end
            local result_url = get_result_url(result)
            if not result_url then error("No image URL in API response") end

            local original_name = (first_info and first_info.filename) or "multi_image.png"
            local new_clip, create_err = clips.create_from_url(result_url, {
                name = generate_filename(original_name, "edit"),
            })
            if not new_clip then
                error("Failed to save result: " .. (create_err or "unknown error"))
            end
            result_clip_id = ensure_jpeg(new_clip.id)
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

    local clip_count = #clip_ids
    local task_id = task.start(action_name .. " (" .. clip_count .. " image" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local last_clip_id = nil
    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            -- Get clip data (returns base64 + mime_type)
            local data, mime_type = clips.get_data(clip_id)
            if not data then
                error("Failed to get clip data")
            end

            -- Validate raster image format (fal.ai endpoints reject SVG and other non-raster formats)
            local supported_types = {
                ["image/png"] = true,
                ["image/jpeg"] = true,
                ["image/webp"] = true,
                ["image/gif"] = true,
                ["image/tiff"] = true,
                ["image/bmp"] = true,
            }
            if not supported_types[mime_type] then
                error("Unsupported image format: " .. mime_type .. ". Only raster images (PNG, JPEG, WebP) are supported.")
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

            -- Download result and create new clip (use temporary name, will rename after conversion)
            local clip_info = clips.get(clip_id)
            local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id .. ".png")
            local temp_filename = generate_filename(original_name, action_id)

            local new_clip, create_err = clips.create_from_url(result_url, {name = temp_filename})
            if not new_clip then
                error("Failed to save result: " .. (create_err or "unknown error"))
            end

            -- Convert to JPEG to reduce file size (fal.ai often returns large PNGs)
            -- ensure_jpeg handles filename renaming to .jpg
            last_clip_id = ensure_jpeg(new_clip.id)
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
