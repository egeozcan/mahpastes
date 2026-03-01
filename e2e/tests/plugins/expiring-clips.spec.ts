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
  }, { timeout: 10000, intervals: [200, 500, 1000] }).toBe(expectedCount);
  return clipIds;
}

test.describe('Expiring Clips Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Expiring Clips Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register card actions (set_expiry and clear_expiry)', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    const setExpiry = actions.card_actions.find(
      (a: any) => a.id === 'set_expiry' && a.plugin_id === plugin!.id
    );
    const clearExpiry = actions.card_actions.find(
      (a: any) => a.id === 'clear_expiry' && a.plugin_id === plugin!.id
    );

    expect(setExpiry).toBeDefined();
    expect(setExpiry.label).toBe('Set Expiry');
    expect(clearExpiry).toBeDefined();
    expect(clearExpiry.label).toBe('Clear Expiry');
  });

  test('should set expiry on a clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a clip
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Set expiry with 3 second duration (test-friendly)
    const result = await app.executePluginActionViaAPI(plugin!.id, 'set_expiry', [clipId], {
      duration: '3',
    });
    expect(result.success).toBe(true);

    // Verify storage was updated
    const expiryMap = JSON.parse(await app.getPluginStorage(plugin!.id, 'expiry_map'));
    expect(expiryMap[String(clipId)]).toBeDefined();
    expect(expiryMap[String(clipId)]).toBeGreaterThan(0);

    const setDuration = await app.getPluginStorage(plugin!.id, 'last_set_duration');
    expect(setDuration).toBe('3');

    const totalSet = await app.getPluginStorage(plugin!.id, 'total_set');
    expect(totalSet).toBe('1');
  });

  test('should clear expiry on a clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const imagePath = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Set expiry first (long duration so it doesn't fire before we clear)
    await app.executePluginActionViaAPI(plugin!.id, 'set_expiry', [clipId], { duration: '86400' });

    // Verify it was set
    let expiryMap = JSON.parse(await app.getPluginStorage(plugin!.id, 'expiry_map'));
    expect(expiryMap[String(clipId)]).toBeDefined();

    // Clear expiry
    const result = await app.executePluginActionViaAPI(plugin!.id, 'clear_expiry', [clipId]);
    expect(result.success).toBe(true);

    // Verify it was cleared
    expiryMap = JSON.parse(await app.getPluginStorage(plugin!.id, 'expiry_map'));
    expect(expiryMap[String(clipId)]).toBeUndefined();

    const clearedCount = await app.getPluginStorage(plugin!.id, 'last_cleared_count');
    expect(clearedCount).toBe('1');
  });

  test('should archive expired clip via scheduled check', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a clip
    const imagePath = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    const clipIds = await waitForClipIds(app, 1);
    const clipId = clipIds[0];

    // Set a very short expiry (5 seconds)
    await app.executePluginActionViaAPI(plugin!.id, 'set_expiry', [clipId], { duration: '5' });

    // Wait for the scheduler to run check_expiry and archive the clip
    // Scheduler runs every 2 seconds; expiry is 5 seconds from now
    // So it should be archived within ~7-10 seconds
    await expect.poll(
      async () => {
        const totalArchived = await app.getPluginStorage(plugin!.id, 'total_archived');
        return parseInt(totalArchived);
      },
      { timeout: 30000, intervals: [1000, 2000, 2000, 3000], message: 'Waiting for expired clip to be archived' }
    ).toBeGreaterThanOrEqual(1);

    // Verify the expiry was removed from the map
    const expiryMap = JSON.parse(await app.getPluginStorage(plugin!.id, 'expiry_map'));
    expect(expiryMap[String(clipId)]).toBeUndefined();

    // Verify the clip is now in the archive
    const archivedCount = await app.getClipCountFromDB(true);
    expect(archivedCount).toBeGreaterThanOrEqual(1);
  });

  test('should run scheduled expiry checks', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Wait for the scheduler to run at least once (interval is 2s, allow generous timeout for CI load)
    await expect.poll(
      async () => parseInt(await app.getPluginStorage(plugin!.id, 'check_count') || '0'),
      { timeout: 30000, intervals: [1000, 2000, 3000, 3000], message: 'Waiting for check_expiry to run' }
    ).toBeGreaterThanOrEqual(1);
  });

  test('should set expiry on multiple clips at once', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    await app.uploadFiles([file1, file2]);
    await app.expectClipCount(2);

    const clipIds = await waitForClipIds(app, 2);

    const result = await app.executePluginActionViaAPI(plugin!.id, 'set_expiry', clipIds, {
      duration: '86400',
    });
    expect(result.success).toBe(true);

    const totalSet = await app.getPluginStorage(plugin!.id, 'total_set');
    expect(totalSet).toBe('2');

    const expiryMap = JSON.parse(await app.getPluginStorage(plugin!.id, 'expiry_map'));
    expect(Object.keys(expiryMap).length).toBe(2);
  });

  test('should report error for unknown action', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'expiring-clips-test.lua'));
    expect(plugin).not.toBeNull();

    const result = await app.executePluginActionViaAPI(plugin!.id, 'unknown_action', [1]);
    expect(result.success).toBe(false);
    expect(result.error).toBeDefined();
  });
});
