import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

// Helper to safely get clip IDs from the backend
async function getClipIds(app: any): Promise<number[]> {
  return app.page.evaluate(async () => {
    // @ts-ignore
    const clips = await window.go.main.App.GetClips(false, [], [], "", "");
    return (clips || []).map((c: any) => c.id);
  });
}

test.describe('QR Code Generator Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('QR Code Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should register UI actions (lightbox and card)', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();

    const actions = await app.getPluginUIActions();
    const lightboxBtn = actions.lightbox_buttons.find(
      (b: any) => b.id === 'generate_qr' && b.plugin_id === plugin!.id
    );
    expect(lightboxBtn).toBeDefined();
    expect(lightboxBtn.label).toBe('Generate QR');

    const cardAction = actions.card_actions.find(
      (a: any) => a.id === 'generate_qr' && a.plugin_id === plugin!.id
    );
    expect(cardAction).toBeDefined();
  });

  test('should generate QR code from text clip', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a text clip
    const textPath = await createTempFile('https://example.com', 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);

    // Wait for clip to be available in DB
    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(1);

    // Execute QR generation (async action)
    await app.executePluginActionViaAPI(plugin!.id, 'generate_qr', [clipIds[0]]);

    // Wait for action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Verify the input text was captured
    const inputText = await app.getPluginStorage(plugin!.id, 'last_input_text');
    expect(inputText).toContain('https://example.com');

    // Verify result clip ID was set
    const resultClipId = await app.getPluginStorage(plugin!.id, 'last_result_clip_id');
    expect(parseInt(resultClipId)).toBeGreaterThan(0);

    // Verify new clip was created in DB
    await expect.poll(
      async () => app.getClipCountFromDB(),
      { timeout: 5000, intervals: [200, 500] }
    ).toBe(2);
  });

  test('should handle multiple text clips', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const text1 = await createTempFile('Hello World 1', 'txt');
    const text2 = await createTempFile('Hello World 2', 'txt');
    await app.uploadFiles([text1, text2]);
    await app.expectClipCount(2);

    // Wait for clips in DB
    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(2);

    await app.executePluginActionViaAPI(plugin!.id, 'generate_qr', clipIds);

    // Wait for action to complete
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');

    // Should create 2 QR clips in DB
    await expect.poll(
      async () => app.getClipCountFromDB(),
      { timeout: 10000, intervals: [500, 1000, 2000] }
    ).toBe(4);
  });

  test('should track action execution count', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    const textPath = await createTempFile('test content', 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);

    let clipIds: number[] = [];
    await expect.poll(async () => {
      clipIds = await getClipIds(app);
      return clipIds.length;
    }, { timeout: 5000, intervals: [200, 500] }).toBe(1);

    await app.executePluginActionViaAPI(plugin!.id, 'generate_qr', [clipIds[0]]);

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'actions_executed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('1');
  });

  test('should report unknown action as error', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'qr-code-test.lua'));
    expect(plugin).not.toBeNull();

    const result = await app.executePluginActionViaAPI(plugin!.id, 'nonexistent', [1]);
    expect(result.success).toBe(false);
    expect(result.error).toBeDefined();
  });
});
