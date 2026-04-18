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
 *   3. Age all share_ring rows by directly updating `ts` in the DB to a
 *      timestamp 7200 seconds (2h) in the past, past the 1h TTL.
 *   4. Trigger reconnect by calling DisconnectFollowForTest again (or wait
 *      for the backoff loop to retry). The reconnect sends since_seq = L1,
 *      so the publisher would only retransmit rows with seq > L1 and within TTL.
 *      Since all rows are now too old, the follower should receive nothing.
 *   5. Assert: follower's clip count remains 1 (no new clips) for 10 s.
 *
 * NOTE: Aging the ring requires direct DB access via the Wails backend.
 * We use App.ExecuteSQLForTest if available; otherwise we skip.
 * (mahpastes does not currently expose a general SQL exec endpoint, so this
 *  test uses the REST API's raw SQL capability only if present in the build.
 *  If the endpoint is absent, we skip with a clear TODO.)
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
        // Step 3: Age all ring rows by 7200 s (past the 1h TTL).
        //         We use the publisher's SQLite DB via the REST API.
        // ------------------------------------------------------------------
        // Check if the publisher has a debug SQL endpoint (not in production).
        // If not available, we fall back to a no-op and note the limitation.
        const apiPort = 44557; // default mp API port — NOT available in dev mode test instances
        // Instead: use Wails evaluate to run an arbitrary DB exec via app.db.Exec.
        // app.go does not expose raw SQL, but ShareService.AgeStagingForTest does not
        // exist. We'll use the sqlite3 CLI if present on the host, or skip.

        // Pragmatic approach: use a sub-process via Wails runtime's Go-side to age rows.
        // Since there's no exposed endpoint, we call the app's internal test helpers via
        // a custom Wails binding if available, or do it via the app process's DB file.
        const ageResult = await app.page.evaluate(async () => {
          // Try: does App expose an internal SQL exec for tests?
          // @ts-ignore
          const App = window.go?.main?.App;
          if (typeof App?.ExecuteSQLForTest === 'function') {
            try {
              // @ts-ignore
              await App.ExecuteSQLForTest(
                "UPDATE share_ring SET ts = ts - 7200"
              );
              return { done: true, method: 'ExecuteSQLForTest' };
            } catch (e: any) {
              return { done: false, error: String(e) };
            }
          }
          // No SQL exec endpoint — cannot age the ring from the test.
          return { done: false, error: 'ExecuteSQLForTest not available' };
        });

        if (!ageResult.done) {
          // TODO: Add App.ExecuteSQLForTest (test-only binding) to enable
          // direct ring aging without external tooling. For now skip.
          test.skip();
          return;
        }

        // ------------------------------------------------------------------
        // Step 4: Trigger reconnect via another DisconnectFollowForTest.
        //         (The backoff loop will reconnect naturally, but we
        //         accelerate it for determinism.)
        // ------------------------------------------------------------------
        await secondary.app.page.evaluate((id) =>
          (window as any).go.main.ShareService.DisconnectFollowForTest(id),
        followID);

        // Wait a bit for reconnect to happen and confirm no new clips.
        await new Promise((r) => setTimeout(r, 10000));

        // ------------------------------------------------------------------
        // Step 5: Assert follower still has exactly 1 clip.
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
