import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Advanced Editor Tools', () => {
  // ==================== Zoom Controls ====================

  test.describe('Zoom Controls', () => {
    test('should zoom in and update display', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Get initial zoom level
      const initialZoom = await app.getZoomLevel();

      // Zoom in
      await app.setZoom('in');

      // Zoom display should have increased
      const newZoom = await app.getZoomLevel();
      const initialPct = parseInt(initialZoom.replace('%', ''), 10);
      const newPct = parseInt(newZoom.replace('%', ''), 10);
      expect(newPct).toBeGreaterThan(initialPct);
    });

    test('should zoom out and update display', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Zoom to 100% first to have a known baseline
      await app.setZoom('100');
      const baseZoom = await app.getZoomLevel();
      expect(baseZoom).toBe('100%');

      // Zoom out
      await app.setZoom('out');

      const newZoom = await app.getZoomLevel();
      const newPct = parseInt(newZoom.replace('%', ''), 10);
      expect(newPct).toBeLessThan(100);
    });

    test('should zoom to fit', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Zoom to 100% then fit
      await app.setZoom('100');
      await app.setZoom('fit');

      // Fit zoom should have updated the display
      const zoom = await app.getZoomLevel();
      expect(zoom).toMatch(/^\d+%$/);
    });

    test('should zoom to 100%', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Zoom in a couple of times, then reset to 100%
      await app.setZoom('in');
      await app.setZoom('in');
      await app.setZoom('100');

      const zoom = await app.getZoomLevel();
      expect(zoom).toBe('100%');
    });

    test('should zoom with ctrl+scroll wheel', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Set to 100% as baseline
      await app.setZoom('100');

      // Dispatch a WheelEvent from inside the browser with ctrlKey=true
      // and check the internal zoom level (wheel handler updates zoom but
      // doesn't call updateZoomDisplay — only ZoomTool buttons do that)
      const zoomChanged = await app.page.evaluate((canvasSelector) => {
        const canvas = document.querySelector(canvasSelector) as HTMLElement;
        const rect = canvas.getBoundingClientRect();
        // @ts-ignore - EditorCore is a global
        const beforeZoom = EditorCore.zoomLevel;
        const event = new WheelEvent('wheel', {
          deltaY: -100,
          ctrlKey: true,
          clientX: rect.left + 100,
          clientY: rect.top + 100,
          bubbles: true,
          cancelable: true,
        });
        canvas.dispatchEvent(event);
        // @ts-ignore
        return EditorCore.zoomLevel !== beforeZoom;
      }, selectors.editor.canvas);

      expect(zoomChanged).toBe(true);
    });

    test('should display zoom buttons', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await expect(app.page.locator(selectors.editor.zoomIn)).toBeVisible();
      await expect(app.page.locator(selectors.editor.zoomOut)).toBeVisible();
      await expect(app.page.locator(selectors.editor.zoomFit)).toBeVisible();
      await expect(app.page.locator(selectors.editor.zoom100)).toBeVisible();
      await expect(app.page.locator(selectors.editor.zoomDisplay)).toBeVisible();
    });
  });

  // ==================== Arrow Tool ====================

  test.describe('Arrow Tool', () => {
    test('should select arrow tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('arrow');

      const isActive = await app.isToolActive('arrow');
      expect(isActive).toBe(true);
    });

    test('should draw arrow and enable undo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('arrow');
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 150, y: 150 });

      // Drawing should add an undo state
      const undoEnabled = await app.isUndoEnabled();
      expect(undoEnabled).toBe(true);
    });
  });

  // ==================== Eyedropper Tool ====================

  test.describe('Eyedropper Tool', () => {
    test('should select eyedropper tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('eyedropper');

      const isActive = await app.isToolActive('eyedropper');
      expect(isActive).toBe(true);
    });

    test('should pick color from canvas', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, [0, 128, 255]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Set a known color first so we can detect the change
      await app.setEditorColor('#000000');

      // Switch to eyedropper and click on canvas
      await app.selectTool('eyedropper');
      const canvas = app.page.locator(selectors.editor.canvas);
      await canvas.click({ position: { x: 100, y: 100 } });

      // Color picker should have been updated (not black anymore since image is blue)
      const colorValue = await app.page.locator(selectors.editor.colorPicker).inputValue();
      expect(colorValue).not.toBe('#000000');
    });
  });

  // ==================== Rotate/Flip ====================

  test.describe('Rotate and Flip', () => {
    test('should rotate CW and swap canvas dimensions', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 100), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const before = await app.getCanvasDimensions();
      expect(before.width).toBe(200);
      expect(before.height).toBe(100);

      // Rotate 90 degrees CW
      await app.page.locator(selectors.editor.rotateCW).click();

      const after = await app.getCanvasDimensions();
      // Width and height should be swapped
      expect(after.width).toBe(100);
      expect(after.height).toBe(200);
    });

    test('should rotate CCW and swap canvas dimensions', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 100), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Rotate 90 degrees CCW
      await app.page.locator(selectors.editor.rotateCCW).click();

      const after = await app.getCanvasDimensions();
      expect(after.width).toBe(100);
      expect(after.height).toBe(200);
    });

    test('should undo rotate and restore original dimensions', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 100), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const before = await app.getCanvasDimensions();
      expect(before.width).toBe(200);
      expect(before.height).toBe(100);

      // Rotate CW (swaps to 100x200)
      await app.page.locator(selectors.editor.rotateCW).click();
      const rotated = await app.getCanvasDimensions();
      expect(rotated.width).toBe(100);
      expect(rotated.height).toBe(200);

      // Undo should restore original 200x100
      await app.editorUndo();
      const restored = await app.getCanvasDimensions();
      expect(restored.width).toBe(200);
      expect(restored.height).toBe(100);
    });

    test('should flip horizontal and enable undo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.locator(selectors.editor.flipH).click();

      // Flip should add an undo state
      const undoEnabled = await app.isUndoEnabled();
      expect(undoEnabled).toBe(true);
    });

    test('should flip vertical and enable undo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.locator(selectors.editor.flipV).click();

      const undoEnabled = await app.isUndoEnabled();
      expect(undoEnabled).toBe(true);
    });

    test('should show rotate/flip buttons', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await expect(app.page.locator(selectors.editor.rotateCW)).toBeVisible();
      await expect(app.page.locator(selectors.editor.rotateCCW)).toBeVisible();
      await expect(app.page.locator(selectors.editor.flipH)).toBeVisible();
      await expect(app.page.locator(selectors.editor.flipV)).toBeVisible();
    });
  });

  // ==================== Crop Tool ====================

  test.describe('Crop Tool', () => {
    test('should select crop tool and show crop options', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('crop');

      const isActive = await app.isToolActive('crop');
      expect(isActive).toBe(true);

      // Crop options toolbar should be visible
      const cropOptions = app.page.locator(selectors.editor.cropOptions);
      await expect(cropOptions).toBeVisible();
    });

    test('should show crop confirm and cancel buttons', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('crop');

      await expect(app.page.locator(selectors.editor.cropConfirm)).toBeVisible();
      await expect(app.page.locator(selectors.editor.cropCancel)).toBeVisible();
    });

    test('should crop image and reduce canvas size', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const before = await app.getCanvasDimensions();

      // Draw a crop region and confirm
      await app.cropImage({ x: 20, y: 20 }, { x: 100, y: 100 });

      // Canvas should be smaller after crop
      const after = await app.getCanvasDimensions();
      expect(after.width).toBeLessThan(before.width);
      expect(after.height).toBeLessThan(before.height);
    });

    test('should cancel crop without changing canvas', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const before = await app.getCanvasDimensions();

      // Select crop tool and draw a region
      await app.selectTool('crop');
      await app.drawOnCanvas({ x: 20, y: 20 }, { x: 100, y: 100 });

      // Cancel the crop
      await app.page.locator(selectors.editor.cropCancel).click();

      // Canvas should remain the same size
      const after = await app.getCanvasDimensions();
      expect(after.width).toBe(before.width);
      expect(after.height).toBe(before.height);
    });

    test('should have aspect ratio selector', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('crop');

      await expect(app.page.locator(selectors.editor.cropRatio)).toBeVisible();
    });
  });

  // ==================== Selection Tool ====================

  test.describe('Selection Tool', () => {
    test('should select the selection tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('select');

      const isActive = await app.isToolActive('select');
      expect(isActive).toBe(true);
    });

    test('should create selection region', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('select');
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });

      // After creating a selection, the select tool should remain active
      const isActive = await app.isToolActive('select');
      expect(isActive).toBe(true);
    });

    test('should move selection and enable undo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('select');
      // Create a selection region
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });

      // Drag inside the selection to move it
      await app.drawOnCanvas({ x: 75, y: 75 }, { x: 150, y: 150 });

      // Moving should keep the select tool active and enable undo
      const isActive = await app.isToolActive('select');
      expect(isActive).toBe(true);
      const undoEnabled = await app.isUndoEnabled();
      expect(undoEnabled).toBe(true);
    });

    test('should support copy and paste with keyboard', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('select');
      // Create a selection region
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });

      // Copy the selection (use ControlOrMeta for cross-platform compatibility)
      await app.page.keyboard.press('ControlOrMeta+c');

      // Paste the selection
      await app.page.keyboard.press('ControlOrMeta+v');

      // Selection tool should still be active after paste
      const isActive = await app.isToolActive('select');
      expect(isActive).toBe(true);
    });
  });

  // ==================== Anonymize Tool ====================

  test.describe('Anonymize Tool', () => {
    test('should select anonymize tool and show options', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('anonymize');

      const isActive = await app.isToolActive('anonymize');
      expect(isActive).toBe(true);

      // Anonymize options toolbar should be visible
      const anonOptions = app.page.locator(selectors.editor.anonymizeOptions);
      await expect(anonOptions).toBeVisible();
    });

    test('should anonymize with rectangle mode and enable undo', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.anonymizeRegion({ x: 30, y: 30 }, { x: 120, y: 120 }, 'rect');

      // Should add an undo state
      const undoEnabled = await app.isUndoEnabled();
      expect(undoEnabled).toBe(true);
    });

    test('should switch between brush and rect modes', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('anonymize');

      // Click rect mode
      await app.page.locator(selectors.editor.anonRect).click();
      // Rect button should have active styling (bg-stone-800)
      const rectHasActive = await app.page.locator(selectors.editor.anonRect).evaluate(
        (el) => el.classList.contains('bg-stone-800')
      );
      expect(rectHasActive).toBe(true);

      // Click brush mode
      await app.page.locator(selectors.editor.anonBrush).click();
      const brushHasActive = await app.page.locator(selectors.editor.anonBrush).evaluate(
        (el) => el.classList.contains('bg-stone-800')
      );
      expect(brushHasActive).toBe(true);
    });

    test('should switch between pixelate and blur effects', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('anonymize');

      // Click blur effect
      await app.page.locator(selectors.editor.anonBlur).click();
      const blurHasActive = await app.page.locator(selectors.editor.anonBlur).evaluate(
        (el) => el.classList.contains('bg-stone-800')
      );
      expect(blurHasActive).toBe(true);

      // Click pixelate effect
      await app.page.locator(selectors.editor.anonPixelate).click();
      const pixelateHasActive = await app.page.locator(selectors.editor.anonPixelate).evaluate(
        (el) => el.classList.contains('bg-stone-800')
      );
      expect(pixelateHasActive).toBe(true);
    });

    test('should show mode and effect buttons', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('anonymize');

      await expect(app.page.locator(selectors.editor.anonBrush)).toBeVisible();
      await expect(app.page.locator(selectors.editor.anonRect)).toBeVisible();
      await expect(app.page.locator(selectors.editor.anonPixelate)).toBeVisible();
      await expect(app.page.locator(selectors.editor.anonBlur)).toBeVisible();
    });
  });

  // ==================== Text Font Size ====================

  test.describe('Text Tool Font Size', () => {
    test('should select text tool and show font size input', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('text');

      const isActive = await app.isToolActive('text');
      expect(isActive).toBe(true);

      // Text options toolbar should be visible
      const textOptions = app.page.locator(selectors.editor.textOptions);
      await expect(textOptions).toBeVisible();
    });

    test('should change font size', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('text');
      await app.setFontSize(48);

      const fontSizeInput = app.page.locator(selectors.editor.fontSize);
      await expect(fontSizeInput).toHaveValue('48');
    });

    test('should have default font size of 16', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('text');

      const fontSizeInput = app.page.locator(selectors.editor.fontSize);
      await expect(fontSizeInput).toHaveValue('16');
    });
  });

  // ==================== Keyboard Shortcuts ====================

  test.describe('Keyboard Shortcuts', () => {
    test('should switch to select tool with V key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // Start with brush tool
      await app.selectTool('brush');
      expect(await app.isToolActive('brush')).toBe(true);

      // Press V to switch to select
      await app.page.keyboard.press('v');

      const isActive = await app.isToolActive('select');
      expect(isActive).toBe(true);
    });

    test('should switch to crop tool with C key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('c');

      const isActive = await app.isToolActive('crop');
      expect(isActive).toBe(true);
    });

    test('should switch to arrow tool with W key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('w');

      const isActive = await app.isToolActive('arrow');
      expect(isActive).toBe(true);
    });

    test('should switch to anonymize tool with X key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('x');

      const isActive = await app.isToolActive('anonymize');
      expect(isActive).toBe(true);
    });

    test('should switch to eyedropper with I key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.page.keyboard.press('i');

      const isActive = await app.isToolActive('eyedropper');
      expect(isActive).toBe(true);
    });

    test('should rotate CW with R key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 100), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const before = await app.getCanvasDimensions();

      await app.page.keyboard.press('r');

      // Dimensions should be swapped after rotation
      const after = await app.getCanvasDimensions();
      expect(after.width).toBe(before.height);
      expect(after.height).toBe(before.width);
    });
  });

  // ==================== Bracket Shortcuts (Brush Size) ====================

  test.describe('Brush Size Shortcuts', () => {
    test('should increase brush size with ] key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');

      // Get initial brush size
      const initialSize = await app.page.locator(selectors.editor.brushSize).inputValue();

      // Press ] to increase
      await app.page.keyboard.press(']');

      const newSize = await app.page.locator(selectors.editor.brushSize).inputValue();
      expect(parseInt(newSize, 10)).toBeGreaterThan(parseInt(initialSize, 10));
    });

    test('should decrease brush size with [ key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('brush');

      // First increase to make sure we have room to decrease
      await app.page.keyboard.press(']');
      await app.page.keyboard.press(']');
      const increasedSize = await app.page.locator(selectors.editor.brushSize).inputValue();

      // Press [ to decrease
      await app.page.keyboard.press('[');

      const newSize = await app.page.locator(selectors.editor.brushSize).inputValue();
      expect(parseInt(newSize, 10)).toBeLessThan(parseInt(increasedSize, 10));
    });
  });
});
