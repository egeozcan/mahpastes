import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

async function getClipIds(app: any): Promise<number[]> {
  return app.page.evaluate(async () => {
    // @ts-ignore
    const clips = await window.go.main.App.GetClips(false, [], [], "", "");
    return (clips || []).map((c: any) => c.id);
  });
}

async function waitForClipIds(app: any, expectedCount: number): Promise<number[]> {
  let clipIds: number[] = [];
  await expect.poll(async () => {
    clipIds = await getClipIds(app);
    return clipIds.length;
  }, { timeout: 5000, intervals: [200, 500] }).toBe(expectedCount);
  return clipIds;
}

test.describe('Palette Extractor Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'palette-extractor-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Palette Extractor Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register UI actions (lightbox and card)', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'palette-extractor-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    expect(actions.lightbox_buttons.length).toBeGreaterThan(0);
    expect(actions.card_actions.length).toBeGreaterThan(0);

    const lightboxBtn = actions.lightbox_buttons.find(
      (b: any) => b.id === 'extract_palette' && b.plugin_id === plugin!.id
    );
    expect(lightboxBtn).toBeDefined();
    expect(lightboxBtn.label).toBe('Extract Palette');
  });

  test('should extract palette and create SVG swatch clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'palette-extractor-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Get clip ID
    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Execute palette extraction via API (async action)
    await app.executePluginActionViaAPI(plugin!.id, 'extract_palette', [clipId], { count: 4 });

    // Wait for action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Check for errors first
    const lastError = await app.getPluginStorage(plugin!.id, 'last_error');
    expect(lastError).toBeFalsy();

    // Verify plugin recorded the colors
    const colorCount = parseInt(await app.getPluginStorage(plugin!.id, 'last_color_count'));
    expect(colorCount).toBeGreaterThan(0);
    expect(colorCount).toBeLessThanOrEqual(4);

    // Verify result clip ID
    const resultClipId = await app.getPluginStorage(plugin!.id, 'last_result_clip_id');
    expect(parseInt(resultClipId)).toBeGreaterThan(0);

    // Verify SVG clip was created in DB
    const totalClips = await app.getClipCountFromDB();
    expect(totalClips).toBe(2);
  });

  test('should tag original clip with hex colors when option enabled', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'palette-extractor-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clips = await app.page.evaluate(async () => {
      // @ts-ignore
      return await window.go.main.App.GetClips(false, [], [], "", "");
    });
    const clipId = clips[0].id;

    // Execute with tag_colors enabled
    await app.executePluginActionViaAPI(plugin!.id, 'extract_palette', [clipId], {
      count: 3,
      tag_colors: true,
    });

    // Wait for action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Verify color tags were created
    await expect.poll(
      async () => {
        const tagged = await app.getPluginStorage(plugin!.id, 'last_tagged_colors');
        try { return JSON.parse(tagged).length; } catch { return 0; }
      },
      { timeout: 5000, intervals: [200, 500, 1000] }
    ).toBeGreaterThan(0);

    // Verify tags exist in the system
    const allTags = await app.getAllTags();
    // Should have at least one hex color tag (starts with #)
    const colorTags = allTags.filter(t => t.name.startsWith('#'));
    expect(colorTags.length).toBeGreaterThan(0);
  });

  test('should track action execution count', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'palette-extractor-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds2 = await waitForClipIds(app, 1);

    await app.executePluginActionViaAPI(plugin!.id, 'extract_palette', [clipIds2[0]], { count: 3 });

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');
  });
});
