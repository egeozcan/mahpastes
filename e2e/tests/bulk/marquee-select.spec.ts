import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Helper: perform a marquee drag from (startX, startY) to (endX, endY)
async function marqueeDrag(
  page: any,
  startX: number,
  startY: number,
  endX: number,
  endY: number,
  options?: { shift?: boolean }
) {
  await page.mouse.move(startX, startY);
  if (options?.shift) {
    await page.keyboard.down('Shift');
  }
  await page.mouse.down();
  // Move in steps to trigger mousemove events and exceed threshold
  await page.mouse.move(endX, endY, { steps: 10 });
  await page.mouse.up();
  if (options?.shift) {
    await page.keyboard.up('Shift');
  }
}

// Helper: get empty space coordinates to the right of clips in the gallery
async function getGalleryEmptySpace(page: any) {
  const gallery = page.locator(selectors.gallery.container);
  const galleryBox = await gallery.boundingBox();
  // Right side of gallery is always empty when there are fewer clips than columns
  return {
    x: galleryBox!.x + galleryBox!.width - 20,
    y: galleryBox!.y + 20,
    galleryBox: galleryBox!,
  };
}

test.describe('Marquee Selection', () => {
  test('should select clips via marquee drag', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Drag from empty space on the right across all clips to the left
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // All 3 clips should be selected
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('3 selected');
  });

  test('should replace existing selection on plain marquee drag', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Select all via checkbox
    await app.selectAll();
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('3 selected');

    // Get position of the first clip only (leftmost)
    const firstClip = app.page.locator(selectors.gallery.clipCardByName(filenames[0]));
    const firstClipBox = await firstClip.boundingBox();

    const { x: emptyX } = await getGalleryEmptySpace(app.page);

    // Marquee drag that only covers the first clip
    // Start from empty space, drag left but only to the first clip
    await marqueeDrag(
      app.page,
      emptyX,
      firstClipBox!.y + firstClipBox!.height / 2,
      firstClipBox!.x + firstClipBox!.width / 2,
      firstClipBox!.y + firstClipBox!.height / 2
    );

    // Should NOT be "3 selected" anymore — plain marquee replaces
    // At least 1 clip should be selected (the one we dragged over)
    await expect(selectedCount).not.toHaveText('3 selected');
  });

  test('should add to existing selection with Shift+marquee', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Select the first clip via checkbox
    await app.selectClip(filenames[0]);
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('1 selected');

    // Get position of the last clip
    const lastClip = app.page.locator(selectors.gallery.clipCardByName(filenames[2]));
    const lastClipBox = await lastClip.boundingBox();

    const { x: emptyX } = await getGalleryEmptySpace(app.page);

    // Shift+marquee drag over the last clip only
    await marqueeDrag(
      app.page,
      emptyX,
      lastClipBox!.y + lastClipBox!.height / 2,
      lastClipBox!.x + lastClipBox!.width / 2,
      lastClipBox!.y + lastClipBox!.height / 2,
      { shift: true }
    );

    // Should now have 2 selected (first from checkbox + last from shift-marquee)
    await expect(selectedCount).toHaveText('2 selected');
  });

  test('should deselect all when clicking empty space', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    // Select all via checkbox
    await app.selectAll();
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');

    // Click on empty space (no drag — just click)
    const { x: emptyX, y: emptyY } = await getGalleryEmptySpace(app.page);
    await app.page.mouse.click(emptyX, emptyY);

    // Bulk toolbar should be hidden (no selection)
    const toolbar = app.page.locator(selectors.bulk.toolbar);
    await expect(toolbar).toHaveClass(/opacity-0/);
  });

  test('should work in folder mode', async ({ app }) => {
    // Create a tag and enable folder mode
    await app.createTag('test-folder');
    await app.enableFolderMode();

    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across all clips
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');
  });

  test('should not select filtered-out clips', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
    ]);
    const filenames = files.map((f) => path.basename(f));
    await app.uploadFiles(files);
    await app.expectClipCount(3);

    // Search for only the first file to filter out the others
    await app.search(filenames[0]);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across entire gallery width
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // Only the visible (matching) clip should be selected
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('1 selected');
  });

  test('should show bulk toolbar with correct count after marquee', async ({ app }) => {
    const files = await Promise.all([
      createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
      createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
    ]);
    await app.uploadFiles(files);
    await app.expectClipCount(2);

    const { x: emptyX, y: emptyY, galleryBox } = await getGalleryEmptySpace(app.page);

    // Marquee drag across all clips
    await marqueeDrag(
      app.page,
      emptyX,
      emptyY,
      galleryBox.x + 5,
      emptyY
    );

    // Bulk toolbar should be visible with correct count
    const toolbar = app.page.locator(selectors.bulk.toolbar);
    await expect(toolbar).toHaveClass(/opacity-100/);
    const selectedCount = app.page.locator(selectors.bulk.selectedCount);
    await expect(selectedCount).toHaveText('2 selected');
  });
});
