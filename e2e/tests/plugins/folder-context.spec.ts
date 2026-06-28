import { test, expect } from '../../fixtures/test-fixtures.js';
import * as fs from 'fs/promises';
import * as path from 'path';

// A plugin whose global action records the invocation context it receives and
// tags the clip it creates into the active folder (when one is provided).
const FOLDER_CONTEXT_PLUGIN = `
Plugin = {
  name = "Folder Context Plugin",
  version = "1.0.0",
  events = {"app:startup"},
  ui = {
    global_actions = {
      {id = "folder_create", label = "Folder Create", icon = "sparkles"},
      -- async = true runs the handler detached, in a background goroutine.
      {id = "folder_create_async", label = "Folder Create Async", icon = "refresh", async = true},
    },
  },
}
function on_startup() end

local function tag_into_folder(clip_id, context)
  if context.folder_tag_id and context.folder_tag_id > 0 then
    tags.add_to_clip(context.folder_tag_id, clip_id)
  end
end

function on_ui_action(action_id, clip_ids, options, context)
  context = context or {}
  -- Record what context was delivered so the test can assert on it.
  storage.set("ctx_folder_tag_id", tostring(context.folder_tag_id or 0))
  storage.set("ctx_folder_tag_path", context.folder_tag_path or "")
  if action_id == "folder_create" then
    local new_clip = clips.create({name = "folder_created.txt", data = "created", mime_type = "text/plain"})
    tag_into_folder(new_clip.id, context)
    return {success = true, result_clip_id = new_clip.id}
  end
  if action_id == "folder_create_async" then
    -- Mirror fal.ai: use the task API and tag the result before task.complete,
    -- which drives the gallery refresh once the detached work finishes.
    local task_id = task.start("Folder Create Async", 1)
    local new_clip = clips.create({name = "folder_created_async.txt", data = "created", mime_type = "text/plain"})
    tag_into_folder(new_clip.id, context)
    task.progress(task_id, 1)
    task.complete(task_id)
    return {success = true, result_clip_id = new_clip.id}
  end
  return {success = false, error = "unknown action"}
end
`;

async function installFolderContextPlugin(app: any, tempDir: string) {
  const pluginPath = path.join(tempDir, 'folder-context-plugin.lua');
  await fs.writeFile(pluginPath, FOLDER_CONTEXT_PLUGIN);
  const plugin = await app.importPluginFromPath(pluginPath);
  expect(plugin).not.toBeNull();
  await app.enablePlugin(plugin!.id);
  await app.page.reload();
  await app.waitForReady();
  return plugin!;
}

async function readClipTags(app: any, filename: string): Promise<string[]> {
  return app.page.evaluate(async (fn: string) => {
    // @ts-ignore - Wails runtime
    const clips = await window.go.main.App.GetClips(false, [], [], '', '');
    const clip = clips.find((c: any) => c.filename === fn);
    if (!clip) return [];
    // @ts-ignore - Wails runtime
    const tags = await window.go.main.App.GetClipTags(clip.id);
    return tags.map((t: any) => t.name);
  }, filename);
}

async function readPluginStorage(app: any, pluginId: number, key: string): Promise<string> {
  return app.page.evaluate(async ({ id, k }: { id: number; k: string }) => {
    // @ts-ignore - Wails runtime
    return await window.go.main.PluginService.GetPluginStorage(id, k);
  }, { id: pluginId, k: key });
}

test.describe('Plugin Folder Context', () => {
  test.afterEach(async ({ app }) => {
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('global action inside a folder receives the folder tag path and tags its result', async ({ app, tempDir }) => {
    await app.createTag('work/client1');
    const plugin = await installFolderContextPlugin(app, tempDir);

    await app.enterFolder('work/client1');
    await app.clickDrawerPluginAction(plugin.id, 'folder_create');

    // The created clip lands in (and stays visible inside) the active folder.
    await app.expectClipVisible('folder_created.txt');
    await app.expectClipCount(1);

    // The plugin received the full folder tag path...
    expect(await readPluginStorage(app, plugin.id, 'ctx_folder_tag_path')).toBe('work/client1');

    // ...and used it to tag the new clip into the folder.
    expect(await readClipTags(app, 'folder_created.txt')).toContain('work/client1');
  });

  test('async (detached) action inside a folder still tags its result into the folder', async ({ app, tempDir }) => {
    await app.createTag('work/client1');
    const plugin = await installFolderContextPlugin(app, tempDir);

    await app.enterFolder('work/client1');
    await app.clickDrawerPluginAction(plugin.id, 'folder_create_async');

    // The action runs detached; the folder context was snapshotted at trigger
    // time and travels into the background goroutine. task.complete drives the
    // gallery refresh, so the tagged result appears inside the folder.
    await app.expectClipVisible('folder_created_async.txt');
    await app.expectClipCount(1);

    expect(await readPluginStorage(app, plugin.id, 'ctx_folder_tag_path')).toBe('work/client1');
    expect(await readClipTags(app, 'folder_created_async.txt')).toContain('work/client1');
  });

  test('global action outside folder view receives no folder context and leaves the clip untagged', async ({ app, tempDir }) => {
    const plugin = await installFolderContextPlugin(app, tempDir);

    // Not in folder view — run the action from the normal all-clips view.
    await app.clickDrawerPluginAction(plugin.id, 'folder_create');

    await app.expectClipVisible('folder_created.txt');

    // No folder context was delivered.
    expect(await readPluginStorage(app, plugin.id, 'ctx_folder_tag_id')).toBe('0');

    // The clip is untagged.
    expect(await readClipTags(app, 'folder_created.txt')).toEqual([]);
  });
});
