import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * Background & Startup settings, the drawer's Quit entry and the editor's
 * unsaved-changes report that quitting asks about.
 *
 * Nothing here may quit the app or register it with the OS: QuitApp is never
 * called, and SetLaunchAtLogin is replaced in the page so no real login entry
 * is written to the machine running the tests.
 */
test.describe('App Lifecycle', () => {
  const section = '[data-testid="desktop-lifecycle-settings"]';
  const keepToggle = '[data-testid="keep-running-toggle"]';
  const keepLabel = '[data-testid="keep-running-label"]';
  const keepRestart = '[data-testid="keep-running-restart"]';
  const loginBlock = '[data-testid="launch-at-login-block"]';
  const loginLabel = '[data-testid="launch-at-login-label"]';
  const loginToggle = '[data-testid="launch-at-login-toggle"]';
  const hiddenToggle = '[data-testid="launch-hidden-toggle"]';
  const hiddenNote = '[data-testid="launch-hidden-note"]';
  const loginStatus = '[data-testid="launch-at-login-status"]';
  const drawerQuit = '[data-testid="drawer-quit"]';
  const quitButton = '[data-testid="quit-app-btn"]';

  // Replaces the login-entry bindings with an in-page fake that remembers
  // every call. The real GetDesktopLifecycle still answers for keep running.
  async function fakeLaunchAtLogin(app: any, initial: Record<string, unknown> = {}) {
    await app.page.evaluate((start: Record<string, unknown>) => {
      const w = window as any;
      const App = w.go.main.App;
      const realGet = App.GetDesktopLifecycle;
      const login = {
        supported: true, enabled: false, hidden: false, target: '', current: '/opt/mahpastes',
        target_missing: false, other_copy: false, disabled_by_system: false, location: '/fake/autostart',
        ...start,
      };
      w.__launchAtLoginCalls = [];
      App.GetDesktopLifecycle = async () => ({ ...(await realGet()), launch_at_login: { ...login }, launch_at_login_error: '' });
      App.SetLaunchAtLogin = async (enabled: boolean, hidden: boolean) => {
        w.__launchAtLoginCalls.push([enabled, hidden]);
        Object.assign(login, { enabled, hidden, target: enabled ? login.current : '' });
        return { ...login };
      };
    }, initial);
  }

  const launchCalls = (app: any) => app.page.evaluate(() => (window as any).__launchAtLoginCalls);

  test('keep running when closed is stored and applies on next start', async ({ app }) => {
    await app.openSettingsModal();
    await expect(app.page.locator(section)).toBeVisible();
    const startedWith = await app.page.locator(keepToggle).isChecked();

    try {
      await app.page.locator(keepLabel).click();
      await expect(app.page.locator(keepToggle)).toBeChecked({ checked: !startedWith });
      // This window still closes the way it started.
      await expect(app.page.locator(keepRestart)).toBeVisible();

      const stored = await app.page.evaluate(async () =>
        (await (window as any).go.main.App.GetDesktopLifecycle()).keep_running_when_closed);
      expect(stored).toBe(!startedWith);

      // Reopening settings reads the stored choice back.
      await app.closeSettingsModal();
      await app.openSettingsModal();
      await expect(app.page.locator(keepToggle)).toBeChecked({ checked: !startedWith });
    } finally {
      await app.page.evaluate((keep: boolean) =>
        (window as any).go.main.App.SetKeepRunningWhenClosed(keep), startedWith);
    }
    await app.closeSettingsModal();
    await app.openSettingsModal();
    await expect(app.page.locator(keepRestart)).toBeHidden();
  });

  test('launch at login turns on hidden by default and can be changed', async ({ app }) => {
    await fakeLaunchAtLogin(app);
    await app.openSettingsModal();

    await expect(app.page.locator(loginBlock)).toBeVisible();
    await expect(app.page.locator(loginToggle)).not.toBeChecked();
    await expect(app.page.locator(hiddenToggle)).toBeDisabled();

    await app.page.locator(loginLabel).click();
    await expect(app.page.locator(loginToggle)).toBeChecked();
    await expect(app.page.locator(hiddenToggle)).toBeEnabled();
    await expect(app.page.locator(hiddenToggle)).toBeChecked();
    await expect(app.page.locator(hiddenNote)).toBeVisible();
    expect(await launchCalls(app)).toEqual([[true, true]]);

    await app.page.locator('[data-testid="launch-hidden-label"]').click();
    await expect(app.page.locator(hiddenToggle)).not.toBeChecked();
    await expect(app.page.locator(hiddenNote)).toBeHidden();

    await app.page.locator(loginLabel).click();
    await expect(app.page.locator(loginToggle)).not.toBeChecked();
    expect(await launchCalls(app)).toEqual([[true, true], [true, false], [false, false]]);
  });

  test('a login entry for another copy is reported, not rewritten', async ({ app }) => {
    await fakeLaunchAtLogin(app, { enabled: true, hidden: true, target: '/old/mahpastes', other_copy: true });
    await app.openSettingsModal();

    await expect(app.page.locator(loginToggle)).toBeChecked();
    await expect(app.page.locator(loginStatus)).toBeVisible();
    await expect(app.page.locator(loginStatus)).toContainText('/old/mahpastes');
    expect(await launchCalls(app)).toEqual([]);
  });

  test('an entry switched off by the system shows as off with an explanation', async ({ app }) => {
    await fakeLaunchAtLogin(app, { enabled: false, disabled_by_system: true, target: '/opt/mahpastes' });
    await app.openSettingsModal();

    await expect(app.page.locator(loginToggle)).not.toBeChecked();
    await expect(app.page.locator(loginStatus)).toContainText('switched off');
  });

  test('the drawer offers Quit and the shortcut is listed', async ({ app }) => {
    await app.openDrawer();
    await expect(app.page.locator(drawerQuit)).toBeVisible();
    await expect(app.page.locator(quitButton)).toHaveText(/Quit mahpastes/);
    // Never clicked: it would end the app under test.
    await app.closeDrawer();

    await app.openSettingsModal();
    await expect(app.page.locator(selectors.shortcuts.shortcutRow('quit-app'))).toContainText('Quit mahpastes');
  });

  test('the editor reports unsaved changes so quitting can ask', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      const App = w.go.main.App;
      const real = App.SetUnsavedChanges;
      w.__unsavedReports = [];
      App.SetUnsavedChanges = (dirty: boolean) => { w.__unsavedReports.push(dirty); return real(dirty); };
    });
    const reports = () => app.page.evaluate(() => (window as any).__unsavedReports);

    const textPath = await createTempFile('original text', 'txt');
    const filename = path.basename(textPath);
    await app.uploadFile(textPath);
    await app.openTextEditor(filename);

    await app.setTextEditorContent('edited');
    await expect.poll(reports).toContain(true);

    // Back to the saved text: nothing to ask about.
    await app.setTextEditorContent('original text');
    await expect.poll(async () => (await reports()).at(-1)).toBe(false);

    await app.setTextEditorContent('edited again');
    await expect.poll(async () => (await reports()).at(-1)).toBe(true);

    // Saving clears it.
    await app.page.locator(selectors.textEditor.saveButton).click();
    await expect.poll(async () => (await reports()).at(-1)).toBe(false);
  });
});
