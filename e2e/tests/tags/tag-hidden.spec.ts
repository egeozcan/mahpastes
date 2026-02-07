import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Hidden Tags', () => {
  test('should hide clips with a hidden tag from gallery', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename1 = path.basename(image1);
    const filename2 = path.basename(image2);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.expectClipCount(2);

    await app.createTag('secret');
    await app.addTagToClip(filename1, 'secret');

    // Hide the tag
    await app.setHiddenTags(['secret']);
    await app.refreshClips();

    // Only the non-hidden clip should be visible
    await app.expectClipCount(1);
    await app.expectClipVisible(filename2);
  });

  test('should hide clip with both hidden and visible tags (hide wins)', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('visible');
    await app.createTag('hidden');
    await app.addTagToClip(filename1, 'visible');
    await app.addTagToClip(filename1, 'hidden');

    // Hide one of the tags
    await app.setHiddenTags(['hidden']);
    await app.refreshClips();

    // Clip should be hidden (hide wins)
    await app.expectClipCount(0);
  });

  test('should show hidden clips when explicitly filtering by hidden tag', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.createTag('private');
    await app.addTagToClip(filename1, 'private');

    // Hide the tag
    await app.setHiddenTags(['private']);
    await app.refreshClips();
    await app.expectClipCount(1);

    // Explicitly filter by the hidden tag - should override hiding
    await app.filterByTag('private');
    await app.expectClipCount(1);
    await app.expectClipVisible(filename1);
  });

  test('should persist hidden tag setting across page reloads', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename2 = path.basename(image2);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.createTag('persist-test');
    await app.addTagToClip(path.basename(image1), 'persist-test');

    // Hide the tag
    await app.setHiddenTags(['persist-test']);

    // Reload the page
    await app.page.reload();
    await app.waitForReady();

    // Hidden tags should still be in effect
    await app.expectClipCount(1);
    await app.expectClipVisible(filename2);
  });

  test('should toggle hidden tag in settings modal', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('toggle-test');
    await app.addTagToClip(filename1, 'toggle-test');

    // Reload to get fresh tag state in UI
    await app.page.reload();
    await app.waitForReady();
    await app.expectClipCount(1);

    // Open settings and toggle hidden
    await app.openSettingsModal();
    await app.toggleHiddenTagInSettings('toggle-test');
    await app.closeSettingsModal();

    // Clip should now be hidden
    await app.expectClipCount(0);

    // Toggle back
    await app.openSettingsModal();
    await app.toggleHiddenTagInSettings('toggle-test');
    await app.closeSettingsModal();

    // Clip should be visible again
    await app.expectClipCount(1);
  });

  test('should dim hidden tags in filter dropdown', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('dimmed');
    await app.addTagToClip(filename1, 'dimmed');

    // Hide the tag
    await app.setHiddenTags(['dimmed']);

    // Reload to get fresh UI
    await app.page.reload();
    await app.waitForReady();

    // Open filter dropdown
    await app.openTagFilterDropdown();

    // The hidden tag's label should have opacity-50 class
    const tagLabel = app.page.locator('[data-testid="tag-checkbox-dimmed"]').locator('..');
    await expect(tagLabel).toHaveClass(/opacity-50/);
  });
});
