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

test.describe('Image API Edge Cases', () => {
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

  test.describe('Image clip edge cases', () => {
    test('should reject grayscale_pixels exceeding pixel limit', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
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
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'grayscale_limit_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'grayscale_limit_err');
      expect(err).toContain('too large');
    });

    test('should reject overlay_text with invalid color', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'overlay_bad_color_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'overlay_bad_color_err');
      expect(err).toContain('invalid color');
    });

    test('should reject composite with missing dimensions', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
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
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'composite_no_dims_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'composite_no_dims_err');
      expect(err).toContain('width and height are required');
    });

    test('should reject composite with missing layers', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [128, 0, 128]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'composite_no_layers_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'composite_no_layers_err');
      expect(err).toContain('layers are required');
    });

    test('should reject composite with invalid layer missing clip_id', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [255, 255, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'composite_bad_layer_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'composite_bad_layer_err');
      expect(err).toContain('clip_id is required');
    });

    test('should reject resize with zero dimensions', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 128, 128]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'resize_zero_w_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'resize_zero_w_err');
      expect(err).toContain('width and height must be positive');
    });

    test('should reject resize with negative dimensions', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
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
        async () => app.getPluginStorage(plugin!.id, 'image_edge_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'resize_neg_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'resize_neg_err');
      expect(err).toContain('width and height must be positive');
    });
  });

  test.describe('Non-image clip handling', () => {
    test('should reject resize on text clip', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const textPath = await createTempFile(generateTestText('edge-case'), 'txt');
      await app.uploadFile(textPath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'non_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'resize_text_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'resize_text_err');
      expect(err).toContain('not an image');
    });

    test('should reject overlay_text on text clip', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const textPath = await createTempFile(generateTestText('overlay-edge'), 'txt');
      await app.uploadFile(textPath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'non_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'overlay_text_clip_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'overlay_text_clip_err');
      expect(err).toContain('not an image');
    });

    test('should reject dominant_colors on text clip', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-edge-cases-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const textPath = await createTempFile(generateTestText('colors-edge'), 'txt');
      await app.uploadFile(textPath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'non_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'colors_text_ok')).toBe('false');
      const err = await app.getPluginStorage(plugin!.id, 'colors_text_err');
      expect(err).toContain('not an image');
    });
  });
});
