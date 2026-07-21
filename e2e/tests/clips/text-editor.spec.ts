import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Text Editor', () => {
  test('Save As asks for a filename and preserves the original clip', async ({ app }) => {
    const originalContent = 'original text';
    const textPath = await createTempFile(originalContent, 'txt');
    const filename = path.basename(textPath);
    const copyFilename = filename.replace('.txt', '-copy.txt');

    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await expect(app.page.locator(selectors.textEditor.currentFilename)).toHaveText(filename);
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeDisabled();

    await app.setTextEditorContent('edited copy');
    await expect(app.page.locator(selectors.textEditor.saveButton)).toBeEnabled();
    await app.page.locator(selectors.textEditor.saveAsButton).click();

    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toHaveValue(
      filename.replace('.txt', '_edited.txt'),
    );

    await app.page.locator(selectors.textEditor.saveAsInput).press('Escape');
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.saveAsButton)).toBeFocused();

    await app.page.locator(selectors.textEditor.saveAsButton).click();
    await app.page.locator(selectors.textEditor.saveAsInput).fill(copyFilename);
    await app.page.locator(selectors.textEditor.saveAsButton).click();
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);

    await app.expectClipCount(2);

    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue(originalContent);
    await app.cancelTextEditor();

    await app.openTextEditor(copyFilename);
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('edited copy');
    await app.cancelTextEditor();
  });

  test('formats JSON and warns before saving invalid JSON', async ({ app }) => {
    const jsonPath = await createTempFile('{"name":"mahpastes","items":[1,2]}', 'json');
    const filename = path.basename(jsonPath);

    await app.uploadFile(jsonPath);
    await app.openTextEditor(filename);

    await expect(app.page.locator(selectors.textEditor.formatJSONButton)).toBeVisible();
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toContainText('Valid JSON');

    await app.page.locator(selectors.textEditor.formatJSONButton).click();
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue(
      '{\n  "name": "mahpastes",\n  "items": [\n    1,\n    2\n  ]\n}',
    );

    await app.setTextEditorContent('{"broken": }');
    await expect(app.page.locator(selectors.textEditor.validationStatus)).toContainText('Invalid JSON');

    await app.page.locator(selectors.textEditor.saveButton).click();
    await expect(app.page.locator(selectors.confirm.title)).toHaveText('Save invalid JSON?');
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);

    await app.page.locator(selectors.confirm.confirmButton).click();
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);

    await app.openTextEditor(filename);
    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('{"broken": }');
    await app.cancelTextEditor();
  });

  test('finds and replaces all matches without moving keyboard focus', async ({ app }) => {
    const originalContent = `alpha\n${'line\n'.repeat(80)}alpha`;
    const textPath = await createTempFile(originalContent, 'txt');
    const filename = path.basename(textPath);

    await app.uploadFile(textPath);
    await app.openTextEditor(filename);
    await app.page.locator(selectors.textEditor.findToggle).click();

    const findInput = app.page.locator(selectors.textEditor.findInput);
    await findInput.pressSequentially('alpha');
    await expect(findInput).toBeFocused();
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toContainText('1 of 2');
    await expect(app.page.locator(selectors.textEditor.searchMatches)).toHaveCount(2);
    const activeMatch = app.page.locator(selectors.textEditor.activeSearchMatch);
    await expect(activeMatch).toHaveText('alpha');
    await expect(activeMatch).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');

    await findInput.press('Enter');
    await expect(findInput).toBeFocused();
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toContainText('2 of 2');
    await expect(app.page.locator(selectors.textEditor.activeSearchMatch)).toHaveText('alpha');
    await expect.poll(() => app.page.locator(selectors.textEditor.textarea).evaluate(
      (element: HTMLTextAreaElement) => element.scrollTop,
    )).toBeGreaterThan(0);

    await app.page.locator(selectors.textEditor.replaceInput).fill('gamma');
    await app.page.locator(selectors.textEditor.replaceAllButton).click();

    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue(
      originalContent.replaceAll('alpha', 'gamma'),
    );
    await expect(app.page.locator(selectors.textEditor.searchStatus)).toContainText('No matches');

    await findInput.focus();
    await findInput.press('Escape');
    await expect(app.page.locator(selectors.textEditor.findPanel)).toBeHidden();
    await expect(app.page.locator(selectors.textEditor.textarea)).toBeFocused();
    await app.cancelTextEditor();
  });

  test('toggles wrapping and reports the cursor position and character count', async ({ app }) => {
    const textPath = await createTempFile('one\ntwo', 'txt');
    const filename = path.basename(textPath);

    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    const wrapToggle = app.page.locator(selectors.textEditor.wrapToggle);
    const textarea = app.page.locator(selectors.textEditor.textarea);
    await expect(wrapToggle).toHaveAttribute('aria-pressed', 'true');
    await wrapToggle.click();
    await expect(wrapToggle).toHaveAttribute('aria-pressed', 'false');
    await expect(textarea).toHaveAttribute('wrap', 'off');

    await textarea.evaluate((element: HTMLTextAreaElement) => {
      element.focus();
      element.setSelectionRange(5, 5);
      element.dispatchEvent(new Event('select', { bubbles: true }));
    });

    await expect(app.page.locator(selectors.textEditor.cursorStatus)).toHaveText('Ln 2, Col 2');
    await expect(app.page.locator(selectors.textEditor.characterStatus)).toHaveText('7 characters');
    await app.cancelTextEditor();
  });

  test('recovers an unsaved draft after the page reloads', async ({ app }) => {
    const textPath = await createTempFile('saved content', 'txt');
    const filename = path.basename(textPath);

    await app.uploadFile(textPath);
    await app.openTextEditor(filename);
    await app.setTextEditorContent('recovered unsaved content');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Draft saved');

    await app.page.reload();
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
    await app.expectClipVisible(filename);
    await app.openTextEditor(filename);

    await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('recovered unsaved content');
    await expect(app.page.locator(selectors.textEditor.draftStatus)).toHaveText('Recovered draft');
    await app.cancelTextEditor();
  });
});
