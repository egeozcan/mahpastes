/**
 * share-publisher-restart.spec.ts
 *
 * Verifies that after the publisher app restarts, the follower reconnects
 * automatically (via its backoff loop) and receives clips published after
 * the restart.
 *
 * STEPS:
 *   1. Publisher A shares tag `restart-tag`; follower B follows into `restart-inbox`.
 *   2. A uploads clip 1 (tagged); B receives it.  L1 = B.last_seq.
 *   3. Restart the publisher (app.restart).  After restart the share is re-activated
 *      (share entries are loaded back from the DB by ShareManager on startup).
 *   4. A uploads clip 2 (tagged restart-tag).
 *   5. Poll: B's clip count reaches 2 within 60 s (backoff reconnect, no manual nudge).
 *   6. Assert B.last_seq > L1 and B still has the same follow row ID.
 *
 * NOTE: This test restarts the publisher app (wails dev). The suite must be
 * `serial` to avoid competing restarts fighting over the build/ directory.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';

test.describe('Share - Publisher restart', () => {

  // Serial to avoid parallel restart contention on the build/ directory.
  test.describe.configure({ mode: 'serial' });

  // Restart + mDNS rediscovery can take a couple of minutes.
  test.setTimeout(300000);

  test(
    'follower reconnects and receives clip after publisher restarts',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Publisher creates tag and starts share.
      // ------------------------------------------------------------------
      const tagName = 'restart-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);
      expect(shareString).toMatch(/^mp-share:v1:/);

      // ------------------------------------------------------------------
      // Step 2: Spawn follower and follow the share.
      // ------------------------------------------------------------------
      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        const localTagName = 'restart-inbox';
        await secondary.app.followShare(shareString, localTagName);

        // Capture the follow ID.
        const initStatus = await secondary.app.getShareStatus();
        const initFollow = (initStatus.follows || []).find(
          (f: any) => f.local_tag_name === localTagName,
        );
        expect(initFollow).toBeDefined();
        const followID: number = initFollow.id;

        // Upload clip 1 and wait for it to arrive on the follower.
        const img1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
        await app.uploadFile(img1);
        await app.addTagToClip(img1.split('/').pop()!, tagName);

        let L1 = 0;
        await expect.poll(
          async () => {
            const st = await secondary.app.getShareStatus();
            const f = (st.follows || []).find((ff: any) => ff.id === followID);
            L1 = f?.last_seq ?? 0;
            return (await secondary.app.getClipCountFromDB()) >= 1 && L1 > 0;
          },
          {
            timeout: 60000,
            intervals: [2000, 2000, 3000],
            message: 'Follower did not receive clip 1 within 60 s',
          },
        ).toBe(true);

        // ------------------------------------------------------------------
        // Step 3: Restart the publisher.
        //         After restart the share is re-initialised from the DB.
        // ------------------------------------------------------------------
        await app.restart(testInfo.parallelIndex);

        // Verify share is re-active after restart.
        await expect.poll(
          async () => {
            const st = await app.getShareStatus();
            const s = (st.shares || []).find((sh: any) => sh.tag_id === tagID);
            return s?.status;
          },
          {
            timeout: 30000,
            intervals: [1000, 2000, 2000],
            message: 'Publisher share did not return to active after restart',
          },
        ).toBe('active');

        // ------------------------------------------------------------------
        // Step 4: Upload clip 2 on the restarted publisher.
        // ------------------------------------------------------------------
        const img2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
        await app.uploadFile(img2);
        await app.addTagToClip(img2.split('/').pop()!, tagName);

        // Confirm publisher has advanced its clips_pushed.
        await expect.poll(
          async () => {
            const st = await app.getShareStatus();
            const s = (st.shares || []).find((sh: any) => sh.tag_id === tagID);
            return s?.clips_pushed ?? 0;
          },
          { timeout: 15000, intervals: [500, 1000] },
        ).toBeGreaterThanOrEqual(1);

        // ------------------------------------------------------------------
        // Step 5: Wait for the follower to reconnect and receive clip 2.
        //         The follower's backoff loop retries ~every 5–30 s.
        // ------------------------------------------------------------------
        await expect.poll(
          async () => secondary.app.getClipCountFromDB(),
          {
            timeout: 120000,
            intervals: [3000, 3000, 5000, 5000, 10000],
            message: 'Follower did not reach 2 clips within 120 s after publisher restart',
          },
        ).toBeGreaterThanOrEqual(2);

        // ------------------------------------------------------------------
        // Step 6: Final assertions.
        // ------------------------------------------------------------------
        const finalStatus = await secondary.app.getShareStatus();
        const finalFollow = (finalStatus.follows || []).find(
          (f: any) => f.id === followID,
        );
        expect(finalFollow).toBeDefined();
        // Same follow row (not re-created).
        expect(finalFollow.id).toBe(followID);
        // last_seq advanced past L1.
        expect(finalFollow.last_seq).toBeGreaterThan(L1);

      } finally {
        try { await app.stopShare(tagID); } catch { /* ignore */ }
        await secondary.cleanup();
      }
    },
  );
});
