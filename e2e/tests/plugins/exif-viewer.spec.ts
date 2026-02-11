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
      const clips = await window.go.main.App.GetClips(false, [], []);
      return (clips || []).map((c: any) => c.id);
    });
    return clipIds.length;
  }, { timeout: 5000, intervals: [200, 500] }).toBe(expectedCount);
  return clipIds;
}

test.describe('EXIF/Metadata Viewer Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('EXIF Viewer Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register UI actions', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    const lightboxBtn = actions.lightbox_buttons.find(
      (b: any) => b.id === 'view_exif' && b.plugin_id === plugin!.id
    );
    expect(lightboxBtn).toBeDefined();
    expect(lightboxBtn.label).toBe('View EXIF');
  });

  test('should extract image info and create EXIF report', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(200, 150, [100, 200, 100]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Wait for clip:created event processing (may receive >1 due to event timing)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'clips_seen') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);

    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Execute EXIF extraction (async action)
    await app.executePluginActionViaAPI(plugin!.id, 'view_exif', [clipId]);

    // Wait for async action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Verify image info was recorded
    const infoStr = await app.getPluginStorage(plugin!.id, 'last_info');
    expect(infoStr).not.toBe('');
    const info = JSON.parse(infoStr);
    expect(info.width).toBe(200);
    expect(info.height).toBe(150);
    expect(info.format).toBeTruthy();

    // Verify report clip was created in DB
    const totalClips = await app.getClipCountFromDB();
    expect(totalClips).toBe(2);
  });

  test('should track clip:created events for images', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Wait for event processing (may receive >1 due to event timing)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'clips_seen') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);

    // Verify it detected image content type
    const isImage = await app.getPluginStorage(plugin!.id, 'last_clip_is_image');
    expect(isImage).toBe('true');
  });

  test('should detect non-image clips in clip:created handler', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a text clip (not an image)
    const textPath = await createTempFile('Hello World', 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);

    // Wait for event processing (may receive >1 due to event timing)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'clips_seen') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);

    // Should detect it is not an image
    const isImage = await app.getPluginStorage(plugin!.id, 'last_clip_is_image');
    expect(isImage).toBe('false');
  });

  test('should handle test images without EXIF data gracefully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Test-generated images have no EXIF data
    const imagePath = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'clips_seen') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(1);

    // Auto-tag should be "none" since test images have no EXIF
    const autoTag = await app.getPluginStorage(plugin!.id, 'last_auto_tag');
    expect(autoTag).toBe('none');
  });

  test('should handle multiple clips in EXIF action', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'exif-viewer-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const file1 = await createTempFile(generateTestImage(80, 60, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(120, 90, [0, 255, 0]), 'png');
    await app.uploadFiles([file1, file2]);
    await app.expectClipCount(2);

    // Wait for both clip:created events (may receive >2 due to event timing)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'clips_seen') || '0'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBeGreaterThanOrEqual(2);

    const clipIds = await waitForClipIds(app, 2);

    await app.executePluginActionViaAPI(plugin!.id, 'view_exif', clipIds);

    // Wait for async action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Should create 2 EXIF report clips in DB
    await expect.poll(
      async () => app.getClipCountFromDB(),
      { timeout: 5000, intervals: [200, 500, 1000] }
    ).toBe(4);
  });
});
