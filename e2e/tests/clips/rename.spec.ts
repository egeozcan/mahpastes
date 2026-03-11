import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Rename Clip', () => {
  test('should rename an image clip via context menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipVisible(originalName);

    // Open context menu and click Rename
    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();

    // Prompt dialog should appear with current filename
    const promptDialog = app.page.locator(selectors.prompt.dialog);
    await expect(promptDialog).not.toHaveAttribute('inert', '');
    const input = app.page.locator(selectors.prompt.input);
    await expect(input).toBeVisible();
    await expect(input).toHaveValue(originalName);

    // Clear and type new name
    await input.fill('renamed-image.png');
    await app.page.locator(selectors.prompt.saveButton).click();

    // Verify toast and updated card
    await expect(app.page.locator(selectors.toast.container)).toContainText('Clip renamed');
    await app.expectClipVisible('renamed-image.png');
  });

  test('should rename a text clip via context menu', async ({ app }) => {
    const textPath = await createTempFile(generateTestText('rename-test'), 'txt');
    const originalName = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.expectClipVisible(originalName);

    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();

    const input = app.page.locator(selectors.prompt.input);
    await input.fill('new-name.txt');
    await app.page.locator(selectors.prompt.saveButton).click();

    await expect(app.page.locator(selectors.toast.container)).toContainText('Clip renamed');
    await app.expectClipVisible('new-name.txt');
  });

  test('should cancel rename without changes', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();

    const input = app.page.locator(selectors.prompt.input);
    await input.fill('should-not-apply.png');
    await app.page.locator(selectors.prompt.cancelButton).click();

    // Original name should still be visible
    await app.expectClipVisible(originalName);
  });

  test('should submit rename with Enter key', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const originalName = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openCardMenu(originalName);
    await app.page.locator(selectors.cardMenu.rename).click();

    const input = app.page.locator(selectors.prompt.input);
    await input.fill('enter-rename.png');
    await input.press('Enter');

    await expect(app.page.locator(selectors.toast.container)).toContainText('Clip renamed');
    await app.expectClipVisible('enter-rename.png');
  });
});
