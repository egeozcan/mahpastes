import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import path from 'path';

test.describe('Folder Mode', () => {
  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('folder mode button toggles state', async ({ app }) => {
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    await expect(btn).toHaveAttribute('aria-pressed', 'false');
    await btn.click();
    await expect(btn).toHaveAttribute('aria-pressed', 'true');
    await btn.click();
    await expect(btn).toHaveAttribute('aria-pressed', 'false');
  });

  test('folder mode shows tag folders at root', async ({ app }) => {
    await app.createTag('work');
    await app.createTag('personal');
    await app.toggleFolderMode();
    await app.expectFolderVisible('work');
    await app.expectFolderVisible('personal');
  });

  test('clicking folder navigates into subtag', async ({ app }) => {
    await app.createTag('work/client1');
    await app.createTag('work/client2');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.expectFolderVisible('client1');
    await app.expectFolderVisible('client2');
  });

  test('nested folder keeps clips tagged to the current folder visible', async ({ app }) => {
    await app.createTag('work/client1/projectABC');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.addTagToClip(filename, 'work/client1');

    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.clickFolder('client1');

    await app.expectFolderVisible('projectABC');
    await app.expectClipVisible(filename);
  });

  test('folder mode shows directly-tagged clips alongside folders', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    // Should see client1 folder AND the clip
    await app.expectFolderVisible('client1');
    await app.expectClipCount(1);
  });

  test('root level only shows untagged clips', async ({ app }) => {
    // Create a tag and a tagged clip
    await app.createTag('photos');
    const taggedPath = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
    const taggedName = path.basename(taggedPath);
    await app.uploadFile(taggedPath);
    await app.addTagToClip(taggedName, 'photos');

    // Create an untagged clip
    const untaggedPath = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
    const untaggedName = path.basename(untaggedPath);
    await app.uploadFile(untaggedPath);

    await app.toggleFolderMode();

    // Should see the folder for "photos" and only the untagged clip
    await app.expectFolderVisible('photos');
    await app.expectClipVisible(untaggedName);
    await app.expectClipNotVisible(taggedName);
  });

  test('clips tagged with subtag do not appear in parent folder', async ({ app }) => {
    // Create parent and child tags
    await app.createTag('work/client1');

    // Create a clip tagged with the subtag only
    const subtag_path = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
    const subtagName = path.basename(subtag_path);
    await app.uploadFile(subtag_path);
    await app.addTagToClip(subtagName, 'work/client1');

    // Create a clip tagged directly with the parent
    const parentPath = await createTempFile(generateTestImage(100, 100, 'yellow'), 'png');
    const parentName = path.basename(parentPath);
    await app.uploadFile(parentPath);
    await app.addTagToClip(parentName, 'work');

    await app.toggleFolderMode();
    await app.clickFolder('work');

    // Should see the client1 subfolder and the directly-tagged clip
    await app.expectFolderVisible('client1');
    await app.expectClipVisible(parentName);
    // Should NOT see the clip that's only tagged with work/client1
    await app.expectClipNotVisible(subtagName);
  });

  test('exiting folder mode preserves tag filters', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/client1');
    await app.toggleFolderMode();
    await app.clickFolder('work');
    // Exit folder mode — filter should remain on "work"
    await app.toggleFolderMode();
    // Clip should still be visible (filtered by work, which includes descendants)
    await app.expectClipCount(1);
  });

  test('enabling folder mode after selecting a subtag renders breadcrumb pills', async ({ app }) => {
    await app.createTag('work/client1');

    await app.filterByTag('work/client1');
    await app.toggleFolderMode();

    await expect(app.page.locator(selectors.tags.tagPill('work'))).toBeVisible();
    await expect(app.page.locator(selectors.tags.tagPill('work/client1'))).toBeVisible();
  });

  test('folder mode shows full subtag paths on clip cards', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.addTagToClip(filename, 'work');
    await app.addTagToClip(filename, 'work/client1');

    await app.toggleFolderMode();
    await app.clickFolder('work');
    await app.clickFolder('client1');

    const clip = await app.getClipByFilename(filename);
    const subtagPill = clip.locator(selectors.tags.tagPill('work/client1'));
    await expect(subtagPill).toHaveText('work/client1');
  });
});
