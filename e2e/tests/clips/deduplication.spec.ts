import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Clip Deduplication', () => {
  test('should show duplicate badge when same content uploaded twice', async ({ app }) => {
    const imageData = generateTestImage(50, 50, [255, 0, 0]);
    const file1 = await createTempFile(imageData, 'png');
    const file2 = await createTempFile(imageData, 'png');

    await app.uploadFile(file1);
    await app.uploadFile(file2);
    await app.expectClipCount(2);

    // Both cards should show "2 copies" badge
    const badge1 = await app.getDuplicateBadgeText(path.basename(file1));
    const badge2 = await app.getDuplicateBadgeText(path.basename(file2));
    expect(badge1).toBe('2 copies');
    expect(badge2).toBe('2 copies');
  });

  test('should not show badge for unique clips', async ({ app }) => {
    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');

    await app.uploadFile(file1);
    await app.uploadFile(file2);

    const badge = await app.getDuplicateBadgeText(path.basename(file1));
    expect(badge).toBeNull();
  });

  test('should merge duplicates from card menu', async ({ app }) => {
    const imageData = generateTestImage(50, 50, [255, 0, 0]);
    const file1 = await createTempFile(imageData, 'png');
    const file2 = await createTempFile(imageData, 'png');

    await app.uploadFile(file1);
    await app.uploadFile(file2);
    await app.expectClipCount(2);

    await app.clickMergeDuplicatesInCardMenu(path.basename(file1));
    await app.expectClipCount(1);
  });

  test('should deduplicate all from sidebar', async ({ app }) => {
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const img2 = generateTestImage(50, 50, [0, 255, 0]);

    // Create 2 duplicate groups
    await app.uploadFile(await createTempFile(img1, 'png'));
    await app.uploadFile(await createTempFile(img1, 'png'));
    await app.uploadFile(await createTempFile(img2, 'png'));
    await app.uploadFile(await createTempFile(img2, 'png'));
    await app.expectClipCount(4);

    await app.clickDeduplicateAll();
    await app.confirmDialog();
    await app.expectClipCount(2);
  });
});
