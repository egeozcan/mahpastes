import { test, expect } from '../../fixtures/test-fixtures.js';
import { selectors } from '../../helpers/selectors.js';
import { generateTestImage, createTempFile } from '../../helpers/test-data.js';
import * as path from 'path';

// A press that starts inside the content and is released over the backdrop
// produces a click targeted at the backdrop (the common ancestor). Only a
// press that both starts and ends on the backdrop may close the viewer.

async function uploadImage(app: any): Promise<string> {
  const imagePath = await createTempFile(generateTestImage(200, 150, '#336699'), 'png');
  await app.uploadFile(imagePath);
  await app.expectClipCount(1);
  return path.basename(imagePath);
}

test.describe('Backdrop drags', () => {
  test('dragging from the lightbox image onto the backdrop keeps the lightbox open', async ({ app }) => {
    const name = await uploadImage(app);
    await app.openLightbox(name);

    const img = await app.page.locator(selectors.lightbox.image).boundingBox();
    const viewport = await app.page.locator(selectors.lightbox.viewport).boundingBox();
    if (!img || !viewport) throw new Error('lightbox not laid out');

    await app.page.mouse.move(img.x + img.width / 2, img.y + img.height / 2);
    await app.page.mouse.down();
    await app.page.mouse.move(viewport.x + 8, viewport.y + 8, { steps: 4 });
    await app.page.mouse.up();
    await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);

    // Pressing on the backdrop and releasing over the bottom bar is not a
    // backdrop click either.
    const bar = await app.page.locator(selectors.lightbox.bar).boundingBox();
    if (!bar) throw new Error('lightbox bar not laid out');
    await app.page.mouse.move(viewport.x + 8, viewport.y + 8);
    await app.page.mouse.down();
    await app.page.mouse.move(bar.x + 2, bar.y + bar.height / 2, { steps: 4 });
    await app.page.mouse.up();
    await expect(app.page.locator(selectors.lightbox.overlay)).toHaveClass(/active/);

    // A real backdrop click still closes it.
    await app.page.mouse.click(viewport.x + 8, viewport.y + 8);
    await expect(app.page.locator(selectors.lightbox.overlay)).not.toHaveClass(/active/);
  });

  test('dragging from the editor canvas onto the backdrop keeps the editor open', async ({ app }) => {
    const name = await uploadImage(app);
    await app.openImageEditor(name);

    const canvas = await app.page.locator(selectors.editor.canvas).boundingBox();
    if (!canvas) throw new Error('canvas not laid out');

    await app.page.mouse.move(canvas.x + canvas.width / 2, canvas.y + canvas.height / 2);
    await app.page.mouse.down();
    await app.page.mouse.move(3, 3, { steps: 4 });
    await app.page.mouse.up();

    await expect(app.page.locator(selectors.editor.modal)).toHaveClass(/active/);
    await expect(app.page.locator(selectors.confirm.dialog)).not.toHaveClass(/opacity-100/);
  });
});
