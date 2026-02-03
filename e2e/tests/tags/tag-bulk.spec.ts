import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Bulk Tag Operations', () => {
  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
  });

  test.describe('Bulk Add Tags', () => {
    test('should add tag to multiple selected clips', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.createTag('batch');

      // Select both clips
      await app.selectClips([filename1, filename2]);

      // Bulk add tag
      await app.bulkAddTag('batch');

      // Both clips should have the tag
      await app.expectClipHasTag(filename1, 'batch');
      await app.expectClipHasTag(filename2, 'batch');
    });

    test('should show bulk tag button when clips selected', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);

      // Select clip
      await app.selectClip(filename);

      // Bulk tag button should be visible
      const bulkTagBtn = app.page.locator('#bulk-tag-btn');
      await expect(bulkTagBtn).toBeVisible();
    });

    test('should bulk add tag to all selected clips', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(40, 40, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(40, 40, [0, 255, 0]), 'png');
      const image3 = await createTempFile(generateTestImage(40, 40, [0, 0, 255]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);
      const filename3 = path.basename(image3);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.uploadFile(image3);
      await app.createTag('bulktag');

      // Select all
      await app.selectAll();

      // Bulk add tag
      await app.bulkAddTag('bulktag');

      // All clips should have the tag
      await app.expectClipHasTag(filename1, 'bulktag');
      await app.expectClipHasTag(filename2, 'bulktag');
      await app.expectClipHasTag(filename3, 'bulktag');
    });
  });

  test.describe('Bulk Remove Tags', () => {
    test('should remove tag from multiple selected clips', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.createTag('toremove');

      // Add tag to both clips first
      await app.addTagToClip(filename1, 'toremove');
      await app.addTagToClip(filename2, 'toremove');

      // Verify tags are added
      await app.expectClipHasTag(filename1, 'toremove');
      await app.expectClipHasTag(filename2, 'toremove');

      // Select both and remove tag
      await app.selectClips([filename1, filename2]);
      await app.bulkRemoveTag('toremove');

      // Both clips should no longer have the tag
      await app.expectClipDoesNotHaveTag(filename1, 'toremove');
      await app.expectClipDoesNotHaveTag(filename2, 'toremove');
    });
  });
});
