import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Path to test plugins directory
const TEST_PLUGINS_DIR = path.resolve(__dirname, '../../test-plugins');

test.describe('Plugin Execution', () => {
  let eventTrackerPluginId: number | null = null;

  test.beforeEach(async ({ app }) => {
    // Clean up any existing plugins and clips
    await app.deleteAllPlugins();
    await app.deleteAllClips();
    await app.deleteAllTags();
    eventTrackerPluginId = null;
  });

  test.afterEach(async ({ app }) => {
    // Clean up
    if (eventTrackerPluginId) {
      try {
        await app.removePlugin(eventTrackerPluginId);
      } catch {
        // Ignore cleanup errors
      }
    }
  });

  test.describe('Plugin Import and Lifecycle', () => {
    test('should import plugin from path and enable it', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);

      expect(plugin).not.toBeNull();
      expect(plugin?.name).toBe('Event Tracker');
      expect(plugin?.version).toBe('1.0.0');
      expect(plugin?.enabled).toBe(true);

      eventTrackerPluginId = plugin?.id ?? null;

      // Verify plugin is listed
      const plugins = await app.getPlugins();
      expect(plugins.length).toBe(1);
      expect(plugins[0].name).toBe('Event Tracker');
    });

    test('should initialize storage on load', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      // Wait for plugin to initialize
      await app.page.waitForTimeout(500);

      // Check that plugin initialized its storage at load time
      const loaded = await app.getPluginStorage(plugin!.id, 'loaded');
      expect(loaded).toBe('true');

      const loadTime = await app.getPluginStorage(plugin!.id, 'load_time');
      expect(loadTime).not.toBe('');
    });

    test('should disable and re-enable plugin', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      // Disable
      await app.disablePlugin(plugin!.id);
      await app.expectPluginDisabled('Event Tracker');

      // Re-enable
      await app.enablePlugin(plugin!.id);
      await app.expectPluginEnabled('Event Tracker');
    });
  });

  test.describe('Clip Events', () => {
    test('should receive clip:created event when clip is uploaded', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin first
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      // Wait for plugin to initialize
      await app.page.waitForTimeout(500);

      // Upload a clip
      const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Wait for event to be processed
      await app.page.waitForTimeout(500);

      // Check that plugin received the event
      const clipCreatedCount = await app.getPluginStorage(plugin!.id, 'count_clip_created');
      expect(clipCreatedCount).toBe('1');

      // Check event log contains clip:created
      const eventLog = await app.getPluginStorage(plugin!.id, 'event_log');
      expect(eventLog).toContain('clip:created');
    });

    test('should receive clip:deleted event when clip is deleted', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Upload and then delete a clip
      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.deleteClip(filename);
      await app.expectClipCount(0);

      // Wait for event to be processed
      await app.page.waitForTimeout(500);

      // Check that plugin received the delete event
      const clipDeletedCount = await app.getPluginStorage(plugin!.id, 'count_clip_deleted');
      expect(clipDeletedCount).toBe('1');
    });

    test('should receive clip:archived and clip:unarchived events', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Upload a clip
      const imagePath = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Archive the clip
      await app.archiveClip(filename);
      await app.page.waitForTimeout(500);

      // Check archived event
      const archivedCount = await app.getPluginStorage(plugin!.id, 'count_clip_archived');
      expect(archivedCount).toBe('1');

      // Switch to archive view and unarchive
      await app.toggleArchiveView();
      await app.archiveClip(filename); // Toggle unarchive
      await app.page.waitForTimeout(500);

      // Check unarchived event
      const unarchivedCount = await app.getPluginStorage(plugin!.id, 'count_clip_unarchived');
      expect(unarchivedCount).toBe('1');
    });

    test('should receive multiple clip:created events for multiple uploads', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Upload multiple clips
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

      await app.uploadFiles([file1, file2, file3]);
      await app.expectClipCount(3);

      // Wait for events
      await app.page.waitForTimeout(500);

      // Check count
      const clipCreatedCount = await app.getPluginStorage(plugin!.id, 'count_clip_created');
      expect(clipCreatedCount).toBe('3');
    });
  });

  test.describe('Tag Events', () => {
    test('should receive tag:created event when tag is created', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Create a tag
      await app.createTag('TestTag');
      await app.page.waitForTimeout(500);

      // Check tag created event
      const tagCreatedCount = await app.getPluginStorage(plugin!.id, 'count_tag_created');
      expect(tagCreatedCount).toBe('1');
    });

    test('should receive tag:deleted event when tag is deleted', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Create and delete a tag
      await app.createTag('DeleteMe');
      await app.page.waitForTimeout(300);
      await app.deleteTag('DeleteMe');
      await app.page.waitForTimeout(500);

      // Check tag deleted event
      const tagDeletedCount = await app.getPluginStorage(plugin!.id, 'count_tag_deleted');
      expect(tagDeletedCount).toBe('1');
    });

    test('should receive tag:added_to_clip and tag:removed_from_clip events', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Upload a clip
      const imagePath = await createTempFile(generateTestImage(100, 100, [128, 128, 128]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Create a tag
      await app.createTag('MyTag');
      await app.page.waitForTimeout(300);

      // Add tag to clip
      await app.addTagToClip(filename, 'MyTag');
      await app.page.waitForTimeout(500);

      // Check tag added event
      const tagAddedCount = await app.getPluginStorage(plugin!.id, 'count_tag_added_to_clip');
      expect(tagAddedCount).toBe('1');

      // Remove tag from clip
      await app.removeTagFromClip(filename, 'MyTag');
      await app.page.waitForTimeout(500);

      // Check tag removed event
      const tagRemovedCount = await app.getPluginStorage(plugin!.id, 'count_tag_removed_from_clip');
      expect(tagRemovedCount).toBe('1');
    });
  });

  test.describe('Plugin Storage', () => {
    test('should persist data between events', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Upload multiple clips to generate multiple events
      for (let i = 0; i < 3; i++) {
        const imagePath = await createTempFile(generateTestImage(50 + i * 10, 50 + i * 10), 'png');
        await app.uploadFile(imagePath);
        await app.page.waitForTimeout(300);
      }

      await app.expectClipCount(3);
      await app.page.waitForTimeout(500);

      // Check event log has all events
      const eventLog = await app.getPluginStorage(plugin!.id, 'event_log');
      const events = JSON.parse(eventLog);

      // Should have 3 clip:created events
      const clipCreatedEvents = events.filter((e: any) => e.event === 'clip:created');
      expect(clipCreatedEvents.length).toBe(3);

      // Each event should have timestamp
      for (const event of events) {
        expect(event.time).toBeDefined();
        expect(typeof event.time).toBe('number');
      }
    });
  });

  test.describe('Plugin Error Handling', () => {
    test('should continue working after plugin processes many events', async ({ app }) => {
      const pluginPath = path.join(TEST_PLUGINS_DIR, 'event-tracker.lua');

      // Import plugin
      const plugin = await app.importPluginFromPath(pluginPath);
      expect(plugin).not.toBeNull();
      eventTrackerPluginId = plugin?.id ?? null;

      await app.page.waitForTimeout(500);

      // Create many clips rapidly
      const files: string[] = [];
      for (let i = 0; i < 5; i++) {
        files.push(await createTempFile(generateTestImage(30 + i, 30 + i), 'png'));
      }

      await app.uploadFiles(files);
      await app.expectClipCount(5);
      await app.page.waitForTimeout(1000);

      // Delete all clips
      for (const file of files) {
        const filename = path.basename(file);
        try {
          await app.deleteClip(filename);
        } catch {
          // Might already be deleted or view switched
        }
      }

      await app.page.waitForTimeout(500);

      // Plugin should still be enabled and working
      const plugins = await app.getPlugins();
      const eventTracker = plugins.find(p => p.name === 'Event Tracker');
      expect(eventTracker?.status).not.toBe('error');
    });
  });
});
