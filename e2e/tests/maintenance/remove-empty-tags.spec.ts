import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';
import { selectors } from '../../helpers/selectors';

test.describe('Remove Empty Tags', () => {
  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
  });

  test('deletes a tag with no clips and no subtags', async ({ app }) => {
    await app.createTag('empty');
    await app.expectTagCount(1);

    await app.clickRemoveEmptyTags();
    await app.confirmDialog();
    await app.expectToast('Removed 1 empty tag');

    await app.expectTagCount(0);
  });

  test('keeps tags that have clips', async ({ app }) => {
    await app.createTag('populated');
    await app.createTag('lonely');

    const imagePath = await createTempFile(generateTestImage(20, 20, [200, 100, 50]), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'populated');

    await app.clickRemoveEmptyTags();
    await app.confirmDialog();
    await app.expectToast('Removed 1 empty tag');

    const tags = await app.getAllTags();
    expect(tags.map(t => t.name).sort()).toEqual(['populated']);
  });

  test('keeps a parent that has a non-empty descendant', async ({ app }) => {
    await app.createTag('project');
    await app.createTag('project/active');

    const imagePath = await createTempFile(generateTestImage(20, 20, [0, 120, 200]), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'project/active');

    await app.clickRemoveEmptyTags();

    // Both tags survive: 'project/active' has a clip, 'project' has a non-empty descendant.
    // With no candidates, no confirm dialog should appear — we should see the toast directly.
    await app.expectToast('No empty tags to remove');

    const tags = await app.getAllTags();
    expect(tags.map(t => t.name).sort()).toEqual(['project', 'project/active']);
  });

  test('cascades: removes parent and leaf child in a single pass', async ({ app }) => {
    await app.createTag('orphan');
    await app.createTag('orphan/leaf');
    await app.createTag('orphan/leaf/deep');

    await app.clickRemoveEmptyTags();
    await app.confirmDialog();
    await app.expectToast('Removed 3 empty tags');

    await app.expectTagCount(0);
  });

  test('confirm dialog lists the tag names that will be deleted', async ({ app }) => {
    // Creating 'beta/sub' auto-creates 'beta' too (hierarchy ancestor creation),
    // so the cascade will remove all three.
    await app.createTag('alpha');
    await app.createTag('beta/sub');

    await app.clickRemoveEmptyTags();

    const message = app.page.locator(selectors.confirm.message);
    await expect(message).toContainText('3 empty tags will be deleted');
    await expect(message).toContainText('alpha');
    await expect(message).toContainText('beta');
    await expect(message).toContainText('beta/sub');

    await app.confirmDialog();
    await app.expectToast('Removed 3 empty tags');
    await app.expectTagCount(0);
  });

  test('shows toast when there are no empty tags', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(20, 20, [10, 10, 10]), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('kept');
    await app.addTagToClip(path.basename(imagePath), 'kept');

    await app.clickRemoveEmptyTags();
    await app.expectToast('No empty tags to remove');

    // Confirm dialog should NOT have appeared.
    const dialogOpen = await app.page
      .locator(`${selectors.confirm.dialog}.opacity-100`)
      .count();
    expect(dialogOpen).toBe(0);

    await app.expectTagCount(1);
  });

  test('mixed: deletes only the empty tags in a single run', async ({ app }) => {
    // Populated leaf under a populated parent
    await app.createTag('work');
    await app.createTag('work/live');

    // Empty siblings at the top level
    await app.createTag('junk');
    await app.createTag('stale');

    // Empty descendant chain
    await app.createTag('archive');
    await app.createTag('archive/old');

    const imagePath = await createTempFile(generateTestImage(20, 20, [250, 250, 0]), 'png');
    await app.uploadFile(imagePath);
    await app.addTagToClip(path.basename(imagePath), 'work/live');

    await app.clickRemoveEmptyTags();
    await app.confirmDialog();
    // 'junk', 'stale', 'archive/old', 'archive' = 4 removed; 'work' and 'work/live' kept.
    await app.expectToast('Removed 4 empty tags');

    const tags = await app.getAllTags();
    expect(tags.map(t => t.name).sort()).toEqual(['work', 'work/live']);
  });
});
