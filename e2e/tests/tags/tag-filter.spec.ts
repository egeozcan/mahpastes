import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Tag Filtering', () => {
  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
  });

  test.describe('Single Tag Filter', () => {
    test('should filter clips by single tag', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.createTag('filtered');

      // Only tag first clip
      await app.addTagToClip(filename1, 'filtered');

      // Filter by tag
      await app.filterByTag('filtered');

      // Only tagged clip should be visible
      await app.expectClipCount(1);
      await app.expectClipVisible(filename1);
    });

    test('should show all clips when filter is cleared', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.createTag('temp');
      await app.addTagToClip(filename1, 'temp');

      // Apply and then clear filter
      await app.filterByTag('temp');
      await app.expectClipCount(1);

      await app.clearTagFilters();

      // Both clips should be visible
      await app.expectClipCount(2);
    });
  });

  test.describe('Multiple Tag Filter (AND Logic)', () => {
    test('should filter clips that have ALL selected tags', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const image3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);
      const filename3 = path.basename(image3);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.uploadFile(image3);

      await app.createTag('tag-a');
      await app.createTag('tag-b');

      // Clip 1: has both tags
      await app.addTagToClip(filename1, 'tag-a');
      await app.addTagToClip(filename1, 'tag-b');

      // Clip 2: has only tag-a
      await app.addTagToClip(filename2, 'tag-a');

      // Clip 3: has only tag-b
      await app.addTagToClip(filename3, 'tag-b');

      // Filter by both tags (AND logic)
      await app.filterByTags(['tag-a', 'tag-b']);

      // Only clip1 should be visible (has BOTH tags)
      await app.expectClipCount(1);
      await app.expectClipVisible(filename1);
    });

    test('should show no clips when no clip matches all tags', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);
      const filename2 = path.basename(image2);

      await app.uploadFile(image1);
      await app.uploadFile(image2);

      await app.createTag('exclusive-a');
      await app.createTag('exclusive-b');

      // Each clip has only one tag
      await app.addTagToClip(filename1, 'exclusive-a');
      await app.addTagToClip(filename2, 'exclusive-b');

      // Filter by both - no clip has both
      await app.filterByTags(['exclusive-a', 'exclusive-b']);

      await app.expectClipCount(0);
    });
  });

  test.describe('Filter UI', () => {
    test('should show badge with active filter count', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('counted');
      await app.addTagToClip(filename, 'counted');

      // Reload to get fresh UI with tags
      await app.page.reload();
      await app.waitForReady();

      // Use UI to click on tag filter dropdown and select the tag
      await app.openTagFilterDropdown();
      const checkbox = app.page.locator('[data-testid="tag-checkbox-counted"]');
      await checkbox.click();
      // Wait for gallery to re-render after tag filter change
      await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

      // Badge should show count
      const badge = app.page.locator('#tag-filter-badge');
      await expect(badge).toBeVisible();
      await expect(badge).toHaveText('1');
    });

    test('should show active tag pills below filter button', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.createTag('active');
      await app.addTagToClip(filename, 'active');

      // Reload to get fresh UI with tags
      await app.page.reload();
      await app.waitForReady();

      // Use UI to click on tag filter dropdown and select the tag
      await app.openTagFilterDropdown();
      const checkbox = app.page.locator('[data-testid="tag-checkbox-active"]');
      await checkbox.click();
      // Wait for gallery to re-render after tag filter change
      await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });
      await app.closeTagFilterDropdown();

      // Active tags container should show the tag
      await app.expectTagFilterActive('active');
    });

    test('should update clips when clicking tag pill on card', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(image1);

      await app.uploadFile(image1);
      await app.uploadFile(image2);
      await app.createTag('clickable');
      await app.addTagToClip(filename1, 'clickable');

      // Reload to ensure tag is properly rendered on card
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(2);

      // Click on tag pill on the clip card
      const clip = await app.getClipByFilename(filename1);
      const tagPill = clip.locator('[data-testid="tag-pill-clickable"]');
      await tagPill.click();
      // Wait for gallery to re-render after tag filter change
      await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

      // Should filter to only clips with this tag
      await app.expectClipCount(1);
    });
  });

  test.describe('Filter Persistence', () => {
    test('should maintain filter state after adding new clip', async ({ app }) => {
      const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const filename1 = path.basename(image1);

      await app.uploadFile(image1);
      await app.createTag('persistent');
      await app.addTagToClip(filename1, 'persistent');

      // Apply filter
      await app.filterByTag('persistent');
      await app.expectClipCount(1);

      // Upload another clip (untagged)
      const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      await app.uploadFile(image2);

      // Filter should still be active - only tagged clip visible
      await app.expectClipCount(1);
    });
  });
});
