import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Image Comparison', () => {
  test.describe('Panning', () => {
    test('a zoomed-in pair can be panned with the wheel, within limits, and Fit recentres it', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(400, 400, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(400, 400, [0, 255, 0]), 'png'),
      ]);
      await app.uploadFiles(files);
      await app.selectClips(files.map((f) => path.basename(f)));
      await app.openComparison();

      const wheel = (opts: any) => app.page.evaluate((o) => {
        const viewport = document.getElementById('comparison-viewport')!;
        viewport.dispatchEvent(new WheelEvent('wheel', { ...o, bubbles: true, cancelable: true }));
        // @ts-ignore - app.js top-level lets
        return { x: comparePanX, y: comparePanY, zoom: zoomLevel };
      }, opts);

      // Fitted: nothing overflows, so the wheel moves nothing.
      const fitted = await wheel({ deltaY: 80 });
      expect(fitted).toMatchObject({ x: 0, y: 0 });

      // Ctrl+wheel zooms; then the wheel pans.
      for (let i = 0; i < 20; i++) await wheel({ deltaY: -100, ctrlKey: true });
      const zoomed = await wheel({ deltaY: 0 });
      expect(zoomed.zoom).toBeGreaterThan(fitted.zoom);
      const down = await wheel({ deltaY: 60 });
      expect(down.y).toBe(-60);
      const side = await wheel({ deltaY: 40, shiftKey: true });
      expect(side.x).toBe(-40);

      // It stops at the edge instead of leaving the viewport.
      const far = await wheel({ deltaY: 1000000 });
      expect(far.y).toBeLessThan(0);
      expect(Math.abs(far.y)).toBeLessThan(100000);
      const inView = await app.page.evaluate(() => {
        const c = document.getElementById('comparison-container')!.getBoundingClientRect();
        const v = document.getElementById('comparison-viewport')!.getBoundingClientRect();
        return c.bottom >= v.bottom - 1 && c.top <= v.top + 1;
      });
      expect(inView).toBe(true);

      await app.page.locator(selectors.comparison.fitButton).click();
      expect(await wheel({ deltaY: 0 })).toMatchObject({ x: 0, y: 0 });
    });

    test('an edge-aligned pair can still be panned to both of its sides', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(300, 300, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(300, 300, [0, 255, 0]), 'png'),
      ]);
      await app.uploadFiles(files);
      await app.selectClips(files.map((f) => path.basename(f)));
      await app.openComparison();

      // Put the pair against the left edge, as the Align select does, and zoom.
      const edges = await app.page.evaluate(() => {
        const viewport = document.getElementById('comparison-viewport')!;
        viewport.style.justifyContent = 'flex-start';
        // @ts-ignore - app.js top-level lets and modals.js function
        zoomLevel = 5; updateComparisonView();
        const read = () => {
          const c = document.getElementById('comparison-container')!.getBoundingClientRect();
          const v = viewport.getBoundingClientRect();
          return { left: Math.round(c.left - v.left), right: Math.round(v.right - c.right) };
        };
        const wheel = (deltaX: number) => viewport.dispatchEvent(new WheelEvent('wheel', { deltaX, bubbles: true, cancelable: true }));
        wheel(-1000000);
        const atLeft = read();
        wheel(1000000);
        const atRight = read();
        return { atLeft, atRight };
      });
      // Its left edge can be brought to the viewport's left edge, and its
      // right edge to the right one, with no empty band beyond either.
      expect(Math.abs(edges.atLeft.left)).toBeLessThanOrEqual(1);
      expect(Math.abs(edges.atRight.right)).toBeLessThanOrEqual(1);
    });
  });

  test.describe('Open and Close', () => {
    test('should open comparison modal with two selected images', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });

    test('should close comparison modal', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.closeComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(false);
    });
  });

  test.describe('Comparison Modes', () => {
    test('should switch to fade mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      await app.setComparisonMode('fade');

      // Fade slider should be visible
      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toBeVisible();
    });

    test('should switch to slider mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      await app.setComparisonMode('slider');

      // Position slider should be visible
      const positionSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(positionSlider).toBeVisible();
    });
  });

  test.describe('Fade Mode Controls', () => {
    test('should adjust fade level', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(75);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('75');
    });

    test('should set fade to 0 (show first image only)', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(0);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('0');
    });

    test('should set fade to 100 (show second image only)', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('fade');

      await app.setFadeLevel(100);

      const fadeSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(fadeSlider).toHaveValue('100');
    });
  });

  test.describe('Slider Mode Controls', () => {
    test('should adjust slider position', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('slider');

      await app.setSliderPosition(30);

      const positionSlider = app.page.locator(selectors.comparison.rangeSlider);
      await expect(positionSlider).toHaveValue('30');
    });
  });

  test.describe('Zoom Controls', () => {
    test('should have zoom in button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const zoomInBtn = app.page.locator(selectors.comparison.zoomInButton);
      await expect(zoomInBtn).toBeVisible();
    });

    test('should have zoom out button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const zoomOutBtn = app.page.locator(selectors.comparison.zoomOutButton);
      await expect(zoomOutBtn).toBeVisible();
    });

    test('should have fit button', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const fitBtn = app.page.locator(selectors.comparison.fitButton);
      await expect(fitBtn).toBeVisible();
    });
  });

  test.describe('Different Image Sizes', () => {
    test('should compare images of same size', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });

    test('should compare images of different sizes', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(300, 150, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(true);
    });
  });

  test.describe('Diff Mode', () => {
    test('should switch to diff mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('diff');

      const isDiffVisible = await app.isComparisonDiffVisible();
      expect(isDiffVisible).toBe(true);
    });

    test('should show similarity score in diff mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('diff');

      // Wait for diff to load (async backend call)
      await app.page.waitForFunction(
        (sel) => {
          const el = document.querySelector(sel);
          return el && !el.classList.contains('hidden') && el.textContent.includes('similar');
        },
        selectors.comparison.similarity,
        { timeout: 10000 }
      );

      const similarity = await app.getComparisonSimilarity();
      expect(similarity).toContain('similar');
    });

    test('should show threshold label in diff mode', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.setComparisonMode('diff');

      const label = await app.getComparisonRangeLabel();
      expect(label).toBe('Threshold');
    });
  });

  test.describe('Swap Images', () => {
    test('should swap images when swap button clicked', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const initialSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');

      await app.swapComparisonImages();

      const swappedSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
      expect(swappedSrc).not.toBe(initialSrc);
    });
  });

  test.describe('Image Info', () => {
    test('should display image dimensions', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 150, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(300, 250, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      // Wait for image info to appear
      await app.page.waitForSelector(`${selectors.comparison.imageInfo}:not(.hidden)`, { timeout: 5000 });

      const info = await app.getComparisonImageInfo();
      expect(info).toContain('200');
      expect(info).toContain('150');
      expect(info).toContain('300');
      expect(info).toContain('250');
    });
  });

  test.describe('Keyboard Shortcuts', () => {
    test('should switch to diff mode with key 3', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      await app.page.keyboard.press('3');

      const isDiffVisible = await app.isComparisonDiffVisible();
      expect(isDiffVisible).toBe(true);
    });

    test('should switch modes with keys 1 and 2', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      // Switch to slider
      await app.page.keyboard.press('2');
      let label = await app.getComparisonRangeLabel();
      expect(label).toBe('Position');

      // Switch back to fade
      await app.page.keyboard.press('1');
      label = await app.getComparisonRangeLabel();
      expect(label).toBe('Opacity');
    });

    test('should swap images with s key', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();

      const initialSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
      await app.page.keyboard.press('s');
      const swappedSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
      expect(swappedSrc).not.toBe(initialSrc);
    });

    test('should close comparison with Escape', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectClips(filenames);
      await app.openComparison();
      await app.page.keyboard.press('Escape');

      const isOpen = await app.isComparisonOpen();
      expect(isOpen).toBe(false);
    });
  });
});
