import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: orphan rows', () => {
  // The seeding test uses Node-side child_process + sqlite3 CLI to insert an
  // orphan plugin_storage row (plugin_id=99999 has no matching plugins row)
  // directly into the app's SQLite DB.  This bypasses the need for a Go
  // backdoor.  Coverage of the Go-side detection logic is also provided by
  // TestGetOrphanDBRows_DetectsPluginStorage in maintenance_test.go.
  test('detects and cleans a seeded orphan plugin_storage row', async ({ app }) => {
    await app.seedOrphanPluginStorage(99999, 'e2e-orphan-key', 'e2e-orphan-value');

    await app.openMaintenanceModal();
    await app.page.locator('#maintenance-orphan-rows-btn').click();
    await app.confirmDialog();

    const toast = await app.waitForToast(/cleaned/i);
    expect(toast).toMatch(/\d+/);
  });

  test('shows "no orphan rows" when none exist', async ({ app }) => {
    await app.openMaintenanceModal();
    await app.page.locator('#maintenance-orphan-rows-btn').click();
    const toast = await app.waitForToast(/no orphan rows/i);
    expect(toast).toBeTruthy();
  });
});
