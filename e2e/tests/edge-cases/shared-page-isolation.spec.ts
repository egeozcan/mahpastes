import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Every test on a worker shares one page. The fixture's reset must undo what
 * a test leaves on it: replaced global functions and Wails bindings (a
 * showToast spy that dropped the action argument once stripped the button
 * from a later test's toast), and a hidden toast's leftover text.
 */
test.describe('shared page isolation', () => {
  test.describe.configure({ mode: 'serial' });

  test('a test that leaves stubs and a toast behind', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      w.showToast = (message: string) => { document.getElementById('toast')!.textContent = message; };
      w.go.main.App.SaveClipToFile = async () => '/leaked/stub';
      w.runtime.BrowserOpenURL = () => {};
      URL.revokeObjectURL = () => {};
      w.showToast('Leftover toast');
    });
    await expect(app.page.locator(selectors.toast.message)).toHaveText('Leftover toast');
  });

  test('the next test gets the real functions and an empty toast', async ({ app }) => {
    const state = await app.page.evaluate(() => {
      const w = window as any;
      return {
        toastText: document.getElementById('toast')!.textContent,
        save: String(w.go.main.App.SaveClipToFile),
        browserOpen: String(w.runtime.BrowserOpenURL),
        revoke: String(URL.revokeObjectURL),
      };
    });
    expect(state.toastText).toBe('');
    expect(state.save).not.toContain('/leaked/stub');
    expect(state.browserOpen).not.toBe('() => {}');
    expect(state.revoke).toContain('[native code]');

    // The real showToast: it shows the toast and keeps an action's button.
    await app.page.evaluate(() => {
      (window as any).showToast('Restored toast', 'success', { label: 'Act', onClick: () => {} });
    });
    await app.expectToast('Restored toast');
    await expect(app.page.locator(selectors.toast.action)).toHaveText('Act');
  });

  test('a toast assertion does not match a hidden toast', async ({ app }) => {
    // A hidden toast keeps its text and, faded out off-screen, still has a
    // box Playwright counts as visible; expectToast must want it shown.
    await app.page.evaluate(() => {
      const toast = document.getElementById('toast')!;
      toast.textContent = 'Hidden toast';
      toast.classList.remove('translate-x-0', 'opacity-100');
      toast.classList.add('translate-x-full', 'opacity-0');
    });
    await expect(app.page.locator(selectors.toast.message)).toBeVisible();
    await expect(app.expectToast('Hidden toast')).rejects.toThrow();
    await expect(app.waitForToast(/Hidden toast/)).rejects.toThrow();
  });
});
