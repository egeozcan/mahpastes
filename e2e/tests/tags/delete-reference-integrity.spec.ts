import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('DeleteTag reference integrity', () => {
  test.afterEach(async ({ app }) => {
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  // The follow-blocker test is intentionally skipped. Inserting a fake
  // `follows` row requires a dev-only SQL backdoor that has not been wired
  // up. The corresponding Go unit test (TestDeleteTag_BlockedByFollow from
  // Task 4) already covers the precondition check exhaustively. E2e coverage
  // for this path is deferred until a test-mode SQL helper is available.
  test.skip('surfaces follow blocker as a user-friendly error', async ({ app }) => {
    const { tagID } = await app.createTag('blocked');
    // insertFakeFollow would go here — not yet implemented.
    void tagID;
  });

  // Regression for the filter-normalization P2: deleting a tag that is an
  // active filter in non-folder mode must drop the filter rather than leave
  // the gallery filtering on a nonexistent ID.
  test('clears active filter when the filtered tag is deleted (non-folder mode)', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.createTag('filter-target');
    await app.tagClipByIndex(0, 'filter-target');

    // Activate the filter — only the tagged clip should be visible.
    await app.selectTagFilter('filter-target');
    await app.expectClipCount(1);

    // Delete the tag while it is an active filter.
    await app.deleteTag('filter-target');

    // The tag pill should be gone from the active-tags-container.
    await app.expectTagFilterInactive('filter-target');
    // Gallery should show the clip again (no longer filtered to a dead tag).
    await app.expectClipCount(1);
  });
});
