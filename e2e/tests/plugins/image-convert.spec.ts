import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Image Convert', () => {
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

  async function installPluginAndUploadImage(app: any) {
    const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-convert-test.lua');
    const plugin = await app.importPluginFromPath(pluginPath);
    expect(plugin).not.toBeNull();
    pluginId = plugin?.id ?? null;

    await expect
      .poll(async () => app.getPluginStorage(plugin!.id, 'loaded'), {
        timeout: 5000,
        intervals: [100, 200, 500],
      })
      .toBe('true');

    const imagePath = await createTempFile(
      generateTestImage(100, 80, [255, 0, 0]),
      'png'
    );
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await expect
      .poll(async () => app.getPluginStorage(plugin!.id, 'all_tests_done'), {
        timeout: 10000,
        intervals: [200, 500, 1000],
      })
      .toBe('true');

    return plugin!;
  }

  test('should convert PNG to JPEG with default quality', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'jpeg_default_ok')).toBe(
      'true'
    );
    expect(await app.getPluginStorage(plugin.id, 'jpeg_default_mime')).toBe(
      'image/jpeg'
    );
    expect(
      await app.getPluginStorage(plugin.id, 'jpeg_default_has_data')
    ).toBe('true');
  });

  test('should convert PNG to JPEG with explicit quality 50', async ({
    app,
  }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'jpeg_q50_ok')).toBe('true');
    expect(await app.getPluginStorage(plugin.id, 'jpeg_q50_mime')).toBe(
      'image/jpeg'
    );
    expect(await app.getPluginStorage(plugin.id, 'jpeg_q50_has_data')).toBe(
      'true'
    );
  });

  test('should produce smaller output at lower quality', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    const defaultLen = parseInt(
      await app.getPluginStorage(plugin.id, 'jpeg_default_data_len')
    );
    const q50Len = parseInt(
      await app.getPluginStorage(plugin.id, 'jpeg_q50_data_len')
    );

    // Quality 50 should produce smaller data than quality 85
    expect(q50Len).toBeLessThan(defaultLen);
  });

  test('should convert to PNG (identity)', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'png_ok')).toBe('true');
    expect(await app.getPluginStorage(plugin.id, 'png_mime')).toBe(
      'image/png'
    );
    expect(await app.getPluginStorage(plugin.id, 'png_has_data')).toBe('true');
  });

  test('should accept "jpg" as alias for "jpeg"', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'jpg_alias_ok')).toBe('true');
    expect(await app.getPluginStorage(plugin.id, 'jpg_alias_mime')).toBe(
      'image/jpeg'
    );
  });

  test('should return error for invalid format', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'invalid_format_ok')).toBe(
      'false'
    );
    const err = await app.getPluginStorage(plugin.id, 'invalid_format_err');
    expect(err).toContain('unsupported format');
  });

  test('should return error for non-existent clip', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'nonexistent_clip_ok')).toBe(
      'false'
    );
    const err = await app.getPluginStorage(plugin.id, 'nonexistent_clip_err');
    expect(err).toContain('clip not found');
  });

  test('should clamp quality below 1', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(await app.getPluginStorage(plugin.id, 'quality_clamp_low_ok')).toBe(
      'true'
    );
    expect(
      await app.getPluginStorage(plugin.id, 'quality_clamp_low_mime')
    ).toBe('image/jpeg');
  });

  test('should clamp quality above 100', async ({ app }) => {
    const plugin = await installPluginAndUploadImage(app);

    expect(
      await app.getPluginStorage(plugin.id, 'quality_clamp_high_ok')
    ).toBe('true');
    expect(
      await app.getPluginStorage(plugin.id, 'quality_clamp_high_mime')
    ).toBe('image/jpeg');
  });
});
