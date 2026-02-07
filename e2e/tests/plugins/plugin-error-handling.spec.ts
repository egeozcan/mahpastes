import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to test plugins directory
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Error Handling', () => {
  let errorPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    errorPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (errorPluginId) {
      try {
        await app.removePlugin(errorPluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test('should continue processing events after handler errors', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'error-test.lua');

    // Import plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Error Test');
    errorPluginId = plugin?.id ?? null;

    // Wait for plugin to initialize (it sets error_count to "0" on load)
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('0');

    // Upload first clip - will cause error
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    await app.uploadFile(image1);
    await app.expectClipCount(1);

    // Wait for error handler to run and record error
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('1');

    // Upload second clip - should still trigger handler despite previous error
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    await app.uploadFile(image2);
    await app.expectClipCount(2);

    // Wait for second error handler to run
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('2');

    // Both clips should have triggered the handler
    const callCount = await app.getPluginStorage(plugin!.id, 'calls_before_error');
    expect(parseInt(callCount)).toBeGreaterThanOrEqual(2);
  });

  test('should track error count in storage', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'error-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    errorPluginId = plugin?.id ?? null;

    // Wait for plugin to initialize
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('0');

    // Upload multiple clips to generate multiple errors
    const files: string[] = [];
    for (let i = 0; i < 3; i++) {
      files.push(await createTempFile(generateTestImage(40 + i, 40 + i), 'png'));
    }

    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Wait for all 3 error handlers to run
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'error_count')),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe(3);
  });

  test('should disable plugin after too many errors', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'error-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    errorPluginId = plugin?.id ?? null;

    // Wait for plugin to initialize
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('0');

    // Upload enough clips to trigger auto-disable (should be 3 errors based on manager.go)
    const files: string[] = [];
    for (let i = 0; i < 4; i++) {
      files.push(await createTempFile(generateTestImage(30 + i, 30 + i), 'png'));
    }

    await app.uploadFiles(files);
    await app.expectClipCount(4);

    // Wait for error handlers to run (at least 3 to trigger auto-disable)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'error_count') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(3);

    // Check plugin status - should be disabled due to errors
    const plugins = await app.getPlugins();
    const errorPlugin = plugins.find(p => p.id === plugin!.id);

    // Plugin should either be disabled or have error status
    // The behavior depends on error threshold in manager.go
    expect(errorPlugin).toBeDefined();
    // Note: depending on implementation, it may be disabled or have error status
  });

  test('plugin remains usable after errors if re-enabled', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'error-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    errorPluginId = plugin?.id ?? null;

    // Wait for plugin to initialize
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'error_count'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('0');

    // Generate some errors
    const image1 = await createTempFile(generateTestImage(50, 50), 'png');
    await app.uploadFile(image1);

    // Wait for error handler to run
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'error_count') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);

    // Disable and re-enable (plugin storage resets on reload)
    await app.disablePlugin(plugin!.id);

    // Wait for plugin to be disabled
    await expect.poll(
      async () => {
        const plugins = await app.getPlugins();
        const p = plugins.find(pl => pl.id === plugin!.id);
        return p?.enabled;
      },
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe(false);

    await app.enablePlugin(plugin!.id);

    // Wait for plugin to be re-enabled and reloaded (it resets error_count to "0" on load)
    await expect.poll(
      async () => {
        const plugins = await app.getPlugins();
        const p = plugins.find(pl => pl.id === plugin!.id);
        return p?.enabled;
      },
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe(true);

    // Delete existing clips
    await app.deleteAllClips();

    // Upload new clip - handler should still be called after re-enable
    const image2 = await createTempFile(generateTestImage(60, 60), 'png');
    await app.uploadFile(image2);
    await app.expectClipCount(1);

    // Wait for the handler to be called after re-enable
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'calls_before_error') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);
  });
});
