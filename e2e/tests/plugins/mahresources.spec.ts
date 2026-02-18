import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to the mahresources plugin
const PLUGIN_PATH = path.resolve(__dirname, '../../../plugins/mahresources.lua');

test.describe('mahresources Plugin', () => {
  let pluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
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
    // Default might not be set in storage until user saves, so it could be empty.
    // The plugin handles this with fallback defaults in the Lua code.
    expect(serverUrl === '' || serverUrl === 'localhost:8181').toBeTruthy();
  });

  test('should respect content filter setting', async ({ app }) => {
    const plugin = await app.importPluginFromPath(PLUGIN_PATH);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    // Set auto_upload to true and content_filter to image via Wails API
    await app.page.evaluate(async ({ id }) => {
      // @ts-ignore - Wails runtime
      await window.go.main.PluginService.SetPluginStorage(id, 'auto_upload', 'true');
      // @ts-ignore
      await window.go.main.PluginService.SetPluginStorage(id, 'content_filter', 'image');
      // @ts-ignore - Use invalid server so any attempted upload would fail visibly
      await window.go.main.PluginService.SetPluginStorage(id, 'server_url', 'localhost:99999');
    }, { id: plugin!.id });

    // Upload a text clip -- should be filtered (not uploaded)
    // Since the server URL is invalid, if upload was attempted it would fail.
    // But since content filter is "image", text clips should be silently skipped.
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
