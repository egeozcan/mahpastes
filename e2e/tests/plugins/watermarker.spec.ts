import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

async function waitForClipIds(app: any, expectedCount: number): Promise<number[]> {
  let clipIds: number[] = [];
  await expect.poll(async () => {
    clipIds = await app.page.evaluate(async () => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], "", "");
      return (clips || []).map((c: any) => c.id);
    });
    return clipIds.length;
  }, { timeout: 5000, intervals: [200, 500] }).toBe(expectedCount);
  return clipIds;
}

test.describe('Watermarker Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Watermarker Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register UI actions with options', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    const lightboxBtn = actions.lightbox_buttons.find(
      (b: any) => b.id === 'watermark' && b.plugin_id === plugin!.id
    );
    expect(lightboxBtn).toBeDefined();
    expect(lightboxBtn.label).toBe('Watermark');
    expect(lightboxBtn.options).toBeDefined();
    expect(lightboxBtn.options.length).toBeGreaterThan(0);
  });

  test('should apply watermark and create new clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(200, 200, [100, 150, 200]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Apply watermark (async action)
    await app.executePluginActionViaAPI(plugin!.id, 'watermark', [clipId], {
      text: 'Test Watermark',
      position: 'center',
      opacity: 0.5,
      size: 24,
      color: '#ffffff',
    });

    // Wait for async action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Verify result clip was created
    const resultClipId = await app.getPluginStorage(plugin!.id, 'last_result_clip_id');
    expect(parseInt(resultClipId)).toBeGreaterThan(0);

    // Verify new clip exists in DB
    const totalClips = await app.getClipCountFromDB();
    expect(totalClips).toBe(2);

    // Verify options were recorded
    const options = JSON.parse(await app.getPluginStorage(plugin!.id, 'last_options'));
    expect(options.text).toBe('Test Watermark');
    expect(options.position).toBe('center');
  });

  test('should record error when watermark text is missing', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(100, 100), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clips = await app.page.evaluate(async () => {
      // @ts-ignore
      return await window.go.main.App.GetClips(false, [], [], "", "");
    });

    // Execute without text (async action runs in background)
    await app.executePluginActionViaAPI(plugin!.id, 'watermark', [clips[0].id], {});

    // Wait for async action to complete (it should still run and set error)
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'last_error'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toContain('required');

    // No new clip should have been created
    const totalClips = await app.getClipCountFromDB();
    expect(totalClips).toBe(1);
  });

  test('should use custom position and color options', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(200, 200, [50, 100, 150]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds2 = await waitForClipIds(app, 1);

    await app.executePluginActionViaAPI(plugin!.id, 'watermark', [clipIds2[0]], {
      text: 'Bottom Right',
      position: 'bottom-right',
      opacity: 0.8,
      size: 36,
      color: '#ff0000',
    });

    // Wait for completion
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    const options = JSON.parse(await app.getPluginStorage(plugin!.id, 'last_options'));
    expect(options.position).toBe('bottom-right');
    expect(options.color).toBe('#ff0000');
    expect(options.size).toBe(36);
  });

  test('should generate watermarked filename', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(100, 100), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds2 = await waitForClipIds(app, 1);

    await app.executePluginActionViaAPI(plugin!.id, 'watermark', [clipIds2[0]], {
      text: 'Copyright',
    });

    // Wait for completion
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    const resultFilename = await app.getPluginStorage(plugin!.id, 'last_result_filename');
    expect(resultFilename).toContain('_watermarked.');
  });

  test('should handle bulk watermarking on multiple clips', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'watermarker-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const file1 = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    await app.uploadFiles([file1, file2]);
    await app.expectClipCount(2);

    const clipIds = await waitForClipIds(app, 2);

    await app.executePluginActionViaAPI(plugin!.id, 'watermark', clipIds, {
      text: 'Bulk Watermark',
    });

    // Wait for completion
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Should create 2 new watermarked clips
    await expect.poll(
      async () => app.getClipCountFromDB(),
      { timeout: 5000, intervals: [200, 500, 1000] }
    ).toBe(4);
  });
});
