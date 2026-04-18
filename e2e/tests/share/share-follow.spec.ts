/**
 * share-follow.spec.ts
 *
 * End-to-end test for the publisher → follower delivery loop.
 *
 * Two fresh mahpastes instances on the same machine discover each other via
 * libp2p mDNS (no DHT routing table required). The publisher creates and shares
 * a tag, the follower subscribes, the publisher uploads a clip, and the test
 * asserts the follower receives it within 30 s.
 *
 * WHAT IS TESTED:
 *   - Share creation UI (`startShare` helper) — exercises
 *     `#add-share-btn` → `#create-share-confirm-btn` → `ShareService.StartShare`.
 *   - AppHelper additions: `startShare`, `stopShare`, `followShare`,
 *     `getShareStatus`, `spawnSecondary`.
 *   - Full publisher → follower delivery over a live libp2p stream.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';

test.describe('Share - Follow and receive', () => {

  test(
    'follower receives a clip published after following',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Create a tag on the publisher side and start sharing it.
      // ------------------------------------------------------------------
      const tagName = 'shared-tag';
      await app.createTag(tagName);

      const { shareString, tagID } = await app.startShare(tagName);
      expect(shareString).toMatch(/^mp-share:v1:/);
      expect(tagID).toBeGreaterThan(0);

      // ------------------------------------------------------------------
      // Step 2: Spawn the follower (secondary) instance.
      // ------------------------------------------------------------------
      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        // ------------------------------------------------------------------
        // Step 3: Follower follows the share string.
        //
        // NOTE: This step fails with "initial dial: dht find peer: failed to
        // find any peer in table" because the follower's DHT routing table is
        // empty. The test is skipped pending a Go-side fix.
        // ------------------------------------------------------------------
        const localTagName = 'received/from-publisher';
        await secondary.app.followShare(shareString, localTagName);

        // Confirm the follow was recorded in the follower's ShareStatus.
        const followerStatus = await secondary.app.getShareStatus();
        expect(followerStatus.follows).toBeDefined();
        expect(Array.isArray(followerStatus.follows)).toBe(true);
        expect(followerStatus.follows.length).toBeGreaterThan(0);

        // ------------------------------------------------------------------
        // Step 4: Publisher uploads a clip tagged into the shared tag.
        // ------------------------------------------------------------------
        const imagePath = await createTempFile(generateTestImage(50, 50, [0, 128, 255]), 'png');
        await app.uploadFile(imagePath);
        await app.addTagToClip(imagePath.split('/').pop()!, tagName);

        // ------------------------------------------------------------------
        // Step 5: Wait for the follower to receive the clip (≤ 30 s).
        //
        // We poll `ShareService.GetShareStatus()` on the follower's page and
        // check that `clips_received` reaches 1. libp2p bootstrap + DHT can
        // take 5–10 s on localhost; 30 s provides a safe margin.
        // ------------------------------------------------------------------
        await expect.poll(
          async () => {
            const status = await secondary.app.getShareStatus();
            const follows: any[] = status.follows || [];
            const follow = follows.find((f: any) => f.local_tag_name === localTagName);
            return follow?.clips_received ?? 0;
          },
          {
            timeout: 30000,
            intervals: [2000, 2000, 3000, 3000, 5000],
            message: `Follower did not receive the published clip within 30 s`,
          },
        ).toBeGreaterThanOrEqual(1);

        // ------------------------------------------------------------------
        // Sanity-check: the follower's DB should also have a clip.
        // ------------------------------------------------------------------
        const followerClipCount = await secondary.app.getClipCountFromDB();
        expect(followerClipCount).toBeGreaterThanOrEqual(1);

      } finally {
        // ------------------------------------------------------------------
        // Cleanup: stop the share on the publisher side and kill the secondary.
        // ------------------------------------------------------------------
        try {
          await app.stopShare(tagID);
        } catch {
          // Ignore cleanup errors
        }
        await secondary.cleanup();
      }
    },
  );
});
