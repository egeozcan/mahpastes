import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Archiving or restoring a clip from its card patches the gallery in place
 * (the card is removed, counts drop) only when the clip certainly left the
 * view it was toggled from. Two cases where it did not:
 *
 * - a clip:archived plugin handler runs inside ToggleArchive and can
 *   unarchive the clip again before the call returns;
 * - the user switches between the active and archive views while the call is
 *   in flight, so the card with that id now belongs to the other listing.
 */

async function seedTextClips(app: any, names: string[]): Promise<void> {
  await app.page.evaluate(async (names: string[]) => {
    const files = names.map((name, i) => ({ name, content_type: 'text/plain', data: btoa(`archive patch ${i} ${name}`) }));
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles(files, 0, 0);
    // @ts-ignore
    await loadClips();
  }, names);
}

async function renderSeq(app: any): Promise<number> {
  return app.page.evaluate(() => (window as any).__galleryRenderSeq || 0);
}

async function waitRenderAfter(app: any, seq: number): Promise<void> {
  await app.page.waitForFunction((s: number) => ((window as any).__galleryRenderSeq || 0) > s, seq, { timeout: 5000 });
}

async function restoreToggleArchive(app: any): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    if (w.__origToggleArchive) w.go.main.App.ToggleArchive = w.__origToggleArchive;
    delete w.__origToggleArchive;
    if (w.__releaseArchive) w.__releaseArchive();
    delete w.__releaseArchive;
  });
}

test.describe('Archive toggle patch', () => {
  test.afterEach(async ({ app }) => {
    await restoreToggleArchive(app);
  });

  test('keeps the card when a plugin handler unarchives the clip inside the call', async ({ app }) => {
    await seedTextClips(app, ['bounce-a.txt', 'bounce-b.txt']);
    await app.expectClipCount(2);
    // Stand-in for a clip:archived handler that unarchives the clip again
    // before ToggleArchive returns.
    await app.page.evaluate(() => {
      const w = window as any;
      const original = w.go.main.App.ToggleArchive;
      w.__origToggleArchive = original;
      w.go.main.App.ToggleArchive = async (id: number) => {
        await original(id);
        await original(id);
      };
    });

    const before = await renderSeq(app);
    await app.archiveClip('bounce-a.txt');
    await waitRenderAfter(app, before);

    // The backend still holds it active, so the active view still lists it.
    await app.expectClipVisible('bounce-a.txt');
    await app.expectClipCount(2);
    const archived = await app.page.evaluate(async () => {
      const ids = Array.from(document.querySelectorAll('#gallery > li[data-filename="bounce-a.txt"]'))
        .map((li: any) => Number(li.dataset.id));
      // @ts-ignore
      const row = await window.go.main.App.GetClipPreview(ids[0]);
      return !!row.is_archived;
    });
    expect(archived).toBe(false);
  });

  test('does not remove the card from the view switched to while the call was in flight', async ({ app }) => {
    await seedTextClips(app, ['moving.txt', 'staying.txt']);
    await app.expectClipCount(2);
    // The write lands at once; the call's return is held until released.
    await app.page.evaluate(() => {
      const w = window as any;
      const original = w.go.main.App.ToggleArchive;
      w.__origToggleArchive = original;
      const gate = new Promise<void>(resolve => { w.__releaseArchive = resolve; });
      w.go.main.App.ToggleArchive = async (id: number) => {
        await original(id);
        await gate;
      };
    });

    await app.archiveClip('moving.txt');
    // Switch to the archive view while ToggleArchive has not returned: the
    // freshly archived clip is listed there.
    await app.toggleArchiveView();
    await app.expectClipVisible('moving.txt');
    await app.expectClipCount(1);

    const before = await renderSeq(app);
    await app.page.evaluate(() => (window as any).__releaseArchive());
    await waitRenderAfter(app, before);

    // The archive view's card must survive the late completion.
    await app.expectClipVisible('moving.txt');
    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('staying.txt'))).toHaveCount(0);
  });
});
