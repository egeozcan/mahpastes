# Plugin Toast Notifications & Settings UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add toast notification API and settings UI for plugins, allowing plugins to display notifications and declare configurable settings.

**Architecture:** Toast API emits Wails events that frontend listens for. Settings are declared in plugin manifest and rendered in Plugin Manager UI, using existing storage API for persistence.

**Tech Stack:** Go (Wails backend), Lua (plugin sandbox), Vanilla JavaScript (frontend), Playwright (e2e tests)

---

## Task 1: Extend showToast() to support types

**Files:**
- Modify: `frontend/js/utils.js:39-53`

**Step 1: Write the failing test**

Create `e2e/tests/plugins/toast.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Plugin Toast', () => {
  test('showToast displays info type with default styling', async ({ app }) => {
    // Call showToast directly via evaluate
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Test info message', 'info');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    await expect(toast).toHaveText('Test info message');
    // Check it has stone-800 background (default)
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });

  test('showToast displays success type with green styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Success message', 'success');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasSuccessStyle = await toast.evaluate(el => el.classList.contains('bg-emerald-600'));
    expect(hasSuccessStyle).toBe(true);
  });

  test('showToast displays error type with red styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Error message', 'error');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasErrorStyle = await toast.evaluate(el => el.classList.contains('bg-red-600'));
    expect(hasErrorStyle).toBe(true);
  });

  test('showToast defaults to info type when no type provided', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Default message');
    });

    const toast = app.page.locator('#toast');
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/plugins/toast.spec.ts --grep "showToast displays info type" -v`
Expected: FAIL - showToast doesn't support type parameter yet

**Step 3: Update frontend/index.html toast element**

The toast element needs data attributes for dynamic styling. Check `frontend/index.html` for the toast element and ensure it has an id.

**Step 4: Write implementation in utils.js**

Replace `showToast` function in `frontend/js/utils.js`:

```javascript
function showToast(message, type = 'info') {
    const toast = document.getElementById('toast');

    // Color mapping
    const colors = {
        info: 'bg-stone-800',
        success: 'bg-emerald-600',
        error: 'bg-red-600'
    };

    // Remove any existing color classes
    toast.classList.remove('bg-stone-800', 'bg-emerald-600', 'bg-red-600');

    // Add the appropriate color class
    const colorClass = colors[type] || colors.info;
    toast.classList.add(colorClass);

    toast.textContent = message;
    toast.classList.remove('translate-x-full', 'opacity-0');
    toast.classList.add('translate-x-0', 'opacity-100');

    if (window.toastTimeout) {
        clearTimeout(window.toastTimeout);
    }

    window.toastTimeout = setTimeout(() => {
        toast.classList.remove('translate-x-0', 'opacity-100');
        toast.classList.add('translate-x-full', 'opacity-0');
    }, 3000);
}
```

**Step 5: Run test to verify it passes**

Run: `cd e2e && npx playwright test tests/plugins/toast.spec.ts -v`
Expected: PASS

**Step 6: Commit**

```bash
git add frontend/js/utils.js e2e/tests/plugins/toast.spec.ts
git commit -m "feat: extend showToast to support info/success/error types"
```

---

## Task 2: Create Toast API backend

**Files:**
- Create: `plugin/api_toast.go`
- Modify: `plugin/manager.go:114-131` (API registration section)

**Step 1: Create api_toast.go**

```go
package plugin

import (
	"context"
	"html"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxToastMessageLength = 200
	toastRateLimit        = 5 // per minute
)

// ToastAPI provides toast notification functionality for plugins
type ToastAPI struct {
	ctx      context.Context
	pluginID int64
	// Rate limiting
	mu         sync.Mutex
	callTimes  []time.Time
}

// NewToastAPI creates a new toast API instance
func NewToastAPI(ctx context.Context, pluginID int64) *ToastAPI {
	return &ToastAPI{
		ctx:       ctx,
		pluginID:  pluginID,
		callTimes: make([]time.Time, 0, toastRateLimit),
	}
}

// Register adds the toast module to the Lua state
func (t *ToastAPI) Register(L *lua.LState) {
	toastMod := L.NewTable()
	toastMod.RawSetString("show", L.NewFunction(t.show))
	L.SetGlobal("toast", toastMod)
}

func (t *ToastAPI) show(L *lua.LState) int {
	message := L.CheckString(1)
	toastType := L.OptString(2, "info")

	// Validate type
	validTypes := map[string]bool{"info": true, "success": true, "error": true}
	if !validTypes[toastType] {
		toastType = "info"
	}

	// Truncate message if too long
	if len(message) > maxToastMessageLength {
		message = message[:maxToastMessageLength-3] + "..."
	}

	// HTML escape the message
	message = html.EscapeString(message)

	// Rate limiting
	t.mu.Lock()
	now := time.Now()
	// Remove calls older than 1 minute
	cutoff := now.Add(-time.Minute)
	validCalls := make([]time.Time, 0, len(t.callTimes))
	for _, ct := range t.callTimes {
		if ct.After(cutoff) {
			validCalls = append(validCalls, ct)
		}
	}
	t.callTimes = validCalls

	// Check if rate limited
	if len(t.callTimes) >= toastRateLimit {
		t.mu.Unlock()
		// Silently drop - don't error
		L.Push(lua.LFalse)
		return 1
	}

	t.callTimes = append(t.callTimes, now)
	t.mu.Unlock()

	// Emit Wails event
	runtime.EventsEmit(t.ctx, "plugin:toast", map[string]string{
		"message": message,
		"type":    toastType,
	})

	L.Push(lua.LTrue)
	return 1
}
```

**Step 2: Register ToastAPI in manager.go**

In `plugin/manager.go`, in the `loadPlugin` function, after registering other APIs (around line 131), add:

```go
	toastAPI := NewToastAPI(m.ctx, p.ID)
	toastAPI.Register(sandbox.GetState())
```

**Step 3: Run test to verify build**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/plugin-toast-settings && go build ./...`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add plugin/api_toast.go plugin/manager.go
git commit -m "feat: add toast API for plugins with rate limiting"
```

---

## Task 3: Listen for plugin:toast events in frontend

**Files:**
- Modify: `frontend/js/app.js:281-291` (after window.addEventListener('load'))

**Step 1: Add Wails event listener**

In `frontend/js/app.js`, inside the `window.addEventListener('load', ...)` callback, after the existing initialization code, add:

```javascript
    // Listen for plugin toast events
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn("plugin:toast", (data) => {
            if (data && data.message) {
                showToast(data.message, data.type || 'info');
            }
        });
    }
```

**Step 2: Write e2e test for plugin toast**

Add to `e2e/tests/plugins/toast.spec.ts`:

```typescript
test.describe('Plugin Toast API', () => {
  test('plugin can trigger toast via toast.show()', async ({ app }) => {
    // Create a test plugin that calls toast.show on startup
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'toast-test.lua');

    // Import the plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();

    // Wait for toast to appear (plugin shows toast on startup)
    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast).toContainText('Toast test plugin loaded');
  });
});
```

**Step 3: Create test plugin**

Create `e2e/test-plugins/toast-test.lua`:

```lua
Plugin = {
  name = "Toast Test",
  version = "1.0.0",
  description = "Tests toast API",
  author = "Test",
  events = {"app:startup"}
}

function on_startup()
  toast.show("Toast test plugin loaded", "success")
end
```

**Step 4: Run tests**

Run: `cd e2e && npx playwright test tests/plugins/toast.spec.ts -v`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/js/app.js e2e/tests/plugins/toast.spec.ts e2e/test-plugins/toast-test.lua
git commit -m "feat: connect frontend to plugin:toast events"
```

---

## Task 4: Parse settings from plugin manifest

**Files:**
- Modify: `plugin/manifest.go`

**Step 1: Add SettingField struct**

Add to `plugin/manifest.go` after the `Schedule` struct (around line 32):

```go
// SettingField represents a plugin setting declaration
type SettingField struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
}
```

**Step 2: Add Settings to Manifest struct**

Update the `Manifest` struct to include Settings:

```go
type Manifest struct {
	Name        string
	Version     string
	Description string
	Author      string
	Network     map[string][]string
	Filesystem  FilesystemPerms
	Events      []string
	Schedules   []Schedule
	Settings    []SettingField  // Add this line
}
```

**Step 3: Add extractSettings function**

Add after `extractSchedules` function:

```go
// extractSettings extracts settings declarations from the manifest
// Format: settings = { {key = "api_key", type = "password", label = "API Key"}, ... }
func extractSettings(block string) []SettingField {
	var result []SettingField

	// Find the settings block
	settingsPattern := regexp.MustCompile(`settings\s*=\s*\{`)
	loc := settingsPattern.FindStringIndex(block)
	if loc == nil {
		return result
	}

	// Extract the settings block content
	start := loc[1] - 1
	settingsBlock := extractNestedBrace(block[start:])
	if settingsBlock == "" {
		return result
	}

	// Find each setting entry: {key = "...", type = "...", ...}
	depth := 0
	entryStart := -1

	for i := 1; i < len(settingsBlock)-1; i++ {
		c := settingsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := settingsBlock[entryStart : i+1]
				setting := parseSettingEntry(entry)
				if setting.Key != "" && setting.Type != "" && setting.Label != "" {
					// Validate type
					validTypes := map[string]bool{
						"text": true, "password": true, "checkbox": true, "select": true,
					}
					if validTypes[setting.Type] {
						// Validate select has options
						if setting.Type == "select" && len(setting.Options) == 0 {
							// Skip invalid select without options
							entryStart = -1
							continue
						}
						result = append(result, setting)
					}
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseSettingEntry parses a single setting entry
func parseSettingEntry(entry string) SettingField {
	var setting SettingField

	// Extract key
	setting.Key = extractStringField(entry, "key")

	// Extract type
	setting.Type = extractStringField(entry, "type")

	// Extract label
	setting.Label = extractStringField(entry, "label")

	// Extract description (optional)
	setting.Description = extractStringField(entry, "description")

	// Extract default (can be string, bool, or number)
	setting.Default = extractDefaultValue(entry)

	// Extract options for select type
	setting.Options = extractStringArray(entry, "options")

	return setting
}

// extractDefaultValue extracts the default value which can be string, bool, or absent
func extractDefaultValue(entry string) any {
	// Try string first
	strDefault := extractStringField(entry, "default")
	if strDefault != "" {
		return strDefault
	}

	// Try boolean
	boolPattern := regexp.MustCompile(`default\s*=\s*(true|false)`)
	boolMatches := boolPattern.FindStringSubmatch(entry)
	if len(boolMatches) >= 2 {
		return boolMatches[1] == "true"
	}

	return nil
}
```

**Step 4: Call extractSettings in ParseManifest**

In the `ParseManifest` function, before `return manifest, nil`, add:

```go
	// Parse settings
	manifest.Settings = extractSettings(pluginBlock)
```

**Step 5: Write unit test**

Create `plugin/manifest_test.go` if it doesn't exist, or add:

```go
func TestParseManifestWithSettings(t *testing.T) {
	source := `
Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  settings = {
    {key = "api_key", type = "password", label = "API Key", description = "Your API key"},
    {key = "endpoint", type = "text", label = "Endpoint", default = "https://api.example.com"},
    {key = "enabled", type = "checkbox", label = "Enable feature", default = true},
    {key = "mode", type = "select", label = "Mode", options = {"fast", "slow"}, default = "fast"}
  }
}
`
	manifest, err := ParseManifest(source)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	if len(manifest.Settings) != 4 {
		t.Errorf("Expected 4 settings, got %d", len(manifest.Settings))
	}

	// Check first setting
	if manifest.Settings[0].Key != "api_key" {
		t.Errorf("Expected key 'api_key', got '%s'", manifest.Settings[0].Key)
	}
	if manifest.Settings[0].Type != "password" {
		t.Errorf("Expected type 'password', got '%s'", manifest.Settings[0].Type)
	}

	// Check default values
	if manifest.Settings[1].Default != "https://api.example.com" {
		t.Errorf("Expected default 'https://api.example.com', got '%v'", manifest.Settings[1].Default)
	}
	if manifest.Settings[2].Default != true {
		t.Errorf("Expected default true, got '%v'", manifest.Settings[2].Default)
	}

	// Check select options
	if len(manifest.Settings[3].Options) != 2 {
		t.Errorf("Expected 2 options, got %d", len(manifest.Settings[3].Options))
	}
}
```

**Step 6: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/plugin-toast-settings && go test ./plugin/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add plugin/manifest.go plugin/manifest_test.go
git commit -m "feat: parse settings from plugin manifest"
```

---

## Task 5: Expose settings in PluginInfo

**Files:**
- Modify: `plugin_service.go`

**Step 1: Add Settings to PluginInfo struct**

In `plugin_service.go`, update `PluginInfo` struct:

```go
type PluginInfo struct {
	ID          int64                    `json:"id"`
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Description string                   `json:"description"`
	Author      string                   `json:"author"`
	Enabled     bool                     `json:"enabled"`
	Status      string                   `json:"status"`
	Events      []string                 `json:"events"`
	Settings    []plugin.SettingField    `json:"settings"`  // Add this
}
```

**Step 2: Update pluginToInfo helper**

Update the `pluginToInfo` function:

```go
func pluginToInfo(p *plugin.Plugin) *PluginInfo {
	info := &PluginInfo{
		ID:      p.ID,
		Name:    p.Name,
		Version: p.Version,
		Enabled: p.Enabled,
		Status:  p.Status,
	}
	if p.Manifest != nil {
		info.Description = p.Manifest.Description
		info.Author = p.Manifest.Author
		info.Events = p.Manifest.Events
		info.Settings = p.Manifest.Settings  // Add this
	}
	return info
}
```

**Step 3: Update GetPlugins to include settings**

In the `GetPlugins` function, in the loop where we get info from loaded plugins, add:

```go
				if loaded.ID == p.ID && loaded.Manifest != nil {
					p.Description = loaded.Manifest.Description
					p.Author = loaded.Manifest.Author
					p.Events = loaded.Manifest.Events
					p.Settings = loaded.Manifest.Settings  // Add this
					break
				}
```

**Step 4: Run build**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/plugin-toast-settings && go build ./...`
Expected: Build succeeds

**Step 5: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/plugin-toast-settings && ~/go/bin/wails generate module`

**Step 6: Commit**

```bash
git add plugin_service.go frontend/wailsjs/
git commit -m "feat: expose plugin settings in PluginInfo for frontend"
```

---

## Task 6: Add GetAllPluginStorage method

**Files:**
- Modify: `plugin_service.go`

**Step 1: Add method to get all storage for a plugin**

Add to `plugin_service.go`:

```go
// GetAllPluginStorage retrieves all storage key-value pairs for a plugin
func (s *PluginService) GetAllPluginStorage(pluginID int64) (map[string]string, error) {
	if s.app.db == nil {
		return map[string]string{}, nil
	}

	rows, err := s.app.db.Query(`
		SELECT key, value FROM plugin_storage WHERE plugin_id = ?
	`, pluginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		result[key] = string(value)
	}

	return result, nil
}
```

**Step 2: Regenerate Wails bindings**

Run: `~/go/bin/wails generate module`

**Step 3: Commit**

```bash
git add plugin_service.go frontend/wailsjs/
git commit -m "feat: add GetAllPluginStorage for loading plugin settings"
```

---

## Task 7: Render settings UI in Plugin Manager

**Files:**
- Modify: `frontend/js/plugins.js`

**Step 1: Add renderSettingsSection function**

Add after `createPluginCard` function:

```javascript
// --- Render Settings Section ---
function renderSettingsSection(settings, pluginId, storageValues) {
    if (!settings || settings.length === 0) {
        return '';
    }

    const fields = settings.map(field => {
        const currentValue = storageValues[field.key];
        const displayValue = currentValue !== undefined ? currentValue : (field.default || '');

        return renderSettingField(field, displayValue, pluginId);
    }).join('');

    return `
        <div data-settings-section data-plugin-id="${pluginId}">
            <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Settings</h4>
            <div class="space-y-3">
                ${fields}
            </div>
        </div>
    `;
}

function renderSettingField(field, currentValue, pluginId) {
    const description = field.description
        ? `<p class="text-[10px] text-stone-400 mt-1">${escapeHTML(field.description)}</p>`
        : '';

    switch (field.type) {
        case 'text':
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <input type="text"
                           class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                           value="${escapeHTML(currentValue || '')}"
                           placeholder="${escapeHTML(field.default || '')}"
                           data-plugin-id="${pluginId}"
                           data-setting-key="${escapeHTML(field.key)}"
                           data-setting-type="text">
                    ${description}
                </div>
            `;

        case 'password':
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <div class="relative">
                        <input type="password"
                               class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 pr-8 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                               value="${escapeHTML(currentValue || '')}"
                               data-plugin-id="${pluginId}"
                               data-setting-key="${escapeHTML(field.key)}"
                               data-setting-type="password">
                        <button type="button"
                                class="absolute right-2 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-600"
                                data-action="toggle-password">
                            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                            </svg>
                        </button>
                    </div>
                    ${description}
                </div>
            `;

        case 'checkbox':
            const isChecked = currentValue === 'true' || currentValue === true || (currentValue === '' && field.default === true);
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="flex items-center gap-2 cursor-pointer">
                        <input type="checkbox"
                               class="w-4 h-4 rounded border-stone-300 text-stone-800 focus:ring-stone-400/20"
                               ${isChecked ? 'checked' : ''}
                               data-plugin-id="${pluginId}"
                               data-setting-key="${escapeHTML(field.key)}"
                               data-setting-type="checkbox">
                        <span class="text-[11px] font-medium text-stone-600">${escapeHTML(field.label)}</span>
                    </label>
                    ${description}
                </div>
            `;

        case 'select':
            const options = (field.options || []).map(opt => {
                const selected = currentValue === opt || (currentValue === '' && field.default === opt);
                return `<option value="${escapeHTML(opt)}" ${selected ? 'selected' : ''}>${escapeHTML(opt)}</option>`;
            }).join('');
            return `
                <div class="setting-field" data-key="${escapeHTML(field.key)}">
                    <label class="block text-[11px] font-medium text-stone-600 mb-1">${escapeHTML(field.label)}</label>
                    <select class="block w-full border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                            data-plugin-id="${pluginId}"
                            data-setting-key="${escapeHTML(field.key)}"
                            data-setting-type="select">
                        ${options}
                    </select>
                    ${description}
                </div>
            `;

        default:
            return '';
    }
}
```

**Step 2: Update createPluginCard to include settings**

In `createPluginCard`, find the Events Section comment and add settings section before it. The card needs to load storage values when expanded.

Update the expanded details section (around line 102-132) to include:

```javascript
                <!-- Settings Section (loaded dynamically) -->
                <div data-settings-placeholder data-plugin-id="${plugin.id}"></div>

                <!-- Events Section -->
```

**Step 3: Update togglePluginExpand to load settings**

Update `togglePluginExpand` function to load and render settings:

```javascript
async function togglePluginExpand(pluginId) {
    if (expandedPluginId === pluginId) {
        expandedPluginId = null;
    } else {
        expandedPluginId = pluginId;
    }

    // Re-render to update expanded state
    renderPluginsList();

    // Load permissions and settings for newly expanded plugin
    if (expandedPluginId) {
        const card = pluginsList.querySelector(`li[data-id="${expandedPluginId}"]`);
        if (card) {
            await loadPluginPermissions(expandedPluginId, card);
            await loadPluginSettings(expandedPluginId, card);
        }
    }
}
```

**Step 4: Add loadPluginSettings function**

```javascript
// --- Load Plugin Settings ---
async function loadPluginSettings(pluginId, cardElement) {
    const placeholder = cardElement.querySelector('[data-settings-placeholder]');
    if (!placeholder) return;

    // Find the plugin in cache
    const plugin = pluginsCache.find(p => p.id === pluginId);
    if (!plugin || !plugin.settings || plugin.settings.length === 0) {
        placeholder.innerHTML = '';
        return;
    }

    try {
        // Load current storage values
        const storageValues = await window.go.main.PluginService.GetAllPluginStorage(pluginId);

        // Render settings section
        placeholder.innerHTML = renderSettingsSection(plugin.settings, pluginId, storageValues || {});

        // Add event listeners for setting changes
        setupSettingListeners(cardElement, pluginId);
    } catch (error) {
        console.error('Failed to load plugin settings:', error);
        placeholder.innerHTML = '<span class="text-red-500 text-[11px]">Failed to load settings</span>';
    }
}

// --- Setup Setting Event Listeners ---
let settingDebounceTimers = {};

function setupSettingListeners(cardElement, pluginId) {
    // Text and password inputs
    cardElement.querySelectorAll('input[data-setting-type="text"], input[data-setting-type="password"]').forEach(input => {
        input.addEventListener('input', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.value;
            debounceSaveSetting(pluginId, key, value);
        });
    });

    // Checkboxes
    cardElement.querySelectorAll('input[data-setting-type="checkbox"]').forEach(input => {
        input.addEventListener('change', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.checked ? 'true' : 'false';
            saveSetting(pluginId, key, value);
        });
    });

    // Selects
    cardElement.querySelectorAll('select[data-setting-type="select"]').forEach(select => {
        select.addEventListener('change', (e) => {
            const key = e.target.dataset.settingKey;
            const value = e.target.value;
            saveSetting(pluginId, key, value);
        });
    });

    // Password toggle buttons
    cardElement.querySelectorAll('[data-action="toggle-password"]').forEach(btn => {
        btn.addEventListener('click', (e) => {
            const input = e.target.closest('.relative').querySelector('input');
            if (input) {
                input.type = input.type === 'password' ? 'text' : 'password';
            }
        });
    });
}

function debounceSaveSetting(pluginId, key, value) {
    const timerId = `${pluginId}-${key}`;
    if (settingDebounceTimers[timerId]) {
        clearTimeout(settingDebounceTimers[timerId]);
    }
    settingDebounceTimers[timerId] = setTimeout(() => {
        saveSetting(pluginId, key, value);
        delete settingDebounceTimers[timerId];
    }, 300);
}

async function saveSetting(pluginId, key, value) {
    try {
        await window.go.main.PluginService.SetPluginStorage(pluginId, key, value);
    } catch (error) {
        console.error('Failed to save setting:', error);
        showToast('Failed to save setting', 'error');
    }
}
```

**Step 5: Run to verify UI works**

Run: `cd /Users/egecan/Code/mahpastes/.worktrees/plugin-toast-settings && ~/go/bin/wails dev`

Test manually:
1. Import a plugin with settings
2. Open Plugin Manager
3. Expand plugin card
4. Verify settings section appears
5. Change a setting, verify it saves

**Step 6: Commit**

```bash
git add frontend/js/plugins.js
git commit -m "feat: render plugin settings UI in Plugin Manager"
```

---

## Task 8: Write e2e tests for settings

**Files:**
- Create: `e2e/tests/plugins/settings.spec.ts`
- Create: `e2e/test-plugins/settings-test.lua`

**Step 1: Create test plugin with settings**

Create `e2e/test-plugins/settings-test.lua`:

```lua
Plugin = {
  name = "Settings Test",
  version = "1.0.0",
  description = "Tests settings API",
  author = "Test",
  events = {"app:startup"},
  settings = {
    {key = "api_key", type = "password", label = "API Key", description = "Your API key"},
    {key = "endpoint", type = "text", label = "Endpoint URL", default = "https://api.example.com"},
    {key = "enabled", type = "checkbox", label = "Enable feature", default = true},
    {key = "mode", type = "select", label = "Mode", options = {"fast", "balanced", "thorough"}, default = "balanced"}
  }
}

function on_startup()
  -- Read settings and store to indicate they're accessible
  local api_key = storage.get("api_key")
  local endpoint = storage.get("endpoint")
  local enabled = storage.get("enabled")
  local mode = storage.get("mode")

  storage.set("settings_read", "true")
  storage.set("api_key_value", api_key or "nil")
  storage.set("endpoint_value", endpoint or "nil")
  storage.set("enabled_value", enabled or "nil")
  storage.set("mode_value", mode or "nil")
end
```

**Step 2: Create settings e2e tests**

Create `e2e/tests/plugins/settings.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Settings', () => {
  let settingsPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    settingsPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (settingsPluginId) {
      try {
        await app.removePlugin(settingsPluginId);
      } catch {}
    }
  });

  test('settings section renders when plugin has settings', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    settingsPluginId = plugin?.id ?? null;

    // Open plugins modal and expand plugin card
    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Check settings section exists
    const settingsSection = card.locator('[data-settings-section]');
    await expect(settingsSection).toBeVisible();

    // Check all 4 settings fields are present
    await expect(card.locator('[data-setting-key="api_key"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="endpoint"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="enabled"]')).toBeVisible();
    await expect(card.locator('[data-setting-key="mode"]')).toBeVisible();
  });

  test('text input saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Fill in text input
    const endpointInput = card.locator('[data-setting-key="endpoint"]');
    await endpointInput.fill('https://custom.api.com');
    await app.page.waitForTimeout(500); // Wait for debounce

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'endpoint');
    expect(value).toBe('https://custom.api.com');
  });

  test('checkbox saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Uncheck the checkbox (default is true)
    const checkbox = card.locator('[data-setting-key="enabled"]');
    await checkbox.uncheck();
    await app.page.waitForTimeout(300);

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'enabled');
    expect(value).toBe('false');
  });

  test('select saves value to storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Change select value
    const select = card.locator('[data-setting-key="mode"]');
    await select.selectOption('thorough');
    await app.page.waitForTimeout(300);

    // Verify storage was updated
    const value = await app.getPluginStorage(plugin!.id, 'mode');
    expect(value).toBe('thorough');
  });

  test('plugin can read settings via storage.get()', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'settings-test.lua');

    // First set some storage values before importing
    // Import plugin first to get ID
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    // Set storage values
    await app.page.evaluate(async ({ id }) => {
      // @ts-ignore
      await window.go.main.PluginService.SetPluginStorage(id, 'api_key', 'test-key-123');
      await window.go.main.PluginService.SetPluginStorage(id, 'mode', 'fast');
    }, { id: plugin!.id });

    // Re-enable plugin to trigger on_startup with new values
    await app.disablePlugin(plugin!.id);
    await app.enablePlugin(plugin!.id);
    await app.page.waitForTimeout(500);

    // Check that plugin read the settings
    const settingsRead = await app.getPluginStorage(plugin!.id, 'settings_read');
    expect(settingsRead).toBe('true');

    const apiKeyValue = await app.getPluginStorage(plugin!.id, 'api_key_value');
    expect(apiKeyValue).toBe('test-key-123');

    const modeValue = await app.getPluginStorage(plugin!.id, 'mode_value');
    expect(modeValue).toBe('fast');
  });

  test('settings section not shown for plugin without settings', async ({ app }) => {
    // Use existing event-tracker plugin which has no settings
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    settingsPluginId = plugin?.id ?? null;

    await app.openPluginsModal();
    const card = app.page.locator(`[data-testid="plugin-card-${plugin!.id}"]`);
    await card.locator('[data-action="toggle-expand"]').click();
    await app.page.waitForTimeout(500);

    // Settings section should not exist
    const settingsSection = card.locator('[data-settings-section]');
    await expect(settingsSection).not.toBeVisible();
  });
});
```

**Step 3: Run tests**

Run: `cd e2e && npx playwright test tests/plugins/settings.spec.ts -v`
Expected: PASS

**Step 4: Commit**

```bash
git add e2e/tests/plugins/settings.spec.ts e2e/test-plugins/settings-test.lua
git commit -m "test: add e2e tests for plugin settings"
```

---

## Task 9: Add toast rate limiting e2e test

**Files:**
- Modify: `e2e/tests/plugins/toast.spec.ts`
- Create: `e2e/test-plugins/toast-spam.lua`

**Step 1: Create spam test plugin**

Create `e2e/test-plugins/toast-spam.lua`:

```lua
Plugin = {
  name = "Toast Spam Test",
  version = "1.0.0",
  description = "Tests toast rate limiting",
  author = "Test",
  events = {"app:startup"}
}

function on_startup()
  -- Try to send 10 toasts rapidly
  local success_count = 0
  for i = 1, 10 do
    local result = toast.show("Toast " .. i, "info")
    if result then
      success_count = success_count + 1
    end
  end
  storage.set("success_count", tostring(success_count))
end
```

**Step 2: Add rate limiting test**

Add to `e2e/tests/plugins/toast.spec.ts`:

```typescript
  test('toast API rate limits to 5 per minute', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'toast-spam.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();

    // Wait for plugin to run
    await app.page.waitForTimeout(1000);

    // Check that only 5 toasts were allowed
    const successCount = await app.getPluginStorage(plugin!.id, 'success_count');
    expect(parseInt(successCount)).toBe(5);

    // Cleanup
    await app.removePlugin(plugin!.id);
  });
```

**Step 3: Run test**

Run: `cd e2e && npx playwright test tests/plugins/toast.spec.ts --grep "rate limits" -v`
Expected: PASS

**Step 4: Commit**

```bash
git add e2e/tests/plugins/toast.spec.ts e2e/test-plugins/toast-spam.lua
git commit -m "test: add toast rate limiting e2e test"
```

---

## Task 10: Update selectors.ts

**Files:**
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add plugin settings selectors**

Add to the plugins section in `selectors.ts`:

```typescript
  plugins: {
    // ... existing selectors ...
    settingsSection: '[data-settings-section]',
    settingField: (key: string) => `[data-setting-key="${key}"]`,
    settingInput: (key: string) => `[data-setting-key="${key}"]`,
  },
```

**Step 2: Commit**

```bash
git add e2e/helpers/selectors.ts
git commit -m "chore: add plugin settings selectors"
```

---

## Task 11: Final integration test and cleanup

**Step 1: Run all plugin tests**

Run: `cd e2e && npx playwright test tests/plugins/ -v`
Expected: All tests pass

**Step 2: Run full test suite**

Run: `cd e2e && npm test`
Expected: All tests pass (minus the pre-existing flaky test)

**Step 3: Final commit**

```bash
git add -A
git commit -m "feat: complete plugin toast and settings implementation"
```

---

## Summary

This implementation plan adds:

1. **Toast API** - `toast.show(message, type)` with rate limiting
2. **Settings parsing** - Extract settings declarations from plugin manifest
3. **Settings UI** - Render text/password/checkbox/select fields in Plugin Manager
4. **Storage integration** - Settings use existing plugin storage API

**Files created:**
- `plugin/api_toast.go`
- `e2e/tests/plugins/toast.spec.ts`
- `e2e/tests/plugins/settings.spec.ts`
- `e2e/test-plugins/toast-test.lua`
- `e2e/test-plugins/settings-test.lua`
- `e2e/test-plugins/toast-spam.lua`

**Files modified:**
- `frontend/js/utils.js` - Extended showToast with types
- `frontend/js/app.js` - Added plugin:toast event listener
- `frontend/js/plugins.js` - Added settings UI rendering
- `plugin/manifest.go` - Added settings parsing
- `plugin/manager.go` - Register toast API
- `plugin_service.go` - Added Settings to PluginInfo, GetAllPluginStorage
- `e2e/helpers/selectors.ts` - Added settings selectors
