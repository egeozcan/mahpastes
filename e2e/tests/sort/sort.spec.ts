import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';
import * as fs from 'fs/promises';
import * as os from 'os';

/**
 * Create a temp file with a specific filename (not random).
 * Useful for testing sort-by-name with predictable ordering.
 */
async function createNamedTempFile(
  content: Buffer | string,
  filename: string
): Promise<string> {
  const filepath = path.join(os.tmpdir(), filename);
  await fs.writeFile(filepath, content);
  return filepath;
}

test.describe('Sorting', () => {
  test('should show sort button in header', async ({ app }) => {
    await expect(app.page.locator(selectors.sort.button)).toBeVisible();
  });

  test('should open sort popover on click', async ({ app }) => {
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();
  });

  test('should close sort popover on outside click', async ({ app }) => {
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();
    await app.closeSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toHaveCount(0);
  });

  test('should close sort popover on Escape key', async ({ app }) => {
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();
    await app.page.keyboard.press('Escape');
    await expect(app.page.locator(selectors.sort.popover)).toHaveCount(0);
  });

  test('should display all sort field options', async ({ app }) => {
    await app.openSortPopover();
    const popover = app.page.locator(selectors.sort.popover);

    await expect(popover.locator(selectors.sort.option('date'))).toBeVisible();
    await expect(popover.locator(selectors.sort.option('name'))).toBeVisible();
    await expect(popover.locator(selectors.sort.option('size'))).toBeVisible();
    await expect(popover.locator(selectors.sort.option('type'))).toBeVisible();

    await app.closeSortPopover();
  });

  test('should highlight active sort option', async ({ app }) => {
    // Default sort is "date", so date option should be active
    await app.openSortPopover();
    const dateOption = app.page.locator(selectors.sort.option('date'));
    await expect(dateOption).toHaveClass(/bg-stone-100/);
    await app.closeSortPopover();
  });

  test('should close popover after selecting a sort option', async ({ app }) => {
    await app.openSortPopover();
    await app.selectSort('name');
    // selectSort already asserts the popover closes
    await expect(app.page.locator(selectors.sort.popover)).toHaveCount(0);
  });

  test('should sort by filename ascending', async ({ app }) => {
    // Create files with controlled names that sort predictably
    const fileA = await createNamedTempFile(generateTestImage(10, 10, [255, 0, 0]), 'alpha-sort-test.png');
    const fileB = await createNamedTempFile(generateTestImage(10, 10, [0, 255, 0]), 'bravo-sort-test.png');
    const fileC = await createNamedTempFile(generateTestImage(10, 10, [0, 0, 255]), 'charlie-sort-test.png');

    // Upload in non-alphabetical order
    await app.uploadFile(fileC);
    await app.uploadFile(fileA);
    await app.uploadFile(fileB);
    await app.expectClipCount(3);

    // Sort by name (first click = desc)
    await app.openSortPopover();
    await app.selectSort('name');

    // Toggle to ascending (second click)
    await app.openSortPopover();
    await app.selectSort('name');

    const names = await app.getClipFilenames();
    expect(names[0]).toBe('alpha-sort-test.png');
    expect(names[1]).toBe('bravo-sort-test.png');
    expect(names[2]).toBe('charlie-sort-test.png');
  });

  test('should sort by filename descending', async ({ app }) => {
    const fileA = await createNamedTempFile(generateTestImage(10, 10, [255, 0, 0]), 'alpha-sort-desc.png');
    const fileB = await createNamedTempFile(generateTestImage(10, 10, [0, 255, 0]), 'bravo-sort-desc.png');
    const fileC = await createNamedTempFile(generateTestImage(10, 10, [0, 0, 255]), 'charlie-sort-desc.png');

    await app.uploadFile(fileA);
    await app.uploadFile(fileB);
    await app.uploadFile(fileC);
    await app.expectClipCount(3);

    // Sort by name (first click = desc)
    await app.openSortPopover();
    await app.selectSort('name');

    const names = await app.getClipFilenames();
    expect(names[0]).toBe('charlie-sort-desc.png');
    expect(names[1]).toBe('bravo-sort-desc.png');
    expect(names[2]).toBe('alpha-sort-desc.png');
  });

  test('should sort by file size', async ({ app }) => {
    // Create files with different sizes
    const small = await createNamedTempFile(
      generateTestImage(5, 5, [255, 0, 0]),
      'small-size-sort.png'
    );
    const large = await createNamedTempFile(
      generateTestImage(200, 200, [0, 0, 255]),
      'large-size-sort.png'
    );

    await app.uploadFile(small);
    await app.uploadFile(large);
    await app.expectClipCount(2);

    // Sort by size descending (first click)
    await app.openSortPopover();
    await app.selectSort('size');

    const namesDesc = await app.getClipFilenames();
    // Largest first when desc
    expect(namesDesc[0]).toBe('large-size-sort.png');
    expect(namesDesc[1]).toBe('small-size-sort.png');

    // Toggle to ascending (second click)
    await app.openSortPopover();
    await app.selectSort('size');

    const namesAsc = await app.getClipFilenames();
    // Smallest first when asc
    expect(namesAsc[0]).toBe('small-size-sort.png');
    expect(namesAsc[1]).toBe('large-size-sort.png');
  });

  test('should sort by content type', async ({ app }) => {
    const imgFile = await createTempFile(generateTestImage(), 'png');
    const txtFile = await createTempFile(generateTestText('type-sort'), 'txt');

    await app.uploadFile(imgFile);
    await app.uploadFile(txtFile);
    await app.expectClipCount(2);

    // Sort by type (first click = desc)
    await app.openSortPopover();
    await app.selectSort('type');

    const namesDesc = await app.getClipFilenames();
    // text/plain > image/png lexicographically, so text first when desc
    expect(namesDesc[0]).toBe(path.basename(txtFile));
    expect(namesDesc[1]).toBe(path.basename(imgFile));
  });

  test('should toggle direction when clicking same sort field', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);
    await app.expectClipCount(1);

    // Click name to sort desc
    await app.openSortPopover();
    await app.selectSort('name');

    // Reopen and check the direction indicator shows Desc
    await app.openSortPopover();
    const nameOption = app.page.locator(selectors.sort.option('name'));
    await expect(nameOption).toContainText('Desc');
    await app.closeSortPopover();

    // Click again to toggle to asc
    await app.openSortPopover();
    await app.selectSort('name');

    await app.openSortPopover();
    const nameOptionAsc = app.page.locator(selectors.sort.option('name'));
    await expect(nameOptionAsc).toContainText('Asc');
    await app.closeSortPopover();
  });

  test('should switch to new field with desc default', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);

    // Start with name sort (any direction)
    await app.openSortPopover();
    await app.selectSort('name');

    // Switch to size - should default to desc
    await app.openSortPopover();
    await app.selectSort('size');

    await app.openSortPopover();
    const sizeOption = app.page.locator(selectors.sort.option('size'));
    await expect(sizeOption).toHaveClass(/bg-stone-100/);
    await expect(sizeOption).toContainText('Desc');
    await app.closeSortPopover();
  });

  test('should persist sort preference across reload', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);

    // Set sort to name ascending
    await app.openSortPopover();
    await app.selectSort('name'); // desc
    await app.openSortPopover();
    await app.selectSort('name'); // asc

    // Reload the page
    await app.page.reload();
    await app.page.waitForLoadState('networkidle');

    // Verify name sort is still active with correct direction
    await app.openSortPopover();
    const nameOption = app.page.locator(selectors.sort.option('name'));
    await expect(nameOption).toHaveClass(/bg-stone-100/);
    await expect(nameOption).toContainText('Asc');
    await app.closeSortPopover();
  });

  test('should maintain sort order when new clip is added', async ({ app }) => {
    const fileA = await createNamedTempFile(generateTestImage(10, 10, [255, 0, 0]), 'aaa-maintain.png');
    const fileC = await createNamedTempFile(generateTestImage(10, 10, [0, 0, 255]), 'ccc-maintain.png');

    await app.uploadFile(fileA);
    await app.uploadFile(fileC);
    await app.expectClipCount(2);

    // Sort by name ascending
    await app.openSortPopover();
    await app.selectSort('name'); // desc
    await app.openSortPopover();
    await app.selectSort('name'); // asc

    // Upload a file that goes in the middle alphabetically
    const fileB = await createNamedTempFile(generateTestImage(10, 10, [0, 255, 0]), 'bbb-maintain.png');
    await app.uploadFile(fileB);
    await app.expectClipCount(3);

    const names = await app.getClipFilenames();
    expect(names[0]).toBe('aaa-maintain.png');
    expect(names[1]).toBe('bbb-maintain.png');
    expect(names[2]).toBe('ccc-maintain.png');
  });
});
