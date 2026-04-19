import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: stale file sweep', () => {
  test('shows "nothing to clean" when none are stale', async ({ app }) => {
    await app.openMaintenanceModal();
    await app.page.locator('#maintenance-stale-files-btn').click();
    const toast = await app.waitForToast(/no stale files/i);
    expect(toast).toBeTruthy();
  });

  // The seeding test uses Node-side fs.utimesSync to place a backdated temp file
  // in the app's clip_temp_files directory, bypassing the need for a Go backdoor.
  // Coverage of the Go-side detection logic is also provided by the unit test
  // TestGetStaleFiles_DetectsOldTempFiles in maintenance_test.go.
  test('detects and removes a seeded stale temp file', async ({ app }) => {
    await app.seedStaleTempFile('stale-e2e.bin', 100);

    await app.openMaintenanceModal();
    await app.page.locator('#maintenance-stale-files-btn').click();
    await app.confirmDialog();

    const toast = await app.waitForToast(/removed/i);
    // Toast reads "Removed N file(s) (bytes)" — verify the seeded file was
    // counted. A regression where Wails drops multi-return values would
    // report "Removed 0 files (0 B)" — guard against that explicitly.
    const match = toast.match(/Removed\s+(\d+)\s+file/i);
    expect(match).not.toBeNull();
    expect(Number(match![1])).toBeGreaterThanOrEqual(1);
  });
});
