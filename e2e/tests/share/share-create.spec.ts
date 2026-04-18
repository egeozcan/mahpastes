/**
 * share-create.spec.ts
 *
 * Tests share creation and stopping via the UI and backend API.
 *
 * WHAT IS TESTED:
 *   - After creating a share, the share string matches `^mp-share:v1:`.
 *   - After stopping the share, GetShareStatus().shares is empty.
 */

import { test, expect } from '../../fixtures/test-fixtures.js';

test.describe('Share - Create and stop', () => {

  test('share string matches expected format', async ({ app }) => {
    await app.createTag('create-test-tag');
    const { shareString } = await app.startShare('create-test-tag');

    expect(shareString).toMatch(/^mp-share:v1:/);
    // The blob after the prefix must be non-empty base64url.
    const blob = shareString.slice('mp-share:v1:'.length);
    expect(blob.length).toBeGreaterThan(0);
    expect(blob).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  test('stopped share disappears from GetShareStatus', async ({ app }) => {
    await app.createTag('stop-test-tag');
    const { shareString, tagID } = await app.startShare('stop-test-tag');

    expect(shareString).toMatch(/^mp-share:v1:/);

    // Before stopping there should be one share entry.
    const before = await app.getShareStatus();
    expect(before.shares.length).toBeGreaterThanOrEqual(1);
    const found = before.shares.find((s: any) => s.tag_id === tagID);
    expect(found).toBeDefined();
    expect(found.status).toBe('active');

    // Stop the share.
    await app.stopShare(tagID);

    // After stopping, no shares should remain for this tag.
    const after = await app.getShareStatus();
    const stillThere = (after.shares || []).find((s: any) => s.tag_id === tagID);
    expect(stillThere).toBeUndefined();
  });

  test('share status reflects correct tag name', async ({ app }) => {
    const tagName = 'named-share-tag';
    await app.createTag(tagName);
    const { tagID } = await app.startShare(tagName);

    const status = await app.getShareStatus();
    const share = status.shares.find((s: any) => s.tag_id === tagID);
    expect(share).toBeDefined();
    expect(share.tag_name).toBe(tagName);

    // Cleanup
    await app.stopShare(tagID);
  });
});
