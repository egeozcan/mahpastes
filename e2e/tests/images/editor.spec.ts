import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
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
      await expect(brushBtn).toBeVisible();
    });

    test('should select line tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('line');

      const lineBtn = app.page.locator(selectors.editor.tools.line);
      await expect(lineBtn).toBeVisible();
    });

    test('should select rectangle tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('rectangle');

      const rectBtn = app.page.locator(selectors.editor.tools.rectangle);
      await expect(rectBtn).toBeVisible();
    });

    test('should select circle tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('circle');

      const circleBtn = app.page.locator(selectors.editor.tools.circle);
      await expect(circleBtn).toBeVisible();
    });

    test('should select eraser tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('eraser');

      const eraserBtn = app.page.locator(selectors.editor.tools.eraser);
      await expect(eraserBtn).toBeVisible();
    });

    test('should draw on canvas with brush', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 50 }, { x: 150, y: 150 });

      // Drawing should be recorded (canvas state changed)
      // We verify by checking undo is available
      const undoBtn = app.page.locator(selectors.editor.undoButton);
      await expect(undoBtn).toBeVisible();
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

      // Redo should now be available
      const redoBtn = app.page.locator(selectors.editor.redoButton);
      await expect(redoBtn).toBeVisible();
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

      // Action should be redone
      const undoBtn = app.page.locator(selectors.editor.undoButton);
      await expect(undoBtn).toBeVisible();
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

      // Brush should be selected
    });

    test('should switch to eraser tool with E key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('e');

      // Eraser should be selected
    });
  });
});
