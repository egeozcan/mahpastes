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

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Image API', () => {
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

  test.describe('image.info via clip:created event', () => {
    test('should return correct info for uploaded PNG', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      // Upload a 100x80 red PNG
      const imagePath = await createTempFile(generateTestImage(100, 80, [255, 0, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Wait for plugin to process the event
      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Verify image.info result
      const infoStr = await app.getPluginStorage(plugin!.id, 'info_result');
      expect(infoStr).not.toBe('');
      const info = JSON.parse(infoStr);
      expect(info.width).toBe(100);
      expect(info.height).toBe(80);
      expect(info.format).toBe('png');
      expect(info.size).toBeGreaterThan(0);
      expect(info.has_exif).toBe(false);
    });
  });

  test.describe('image.resize via clip:created event', () => {
    test('should resize image successfully', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'resize_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'resize_has_data')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'resize_mime')).toBe('image/png');
    });
  });

  test.describe('image.dominant_colors via clip:created event', () => {
    test('should detect dominant color of solid-color image', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      // Upload a solid red image
      const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      const colorsStr = await app.getPluginStorage(plugin!.id, 'colors_result');
      expect(colorsStr).not.toBe('');
      const colors = JSON.parse(colorsStr);
      expect(colors.length).toBeGreaterThanOrEqual(1);
      // The dominant color should be close to red (#ff0000)
      expect(colors[0]).toBe('#ff0000');
    });
  });

  test.describe('image.grayscale_pixels via clip:created event', () => {
    test('should return correct number of grayscale pixels', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [128, 128, 128]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'grayscale_ok')).toBe('true');
      // 2x2 = 4 pixels
      expect(await app.getPluginStorage(plugin!.id, 'grayscale_count')).toBe('4');
    });
  });

  test.describe('image.diff via clip:created event', () => {
    test('should return similarity 1.0 for identical images', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      const similarity = await app.getPluginStorage(plugin!.id, 'diff_similarity');
      expect(parseFloat(similarity)).toBe(1);
      expect(await app.getPluginStorage(plugin!.id, 'diff_has_data')).toBe('true');
    });
  });

  test.describe('image.overlay_text via clip:created event', () => {
    test('should overlay text on image successfully', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(200, 150, [0, 128, 255]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'overlay_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_has_data')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_mime')).toBe('image/png');
    });
  });

  test.describe('image.composite via clip:created event', () => {
    test('should composite layers successfully', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [255, 128, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'composite_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'composite_has_data')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'composite_mime')).toBe('image/png');
    });
  });

  test.describe('image.metadata via clip:created event', () => {
    test('should return metadata table for PNG (may be empty)', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [64, 64, 64]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // metadata returns a table (PNGs typically have no EXIF, so it should be an empty table)
      expect(await app.getPluginStorage(plugin!.id, 'metadata_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'metadata_type')).toBe('table');
    });
  });

  test.describe('Error handling', () => {
    test('should handle non-image clips gracefully', async ({ app }) => {
      // This test uses a specialized plugin that tests error cases
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      // Upload a text file (not an image)
      const textPath = await createTempFile(generateTestText('not-an-image'), 'txt');
      await app.uploadFile(textPath);
      await app.expectClipCount(1);

      // The plugin should receive the event but image.info should fail
      // Wait a bit for the event to be processed
      await app.page.waitForTimeout(2000);

      // info_error should be set (clip is not an image)
      const infoError = await app.getPluginStorage(plugin!.id, 'info_error');
      expect(infoError).toContain('not an image');
    });

    test('should return error for invalid resize fit mode', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-error-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'error_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Invalid fit mode should return error
      expect(await app.getPluginStorage(plugin!.id, 'resize_invalid_fit_ok')).toBe('false');
      const fitErr = await app.getPluginStorage(plugin!.id, 'resize_invalid_fit_err');
      expect(fitErr).toContain('unknown fit mode');
    });

    test('should resize with contain and cover fit modes', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-error-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(200, 100, [0, 255, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'error_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // contain and cover should succeed
      expect(await app.getPluginStorage(plugin!.id, 'resize_contain_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'resize_cover_ok')).toBe('true');
    });

    test('should return error for invalid overlay_text position', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-error-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'error_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Invalid position should return error
      expect(await app.getPluginStorage(plugin!.id, 'overlay_invalid_pos_ok')).toBe('false');
      const posErr = await app.getPluginStorage(plugin!.id, 'overlay_invalid_pos_err');
      expect(posErr).toContain('unknown position');
    });

    test('should return error for non-existent clip ID', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-error-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [128, 128, 128]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'error_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Non-existent clip ID should return error
      expect(await app.getPluginStorage(plugin!.id, 'info_nonexistent_ok')).toBe('false');
      const infoErr = await app.getPluginStorage(plugin!.id, 'info_nonexistent_err');
      expect(infoErr).toContain('clip not found');

      expect(await app.getPluginStorage(plugin!.id, 'resize_nonexistent_ok')).toBe('false');
      const resizeErr = await app.getPluginStorage(plugin!.id, 'resize_nonexistent_err');
      expect(resizeErr).toContain('clip not found');
    });
  });
});
