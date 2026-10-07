import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { generateTestImage } from '../../helpers/test-data';

/**
 * A change to one clip (delete, archive, rename, expiry, a tag on its card)
 * is patched into the gallery instead of rebuilding every card, and a full
 * reload keeps the cards whose clip did not change. Each card is marked with
 * a JS property: a rebuilt card is a new node and loses it.
 */

async function seedTextClips(app: any, names: string[], autoTagID = 0): Promise<void> {
  await app.page.evaluate(async ({ names, autoTagID }: { names: string[]; autoTagID: number }) => {
    const files = names.map((name, i) => ({ name, content_type: 'text/plain', data: btoa(`patch clip ${i} ${name}`) }));
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles(files, 0, autoTagID);
  }, { names, autoTagID });
  await reload(app);
}

async function reload(app: any): Promise<void> {
  await app.page.evaluate(async () => {
    // @ts-ignore
    await loadClips();
  });
}

async function markCards(app: any): Promise<void> {
  await app.page.evaluate(() => {
    document.querySelectorAll('#gallery > li[data-id]').forEach((li: any) => { li.__patchMark = true; });
  });
}

// Filenames of cards still carrying the mark, i.e. the same DOM node.
async function markedNames(app: any): Promise<string[]> {
  return app.page.locator(selectors.gallery.clipCard).evaluateAll(
    (cards: any[]) => cards.filter(c => c.__patchMark).map(c => c.dataset.filename).sort());
}

async function cardTagsFor(app: any, name: string): Promise<string[]> {
  return app.page.locator(selectors.gallery.clipCardByName(name)).locator('.clip-tags button').evaluateAll(
    (pills: any[]) => pills.map(p => p.title));
}

async function openTagPopoverFor(app: any, name: string): Promise<void> {
  await app.openCardMenu(name);
  await app.page.locator(selectors.cardMenu.tags).click();
}

test.describe('Gallery in-place patches', () => {
  test.afterEach(async ({ app }) => {
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('deleting one clip keeps the other cards and updates the count', async ({ app }) => {
    await seedTextClips(app, ['keep-a.txt', 'gone.txt', 'keep-b.txt']);
    await app.expectClipCount(3);
    await markCards(app);

    await app.deleteClip('gone.txt');

    await app.expectClipCount(2);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('2 clips');
    expect(await markedNames(app)).toEqual(['keep-a.txt', 'keep-b.txt']);
  });

  test('deleting a focused card hands focus to its neighbour', async ({ app }) => {
    await seedTextClips(app, ['f-1.txt', 'f-2.txt', 'f-3.txt']);
    await app.expectClipCount(3);
    const cards = app.page.locator(selectors.gallery.clipCard);
    const middle = await cards.nth(1).getAttribute('data-filename');
    const next = await cards.nth(2).getAttribute('data-filename');
    await cards.nth(1).focus();
    await app.page.keyboard.press('d');
    await app.page.locator(selectors.confirm.confirmButton).click();

    await app.expectClipCount(2);
    await expect(app.page.locator(selectors.gallery.clipCardByName(middle!))).toHaveCount(0);
    await expect.poll(() => app.page.evaluate(() => (document.activeElement as HTMLElement)?.dataset?.filename))
      .toBe(next);
  });

  test('renaming a clip patches its card and keeps the others', async ({ app }) => {
    await seedTextClips(app, ['ren-a.txt', 'ren-b.txt', 'ren-c.txt']);
    await app.expectClipCount(3);
    await markCards(app);

    await app.openCardMenu('ren-b.txt');
    await app.page.locator(selectors.cardMenu.rename).click();
    await app.page.locator(selectors.prompt.input).fill('ren-renamed.txt');
    await app.page.locator(selectors.prompt.saveButton).click();

    await app.expectClipVisible('ren-renamed.txt');
    const renamed = app.page.locator(selectors.gallery.clipCardByName('ren-renamed.txt'));
    await expect(renamed.locator('.clip-filename')).toHaveText('ren-renamed.txt');
    await expect(renamed).toHaveAttribute('aria-label', 'Clip: ren-renamed.txt');
    // Every card, the renamed one included, is the same node as before.
    expect(await markedNames(app)).toEqual(['ren-a.txt', 'ren-c.txt', 'ren-renamed.txt']);

    // The listing agrees after a real reload, which still reuses the cards.
    await reload(app);
    await app.expectClipVisible('ren-renamed.txt');
    expect(await markedNames(app)).toEqual(['ren-a.txt', 'ren-c.txt', 'ren-renamed.txt']);
  });

  test('archiving clips keeps the others and shows the empty state at the end', async ({ app }) => {
    await seedTextClips(app, ['arc-a.txt', 'arc-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);

    await app.archiveClip('arc-a.txt');
    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('1 clip');
    expect(await markedNames(app)).toEqual(['arc-b.txt']);
    await expect(app.page.locator(selectors.gallery.emptyState)).toBeHidden();

    await app.archiveClip('arc-b.txt');
    await app.expectClipCount(0);
    await expect(app.page.locator(selectors.gallery.emptyState)).toHaveText('No active clips. Paste or drop something!');
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('0 clips');
  });

  test('a selected clip that is deleted leaves the selection', async ({ app }) => {
    await seedTextClips(app, ['sel-a.txt', 'sel-b.txt']);
    await app.expectClipCount(2);
    await app.selectClip('sel-a.txt');
    await app.selectClip('sel-b.txt');
    expect(await app.getSelectedCount()).toBe(2);

    await app.deleteClip('sel-a.txt');
    await app.expectClipCount(1);
    // @ts-ignore - app.js global
    await expect.poll(() => app.page.evaluate(() => Array.from(selectedIds))).toHaveLength(1);
    await expect(app.page.locator(selectors.bulk.selectedCount)).toHaveText('1 selected');
  });

  test('deleting with more pages keeps "N of M" and Load more consistent', async ({ app }) => {
    const names = Array.from({ length: 55 }, (_, i) => `pg-${String(i).padStart(3, '0')}.txt`);
    await seedTextClips(app, names);
    await app.expectClipCount(50);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('50 of 55 clips');
    await markCards(app);

    const first = await app.page.locator(selectors.gallery.clipCard).first().getAttribute('data-filename');
    await app.deleteClip(first!);
    await app.expectClipCount(49);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('49 of 54 clips');
    expect((await markedNames(app)).length).toBe(49);

    // The next page starts where the shrunken listing says, with no clip
    // skipped or repeated.
    await app.page.locator(selectors.gallery.loadMoreButton).click();
    await app.expectClipCount(54);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('54 clips');
    const ids = await app.page.locator(selectors.gallery.clipCard).evaluateAll(
      (cards: Element[]) => cards.map(c => (c as HTMLElement).dataset.id));
    expect(new Set(ids).size).toBe(54);
  });

  test('setting and cancelling expiry patches the badge in place', async ({ app }) => {
    await seedTextClips(app, ['exp-a.txt', 'exp-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);

    await app.openCardMenu('exp-a.txt');
    await app.page.locator(selectors.cardMenu.setExpiration).click();
    await app.page.locator('.expiration-popover [role="menuitem"]', { hasText: '1h' }).click();
    const card = app.page.locator(selectors.gallery.clipCardByName('exp-a.txt'));
    await expect(card.locator('.clip-expiration-badge')).toContainText('Temp');

    await app.openCardMenu('exp-a.txt');
    await app.page.locator(selectors.cardMenu.cancelExpiration).click();
    await expect(card.locator('.clip-expiration-badge')).toHaveCount(0);
    expect(await markedNames(app)).toEqual(['exp-a.txt', 'exp-b.txt']);
  });

  test('an expiry patched in place matches the listing, so a reload keeps the card', async ({ app }) => {
    await seedTextClips(app, ['expr-a.txt', 'expr-b.txt']);
    await app.expectClipCount(2);

    await app.openCardMenu('expr-a.txt');
    await app.page.locator(selectors.cardMenu.setExpiration).click();
    await app.page.locator('.expiration-popover [role="menuitem"]', { hasText: '1h' }).click();
    const card = app.page.locator(selectors.gallery.clipCardByName('expr-a.txt'));
    await expect(card.locator('.clip-expiration-badge')).toContainText('Temp');
    // The badge carries the backend's timestamp, not the browser's clock.
    const stored = await app.page.evaluate(async () => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      return clips.find((c: any) => c.filename === 'expr-a.txt').expires_at;
    });
    await expect(card).toHaveAttribute('data-expires-at', stored);

    await markCards(app);
    await reload(app);
    expect(await markedNames(app)).toEqual(['expr-a.txt', 'expr-b.txt']);
  });

  test('a rename patch shows what the backend stored, not what was typed', async ({ app }) => {
    await seedTextClips(app, ['hook-a.txt', 'hook-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);
    // Stand-in for a clip:renamed plugin handler that renames the clip again
    // from inside the call.
    await app.page.evaluate(() => {
      // @ts-ignore
      const svc = window.go.main.App;
      const original = svc.RenameClip;
      (window as any).__origRenameClip = original;
      svc.RenameClip = async (id: number, name: string) => {
        await original(id, name);
        await original(id, 'hook-' + name);
      };
    });
    try {
      await app.openCardMenu('hook-a.txt');
      await app.page.locator(selectors.cardMenu.rename).click();
      await app.page.locator(selectors.prompt.input).fill('typed.txt');
      await app.page.locator(selectors.prompt.saveButton).click();

      await app.expectClipVisible('hook-typed.txt');
      await expect(app.page.locator(selectors.gallery.clipCardByName('typed.txt'))).toHaveCount(0);
      expect(await markedNames(app)).toEqual(['hook-b.txt', 'hook-typed.txt']);
    } finally {
      await app.page.evaluate(() => {
        // @ts-ignore
        window.go.main.App.RenameClip = (window as any).__origRenameClip;
      });
    }
  });

  test('renaming to .md reloads the card with its new type', async ({ app }) => {
    await seedTextClips(app, ['md-a.txt', 'md-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);

    await app.openCardMenu('md-a.txt');
    await app.page.locator(selectors.cardMenu.rename).click();
    await app.page.locator(selectors.prompt.input).fill('md-a.md');
    await app.page.locator(selectors.prompt.saveButton).click();

    await app.expectClipVisible('md-a.md');
    const typed = await app.page.locator(selectors.gallery.clipCardByName('md-a.md'))
      .evaluate((el: any) => el._clip.content_type);
    expect(typed).toContain('markdown');
    // The type change rebuilt that card only.
    expect(await markedNames(app)).toEqual(['md-b.txt']);
  });

  test('unchecking a tag on a card patches it in place when it stays in view', async ({ app }) => {
    await app.createTag('drop-tag');
    await seedTextClips(app, ['untag-a.txt', 'untag-b.txt']);
    await app.addTagToClip('untag-a.txt', 'drop-tag');
    await app.addTagToClip('untag-b.txt', 'drop-tag');
    await expect.poll(() => cardTagsFor(app, 'untag-a.txt')).toEqual(['drop-tag']);
    await markCards(app);

    await openTagPopoverFor(app, 'untag-a.txt');
    await app.page.locator('[data-testid="tag-popover"] [data-testid="tag-checkbox-drop-tag"]').uncheck();

    await expect.poll(() => cardTagsFor(app, 'untag-a.txt')).toEqual([]);
    expect(await cardTagsFor(app, 'untag-b.txt')).toEqual(['drop-tag']);
    expect(await markedNames(app)).toEqual(['untag-a.txt', 'untag-b.txt']);
  });

  test('unchecking the filtered tag on a card removes the card', async ({ app }) => {
    await app.createTag('filt-tag');
    await seedTextClips(app, ['ft-a.txt', 'ft-b.txt']);
    await app.addTagToClip('ft-a.txt', 'filt-tag');
    await app.addTagToClip('ft-b.txt', 'filt-tag');
    await app.filterByTag('filt-tag');
    await app.expectClipCount(2);
    await markCards(app);

    await openTagPopoverFor(app, 'ft-a.txt');
    await app.page.locator('[data-testid="tag-popover"] [data-testid="tag-checkbox-filt-tag"]').uncheck();

    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('ft-a.txt'))).toHaveCount(0);
    expect(await markedNames(app)).toEqual(['ft-b.txt']);
  });

  test('an image card keeps its <img> and thumbnail across a reload', async ({ app }) => {
    const files = [
      { name: 'img-a.png', data: generateTestImage(40, 40, [200, 10, 10]).toString('base64') },
      { name: 'img-b.png', data: generateTestImage(40, 40, [10, 200, 10]).toString('base64') },
    ];
    await app.page.evaluate(async (files) => {
      // @ts-ignore
      await window.go.main.App.UploadFiles(files.map(f => ({ ...f, content_type: 'image/png' })), 0, 0);
    }, files);
    await reload(app);
    const img = app.page.locator(selectors.gallery.clipCardByName('img-a.png')).locator('img[data-thumbnail="true"]');
    await expect(img).toHaveAttribute('src', /.+/);
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
    const src = await img.getAttribute('src');
    await img.evaluate((el: any) => { el.__imgMark = true; });

    await reload(app);
    await reload(app);
    expect(await img.evaluate((el: any) => !!el.__imgMark)).toBe(true);
    await expect(img).toHaveAttribute('src', src!);
  });

  test('a card whose media failed is rebuilt by the next reload', async ({ app }) => {
    await seedTextClips(app, ['fail-a.txt', 'fail-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);
    await app.page.locator(selectors.gallery.clipCardByName('fail-a.txt')).evaluate((el: any) => { el._mediaFailed = true; });

    await reload(app);
    expect(await markedNames(app)).toEqual(['fail-b.txt']);
  });

  test('adding a tag on a card re-renders its pills in place', async ({ app }) => {
    await app.createTag('patch-tag');
    await seedTextClips(app, ['tag-a.txt', 'tag-b.txt']);
    await app.expectClipCount(2);
    await markCards(app);

    await openTagPopoverFor(app, 'tag-a.txt');
    await app.page.locator('[data-testid="tag-popover"] [data-testid="tag-checkbox-patch-tag"]').check();

    await expect.poll(() => cardTagsFor(app, 'tag-a.txt')).toEqual(['patch-tag']);
    expect(await markedNames(app)).toEqual(['tag-a.txt', 'tag-b.txt']);
  });

  test('a tag change that moves a clip out of the filter still removes its card', async ({ app }) => {
    await app.createTag('proj/a');
    await app.createTag('proj/b');
    await seedTextClips(app, ['move-me.txt', 'stay.txt']);
    await app.addTagToClip('move-me.txt', 'proj/a');
    await app.addTagToClip('stay.txt', 'proj/a');
    await app.filterByTag('proj/a');
    await app.expectClipCount(2);
    await markCards(app);

    // Same tree: adding proj/b drops proj/a, so the clip leaves this filter.
    await openTagPopoverFor(app, 'move-me.txt');
    await app.page.locator('[data-testid="tag-popover"] [data-testid="tag-checkbox-proj/b"]').check();

    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('move-me.txt'))).toHaveCount(0);
    // The reload that removed it kept the unchanged card.
    expect(await markedNames(app)).toEqual(['stay.txt']);
  });

  test('a full reload keeps unchanged cards and rebuilds a changed one', async ({ app }) => {
    await seedTextClips(app, ['re-a.txt', 're-b.txt', 're-c.txt']);
    await app.expectClipCount(3);
    await markCards(app);

    // Renamed behind the gallery's back: only its card is rebuilt.
    await app.page.evaluate(async () => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename === 're-b.txt');
      // @ts-ignore
      await window.go.main.App.RenameClip(clip.id, 're-b2.txt');
    });
    await reload(app);

    await app.expectClipVisible('re-b2.txt');
    expect(await markedNames(app)).toEqual(['re-a.txt', 're-c.txt']);

    // Bulk actions still reload; the selection they clear is not left
    // checked on a reused card.
    await app.selectClip('re-a.txt');
    await app.page.evaluate(() => {
      // @ts-ignore
      selectedIds.clear();
    });
    await reload(app);
    await expect(app.page.locator(selectors.gallery.clipCardByName('re-a.txt')).locator('.clip-checkbox')).not.toBeChecked();
  });
});
