/**
 * share-persistence.spec.ts
 *
 * Verifies that shares and follows survive an app restart.
 *
 * WHAT IS TESTED:
 *   - A share created before restart is still present and active after restart.
 *   - The share string is identical after restart (same underlying identity).
 *   - AppHelper.restart() preserves the data directory (SQLite + identity key).
 */

import { test, expect } from '../../fixtures/test-fixtures.js';

test.describe('Share - Persistence across restart', () => {

  // Restart involves killing and respawning wails dev; give plenty of headroom.
  test.setTimeout(300000);

  test(
    'share survives app restart with same share string',
    async ({ app }, testInfo) => {
      // Create a tag and share it.
      const tagName = 'persist-share-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);

      expect(shareString).toMatch(/^mp-share:v1:/);

      // Restart the app (kills the process, respawns with same dataDir).
      await app.restart(testInfo.parallelIndex);

      // After restart, the share should still be present and the string identical.
      const status = await app.getShareStatus();
      expect(status.shares).toBeDefined();
      expect(Array.isArray(status.shares)).toBe(true);

      const found = status.shares.find((s: any) => s.tag_id === tagID);
      expect(found).toBeDefined();
      expect(found.share_string).toBe(shareString);
      expect(found.status).toBe('active');
    },
  );
});
