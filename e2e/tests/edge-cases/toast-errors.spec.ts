import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

// An error toast carries the reason for a failure and is usually the only
// record of it. It used to vanish after 3 seconds like any other toast, with
// no way to keep it or put it away.
test.describe('Error toasts', () => {
  const toast = (app: any) => app.page.locator(selectors.toast.container);
  const show = (app: any, message: string, type: string) =>
    app.page.evaluate(({ message, type }: any) => (window as any).showToast(message, type), { message, type });

  test('an error outlasts an ordinary toast and is announced as an alert', async ({ app }) => {
    await show(app, 'Saved.', 'info');
    await expect(toast(app)).toHaveAttribute('role', 'status');
    await expect(toast(app)).toHaveAttribute('aria-live', 'polite');
    await expect(app.page.locator(selectors.toast.dismiss)).toHaveCount(0);

    await show(app, 'Failed to restore backup: disk full', 'error');
    await expect(toast(app)).toHaveAttribute('role', 'alert');
    await expect(toast(app)).toHaveAttribute('aria-live', 'assertive');
    // The dismiss control is an icon: the toast's text is still the message.
    await expect(toast(app)).toHaveText('Failed to restore backup: disk full');

    await app.page.waitForTimeout(3600);
    await expect(toast(app)).toHaveClass(/opacity-100/);
  });

  test('the dismiss button puts an error away', async ({ app }) => {
    await show(app, 'Failed to import locked.pdf: permission denied', 'error');
    const dismiss = app.page.locator(selectors.toast.dismiss);
    await expect(dismiss).toHaveAttribute('aria-label', 'Dismiss');
    await dismiss.click();
    await expect(toast(app)).toHaveClass(/opacity-0/);
    await expect(dismiss).toHaveCount(0);
  });

  test('a toast stays while the pointer is on its button, and its body lets clicks through', async ({ app }) => {
    await app.page.evaluate(() => (window as any).showToast('Clip archived.', 'info', { label: 'Undo', onClick: () => {} }));
    await app.page.locator(selectors.toast.action).hover();
    await app.page.waitForTimeout(8600);
    await expect(toast(app)).toHaveClass(/opacity-100/);

    await app.page.mouse.move(5, 5);
    await expect(toast(app)).toHaveClass(/opacity-0/, { timeout: 5000 });

    // The body does not take clicks: what is under the toast stays reachable.
    await show(app, 'A message over the bottom-right corner of the page', 'info');
    const through = await app.page.evaluate(() => {
      const r = document.getElementById('toast')!.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + 10, r.top + r.height / 2);
      return !!hit && hit.id !== 'toast' && !document.getElementById('toast')!.contains(hit);
    });
    expect(through).toBe(true);
  });
});
