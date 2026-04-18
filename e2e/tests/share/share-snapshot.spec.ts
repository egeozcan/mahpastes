/**
 * share-snapshot.spec.ts
 *
 * Verifies that the share ring holds a canonical copy of clip data.
 * After the publisher deletes the source clip, a follower that reconnects
 * still receives the original bytes (the ring has the authoritative copy).
 *
 * WHAT IS TESTED:
 *   1. Publisher uploads a clip and tags it into a shared tag.
 *   2. Publisher deletes the source clip from the DB.
 *   3. A fresh follower (spawned after deletion) connects and receives
 *      the clip from the ring, not from live DB storage.
 *   4. The received clip's byte length matches the original.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';

test.describe('Share - Snapshot (ring canonical copy)', () => {

  // Two-instance test: spawning secondary + mDNS discovery + transfer can take >60s.
  // Under full suite load with parallel workers, allow up to 240s.
  test.setTimeout(240000);

  test(
    'follower receives clip even after publisher deleted the source',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Publisher creates tag and starts share.
      // ------------------------------------------------------------------
      const tagName = 'snapshot-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);
      expect(shareString).toMatch(/^mp-share:v1:/);

      // ------------------------------------------------------------------
      // Step 2: Upload clip 1 (the "canonical" clip) and tag it.
      //         Also upload clip 2 as a placeholder so the tag is NOT
      //         orphaned when clip 1 is deleted (DeleteClip calls
      //         deleteTagIfOrphaned which would cascade-delete the share
      //         and ring entries).
      // ------------------------------------------------------------------
      const imageBuffer = generateTestImage(100, 100, [0, 128, 255]);
      const imagePath = await createTempFile(imageBuffer, 'png');
      const filename = imagePath.split('/').pop()!;

      await app.uploadFile(imagePath);
      await app.addTagToClip(filename, tagName);

      // Upload placeholder so the tag survives clip 1 deletion.
      const placeholderBuf = generateTestImage(10, 10, [255, 0, 0]);
      const placeholderPath = await createTempFile(placeholderBuf, 'png');
      await app.uploadFile(placeholderPath);
      await app.addTagToClip(placeholderPath.split('/').pop()!, tagName);

      // Record original size of clip 1.
      const originalBytes = await app.page.evaluate(async (fname) => {
        // @ts-ignore
        const clips = await window.go.main.App.GetClips(false, [], [], '', '');
        const clip = clips.find((c: any) => c.filename?.includes(fname.replace('.png', '')));
        return clip?.size ?? 0;
      }, filename);
      expect(originalBytes).toBeGreaterThan(0);

      // Wait for the ring to have envelopes from both clips.
      await expect.poll(
        async () => {
          const st = await app.getShareStatus();
          const share = (st.shares || []).find((s: any) => s.tag_id === tagID);
          return share?.clips_pushed ?? 0;
        },
        { timeout: 15000, intervals: [500, 1000, 2000] },
      ).toBeGreaterThanOrEqual(2);

      // ------------------------------------------------------------------
      // Step 3: Publisher deletes clip 1 (the large/canonical clip).
      //         The placeholder keeps the tag alive so share + ring survive.
      // ------------------------------------------------------------------
      const clipID = await app.page.evaluate(async (fname) => {
        // @ts-ignore
        const clips = await window.go.main.App.GetClips(false, [], [], '', '');
        const clip = clips.find((c: any) => c.filename?.includes(fname.replace('.png', '')));
        return clip?.id ?? null;
      }, filename);
      expect(clipID).not.toBeNull();

      await app.page.evaluate(async (id) => {
        // @ts-ignore
        await window.go.main.App.DeleteClip(id);
      }, clipID);

      // Clip 1 gone; placeholder still there.
      const pubCount = await app.getClipCountFromDB();
      expect(pubCount).toBe(1); // placeholder only

      // ------------------------------------------------------------------
      // Step 4: Spawn a fresh follower and follow the share.
      // ------------------------------------------------------------------
      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        const localTagName = 'snapshot-received';
        await secondary.app.followShare(shareString, localTagName);

        // ------------------------------------------------------------------
        // Step 5: Wait for the follower to receive the clip from the ring.
        // ------------------------------------------------------------------
        // Wait for both clips to arrive (clip 1 + placeholder).
        await expect.poll(
          async () => secondary.app.getClipCountFromDB(),
          {
            timeout: 120000,
            intervals: [2000, 2000, 3000, 3000, 5000],
            message: 'Follower did not receive 2 clips within 60 s',
          },
        ).toBeGreaterThanOrEqual(2);

        // ------------------------------------------------------------------
        // Step 6: Verify clip 1's byte length matches the original.
        //         Clips are sorted by date descending; find by size.
        // ------------------------------------------------------------------
        const followerOriginalBytes = await secondary.app.page.evaluate(async (expected) => {
          // @ts-ignore
          const clips = await window.go.main.App.GetClips(false, [], [], '', '');
          // Find the clip whose size matches the original
          const match = clips.find((c: any) => c.size === expected);
          return match?.size ?? 0;
        }, originalBytes);
        expect(followerOriginalBytes).toBe(originalBytes);

      } finally {
        try { await app.stopShare(tagID); } catch { /* ignore */ }
        await secondary.cleanup();
      }
    },
  );
});
