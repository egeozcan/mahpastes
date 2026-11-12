import { test, expect } from '../../fixtures/test-fixtures';
import type { AppHelper } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * "System Notifications" in Settings. The e2e app runs with a hidden window
 * (MAHPASTES_START_HIDDEN=1), where main.go never creates the notifier, so no
 * desktop notification can appear during a test run. Coalescing, focus
 * gating and the OS backend are covered by the Go tests in internal/osnotify;
 * here we check the switch defaults to off, persists, and explains why it
 * cannot notify in this run.
 */
test.describe('Settings - system notifications', () => {
  // The fixture's page and data dir are worker-scoped: put the setting back
  // and close the modal so neither leaks into the next test.
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(async () => {
      await (window as any).go.main.NotificationService.SetNotificationsEnabled(false);
    });
    const modal = app.page.locator(selectors.settings.modal);
    if (await modal.evaluate((el) => el.classList.contains('opacity-100'))) {
      await app.closeSettingsModal();
    }
  });

  function getSetting(app: AppHelper): Promise<string> {
    return app.page.evaluate(() => (window as any).go.main.App.GetSetting('os_notifications'));
  }

  test('is off by default and persists when switched on', async ({ app }) => {
    await app.openSettingsModal();
    const section = app.page.locator(selectors.settings.osNotificationsSection);
    const toggle = app.page.locator(selectors.settings.osNotificationsToggle);
    await expect(section).toBeVisible();
    await expect(toggle).not.toBeChecked();
    expect(await getSetting(app)).not.toBe('true');

    await app.page.locator(selectors.settings.osNotificationsLabel).click();
    await expect(toggle).toBeChecked();
    await expect.poll(() => getSetting(app)).toBe('true');

    // Headless run: no notifier, said plainly, and no test button.
    await expect(app.page.locator(selectors.settings.osNotificationsStatus)).toContainText('not available in this run');
    await expect(app.page.locator(selectors.settings.osNotificationsTest)).toBeHidden();

    await app.closeSettingsModal();
    await app.openSettingsModal();
    await expect(toggle).toBeChecked();

    await app.page.locator(selectors.settings.osNotificationsLabel).click();
    await expect(toggle).not.toBeChecked();
    await expect.poll(() => getSetting(app)).toBe('false');
  });
});
