import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Every gallery listing used to stop at 50 clips with no way to see the rest:
 * the footer said "50 clips" with 120 in the library, and a folder card that
 * counted 120 opened onto 50.
 */

async function seedTextClips(app: any, count: number, autoTagID = 0): Promise<void> {
  await app.page.evaluate(async ({ count, autoTagID }: { count: number; autoTagID: number }) => {
    const files = [];
    for (let i = 0; i < count; i++) {
      const name = `paged-${String(i).padStart(3, '0')}.txt`;
      files.push({ name, content_type: 'text/plain', data: btoa(`paged clip ${i}`) });
    }
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles(files, 0, autoTagID);
  }, { count, autoTagID });
}

async function reload(app: any): Promise<void> {
  await app.page.evaluate(async () => {
    // @ts-ignore
    await loadClips();
  });
}

test.describe('Gallery paging', () => {
  test('shows "50 of N" and loads the rest on demand', async ({ app }) => {
    await seedTextClips(app, 120);
    await reload(app);

    await app.expectClipCount(50);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('50 of 120 clips');

    const loadMore = app.page.locator(selectors.gallery.loadMoreButton);
    await expect(loadMore).toBeVisible();
    await loadMore.click();
    await app.expectClipCount(100);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('100 of 120 clips');

    await loadMore.click();
    await app.expectClipCount(120);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('120 clips');
    await expect(loadMore).toBeHidden();

    // No clip is rendered twice across pages.
    const ids = await app.page.locator(selectors.gallery.clipCard).evaluateAll(
      (cards: Element[]) => cards.map(c => (c as HTMLElement).dataset.id));
    expect(new Set(ids).size).toBe(120);
  });

  test('a reload keeps the clips already loaded', async ({ app }) => {
    await seedTextClips(app, 70);
    await reload(app);
    await app.page.locator(selectors.gallery.loadMoreButton).click();
    await app.expectClipCount(70);

    await reload(app);
    await app.expectClipCount(70);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('70 clips');
  });

  test('opening a folder with more than a page of clips can load them all', async ({ app }) => {
    const { tagID } = await app.createTag('big');
    await seedTextClips(app, 60, tagID);
    await app.toggleFolderMode();
    await expect(app.page.locator(selectors.tags.folderCard('big'))).toContainText('60 clips');
    await app.clickFolder('big');

    await app.expectClipCount(50);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('50 of 60 clips');
    await app.page.locator(selectors.gallery.loadMoreButton).click();
    await app.expectClipCount(60);
  });

  test('Load more after the listing shrank behind the gallery skips no clip', async ({ app }) => {
    await seedTextClips(app, 60);
    await reload(app);
    await app.expectClipCount(50);
    // Newest first: paged-059 … paged-010 are on screen. Delete a shown clip
    // without reloading, as the expiry reaper or a REST client would.
    await app.page.evaluate(async () => {
      const api = (window as any).go.main.App;
      const page = await api.ListClipsPage({ mode: 'all', offset: 0, limit: 1 });
      await api.DeleteClip(page.clips[0].id);
    });
    await app.page.locator(selectors.gallery.loadMoreButton).click();
    await app.expectClipCount(59);
    await expect(app.page.locator(selectors.gallery.clipCardByName('paged-009.txt'))).toHaveCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('paged-059.txt'))).toHaveCount(0);
  });

  test('a multi-request reload never renders a clip twice', async ({ app }) => {
    await seedTextClips(app, 260);
    await reload(app);
    for (let i = 0; i < 4; i++) {
      await app.page.locator(selectors.gallery.loadMoreButton).click();
      await app.expectClipCount(100 + i * 50);
    }
    // Rows shift between the reload's two requests: the second overlaps the first.
    await app.page.evaluate(async () => {
      const api = (window as any).go.main.App;
      const original = api.ListClipsPage;
      api.ListClipsPage = (req: any) => original(req.offset > 0 ? { ...req, offset: req.offset - 3 } : req);
      try {
        // @ts-ignore - global
        await loadClips();
      } finally {
        api.ListClipsPage = original;
      }
    });
    const ids = await app.page.locator(selectors.gallery.clipCard).evaluateAll(
      (cards: Element[]) => cards.map(c => (c as HTMLElement).dataset.id));
    expect(new Set(ids).size).toBe(ids.length);
    // 250 wanted; the shifted second request overlaps the first by 3 rows.
    expect(ids.length).toBe(247);
  });

  test('Load more unchecks select-all and hands focus to the first new card on the last page', async ({ app }) => {
    await seedTextClips(app, 60);
    await reload(app);
    await app.expectClipCount(50);
    await app.selectAll();
    await expect(app.page.locator(selectors.bulk.selectAllCheckbox)).toBeChecked();

    const loadMore = app.page.locator(selectors.gallery.loadMoreButton);
    await loadMore.focus();
    await app.page.keyboard.press('Enter');
    await app.expectClipCount(60);
    await expect(app.page.locator(selectors.bulk.selectAllCheckbox)).not.toBeChecked();
    await expect(loadMore).toBeHidden();
    await expect.poll(() => app.page.evaluate(() =>
      Array.from(document.querySelectorAll('#gallery > li')).indexOf(document.activeElement as Element))).toBe(50);
  });

  test('Load more keeps focus on the button while more pages remain', async ({ app }) => {
    await seedTextClips(app, 160);
    await reload(app);
    await app.expectClipCount(50);

    const loadMore = app.page.locator(selectors.gallery.loadMoreButton);
    await loadMore.focus();
    await app.page.keyboard.press('Enter');
    await app.expectClipCount(100);
    await expect(loadMore).toBeVisible();
    await expect(loadMore).toBeFocused();

    await app.page.keyboard.press('Enter');
    await app.expectClipCount(150);
    await expect(loadMore).toBeFocused();
  });

  test('folder cards are not counted as clips', async ({ app }) => {
    await app.createTag('one');
    await app.createTag('two');
    await app.createTag('three');
    await app.toggleFolderMode();
    await app.expectFolderVisible('three');
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('0 clips');
  });
});
