import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Tag Clips', () => {
  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
  });

  test.describe('Add Tags to Clips', () => {
    test('should add a tag to a clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('project');

      await app.addTagToClip(filename, 'project');

      await app.expectClipHasTag(filename, 'project');
    });

    test('should add multiple tags to a clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('urgent');
      await app.createTag('review');

      await app.addTagToClip(filename, 'urgent');
      await app.addTagToClip(filename, 'review');

      await app.expectClipHasTag(filename, 'urgent');
      await app.expectClipHasTag(filename, 'review');
    });

    test('should display tag pill on clip card', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('labeled');
      await app.addTagToClip(filename, 'labeled');

      const clip = await app.getClipByFilename(filename);
      const tagsContainer = clip.locator('.clip-tags');
      await expect(tagsContainer).toBeVisible();

      const tagPill = clip.locator('[data-testid="tag-pill-labeled"]');
      await expect(tagPill).toBeVisible();
    });
  });

  test.describe('Remove Tags from Clips', () => {
    test('should remove a tag from a clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('removable');
      await app.addTagToClip(filename, 'removable');

      // Verify tag is added
      await app.expectClipHasTag(filename, 'removable');

      // Remove the tag
      await app.removeTagFromClip(filename, 'removable');

      await app.expectClipDoesNotHaveTag(filename, 'removable');
    });

    test('should auto-delete tag when it has no more clips', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('orphan-test');
      await app.addTagToClip(filename, 'orphan-test');

      // Verify tag exists
      await app.expectTagCount(1);
      await app.expectClipHasTag(filename, 'orphan-test');

      // Remove the tag from the only clip that has it
      await app.removeTagFromClip(filename, 'orphan-test');

      // Tag should be auto-deleted since no clips have it
      await app.expectTagCount(0);
    });

    test('should not delete tag when other clips still use it', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const filename1 = path.basename(imagePath1);
      const filename2 = path.basename(imagePath2);

      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.createTag('shared-tag');
      await app.addTagToClip(filename1, 'shared-tag');
      await app.addTagToClip(filename2, 'shared-tag');

      // Verify tag exists on both clips
      await app.expectTagCount(1);
      await app.expectClipHasTag(filename1, 'shared-tag');
      await app.expectClipHasTag(filename2, 'shared-tag');

      // Remove tag from first clip only
      await app.removeTagFromClip(filename1, 'shared-tag');

      // Tag should still exist since second clip has it
      await app.expectTagCount(1);
      await app.expectClipHasTag(filename2, 'shared-tag');
    });
  });

  test.describe('Tag Auto-Delete on Clip Deletion', () => {
    test('should auto-delete tag when its only clip is deleted', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('delete-test');
      await app.addTagToClip(filename, 'delete-test');

      // Verify tag exists
      await app.expectTagCount(1);

      // Delete the clip
      await app.deleteClip(filename);

      // Tag should be auto-deleted since no clips have it
      await app.expectTagCount(0);
    });

    test('should auto-delete tag when last clip using it is bulk deleted', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const filename1 = path.basename(imagePath1);
      const filename2 = path.basename(imagePath2);

      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.createTag('bulk-delete-test');
      await app.addTagToClip(filename1, 'bulk-delete-test');
      await app.addTagToClip(filename2, 'bulk-delete-test');

      // Verify tag exists on both clips
      await app.expectTagCount(1);

      // Bulk delete both clips
      await app.selectClip(filename1);
      await app.selectClip(filename2);
      await app.bulkDelete();
      await app.confirmDialog();

      // Tag should be auto-deleted since no clips remain
      await app.expectTagCount(0);
    });

    test('should not delete tag when only some clips using it are deleted', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const filename1 = path.basename(imagePath1);
      const filename2 = path.basename(imagePath2);

      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.createTag('partial-delete');
      await app.addTagToClip(filename1, 'partial-delete');
      await app.addTagToClip(filename2, 'partial-delete');

      // Verify tag exists
      await app.expectTagCount(1);

      // Delete only one clip
      await app.deleteClip(filename1);

      // Tag should still exist since second clip has it
      await app.expectTagCount(1);
      await app.expectClipHasTag(filename2, 'partial-delete');
    });
  });

  test.describe('Tag Persistence on Clips', () => {
    test('should persist clip tags after page reload', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('persistent');
      await app.addTagToClip(filename, 'persistent');

      await app.page.reload();
      await app.waitForReady();

      await app.expectClipHasTag(filename, 'persistent');
    });

    test('should remove tag from clip when tag is deleted', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('cascade');
      await app.addTagToClip(filename, 'cascade');

      // Delete the tag
      await app.deleteTag('cascade');

      await app.page.reload();
      await app.waitForReady();

      // Clip should still exist but without the tag
      await app.expectClipVisible(filename);
      await app.expectClipDoesNotHaveTag(filename, 'cascade');
    });
  });

  test.describe('Tag Display Limits', () => {
    test('should display tags on clip card', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('one');
      await app.createTag('two');
      await app.createTag('three');

      await app.addTagToClip(filename, 'one');
      await app.addTagToClip(filename, 'two');
      await app.addTagToClip(filename, 'three');

      const clip = await app.getClipByFilename(filename);
      const tagsContainer = clip.locator('.clip-tags');
      await expect(tagsContainer).toBeVisible();
    });
  });
});
