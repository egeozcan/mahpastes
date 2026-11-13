import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Cmd/Ctrl+S saves the open clip in place; Cmd/Ctrl+Shift+S is Save As. Both
// have to work with the caret in the text editor, where every other shortcut
// is suppressed as typing.
test.describe('Editor save shortcuts', () => {
  test('mod+s with the caret in the text editor overwrites the clip', async ({ app }) => {
    const textPath = await createTempFile('before', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.setTextEditorContent('after');
    await expect(app.page.locator(selectors.textEditor.editor)).toBeFocused();
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.finishTextSave();

    await app.expectClipCount(1);
    await app.openTextEditor(filename);
    await app.expectTextEditorContent('after');
    await app.cancelTextEditor();
  });

  test('saving a text clip keeps the editor open, with the caret where it was', async ({ app }) => {
    const textPath = await createTempFile('one', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.setTextEditorContent('one two');
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.waitForToast(/Saved\./);
    const save = app.page.locator(selectors.textEditor.saveButton);
    await expect(save).toBeDisabled();
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await expect(app.page.locator(selectors.textEditor.editor)).toBeFocused();

    // Keep typing and save again: nothing was lost in between.
    await app.page.keyboard.type(' three');
    await expect(save).toBeEnabled();
    await save.click();
    await expect(save).toBeDisabled();
    // Saved with the button: the keyboard goes back to the text, not to <body>.
    await expect(app.page.locator(selectors.textEditor.editor)).toBeFocused();

    // Clean, so closing asks nothing.
    await app.page.locator(selectors.textEditor.cancelButton).click();
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);
    await app.openTextEditor(filename);
    await app.expectTextEditorContent('one two three');
    await app.cancelTextEditor();
  });

  test('mod+s on an unchanged text clip writes nothing and says so', async ({ app }) => {
    const textPath = await createTempFile('unchanged', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.page.locator(selectors.textEditor.editor).click();
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.waitForToast(/No changes to save/);
    await expect(app.page.locator(selectors.textEditor.modal)).toHaveClass(/active/);
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeHidden();
    await app.cancelTextEditor();
    await app.expectClipCount(1);
  });

  test('mod+shift+s opens Save As, and mod+s in the filename field creates the copy', async ({ app }) => {
    const textPath = await createTempFile('original', 'txt');
    const filename = path.basename(textPath);
    const copyFilename = filename.replace('.txt', '-copy.txt');
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.setTextEditorContent('the copy');
    await app.page.keyboard.press('ControlOrMeta+Shift+s');
    const input = app.page.locator(selectors.textEditor.saveAsInput);
    await expect(input).toBeVisible();
    await expect(input).toBeFocused();

    // The Save button is hidden while the field shows, so the Save key
    // confirms the copy instead of overwriting the original behind it.
    await input.fill(copyFilename);
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.page.waitForSelector(`${selectors.textEditor.modal}:not(.active)`);

    await app.expectClipCount(2);
    await app.openTextEditor(filename);
    await app.expectTextEditorContent('original');
    await app.cancelTextEditor();
    await app.openTextEditor(copyFilename);
    await app.expectTextEditorContent('the copy');
    await app.cancelTextEditor();
  });

  test('mod+shift+p toggles the preview with the caret in the text editor', async ({ app }) => {
    const markdownPath = await createTempFile('# Title', 'md');
    const filename = path.basename(markdownPath);
    await app.uploadFile(markdownPath);
    await app.openTextEditor(filename);

    await app.page.locator(selectors.textEditor.editor).click();
    await expect(app.page.locator(selectors.textEditor.editTab)).toHaveAttribute('aria-selected', 'true');
    await app.page.keyboard.press('ControlOrMeta+Shift+P');
    await expect(app.page.locator(selectors.textEditor.previewTab)).toHaveAttribute('aria-selected', 'true');
    await app.cancelTextEditor();
  });

  test('mod+s in the image editor overwrites the clip; mod+shift+s opens Save As', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(200, 150, [255, 255, 255]), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    const hashOf = () => app.page.evaluate(async (name) => {
      const card = document.querySelector(`li[data-filename="${name}"]`) as any;
      return card?._clip?.content_hash || '';
    }, filename);
    const hashBefore = await hashOf();
    expect(hashBefore).not.toBe('');

    await app.openImageEditor(filename);

    // Nothing drawn yet: Save is disabled, and the key respects that.
    await app.page.keyboard.press('ControlOrMeta+s');
    await app.waitForToast(/No changes to save/);
    await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);

    await app.page.keyboard.press('ControlOrMeta+Shift+s');
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeVisible();
    await app.page.locator(selectors.textEditor.saveAsInput).press('Escape');
    await expect(app.page.locator(selectors.textEditor.saveAsInput)).toBeHidden();

    await app.selectTool('brush');
    await app.setEditorColor('#000000');
    await app.setBrushSize(40);
    const box = await app.page.locator(selectors.editor.canvas).boundingBox();
    if (!box) throw new Error('editor canvas not visible');
    await app.drawOnCanvas({ x: box.width * 0.2, y: box.height * 0.5 }, { x: box.width * 0.8, y: box.height * 0.5 });

    await app.page.keyboard.press('ControlOrMeta+s');
    await app.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);
    await app.expectClipCount(1);
    await expect.poll(hashOf).not.toBe(hashBefore);
  });

  test('the Save and Save As tooltips name their keys', async ({ app }) => {
    const save = app.page.locator(selectors.textEditor.saveButton);
    const saveAs = app.page.locator(selectors.textEditor.saveAsButton);
    await expect(save).toHaveAttribute('data-tooltip', /Overwrite the original clip with your edits \((⌘S|Ctrl\+S)\)/);
    await expect(saveAs).toHaveAttribute('data-tooltip', /Save edits as a new clip in your library \((⌘⇧S|Ctrl\+Shift\+S)\)/);
  });
});
