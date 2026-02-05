--[[
@plugin
name: FAL.AI Image Processing
version: 0.1.0
author: mahpastes
description: AI-powered image processing using fal.ai - colorization, upscaling, restoration, and AI editing.

[events]
# No events needed - this is a UI-action-only plugin

[network]
# Required for fal.ai API calls
allow = ["fal.ai", "*.fal.ai", "fal.run", "*.fal.run"]

[ui]
# Lightbox buttons for single-image actions
[[ui.lightbox_buttons]]
id = "colorize"
label = "Colorize"
icon = "wand"
tooltip = "Colorize grayscale images using AI"

[[ui.lightbox_buttons]]
id = "upscale"
label = "Upscale"
icon = "expand"
tooltip = "Upscale image resolution using AI"
options = [
    { id = "scale", type = "select", label = "Scale Factor", default = "2", choices = [
        { value = "2", label = "2x" },
        { value = "4", label = "4x" }
    ]},
    { id = "model", type = "select", label = "Model", default = "clarity", choices = [
        { value = "clarity", label = "Clarity Upscaler" },
        { value = "esrgan", label = "ESRGAN" },
        { value = "creative", label = "Creative Upscaler" }
    ]}
]

[[ui.lightbox_buttons]]
id = "restore"
label = "Restore"
icon = "clock"
tooltip = "Restore old/damaged photos using AI"
options = [
    { id = "enhance_resolution", type = "checkbox", label = "Enhance Resolution", default = true },
    { id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true },
    { id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true }
]

[[ui.lightbox_buttons]]
id = "edit"
label = "AI Edit"
icon = "pencil"
tooltip = "Edit image with AI using text prompts"
options = [
    { id = "prompt", type = "text", label = "Edit Prompt", required = true },
    { id = "model", type = "select", label = "Model", default = "flux2", choices = [
        { value = "flux2", label = "FLUX.2 Turbo" },
        { value = "flux2pro", label = "FLUX.2 Pro" },
        { value = "flux1dev", label = "FLUX.1 Dev" }
    ]},
    { id = "guidance", type = "range", label = "Guidance Scale", default = 7.5, min = 1, max = 20, step = 0.5 }
]

[[ui.lightbox_buttons]]
id = "vectorize"
label = "Vectorize"
icon = "sparkles"
tooltip = "Convert image to vector graphics (SVG)"

# Card actions for batch operations (same as lightbox but for multiple images)
[[ui.card_actions]]
id = "colorize"
label = "Colorize"
icon = "wand"
tooltip = "Colorize selected images"

[[ui.card_actions]]
id = "upscale"
label = "Upscale"
icon = "expand"
tooltip = "Upscale selected images"
options = [
    { id = "scale", type = "select", label = "Scale Factor", default = "2", choices = [
        { value = "2", label = "2x" },
        { value = "4", label = "4x" }
    ]},
    { id = "model", type = "select", label = "Model", default = "clarity", choices = [
        { value = "clarity", label = "Clarity Upscaler" },
        { value = "esrgan", label = "ESRGAN" },
        { value = "creative", label = "Creative Upscaler" }
    ]}
]

[[ui.card_actions]]
id = "restore"
label = "Restore"
icon = "clock"
tooltip = "Restore selected images"
options = [
    { id = "enhance_resolution", type = "checkbox", label = "Enhance Resolution", default = true },
    { id = "fix_colors", type = "checkbox", label = "Fix Colors", default = true },
    { id = "remove_scratches", type = "checkbox", label = "Remove Scratches", default = true }
]

[settings]
# API key setting (stored securely)
[[settings.fields]]
id = "api_key"
type = "password"
label = "FAL.AI API Key"
required = true
description = "Get your API key from fal.ai/dashboard/keys"
]]

-- FAL.AI endpoints (for future implementation)
local FAL_ENDPOINTS = {
    colorize = "fal-ai/ddcolor",
    clarity_upscale = "fal-ai/clarity-upscaler",
    esrgan = "fal-ai/esrgan",
    creative_upscale = "fal-ai/creative-upscaler",
    restore = "fal-ai/image-apps-v2/photo-restoration",
    codeformer = "fal-ai/codeformer",
    flux2_edit = "fal-ai/flux-2/turbo/edit",
    flux2_pro_edit = "fal-ai/flux-2-pro/edit",
    flux1_dev_edit = "fal-ai/flux/dev/image-to-image",
    vectorize = "fal-ai/recraft/vectorize"
}

-- Handle UI action from lightbox or card menu
function on_ui_action(action_id, clip_ids, options)
    -- Get API key from settings
    local api_key = storage.get("api_key")

    if not api_key or api_key == "" then
        toast.error("FAL.AI API key not configured. Please set it in plugin settings.")
        return { success = false, error = "API key not configured" }
    end

    -- Stub implementation - show "coming soon" message
    local action_names = {
        colorize = "Colorize",
        upscale = "Upscale",
        restore = "Restore",
        edit = "AI Edit",
        vectorize = "Vectorize"
    }

    local action_name = action_names[action_id] or action_id
    local clip_count = #clip_ids

    toast.info(string.format("%s: Processing %d image(s)... (Coming soon!)", action_name, clip_count))

    -- For now, return success without actually processing
    -- Full implementation will:
    -- 1. Get image data from clips
    -- 2. Upload to fal.ai
    -- 3. Call appropriate endpoint
    -- 4. Download result and create new clip

    log(string.format("FAL.AI %s action called with %d clips, options: %s",
        action_id, clip_count, json.encode(options or {})))

    return { success = true }
end

-- Initialize plugin
log("FAL.AI Image Processing plugin loaded (stub version)")
