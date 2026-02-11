import { test, expect } from '../../fixtures/test-fixtures';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Utils API Extensions', () => {
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

  test('should compute correct sha256 hash', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'sha256_result');
    // sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
    expect(result).toBe('b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9');
  });

  test('should compute correct hmac_sha256', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'hmac_sha256_result');
    // hmac_sha256("secret", "message") = 8b5f48702995c1598c573db1e21866a9b825d4a794d169d7060a03605796360b
    expect(result).toBe('8b5f48702995c1598c573db1e21866a9b825d4a794d169d7060a03605796360b');
  });

  test('should url_encode correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'url_encode_result');
    // url.QueryEscape("hello world&foo=bar") = "hello+world%26foo%3Dbar"
    expect(result).toBe('hello+world%26foo%3Dbar');
  });

  test('should url_decode correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'url_decode_result');
    // url.QueryUnescape("hello+world%26foo%3Dbar") = "hello world&foo=bar"
    expect(result).toBe('hello world&foo=bar');
  });

  test('should return error for invalid url_decode input', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'url_decode_error');
    // Should contain an error string, not "no_error"
    expect(result).not.toBe('no_error');
    expect(result).toBeTruthy();
  });

  test('should handle empty strings for sha256, url_encode, url_decode', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // sha256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
    const sha256Empty = await app.getPluginStorage(plugin!.id, 'sha256_empty');
    expect(sha256Empty).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');

    // url_encode("") = ""
    const urlEncodeEmpty = await app.getPluginStorage(plugin!.id, 'url_encode_empty');
    expect(urlEncodeEmpty).toBe('');

    // url_decode("") = ""
    const urlDecodeEmpty = await app.getPluginStorage(plugin!.id, 'url_decode_empty');
    expect(urlDecodeEmpty).toBe('');
  });

  test('should clipboard_write without error', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'clipboard_write_result');
    expect(result).toBe('true');
  });

  test('should deny clipboard_write without permission', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-no-clipboard-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // clipboard_write should return nil when permission is denied
    const ok = await app.getPluginStorage(plugin!.id, 'clipboard_write_ok');
    expect(ok).toBe('nil');

    // Should have an error message about clipboard access
    const err = await app.getPluginStorage(plugin!.id, 'clipboard_write_err');
    expect(err).toContain('clipboard access not permitted');
  });
});
