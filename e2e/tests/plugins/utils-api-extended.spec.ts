import { test, expect } from '../../fixtures/test-fixtures';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Utils API Extended', () => {
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

  // --- Clipboard with field OMITTED from manifest ---

  test('should deny clipboard_write when clipboard field is omitted from manifest', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-omitted-clipboard-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // clipboard_write should return nil when clipboard field is omitted
    const ok = await app.getPluginStorage(plugin!.id, 'clipboard_write_ok');
    expect(ok).toBe('nil');

    // Should have an error message about clipboard access
    const err = await app.getPluginStorage(plugin!.id, 'clipboard_write_err');
    expect(err).toContain('clipboard access not permitted');
  });

  // --- HMAC edge cases ---

  test('should compute HMAC-SHA256 with empty key', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'hmac_empty_key');
    // hmac_sha256("", "message") computed with Go's crypto/hmac
    expect(result).toBe('eb08c1f56d5ddee07f7bdf80468083da06b64cf4fac64fe3a90883df5feacae4');
  });

  test('should compute HMAC-SHA256 with empty data', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'hmac_empty_data');
    // hmac_sha256("key", "") computed with Go's crypto/hmac
    expect(result).toBe('5d5d139563c95b5967b9bd9a8c9b233a9dedb45072794cd232dc1b74832607d0');
  });

  // --- URL encode/decode with unicode ---

  test('should url_encode unicode characters correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Go's url.QueryEscape encodes UTF-8 bytes as %XX
    const encoded = await app.getPluginStorage(plugin!.id, 'url_encode_unicode');
    expect(encoded).toBe('h%C3%A9llo+w%C3%B6rld');
  });

  test('should url_decode unicode roundtrip correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const decoded = await app.getPluginStorage(plugin!.id, 'url_decode_unicode');
    expect(decoded).toBe('héllo wörld');
  });

  // --- URL encode/decode with special URL characters ---

  test('should url_encode special URL characters correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Go's url.QueryEscape for "/?=&#"
    const encoded = await app.getPluginStorage(plugin!.id, 'url_encode_special');
    expect(encoded).toBe('%2F%3F%3D%26%23');
  });

  test('should url_decode special URL characters roundtrip correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const decoded = await app.getPluginStorage(plugin!.id, 'url_decode_special');
    expect(decoded).toBe('/?=&#');
  });

  // --- SHA-256 of longer string ---

  test('should compute sha256 of longer string correctly', async ({ app }) => {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'utils-ext-edge-cases-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const result = await app.getPluginStorage(plugin!.id, 'sha256_long');
    // sha256("The quick brown fox jumps over the lazy dog") - well-known test vector
    expect(result).toBe('d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592');
  });
});
