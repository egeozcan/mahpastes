import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Plugin System', () => {
  test.beforeEach(async ({ app }) => {
    await app.deleteAllPlugins();
  });

  test.describe('Plugin Management', () => {
    test('should start with no plugins', async ({ app }) => {
      await app.expectPluginCount(0);
    });

    test('should list plugins via API', async ({ app }) => {
      const plugins = await app.getPlugins();
      expect(Array.isArray(plugins)).toBe(true);
    });
  });

  test.describe('Plugin API', () => {
    // Plugin APIs are exposed via PluginService (separate from App) to work around
    // Wails method binding limits. Tests verify the PluginService is available.

    test('should have GetPlugins API available', async ({ app }) => {
      const hasApi = await app.isPluginApiAvailable();
      expect(hasApi).toBe(true);
    });

    test('should have EnablePlugin API available', async ({ app }) => {
      const hasApi = await app.page.evaluate(() => {
        // @ts-ignore
        return typeof window.go?.main?.PluginService?.EnablePlugin === 'function';
      });
      expect(hasApi).toBe(true);
    });

    test('should have DisablePlugin API available', async ({ app }) => {
      const hasApi = await app.page.evaluate(() => {
        // @ts-ignore
        return typeof window.go?.main?.PluginService?.DisablePlugin === 'function';
      });
      expect(hasApi).toBe(true);
    });

    test('should have RemovePlugin API available', async ({ app }) => {
      const hasApi = await app.page.evaluate(() => {
        // @ts-ignore
        return typeof window.go?.main?.PluginService?.RemovePlugin === 'function';
      });
      expect(hasApi).toBe(true);
    });

    test('should have GetPluginPermissions API available', async ({ app }) => {
      const hasApi = await app.page.evaluate(() => {
        // @ts-ignore
        return typeof window.go?.main?.PluginService?.GetPluginPermissions === 'function';
      });
      expect(hasApi).toBe(true);
    });
  });

  test.describe('Plugin Events', () => {
    // These tests verify that clip operations work and would trigger plugin events
    // We can't directly verify plugin execution in e2e, but we verify
    // the clip operations that trigger events work correctly

    test('should emit clip:created event when clip is uploaded', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Verify clip was created (this triggers clip:created event)
      await app.expectClipCount(1);
    });

    test('should emit clip:deleted event when clip is deleted', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Delete triggers clip:deleted event
      await app.deleteClip(filename);
      await app.expectClipCount(0);
    });

    test('should emit clip:archived event when clip is archived', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      // Archive triggers clip:archived event
      await app.archiveClip(filename);

      // Switch to archive view to verify
      await app.toggleArchiveView();
      await app.expectClipVisible(filename);
    });
  });

  test.describe('Plugin Persistence', () => {
    test('should return empty array when no plugins registered', async ({ app }) => {
      const plugins = await app.getPlugins();
      expect(plugins).toEqual([]);
    });

    test('should return empty permissions for non-existent plugin', async ({ app }) => {
      const permissions = await app.getPluginPermissions(999999);
      expect(permissions).toEqual([]);
    });
  });
});

test.describe('Plugin Integration with Clips', () => {
  test('clips operations work regardless of plugin state', async ({ app }) => {
    // Ensure basic clip operations work even with plugin system active
    const imagePath = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const filename = path.basename(imagePath);

    // Upload
    await app.uploadFile(imagePath);
    await app.expectClipVisible(filename);

    // Archive
    await app.archiveClip(filename);
    await app.toggleArchiveView();
    await app.expectClipVisible(filename);

    // Unarchive (toggle again)
    await app.archiveClip(filename);
    await app.toggleArchiveView();
    await app.expectClipVisible(filename);

    // Delete
    await app.deleteClip(filename);
    await app.expectClipCount(0);
  });

  test('multiple clip operations trigger multiple events', async ({ app }) => {
    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

    const filename1 = path.basename(file1);
    const filename2 = path.basename(file2);
    const filename3 = path.basename(file3);

    // Upload multiple files - should trigger 3 clip:created events
    await app.uploadFiles([file1, file2, file3]);
    await app.expectClipCount(3);

    // Delete one - should trigger 1 clip:deleted event
    await app.deleteClip(filename1);
    await app.expectClipCount(2);

    // Archive one - should trigger 1 clip:archived event
    await app.archiveClip(filename2);

    // Verify remaining clips
    await app.expectClipVisible(filename3);
  });
});
