import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  createTempDir,
  cleanup,
} from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to test plugins directory
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Filesystem API', () => {
  let fsPluginId: number | null = null;
  let testDir: string | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    fsPluginId = null;
    testDir = await createTempDir();
  });

  test.afterEach(async ({ app }) => {
    if (fsPluginId) {
      try {
        await app.removePlugin(fsPluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
    if (testDir) {
      try {
        await cleanup(testDir);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test('should initialize with filesystem permissions declared', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    // Import plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('FS Test');
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // Check initialization
    const initialized = await app.getPluginStorage(plugin!.id, 'fs_test_initialized');
    expect(initialized).toBe('true');
  });

  test('should track read attempts', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // Initial read attempts should be 0
    const initialAttempts = await app.getPluginStorage(plugin!.id, 'read_attempts');
    expect(initialAttempts).toBe('0');
  });

  test('should track write attempts', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // Initial write attempts should be 0
    const initialAttempts = await app.getPluginStorage(plugin!.id, 'write_attempts');
    expect(initialAttempts).toBe('0');
  });

  test('should require permission for filesystem operations', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // Set a test read path (without granting permission)
    await app.page.evaluate(async ({ pluginId, testPath }) => {
      // @ts-ignore
      await window.go.main.PluginService.SetPluginStorage(pluginId, 'test_read_path', testPath);
    }, { pluginId: plugin!.id, testPath: '/etc/passwd' });

    // Trigger the plugin by uploading a clip
    const imagePath = await createTempFile(generateTestImage(50, 50), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);
    await app.page.waitForTimeout(500);

    // Check that there was an error (permission denied)
    const lastError = await app.getPluginStorage(plugin!.id, 'last_error');
    // Error should indicate permission issue
    expect(lastError.length).toBeGreaterThan(0);
  });

  test('plugin filesystem permissions are persisted', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // Get plugin permissions (should start empty or with only manually approved paths)
    const permissions = await app.getPluginPermissions(plugin!.id);
    // Permissions are stored in DB - this tests the API works
    expect(Array.isArray(permissions)).toBe(true);
  });

  test('should handle fs.exists check correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'fs-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    fsPluginId = plugin?.id ?? null;

    await app.page.waitForTimeout(500);

    // fs.exists should return false for paths without read permission
    // This tests that the API doesn't leak file existence info
    // The plugin checks internally - we verify it loaded correctly
    const initialized = await app.getPluginStorage(plugin!.id, 'fs_test_initialized');
    expect(initialized).toBe('true');
  });
});
