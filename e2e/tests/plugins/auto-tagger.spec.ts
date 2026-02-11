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

test.describe('Auto-Tagger Plugin', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
  });

  test('should load successfully', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();
    expect(plugin?.name).toBe('Auto-Tagger Test');

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');
  });

  test('should tag image clips with "image" tag', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image clip
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Wait for plugin to process the clip
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'clips_processed'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('1');

    // Verify "image" tag was applied
    const lastTags = await app.getPluginStorage(plugin!.id, 'last_applied_tags');
    const appliedTags = JSON.parse(lastTags);
    expect(appliedTags).toContain('image');
  });

  test('should tag text clips with "text" tag', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a text clip
    const textPath = await createTempFile(generateTestText('hello'), 'txt');
    await app.uploadFile(textPath);
    await app.expectClipCount(1);

    // Wait for plugin to process
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'clips_processed'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('1');

    // Verify "text" tag was applied
    const lastTags = await app.getPluginStorage(plugin!.id, 'last_applied_tags');
    const appliedTags = JSON.parse(lastTags);
    expect(appliedTags).toContain('text');
  });

  test('should process multiple clips and create tags', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload multiple image clips
    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

    await app.uploadFiles([file1, file2, file3]);
    await app.expectClipCount(3);

    // Wait for all clips to be processed
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'clips_processed'),
      { timeout: 10000, intervals: [200, 500, 1000] }
    ).toBe('3');

    // Verify "image" tag exists in the system
    const allTags = await app.getAllTags();
    const imageTag = allTags.find(t => t.name === 'image');
    expect(imageTag).toBeDefined();
  });

  test('should create tags that are visible on clip cards', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload an image
    const imagePath = await createTempFile(generateTestImage(100, 100, [128, 128, 128]), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Wait for processing
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'clips_processed'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('1');

    // Refresh to see the tag on the card
    await app.refreshClips();

    // Check tag is visible on the clip card
    await app.expectClipHasTag(filename, 'image');
  });

  test('should keep a log of all tagging operations', async ({ app }) => {
    const plugin = await app.importPluginFromPath(path.join(TEST_PLUGINS_DIR, 'auto-tagger-test.lua'));
    expect(plugin).not.toBeNull();

    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'loaded'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('true');

    // Upload a clip
    const imagePath = await createTempFile(generateTestImage(100, 100), 'png');
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    // Wait for processing
    await expect.poll(
      async () => app.getPluginStorage(plugin!.id, 'clips_processed'),
      { timeout: 5000, intervals: [100, 200, 500] }
    ).toBe('1');

    // Check the tag log
    const logData = await app.getPluginStorage(plugin!.id, 'tag_log');
    const log = JSON.parse(logData);
    expect(log.length).toBe(1);
    expect(log[0].content_type).toContain('image/');
    expect(log[0].tags_applied).toContain('image');
    expect(log[0].time).toBeGreaterThan(0);
  });
});
