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
  });

  test.describe('Grid Navigation', () => {
    test('should focus first clip on arrow key press', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      await app.page.locator('body').click();
      await app.page.keyboard.press('ArrowRight');

      // Should have a focused clip
      await expect(app.page.locator(selectors.shortcuts.focusedClip)).toBeVisible();
    });

    test('should navigate between clips with arrow keys', async ({ app }) => {
      const imagePath1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const imagePath2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(imagePath1);
      await app.uploadFile(imagePath2);
      await app.expectClipCount(2);

      await app.page.locator('body').click();

      // Focus first clip
      await app.page.keyboard.press('ArrowRight');
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

      await app.page.locator('body').click();
      await app.page.keyboard.press('ArrowRight');
      await app.page.keyboard.press('Enter');

      expect(await app.isLightboxOpen()).toBe(true);
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

      // Open the first image in the lightbox array (imagePath1 is oldest, at index 0)
      await app.openLightbox(path.basename(imagePath1));

      // Ensure lightbox is fully active before pressing shortcut keys
      await app.page.waitForSelector('#lightbox.active');

      // Navigate to next image (ArrowRight goes from index 0 to index 1)
      await app.page.keyboard.press('ArrowRight');

      // Wait for lightbox to update after navigation
      await app.page.waitForTimeout(300);

      // Verify we navigated (caption should change to imagePath2)
      const caption = app.page.locator('#lightbox-caption');
      await expect(caption).toContainText(path.basename(imagePath2));
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
      await app.page.keyboard.press('x');

      // Badge should now show the new key
      await expect(badge).toContainText('X');

      // Close settings
      await app.closeSettingsModal();

      // Verify the new shortcut works
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.page.locator('body').click();
      await app.page.keyboard.press('x');
      await app.expectClipCount(0); // Switched to archive view
    });

    test('should reset shortcuts to defaults', async ({ app }) => {
      await app.openSettingsModal();

      // First rebind something
      const badge = app.page.locator(selectors.shortcuts.shortcutBadge('toggle-archive'));
      await badge.scrollIntoViewIfNeeded();
      await badge.click();
      await app.page.keyboard.press('x');
      await expect(badge).toContainText('X');

      // Click reset
      const resetBtn = app.page.locator(selectors.shortcuts.resetButton);
      await resetBtn.scrollIntoViewIfNeeded();
      await resetBtn.click();

      // Badge should show default again
      await expect(badge).toContainText('A');
    });
  });
});
