-- Test Plugin for Plugin Result Modal E2E Tests
-- Returns modal data from on_ui_action for different formats

Plugin = {
    name = "Modal Test",
    version = "1.0.0",
    description = "Test plugin for modal result functionality",
    author = "e2e-tests",

    ui = {
        lightbox_buttons = {
            {id = "show_markdown", label = "Show Markdown", icon = "info"},
            {id = "show_text", label = "Show Text", icon = "code"},
            {id = "show_image", label = "Show Image", icon = "sparkles"},
            {id = "async_markdown", label = "Async Markdown", icon = "info", async = true},
            {id = "async_no_modal", label = "Async No Modal", icon = "sparkles", async = true},
        },
        card_actions = {
            {id = "show_markdown", label = "Show Markdown", icon = "info"},
            {id = "show_text", label = "Show Text", icon = "code"},
            {id = "show_no_modal", label = "No Modal", icon = "sparkles"},
            {id = "async_markdown", label = "Async Markdown", icon = "info", async = true},
            {id = "async_no_modal", label = "Async No Modal", icon = "sparkles", async = true},
        },
    },
}

function on_ui_action(action_id, clip_ids, options)
    if action_id == "show_markdown" then
        return {
            success = true,
            modal = {
                title = "Markdown Result",
                content = "## Test Header\n\n| Col A | Col B |\n|---|---|\n| val1 | val2 |",
                format = "markdown",
                copy_data = "custom copy data",
                paste_data = "custom paste data",
                paste_name = "test-result.txt",
                paste_content_type = "text/plain",
            }
        }
    elseif action_id == "show_text" then
        return {
            success = true,
            modal = {
                title = "Text Result",
                content = "Line 1\nLine 2\nLine 3",
                format = "text",
                copy_data = "Line 1\nLine 2\nLine 3",
                paste_data = "Line 1\nLine 2\nLine 3",
                paste_name = "text-output.txt",
                paste_content_type = "text/plain",
            }
        }
    elseif action_id == "show_image" then
        -- Tiny 1x1 red pixel PNG as base64
        local img_data = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="
        return {
            success = true,
            modal = {
                title = "Image Result",
                content = img_data,
                format = "image",
            }
        }
    elseif action_id == "show_no_modal" then
        -- No modal, just regular result
        return {success = true}
    elseif action_id == "async_markdown" then
        return {
            success = true,
            modal = {
                title = "Async Markdown Result",
                content = "## Async Header\n\nAsync body text",
                format = "markdown",
                copy_data = "async copy data",
            }
        }
    elseif action_id == "async_no_modal" then
        return {success = true}
    end

    return {success = false, error = "Unknown action: " .. action_id}
end

storage.set("loaded", "true")
log("Modal Test plugin loaded")
