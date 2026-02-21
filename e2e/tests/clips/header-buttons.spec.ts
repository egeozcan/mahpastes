import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Header Buttons', () => {
  test('Add button triggers file input', async ({ app, page }) => {
    const fileInput = page.locator(selectors.upload.fileInput);
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await fileInput.setInputFiles(imagePath);
    await app.expectClipCount(1);
  });

  test('Header archive button toggles archive view', async ({ app, page }) => {
    // Upload a clip then archive it
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.archiveClip(path.basename(imagePath));

    // Click header archive button
    await page.locator(selectors.header.archiveButton).click();
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

    // Should see the archived clip
    await app.expectClipCount(1);

    // Button should show active state
    const pressed = await page.locator(selectors.header.archiveButton).getAttribute('aria-pressed');
    expect(pressed).toBe('true');

    // Click again to go back
    await page.locator(selectors.header.archiveButton).click();
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 5000 });

    // Should be back in active view (0 clips since we archived it)
    await app.expectClipCount(0);

    const pressedAfter = await page.locator(selectors.header.archiveButton).getAttribute('aria-pressed');
    expect(pressedAfter).toBe('false');
  });

  test('Watch view back button returns to clips view', async ({ app, page }) => {
    // Open watch view
    await app.openWatchView();
    expect(await app.isWatchViewOpen()).toBe(true);

    // Click back button
    await page.locator(selectors.watch.backButton).click();

    // Watch view should be hidden
    await page.waitForFunction(
      (sel) => document.querySelector(sel)?.classList.contains('hidden'),
      selectors.watch.view,
      { timeout: 5000 }
    );
    expect(await app.isWatchViewOpen()).toBe(false);
  });
});
