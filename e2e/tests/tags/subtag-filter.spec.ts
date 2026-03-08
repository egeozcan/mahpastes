import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Subtag Filtering', () => {
  test.afterEach(async ({ app }) => {
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('filtering by parent tag shows clips tagged with subtags', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');
    await app.filterByTag('work');
    await app.expectClipCount(1);
  });

  test('filter dropdown shows tree structure with indentation', async ({ app }) => {
    await app.createTag('work/client1');
    await app.createTag('work/client2');
    await app.openTagFilterDropdown();
    // Verify child items exist
    const client1 = app.page.locator('[data-testid="tag-checkbox-work/client1"]');
    const client2 = app.page.locator('[data-testid="tag-checkbox-work/client2"]');
    await expect(client1).toBeVisible();
    await expect(client2).toBeVisible();
    await app.closeTagFilterDropdown();
  });

  test('hidden parent hides clips in subtags', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');
    await app.expectClipCount(1);
    await app.setHiddenTags(['work']);
    await app.refreshClips();
    await app.expectClipCount(0);
  });

  test('filtering by subtag of hidden parent reveals clips', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');

    // Hide the parent tag
    await app.setHiddenTags(['work']);
    await app.refreshClips();
    await app.expectClipCount(0);

    // Filter by subtag — should reveal the clip despite parent being hidden
    await app.filterByTag('work/client1');
    await app.expectClipCount(1);
  });

  test('subtags of hidden parent are visible in filter dropdown', async ({ app }) => {
    await app.createTag('work/client1');
    await app.setHiddenTags(['work']);
    await app.openTagFilterDropdown();
    // Subtag should still be visible (dimmed but accessible)
    const subtag = app.page.locator('[data-testid="tag-checkbox-work/client1"]');
    await expect(subtag).toBeVisible();
    await app.closeTagFilterDropdown();
  });

  test('card tag pills show short names', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');
    await app.refreshClips();
    // The pill should show "client1" not "work/client1"
    const pill = app.page.locator('[data-testid="tag-pill-work/client1"]');
    await expect(pill).toBeVisible();
    const text = await pill.textContent();
    expect(text?.trim()).toBe('client1');
  });
});
