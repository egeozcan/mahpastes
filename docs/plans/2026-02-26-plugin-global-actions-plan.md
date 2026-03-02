# Plugin Global Actions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow plugins to define `global_actions` that appear in the hamburger menu and execute without a clip resource.

**Architecture:** Extends the existing `UIAction`/`UIManifest` system with a third action array (`GlobalActions`). Backend reuses `ExecutePluginAction` with empty `clipIDs`. Frontend renders actions in the nav drawer and reuses the existing options dialog and execution pipeline.

**Tech Stack:** Go (manifest parsing, plugin service), Vanilla JS (drawer rendering), Playwright (e2e tests)

---

### Task 1: Add GlobalActions to Go manifest

**Files:**
- Modify: `plugin/manifest.go:68-71` (UIManifest struct)
- Modify: `plugin/manifest.go:504-527` (extractUI function)

**Step 1: Add GlobalActions field to UIManifest**

In `plugin/manifest.go`, add the field to the struct:

```go
// UIManifest represents plugin UI declarations
type UIManifest struct {
	LightboxButtons []UIAction `json:"lightbox_buttons,omitempty"`
	CardActions     []UIAction `json:"card_actions,omitempty"`
	GlobalActions   []UIAction `json:"global_actions,omitempty"`
}
```

**Step 2: Parse global_actions in extractUI**

In the `extractUI` function, add parsing for the new field and update the nil check:

```go
func extractUI(block string) *UIManifest {
	// Find the ui block
	loc := reUIBlock.FindStringIndex(block)
	if loc == nil {
		return nil
	}

	start := loc[1] - 1
	uiBlock := extractNestedBrace(block[start:])
	if uiBlock == "" {
		return nil
	}

	ui := &UIManifest{}
	ui.LightboxButtons = extractUIActions(uiBlock, "lightbox_buttons")
	ui.CardActions = extractUIActions(uiBlock, "card_actions")
	ui.GlobalActions = extractUIActions(uiBlock, "global_actions")

	// Return nil if no actions defined
	if len(ui.LightboxButtons) == 0 && len(ui.CardActions) == 0 && len(ui.GlobalActions) == 0 {
		return nil
	}

	return ui
}
```

**Step 3: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: Clean compilation, no errors.

**Step 4: Commit**

```bash
git add plugin/manifest.go
git commit -m "feat: add GlobalActions field to UIManifest and parse global_actions from plugin manifests"
```

---

### Task 2: Add GlobalActions to plugin service response

**Files:**
- Modify: `plugin_service.go:54-57` (UIActionsResponse struct)
- Modify: `plugin_service.go:367-418` (GetPluginUIActions function)

**Step 1: Add GlobalActions to UIActionsResponse**

```go
// UIActionsResponse contains all plugin UI actions
type UIActionsResponse struct {
	LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
	CardActions     []PluginUIAction `json:"card_actions"`
	GlobalActions   []PluginUIAction `json:"global_actions"`
}
```

**Step 2: Initialize and populate GlobalActions in GetPluginUIActions**

Add `GlobalActions: []PluginUIAction{}` to the response initialization, and add a loop to collect global actions from enabled plugins — same pattern as lightbox/card:

```go
func (s *PluginService) GetPluginUIActions() (*UIActionsResponse, error) {
	if s.app.pluginManager == nil {
		return &UIActionsResponse{
			LightboxButtons: []PluginUIAction{},
			CardActions:     []PluginUIAction{},
			GlobalActions:   []PluginUIAction{},
		}, nil
	}

	response := &UIActionsResponse{
		LightboxButtons: []PluginUIAction{},
		CardActions:     []PluginUIAction{},
		GlobalActions:   []PluginUIAction{},
	}

	plugins := s.app.pluginManager.GetPlugins()
	for _, p := range plugins {
		if !p.Enabled || p.Manifest == nil || p.Manifest.UI == nil {
			continue
		}

		// Add lightbox buttons (existing code unchanged)
		for _, btn := range p.Manifest.UI.LightboxButtons {
			response.LightboxButtons = append(response.LightboxButtons, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         btn.ID,
				Label:      btn.Label,
				Icon:       btn.Icon,
				Async:      btn.Async,
				Options:    btn.Options,
				FileTypes:  btn.FileTypes,
				MaxSize:    btn.MaxSize,
			})
		}

		// Add card actions (existing code unchanged)
		for _, action := range p.Manifest.UI.CardActions {
			response.CardActions = append(response.CardActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Async:      action.Async,
				Options:    action.Options,
				FileTypes:  action.FileTypes,
				MaxSize:    action.MaxSize,
			})
		}

		// Add global actions
		for _, action := range p.Manifest.UI.GlobalActions {
			response.GlobalActions = append(response.GlobalActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Async:      action.Async,
				Options:    action.Options,
			})
		}
	}

	return response, nil
}
```

Note: GlobalActions intentionally omits `FileTypes` and `MaxSize` since they don't apply.

**Step 3: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: Clean compilation, no errors.

**Step 4: Commit**

```bash
git add plugin_service.go
git commit -m "feat: include global_actions in GetPluginUIActions response"
```

---

### Task 3: Add drawer container to HTML

**Files:**
- Modify: `frontend/index.html:188-189` (nav drawer, before closing `</nav>`)

**Step 1: Add divider and container before closing nav tag**

Insert right before the `</nav>` closing tag (line 189):

```html
            <div id="drawer-plugin-actions" class="hidden">
                <div class="my-1 border-t border-stone-100"></div>
            </div>
        </nav>
```

This adds a hidden container with a divider. The JS will populate it with action buttons and show/hide it.

**Step 2: Verify the HTML is valid**

Open the app with `make dev` and confirm the drawer still works correctly. The new container should be invisible (hidden class).

**Step 3: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add drawer-plugin-actions container to hamburger menu"
```

---

### Task 4: Add global actions rendering in frontend JS

**Files:**
- Modify: `frontend/js/ui.js:24-32` (loadPluginUIActions function)
- Modify: `frontend/js/app.js:26-30` (drawer click handler)
- Modify: `frontend/js/plugins.js:717-727` (openPluginOptionsDialog title)

**Step 1: Extend loadPluginUIActions to also render global actions in drawer**

In `frontend/js/ui.js`, after the existing `loadPluginUIActions` function (which caches `pluginUIActions`), add a new function `renderDrawerPluginActions()` and call it from `loadPluginUIActions`:

```javascript
// Load plugin UI actions from backend
async function loadPluginUIActions() {
    try {
        pluginUIActions = await window.go.main.PluginService.GetPluginUIActions();
    } catch (error) {
        console.error('Failed to load plugin UI actions:', error);
        pluginUIActions = { card_actions: [], lightbox_buttons: [], global_actions: [] };
    }
    renderDrawerPluginActions();
    return pluginUIActions;
}

// Render global plugin actions in the hamburger menu drawer
function renderDrawerPluginActions() {
    const container = document.getElementById('drawer-plugin-actions');
    if (!container) return;

    // Remove old action buttons (keep the divider which is the first child)
    while (container.children.length > 1) {
        container.removeChild(container.lastChild);
    }

    const actions = pluginUIActions?.global_actions || [];
    if (actions.length === 0) {
        container.classList.add('hidden');
        return;
    }

    container.classList.remove('hidden');

    actions.forEach(action => {
        const btn = document.createElement('button');
        btn.className = 'border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2.5 px-3 rounded-md transition-colors flex items-center w-full';
        btn.dataset.globalAction = 'true';
        btn.dataset.pluginId = action.plugin_id;
        btn.dataset.actionId = action.id;
        btn.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';
        btn.dataset.isAsync = action.async ? 'true' : 'false';

        let iconHtml = '';
        if (typeof getPluginIcon === 'function') {
            iconHtml = getPluginIcon(action.icon) || getPluginIcon('bolt') || '';
        }
        // Wrap icon in a span with same styling as drawer menu icons
        if (iconHtml) {
            iconHtml = iconHtml.replace('<svg ', '<svg class="w-4 h-4 mr-2 opacity-60" ');
        }

        btn.innerHTML = `${iconHtml}<span>${escapeHTML(action.label)}</span>`;
        container.appendChild(btn);
    });
}
```

**Step 2: Add click handler for global action buttons in drawer**

In `frontend/js/app.js`, modify the drawer click handler (lines 26-30) to also handle global action clicks:

```javascript
// Close drawer when any nav button inside it is clicked
navDrawer.addEventListener('click', (e) => {
    const globalActionBtn = e.target.closest('[data-global-action]');
    if (globalActionBtn) {
        closeDrawer();
        handleGlobalAction(globalActionBtn);
        return;
    }
    if (e.target.closest('button[id]') && e.target.closest('button[id]') !== drawerCloseBtn) {
        closeDrawer();
    }
});

// Handle a global plugin action click from the drawer
function handleGlobalAction(btn) {
    const pluginId = parseInt(btn.dataset.pluginId, 10);
    const actionId = btn.dataset.actionId;
    const hasOptions = btn.dataset.hasOptions === 'true';
    const isAsync = btn.dataset.isAsync === 'true';

    if (hasOptions) {
        // Find the full action object from cache
        const action = pluginUIActions?.global_actions?.find(
            a => a.plugin_id === pluginId && a.id === actionId
        );
        if (action) {
            openPluginOptionsDialog(action, []);
        }
    } else {
        executePluginAction(pluginId, actionId, [], {}, isAsync);
    }
}
```

**Step 3: Fix options dialog title for empty clipIds**

In `frontend/js/plugins.js`, update the `openPluginOptionsDialog` function (line 727) to handle empty clip IDs gracefully:

```javascript
function openPluginOptionsDialog(action, clipIds) {
    currentPluginAction = action;
    currentActionClipIds = clipIds;

    const modal = document.getElementById('plugin-options-modal');
    const title = document.getElementById('plugin-options-title');
    const form = document.getElementById('plugin-options-form');

    // Set title — handle global actions (no clips)
    const clipCount = clipIds.length;
    if (clipCount === 0) {
        title.textContent = action.label;
    } else {
        title.textContent = `${action.label} - ${clipCount} ${clipCount === 1 ? 'clip' : 'clips'}`;
    }

    // ... rest of function unchanged
```

**Step 4: Verify the app builds and renders correctly**

Run: `make dev`
Expected: App starts, drawer opens normally, no JS console errors.

**Step 5: Commit**

```bash
git add frontend/js/ui.js frontend/js/app.js frontend/js/plugins.js
git commit -m "feat: render plugin global actions in hamburger menu with options dialog support"
```

---

### Task 5: Add global_actions to test plugin

**Files:**
- Modify: `e2e/fixtures/test-plugin.lua:14-32` (Plugin UI section)

**Step 1: Add global_actions to the test plugin manifest**

Add a `global_actions` section to the test plugin's UI manifest, after `card_actions`:

```lua
    global_actions = {
      {id = "test_global_simple", label = "Test Global Simple", icon = "sparkles"},
      {id = "test_global_options", label = "Test Global Options", icon = "pencil",
        options = {
          {id = "name", type = "text", label = "Name", required = true, default = "test"},
        }
      },
      {id = "test_global_async", label = "Test Global Async", icon = "refresh", async = true},
    },
```

**Step 2: Handle global actions in the Lua handler**

Update the `VALID_ACTIONS` table and the `on_ui_action` function to handle global actions (which receive empty `clip_ids`):

```lua
local VALID_ACTIONS = {
  test_simple = true,
  test_options = true,
  test_bulk = true,
  test_image_only = true,
  test_text_only = true,
  test_global_simple = true,
  test_global_options = true,
  test_global_async = true,
}

function on_ui_action(action_id, clip_ids, options)
  -- Validate action ID
  if not VALID_ACTIONS[action_id] then
    return { success = false, error = "Unknown action: " .. tostring(action_id) }
  end

  -- Handle global actions (no clip_ids)
  if action_id == "test_global_simple" then
    local new_clip = clips.create({
      name = "global_simple_result.txt",
      data = "global action executed",
      mime_type = "text/plain",
    })
    return {result_clip_id = new_clip.id}
  end

  if action_id == "test_global_options" then
    local name = options.name or "unnamed"
    local new_clip = clips.create({
      name = name .. ".txt",
      data = "global action with name: " .. name,
      mime_type = "text/plain",
    })
    return {result_clip_id = new_clip.id}
  end

  if action_id == "test_global_async" then
    local task_id = task.start("Global Async", 1)
    local new_clip = clips.create({
      name = "global_async_result.txt",
      data = "async global action executed",
      mime_type = "text/plain",
    })
    task.progress(task_id, 1)
    task.complete(task_id)
    return {result_clip_id = new_clip.id}
  end

  -- ... existing clip-based action handling below unchanged
```

**Step 3: Commit**

```bash
git add e2e/fixtures/test-plugin.lua
git commit -m "test: add global_actions to test plugin for e2e testing"
```

---

### Task 6: Add selectors and AppHelper methods for global actions

**Files:**
- Modify: `e2e/helpers/selectors.ts:16-25` (drawer selectors)
- Modify: `e2e/fixtures/test-fixtures.ts` (AppHelper methods)

**Step 1: Add drawer plugin action selectors**

In `e2e/helpers/selectors.ts`, add to the `drawer` section:

```typescript
  drawer: {
    overlay: '#drawer-overlay',
    panel: '#nav-drawer',
    closeButton: '#drawer-close-btn',
    watchButton: '#toggle-watch-view-btn',
    watchIndicator: '#watch-indicator',
    archiveButton: '#toggle-archive-view-btn',
    clearAllButton: '#delete-all-temp-btn',
    settingsButton: '#open-settings-btn',
    pluginActionsContainer: '#drawer-plugin-actions',
    pluginAction: '#drawer-plugin-actions [data-global-action]',
  },
```

**Step 2: Add AppHelper methods for global actions**

Add these methods to the AppHelper class in `e2e/fixtures/test-fixtures.ts`, near the existing plugin action helpers (around line 1946):

```typescript
  async expectDrawerPluginActionsVisible(): Promise<void> {
    await this.openDrawer();
    const container = this.page.locator(selectors.drawer.pluginActionsContainer);
    await expect(container).toBeVisible();
  }

  async expectDrawerPluginActionsHidden(): Promise<void> {
    await this.openDrawer();
    const container = this.page.locator(selectors.drawer.pluginActionsContainer);
    await expect(container).toBeHidden();
  }

  async expectDrawerPluginActionsCount(count: number): Promise<void> {
    await this.openDrawer();
    const actions = this.page.locator(selectors.drawer.pluginAction);
    await expect(actions).toHaveCount(count);
  }

  async clickDrawerPluginAction(pluginId: number, actionId: string): Promise<void> {
    await this.openDrawer();
    const btn = this.page.locator(
      `${selectors.drawer.pluginAction}[data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`
    );
    await btn.click();
  }
```

**Step 3: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: add selectors and AppHelper methods for drawer global actions"
```

---

### Task 7: Write e2e tests for global actions

**Files:**
- Create: `e2e/tests/plugins/global-actions.spec.ts`

**Step 1: Create the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGIN_PATH = path.resolve(__dirname, '../../fixtures/test-plugin.lua');

test.describe('Plugin Global Actions', () => {
  test('should not show plugin section in drawer when no plugins installed', async ({ app }) => {
    await app.expectDrawerPluginActionsHidden();
  });

  test('should show global actions in drawer after plugin install', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    // Reload to refresh plugin UI actions
    await app.page.reload();
    await app.waitForReady();

    await app.expectDrawerPluginActionsVisible();
    await app.expectDrawerPluginActionsCount(3); // simple, options, async
  });

  test('should not show global actions when plugin disabled', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.disablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.expectDrawerPluginActionsHidden();
  });

  test('should execute simple global action and create clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_simple');

    // Should create a new clip
    await app.expectClipCount(1);
    await app.expectClipVisible('global_simple_result.txt');
  });

  test('should show options dialog for global action with options', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_options');

    // Options dialog should open
    const isOpen = await app.isPluginOptionsModalOpen();
    expect(isOpen).toBe(true);

    // Title should show action name only (no clip count)
    const title = await app.page.locator('#plugin-options-title').textContent();
    expect(title).toBe('Test Global Options');
  });

  test('should execute global action with options and create clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    await app.page.reload();
    await app.waitForReady();

    await app.clickDrawerPluginAction(plugin!.id, 'test_global_options');

    // Fill in options and submit
    await app.fillPluginOptionsForm({ name: 'my_creation' });
    await app.submitPluginOptionsForm();

    // Should create a new clip with the provided name
    await app.expectClipCount(1);
    await app.expectClipVisible('my_creation.txt');
  });

  test('should include global_actions in API response', async ({ app }) => {
    const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    await app.enablePlugin(plugin!.id);

    const actions = await app.getPluginUIActions();
    expect(actions.global_actions).toBeDefined();
    expect(actions.global_actions.length).toBe(3);

    const simpleAction = actions.global_actions.find((a: any) => a.id === 'test_global_simple');
    expect(simpleAction).toBeDefined();
    expect(simpleAction.plugin_id).toBe(plugin!.id);
    expect(simpleAction.label).toBe('Test Global Simple');
  });
});
```

**Step 2: Run the tests to verify they pass**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Plugin Global Actions"`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add e2e/tests/plugins/global-actions.spec.ts
git commit -m "test: add e2e tests for plugin global actions in hamburger menu"
```

---

### Task 8: Run full e2e test suite

**Step 1: Run all tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass, including existing plugin UI action tests.

**Step 2: Fix any failures**

If any existing tests fail, investigate and fix them before proceeding.

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve test failures from global actions integration"
```

---

### Task 9: Regenerate Wails bindings

**Step 1: Regenerate bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`
Expected: Bindings regenerated successfully. The `frontend/wailsjs/` directory should have updated TypeScript types that include the new `global_actions` field.

**Step 2: Commit generated files**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for global_actions"
```

Note: This task should be done after Task 2 (Go changes) and before Task 4 (frontend JS changes that use the API). The ordering in execution should be: Tasks 1-2-9-3-4-5-6-7-8.
