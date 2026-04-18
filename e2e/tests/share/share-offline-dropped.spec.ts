/**
 * share-offline-dropped.spec.ts
 *
 * Verifies that stale ring entries (older than the TTL) are NOT delivered
 * to a reconnecting follower.
 *
 * APPROACH:
 *   1. Publisher shares a tag; follower subscribes and receives clip 1.
 *      Record last_seq L1.
 *   2. Force-disconnect the follower via DisconnectFollowForTest.
 *   3. Upload clip 2 on publisher WHILE follower is disconnected. Clip 2's
 *      chunks land in the ring with fresh ts and seq > L1.
 *   4. Age the whole ring by 7200 s via ShareService.AgeShareRingForTest.
 *      Clip 2's ring rows now have aged ts (older than the 1h TTL).
 *   5. Let the follower's runFollowLoop reconnect on its own backoff. On
 *      handshake it sends since_seq = L1. Publisher's RingRetransmit
 *      filter (seq > L1 AND ts >= cutoff) drops every aged row.
 *   6. Assert: follower's clip count stays at 1 — clip 2's envelopes
 *      were dropped by the TTL filter rather than being replayed.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';

test.describe('Share - Offline dropped (stale ring)', () => {

  // Two-instance mDNS setup + parallel suite load can take 3+ minutes.
  // The test skips after setup if ExecuteSQLForTest is unavailable.
  test.setTimeout(240000);

  test(
    'follower does NOT receive aged-out ring entries after reconnect',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Set up share and follower, deliver clip 1.
      // ------------------------------------------------------------------
      const tagName = 'dropped-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);

      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        const localTagName = 'dropped-inbox';
        await secondary.app.followShare(shareString, localTagName);

        const initStatus = await secondary.app.getShareStatus();
        const initFollow = (initStatus.follows || []).find(
          (f: any) => f.local_tag_name === localTagName,
        );
        expect(initFollow).toBeDefined();
        const followID: number = initFollow.id;

        // Upload clip 1 and wait for follower to receive it.
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
          { timeout: 120000, intervals: [2000, 2000, 3000] },
        ).toBe(true);

        // ------------------------------------------------------------------
        // Step 2: Force disconnect.
        // ------------------------------------------------------------------
        await secondary.app.page.evaluate((id) =>
          (window as any).go.main.ShareService.DisconnectFollowForTest(id),
        followID);
        await expect.poll(
          async () => {
            const st = await secondary.app.getShareStatus();
            return (st.follows || []).find((f: any) => f.id === followID)?.status;
          },
          { timeout: 10000, intervals: [500, 1000] },
        ).toBe('offline');

        // ------------------------------------------------------------------
        // Step 3: Upload clip 2 on publisher WHILE follower is disconnected.
        //         The direct-to-followers fan-out is a no-op (no live
        //         stream); the chunks still land in share_ring for the
        //         eventual catch-up.
        // ------------------------------------------------------------------
        const img2 = await createTempFile(generateTestImage(60, 60, [0, 255, 0]), 'png');
        await app.uploadFile(img2);
        await app.addTagToClip(img2.split('/').pop()!, tagName);

        // Wait for the publisher's shares.last_seq to advance past L1,
        // confirming the envelopes for clip 2 are written to the ring.
        await expect.poll(
          async () => {
            const st = await app.getShareStatus();
            const s = (st.shares || []).find((sh: any) => sh.tag_id === tagID);
            return (s?.last_seq ?? 0) > L1;
          },
          { timeout: 20000, intervals: [500, 1000, 1500] },
        ).toBe(true);

        // ------------------------------------------------------------------
        // Step 4: Age every ring row by 7200 s (past the 1h TTL) so clip 2's
        //         freshly-stored chunks are now past the retransmit cutoff.
        // ------------------------------------------------------------------
        await app.page.evaluate(() =>
          (window as any).go.main.ShareService.AgeShareRingForTest(7200),
        );

        // ------------------------------------------------------------------
        // Step 5: Rely on the follower's runFollowLoop to reconnect on its
        //         own backoff. Handshake sends since_seq = L1; the publisher's
        //         RingRetransmit filter drops every row whose ts < now - TTL,
        //         so clip 2's envelopes never travel.
        // ------------------------------------------------------------------

        // Wait up to 45 s to give the follow loop's backoff plenty of time
        // to redial, handshake, and process whatever the publisher ships
        // back. Poll the follow status to confirm a reconnect happened.
        await expect.poll(
          async () => {
            const st = await secondary.app.getShareStatus();
            return (st.follows || []).find((f: any) => f.id === followID)?.status;
          },
          { timeout: 45000, intervals: [1000, 2000, 3000] },
        ).toBe('connected');

        // Another settle window for any envelope the follower might have
        // accepted before the TTL filter kicked in (should be zero).
        await new Promise((r) => setTimeout(r, 5000));

        // ------------------------------------------------------------------
        // Step 6: Assert follower still has exactly 1 clip — clip 2 was
        //         dropped by the TTL filter on the publisher's replay.
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
