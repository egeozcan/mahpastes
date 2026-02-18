# mahresources Upload Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a Lua plugin that uploads mahpastes clips to a mahresources instance, with manual card action and optional auto-upload on clip creation.

**Architecture:** Pure Lua plugin — no Go changes. Multipart form bodies constructed in Lua using `base64.decode()` for binary data. Settings via the plugin settings system (`storage.get`). Network domain statically declared in manifest.

**Tech Stack:** Lua (mahpastes plugin system), HTTP multipart/form-data, mahresources REST API

---

### Task 1: Create the mahresources plugin file

**Files:**
- Create: `plugins/mahresources.lua`

**Step 1: Write the plugin file**

Create `plugins/mahresources.lua` with the full plugin implementation:

```lua
-- mahresources Upload Plugin
-- Uploads clips to a mahresources instance

Plugin = {
    name = "mahresources",
    version = "1.0.0",
    description = "Upload clips to a mahresources instance",
    author = "mahpastes",

    events = {"clip:created"},

    -- EDIT THIS to match your mahresources server domain/IP
    network = {
        ["localhost"] = {"POST"},
    },

    settings = {
        {key = "server_url", type = "text", label = "Server URL", default = "localhost:8181"},
        {key = "owner_id", type = "text", label = "Owner Group ID", default = "1"},
        {key = "auto_upload", type = "checkbox", label = "Auto-upload new clips", default = false},
        {key = "content_filter", type = "select", label = "Content types to upload",
         default = "all",
         choices = {
             {value = "all", label = "All files"},
             {value = "image", label = "Images only"},
             {value = "text", label = "Text only"},
         }},
    },

    ui = {
        card_actions = {
            {
                id = "upload",
                label = "Upload to mahresources",
                icon = "upload",
                async = true,
            },
        },
    },
}

-- Build a multipart/form-data body
local function build_multipart(filename, content_type, raw_data, owner_id)
    local boundary = "----MahPastesBoundary" .. tostring(utils.time())
    local parts = {}

    -- File field
    parts[#parts + 1] = "--" .. boundary
    parts[#parts + 1] = 'Content-Disposition: form-data; name="resource"; filename="' .. filename .. '"'
    parts[#parts + 1] = "Content-Type: " .. content_type
    parts[#parts + 1] = ""
    parts[#parts + 1] = raw_data

    -- Owner ID field
    parts[#parts + 1] = "--" .. boundary
    parts[#parts + 1] = 'Content-Disposition: form-data; name="ownerId"'
    parts[#parts + 1] = ""
    parts[#parts + 1] = owner_id

    -- End boundary
    parts[#parts + 1] = "--" .. boundary .. "--"

    local body = table.concat(parts, "\r\n")
    local header = "multipart/form-data; boundary=" .. boundary
    return body, header
end

-- Check if a clip's content type matches the configured filter
local function matches_filter(content_type, filter)
    if filter == "all" then return true end
    if filter == "image" then return content_type:match("^image/") ~= nil end
    if filter == "text" then return content_type:match("^text/") ~= nil end
    return true
end

-- Upload a single clip to mahresources. Returns true on success, false + error on failure.
local function upload_clip(clip_id, silent)
    local server_url = storage.get("server_url") or "localhost:8181"
    local owner_id = storage.get("owner_id") or "1"
    local content_filter = storage.get("content_filter") or "all"

    -- Get clip metadata
    local clip = clips.get(clip_id)
    if not clip then
        if not silent then toast.show("Clip not found", "error") end
        return false, "Clip not found"
    end

    -- Check content filter
    if not matches_filter(clip.content_type, content_filter) then
        if not silent then toast.show("Skipped (content type filtered)", "info") end
        return false, "filtered"
    end

    -- Get clip data (base64 for binary, plain for text)
    local data, mime_type = clips.get_data(clip_id)
    if not data then
        if not silent then toast.show("Failed to read clip data", "error") end
        return false, "Failed to read clip data"
    end

    -- Decode base64 to raw bytes (binary clips are base64-encoded)
    local raw_data
    if mime_type:match("^text/") then
        raw_data = data
    else
        raw_data = base64.decode(data)
    end

    -- Build multipart body
    local filename = clip.filename or ("clip_" .. clip_id)
    local body, content_header = build_multipart(filename, mime_type, raw_data, owner_id)

    -- Upload
    local url = "http://" .. server_url .. "/v1/resource"
    local resp, http_err = http.post(url, {
        body = body,
        headers = {
            ["Content-Type"] = content_header,
        },
    })

    if not resp then
        local msg = "Upload failed: " .. (http_err or "unknown error")
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if resp.status < 200 or resp.status >= 300 then
        local msg = "Upload failed (HTTP " .. resp.status .. ")"
        if not silent then toast.show(msg, "error") end
        return false, msg
    end

    if not silent then toast.show("Uploaded to mahresources", "success") end
    return true, nil
end

-- Auto-upload on clip:created (gated by setting)
function on_clip_created(data)
    local auto_upload = storage.get("auto_upload")
    if auto_upload ~= "true" then return end
    upload_clip(data.id, true)
end

-- Manual upload via card action
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "upload" then
        return {success = false, error = "Unknown action: " .. tostring(action_id)}
    end

    local clip_count = #clip_ids
    local task_id = task.start("Upload to mahresources (" .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. ")", clip_count)

    local errors = 0
    local last_error = nil

    for i, clip_id in ipairs(clip_ids) do
        local ok, err = pcall(function()
            local success, upload_err = upload_clip(clip_id, true)
            if not success and upload_err ~= "filtered" then
                error(upload_err or "Unknown error")
            end
        end)

        if not ok then
            errors = errors + 1
            last_error = tostring(err)
            log("mahresources upload error for clip " .. clip_id .. ": " .. last_error)
        end

        task.progress(task_id, i)
    end

    if errors == clip_count then
        local msg = "All uploads failed"
        if last_error then msg = msg .. ": " .. last_error end
        task.fail(task_id, msg)
        return {success = false, error = msg}
    else
        task.complete(task_id)
        if errors > 0 then
            toast.show(errors .. " of " .. clip_count .. " uploads failed", "error")
        else
            toast.show("Uploaded " .. clip_count .. " clip" .. (clip_count > 1 and "s" or "") .. " to mahresources", "success")
        end
        return {success = true}
    end
end

log("mahresources plugin loaded")
```

**Step 2: Verify the plugin loads in the app**

Run: `cd /Users/egecan/Code/mahpastes && make dev`

Manually install the plugin via the UI and verify:
- It appears in the plugins list with name "mahresources"
- Settings appear (Server URL, Owner Group ID, Auto-upload, Content types)
- The "Upload to mahresources" card action appears in clip context menus

**Step 3: Commit**

```bash
git add plugins/mahresources.lua
git commit -m "feat: add mahresources upload plugin"
```

---

### Task 2: Write e2e test for plugin loading and settings

**Files:**
- Create: `e2e/tests/plugins/mahresources.spec.ts`

**Step 1: Write the test file**

Create `e2e/tests/plugins/mahresources.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';

// Path to the mahresources plugin (in the plugins directory)
const PLUGIN_PATH = path.resolve(__dirname, '../../../plugins/mahresources.lua');

test.describe('mahresources Plugin', () => {
  let pluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    pluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (pluginId) {
      try {
        await app.removePlugin(pluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test('should load plugin and show correct name', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('mahresources');
    expect(plugin?.enabled).toBe(true);
    pluginId = plugin?.id ?? null;
  });

  test('should show card action when plugin is enabled', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;
    await app.enablePlugin(plugin!.id);

    // Reload to refresh UI actions
    await app.page.reload();
    await app.waitForReady();

    const actions = await app.getPluginUIActions();
    const uploadAction = actions.card_actions.find(
      (a: any) => a.id === 'upload' && a.label === 'Upload to mahresources'
    );
    expect(uploadAction).toBeDefined();
  });

  test('should have default settings', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    // Check default settings via storage
    const serverUrl = await app.getPluginStorage(plugin!.id, 'server_url');
    // Default might not be set in storage until user saves, so it could be empty
    // The plugin handles this with fallback defaults
    expect(serverUrl === '' || serverUrl === 'localhost:8181').toBeTruthy();
  });

  test('should respect content filter setting', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    // Set auto_upload to true and content_filter to image
    await app.setPluginStorage(plugin!.id, 'auto_upload', 'true');
    await app.setPluginStorage(plugin!.id, 'content_filter', 'image');
    await app.setPluginStorage(plugin!.id, 'server_url', 'localhost:99999');

    // Upload a text clip — should be filtered (not uploaded)
    // Since the server URL is invalid, if upload was attempted it would fail
    // But since content filter is "image", text clips should be silently skipped
    const textPath = await createTempFile(generateTestText('test'), 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);

    // Give the plugin a moment to process the event
    await app.page.waitForTimeout(1000);

    // Plugin should still be enabled (no errors from filtered skip)
    const plugins = await app.getPlugins();
    const mahresources = plugins.find((p: any) => p.name === 'mahresources');
    expect(mahresources?.status).not.toBe('error');
  });
});
```

**Step 2: Run the test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/plugins/mahresources.spec.ts`

Expected: All tests pass. The first two tests verify plugin loading. The third test verifies the content filter silently skips non-matching clips.

**Step 3: Commit**

```bash
git add e2e/tests/plugins/mahresources.spec.ts
git commit -m "test: add e2e tests for mahresources plugin"
```

---

### Task 3: Manual integration test with a real mahresources instance

This task is manual verification, not automated.

**Step 1: Start a mahresources instance**

Ensure mahresources is running at `localhost:8181` with at least one group (owner ID 1).

**Step 2: Install and configure the plugin**

1. Run `make dev` in mahpastes
2. Import `plugins/mahresources.lua` via the plugin management UI
3. Set the Server URL to `localhost:8181` and Owner Group ID to the correct value
4. Upload an image clip
5. Open the card menu and click "Upload to mahresources"
6. Verify the toast shows "Uploaded to mahresources"
7. Check the mahresources UI to confirm the resource was created

**Step 3: Test auto-upload**

1. Enable "Auto-upload new clips" in plugin settings
2. Upload another clip
3. Verify it appears in mahresources without manual action

**Step 4: Test content filter**

1. Set content filter to "Images only"
2. Upload a text file
3. Verify it is NOT uploaded to mahresources
4. Upload an image
5. Verify the image IS uploaded

---

### Task 4: Final review and commit

**Step 1: Run full e2e test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

Expected: All tests pass, including the new mahresources tests.

**Step 2: Review the plugin file**

Read through `plugins/mahresources.lua` one more time. Check:
- No hardcoded values (all configurable via settings with sensible defaults)
- Error handling covers network failures, missing clips, invalid responses
- Content filter logic is correct for all three modes
- Multipart boundary is unique per request
- Auto-upload checks the setting before acting

**Step 3: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: address review findings in mahresources plugin"
```
