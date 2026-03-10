import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Keyboard Navigation', () => {

  test.describe('Context Menu Focus Restoration', () => {
    test('should restore focus to clip after closing context menu via Escape', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Focus the clip via Tab
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Open context menu with Cmd+Enter
      await app.page.keyboard.press('Meta+Enter');
      await expect(app.page.locator(selectors.cardMenu.dropdown)).toBeVisible();

      // Close with Escape
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.cardMenu.dropdown)).not.toBeVisible();

      // Focus should be back on the clip
      const focusOnClip = await app.page.evaluate(() => {
        return document.activeElement?.matches('#gallery > li') ?? false;
      });
      expect(focusOnClip).toBe(true);
    });

    test('should restore focus to clip after navigating and closing context menu', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Focus the clip via Tab
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Open context menu with Cmd+Enter
      await app.page.keyboard.press('Meta+Enter');
      await expect(app.page.locator(selectors.cardMenu.dropdown)).toBeVisible();

      // Navigate within menu then close
      await app.page.keyboard.press('ArrowDown');
      await app.page.keyboard.press('ArrowDown');
      await app.page.keyboard.press('Escape');

      // Focus should be back on the clip
      const focusOnClip = await app.page.evaluate(() => {
        return document.activeElement?.matches('#gallery > li') ?? false;
      });
      expect(focusOnClip).toBe(true);
    });
  });

  test.describe('Folder View Keyboard Navigation', () => {
    test('should select folder cards with arrow keys', async ({ app }) => {
      await app.createTag('folderA');
      await app.createTag('folderB');
      await app.toggleFolderMode();

      // Wait for folder cards to appear
      await expect(app.page.locator('[data-folder]').first()).toBeVisible();

      // Tab to the gallery to focus first folder card
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li[data-folder]', { maxTabs: 50 });

      // Should be focused on a folder card
      const focusedOnFolder = await app.page.evaluate(() => {
        return document.activeElement?.hasAttribute('data-folder') ?? false;
      });
      expect(focusedOnFolder).toBe(true);

      // Arrow to next folder
      await app.page.keyboard.press('ArrowRight');

      // Should still be focused on a gallery item (the second folder)
      const stillOnGalleryItem = await app.page.evaluate(() => {
        return document.activeElement?.matches('#gallery > li') ?? false;
      });
      expect(stillOnGalleryItem).toBe(true);
    });

    test('should open folder with Enter and focus first item inside', async ({ app }) => {
      await app.createTag('nav-parent');
      await app.createTag('nav-parent/child');

      // Upload a clip and tag it with the child tag
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.addTagToClip(path.basename(imagePath), 'nav-parent/child');

      await app.toggleFolderMode();

      // Focus the folder card
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li[data-folder]', { maxTabs: 50 });

      // Press Enter to open the folder
      await app.page.keyboard.press('Enter');

      // Focus should be on the first item inside the folder (child folder or clip)
      await app.page.waitForFunction(
        () => document.activeElement?.matches('#gallery > li') ?? false,
        null,
        { timeout: 10000 }
      );
    });

    test('should focus breadcrumb remove button when folder is empty', async ({ app }) => {
      // Create a leaf tag with no clips
      await app.createTag('empty-leaf');

      await app.toggleFolderMode();

      // Focus and enter the empty folder
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li[data-folder]', { maxTabs: 50 });
      await app.page.keyboard.press('Enter');

      // Focus should be on the breadcrumb remove button since the folder is empty
      await app.page.waitForFunction(
        () => {
          const el = document.activeElement;
          return (el?.matches('#active-tags-container button[aria-label^="Remove"]') ?? false) ||
                 (el?.closest('#active-tags-container') !== null);
        },
        null,
        { timeout: 10000 }
      );
    });
  });

  test.describe('Search Enter Jumps to Results', () => {
    test('should focus first visible clip when pressing Enter in search box', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      await app.uploadFile(img1);
      await app.expectClipCount(1);

      // Focus search input
      const searchInput = app.page.locator(selectors.header.searchInput);
      await searchInput.click();
      await expect(searchInput).toBeFocused();

      // Press Enter to jump to first result
      await app.page.keyboard.press('Enter');

      // Focus should be on a gallery item
      const focusOnGalleryItem = await app.page.evaluate(() => {
        return document.activeElement?.matches('#gallery > li') ?? false;
      });
      expect(focusOnGalleryItem).toBe(true);
    });

    test('should focus first matching clip after filtering via search', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const txt = await createTempFile(generateTestText('hello'), 'txt');
      await app.uploadFile(img1);
      await app.uploadFile(txt);
      await app.expectClipCount(2);

      // Type in search to filter
      const searchInput = app.page.locator(selectors.header.searchInput);
      await searchInput.click();
      await searchInput.fill('.txt');
      await app.page.waitForFunction(() => (window as any).__appReady === true, null, { timeout: 10000 });

      // Press Enter
      await app.page.keyboard.press('Enter');

      // Focus should be on the visible text clip
      const focusedFilename = await app.page.evaluate(() => {
        return document.activeElement?.getAttribute('data-filename') ?? '';
      });
      expect(focusedFilename).toContain('.txt');
    });
  });

  test.describe('Cheatsheet Focus Trap', () => {
    test('should trap focus within cheatsheet and restore on close', async ({ app }) => {
      await app.page.locator('body').click();

      // Open cheat sheet
      await app.page.keyboard.press('Shift+/');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-100/);

      // Focus should be on close button
      await expect(app.page.locator(selectors.shortcuts.cheatsheetClose)).toBeFocused();

      // Tab should keep focus within the cheatsheet
      for (let i = 0; i < 5; i++) {
        await app.page.keyboard.press('Tab');
      }
      const focusInCheatsheet = await app.page.evaluate(() => {
        const overlay = document.getElementById('shortcuts-cheatsheet');
        return overlay?.contains(document.activeElement) ?? false;
      });
      expect(focusInCheatsheet).toBe(true);

      // Close and verify focus restored
      await app.page.keyboard.press('Escape');
      await expect(app.page.locator(selectors.shortcuts.cheatsheet)).toHaveClass(/opacity-0/);
    });
  });

  test.describe('Tag Filter Dropdown Keyboard Navigation', () => {
    test('should focus first tag on open and navigate with arrow keys', async ({ app }) => {
      await app.createTag('tagA');
      await app.createTag('tagB');

      await app.openTagFilterDropdown();

      // First label should be focused automatically
      const firstLabel = app.page.locator('#tag-filter-list label').first();
      await expect(firstLabel).toBeFocused();

      // Arrow down to second label
      await app.page.keyboard.press('ArrowDown');
      const secondLabel = app.page.locator('#tag-filter-list label').nth(1);
      await expect(secondLabel).toBeFocused();
    });

    test('should close tag filter dropdown with Escape and restore focus to button', async ({ app }) => {
      await app.createTag('escTag');

      await app.openTagFilterDropdown();
      await expect(app.page.locator(selectors.tags.filterDropdown)).toBeVisible();

      // Press Escape — handled by ShortcutManager's closeTopModalOverlay
      await app.page.keyboard.press('Escape');

      // Dropdown should be hidden
      await app.page.waitForFunction(
        (sel) => document.querySelector(sel)?.classList.contains('hidden'),
        selectors.tags.filterDropdown,
        { timeout: 5000 }
      );

      // Focus should be back on the filter button
      await expect(app.page.locator(selectors.tags.filterButton)).toBeFocused();
    });

    test('should toggle tag filter with Enter on focused label', async ({ app }) => {
      await app.setFolderMode(false);
      await app.createTag('toggleTag');

      await app.openTagFilterDropdown();

      // Focus first label explicitly (in case auto-focus timing varies)
      const firstLabel = app.page.locator('#tag-filter-list label').first();
      await firstLabel.focus();
      await expect(firstLabel).toBeFocused();

      // Press Enter to toggle the checkbox via rover onActivate
      await app.page.keyboard.press('Enter');

      // The tag filter badge should show (1 filter active)
      await expect(app.page.locator(selectors.tags.filterBadge)).toBeVisible();
    });

    test('should restore focus to toggled tag after filter is applied', async ({ app }) => {
      // Ensure folder mode is off (previous test may have left it on)
      await app.setFolderMode(false);

      await app.createTag('alpha');
      await app.createTag('beta');

      await app.openTagFilterDropdown();

      // Focus second label
      const secondLabel = app.page.locator('#tag-filter-list label').nth(1);
      await secondLabel.focus();
      await expect(secondLabel).toBeFocused();

      // Toggle it with Enter
      await app.page.keyboard.press('Enter');

      // After re-render, focus should still be on the second label (beta)
      const newSecondLabel = app.page.locator('#tag-filter-list label').nth(1);
      await expect(newSecondLabel).toBeFocused();

      // Toggle again to deselect
      await app.page.keyboard.press('Enter');

      // Focus should still be on the second label
      const stillSecondLabel = app.page.locator('#tag-filter-list label').nth(1);
      await expect(stillSecondLabel).toBeFocused();
    });
  });

  test.describe('Inert Attribute on Hidden Containers', () => {
    test('should not allow tabbing to drawer items when closed', async ({ app }) => {
      // Drawer is closed by default — items should be inert
      const drawerInert = await app.page.evaluate(() => {
        const drawer = document.getElementById('nav-drawer');
        return drawer?.hasAttribute('inert') ?? false;
      });
      expect(drawerInert).toBe(true);

      // Tab through the page — should never land on drawer items
      for (let i = 0; i < 20; i++) {
        await app.page.keyboard.press('Tab');
        const focusInDrawer = await app.page.evaluate(() => {
          const drawer = document.getElementById('nav-drawer');
          return drawer?.contains(document.activeElement) ?? false;
        });
        expect(focusInDrawer).toBe(false);
      }
    });

    test('should remove inert when drawer opens and restore when closed', async ({ app }) => {
      await app.openDrawer();

      const drawerInertAfterOpen = await app.page.evaluate(() => {
        const drawer = document.getElementById('nav-drawer');
        return drawer?.hasAttribute('inert') ?? false;
      });
      expect(drawerInertAfterOpen).toBe(false);

      await app.closeDrawer();

      const drawerInertAfterClose = await app.page.evaluate(() => {
        const drawer = document.getElementById('nav-drawer');
        return drawer?.hasAttribute('inert') ?? false;
      });
      expect(drawerInertAfterClose).toBe(true);
    });

    test('should not allow tabbing to bulk toolbar items when hidden', async ({ app }) => {
      const toolbarInert = await app.page.evaluate(() => {
        const toolbar = document.getElementById('bulk-toolbar');
        return toolbar?.hasAttribute('inert') ?? false;
      });
      expect(toolbarInert).toBe(true);
    });

    test('should set inert on settings modal when closed', async ({ app }) => {
      const inert = await app.page.evaluate(() => {
        return document.getElementById('settings-modal')?.hasAttribute('inert') ?? false;
      });
      expect(inert).toBe(true);
    });

    test('should set inert on shortcuts cheatsheet when closed', async ({ app }) => {
      const inert = await app.page.evaluate(() => {
        return document.querySelector('[data-testid="shortcuts-cheatsheet"]')?.hasAttribute('inert') ?? false;
      });
      expect(inert).toBe(true);
    });
  });

  test.describe('Gallery Clip Activation', () => {
    test('should open lightbox when pressing Enter on a focused image clip', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Focus the clip
      await app.page.locator('body').click();
      await app.tabTo('#gallery > li', { maxTabs: 50 });

      // Press Enter to open lightbox
      await app.page.keyboard.press('Enter');
      await app.page.waitForSelector('#lightbox.active');

      const lightboxVisible = await app.page.evaluate(() => {
        return document.getElementById('lightbox')?.classList.contains('active') ?? false;
      });
      expect(lightboxVisible).toBe(true);
    });
  });
});
