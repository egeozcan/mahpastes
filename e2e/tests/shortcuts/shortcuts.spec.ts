import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Keyboard Shortcuts', () => {

  test.describe('Cheat Sheet', () => {
    test('should open cheat sheet with ? key and close with Escape', async ({ app }) => {
      // Focus body to ensure shortcuts work
      await app.page.locator('body').click();

      // Open cheat sheet
      await app.page.keyboard.press('Shift+/'); // ? is Shift+/
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Should show shortcut categories
      const content = app.page.locator(selectors.shortcuts.cheatsheetContent);
      await expect(content).toContainText('Navigation');
      await expect(content).toContainText('Gallery');

      // Close with Escape
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });

    test('should close cheat sheet by clicking backdrop', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Click the backdrop (the overlay itself, not the content panel)
      await app.page.locator(selectors.shortcuts.cheatsheet).click({ position: { x: 10, y: 10 } });
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });

    test('should toggle cheat sheet with repeated ? presses', async ({ app }) => {
      await app.page.locator('body').click();

      // Open
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Close by pressing ? again (toggle behavior)
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });

    test('should show all shortcut categories in cheat sheet', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      const content = app.page.locator(selectors.shortcuts.cheatsheetContent);
      await expect(content).toContainText('Navigation');
      await expect(content).toContainText('Gallery');
      await expect(content).toContainText('Clip Actions');
      await expect(content).toContainText('Lightbox');
      await expect(content).toContainText('Bulk Actions');
      await expect(content).toContainText('System');
    });

    test('should close cheat sheet by clicking close button', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      await app.page.locator(selectors.shortcuts.cheatsheetClose).click();
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });

    test('should show "Edit shortcuts in Settings" link in cheat sheet', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      const settingsLink = app.page.locator('#cheatsheet-open-settings');
      await expect(settingsLink).toBeVisible();
      await expect(settingsLink).toContainText('Settings');
    });

    test('should open settings from cheat sheet link', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Click the "Settings" link
      await app.page.locator('#cheatsheet-open-settings').click();

      // Cheat sheet should close and settings should open
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
      await expect(app.page.locator(selectors.settings.modal)).not.toHaveClass(/opacity-0/);
    });
  });

  test.describe('Navigation Shortcuts', () => {
    test('should toggle archive view with "a" key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('a');

      // Should be in archive view (empty since clip is not archived)
      await app.expectClipCount(0);

      // Press a again to go back
      await app.page.keyboard.press('a');
      await app.expectClipCount(1);
    });

    test('should focus search with "/" key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('/');

      const searchInput = app.page.locator(selectors.header.searchInput);
      await expect(searchInput).toBeFocused();
    });

    test('should open settings with "," key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press(',');

      await expect(app.page.locator(selectors.settings.modal)).not.toHaveClass(/opacity-0/);
    });

    test('should open drawer with "m" key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('m');

      // Drawer should open (loses translate-x-full class)
      await app.page.waitForFunction(
        (sel) => !document.querySelector(sel)?.classList.contains('translate-x-full'),
        selectors.drawer.panel,
        { timeout: 5000 }
      );
    });

    test('should close drawer with Escape', async ({ app }) => {
      // Open drawer first
      await app.openDrawer();

      // Close with Escape
      await app.page.keyboard.press('Escape');

      // Drawer should close
      await app.page.waitForFunction(
        (sel) => document.querySelector(sel)?.classList.contains('translate-x-full'),
        selectors.drawer.panel,
        { timeout: 5000 }
      );
    });

    test('should open watch view with "w" key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('w');

      await expect(app.page.locator(selectors.watch.view)).toBeVisible();
    });

    test('should open plugins modal with "p" key', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press('p');

      await expect(app.page.locator(selectors.plugins.modal)).toHaveClass(/opacity-100/);
    });

    test('should clear focus when "/" focuses search', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery to focus a clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Focus search should clear grid focus
      await app.page.keyboard.press('/');
      expect(await app.getFocusedClipIndex()).toBe(-1);
    });
  });

  test.describe('Input Guard', () => {
    test('should not fire shortcuts when typing in search input', async ({ app }) => {
      // Focus search
      await app.page.locator(selectors.header.searchInput).click();
      await app.page.keyboard.type('a');

      // Should not toggle archive view - search should have the 'a'
      const searchInput = app.page.locator(selectors.header.searchInput);
      await expect(searchInput).toHaveValue('a');
    });

    test('should not fire multiple different shortcuts when typing in search', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Focus search and type several shortcut characters
      await app.page.locator(selectors.header.searchInput).click();
      await app.page.keyboard.type('awpn');

      // Search should contain the typed chars
      const searchInput = app.page.locator(selectors.header.searchInput);
      await expect(searchInput).toHaveValue('awpn');

      // Gallery should still show the clip (no shortcuts fired)
      await app.expectClipCount(1);
    });
  });

  test.describe('Grid Navigation', () => {
    test('should focus first clip when Tabbing into gallery', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Tab into gallery (click body first for consistent focus position)
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Should have a focused clip
      await expect(app.page.locator(selectors.shortcuts.focusedClip)).toBeVisible();
    });

    test('should navigate between clips with arrow keys', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Tab into gallery to focus first clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      const idx1 = await app.getFocusedClipIndex();
      expect(idx1).toBe(0);

      // Move right
      await app.page.keyboard.press('ArrowRight');
      const idx2 = await app.getFocusedClipIndex();
      expect(idx2).toBe(1);
    });

    test('should open lightbox on Enter when clip is focused', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery then press Enter
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      await app.page.keyboard.press('Enter');

      expect(await app.isLightboxOpen()).toBe(true);
    });

    test('should clamp navigation at first clip (ArrowLeft does not go below 0)', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Tab into gallery to focus first clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Try to go left past start
      await app.page.keyboard.press('ArrowLeft');
      expect(await app.getFocusedClipIndex()).toBe(0);
    });

    test('should clamp navigation at last clip (ArrowRight does not wrap)', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Tab into gallery, then move to last
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      await app.page.keyboard.press('ArrowRight');
      expect(await app.getFocusedClipIndex()).toBe(1);

      // Try to go past end
      await app.page.keyboard.press('ArrowRight');
      expect(await app.getFocusedClipIndex()).toBe(1);
    });

    test('should navigate with ArrowDown key', async ({ app }) => {
      // Upload several clips to potentially have multiple rows
      const paths = [];
      for (let i = 0; i < 3; i++) {
        const p = await createTempFile(generateTestImage(100, 100, ['red', 'blue', 'green'][i]), 'png');
        paths.push(p);
      }
      for (const p of paths) {
        await app.uploadFile(p);
      }
      await app.expectClipCount(3);

      // Tab into gallery to focus first clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      const startIdx = await app.getFocusedClipIndex();
      expect(startIdx).toBe(0);

      // ArrowDown should move focus
      await app.page.keyboard.press('ArrowDown');
      const idx = await app.getFocusedClipIndex();
      expect(idx).toBeGreaterThanOrEqual(0);
    });

    test('should navigate with ArrowUp key', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Tab into gallery, move right, then up should clamp to 0
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      await app.page.keyboard.press('ArrowRight');
      expect(await app.getFocusedClipIndex()).toBe(1);

      await app.page.keyboard.press('ArrowUp');
      // ArrowUp moves up by column count, should clamp to 0
      const idx = await app.getFocusedClipIndex();
      expect(idx).toBeGreaterThanOrEqual(0);
    });

    test('should show focused clip visual indicator', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery to focus clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // The focused clip should be :focus-visible
      const focusedClip = app.page.locator(selectors.shortcuts.focusedClip);
      await expect(focusedClip).toBeVisible();
      expect(await app.isFocusedClipVisible()).toBe(true);
    });

    test('should not navigate when gallery is empty', async ({ app }) => {
      await app.expectClipCount(0);

      await app.page.locator('body').click();

      // With no clips, Tab should not enter an empty gallery
      // Try pressing Tab a few times — focus should never land on a gallery li
      for (let i = 0; i < 5; i++) {
        await app.page.keyboard.press('Tab');
      }
      expect(await app.getFocusedClipIndex()).toBe(-1);
    });

    test('should Tab into gallery and focus first clip, then Tab out', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.expectClipCount(2);

      // Tab into the gallery — first clip should get focus
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Arrow right to second clip
      await app.page.keyboard.press('ArrowRight');
      expect(await app.getFocusedClipIndex()).toBe(1);

      // Tab out of gallery — focus should leave gallery
      await app.page.keyboard.press('Tab');
      expect(await app.getFocusedClipIndex()).toBe(-1);
    });

    test('should support Home/End keys in gallery', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const img3 = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.uploadFile(img3);
      await app.expectClipCount(3);

      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // End should jump to last
      await app.page.keyboard.press('End');
      expect(await app.getFocusedClipIndex()).toBe(2);

      // Home should jump to first
      await app.page.keyboard.press('Home');
      expect(await app.getFocusedClipIndex()).toBe(0);
    });
  });

  test.describe('Clip Actions (with focused clip)', () => {
    test('should delete focused clip with "d" key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery to focus clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });
      expect(await app.getFocusedClipIndex()).toBe(0);

      // Press 'd' to delete
      await app.page.keyboard.press('d');

      // Confirm the delete dialog
      await app.confirmDialog();

      await app.expectClipCount(0);
    });

    test('should archive focused clip with "e" key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery to focus clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Press 'e' to archive
      await app.page.keyboard.press('e');

      // Clip should disappear from active view
      await app.expectClipCount(0);

      // Should appear in archive
      await app.page.locator('body').click();
      await app.page.keyboard.press('a'); // toggle to archive
      await app.expectClipCount(1);
    });

    test('should select/deselect focused clip with Space key', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery to focus clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Press Space to select
      await app.page.keyboard.press('Space');

      // Bulk toolbar should be visible (clip is selected)
      expect(await app.isBulkToolbarVisible()).toBe(true);
      expect(await app.getSelectedCount()).toBe(1);

      // Press Space again to deselect
      await app.page.keyboard.press('Space');
      expect(await app.isBulkToolbarVisible()).toBe(false);
    });

    test('should not fire clip actions when no clip is focused', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();

      // Don't focus a clip - press 'd' to delete
      await app.page.keyboard.press('d');

      // Clip should still be there (no focused clip, so action does nothing)
      await app.expectClipCount(1);
    });
  });

  test.describe('Bulk Shortcuts', () => {
    test('should clear selection with Escape when clips are selected', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Select both clips
      await app.selectClip(path.basename(imagePath1));
      await app.selectClip(path.basename(imagePath2));
      expect(await app.getSelectedCount()).toBe(2);

      // Press Escape to clear selection
      await app.page.keyboard.press('Escape');
      expect(await app.isBulkToolbarVisible()).toBe(false);
    });

    test('should delete selected clips with "d" key in bulk mode', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Select both clips
      await app.selectClip(path.basename(imagePath1));
      await app.selectClip(path.basename(imagePath2));
      expect(await app.getSelectedCount()).toBe(2);

      // Press 'd' to delete in bulk mode
      await app.page.locator('body').click();
      await app.page.keyboard.press('d');

      // Confirm the delete
      await app.confirmDialog();

      await app.expectClipCount(0);
    });

    test('should archive selected clips with "e" key in bulk mode', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Select both clips
      await app.selectClip(path.basename(imagePath1));
      await app.selectClip(path.basename(imagePath2));

      // Press 'e' to archive in bulk mode
      await app.page.locator('body').click();
      await app.page.keyboard.press('e');

      // Clips should disappear from active view
      await app.expectClipCount(0);
    });
  });

  test.describe('Lightbox Shortcuts', () => {
    test('should close lightbox with Escape', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));

      await app.page.keyboard.press('Escape');
      expect(await app.isLightboxOpen()).toBe(false);
    });

    test('should navigate images with arrow keys in lightbox', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);

      // Default sort is "date desc", so imagePath2 (newest) is at index 0
      // Open imagePath2 (index 0) and navigate right to imagePath1 (index 1)
      await app.openLightbox(path.basename(imagePath2));

      // Ensure lightbox is fully active before pressing shortcut keys
      await app.page.waitForSelector('#lightbox.active');

      // Navigate to next image (ArrowRight goes from index 0 to index 1)
      await app.page.keyboard.press('ArrowRight');

      // Wait for lightbox to update after navigation
      await app.page.waitForTimeout(300);

      // Verify we navigated (caption should change to imagePath1)
      const caption = app.page.locator('#lightbox-caption');
      await expect(caption).toContainText(path.basename(imagePath1));
    });

    test('should navigate to previous image with ArrowLeft in lightbox', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);

      // Default sort is "date desc", so imagePath2 is at index 0, imagePath1 at index 1
      // Open imagePath1 (index 1) and navigate left to imagePath2 (index 0)
      await app.openLightbox(path.basename(imagePath1));
      await app.page.waitForSelector('#lightbox.active');

      // Navigate to previous image
      await app.page.keyboard.press('ArrowLeft');
      await app.page.waitForTimeout(300);

      // Verify we navigated back
      const caption = app.page.locator('#lightbox-caption');
      await expect(caption).toContainText(path.basename(imagePath2));
    });

    test('should zoom in with "+" key in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      await app.page.waitForSelector('#lightbox.active');

      // Get initial zoom value
      const initialZoom = await app.page.locator(selectors.lightbox.zoomSlider).inputValue();

      // Press Shift+= which produces '+' on US keyboard
      await app.page.keyboard.press('Shift+=');
      await app.page.waitForTimeout(100);

      const newZoom = await app.page.locator(selectors.lightbox.zoomSlider).inputValue();
      expect(parseInt(newZoom)).toBeGreaterThan(parseInt(initialZoom));
    });

    test('should zoom out with "-" key in lightbox', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      await app.page.waitForSelector('#lightbox.active');

      // First zoom in so we have room to zoom out
      await app.page.keyboard.press('Shift+=');
      await app.page.waitForTimeout(100);
      const afterZoomIn = await app.page.locator(selectors.lightbox.zoomSlider).inputValue();
      expect(parseInt(afterZoomIn)).toBeGreaterThan(100);

      // Press - to zoom out
      await app.page.keyboard.press('-');
      await app.page.waitForTimeout(100);

      const afterZoomOut = await app.page.locator(selectors.lightbox.zoomSlider).inputValue();
      expect(parseInt(afterZoomOut)).toBeLessThan(parseInt(afterZoomIn));
    });
  });

  test.describe('Context Awareness', () => {
    test('should not fire gallery shortcuts when lightbox is open', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      await app.page.waitForSelector('#lightbox.active');

      // Press 'a' - should NOT toggle archive view (lightbox context overrides)
      await app.page.keyboard.press('a');

      // Close lightbox
      await app.page.keyboard.press('Escape');
      expect(await app.isLightboxOpen()).toBe(false);

      // Clip should still be there (archive toggle didn't fire)
      await app.expectClipCount(1);
    });

    test('should not fire shortcuts when settings modal is open', async ({ app }) => {
      await app.openSettingsModal();

      // Press 'a' - should NOT toggle archive view
      await app.page.keyboard.press('a');

      // Close settings
      await app.closeSettingsModal();

      // Gallery should still be in active view
      expect(await app.isArchiveViewActive()).toBe(false);
    });

    test('should not fire shortcuts when confirm dialog is open', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Open delete confirmation
      await app.clickDeleteInCardMenu(path.basename(imagePath));

      // Press 'a' while dialog is open - should not toggle archive
      await app.page.keyboard.press('a');

      // Cancel the dialog
      await app.cancelDialog();

      // Still in active view with 1 clip
      await app.expectClipCount(1);
    });

    test('should not fire shortcuts when plugins modal is open', async ({ app }) => {
      await app.openPluginsModal();

      // Press 'a' - should NOT toggle archive view
      await app.page.keyboard.press('a');

      await app.closePluginsModal();

      // Gallery should still be in active view
      expect(await app.isArchiveViewActive()).toBe(false);
    });

    test('should prioritize bulk context over clip context', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      // Focus a clip AND select clips (both clip and bulk context active)
      await app.page.locator('body').click();
      await app.page.keyboard.press('ArrowRight');
      await app.selectClip(path.basename(imagePath1));
      await app.selectClip(path.basename(imagePath2));

      // Press Escape - should clear selection (bulk context), not do something else
      await app.page.keyboard.press('Escape');
      expect(await app.isBulkToolbarVisible()).toBe(false);
    });
  });

  test.describe('Gallery Shortcuts', () => {
    test('should trigger file upload with "n" key', async ({ app }) => {
      await app.page.locator('body').click();

      // We can't fully test the file dialog opening, but we can verify
      // the file input receives a click by checking its event listener
      const fileInputClicked = await app.page.evaluate(() => {
        return new Promise<boolean>((resolve) => {
          const fileInput = document.getElementById('file-input');
          if (!fileInput) { resolve(false); return; }
          const handler = () => { resolve(true); fileInput.removeEventListener('click', handler); };
          fileInput.addEventListener('click', handler);
          // Trigger the shortcut
          document.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true }));
          // If no click after 500ms, resolve false
          setTimeout(() => resolve(false), 500);
        });
      });

      expect(fileInputClicked).toBe(true);
    });

    test('should toggle select all with Cmd/Ctrl+A', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      await app.page.locator('body').click();

      // Use Meta+a (maps to mod+a in eventToCombo)
      await app.page.keyboard.press('Meta+a');

      expect(await app.isBulkToolbarVisible()).toBe(true);
      expect(await app.getSelectedCount()).toBe(2);

      // Press again to deselect all
      await app.page.keyboard.press('Meta+a');
      expect(await app.isBulkToolbarVisible()).toBe(false);
    });
  });

  test.describe('Settings UI', () => {
    test('should show shortcuts section in settings', async ({ app }) => {
      await app.openSettingsModal();

      const section = app.page.locator(selectors.shortcuts.settingsSection);
      await expect(section).toBeVisible();

      // Should have shortcut rows
      const list = app.page.locator(selectors.shortcuts.settingsList);
      await expect(list).toContainText('Toggle Archive View');
      await expect(list).toContainText('Focus Search');
    });

    test('should rebind a shortcut via click-to-record', async ({ app }) => {
      await app.openSettingsModal();

      // Click the badge for toggle-archive
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();

      // Badge should be in recording mode (shows "...")
      await expect(badge).toContainText('...');

      // Press a new key
      await app.page.keyboard.press('q');

      // Badge should now show the new key
      await expect(badge).toContainText('Q');

      // Close settings
      await app.closeSettingsModal();

      // Verify the new shortcut works
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('q');
      await app.expectClipCount(0); // Switched to archive view
    });

    test('should reset shortcuts to defaults', async ({ app }) => {
      await app.openSettingsModal();

      // First rebind something
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('q');
      await expect(badge).toContainText('Q');

      // Click reset
      const resetBtn = app.page.locator(selectors.shortcuts.resetButton);
      await resetBtn.scrollIntoViewIfNeeded();
      await resetBtn.click();

      // Badge should show default again
      await expect(badge).toContainText('A');
    });

    test('should cancel recording with Escape', async ({ app }) => {
      await app.openSettingsModal();

      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();

      // Get the original text
      const originalText = await badge.textContent();

      // Start recording
      await badge.click();
      await expect(badge).toContainText('...');

      // Cancel with Escape
      await app.page.keyboard.press('Escape');

      // Badge should show original binding
      await expect(badge).toContainText(originalText!.trim());
    });

    test('should show conflict warning when rebinding to used key', async ({ app }) => {
      await app.openSettingsModal();

      // Try to rebind focus-search (default: /) to 'a' (used by toggle-archive)
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('focus-search'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await expect(badge).toContainText('...');

      // Press 'a' - which conflicts with toggle-archive
      await app.page.keyboard.press('a');

      // Conflict warning should appear
      const warning = app.page.locator('[data-testid="shortcut-conflict-warning"]');
      await expect(warning).toBeVisible();
      await expect(warning).toContainText('Toggle Archive View');
    });

    test('should allow overriding conflict', async ({ app }) => {
      await app.openSettingsModal();

      // Try to rebind focus-search to 'a' (conflicts with toggle-archive)
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('focus-search'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('a');

      // Click override
      const overrideBtn = app.page.locator('[data-testid="shortcut-conflict-override"]');
      await expect(overrideBtn).toBeVisible();
      await overrideBtn.click();

      // focus-search should now show 'A'
      await expect(app.page.locator(selectors.shortcuts.shortcutBadge('focus-search'))).toContainText('A');

      // toggle-archive should be unbound (shown as dash)
      const archiveBadge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      const archiveText = await archiveBadge.textContent();
      expect(archiveText?.trim()).toBe('\u2014'); // em-dash for unbound
    });

    test('should allow cancelling conflict', async ({ app }) => {
      await app.openSettingsModal();

      // Try to rebind focus-search to 'a' (conflicts with toggle-archive)
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('focus-search'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('a');

      // Click cancel
      const cancelBtn = app.page.locator('[data-testid="shortcut-conflict-cancel"]');
      await expect(cancelBtn).toBeVisible();
      await cancelBtn.click();

      // Warning should be dismissed
      await expect(app.page.locator('[data-testid="shortcut-conflict-warning"]')).not.toBeVisible();

      // focus-search should still have original binding
      await expect(app.page.locator(selectors.shortcuts.shortcutBadge('focus-search'))).toContainText('/');
    });

    test('should rebind with modifier keys', async ({ app }) => {
      await app.openSettingsModal();

      // Rebind toggle-archive to Ctrl+Shift+X
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();

      await app.page.keyboard.press('Control+Shift+x');

      // Badge should show the modifier combo
      const text = await badge.textContent();
      expect(text).toBeTruthy();
      // Should contain something indicating modifiers (Ctrl or ⌘)
      expect(text!.length).toBeGreaterThan(1);
    });

    test('should show all category headers in settings', async ({ app }) => {
      await app.openSettingsModal();

      const list = app.page.locator(selectors.shortcuts.settingsList);
      await expect(list).toContainText('Navigation');
      await expect(list).toContainText('Gallery');
      await expect(list).toContainText('Clip Actions');
      await expect(list).toContainText('Lightbox');
      await expect(list).toContainText('Bulk Actions');
      await expect(list).toContainText('System');
    });

    test('should show all registered shortcuts in settings list', async ({ app }) => {
      await app.openSettingsModal();

      const list = app.page.locator(selectors.shortcuts.settingsList);

      // Check for a selection of key shortcuts across categories
      await expect(list).toContainText('Focus Search');
      await expect(list).toContainText('Toggle Archive View');
      await expect(list).toContainText('Open Settings');
      await expect(list).toContainText('Open in Lightbox');
      await expect(list).toContainText('Delete Clip');
      await expect(list).toContainText('Close Lightbox');
      await expect(list).toContainText('Show Shortcuts');
    });

    test('should show recording visual state (pulse animation)', async ({ app }) => {
      await app.openSettingsModal();

      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();

      // Badge should have recording classes
      await expect(badge).toHaveClass(/ring-2/);
      await expect(badge).toHaveClass(/ring-stone-400/);
      await expect(badge).toHaveClass(/animate-pulse/);

      // Cancel
      await app.page.keyboard.press('Escape');
    });

    test('should show toast after reset to defaults', async ({ app }) => {
      await app.openSettingsModal();

      // First rebind something
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('q');

      // Click reset
      const resetBtn = app.page.locator(selectors.shortcuts.resetButton);
      await resetBtn.scrollIntoViewIfNeeded();
      await resetBtn.click();

      // Should show toast
      await app.expectToast('Shortcuts reset to defaults');
    });

    test('should persist rebinding after closing and reopening settings', async ({ app }) => {
      await app.openSettingsModal();

      // Rebind toggle-archive to 'q'
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('q');
      await expect(badge).toContainText('Q');

      // Close and reopen settings
      await app.closeSettingsModal();
      await app.openSettingsModal();

      // Should still show 'X'
      const reopenedBadge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await reopenedBadge.scrollIntoViewIfNeeded();
      await expect(reopenedBadge).toContainText('Q');
    });

    test('should not accept bare modifier keys as shortcuts', async ({ app }) => {
      await app.openSettingsModal();

      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await expect(badge).toContainText('...');

      // Press only a modifier key
      await app.page.keyboard.down('Shift');
      await app.page.keyboard.up('Shift');

      // Should still be in recording mode (bare modifier ignored)
      await expect(badge).toContainText('...');

      // Cancel
      await app.page.keyboard.press('Escape');
    });
  });

  test.describe('Shortcut Persistence', () => {
    test('should preserve rebinding when original key is pressed', async ({ app }) => {
      await app.openSettingsModal();

      // Rebind toggle-archive from 'a' to 'q'
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('q');
      await expect(badge).toContainText('Q');

      await app.closeSettingsModal();

      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Original key 'a' should NOT toggle archive anymore
      await app.page.locator('body').click();
      await app.page.keyboard.press('a');
      await app.expectClipCount(1); // Still in active view

      // New key 'q' should toggle archive
      await app.page.keyboard.press('q');
      await app.expectClipCount(0); // Switched to archive
    });

    test('should reflect rebinding in cheat sheet', async ({ app }) => {
      await app.openSettingsModal();

      // Rebind toggle-archive to 'q'
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('q');

      await app.closeSettingsModal();

      // Open cheat sheet
      await app.page.locator('body').click();
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // The cheat sheet should show the new key 'X' for toggle archive
      const content = app.page.locator(selectors.shortcuts.cheatsheetContent);
      await expect(content).toContainText('Q');
    });
  });

  test.describe('Modal Focus Management', () => {
    test('should focus settings modal on open and restore focus on close', async ({ app }) => {
      await app.page.locator('body').click();
      await app.page.keyboard.press(',');
      await expect(app.page.locator(selectors.settings.modal)).not.toHaveClass(/opacity-0/);

      // Focus should be inside settings modal
      const focusInSettings = await app.page.evaluate(() => {
        const modal = document.getElementById('settings-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusInSettings).toBe(true);

      // Close with Escape
      await app.page.keyboard.press('Escape');
      await app.page.waitForSelector(`${selectors.settings.modal}.opacity-0`, { timeout: 5000 });

      // Focus should not be in settings
      const focusAfterClose = await app.page.evaluate(() => {
        const modal = document.getElementById('settings-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusAfterClose).toBe(false);
    });

    test('should focus cancel button in confirm dialog', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.clickDeleteInCardMenu(path.basename(imagePath));

      // Cancel button should be focused
      await expect(app.page.locator(selectors.confirm.cancelButton)).toBeFocused();
    });

    test('should focus plugins modal on open and restore focus on close', async ({ app }) => {
      await app.openPluginsModal();

      // Focus should be inside plugins modal
      const focusInPlugins = await app.page.evaluate(() => {
        const modal = document.getElementById('plugins-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusInPlugins).toBe(true);

      // Close
      await app.closePluginsModal();

      // Focus should not be in plugins modal
      const focusAfterClose = await app.page.evaluate(() => {
        const modal = document.getElementById('plugins-modal');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusAfterClose).toBe(false);
    });

    test('should focus lightbox on open and restore focus on close', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.openLightbox(path.basename(imagePath));
      await app.page.waitForSelector('#lightbox.active');

      // Focus should be inside lightbox
      const focusInLightbox = await app.page.evaluate(() => {
        const modal = document.getElementById('lightbox');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusInLightbox).toBe(true);

      // Close with Escape and wait for transition + focus restore (300ms setTimeout)
      await app.page.keyboard.press('Escape');
      await app.page.waitForTimeout(500);

      // Focus should not be in lightbox
      const focusAfterClose = await app.page.evaluate(() => {
        const modal = document.getElementById('lightbox');
        return modal?.contains(document.activeElement) ?? false;
      });
      expect(focusAfterClose).toBe(false);
    });
  });

  test.describe('Edge Cases', () => {
    test('should handle rapid key presses without errors', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Tab into gallery first, then rapidly press arrow keys
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      for (let i = 0; i < 10; i++) {
        await app.page.keyboard.press('ArrowRight');
      }

      // Should not crash - focus index should be clamped
      const idx = await app.getFocusedClipIndex();
      expect(idx).toBeGreaterThanOrEqual(0);
    });

    test('should handle shortcuts with no clips gracefully', async ({ app }) => {
      await app.expectClipCount(0);

      await app.page.locator('body').click();

      // These should all be no-ops without errors
      await app.page.keyboard.press('ArrowRight');
      await app.page.keyboard.press('Enter');
      await app.page.keyboard.press('d');
      await app.page.keyboard.press('c');
      await app.page.keyboard.press('e');
      await app.page.keyboard.press('Space');

      // App should still be functional
      await app.expectClipCount(0);
    });

    test('should not fire gallery shortcuts when watch view is open', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Open watch view
      await app.page.locator('body').click();
      await app.page.keyboard.press('w');
      await expect(app.page.locator(selectors.watch.view)).toBeVisible();

      // Press 'a' - should NOT toggle archive
      await app.page.keyboard.press('a');

      // Close watch view and check gallery is unchanged
      await app.closeWatchView();
      await app.expectClipCount(1);
    });
  });
});
