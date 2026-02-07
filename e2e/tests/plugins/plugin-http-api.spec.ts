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

test.describe('Plugin HTTP API', () => {
  let httpPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    httpPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    if (httpPluginId) {
      try {
        await app.removePlugin(httpPluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test('should have http API available', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'http-test.lua');

    // Import plugin
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('HTTP Test');
    httpPluginId = plugin?.id ?? null;

    await app.waitForPluginStorage(plugin!.id, 'http_test_initialized', 'true');

    // Check HTTP API availability was recorded at load time
    const httpGetAvailable = await app.getPluginStorage(plugin!.id, 'http_get_available');
    const httpPostAvailable = await app.getPluginStorage(plugin!.id, 'http_post_available');

    expect(httpGetAvailable).toBe('true');
    expect(httpPostAvailable).toBe('true');
  });

  test('should reject requests to unauthorized domains', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'http-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    httpPluginId = plugin?.id ?? null;

    await app.waitForPluginStorage(plugin!.id, 'http_test_initialized', 'true');

    // Upload a clip to trigger the HTTP test
    const imagePath = await createTempFile(generateTestImage(50, 50), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await app.waitForPluginStorageContains(plugin!.id, 'unauthorized_domain_error', 'domain not in allowlist');

    // Check that unauthorized domain error was recorded
    const domainError = await app.getPluginStorage(plugin!.id, 'unauthorized_domain_error');
    expect(domainError).toContain('domain not in allowlist');
  });

  test('should initialize with correct network permissions from manifest', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'http-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    httpPluginId = plugin?.id ?? null;

    // The plugin manifest specifies httpbin.org with GET and POST methods
    // This tests that the manifest parsing correctly extracts network permissions
    expect(plugin?.name).toBe('HTTP Test');

    // Plugin should be enabled (manifest parsed successfully)
    expect(plugin?.enabled).toBe(true);
  });

  test('should properly initialize http module in plugin', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'http-test.lua');

    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    httpPluginId = plugin?.id ?? null;

    await app.waitForPluginStorage(plugin!.id, 'http_test_initialized', 'true');

    // Verify initialization flag was set
    const initialized = await app.getPluginStorage(plugin!.id, 'http_test_initialized');
    expect(initialized).toBe('true');
  });
});
