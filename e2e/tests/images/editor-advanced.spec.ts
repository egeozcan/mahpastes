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

    test('should show resize cursors when hovering crop handles', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('crop');

      const canvas = app.page.locator(selectors.editor.canvas);
      const box = await canvas.boundingBox();
      if (!box) throw new Error('Canvas not visible');

      const handles = [
        { x: 20, y: 20, cursor: 'nwse-resize' },
        { x: 100, y: 20, cursor: 'ns-resize' },
        { x: 180, y: 20, cursor: 'nesw-resize' },
        { x: 180, y: 100, cursor: 'ew-resize' },
        { x: 180, y: 180, cursor: 'nwse-resize' },
        { x: 100, y: 180, cursor: 'ns-resize' },
        { x: 20, y: 180, cursor: 'nesw-resize' },
        { x: 20, y: 100, cursor: 'ew-resize' },
      ];

      for (const handle of handles) {
        await app.page.mouse.move(box.x + handle.x, box.y + handle.y);
        await expect(canvas).toHaveCSS('cursor', handle.cursor);
      }
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

    test('should crop beyond the image and keep the added area transparent', async ({ app }) => {
      const sourceColor: [number, number, number] = [200, 100, 50];
      const imagePath = await createTempFile(generateTestImage(200, 200, sourceColor), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      const backgroundColor = [17, 34, 51];
      await app.page.locator(selectors.editor.canvas).evaluate((canvas, color) => {
        (canvas as HTMLCanvasElement).style.backgroundColor = `rgb(${color.join(', ')})`;
      }, backgroundColor);

      await app.cropImage({ x: 5, y: 5 }, { x: 250, y: 250 });

      const dimensions = await app.getCanvasDimensions();
      expect(dimensions.width).toBeGreaterThan(200);
      expect(dimensions.height).toBeGreaterThan(200);

      const pixels = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const imageCanvas = canvas as HTMLCanvasElement;
        const context = imageCanvas.getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return {
          source: Array.from(context.getImageData(0, 0, 1, 1).data),
          background: Array.from(context.getImageData(imageCanvas.width - 1, imageCanvas.height - 1, 1, 1).data),
        };
      });
      expect(pixels.source).toEqual([...sourceColor, 255]);
      expect(pixels.background).toEqual([0, 0, 0, 0]);
    });

    test('should keep the crop border and handle visible beyond the image bounds', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('crop');
      await app.drawOnCanvas({ x: 5, y: 5 }, { x: 250, y: 250 });

      const overlay = app.page.locator(selectors.editor.overlayCanvas);
      await expect.poll(async () => overlay.evaluate((canvas) => {
        const overlayCanvas = canvas as HTMLCanvasElement;
        if (overlayCanvas.width < 254 || overlayCanvas.height < 254) return false;
        const context = overlayCanvas.getContext('2d');
        if (!context) return false;
        const handlePixel = context.getImageData(248, 248, 1, 1).data;
        return handlePixel[3] > 0;
      })).toBe(true);

      // The visible out-of-bounds handle should remain interactive.
      await app.drawOnCanvas({ x: 250, y: 250 }, { x: 275, y: 275 });
      await expect.poll(async () => overlay.evaluate((canvas) => {
        const overlayCanvas = canvas as HTMLCanvasElement;
        if (overlayCanvas.width < 279 || overlayCanvas.height < 279) return false;
        const context = overlayCanvas.getContext('2d');
        if (!context) return false;
        return context.getImageData(273, 273, 1, 1).data[3] > 0;
      })).toBe(true);

      // Move the same selection beyond the top-left edge as well. The overlay
      // should translate its origin and keep that corner handle rendered.
      await app.drawOnCanvas({ x: 100, y: 100 }, { x: -25, y: -25 });
      await expect.poll(async () => overlay.evaluate((canvas) => {
        const overlayCanvas = canvas as HTMLCanvasElement;
        const context = overlayCanvas.getContext('2d');
        if (!context) return false;
        const handlePixel = context.getImageData(3, 3, 1, 1).data;
        return parseFloat(overlayCanvas.style.left) < 0 &&
          parseFloat(overlayCanvas.style.top) < 0 &&
          handlePixel[3] > 0;
      })).toBe(true);
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

      // Commit the moved selection (undo is saved on commit, not on move)
      await app.page.keyboard.press('Enter');

      // Moving and committing should keep the select tool active and enable undo
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

  // ==================== Correctness regressions ====================

  test.describe('Correctness regressions', () => {
    test('should disable clean in-place save and avoid dirty state for a click-only brush gesture', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(4001, 100), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await expect(app.page.locator('#editor-save-in-place')).toBeDisabled();
      await expect(app.page.locator('#editor-downscale-warning')).toContainText('4001×100 resized to 4000×99');
      await app.page.locator(selectors.editor.canvas).click({ position: { x: 80, y: 10 } });
      await expect(app.page.locator(selectors.editor.undoButton)).toBeDisabled();

      await app.page.locator(selectors.editor.cancelButton).click();
      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);
    });

    test('should commit a floating selection before save as', async ({ app }) => {
      const sourceColor: [number, number, number] = [12, 80, 190];
      const imagePath = await createTempFile(generateTestImage(200, 200, sourceColor), 'png');
      const filename = path.basename(imagePath);
      const parsed = path.parse(filename);
      const editedFilename = `${parsed.name}_edited${parsed.ext}`;
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('select');
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });
      await app.saveEditorAsNewClip();

      await app.openImageEditor(editedFilename);
      const pixel = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return Array.from(context.getImageData(60, 60, 1, 1).data);
      });
      expect(pixel).toEqual([...sourceColor, 255]);
    });

    test('should cancel crop with Escape before closing the editor', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.selectTool('crop');

      await app.page.keyboard.press('Escape');

      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
      expect(await app.isToolActive('brush')).toBe(true);
    });

    test('should return from eyedropper to the previously selected tool', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, [20, 40, 60]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.selectTool('line');
      await app.selectTool('eyedropper');

      await app.page.locator(selectors.editor.canvas).click({ position: { x: 80, y: 80 } });

      expect(await app.isToolActive('line')).toBe(true);
    });

    test('should delete a selection to transparency and undo it', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, [60, 120, 180]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.selectTool('select');
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });
      await app.page.keyboard.press('Delete');

      const alpha = async () => app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return context.getImageData(60, 60, 1, 1).data[3];
      });
      expect(await alpha()).toBe(0);
      await app.editorUndo();
      expect(await alpha()).toBe(255);
    });

    test('should commit a floating selection before rotating', async ({ app }) => {
      const sourceColor: [number, number, number] = [30, 90, 170];
      const imagePath = await createTempFile(generateTestImage(200, 200, sourceColor), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.selectTool('select');
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 120, y: 120 });

      await app.page.locator(selectors.editor.rotateCW).click();

      const pixel = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return Array.from(context.getImageData(140, 60, 1, 1).data);
      });
      expect(pixel).toEqual([...sourceColor, 255]);
    });

    test('should keep the eraser baseline aligned after rotation', async ({ app }) => {
      const sourceColor: [number, number, number] = [18, 70, 130];
      const imagePath = await createTempFile(generateTestImage(200, 100, sourceColor), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.page.locator(selectors.editor.rotateCW).click();
      await app.setEditorColor('#ff0000');
      await app.setBrushSize(20);
      await app.selectTool('brush');
      await app.drawOnCanvas({ x: 50, y: 20 }, { x: 50, y: 180 });
      await app.selectTool('eraser');
      // The helper emits one long move, so the eraser must interpolate stamps
      // rather than restoring only the delivered endpoint.
      await app.drawOnCanvas({ x: 50, y: 20 }, { x: 50, y: 180 });

      const pixel = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return Array.from(context.getImageData(50, 100, 1, 1).data);
      });
      expect(pixel).toEqual([...sourceColor, 255]);
    });

    test('should save rasterized GIF content with a truthful PNG name and MIME', async ({ app }) => {
      const gif = Buffer.from('R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==', 'base64');
      const imagePath = await createTempFile(gif, 'gif');
      const filename = path.basename(imagePath);
      const expectedFilename = `${path.parse(filename).name}_edited.png`;
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.saveEditorAsNewClip();

      const saved = await app.page.evaluate(async (expected) => {
        const clips = await (window as any).go.main.App.GetClips(false, [], [], '', '');
        return clips.find((clip: any) => clip.filename === expected);
      }, expectedFilename);
      expect(saved).toBeTruthy();
      expect(saved.content_type).toBe('image/png');
    });

    test('should normalize an unsupported format when saving in place', async ({ app }) => {
      const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect width="40" height="40" fill="white"/></svg>');
      const imagePath = await createTempFile(svg, 'svg');
      const filename = path.basename(imagePath);
      const expectedFilename = `${path.parse(filename).name}.png`;
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.setEditorColor('#ff0000');
      await app.drawOnCanvas({ x: 5, y: 5 }, { x: 30, y: 30 });
      await app.page.locator('#editor-save-in-place').click();
      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);

      const clips = await app.page.evaluate(async () => (window as any).go.main.App.GetClips(false, [], [], '', ''));
      expect(clips).toHaveLength(1);
      expect(clips[0].filename).toBe(expectedFilename);
      expect(clips[0].content_type).toBe('image/png');
    });

    test('should preserve PNG transparency through Save As', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(100, 100, [40, 80, 120]), 'png');
      const filename = path.basename(imagePath);
      const editedFilename = `${path.parse(filename).name}_edited.png`;
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.selectTool('select');
      await app.drawOnCanvas({ x: 20, y: 20 }, { x: 80, y: 80 });
      await app.page.keyboard.press('Delete');
      await app.saveEditorAsNewClip();
      await app.openImageEditor(editedFilename);

      const alpha = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return context.getImageData(50, 50, 1, 1).data[3];
      });
      expect(alpha).toBe(0);
    });

    test('should keep undo snapshots under the strict history budget', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(2500, 2500, [240, 240, 240]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.setEditorColor('#ff0000');
      await app.setBrushSize(20);
      for (let index = 0; index < 5; index += 1) {
        await app.drawOnCanvas(
          { x: 100 + index * 40, y: 100 },
          { x: 120 + index * 40, y: 120 },
        );
      }
      // Geometry changes retain older eraser baselines; those buffers must be
      // included in the same history accounting.
      await app.page.locator(selectors.editor.rotateCW).click();
      await app.page.locator(selectors.editor.rotateCW).click();

      const history = await app.page.evaluate(() => {
        // @ts-ignore global editor module
        return EditorCore.getHistoryStats();
      });
      expect(history.bytes).toBeLessThanOrEqual(history.maxBytes);
      expect(history.undoDepth).toBeLessThan(5);
    });

    test('should keep the canvas coordinate under the wheel cursor stationary', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(300, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.setZoom('100');

      const result = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const rect = canvas.getBoundingClientRect();
        const clientX = rect.left + rect.width * 0.8;
        const clientY = rect.top + rect.height * 0.25;
        // @ts-ignore global editor module
        const before = EditorCore.screenToCanvas(clientX, clientY);
        canvas.dispatchEvent(new WheelEvent('wheel', {
          deltaY: -100, ctrlKey: true, clientX, clientY, bubbles: true, cancelable: true,
        }));
        // @ts-ignore global editor module
        const after = EditorCore.screenToCanvas(clientX, clientY);
        return { before, after };
      });
      expect(Math.abs(result.after.x - result.before.x)).toBeLessThan(0.1);
      expect(Math.abs(result.after.y - result.before.y)).toBeLessThan(0.1);
    });

    test('should restore focus to the clip actions button after closing', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.closeImageEditor();

      await expect.poll(() => app.page.evaluate(() => {
        const active = document.activeElement as HTMLElement | null;
        return active?.dataset.action === 'menu';
      })).toBe(true);
    });

    test('should expose named controls and wrap focus at both dialog boundaries', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await expect(app.page.locator(selectors.editor.modal)).toHaveAccessibleName(new RegExp(`Edit.*${filename}`));
      await expect(app.page.locator(selectors.editor.colorPicker)).toHaveAccessibleName('Color');
      await expect(app.page.locator(selectors.editor.opacity)).toHaveAccessibleName('Opacity');
      await expect(app.page.locator(selectors.editor.brushSize)).toHaveAccessibleName('Size');
      await expect(app.page.getByLabel('Image editing canvas')).toBeVisible();

      const focusableSelector = 'button:not([disabled]):not([tabindex="-1"]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
      const boundaries = await app.page.locator(selectors.editor.modal).evaluate((modal, selector) => {
        const items = Array.from(modal.querySelectorAll(selector)).filter((element) => {
          const htmlElement = element as HTMLElement;
          return htmlElement.offsetParent !== null || htmlElement.offsetWidth > 0;
        }) as HTMLElement[];
        items[0].dataset.focusBoundary = 'first';
        items[items.length - 1].dataset.focusBoundary = 'last';
        items[items.length - 1].focus();
        return { first: items[0].dataset.focusBoundary, last: items[items.length - 1].dataset.focusBoundary };
      }, focusableSelector);
      expect(boundaries).toEqual({ first: 'first', last: 'last' });

      await app.page.keyboard.press('Tab');
      await expect(app.page.locator('[data-focus-boundary="first"]')).toBeFocused();
      await app.page.locator('[data-focus-boundary="first"]').focus();
      await app.page.keyboard.press('Shift+Tab');
      await expect(app.page.locator('[data-focus-boundary="last"]')).toBeFocused();
    });

    test('should preserve both copies when Alt-dragging a selection', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, [255, 255, 255]), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);
      await app.setEditorColor('#ff0000');
      await app.setBrushSize(20);
      await app.drawOnCanvas({ x: 30, y: 30 }, { x: 50, y: 50 });
      await app.selectTool('select');
      await app.drawOnCanvas({ x: 20, y: 20 }, { x: 60, y: 60 });

      const box = await app.page.locator(selectors.editor.canvas).boundingBox();
      if (!box) throw new Error('Canvas not visible');
      await app.page.keyboard.down('Alt');
      await app.page.mouse.move(box.x + 40, box.y + 40);
      await app.page.mouse.down();
      await app.page.mouse.move(box.x + 100, box.y + 100, { steps: 5 });
      await app.page.mouse.up();
      await app.page.keyboard.up('Alt');
      await app.page.keyboard.press('Enter');

      const pixels = await app.page.locator(selectors.editor.canvas).evaluate((canvas) => {
        const context = (canvas as HTMLCanvasElement).getContext('2d');
        if (!context) throw new Error('Canvas context unavailable');
        return {
          original: Array.from(context.getImageData(40, 40, 1, 1).data),
          duplicate: Array.from(context.getImageData(100, 100, 1, 1).data),
        };
      });
      expect(pixels.original.slice(0, 3)).toEqual([255, 0, 0]);
      expect(pixels.duplicate.slice(0, 3)).toEqual([255, 0, 0]);
    });

    test('should cancel pending text and selection before closing', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.selectTool('text');
      await app.page.locator(selectors.editor.canvas).click({ position: { x: 50, y: 50 } });
      const annotationInput = app.page.locator('#canvas-text-input');
      await annotationInput.fill('discard me');
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
      await expect(annotationInput).toBeHidden();
      expect(await app.isUndoEnabled()).toBe(false);

      await app.selectTool('select');
      await app.drawOnCanvas({ x: 20, y: 20 }, { x: 80, y: 80 });
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.editor.modal)).not.toHaveClass(/active/);
    });

    test('should honor reduced motion and support all zoom shortcuts', async ({ app }) => {
      await app.page.emulateMedia({ reducedMotion: 'reduce' });
      try {
        const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
        const filename = path.basename(imagePath);
        await app.uploadFile(imagePath);
        await app.openImageEditor(filename);

        await expect(app.page.locator(selectors.editor.modal)).toHaveCSS('transition-duration', '0s');
        await app.setZoom('100');
        await app.page.keyboard.press('ControlOrMeta+=');
        // Zoom steps are proportional, so one step up from 100% is 125%.
        await expect(app.page.locator(selectors.editor.zoomDisplay)).toHaveText('125%');
        await app.page.keyboard.press('ControlOrMeta+-');
        await expect(app.page.locator(selectors.editor.zoomDisplay)).toHaveText('100%');
        await app.page.keyboard.press('ControlOrMeta+0');
        await expect(app.page.locator(selectors.editor.zoomDisplay)).toHaveText(/^\d+%$/);
        await app.page.keyboard.press('ControlOrMeta+1');
        await expect(app.page.locator(selectors.editor.zoomDisplay)).toHaveText('100%');

        await app.selectTool('crop');
        const overlay = app.page.locator(selectors.editor.overlayCanvas);
        const before = await overlay.evaluate((canvas) => (canvas as HTMLCanvasElement).toDataURL());
        await app.page.waitForTimeout(150);
        const after = await overlay.evaluate((canvas) => (canvas as HTMLCanvasElement).toDataURL());
        expect(after).toBe(before);
      } finally {
        await app.page.emulateMedia({ reducedMotion: 'no-preference' });
      }
    });

    test('should not activate image shortcuts from the text editor', async ({ app }) => {
      const textPath = await createTempFile(Buffer.from('plain text'), 'txt');
      const filename = path.basename(textPath);
      await app.uploadFile(textPath);
      await app.openTextEditor(filename);
      await app.page.locator('#editor-close').focus();

      await app.page.keyboard.press('b');
      await app.page.keyboard.press('Backspace');
      await app.page.keyboard.press(']');
      const editorState = await app.page.evaluate(() => ({
        // @ts-ignore global editor module
        activeTool: EditorCore.activeToolName,
        // @ts-ignore global editor module
        brushSize: EditorCore.brushSize,
      }));
      expect(editorState).toEqual({ activeTool: null, brushSize: 8 });
      await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
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

  // ==================== Proportional Zoom Stepping ====================

  test.describe('Zoom Stepping', () => {
    test('should step zoom multiplicatively rather than by a fixed amount', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.setZoom('100');
      expect(await app.getZoomLevel()).toBe('100%');

      await app.setZoom('in');
      expect(await app.getZoomLevel()).toBe('125%');

      await app.setZoom('out');
      expect(await app.getZoomLevel()).toBe('100%');

      await app.setZoom('out');
      expect(await app.getZoomLevel()).toBe('80%');
    });

    test('should shrink zoom by a constant ratio on every step', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.setZoom('100');

      const levels: number[] = [];
      for (let i = 0; i < 4; i++) {
        await app.setZoom('out');
        levels.push(await app.getInternalZoomLevel());
      }

      // Proportional stepping divides by the same factor every time. Subtracting
      // a fixed 0.1 gives a ratio that drifts (0.900, 0.889, 0.875, 0.857) and
      // eventually dies on the 0.1 clamp floor, so pinning the ratio is what
      // separates the two -- a "still decreasing" check does not, because the
      // additive walk lands on 0.10000000000000014 rather than exactly 0.1.
      let previous = 1;
      for (const level of levels) {
        expect(level / previous).toBeCloseTo(0.8, 2);
        previous = level;
      }
    });
  });

  // ==================== Anonymize Region Clipping ====================

  test.describe('Anonymize Region Clipping', () => {
    test('should trim an edge dab to the canvas instead of sliding it inward', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, 'white'), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      await app.setZoom('100');
      await app.selectTool('anonymize');
      await app.page.locator(selectors.editor.anonBrush).click();
      await app.page.locator(selectors.editor.anonPixelate).click();
      await app.setBrushSize(8);

      // The dab spans cx +/- 16, so at cx = 2 it starts 14px off the left edge.
      // Clamping only the origin would slide the whole 32px-wide region inward
      // and reach x = 31; trimming stops it at x = 17.
      await app.page.locator(selectors.editor.canvas).click({ position: { x: 2, y: 100 } });

      const extent = await app.getChangedPixelExtent('#ffffff');
      expect(extent).not.toBeNull();
      expect(extent!.maxX).toBeLessThanOrEqual(18);
    });
  });

  // ==================== Shape Constraints ====================

  test.describe('Shape Constraints', () => {
    test('should draw a free rectangle when shift is not held', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, 'white'), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // 1:1 with no pan, so the drag distances below are canvas pixels.
      await app.setZoom('100');
      await app.selectTool('rectangle');
      await app.setEditorColor('#0000ff');
      await app.setBrushSize(2);
      await app.drawOnCanvas({ x: 40, y: 40 }, { x: 160, y: 90 });

      const bounds = await app.getPaintedBounds('#0000ff');
      expect(bounds).not.toBeNull();
      // The drag is 120x50, so the painted box must stay clearly oblong.
      expect(bounds!.width).toBeGreaterThan(bounds!.height * 1.5);
    });

    test('should constrain the rectangle to a square while shift is held', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200, 'white'), 'png');
      const filename = path.basename(imagePath);
      await app.uploadFile(imagePath);
      await app.openImageEditor(filename);

      // 1:1 with no pan, so the drag distances below are canvas pixels.
      await app.setZoom('100');
      await app.selectTool('rectangle');
      await app.setEditorColor('#0000ff');
      await app.setBrushSize(2);

      const box = await app.page.locator(selectors.editor.canvas).boundingBox();
      if (!box) throw new Error('Canvas not visible');
      await app.page.keyboard.down('Shift');
      try {
        await app.page.mouse.move(box.x + 40, box.y + 40);
        await app.page.mouse.down();
        await app.page.mouse.move(box.x + 160, box.y + 90);
        await app.page.mouse.up();
      } finally {
        await app.page.keyboard.up('Shift');
      }

      const bounds = await app.getPaintedBounds('#0000ff');
      expect(bounds).not.toBeNull();
      // Same 120x50 drag, squared off to the longer axis and still anchored at
      // the press point -- asserting only "width == height" would accept a
      // square of any size drawn anywhere.
      expect(Math.abs(bounds!.width - bounds!.height)).toBeLessThanOrEqual(4);
      expect(bounds!.width).toBeGreaterThanOrEqual(118);
      expect(bounds!.width).toBeLessThanOrEqual(128);
      expect(bounds!.height).toBeGreaterThanOrEqual(118);
      expect(bounds!.height).toBeLessThanOrEqual(128);
      expect(bounds!.x).toBeGreaterThanOrEqual(37);
      expect(bounds!.x).toBeLessThanOrEqual(42);
      expect(bounds!.y).toBeGreaterThanOrEqual(37);
      expect(bounds!.y).toBeLessThanOrEqual(42);
    });
  });
});
