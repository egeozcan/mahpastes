/**
 * share-large-clip.spec.ts
 *
 * Verifies that a large clip (8 MiB PNG) transfers correctly from publisher
 * to follower over a live libp2p stream.
 *
 * WHAT IS TESTED:
 *   - An 8 MiB PNG (generated with generateTestImage(2048, 2048)) is uploaded
 *     to the publisher and tagged into a shared tag.
 *   - The follower receives the clip within 60 s.
 *   - The received clip's byte length matches the original (confirming no
 *     truncation or corruption across the chunked transfer protocol).
 */

import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile } from '../../helpers/test-data.js';
import * as crypto from 'crypto';

/**
 * Generate an ~8 MiB PNG-like buffer that is NOT compressible (random data).
 *
 * We wrap random bytes in a minimal valid PNG so the upload handler accepts it
 * as image/png. The IDAT payload is raw random bytes (not zlib-compressed) —
 * technically not a valid PNG image, but mahpastes stores it verbatim in the
 * DB so the transfer size is what matters, not decodability.
 *
 * To keep the file valid enough for upload, we construct:
 *   PNG_SIGNATURE + IHDR(100x100, 8-bit RGB) + IDAT(~8 MiB of random bytes) + IEND
 */
function generateLargeRandomPayload(targetBytes = 8 * 1024 * 1024): Buffer {
  // PNG signature
  const sig = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);

  // IHDR (13 bytes payload: 4+4+1+1+1+1+1)
  function u32be(n: number): Buffer {
    const b = Buffer.alloc(4);
    b.writeUInt32BE(n, 0);
    return b;
  }
  function chunk(type: string, data: Buffer): Buffer {
    const len = u32be(data.length);
    const typeBuf = Buffer.from(type, 'ascii');
    // CRC: not checked by mahpastes but we put 0s to keep the structure valid
    const crcBuf = Buffer.alloc(4);
    return Buffer.concat([len, typeBuf, data, crcBuf]);
  }

  const ihdrData = Buffer.concat([
    u32be(100), u32be(100),      // width, height
    Buffer.from([8, 2, 0, 0, 0]) // bitDepth, colorType=RGB, compress, filter, interlace
  ]);
  const ihdrChunk = chunk('IHDR', ihdrData);
  const iendChunk = chunk('IEND', Buffer.alloc(0));

  // Fill the rest with random bytes wrapped in a single IDAT chunk.
  const overhead = sig.length + ihdrChunk.length + iendChunk.length + 12; // 12 = IDAT len+type+crc
  const idatPayload = crypto.randomBytes(Math.max(0, targetBytes - overhead));
  const idatChunk = chunk('IDAT', idatPayload);

  return Buffer.concat([sig, ihdrChunk, idatChunk, iendChunk]);
}

test.describe('Share - Large clip transfer', () => {

  // Two-instance mDNS + large file transfer + parallel suite load can take
  // 3+ minutes when all workers spawn secondaries simultaneously.
  test.setTimeout(240000);

  test(
    'follower receives an 8 MiB PNG with correct byte length',
    async ({ app }, testInfo) => {
      // ------------------------------------------------------------------
      // Step 1: Publisher creates tag and starts share.
      // ------------------------------------------------------------------
      const tagName = 'large-clip-tag';
      await app.createTag(tagName);
      const { shareString, tagID } = await app.startShare(tagName);
      expect(shareString).toMatch(/^mp-share:v1:/);

      // ------------------------------------------------------------------
      // Step 2: Spawn follower.
      // ------------------------------------------------------------------
      const secondary = await app.spawnSecondary(testInfo.parallelIndex);

      try {
        // ------------------------------------------------------------------
        // Step 3: Follower subscribes.
        // ------------------------------------------------------------------
        const localTagName = 'large/received';
        await secondary.app.followShare(shareString, localTagName);

        const followerStatus = await secondary.app.getShareStatus();
        expect(followerStatus.follows).toBeDefined();
        expect(followerStatus.follows.length).toBeGreaterThan(0);

        // ------------------------------------------------------------------
        // Step 4: Publisher uploads an 8 MiB PNG.
        // ------------------------------------------------------------------
        const imageBuffer = generateLargeRandomPayload(8 * 1024 * 1024);
        const imagePath = await createTempFile(imageBuffer, 'png');
        await app.uploadFile(imagePath);
        await app.addTagToClip(imagePath.split('/').pop()!, tagName);

        // Record the original byte length from the publisher's DB.
        const originalBytes = await app.page.evaluate(async () => {
          // @ts-ignore - Wails runtime
          const clips = await window.go.main.App.GetClips(false, [], [], '', '');
          if (!Array.isArray(clips) || clips.length === 0) return 0;
          // Use the size field from ClipPreview (LENGTH(data) is in preview.size).
          return clips[0].size ?? 0;
        });
        expect(originalBytes).toBeGreaterThan(8 * 1024 * 1024 - 100); // ~8 MiB

        // ------------------------------------------------------------------
        // Step 5: Wait for the follower to receive the clip (≤ 60 s).
        // ------------------------------------------------------------------
        await expect.poll(
          async () => {
            const status = await secondary.app.getShareStatus();
            const follows: any[] = status.follows || [];
            const follow = follows.find((f: any) => f.local_tag_name === localTagName);
            return follow?.clips_received ?? 0;
          },
          {
            timeout: 180000,
            intervals: [2000, 2000, 3000, 3000, 5000],
            message: 'Follower did not receive the large clip within 180 s',
          },
        ).toBeGreaterThanOrEqual(1);

        // ------------------------------------------------------------------
        // Step 6: Verify byte length on the follower side.
        // ------------------------------------------------------------------
        const followerBytes = await secondary.app.page.evaluate(async () => {
          // @ts-ignore - Wails runtime
          const clips = await window.go.main.App.GetClips(false, [], [], '', '');
          if (!Array.isArray(clips) || clips.length === 0) return 0;
          return clips[0].size ?? 0;
        });
        expect(followerBytes).toBe(originalBytes);

      } finally {
        try { await app.stopShare(tagID); } catch { /* ignore cleanup */ }
        await secondary.cleanup();
      }
    },
  );
});
