import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Settings > About: the stamped version and the opt-out release check. The
 * e2e app runs headless (MAHPASTES_START_HIDDEN=1), where main.go never starts
 * the release checker; toggling the setting there sends no request either
 * (TestReleaseCheckerNeverRequestsBeforeStart), so no test reaches the network.
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
    const banner = app.page.locator(selectors.settings.updateBanner);
    await expect(banner).toContainText('99.0.0');
    await expect(app.page.locator(selectors.settings.updateBannerLink)).toHaveJSProperty('tagName', 'BUTTON');
    await app.page.locator(selectors.settings.updateBannerDismiss).click();
    await expect(banner).toHaveCount(0);

    // A dismissed version stays dismissed.
    await app.page.evaluate(() => {
      (window as any).runtime.EventsEmit('app:update_available', { version: '99.0.0', url: 'https://github.com/egeozcan/mahpastes/releases/tag/v99.0.0' });
    });
    await expect(banner).toHaveCount(0);

    // But the release can still be found: Settings > About names it.
    await app.page.evaluate(() => {
      (window as any).__openedURLs = [];
      (window as any).runtime.BrowserOpenURL = (url: string) => { (window as any).__openedURLs.push(url); };
    });
    await app.openSettingsModal();
    const about = app.page.locator('[data-testid="about-update"]');
    await expect(about).toContainText('Version 99.0.0 is available.');
    await app.page.locator('[data-testid="about-update-link"]').click();
    expect(await app.page.evaluate(() => (window as any).__openedURLs))
      .toEqual(['https://github.com/egeozcan/mahpastes/releases/tag/v99.0.0']);

    // Switching the check off takes the line away with the banner.
    const toggle = app.page.locator('[data-testid="update-check-toggle"]');
    await app.page.locator('[data-testid="update-check-toggle-label"]').click();
    await expect(toggle).not.toBeChecked();
    await expect(about).toBeHidden();
    await app.page.locator('[data-testid="update-check-toggle-label"]').click();
    await expect(toggle).toBeChecked();
  });

  test('the footer button closes Settings and says changes are already saved', async ({ app }) => {
    await app.openSettingsModal();
    await expect(app.page.locator('[data-testid="settings-autosave-note"]')).toHaveText('Changes are saved as you make them.');
    const done = app.page.locator('#settings-save');
    await expect(done).toHaveText('Done');
    await done.click();
    await expect(app.page.locator('#settings-modal')).toHaveClass(/opacity-0/);
    // No "Settings saved" toast for a button that saved nothing.
    await expect(app.page.locator('#toast')).not.toContainText('Settings saved');
  });
});
