import { test } from '../../fixtures/test-fixtures';

test.describe('Folder view — delete', () => {
  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed').catch(() => 'false');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('navigates up when current folder is deleted', async ({ app }) => {
    await app.createTag('work/clientA');
    await app.enterFolder('work/clientA');

    // Deleting the current folder should navigate up to the parent.
    await app.deleteTag('work/clientA');

    // Expect nav to parent 'work'.
    await app.expectFolderHeader('work');
  });

  test('exits folder mode when deleted tag has no parent', async ({ app }) => {
    await app.createTag('lonely');
    await app.enterFolder('lonely');

    // Deleting a root tag with no parent should exit folder mode entirely.
    await app.deleteTag('lonely');

    await app.expectNotInFolderMode();
  });
});
