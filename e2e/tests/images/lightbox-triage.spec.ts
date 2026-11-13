import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestVideo } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// Delete and Archive used to close the viewer before acting (a cancelled
// delete still lost your place), and the viewer had no keys for them: the card
// keys belong to the focused card, which the open viewer covers.
test.describe('Lightbox triage', () => {
  const overlay = (app: any) => app.page.locator(selectors.lightbox.overlay);
  const caption = (app: any) => app.page.locator(selectors.lightbox.caption);

  async function uploadImages(app: any, count: number): Promise<string[]> {
    const names: string[] = [];
    const colors: [number, number, number][] = [[255, 0, 0], [0, 255, 0], [0, 0, 255]];
    for (let i = 0; i < count; i++) {
      const filePath = await createTempFile(generateTestImage(60, 40, colors[i]), 'png');
      names.push(path.basename(filePath));
      await app.uploadFile(filePath);
    }
    return names;
  }

  test('a cancelled delete keeps the viewer on the same clip', async ({ app }) => {
    const [first] = await uploadImages(app, 2);
    await app.openLightbox(first);

    await app.page.keyboard.press('d');
    await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);
    await app.page.locator(selectors.confirm.cancelButton).click();

    await expect(overlay(app)).toHaveClass(/active/);
    await expect(caption(app)).toContainText(first);
    await app.closeLightbox();
    await app.expectClipCount(2);
  });

  test('a confirmed delete moves to the neighbouring clip, and the last one closes the viewer', async ({ app }) => {
    const [first, second] = await uploadImages(app, 2);
    await app.openLightbox(first);

    await app.page.keyboard.press('d');
    await app.page.locator(selectors.confirm.confirmButton).click();
    await expect(caption(app)).toContainText(second);
    await expect(overlay(app)).toHaveClass(/active/);
    await app.expectClipCount(1);

    await app.page.keyboard.press('d');
    await app.page.locator(selectors.confirm.confirmButton).click();
    await expect(overlay(app)).not.toHaveClass(/active/);
    await app.expectClipCount(0);
  });

  test('A archives the clip on screen and moves on', async ({ app }) => {
    const [first, second] = await uploadImages(app, 2);
    await app.openLightbox(first);

    await app.page.keyboard.press('a');
    await expect(caption(app)).toContainText(second);
    await expect(overlay(app)).toHaveClass(/active/);
    await app.closeLightbox();
    await app.expectClipCount(1);
    await app.expectClipNotVisible(first);
  });

  test('a viewer opened on a clip outside the gallery list closes instead of moving to another clip', async ({ app }) => {
    const [first, second] = await uploadImages(app, 2);
    // Hide `first` behind the plain search filter, then open it alone, as a
    // link to a clip that is not on screen does.
    await app.search(second.replace('.png', ''));
    await app.page.evaluate((name) => {
      const card = document.querySelector(`li[data-filename="${name}"]`) as any;
      (window as any).LightboxController.openSingle({ clip: card._clip, opener: null });
    }, first);
    await expect(caption(app)).toContainText(first);
    await expect(app.page.locator(selectors.lightbox.viewport)).toHaveAttribute('aria-busy', 'false');

    await app.page.keyboard.press('a');
    await expect(overlay(app)).not.toHaveClass(/active/);

    // Only the clip that was open was archived.
    await app.clearSearch();
    await app.expectClipCount(1);
    await app.expectClipVisible(second);
  });

  test('Delete from the Actions menu keeps focus inside the viewer as it moves on', async ({ app }) => {
    const [first, second] = await uploadImages(app, 2);
    await app.openLightbox(first);

    await app.page.locator('#lightbox-file-menu-trigger').click();
    await app.page.locator('[data-action="delete"]').last().click();
    await app.page.locator(selectors.confirm.confirmButton).click();

    await expect(caption(app)).toContainText(second);
    await expect.poll(() => app.page.evaluate(
      () => !!document.activeElement && document.getElementById('lightbox')!.contains(document.activeElement),
    )).toBe(true);
  });

  test('T opens the tag popover and mod+Enter the Actions menu', async ({ app }) => {
    await app.createTag('triage');
    const [first] = await uploadImages(app, 1);
    await app.openLightbox(first);

    await app.page.keyboard.press('ControlOrMeta+Enter');
    await expect(app.page.locator('#lightbox-file-menu-trigger')).toHaveAttribute('aria-expanded', 'true');
    await app.page.keyboard.press('Escape');
    await expect(overlay(app)).toHaveClass(/active/);

    await app.page.keyboard.press('t');
    await expect(app.page.locator('[data-testid="tag-popover"], .tag-popover').first()).toBeVisible();
    await expect(overlay(app)).toHaveClass(/active/);
  });

  test('Space right after opening a video plays it instead of closing the viewer', async ({ app }) => {
    const videoPath = await createTempFile(generateTestVideo(), 'mp4');
    await app.uploadFile(videoPath);
    await app.openLightbox(path.basename(videoPath));
    const video = app.page.locator(selectors.lightbox.video);
    await video.evaluate((element: HTMLVideoElement) => { element.loop = true; element.muted = true; });
    await expect(app.page.locator(selectors.lightbox.closeButton)).toBeFocused();

    await app.page.keyboard.press('Space');
    await expect(overlay(app)).toHaveClass(/active/);
    await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(false);

    await app.page.keyboard.press('Space');
    await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.paused)).toBe(true);
    await expect(overlay(app)).toHaveClass(/active/);
  });
});
