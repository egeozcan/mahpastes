import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: compact database', () => {
  test('runs VACUUM + ANALYZE and reports reclaimed space', async ({ app }) => {
    await app.openMaintenanceModal();
    const compactBtn = app.page.locator('#maintenance-compact-db-btn');
    await compactBtn.click();
    await app.confirmDialog();

    const toast = await app.waitForToast(/reclaimed|was/i);
    expect(toast).toMatch(/bytes|KB|MB|B\b/);
    // Guard against a regression where Wails v2 silently drops multi-return
    // values and the toast reports "was 0 B, now 0 B". The seeded test DB
    // always has a nonzero size.
    const match = toast.match(/was\s+([\d.]+)\s*(B|KB|MB|GB)/i);
    expect(match).not.toBeNull();
    expect(Number(match![1])).toBeGreaterThan(0);
  });
});
