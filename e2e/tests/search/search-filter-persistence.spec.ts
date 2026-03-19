import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';

/**
 * Create a temp file with a specific filename (not random).
 * Useful for testing with predictable filenames.
 */
async function createNamedTempFile(
  content: Buffer | string,
  filename: string
): Promise<string> {
  const filepath = path.join(os.tmpdir(), filename);
  await fs.writeFile(filepath, content);
  return filepath;
}

/**
 * Get filenames of clips that are actually visible (not hidden by search filter).
 * The search filter hides clips via display:none on <li> elements.
 */
async function getVisibleClipFilenames(app: any): Promise<string[]> {
  return app.page.evaluate(() => {
    const cards = document.querySelectorAll('#gallery > li:not([data-folder])');
    return Array.from(cards)
      .filter((c: any) => c.style.display !== 'none')
      .map((c: any) => c.dataset.filename);
  });
}

test.describe('Search filter persistence after gallery operations', () => {
  /**
   * BUG: Search filter is not re-applied after operations that trigger loadClips().
   *
   * The search filter in the app works by listening to the 'input' event on the
   * search box and toggling display:none on gallery <li> elements. However, when
   * loadClips() is called (which happens after archive, delete, upload, tag changes,
   * bulk operations, sort changes, etc.), it replaces gallery.innerHTML with fresh
   * DOM nodes but does NOT re-apply the search filter. The search input retains its
   * text value, but the newly-rendered clips are all visible regardless of the query.
   *
   * This results in an inconsistent UI state where the search box shows a filter
   * query but the gallery shows unfiltered clips.
   */

  test('search filter should persist after archiving a clip', async ({ app }) => {
    // Upload clips with distinguishable names
    const fileAlpha = await createNamedTempFile(
      generateTestImage(10, 10, [255, 0, 0]),
      'alpha-persist.png'
    );
    const fileBravo = await createNamedTempFile(
      generateTestImage(10, 10, [0, 255, 0]),
      'bravo-persist.png'
    );
    const fileCharlie = await createNamedTempFile(
      generateTestImage(10, 10, [0, 0, 255]),
      'charlie-persist.png'
    );

    await app.uploadFile(fileAlpha);
    await app.uploadFile(fileBravo);
    await app.uploadFile(fileCharlie);
    await app.expectClipCount(3);

    // Search for "alpha" -- only alpha-persist.png should be visible
    await app.search('alpha');
    const visibleBeforeArchive = await getVisibleClipFilenames(app);
    expect(visibleBeforeArchive).toEqual(['alpha-persist.png']);

    // Archive alpha-persist.png via the card menu
    await app.archiveClip('alpha-persist.png');

    // Wait for the gallery to reload (archived clip leaves the DOM)
    await app.page.waitForFunction(
      (fn: string) => !document.querySelector(`#gallery > li[data-filename="${fn}"]`),
      'alpha-persist.png',
      { timeout: 10000 }
    );

    // After archiving, the search input still contains "alpha"
    const searchValue = await app.page.locator(selectors.header.searchInput).inputValue();
    expect(searchValue).toBe('alpha');

    // The search filter should still be applied to the gallery.
    // Since the only clip matching "alpha" was archived, no clips should be visible.
    const visibleAfterArchive = await getVisibleClipFilenames(app);
    expect(visibleAfterArchive).toEqual([]);
  });

  test('search filter should persist after deleting a clip', async ({ app }) => {
    const fileAlpha = await createNamedTempFile(
      generateTestImage(10, 10, [255, 0, 0]),
      'alpha-del.png'
    );
    const fileBravo = await createNamedTempFile(
      generateTestImage(10, 10, [0, 255, 0]),
      'bravo-del.png'
    );
    const fileCharlie = await createNamedTempFile(
      generateTestImage(10, 10, [0, 0, 255]),
      'charlie-del.png'
    );

    await app.uploadFile(fileAlpha);
    await app.uploadFile(fileBravo);
    await app.uploadFile(fileCharlie);
    await app.expectClipCount(3);

    // Search for "bravo" -- only bravo-del.png should be visible
    await app.search('bravo');
    const visibleBefore = await getVisibleClipFilenames(app);
    expect(visibleBefore).toEqual(['bravo-del.png']);

    // Delete the visible clip
    await app.deleteClip('bravo-del.png');

    // After deletion, the search input still says "bravo"
    const searchValue = await app.page.locator(selectors.header.searchInput).inputValue();
    expect(searchValue).toBe('bravo');

    // No clips should match "bravo" anymore
    // BUG: The remaining clips (alpha, charlie) become visible despite not matching
    const visibleAfter = await getVisibleClipFilenames(app);
    expect(visibleAfter).toEqual([]);
  });

  test('search filter should persist after uploading a new clip', async ({ app }) => {
    const fileAlpha = await createNamedTempFile(
      generateTestImage(10, 10, [255, 0, 0]),
      'alpha-upload.png'
    );
    const fileBravo = await createNamedTempFile(
      generateTestImage(10, 10, [0, 255, 0]),
      'bravo-upload.png'
    );

    await app.uploadFile(fileAlpha);
    await app.uploadFile(fileBravo);
    await app.expectClipCount(2);

    // Search for "alpha" -- only alpha-upload.png should be visible
    await app.search('alpha');
    const visibleBefore = await getVisibleClipFilenames(app);
    expect(visibleBefore).toEqual(['alpha-upload.png']);

    // Upload a new clip that does NOT match the search.
    // Can't use app.uploadFile() because it waits for a visible <li>,
    // but the new clip will be hidden by the search filter.
    const fileCharlie = await createNamedTempFile(
      generateTestImage(10, 10, [0, 0, 255]),
      'charlie-upload.png'
    );
    const fileInput = app.page.locator('#file-input');
    await fileInput.setInputFiles(fileCharlie);

    // Wait for gallery to re-render with the new clip (3 total, some hidden)
    await app.page.waitForFunction(
      () => document.querySelectorAll('#gallery > li:not([data-folder])').length === 3,
      { timeout: 10000 }
    );

    // After upload, the search input still says "alpha"
    const searchValue = await app.page.locator(selectors.header.searchInput).inputValue();
    expect(searchValue).toBe('alpha');

    // Only alpha-upload.png should still be visible
    const visibleAfter = await getVisibleClipFilenames(app);
    expect(visibleAfter).toEqual(['alpha-upload.png']);
  });
});
