import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestText } from '../../helpers/test-data';
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

  test('should show system metadata rows', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await app.openMetadataModal(filename);
    await app.expectSystemMetadataVisible();
    await app.expectSystemMetadataRowCount(4);
    await app.closeMetadataModal();
  });

  test('should show correct filename in system metadata', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    const filenameValue = await app.getSystemMetadataValue('Filename');
    expect(filenameValue).toBe(filename);
    await app.closeMetadataModal();
  });

  test('should show correct content type in system metadata', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    const typeValue = await app.getSystemMetadataValue('Type');
    expect(typeValue).toBe('image/png');
    await app.closeMetadataModal();
  });
});

test.describe('Metadata modal — load failures and stale responses', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      w.__releaseA?.();
      delete w.__releaseA;
      w.__restoreGetClipMetadata?.();
      delete w.__restoreGetClipMetadata;
    });
  });

  test('a failed load shows an error and keeps Save from wiping the stored keys', async ({ app }) => {
    const file = await createTempFile(generateTestText('meta-fail'), 'txt');
    const name = path.basename(file);
    await app.uploadFile(file);
    const clipId = Number(await app.page.locator(selectors.gallery.clipCardByName(name)).getAttribute('data-id'));
    await app.page.evaluate(async (id) => {
      const api = (window as any).go.main.App;
      await api.SetClipMetadata(id, 'keep', 'me');
      const original = api.GetClipMetadata;
      (window as any).__restoreGetClipMetadata = () => { api.GetClipMetadata = original; };
      api.GetClipMetadata = () => Promise.reject('database is locked');
    }, clipId);

    const card = app.page.locator(selectors.gallery.clipCardByName(name));
    await card.locator(selectors.clipActions.menuTrigger).click();
    await app.page.locator(selectors.cardMenu.metadata).click();

    await expect(app.page.locator('[data-testid="metadata-error"]')).toContainText('database is locked');
    await expect(app.page.locator(selectors.metadata.saveButton)).toBeDisabled();
    await app.closeMetadataModal();

    const stored = await app.page.evaluate(async (id) => {
      (window as any).__restoreGetClipMetadata();
      return (window as any).go.main.App.GetClipMetadata(id);
    }, clipId);
    expect(stored).toEqual({ keep: 'me' });
  });

  test("a slow response for the previous clip never renders into the next one", async ({ app }) => {
    const a = await createTempFile(generateTestText('meta-a'), 'txt');
    const b = await createTempFile(generateTestText('meta-b'), 'txt');
    await app.uploadFile(a);
    await app.uploadFile(b);
    const idA = Number(await app.page.locator(selectors.gallery.clipCardByName(path.basename(a))).getAttribute('data-id'));
    const idB = Number(await app.page.locator(selectors.gallery.clipCardByName(path.basename(b))).getAttribute('data-id'));
    await app.page.evaluate(async ({ idA, idB }) => {
      const api = (window as any).go.main.App;
      await api.SetClipMetadata(idA, 'owner', 'clip-a');
      await api.SetClipMetadata(idB, 'owner', 'clip-b');
      const original = api.GetClipMetadata;
      (window as any).__restoreGetClipMetadata = () => { api.GetClipMetadata = original; };
      api.GetClipMetadata = (id: number) => id === idA
        ? new Promise(resolve => {
          (window as any).__releaseA = () => {
            const result = original(id);
            resolve(result);
            return result.then(() => {}, () => {});
          };
        })
        : original(id);
    }, { idA, idB });

    const cardA = app.page.locator(selectors.gallery.clipCardByName(path.basename(a)));
    await cardA.locator(selectors.clipActions.menuTrigger).click();
    await app.page.locator(selectors.cardMenu.metadata).click();
    await app.closeMetadataModal();
    await app.openMetadataModal(path.basename(b));
    // Let A's response land, then give its (dropped) render a frame to show up.
    await app.page.evaluate(async () => {
      const w = window as any;
      const pending = w.__releaseA();
      delete w.__releaseA;
      await pending;
      await new Promise(requestAnimationFrame);
    });

    await app.expectMetadataRowCount(1);
    await expect(app.page.locator('[data-testid="metadata-value"]').first()).toHaveValue('clip-b');
    await app.closeMetadataModal();
  });
});
