import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

test.describe('View Switch Controls Visibility', () => {
  test('should hide clip controls in watch view', async ({ app }) => {
    // Controls visible in clips view
    await expect(app.page.locator('#clip-controls')).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeVisible();

    // Switch to watch view
    await app.openWatchView();

    // Controls should be hidden
    await expect(app.page.locator('#clip-controls')).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeHidden();
  });

  test('should restore clip controls when returning to clips view', async ({ app }) => {
    await app.openWatchView();
    await app.closeWatchView();

    await expect(app.page.locator('#clip-controls')).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeVisible();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeVisible();
  });

  test('should hide clip controls in serve view', async ({ app }) => {
    await app.switchToServeView();

    await expect(app.page.locator('#clip-controls')).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.addButton)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.expirySelect)).toBeHidden();
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toBeHidden();
  });

  test('should hide active tags bar when switching away with filters active', async ({ app }) => {
    // Create a tag and apply it as a filter so active-tags-container becomes visible
    await app.createTag('filter-test');
    await app.filterByTag('filter-test');
    await expect(app.page.locator('#active-tags-container')).toBeVisible();

    // Switch to watch view — active tags bar should hide
    await app.openWatchView();
    await expect(app.page.locator('#active-tags-container')).toBeHidden();

    // Return to clips — active tags bar should reappear (filter still active)
    await app.closeWatchView();
    await expect(app.page.locator('#active-tags-container')).toBeVisible();
  });

  test('should close sort popover when switching views', async ({ app }) => {
    // Open the sort popover
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();

    // Switch to watch view — popover should close
    await app.openWatchView();
    await expect(app.page.locator(selectors.sort.popover)).toBeHidden();
  });
});
