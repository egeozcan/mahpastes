import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Metadata', () => {
  test('should open metadata modal from card menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should add a key-value pair and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('author', 'test-user');
    await app.saveMetadata();

    // Reopen and verify it persisted
    await app.openMetadataModal(filename);
    await app.expectMetadataRow('author', 'test-user');
    await app.expectMetadataRowCount(1);
    await app.closeMetadataModal();
  });

  test('should edit an existing value and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add initial metadata
    await app.openMetadataModal(filename);
    await app.addMetadataField('status', 'draft');
    await app.saveMetadata();

    // Edit the value
    await app.openMetadataModal(filename);
    const row = app.page.locator(selectors.metadata.row).first();
    await row.locator(selectors.metadata.valueInput).fill('published');
    await app.saveMetadata();

    // Verify edited value persisted
    await app.openMetadataModal(filename);
    await app.expectMetadataRow('status', 'published');
    await app.closeMetadataModal();
  });

  test('should delete a key-value pair and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add two fields
    await app.openMetadataModal(filename);
    await app.addMetadataField('key1', 'value1');
    await app.addMetadataField('key2', 'value2');
    await app.saveMetadata();

    // Delete first row
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(2);
    await app.deleteMetadataRow(0);
    await app.saveMetadata();

    // Verify only one remains
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(1);
    await app.closeMetadataModal();
  });

  test('should show empty state when no metadata exists', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should persist metadata after close and reopen', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('prompt', 'a beautiful sunset');
    await app.addMetadataField('model', 'flux-pro');
    await app.saveMetadata();

    // Reopen and check persistence
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(2);
    await app.expectMetadataRow('prompt', 'a beautiful sunset');
    await app.expectMetadataRow('model', 'flux-pro');
    await app.closeMetadataModal();
  });

  test('should show empty state after deleting all rows', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add a field
    await app.openMetadataModal(filename);
    await app.addMetadataField('temp', 'data');
    await app.saveMetadata();

    // Delete it
    await app.openMetadataModal(filename);
    await app.deleteMetadataRow(0);
    await app.saveMetadata();

    // Verify empty state
    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should not save rows with empty keys', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('', 'orphan-value');
    await app.addMetadataField('valid-key', 'valid-value');
    await app.saveMetadata();

    // Only the valid key should persist
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(1);
    await app.expectMetadataRow('valid-key', 'valid-value');
    await app.closeMetadataModal();
  });
});
