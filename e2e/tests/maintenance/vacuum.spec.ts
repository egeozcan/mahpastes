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
  });
});
