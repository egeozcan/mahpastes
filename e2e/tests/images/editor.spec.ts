import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Image Editor', () => {
  test.describe('Open and Close', () => {
    test('should open image editor', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const isOpen = await app.isEditorOpen();
      expect(isOpen).toBe(true);
    });

    test('should close editor without saving', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.closeImageEditor();

      const isOpen = await app.isEditorOpen();
      expect(isOpen).toBe(false);

      // Should still have only one clip
      await app.expectClipCount(1);
    });

    test('should display canvas with image', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const canvas = app.page.locator(selectors.editor.canvas);
      await expect(canvas).toBeVisible();
    });

    test('should confirm before discarding image edits', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });
      await app.page.locator(selectors.editor.cancelButton).click();

      await expect(app.page.locator(selectors.confirm.title)).toHaveText('Discard unsaved changes?');
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);

      await app.page.locator(selectors.confirm.cancelButton).click();
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);

      await app.page.locator(selectors.editor.cancelButton).click();
      await app.page.locator(selectors.confirm.confirmButton).click();
      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);
    });

    test('should close without confirmation after undoing all image edits', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });
      await app.editorUndo();
      await app.page.locator(selectors.editor.cancelButton).click();

      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);
      await expect(app.page.locator(selectors.confirm.dialog)).not.toHaveClass(/opacity-100/);
    });

    test('should confirm before discarding text edits', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('editor-close'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);
      await app.openTextEditor(filename);
      await app.setTextEditorContent('changed but not saved');
      await app.page.keyboard.press('Escape');

      await expect(app.page.locator(selectors.confirm.title)).toHaveText('Discard unsaved changes?');
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);

      await app.page.locator(selectors.confirm.confirmButton).click();
      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);
    });
  });

  test.describe('Drawing Tools', () => {
    test('should select brush tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');

      // Tool should be selected (active state)
      const brushBtn = app.page.locator(selectors.editor.tools.brush);
      await expect(brushBtn).toHaveAttribute('aria-pressed', 'true');
    });

    test('should select line tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('line');

      const lineBtn = app.page.locator(selectors.editor.tools.line);
      await expect(lineBtn).toHaveAttribute('aria-pressed', 'true');
    });

    test('should select rectangle tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('rectangle');

      const rectBtn = app.page.locator(selectors.editor.tools.rectangle);
      await expect(rectBtn).toHaveAttribute('aria-pressed', 'true');
    });

    test('should select circle tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('circle');

      const circleBtn = app.page.locator(selectors.editor.tools.circle);
      await expect(circleBtn).toHaveAttribute('aria-pressed', 'true');
    });

    test('should select eraser tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('eraser');

      const eraserBtn = app.page.locator(selectors.editor.tools.eraser);
      await expect(eraserBtn).toHaveAttribute('aria-pressed', 'true');
    });

    test('should draw on canvas with brush', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 150, y: 150 });

      expect(await app.isUndoEnabled()).toBe(true);
    });
  });

  test.describe('Color and Size', () => {
    test('should change brush color', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.setEditorColor('#ff0000');

      const colorPicker = app.page.locator(selectors.editor.colorPicker);
      await expect(colorPicker).toHaveValue('#ff0000');
    });

    test('should change brush size', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.setBrushSize(20);

      const sizeInput = app.page.locator(selectors.editor.brushSize);
      await expect(sizeInput).toHaveValue('20');
    });
  });

  test.describe('Undo and Redo', () => {
    test('should undo drawing action', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });

      await app.editorUndo();

      await expect(app.page.locator(selectors.editor.redoButton)).toBeEnabled();
    });

    test('should redo undone action', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });
      await app.editorUndo();
      await app.editorRedo();

      expect(await app.isUndoEnabled()).toBe(true);
    });

    test('should support keyboard shortcuts for undo/redo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });

      // Undo with Ctrl+Z
      await app.page.keyboard.press('Control+z');

      // Redo with Ctrl+Y
      await app.page.keyboard.press('Control+y');
    });
  });

  test.describe('Save Changes', () => {
    test('should save text edits in place without a discard confirmation', async ({ app }) => {
      const textPath = await createTempFile(generateTestText('save-in-place'), 'txt');
      const filename = path.basename(textPath);

      await app.uploadFile(textPath);
      await app.openTextEditor(filename);
      await app.setTextEditorContent('saved text content');
      await app.saveTextEditor();

      await expect(app.page.locator(selectors.confirm.dialog)).not.toHaveClass(/opacity-100/);

      await app.openTextEditor(filename);
      await expect(app.page.locator(selectors.textEditor.textarea)).toHaveValue('saved text content');
      await app.cancelTextEditor();
    });

    test('should save edited image as new clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.openImageEditor(filename);
      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 100, y: 100 });
      await app.saveEditorAsNewClip();

      // Should now have 2 clips (original + edited)
      await app.expectClipCount(2);
    });
  });

  test.describe('Keyboard Shortcuts', () => {
    test('should switch to brush tool with B key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('b');

      expect(await app.isToolActive('brush')).toBe(true);
    });

    test('should switch to eraser tool with E key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('e');

      expect(await app.isToolActive('eraser')).toBe(true);
    });
  });
});
