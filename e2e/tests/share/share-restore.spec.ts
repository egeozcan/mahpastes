/**
 * share-restore.spec.ts
 *
 * Tests the backup+restore flow for shares with both identity policies.
 *
 * WHAT IS TESTED:
 *   "takeover" path:
 *     - Backup an app that has an active share.
 *     - Restore the backup with identityPolicy="takeover".
 *     - After app restart the restored share row has status "active"
 *       (identity was adopted from backup).
 *
 *   "keep" path:
 *     - Backup an app that has an active share.
 *     - Restore the backup with identityPolicy="keep".
 *     - After app restart the restored share row has status "invalid"
 *       (shares came from a different peer id; the UI renders
 *       "(Re-share needed)" badge for invalid shares).
 *
 * NOTE: GetShareStatus() reads from the in-memory publications map (not the
 * DB). After a ConfirmRestoreBackup call the ShareManager is NOT reloaded —
 * the app must restart before the restored share rows become visible via
 * GetShareStatus. Both tests therefore restart after restore.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import * as path from 'path';

// Run serially: each test restarts its Wails instance. Parallel restarts
// contend on the shared build/ directory and cause compilation failures.
test.describe.configure({ mode: 'serial' });

test.describe('Share - Backup and restore', () => {

  // Each test restarts the app; give plenty of headroom.
  test.setTimeout(300000);

  test('takeover policy keeps restored share status=active', async ({ app, tempDir }, testInfo) => {
    // Create a tag, start a share, note the share string.
    await app.createTag('restore-takeover-tag');
    const { shareString, tagID } = await app.startShare('restore-takeover-tag');
    expect(shareString).toMatch(/^mp-share:v1:/);

    // Create a backup.
    const backupPath = path.join(tempDir, 'takeover-backup.zip');
    await app.page.evaluate(async (p) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.CreateBackup(p);
    }, backupPath);

    // Restore with "takeover" — the backup's identity is adopted.
    // (This overwrites all DB data with the backup's data.)
    const result = await app.page.evaluate(async (p) => {
      try {
        // @ts-ignore
        await window.go.main.App.ConfirmRestoreBackup(p, 'takeover');
        return { success: true };
      } catch (e: any) {
        return { success: false, error: String(e) };
      }
    }, backupPath);
    expect(result.success).toBe(true);

    // Restart to reload ShareManager from restored DB.
    await app.restart(testInfo.parallelIndex);

    // The restored share should be status=active.
    const restored = await app.getShareStatus();
    const share = (restored.shares || []).find((s: any) => s.tag_id === tagID);
    expect(share).toBeDefined();
    expect(share.status).toBe('active');
    expect(share.share_string).toBe(shareString);
  });

  test('keep policy marks restored share as invalid', async ({ app, tempDir }, testInfo) => {
    // Create a tag, start a share.
    await app.createTag('restore-keep-tag');
    const { shareString, tagID } = await app.startShare('restore-keep-tag');
    expect(shareString).toMatch(/^mp-share:v1:/);

    // Create a backup.
    const backupPath = path.join(tempDir, 'keep-backup.zip');
    await app.page.evaluate(async (p) => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.CreateBackup(p);
    }, backupPath);

    // Restore with "keep" — the local identity is preserved; restored shares
    // used a DIFFERENT peer id and must be marked invalid.
    const result = await app.page.evaluate(async (p) => {
      try {
        // @ts-ignore
        await window.go.main.App.ConfirmRestoreBackup(p, 'keep');
        return { success: true };
      } catch (e: any) {
        return { success: false, error: String(e) };
      }
    }, backupPath);
    expect(result.success).toBe(true);

    // Restart to reload ShareManager from restored DB.
    await app.restart(testInfo.parallelIndex);

    // After restore with "keep", the share row should exist but be invalid.
    const restored = await app.getShareStatus();
    const share = (restored.shares || []).find((s: any) => s.tag_id === tagID);
    expect(share).toBeDefined();
    expect(share.status).toBe('invalid');
  });
});
