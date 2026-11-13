import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// An empty library was one grey sentence, and an empty folder said "No clips
// match the selected tags." Both now say what they are and offer the ways in.
test.describe('Empty states offer a next step', () => {
  const status = (app: any) => app.page.locator(selectors.gallery.emptyState);
  const action = (app: any, name: string) => app.page.locator(`[data-testid="gallery-status-${name}"]`);

  test('an empty library offers Add files and Import a folder', async ({ app }) => {
    await expect(status(app)).toHaveText('No active clips. Paste or drop something!');
    await expect(action(app, 'add')).toBeVisible();
    await expect(action(app, 'import')).toBeVisible();
    await expect(action(app, 'new-folder')).toBeHidden();
    await expect(action(app, 'retry')).toBeHidden();

    // Add files opens the same picker as the bottom bar's button.
    const chooser = app.page.waitForEvent('filechooser');
    await action(app, 'add').click();
    const filePath = await createTempFile(generateTestImage(), 'png');
    await (await chooser).setFiles([filePath]);
    await app.expectClipVisible(path.basename(filePath));
    await expect(status(app)).toBeHidden();
    await expect(action(app, 'add')).toBeHidden();
  });

  test('a filter that matches nothing offers no actions', async ({ app }) => {
    await app.createTag('lonely');
    await app.filterByTag('lonely');
    await expect(status(app)).toHaveText('No clips match the selected tags.');
    await expect(app.page.locator('[data-testid="gallery-status-actions"]')).toBeHidden();
  });

  test('the archive says it is empty and offers nothing to add', async ({ app }) => {
    await app.toggleArchiveView();
    await expect(status(app)).toHaveText('No archived clips.');
    await expect(app.page.locator('[data-testid="gallery-status-actions"]')).toBeHidden();
  });

  test('folder mode: the first folder can be created from an empty library, and a subfolder inside an empty folder', async ({ app }) => {
    await app.toggleFolderMode();
    await expect(action(app, 'new-folder')).toBeVisible();

    await action(app, 'new-folder').click();
    await app.page.locator(selectors.prompt.input).fill('projects');
    await app.page.locator(selectors.prompt.saveButton).click();
    await app.expectFolderVisible('projects');

    await app.clickFolder('projects');
    await expect(status(app)).toHaveText('This folder is empty. Paste or drop something to add it here.');
    await expect(action(app, 'import')).toBeHidden();

    // New folder is prefilled with the current folder's path.
    await action(app, 'new-folder').click();
    await expect(app.page.locator(selectors.prompt.input)).toHaveValue('projects/');
    await app.page.locator(selectors.prompt.input).fill('projects/alpha');
    await app.page.locator(selectors.prompt.saveButton).click();
    await app.expectFolderVisible('alpha');
    await expect(status(app)).toBeHidden();
  });
});
