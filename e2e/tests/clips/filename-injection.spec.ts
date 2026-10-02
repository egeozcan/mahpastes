import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

/**
 * A clip's filename and content type are not trusted input: a followed P2P
 * share delivers both verbatim from the remote publisher, and a server-mode
 * editor key can rename any clip. The card template builds its markup with
 * innerHTML, so every interpolation of either value must be escaped — one
 * raw attribute is enough to run script in the webview, which exposes every
 * bound Go method.
 */
test.describe('Clip card filename injection', () => {
  const hostile = 'x" data-pwned="1 <img id=pwn-card src=x>.<i>';

  test('a hostile filename renders as text, never as markup', async ({ app }) => {
    // An unknown extension keeps the type badge on the filename-derived path
    // (getFriendlyFileType falls back to the last ≤5-char extension).
    const filePath = await createTempFile(Buffer.from([0, 1, 2, 3, 4, 5, 6, 7]), 'zzq');
    await app.uploadFile(filePath);
    await app.expectClipVisible(path.basename(filePath));

    const clipId = await app.page.evaluate(async ({ name, newName }) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === name);
      // @ts-ignore - Wails runtime
      await window.go.main.App.RenameClip(clip.id, newName);
      return clip.id;
    }, { name: path.basename(filePath), newName: hostile });

    await app.refreshClips();

    const card = app.page.locator(selectors.gallery.clipCardById(String(clipId)));
    await expect(card).toBeVisible();

    // Nothing the filename carried became an element or an attribute.
    expect(await app.page.locator('#pwn-card').count()).toBe(0);
    expect(await app.page.locator('[data-pwned]').count()).toBe(0);
    expect(await card.locator('i').count()).toBe(0);

    // The accessible names still carry the literal filename.
    await expect(card.locator('.clip-checkbox')).toHaveAttribute('aria-label', `Select clip ${hostile}`);
    await expect(card).toHaveAttribute('aria-label', `Clip: ${hostile}`);
  });
});
