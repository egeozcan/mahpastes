/**
 * share-offline-dropped.spec.ts
 *
 * Verifies that share_ring entries older than the TTL are NOT delivered to a
 * subscribing follower.
 *
 * APPROACH (no races): age the ring BEFORE the follower ever exists, then
 * subscribe and confirm the aged rows are filtered out by RingRetransmit:
 *
 *   1. Publisher starts share; uploads clip 1; waits for publisher's
 *      shares.last_seq to advance (confirms the ring was populated).
 *   2. Call ShareService.AgeShareRingForTest(7200) to move every row's
 *      ts 2 h into the past (past the 1 h TTL cutoff).
 *   3. Spawn the secondary (follower) for the first time and call
 *      followShare. The handshake sends since_seq=0. The publisher's
 *      RingRetransmit filter (seq > 0 AND ts >= now-TTL) drops every
 *      aged envelope — follower receives NOTHING from the ring.
 *   4. Upload clip 2 on publisher — its ring row has fresh ts, and the
 *      direct-to-followers fan-out delivers it live to the connected
 *      follower. This proves the follower IS wired up correctly (it
 *      just didn't receive the aged rows), not that the follow is broken.
 *   5. Assert: follower has exactly 1 clip (clip 2), not 2.
 *
 * This deliberately avoids the disconnect/reconnect race of the plan's
 * original sketch: there the follower's runFollowLoop reconnects on a 1 s
 * backoff, racing the test's "age ring" step and potentially delivering
 * clip 2 live before aging takes effect.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

test.describe('Share - Offline dropped (stale ring)', () => {

  // Two-instance mDNS setup + ring handling can take 2+ minutes under load.
  test.setTimeout(240000);

  test(
    'follower does NOT receive aged-out ring entries after subscribing',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Publisher — start share, upload clip 1, wait for ring row.
      // ------------------------------------------------------------------
      const tagName = 'dropped-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);

      const img1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      await app.uploadFile(img1);
      await app.addTagToClip(path.basename(img1), tagName);

      // Confirm the publisher's ring row is written.
      await expect.poll(
        async () => {
          const st = await app.getShareStatus();
          const s = (st.shares || []).find((sh: any) => sh.tag_id === tagID);
          return (s?.last_seq ?? 0) > 0;
        },
        { timeout: 20000, intervals: [500, 1000, 1500] },
      ).toBe(true);

      // ------------------------------------------------------------------
      // Step 2: Age every ring row past the 1 h TTL.
      // ------------------------------------------------------------------
      await app.page.evaluate(() =>
        (window as any).go.main.ShareService.AgeShareRingForTest(7200),
      );

      // ------------------------------------------------------------------
      // Step 3: Spawn follower and subscribe. Handshake with since_seq=0
      //         should produce zero retransmitted envelopes because every
      //         ring row is now past ts >= now-TTL.
      // ------------------------------------------------------------------
      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        const localTagName = 'dropped-inbox';
        await secondary.app.followShare(shareString, localTagName);

        // Let the follower settle into the connected state.
        await expect.poll(
          async () => {
            const st = await secondary.app.getShareStatus();
            const f = (st.follows || []).find(
              (ff: any) => ff.local_tag_name === localTagName,
            );
            return f?.status;
          },
          { timeout: 30000, intervals: [500, 1000, 2000] },
        ).toBe('connected');

        // Give the publisher some time to have replayed from the ring
        // (even though we expect zero envelopes to get past the filter).
        await new Promise((r) => setTimeout(r, 4000));

        // ------------------------------------------------------------------
        // Step 4: Upload clip 2 on publisher. Its fresh ring row AND the
        //         live direct-emit path deliver it to the now-connected
        //         follower.
        // ------------------------------------------------------------------
        const img2 = await createTempFile(generateTestImage(60, 60, [0, 255, 0]), 'png');
        await app.uploadFile(img2);
        await app.addTagToClip(path.basename(img2), tagName);

        // Follower should receive clip 2 (but not the aged clip 1).
        await expect.poll(
          async () => secondary.app.getClipCountFromDB(),
          { timeout: 30000, intervals: [1000, 2000, 3000] },
        ).toBe(1);

        // Belt-and-suspenders: wait a little longer, confirm no extra
        // clip leaks in from the (aged) ring later.
        await new Promise((r) => setTimeout(r, 3000));

        // ------------------------------------------------------------------
        // Step 5: Final assertion. Follower has 1 clip (clip 2), not 2.
        //         If TTL filtering were broken, clip 1 would have replayed
        //         during handshake and count would be 2.
        // ------------------------------------------------------------------
        const finalCount = await secondary.app.getClipCountFromDB();
        expect(finalCount).toBe(1);

      } finally {
        try { await app.stopShare(tagID); } catch { /* ignore */ }
        await secondary.cleanup();
      }
    },
  );
});
