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
    const clips = await window.go.main.App.GetClips(false, [], []);
    return (clips || []).map((c: any) => c.id);
  });
}

test.describe('ASCII Art Converter Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('ASCII Art Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register UI actions', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    const cardAction = actions.card_actions.find(
      (a: any) => a.id === 'to_ascii' && a.plugin_id === plugin!.id
    );
    expect(cardAction).toBeDefined();
    expect(cardAction.label).toBe('To ASCII');
  });

  test('should convert image to ASCII art text clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Wait for UI actions to be registered
    await expect.poll(async () => {
      const actions = await app.getPluginUIActions();
      return actions.card_actions.some(
        (a: any) => a.id === 'to_ascii' && a.plugin_id === plugin!.id
      );
    }, { timeout: 5000, intervals: [200, 500] }).toBe(true);

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(100, 100, [128, 128, 128]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Get clip ID (with retry for timing)
    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(1);

    // Execute ASCII art conversion (async action returns immediately)
    const result = await app.executePluginActionViaAPI(plugin!.id, 'to_ascii', [clipIds[0]], { width: 80 });
    expect(result.success).toBe(true);

    // Wait for async action to complete by polling storage
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 15000, intervals: [500, 1000, 2000] }
    ).toBe('1');

    // Check for errors first
    const lastError = await app.getPluginStorage(plugin!.id, 'last_error');
    expect(lastError).toBeFalsy();

    // Verify dimensions recorded
    const width = await app.getPluginStorage(plugin!.id, 'last_width');
    expect(width).toBe('80');

    const height = await app.getPluginStorage(plugin!.id, 'last_height');
    expect(height).toBe('40'); // width / 2

    // Verify art was created with correct line count
    const artLines = await app.getPluginStorage(plugin!.id, 'last_art_lines');
    expect(parseInt(artLines)).toBe(40);

    // Verify result clip ID was set
    const resultClipId = await app.getPluginStorage(plugin!.id, 'last_result_clip_id');
    expect(parseInt(resultClipId)).toBeGreaterThan(0);

    // Verify new text clip was created in DB
    const totalClips = await app.getClipCountFromDB();
    expect(totalClips).toBe(2);
  });

  test('should respect custom width option', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(100, 100, [200, 100, 50]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(1);

    // Use custom width of 60
    await app.executePluginActionViaAPI(plugin!.id, 'to_ascii', [clipIds[0]], { width: 60 });

    // Wait for completion
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Check for errors first
    const lastError = await app.getPluginStorage(plugin!.id, 'last_error');
    expect(lastError).toBeFalsy();

    const width = await app.getPluginStorage(plugin!.id, 'last_width');
    expect(width).toBe('60');

    const height = await app.getPluginStorage(plugin!.id, 'last_height');
    expect(height).toBe('30'); // 60 / 2
  });

  test('should handle multiple clips', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    await app.uploadFiles([file1, file2]);
    await app.expectClipCount(2);

    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(2);

    await app.executePluginActionViaAPI(plugin!.id, 'to_ascii', clipIds, { width: 40 });

    // Wait for completion
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Should create 2 new text clips (one per input) - check DB count
    await expect.poll(
      async () => app.getClipCountFromDB(),
      { timeout: 5000, intervals: [200, 500, 1000] }
    ).toBe(4);
  });

  test('should report unknown action as error', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'ascii-art-test.lua'));
    expect(plugin).not.toBeNull();

    const result = await app.executePluginActionViaAPI(plugin!.id, 'nonexistent', [1]);
    expect(result.success).toBe(false);
    expect(result.error).toBeDefined();
  });
});
