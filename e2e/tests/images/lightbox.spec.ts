import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Image Lightbox', () => {
  test.afterEach(async ({ app }) => {
    await app.page.setViewportSize({ width: 1280, height: 800 });
  });

  test.describe('Open and Close', () => {
    test('should open lightbox when clicking view on image', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should close lightbox when clicking close button', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);
      await app.closeLightbox();

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(false);
    });

    test('should close lightbox when pressing Escape', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      await app.page.keyboard.press('Escape');
      // Wait for lightbox to close (loses .active class)
      await app.page.waitForSelector(`${selectors.lightbox.overlay}:not(.active)`);

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(false);
    });

    test('should display image in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });

    test('keeps desktop close and navigation controls inside the viewport', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      await app.uploadFiles(files);
      await app.openLightbox(path.basename(files[1]));

      await expect(app.page.locator(selectors.lightbox.closeButton)).toBeInViewport();
      await expect(app.page.locator(selectors.lightbox.nextButton)).toBeInViewport();
      await app.page.locator(selectors.lightbox.nextButton).click();
      await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(files[0]));
      await app.page.locator(selectors.lightbox.closeButton).click();
      await expect(app.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
    });
  });

  test.describe('Navigation', () => {
    test('should navigate to next image', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);

      // Open lightbox on first image (newest is first, so that's files[2])
      await app.openLightbox(filenames[2]);

      // Navigate next
      await app.lightboxNext();

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should navigate to previous image', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[1]);

      await app.lightboxPrev();

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('should navigate with arrow keys', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[0]);

      // Navigate with arrow key
      await app.page.keyboard.press('ArrowRight');

      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });

    test('stays on the last image at the navigation boundary', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.openLightbox(filenames[0]);
      const caption = await app.page.locator(selectors.lightbox.caption).textContent();

      await expect(app.page.locator(selectors.lightbox.nextButton)).toBeHidden();
      await app.page.keyboard.press('ArrowRight');
      await expect(app.page.locator(selectors.lightbox.caption)).toHaveText(caption || '');
      expect(await app.isLightboxOpen()).toBe(true);
    });
  });

  test.describe('Single Image', () => {
    test('should handle lightbox with single image', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      // Navigation buttons may be hidden or disabled for single image
      const isOpen = await app.isLightboxOpen();
      expect(isOpen).toBe(true);
    });
  });

  test.describe('Lightbox with Different Image Sizes', () => {
    test('should display small image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(50, 50), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });

    test('should display large image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(800, 600), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });

    test('should display non-square image correctly', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(400, 100), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const lightboxImage = app.page.locator(selectors.lightbox.image);
      await expect(lightboxImage).toBeVisible();
    });
  });

  test.describe('Focus Management', () => {
    test('should trap Tab focus within lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.openLightbox(path.basename(imagePath));

      // Tab through all focusable elements — should cycle back to first
      const focusableCount = await app.page.evaluate(() => {
        const lb = document.getElementById('lightbox');
        return lb!.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])').length;
      });
      expect(focusableCount).toBeGreaterThan(0);

      // Press Tab enough times to cycle
      for (let i = 0; i < focusableCount + 1; i++) {
        await app.page.keyboard.press('Tab');
      }

      // Focus should still be inside lightbox
      const focusInLightbox = await app.page.evaluate(() => {
        const lb = document.getElementById('lightbox');
        return lb?.contains(document.activeElement) ?? false;
      });
      expect(focusInLightbox).toBe(true);
    });

    test('should restore focus to previous element after lightbox close', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Open lightbox via the view button (click-based, since roving tabindex isn't on gallery yet)
      await app.openLightbox(path.basename(imagePath));
      expect(await app.isLightboxOpen()).toBe(true);

      // Close lightbox with Escape
      await app.page.keyboard.press('Escape');
      await app.page.waitForFunction(() => {
        const lb = document.getElementById('lightbox');
        return !lb?.classList.contains('active');
      });

      // Wait for focus restoration (lightbox has a 300ms delay)
      await app.page.waitForTimeout(400);

      // Focus should NOT be inside the lightbox
      const focusInLightbox = await app.page.evaluate(() => {
        const lb = document.getElementById('lightbox');
        return lb?.contains(document.activeElement) ?? false;
      });
      expect(focusInLightbox).toBe(false);
    });
  });

  test.describe('Zoom Info Display', () => {
    test('should display zoom percentage in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const zoomInfo = app.page.locator(selectors.lightbox.zoomInfo);
      await expect(zoomInfo).toBeVisible();
      // Should show a percentage (e.g., "100%" or "50%")
      await expect(zoomInfo).toHaveText(/^\d+%$/);
    });

    test('should show zoom relative to native dimensions', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(2000, 2000), 'png');
      const filename = path.basename(imagePath);

      await app.uploadFile(imagePath);
      await app.openLightbox(filename);

      const zoomInfo = app.page.locator(selectors.lightbox.zoomInfo);
      await expect(zoomInfo).toBeVisible();
      await expect.poll(async () => {
        const text = await zoomInfo.textContent();
        return parseInt(text || '100', 10);
      }, { timeout: 5000 }).toBeLessThan(100);
    });
  });

  test.describe('Controller lifecycle and focus', () => {
    test('restores the exact opener after keyboard navigation', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      await app.uploadFiles(files);

      const opener = await app.getClipByFilename(path.basename(files[1]));
      await opener.focus();
      await opener.press('Enter');
      await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
      await app.page.keyboard.press('ArrowRight');
      await app.page.keyboard.press('Escape');

      await expect(opener).toBeFocused();
    });

    test('focuses the clip at the former opener index when the opener is removed', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
      ]);
      await app.uploadFiles(files);
      const openerCard = await app.getClipByFilename(path.basename(files[1]));
      await openerCard.locator('[data-action="open-lightbox"]').click();
      await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);
      await openerCard.evaluate(card => card.remove());
      await app.page.keyboard.press('Escape');

      const remainingCard = await app.getClipByFilename(path.basename(files[0]));
      await expect(remainingCard).toBeFocused();
    });
  });

  test.describe('Actual-scale zoom and input', () => {
    test('uses actual image scale for Fit and 1:1', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(2000, 1000), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      const fitText = await app.page.locator(selectors.lightbox.zoomInfo).textContent();
      expect(Number.parseInt(fitText || '100', 10)).toBeLessThan(100);
      await app.page.locator(selectors.lightbox.zoomActual).click();
      await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('100%');
      await app.page.locator(selectors.lightbox.zoomFit).click();
      await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText(fitText || '');
    });

    test('clamps actual scale at 800 percent', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      await app.page.locator(selectors.lightbox.zoomSlider).press('End');
      await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('800%');
    });

    test('recomputes Fit when the viewport changes', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(2000, 1000), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      const before = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100', 10);
      await app.page.setViewportSize({ width: 640, height: 600 });
      await expect.poll(async () => Number.parseInt(
        await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100',
        10,
      )).toBeLessThan(before);
      await expect(app.page.locator(selectors.lightbox.zoomFit)).toHaveAttribute('aria-pressed', 'true');
    });

    test('keeps the image coordinate beneath the modified-wheel cursor stationary', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      const result = await app.page.locator(selectors.lightbox.viewport).evaluate((viewport) => {
        const rect = viewport.getBoundingClientRect();
        const clientX = rect.left + rect.width * 0.55;
        const clientY = rect.top + rect.height * 0.45;
        const image = document.getElementById('lightbox-img')!;
        const before = image.getBoundingClientRect();
        const beforePoint = {
          x: (clientX - before.left) / before.width,
          y: (clientY - before.top) / before.height,
        };
        viewport.dispatchEvent(new WheelEvent('wheel', {
          deltaY: -120,
          ctrlKey: true,
          clientX,
          clientY,
          bubbles: true,
          cancelable: true,
        }));
        const after = image.getBoundingClientRect();
        return {
          beforePoint,
          afterPoint: {
            x: (clientX - after.left) / after.width,
            y: (clientY - after.top) / after.height,
          },
        };
      });
      expect(Math.abs(result.afterPoint.x - result.beforePoint.x)).toBeLessThan(0.001);
      expect(Math.abs(result.afterPoint.y - result.beforePoint.y)).toBeLessThan(0.001);
    });

    test('drags a zoomed image and double-click toggles Fit and 1:1', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(1600, 1200), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      const viewport = app.page.locator(selectors.lightbox.viewport);
      await viewport.dblclick({ position: { x: 320, y: 220 } });
      await expect(app.page.locator(selectors.lightbox.zoomInfo)).toHaveText('100%');
      const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
      const box = await viewport.boundingBox();
      if (!box) throw new Error('Lightbox viewport has no bounding box');
      await app.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await app.page.mouse.down();
      await app.page.mouse.move(box.x + box.width / 2 + 80, box.y + box.height / 2 + 40);
      await app.page.mouse.up();
      const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
      expect(after).not.toBe(before);
      await viewport.dblclick({ position: { x: 320, y: 220 } });
      await expect(app.page.locator(selectors.lightbox.zoomFit)).toHaveAttribute('aria-pressed', 'true');
    });

    test('pinch zooms around the touch centroid', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      const fit = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '100', 10);
      await app.page.locator(selectors.lightbox.viewport).evaluate(viewport => {
        type TestTouch = { clientX: number; clientY: number };
        const fire = (type: string, touches: TestTouch[]) => {
          const event = new Event(type, { bubbles: true, cancelable: true });
          Object.defineProperty(event, 'touches', { value: touches });
          viewport.dispatchEvent(event);
        };
        fire('touchstart', [{ clientX: 180, clientY: 200 }, { clientX: 280, clientY: 200 }]);
        fire('touchmove', [{ clientX: 140, clientY: 200 }, { clientX: 320, clientY: 200 }]);
        fire('touchend', []);
      });
      const zoomed = Number.parseInt(await app.page.locator(selectors.lightbox.zoomInfo).textContent() || '0', 10);
      expect(zoomed).toBeGreaterThan(fit);
    });

    test('pans with Shift+Arrow without navigating', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(1600, 1200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(1600, 1200, [0, 0, 255]), 'png'),
      ]);
      await app.uploadFiles(files);
      await app.openLightbox(path.basename(files[1]));
      await app.page.keyboard.press('1');
      const caption = await app.page.locator(selectors.lightbox.caption).textContent();
      const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(el => getComputedStyle(el).transform);
      await app.page.keyboard.press('Shift+ArrowRight');
      const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(el => getComputedStyle(el).transform);
      expect(after).not.toBe(before);
      await expect(app.page.locator(selectors.lightbox.caption)).toHaveText(caption || '');
    });

    for (const axis of ['vertical', 'horizontal'] as const) {
      test(`advances once for a ${axis} wheel gesture at Fit`, async ({ app }) => {
        const files = await Promise.all([
          createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png'),
          createTempFile(generateTestImage(100, 100, [0, 255, 0]), 'png'),
          createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png'),
        ]);
        await app.uploadFiles(files);
        await app.openLightbox(path.basename(files[2]));
        await app.page.locator(selectors.lightbox.viewport).evaluate((viewport, wheelAxis) => {
          for (const delta of [8, 18, 30, 22, 10, 4]) {
            const init = wheelAxis === 'horizontal' ? { deltaX: delta } : { deltaY: delta };
            viewport.dispatchEvent(new WheelEvent('wheel', { ...init, bubbles: true, cancelable: true }));
          }
        }, axis);
        await expect(app.page.locator(selectors.lightbox.caption)).toContainText(path.basename(files[1]));
      });
    }

    test('pans instead of navigating when wheel-scrolling above Fit', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(1600, 1200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(1600, 1200, [0, 0, 255]), 'png'),
      ]);
      await app.uploadFiles(files);
      await app.openLightbox(path.basename(files[1]));
      await app.page.keyboard.press('1');
      const caption = await app.page.locator(selectors.lightbox.caption).textContent();
      const before = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
      await app.page.locator(selectors.lightbox.viewport).evaluate(viewport => {
        viewport.dispatchEvent(new WheelEvent('wheel', { deltaY: 80, bubbles: true, cancelable: true }));
      });
      const after = await app.page.locator(selectors.lightbox.panLayer).evaluate(element => getComputedStyle(element).transform);
      expect(after).not.toBe(before);
      await expect(app.page.locator(selectors.lightbox.caption)).toHaveText(caption || '');
    });
  });

  test.describe('Accessibility and responsive layout', () => {
    test('names every zoom control and exposes actual slider value text', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(1200, 800), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      const viewer = app.page.locator(selectors.lightbox.overlay);
      await expect(viewer.getByRole('button', { name: 'Zoom out', exact: true })).toBeVisible();
      await expect(viewer.getByRole('slider', { name: 'Image zoom' })).toHaveAttribute('aria-valuetext', /% actual; Fit is \d+%/);
      await expect(viewer.getByRole('button', { name: 'Zoom in', exact: true })).toBeVisible();
      await expect(viewer.getByRole('button', { name: 'Fit image to window' })).toBeVisible();
      await expect(viewer.getByRole('button', { name: 'Show image at actual size' })).toBeVisible();
    });

    test('keeps the integrated toolbar usable at a narrow viewport', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(800, 600), 'png');
      await app.uploadFile(imagePath);
      await app.page.setViewportSize({ width: 480, height: 700 });
      await app.openLightbox(path.basename(imagePath));

      const controls = [selectors.lightbox.closeButton, selectors.lightbox.zoomOut, selectors.lightbox.zoomSlider, selectors.lightbox.zoomIn, selectors.lightbox.zoomFit, selectors.lightbox.zoomActual];
      for (const selector of controls) await expect(app.page.locator(selector)).toBeInViewport();
      const layout = await app.page.locator(selectors.lightbox.bar).evaluate(element => ({
        overflow: element.scrollWidth > element.clientWidth,
        clientWidth: element.clientWidth,
        scrollWidth: element.scrollWidth,
        children: Array.from(element.children).map(child => ({
          className: child.className,
          clientWidth: child.clientWidth,
          scrollWidth: child.scrollWidth,
        })),
      }));
      expect(layout.overflow, JSON.stringify(layout)).toBe(false);
    });
  });
});
