import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Select All with Search Filter', () => {
  /**
   * BUG: When a search filter is active, "Select All" selects ALL clips in
   * the gallery, including those hidden by the search filter. Subsequent
   * bulk operations (archive, delete, tag) then affect clips the user
   * cannot see, which can lead to unintended data loss.
   *
   * Expected: "Select All" should only select clips that are currently
   * visible (i.e., matching the search filter).
   *
   * Actual: "Select All" selects every clip in the gallery DOM, including
   * those hidden via display:none by the search filter.
   */

  test('select all during search should only select visible (matching) clips', async ({ app }) => {
    // Upload 4 clips: 3 images and 1 text file
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
      createTempFile(generateTestText('searchable-content'), 'txt'),
    ]);
    const filenames = files.map((f) => path.basename(f));

    await app.uploadFiles(files);
    await app.expectClipCount(4);

    // Search for the text file by extension to filter down to 1 visible clip
    await app.search('.txt');

    // Only the text file should be visible
    await app.expectClipVisible(filenames[3]);

    // The image clips should be hidden by the search filter
    await app.expectClipNotVisible(filenames[0]);
    await app.expectClipNotVisible(filenames[1]);
    await app.expectClipNotVisible(filenames[2]);

    // Select the visible clip first (to make the bulk toolbar appear)
    await app.selectClip(filenames[3]);

    // Click "Select All" — should only select the 1 visible clip
    const selectAllCheckbox = app.page.locator(selectors.bulk.selectAllCheckbox);
    await selectAllCheckbox.check();

    // EXPECTED: Only 1 clip selected (the visible one matching the search)
    // ACTUAL (BUG): 4 clips selected (all clips, including 3 hidden ones)
    const selectedCount = await app.getSelectedCount();
    expect(selectedCount).toBe(1);
  });

  test('bulk archive during search should only affect visible clips', async ({ app }) => {
    // Upload 3 images and 1 text file
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
      createTempFile(generateTestText('archive-filter-test'), 'txt'),
    ]);
    const filenames = files.map((f) => path.basename(f));

    await app.uploadFiles(files);
    await app.expectClipCount(4);

    // Search to filter down to just the text file
    await app.search('.txt');

    // Select visible clip and then select all
    await app.selectClip(filenames[3]);
    const selectAllCheckbox = app.page.locator(selectors.bulk.selectAllCheckbox);
    await selectAllCheckbox.check();

    // Archive the "selected" clips
    await app.bulkArchive();

    // Clear search to see all remaining clips
    await app.clearSearch();

    // EXPECTED: 3 image clips remain (only the visible text clip was archived)
    // ACTUAL (BUG): 0 clips remain (all 4 were archived, including the 3 hidden images)
    await app.expectClipCount(3);

    // Verify the text file is no longer in main view (it was archived)
    await app.expectClipNotVisible(filenames[3]);

    // Verify the image clips are still in main view (they were hidden and should not have been archived)
    await app.expectClipVisible(filenames[0]);
    await app.expectClipVisible(filenames[1]);
    await app.expectClipVisible(filenames[2]);
  });

  test('bulk delete during search should only affect visible clips', async ({ app }) => {
    // Upload 2 images and 1 text file
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestText('delete-filter-test'), 'txt'),
    ]);
    const filenames = files.map((f) => path.basename(f));

    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Search to filter to just the text file
    await app.search('.txt');

    // Select visible clip and then select all
    await app.selectClip(filenames[2]);
    const selectAllCheckbox = app.page.locator(selectors.bulk.selectAllCheckbox);
    await selectAllCheckbox.check();

    // Delete the "selected" clips
    await app.bulkDelete();
    await app.confirmDialog();

    // Clear search to see remaining clips
    await app.clearSearch();

    // EXPECTED: 2 image clips remain (only the visible text clip was deleted)
    // ACTUAL (BUG): 0 clips remain (all 3 were deleted, including the 2 hidden images)
    await app.expectClipCount(2);

    // Verify the images survived the delete
    await app.expectClipVisible(filenames[0]);
    await app.expectClipVisible(filenames[1]);
  });
});
