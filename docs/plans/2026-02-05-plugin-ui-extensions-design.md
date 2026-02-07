# Plugin UI Extensions Design

Enable plugins to add UI elements (lightbox buttons, card actions) and process clips, allowing fal.ai functionality to be extracted as a plugin.

## Decisions

- **UI integration**: Lightbox buttons + card actions (dropdown menu)
- **Long-running ops**: Use existing task queue with plugin task API
- **Clip data access**: `clips.get_data(id)` returns raw content for any type
- **Creating results**: Extend `clips.create()` with data param + add `clips.create_from_url()`
- **UI declaration**: Declarative in manifest (no runtime registration)
- **Icons**: Named icon set (~25 curated icons)
- **Card menu**: Consolidate existing buttons into dropdown with labels, plugin actions below divider
- **Bulk operations**: Handler always receives array of clip IDs
- **Options dialogs**: Declarative form fields in manifest
- **Option choices**: Static (separate actions per model variant)
- **Progress reporting**: `task.start/progress/complete/fail` API
- **Visibility**: Always visible, handle missing prerequisites in action handler
- **Navigation**: Handler returns `{result_clip_id = id}`, framework shows "View" link

## Manifest Extensions

```lua
Plugin = {
  name = "FAL.AI Image Processing",
  version = "1.0.0",

  settings = {
    {id = "api_key", type = "password", label = "API Key", required = true},
  },

  ui = {
    lightbox_buttons = {
      {id = "colorize", label = "Colorize", icon = "wand"},
      {id = "restore", label = "Restore", icon = "sparkles",
        options = {
          {id = "fix_colors", type = "checkbox", label = "Fix colors", default = true},
          {id = "remove_scratches", type = "checkbox", label = "Remove scratches", default = true},
        }
      },
      {id = "edit_flux", label = "AI Edit", icon = "pencil",
        options = {
          {id = "prompt", type = "text", label = "Edit prompt", required = true},
          {id = "strength", type = "range", label = "Strength", min = 0.1, max = 1, step = 0.1, default = 0.8},
        }
      },
    },

    card_actions = {
      {id = "colorize", label = "Colorize", icon = "wand"},
    },
  },

  network = {
    ["fal.ai"] = {"POST"},
  },
}
```

**Option types**: `text`, `password`, `checkbox`, `select` (with `choices`), `range` (with `min`, `max`, `step`, `default`)

## Lua APIs

### Clips API Extensions

```lua
-- Get raw clip data (base64 for binary, plain text for text types)
local data, mime_type = clips.get_data(clip_id)

-- Create clip with inline data
local new_clip = clips.create({
  name = "colorized.png",
  data = base64_image_data,
  mime_type = "image/png",
})

-- Create clip from URL (downloads and stores)
local new_clip = clips.create_from_url(result_url, {
  name = "colorized.png",
  mime_type = "image/png",
})
```

### Task API

```lua
local task_id = task.start("Colorizing images", total_count)
task.progress(task_id, current_index)
task.complete(task_id)
task.fail(task_id, "Error message")
```

### Handler Signature

```lua
function on_ui_action(action_id, clip_ids, options)
  -- Process clips...
  return {result_clip_id = new_clip.id}
end
```

## Frontend UI

### Card Menu

Consolidate existing card buttons into dropdown:

```
[Icon] Copy
[Icon] Archive
[Icon] Delete (red)
─────────────────
[Icon] Colorize
[Icon] Upscale
```

Accessibility: `role="menu"`, `role="menuitem"`, arrow key navigation, Enter/Escape handling.

### Lightbox Buttons

Plugin buttons appear in lightbox bottom bar alongside any built-in buttons.

### Options Dialog

Modal with form fields rendered from manifest. Title shows action name and clip count.

## Backend

### New Structs (`plugin/manifest.go`)

```go
type UIManifest struct {
    LightboxButtons []UIAction `json:"lightbox_buttons,omitempty"`
    CardActions     []UIAction `json:"card_actions,omitempty"`
}

type UIAction struct {
    ID      string      `json:"id"`
    Label   string      `json:"label"`
    Icon    string      `json:"icon,omitempty"`
    Options []FormField `json:"options,omitempty"`
}

type FormField struct {
    ID       string   `json:"id"`
    Type     string   `json:"type"`
    Label    string   `json:"label"`
    Required bool     `json:"required,omitempty"`
    Default  any      `json:"default,omitempty"`
    Choices  []Choice `json:"choices,omitempty"`
    Min      float64  `json:"min,omitempty"`
    Max      float64  `json:"max,omitempty"`
    Step     float64  `json:"step,omitempty"`
}
```

### PluginService Methods

```go
func (s *PluginService) ExecutePluginAction(pluginID, actionID string, clipIDs []int64, options map[string]any) (*ActionResult, error)
func (s *PluginService) GetPluginUIActions() (*UIActionsResponse, error)
```

## Test Plugin

```lua
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",

  settings = {
    {id = "prefix", type = "text", label = "Output prefix", default = "processed"},
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

function on_ui_action(action_id, clip_ids, options)
  local settings = storage.get("settings") or {}
  local prefix = settings.prefix or "processed"

  local task_id = task.start("Test Processing", #clip_ids)
  local last_clip = nil

  for i, clip_id in ipairs(clip_ids) do
    local data, mime_type = clips.get_data(clip_id)
    local clip = clips.get(clip_id)

    local new_name = prefix .. "_" .. clip.name
    if options.uppercase then
      new_name = string.upper(new_name)
    end

    last_clip = clips.create({
      name = new_name .. (options.suffix or ""),
      data = data,
      mime_type = mime_type,
    })

    task.progress(task_id, i)
  end

  task.complete(task_id)
  return {result_clip_id = last_clip and last_clip.id}
end
```

## Implementation Files

### Create

| File | Purpose |
|------|---------|
| `plugin/api_task.go` | Task queue Lua API |
| `plugins/test-plugin.lua` | E2E test plugin |
| `plugins/fal-ai.lua` | Extracted fal.ai plugin |

### Modify

| File | Changes |
|------|---------|
| `plugin/manifest.go` | UIManifest, UIAction, FormField structs |
| `plugin/api_clips.go` | `get_data()`, extend `create()`, `create_from_url()` |
| `plugin/manager.go` | Register task API, `on_ui_action` handler |
| `plugin_service.go` | `ExecutePluginAction()`, `GetPluginUIActions()` |
| `frontend/index.html` | Card menu dropdown, options dialog |
| `frontend/js/ui.js` | Card menu rendering |
| `frontend/js/modals.js` | Lightbox plugin buttons, options dialog |
| `frontend/css/main.css` | Card menu styles |

### Remove (after plugin works)

| File | Reason |
|------|--------|
| `falai.go` | Moved to plugin |
| `app.go` fal.ai methods | Moved to plugin |
| `frontend/js/ai-processing.js` | Replaced by plugin system |

## E2E Tests

- Plugin button visibility in lightbox
- Plugin actions in card menu
- Options dialog rendering and submission
- Task progress updates
- Clip data get/create APIs
- Bulk action processing
