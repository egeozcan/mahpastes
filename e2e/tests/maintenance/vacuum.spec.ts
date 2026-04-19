import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: compact database', () => {
  test('runs VACUUM + ANALYZE and reports reclaimed space', async ({ app }) => {
    await app.openMaintenanceModal();
    const compactBtn = app.page.locator('#maintenance-compact-db-btn');
    await compactBtn.click();

    // Confirm the dialog is themed as non-destructive (primary variant):
    // stone-themed icon and a "Compact" button, not the red "Delete" default.
    // SVG className is read-only in browsers, so we verify the class attribute
    // directly to catch the regression where only the button got restyled.
    const iconSvg = app.page.locator('#confirm-icon-svg');
    await expect(iconSvg).toHaveClass(/text-stone-600/);
    await expect(iconSvg).not.toHaveClass(/text-red-500/);
    await expect(app.page.locator('#confirm-yes-btn')).toHaveText('Compact');

    await app.confirmDialog();

    const toast = await app.waitForToast(/reclaimed|was/i);
    expect(toast).toMatch(/bytes|KB|MB|B\b/);
    // Guard against a regression where Wails v2 silently drops multi-return
    // values and the toast reports "was 0 B, now 0 B". The seeded test DB
    // always has a nonzero size.
    const match = toast.match(/was\s+([\d.]+)\s*(B|KB|MB|GB)/i);
    expect(match).not.toBeNull();
    expect(Number(match![1])).toBeGreaterThan(0);
    // Reclaimed should never be NaN — catch regressions in formatFileSize
    // (e.g., Math.log of a negative number when the DB grew transiently).
    expect(toast).not.toMatch(/NaN|undefined/i);
  });
});
