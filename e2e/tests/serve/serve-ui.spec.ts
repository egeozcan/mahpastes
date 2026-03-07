import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

test.describe('Serve - UI', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should show 3-way view toggle in drawer', async ({ app }) => {
    await app.openDrawer();
    await expect(app.page.locator(selectors.viewTabs.clips)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.watch)).toBeVisible();
    await expect(app.page.locator(selectors.viewTabs.serve)).toBeVisible();
  });

  test('should switch to serve view via tab', async ({ app }) => {
    await app.switchToServeView();
    await expect(app.page.locator(selectors.serve.view)).toBeVisible();
    await expect(app.page.locator('#gallery')).not.toBeVisible();
  });

  test('should switch back to clips via back button', async ({ app }) => {
    await app.switchToServeView();
    await app.page.click(selectors.serve.backBtn);
    await expect(app.page.locator('#gallery')).toBeVisible();
    await expect(app.page.locator(selectors.serve.view)).not.toBeVisible();
  });
});
