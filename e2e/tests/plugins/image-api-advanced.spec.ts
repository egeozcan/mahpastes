import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Image API Advanced', () => {
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

  test.describe('image.diff with different images', () => {
    test('should detect differences between red and blue images', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-advanced-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      // Upload red image (first clip - triggers single image tests)
      const redImage = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(redImage);
      await app.expectClipCount(1);

      // Wait for single image tests to finish before uploading second
      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'single_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Upload blue image (second clip - triggers diff test)
      const blueImage = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
      await app.uploadFile(blueImage);
      await app.expectClipCount(2);

      // Wait for all tests to complete
      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'all_advanced_tests_done'),
        { timeout: 15000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Verify diff detected differences
      expect(await app.getPluginStorage(plugin!.id, 'diff_different_ok')).toBe('true');
      const similarity = parseFloat(await app.getPluginStorage(plugin!.id, 'diff_different_similarity'));
      expect(similarity).toBeLessThan(1.0);
      expect(similarity).toBeGreaterThanOrEqual(0);
      expect(await app.getPluginStorage(plugin!.id, 'diff_different_has_data')).toBe('true');
    });
  });

  test.describe('image.resize output verification', () => {
    test('should produce valid PNG output with correct mime type', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-advanced-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(200, 160, [0, 128, 255]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'single_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'resize_verify_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'resize_verify_has_data')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'resize_verify_mime')).toBe('image/png');
    });
  });

  test.describe('image.overlay_text with explicit x/y coordinates', () => {
    test('should overlay text at specified x/y position', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-advanced-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(200, 150, [64, 64, 64]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'single_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'overlay_xy_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_xy_has_data')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_xy_mime')).toBe('image/png');
    });
  });

  test.describe('image.overlay_text with all position keywords', () => {
    test('should succeed for all 7 position keywords', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-advanced-test.lua');
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      pluginId = plugin?.id ?? null;

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'loaded'),
        { timeout: 5000, intervals: [100, 200, 500] }
      ).toBe('true');

      const imagePath = await createTempFile(generateTestImage(300, 200, [128, 0, 128]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await expect.poll(
        async () => app.getPluginStorage(plugin!.id, 'single_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      // Verify all positions succeeded
      expect(await app.getPluginStorage(plugin!.id, 'overlay_all_positions_ok')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_positions_count')).toBe('7');

      // Verify each position individually
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_center')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_top')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_bottom')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_top_left')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_top_right')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_bottom_left')).toBe('true');
      expect(await app.getPluginStorage(plugin!.id, 'overlay_pos_bottom_right')).toBe('true');
    });
  });

  test.describe('image.dominant_colors with image', () => {
    test('should return at least one dominant color', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'image-api-advanced-test.lua');
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
        async () => app.getPluginStorage(plugin!.id, 'single_image_tests_done'),
        { timeout: 10000, intervals: [200, 500, 1000] }
      ).toBe('true');

      expect(await app.getPluginStorage(plugin!.id, 'colors_multi_ok')).toBe('true');
      const colorCount = parseInt(await app.getPluginStorage(plugin!.id, 'colors_multi_count'));
      expect(colorCount).toBeGreaterThanOrEqual(1);

      // Verify result is parseable JSON array
      const colorsStr = await app.getPluginStorage(plugin!.id, 'colors_multi_result');
      expect(colorsStr).not.toBe('');
      const colors = JSON.parse(colorsStr);
      expect(Array.isArray(colors)).toBe(true);
      expect(colors.length).toBeGreaterThanOrEqual(1);
    });
  });
});
