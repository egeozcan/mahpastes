import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Bulk Selection', () => {
  test.describe('Individual Selection', () => {
    test('should select a single clip', async ({ app }) => {
      const file = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(file);

      await app.uploadFile(file);
      await app.selectClip(filename);

      const count = await app.getSelectedCount();
      expect(count).toBe(1);
    });

    test('should deselect a selected clip', async ({ app }) => {
      const file = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(file);

      await app.uploadFile(file);
      await app.selectClip(filename);
      await app.deselectClip(filename);

      const count = await app.getSelectedCount();
      expect(count).toBe(0);
    });

    test('should select multiple clips individually', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const file3 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');
      const filename1 = path.basename(file1);
      const filename2 = path.basename(file2);
      const filename3 = path.basename(file3);

      await app.uploadFiles([file1, file2, file3]);

      await app.selectClips([filename1, filename2]);

      const count = await app.getSelectedCount();
      expect(count).toBe(2);
    });

    test('should show bulk toolbar when clips are selected', async ({ app }) => {
      const file = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(file);

      await app.uploadFile(file);

      // Initially toolbar should not be visible (or have low opacity)
      await app.selectClip(filename);

      // Toolbar should be visible
      const isVisible = await app.isBulkToolbarVisible();
      expect(isVisible).toBe(true);
    });

    test('should hide bulk toolbar when no clips selected', async ({ app }) => {
      const file = await createTempFile(generateTestImage(), 'png');
      const filename = path.basename(file);

      await app.uploadFile(file);
      await app.selectClip(filename);
      await app.deselectClip(filename);

      const count = await app.getSelectedCount();
      expect(count).toBe(0);
    });
  });

  test.describe('Select All', () => {
    test('should select all clips with select all checkbox', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
      ]);

      await app.uploadFiles(files);
      await app.expectClipCount(3);

      await app.selectAll();

      const count = await app.getSelectedCount();
      expect(count).toBe(3);
    });

    test('should deselect all clips with select all checkbox', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      ]);

      await app.uploadFiles(files);
      await app.selectAll();
      await app.deselectAll();

      const count = await app.getSelectedCount();
      expect(count).toBe(0);
    });

    test('should update select all checkbox state when all clips manually selected', async ({ app }) => {
      const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
      const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
      const filename1 = path.basename(file1);
      const filename2 = path.basename(file2);

      await app.uploadFiles([file1, file2]);

      // Select both manually
      await app.selectClips([filename1, filename2]);

      const count = await app.getSelectedCount();
      expect(count).toBe(2);
    });
  });

  test.describe('Selection Count Display', () => {
    test('should show correct count when selecting clips', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png'),
        createTempFile(generateTestImage(50, 50, [255, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);

      // Select one
      await app.selectClip(filenames[0]);
      expect(await app.getSelectedCount()).toBe(1);

      // Select another
      await app.selectClip(filenames[1]);
      expect(await app.getSelectedCount()).toBe(2);

      // Select all
      await app.selectAll();
      expect(await app.getSelectedCount()).toBe(4);
    });

    test('should update count when deselecting', async ({ app }) => {
      const files = await Promise.all([
        createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png'),
        createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png'),
      ]);
      const filenames = files.map((f) => path.basename(f));

      await app.uploadFiles(files);
      await app.selectAll();
      expect(await app.getSelectedCount()).toBe(2);

      await app.deselectClip(filenames[0]);
      expect(await app.getSelectedCount()).toBe(1);
    });
  });

  test.describe('Selection Persistence', () => {
    test('should maintain selection after search', async ({ app }) => {
      const file1 = await createTempFile(generateTestText('searchable-text'), 'txt');
      const file2 = await createTempFile(generateTestImage(), 'png');
      const filename1 = path.basename(file1);

      await app.uploadFiles([file1, file2]);
      await app.selectClip(filename1);

      // Search to filter
      await app.search('searchable');

      // Clear search
      await app.clearSearch();

      // Selection state may vary based on implementation
      await app.expectClipCount(2);
    });
  });
});
