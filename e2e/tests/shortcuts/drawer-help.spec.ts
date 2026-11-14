import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

// The cheat sheet and the documentation were reachable only from the macOS
// menu bar (and '?', which nothing in the window mentioned). Windows and Linux
// have no menu bar at all.
test.describe('Help entries in the nav drawer', () => {
  const sheet = (app: any) => app.page.locator('#shortcuts-cheatsheet');

  test('Keyboard Shortcuts opens the cheat sheet over a closed drawer and names its key', async ({ app }) => {
    await app.openDrawer();
    const button = app.page.locator(selectors.drawer.shortcutsButton);
    await expect(button.locator('kbd')).toHaveText('?');

    await button.click();
    await expect(sheet(app)).toHaveClass(/opacity-100/);
    await expect(app.page.locator(selectors.drawer.panel)).toHaveClass(/translate-x-full/);

    await app.page.keyboard.press('Escape');
    await expect(sheet(app)).toHaveClass(/opacity-0/);
  });

  test('the key badge follows a rebinding', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore - app global
      ShortcutManager.setOverride('show-cheatsheet', 'shift+h');
    });
    await app.openDrawer();
    await expect(app.page.locator(selectors.drawer.shortcutsButton).locator('kbd')).not.toHaveText('?');
    await app.page.evaluate(() => {
      // @ts-ignore - app global
      ShortcutManager.resetAllToDefaults();
    });
    await expect(app.page.locator(selectors.drawer.shortcutsButton).locator('kbd')).toHaveText('?');
  });

  test('Documentation opens the docs site and closes the drawer', async ({ app }) => {
    await app.page.evaluate(() => {
      (window as any).__openedURLs = [];
      (window as any).runtime.BrowserOpenURL = (url: string) => { (window as any).__openedURLs.push(url); };
    });
    await app.openDrawer();
    await app.page.locator(selectors.drawer.docsButton).click();
    expect(await app.page.evaluate(() => (window as any).__openedURLs))
      .toEqual(['https://egeozcan.github.io/mahpastes/']);
    await expect(app.page.locator(selectors.drawer.panel)).toHaveClass(/translate-x-full/);
  });
});
