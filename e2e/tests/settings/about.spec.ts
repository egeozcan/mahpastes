import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Settings > About: the stamped version and the opt-out release check. The
 * e2e app runs headless (MAHPASTES_START_HIDDEN=1), where main.go never starts
 * the release check, so no test makes a network request.
 */
test.describe('Settings - about and release check', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(async () => {
      await (window as any).go.main.AppInfoService.SetUpdateCheckEnabled(true);
    });
    const modal = app.page.locator(selectors.settings.modal);
    if (await modal.evaluate((el) => el.classList.contains('opacity-100'))) {
      await app.closeSettingsModal();
    }
  });

  test('shows the app version', async ({ app }) => {
    await app.openSettingsModal();
    await expect(app.page.locator(selectors.settings.aboutSection)).toBeVisible();
    await expect(app.page.locator(selectors.settings.aboutVersion)).toHaveText(/^Version \S+/);
    await expect(app.page.locator(selectors.settings.aboutVersion)).not.toHaveText('Version unknown');
  });

  test('the release check is on by default and can be switched off', async ({ app }) => {
    await app.openSettingsModal();
    const toggle = app.page.locator(selectors.settings.updateCheckToggle);
    await expect(toggle).toBeChecked();

    await app.page.locator(selectors.settings.updateCheckLabel).click();
    await expect(toggle).not.toBeChecked();
    await expect
      .poll(() => app.page.evaluate(() => (window as any).go.main.App.GetSetting('update_check')))
      .toBe('false');

    await app.closeSettingsModal();
    await app.openSettingsModal();
    await expect(toggle).not.toBeChecked();
  });

  test('a found release shows a dismissible notice', async ({ app }) => {
    await app.page.evaluate(() => {
      localStorage.removeItem('mahpastes.dismissedUpdate');
      // The Wails JS runtime notifies local listeners on emit, like the backend does.
      (window as any).runtime.EventsEmit('app:update_available', { version: '99.0.0', url: 'https://github.com/egeozcan/mahpastes/releases/tag/v99.0.0' });
    });
    const banner = app.page.locator('[data-testid="update-banner"]');
    await expect(banner).toContainText('99.0.0');
    await app.page.locator('[data-testid="update-banner-dismiss"]').click();
    await expect(banner).toHaveCount(0);
  });
});
