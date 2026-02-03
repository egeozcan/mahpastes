import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Plugin Toast', () => {
  test('showToast displays info type with default styling', async ({ app }) => {
    // Call showToast directly via evaluate
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Test info message', 'info');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    await expect(toast).toHaveText('Test info message');
    // Check it has stone-800 background (default)
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });

  test('showToast displays success type with green styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Success message', 'success');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasSuccessStyle = await toast.evaluate(el => el.classList.contains('bg-emerald-600'));
    expect(hasSuccessStyle).toBe(true);
  });

  test('showToast displays error type with red styling', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Error message', 'error');
    });

    const toast = app.page.locator('#toast');
    await expect(toast).toBeVisible();
    const hasErrorStyle = await toast.evaluate(el => el.classList.contains('bg-red-600'));
    expect(hasErrorStyle).toBe(true);
  });

  test('showToast defaults to info type when no type provided', async ({ app }) => {
    await app.page.evaluate(() => {
      // @ts-ignore
      showToast('Default message');
    });

    const toast = app.page.locator('#toast');
    const hasInfoStyle = await toast.evaluate(el => el.classList.contains('bg-stone-800'));
    expect(hasInfoStyle).toBe(true);
  });
});
