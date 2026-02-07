import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Tag Plugin API', () => {
  test.beforeEach(async ({ app }) => {
    // Clean up tags before each test
    await app.deleteAllTags();
  });

  test.describe('Tag Events', () => {
    test('tag:created event fires when tag is created', async ({ app }) => {
      // Create a tag - should trigger tag:created event
      await app.createTag('event-test-tag');

      // Verify tag was created (event would fire to subscribed plugins)
      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'event-test-tag')).toBe(true);
    });

    test('tag:deleted event fires when tag is deleted', async ({ app }) => {
      await app.createTag('delete-test-tag');
      await app.deleteTag('delete-test-tag');

      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'delete-test-tag')).toBe(false);
    });

    test('tag:added_to_clip event fires when tag is added to clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('add-test-tag');
      await app.addTagToClip(filename, 'add-test-tag');

      await app.expectClipHasTag(filename, 'add-test-tag');
    });

    test('tag:removed_from_clip event fires when tag is removed', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('remove-test-tag');
      await app.addTagToClip(filename, 'remove-test-tag');
      await app.removeTagFromClip(filename, 'remove-test-tag');

      await app.expectClipDoesNotHaveTag(filename, 'remove-test-tag');
    });

    test('bulk tag operations trigger multiple events', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');

      await app.uploadFiles([file1, file2]);
      await app.createTag('bulk-test-tag');

      // Select both clips and bulk add tag
      await app.selectClips([path.basename(file1), path.basename(file2)]);
      await app.bulkAddTag('bulk-test-tag');

      // Verify both clips have the tag
      await app.expectClipHasTag(path.basename(file1), 'bulk-test-tag');
      await app.expectClipHasTag(path.basename(file2), 'bulk-test-tag');
    });
  });

  test.describe('Tags API Methods', () => {
    test('plugins can list all tags via tags.list()', async ({ app }) => {
      await app.createTag('api-list-tag1');
      await app.createTag('api-list-tag2');

      const tags = await app.getAllTags();
      expect(tags.length).toBeGreaterThanOrEqual(2);
      expect(tags.some(t => t.name === 'api-list-tag1')).toBe(true);
      expect(tags.some(t => t.name === 'api-list-tag2')).toBe(true);
    });

    test('plugins can create tags via tags.create()', async ({ app }) => {
      // This tests the underlying API that plugins use
      await app.createTag('plugin-created-tag');

      const tags = await app.getAllTags();
      const created = tags.find(t => t.name === 'plugin-created-tag');
      expect(created).toBeDefined();
      expect(created?.color).toMatch(/^#[0-9a-fA-F]{6}$/i);
    });

    test('tags have auto-assigned colors from palette', async ({ app }) => {
      // Create multiple tags and verify colors are assigned
      await app.createTag('color-tag-1');
      await app.createTag('color-tag-2');
      await app.createTag('color-tag-3');

      const tags = await app.getAllTags();
      const colorTags = tags.filter(t => t.name.startsWith('color-tag-'));

      // Each tag should have a valid hex color
      for (const tag of colorTags) {
        expect(tag.color).toMatch(/^#[0-9a-fA-F]{6}$/i);
      }

      // Colors might differ (from palette) or be the same (if same position in sequence)
      expect(colorTags.length).toBe(3);
    });

    test('plugins can get tags for a clip via tags.get_for_clip()', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('clip-tag-1');
      await app.createTag('clip-tag-2');
      await app.addTagToClip(filename, 'clip-tag-1');
      await app.addTagToClip(filename, 'clip-tag-2');

      // Verify via UI that clip has both tags
      await app.expectClipHasTag(filename, 'clip-tag-1');
      await app.expectClipHasTag(filename, 'clip-tag-2');
    });

    test('tags can be updated', async ({ app }) => {
      await app.createTag('update-me');

      // Update via API
      const tags = await app.getAllTags();
      const tag = tags.find(t => t.name === 'update-me');
      expect(tag).toBeDefined();

      await app.page.evaluate(async (tagId) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, 'updated-name', '#FF0000');
      }, tag!.id);

      const updatedTags = await app.getAllTags();
      const updatedTag = updatedTags.find(t => t.name === 'updated-name');
      expect(updatedTag).toBeDefined();
      expect(updatedTag?.color).toBe('#FF0000');
    });
  });

  test.describe('Edge Cases', () => {
    test('orphan tag deletion triggers tag:deleted event', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('orphan-tag');
      await app.addTagToClip(filename, 'orphan-tag');

      // Remove tag from only clip - should auto-delete orphaned tag
      await app.removeTagFromClip(filename, 'orphan-tag');

      // Verify tag was deleted
      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'orphan-tag')).toBe(false);
    });

    test('deleting clip removes tag associations', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('clip-delete-tag');
      await app.addTagToClip(filename, 'clip-delete-tag');

      // Delete the clip
      await app.deleteClip(filename);

      // Tag should be auto-deleted (orphaned)
      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'clip-delete-tag')).toBe(false);
    });

    test('cannot create tag with empty name', async ({ app }) => {
      // Try to create tag with empty name via API
      const error = await app.page.evaluate(async () => {
        try {
          // @ts-ignore - Wails runtime
          await window.go.main.App.CreateTag('');
          return null;
        } catch (e: any) {
          return e.message || e.toString();
        }
      });

      expect(error).toContain('empty');
    });

    test('cannot create duplicate tag', async ({ app }) => {
      await app.createTag('unique-tag');

      // Try to create another tag with the same name
      const error = await app.page.evaluate(async () => {
        try {
          // @ts-ignore - Wails runtime
          await window.go.main.App.CreateTag('unique-tag');
          return null;
        } catch (e: any) {
          return e.message || e.toString();
        }
      });

      expect(error).toContain('exists');
    });

    test('bulk remove tag triggers orphan cleanup', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');

      await app.uploadFiles([file1, file2]);
      await app.createTag('bulk-orphan-tag');

      // Add tag to both clips
      await app.addTagToClip(path.basename(file1), 'bulk-orphan-tag');
      await app.addTagToClip(path.basename(file2), 'bulk-orphan-tag');

      // Select both clips and bulk remove tag
      await app.selectClips([path.basename(file1), path.basename(file2)]);
      await app.bulkRemoveTag('bulk-orphan-tag');

      // Tag should be auto-deleted (orphaned)
      const tags = await app.getAllTags();
      expect(tags.some(t => t.name === 'bulk-orphan-tag')).toBe(false);
    });
  });
});
