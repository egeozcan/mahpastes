import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * The macOS menu bar (app_menu.go) emits "menu:action"; app-info.js handles
 * it. Playwright cannot click a native menu, so these drive the same handler
 * through window.__handleMenuAction.
 *
 * A menu bar item works from every main view (the gallery's single-letter
 * keys do not), and never acts behind a dialog or a full-screen viewer.
 */
test.describe('Menu bar actions', () => {
  const menu = (app: any, id: string) =>
    app.page.evaluate((menuId: string) => (window as any).__handleMenuAction(menuId), id);
  // The gallery list has no size while empty, so check the section that
  // switchView shows and hides instead of the list's own visibility.
  const expectGalleryShown = (app: any) =>
    expect(app.page.locator('#gallery').locator('..')).not.toHaveClass(/(^|\s)hidden(\s|$)/);

  test.afterEach(async ({ app }) => {
    // Leave the worker's page in the plain gallery for the next test.
    await app.page.evaluate(() => {
      const w = window as any;
      if (typeof w.switchView === 'function') w.switchView('clips');
    });
  });

  // Share, the REST API dialog and Maintenance had no menu item and no
  // rebindable action: the nav drawer was the only way to them.
  test('View > Share, REST API and Maintenance open from any main view', async ({ app }) => {
    await app.switchToServeView();
    await menu(app, 'share');
    await expect(app.page.locator('#share-view')).toBeVisible();
    await expect(app.page.locator('[data-testid="share-intro"]')).toContainText('peer to peer');

    await menu(app, 'api');
    await expect(app.page.locator('[data-testid="api-modal"]')).toHaveClass(/opacity-100/);
    await app.page.keyboard.press('Escape');
    await expect(app.page.locator('[data-testid="api-modal"]')).toHaveClass(/opacity-0/);

    await menu(app, 'maintenance');
    await expect(app.page.locator('[data-testid="maintenance-modal"]')).toHaveClass(/opacity-100/);
    await app.page.keyboard.press('Escape');

    // The three are rebindable actions too, listed with the other navigation keys.
    const ids = await app.page.evaluate(() => {
      // @ts-ignore - app global
      const actions = ShortcutManager.actions;
      return ['open-share', 'open-api', 'open-maintenance'].map((id) => actions.has(id) && actions.get(id).category);
    });
    expect(ids).toEqual(['navigation', 'navigation', 'navigation']);
  });

  test('View > Watch Folders switches from the Serve view, and View > Serve toggles back', async ({ app }) => {
    await app.switchToServeView();

    await menu(app, 'watch');
    await expect(app.page.locator(selectors.watch.view)).toBeVisible();
    await expect(app.page.locator(selectors.serve.view)).toBeHidden();

    await menu(app, 'serve');
    await expect(app.page.locator(selectors.serve.view)).toBeVisible();

    await menu(app, 'serve');
    await expect(app.page.locator(selectors.serve.view)).toBeHidden();
    await expectGalleryShown(app);
  });

  test('View > Folder Mode from the Watch view returns to the gallery in folder mode', async ({ app }) => {
    await app.openWatchView();

    await menu(app, 'folder-mode');
    await expect(app.page.locator(selectors.watch.view)).toBeHidden();
    await expectGalleryShown(app);
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'true');

    // Already on: picking it from another view keeps folder mode.
    await app.openWatchView();
    await menu(app, 'folder-mode');
    await expectGalleryShown(app);
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'true');

    // From the gallery it is the plain toggle.
    await menu(app, 'folder-mode');
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'false');
  });

  test('View > Archive from the Serve view shows the archive', async ({ app }) => {
    await app.switchToServeView();

    await menu(app, 'archive');
    await expect(app.page.locator(selectors.serve.view)).toBeHidden();
    await expectGalleryShown(app);
    await expect(app.page.locator(selectors.header.archiveButton)).toHaveAttribute('aria-pressed', 'true');

    await menu(app, 'archive');
    await expect(app.page.locator(selectors.header.archiveButton)).toHaveAttribute('aria-pressed', 'false');
  });

  test('File > Settings… closes an open drawer and opens Settings', async ({ app }) => {
    await app.openDrawer();

    await menu(app, 'settings');
    await expect(app.page.locator(selectors.drawer.panel)).toHaveClass(/translate-x-full/);
    await expect(app.page.locator(selectors.settings.modal)).toHaveClass(/opacity-100/);

    await app.closeSettingsModal();
  });

  test('menu items do nothing while a dialog is in front', async ({ app }) => {
    await app.openSettingsModal();

    for (const id of ['watch', 'serve', 'archive', 'folder-mode', 'plugins', 'import-folder']) {
      await menu(app, id);
    }
    await expect(app.page.locator(selectors.watch.view)).toBeHidden();
    await expect(app.page.locator(selectors.serve.view)).toBeHidden();
    await expect(app.page.locator(selectors.plugins.modal)).not.toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.header.archiveButton)).toHaveAttribute('aria-pressed', 'false');
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'false');

    await app.closeSettingsModal();
  });
});
