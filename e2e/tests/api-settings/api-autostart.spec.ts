import { test, expect } from '../../fixtures/test-fixtures';
import type { AppHelper } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * "Start on launch" in the REST API modal. The launch itself (main.go
 * OnStartup) is covered by the Go tests for App.StartAPIOnLaunch; here we
 * check the switch persists the choice together with the port it will use.
 */
test.describe('API Settings - start on launch', () => {
  // The fixture's page and data dir are worker-scoped: put the setting back
  // and close the modal so neither leaks into the next test.
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(async () => {
      const go = (window as any).go.main;
      await go.APIService.SetAPIAutostart(false, 44557);
      await go.App.SetSetting('api_port', '44557');
    });
    const modal = app.page.locator(selectors.api.modal);
    if (await modal.evaluate((el) => el.classList.contains('opacity-100'))) {
      await closeApiModal(app);
    }
  });

  async function openApiModal(app: AppHelper) {
    await app.openDrawer();
    await app.page.locator(selectors.api.openButton).click();
    await app.page.waitForSelector(`${selectors.api.modal}.opacity-100`, { timeout: 5000 });
  }

  async function closeApiModal(app: AppHelper) {
    await app.page.locator(selectors.api.closeButton).click();
    await app.page.waitForSelector(`${selectors.api.modal}.opacity-0`, { timeout: 5000 });
  }

  function getSetting(app: AppHelper, key: string): Promise<string> {
    return app.page.evaluate((k) => (window as any).go.main.App.GetSetting(k), key);
  }

  test('persists the choice and the port shown above it', async ({ app }) => {
    await openApiModal(app);
    const toggle = app.page.locator(selectors.api.autostartToggle);
    const portInput = app.page.locator(selectors.api.portInput);
    await expect(toggle).not.toBeChecked();

    await portInput.fill('45123');
    await app.page.locator(selectors.api.autostartLabel).click();
    await expect(toggle).toBeChecked();
    await expect.poll(() => getSetting(app, 'api_autostart')).toBe('true');
    await expect.poll(() => getSetting(app, 'api_port')).toBe('45123');

    // Reopening shows the saved choice and the port the next launch uses,
    // whatever the field held before (set without a change event, so nothing
    // is saved).
    await portInput.evaluate((el: HTMLInputElement) => { el.value = '1'; });
    await closeApiModal(app);
    await openApiModal(app);
    await expect(toggle).toBeChecked();
    await expect(portInput).toHaveValue('45123');

    // Editing the port while it is on moves the launch port with it.
    await portInput.fill('45124');
    await portInput.blur();
    await expect.poll(() => getSetting(app, 'api_port')).toBe('45124');

    await app.page.locator(selectors.api.autostartLabel).click();
    await expect(toggle).not.toBeChecked();
    await expect.poll(() => getSetting(app, 'api_autostart')).toBe('false');
  });

  test('refuses an out-of-range port and stays off', async ({ app }) => {
    await openApiModal(app);
    const toggle = app.page.locator(selectors.api.autostartToggle);

    await app.page.locator(selectors.api.portInput).fill('70000');
    await app.page.locator(selectors.api.autostartLabel).click();

    await expect(app.page.locator(selectors.toast.container)).toContainText('invalid port');
    await expect(toggle).not.toBeChecked();
    expect(await getSetting(app, 'api_autostart')).not.toBe('true');
  });

  // These used to be saved as some other port (44557 for 0 or an empty field,
  // 1 for "1e3", 80 for "80.5") while the field kept showing what was typed.
  for (const text of ['0', '', '1e3', '80.5']) {
    test(`refuses port "${text}" instead of saving a different one`, async ({ app }) => {
      await openApiModal(app);
      const toggle = app.page.locator(selectors.api.autostartToggle);
      const portInput = app.page.locator(selectors.api.portInput);
      const before = await getSetting(app, 'api_port');

      // fill() refuses exponent text on a number input; set it directly.
      await portInput.evaluate((el: HTMLInputElement, v) => { el.value = v; }, text);
      await app.page.locator(selectors.api.autostartLabel).click();

      await expect(app.page.locator(selectors.toast.container)).toContainText('invalid port');
      await expect(toggle).not.toBeChecked();
      expect(await getSetting(app, 'api_autostart')).not.toBe('true');
      expect(await getSetting(app, 'api_port')).toBe(before);
    });
  }

  test('an invalid port typed while it is on leaves the saved port alone', async ({ app }) => {
    await openApiModal(app);
    const toggle = app.page.locator(selectors.api.autostartToggle);
    const portInput = app.page.locator(selectors.api.portInput);

    await portInput.fill('45123');
    await app.page.locator(selectors.api.autostartLabel).click();
    await expect.poll(() => getSetting(app, 'api_port')).toBe('45123');

    await portInput.fill('0');
    await portInput.blur();
    await expect(app.page.locator(selectors.toast.container)).toContainText('invalid port');
    await expect(toggle).toBeChecked();
    expect(await getSetting(app, 'api_port')).toBe('45123');
    expect(await getSetting(app, 'api_autostart')).toBe('true');
  });
});
