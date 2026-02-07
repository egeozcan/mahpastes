-- Test Plugin for E2E Testing
-- Exercises all plugin UI extension APIs

Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  description = "E2E test plugin for UI extensions",
  author = "mahpastes",

  settings = {
    {key = "prefix", type = "text", label = "Output prefix", default = "processed"},
  },

  ui = {
    lightbox_buttons = {
      {id = "test_simple", label = "Test Simple", icon = "sparkles"},
      {id = "test_options", label = "Test Options", icon = "pencil",
        options = {
          {id = "suffix", type = "text", label = "Suffix", default = "_modified"},
          {id = "uppercase", type = "checkbox", label = "Uppercase", default = false},
        }
      },
    },
    card_actions = {
      {id = "test_simple", label = "Test Simple", icon = "sparkles"},
      {id = "test_bulk", label = "Test Bulk", icon = "refresh"},
    },
  },
}

-- Valid action IDs for this plugin
local VALID_ACTIONS = {
  test_simple = true,
  test_options = true,
  test_bulk = true,
}

function on_ui_action(action_id, clip_ids, options)
  -- Validate action ID
  if not VALID_ACTIONS[action_id] then
    return { success = false, error = "Unknown action: " .. tostring(action_id) }
  end

  local settings_json = storage.get("settings") or "{}"
  local settings = json.decode(settings_json) or {}
  local prefix = settings.prefix or "processed"

  local task_id = task.start("Test Processing", #clip_ids)
  local last_clip = nil

  for i, clip_id in ipairs(clip_ids) do
    local data, mime_type = clips.get_data(clip_id)
    local clip = clips.get(clip_id)

    local new_name = prefix .. "_" .. (clip.filename or "clip")

    if options.uppercase then
      new_name = string.upper(new_name)
    end

    if options.suffix then
      new_name = new_name .. options.suffix
    end

    -- Create a copy with modified name
    last_clip = clips.create({
      name = new_name,
      data = data,
      mime_type = mime_type,
    })

    task.progress(task_id, i)
  end

  task.complete(task_id)

  if last_clip then
    return {result_clip_id = last_clip.id}
  end
  return {}
end
