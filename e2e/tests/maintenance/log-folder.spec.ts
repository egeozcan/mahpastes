import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs';
import * as path from 'path';

test.describe('Maintenance: log folder', () => {
  test('the app keeps a log file in the data directory and offers to open its folder', async ({ app }) => {
    // The log is opened at startup, before the window exists.
    expect(app.dataDir).not.toBe('');
    const logFile = path.join(app.dataDir, 'logs', 'mahpastes.log');
    expect(fs.existsSync(logFile)).toBe(true);

    await app.openMaintenanceModal();
    await expect(app.page.locator(selectors.maintenance.logSection)).toBeVisible();
    // Not clicked: it would launch the runner's file manager.
    await expect(app.page.locator(selectors.maintenance.openLogsButton)).toBeVisible();
    await expect(app.page.locator(selectors.maintenance.openLogsButton)).toHaveText(/Open Log Folder/);
  });
});
