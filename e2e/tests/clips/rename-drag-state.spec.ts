import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * A rename is patched into its card instead of reloading the gallery. The
 * reload used to clear every prepared drag-out item; the patch must drop the
 * renamed clip's own item, or a drag hands the drop target the old filename
 * (the DownloadURL payload names the file).
 */

async function seedTextClip(app: any, name: string): Promise<number> {
  const id = await app.page.evaluate(async (name: string) => {
    const files = [{ name, content_type: 'text/plain', data: btoa(`rename drag ${name}`) }];
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles(files, 0, 0);
    // @ts-ignore
    await loadClips();
    const card = document.querySelector(`#gallery > li[data-filename="${name}"]`) as HTMLElement | null;
    return card ? Number(card.dataset.id) : 0;
  }, name);
  expect(id).toBeGreaterThan(0);
  return id;
}

async function renameViaMenu(app: any, from: string, to: string): Promise<void> {
  await app.openCardMenu(from);
  await app.page.locator(selectors.cardMenu.rename).click();
  await app.page.locator(selectors.prompt.input).fill(to);
  await app.page.locator(selectors.prompt.saveButton).click();
  await app.expectClipVisible(to);
}

test.describe('Rename patch and drag-out state', () => {
  test('a patched rename drops the clip\'s prepared drag item', async ({ app }) => {
    const id = await seedTextClip(app, 'drag-before.txt');

    const before = await app.page.evaluate(async (id: number) => {
      // @ts-ignore
      const item = await window.__testHelpers.prepareDragForTest(id);
      return item?.filename;
    }, id);
    expect(before).toBe('drag-before.txt');

    // Mark the card: the rename must be patched in, not reloaded (a reload
    // would clear drag state on its own and hide the bug).
    await app.page.evaluate(() => {
      document.querySelectorAll('#gallery > li[data-id]').forEach((li: any) => { li.__dragMark = true; });
    });

    await renameViaMenu(app, 'drag-before.txt', 'drag-after.txt');

    const card = app.page.locator(selectors.gallery.clipCardByName('drag-after.txt'));
    await expect.poll(() => card.evaluate((li: any) => !!li.__dragMark)).toBe(true);

    await expect.poll(() => app.page.evaluate((id: number) => {
      // @ts-ignore
      const item = window.__testHelpers.getPreparedDragItemForTest(id);
      return item ? item.filename : null;
    }, id)).toBeNull();

    const after = await app.page.evaluate(async (id: number) => {
      // @ts-ignore
      const item = await window.__testHelpers.prepareDragForTest(id);
      return item?.filename;
    }, id);
    expect(after).toBe('drag-after.txt');
  });

  test('a preparation in flight across a rename does not repopulate the old filename', async ({ app }) => {
    const id = await seedTextClip(app, 'flight-before.txt');

    // Hold the first PrepareClipForTransfer answer until after the rename,
    // then let it resolve with the pre-rename item.
    const result = await app.page.evaluate(async (id: number) => {
      // @ts-ignore
      const service = window.go.main.TransferService;
      const original = service.PrepareClipForTransfer;
      let release: () => void = () => {};
      const gate = new Promise<void>((r) => { release = r; });
      let calls = 0;
      service.PrepareClipForTransfer = async (req: any) => {
        calls++;
        const item = await original.call(service, req);
        if (calls === 1) await gate;
        return item;
      };
      try {
        // @ts-ignore
        const pending = window.__testHelpers.prepareDragForTest(id);
        // Let the first call read the clip under its old name.
        await new Promise((r) => setTimeout(r, 200));
        // @ts-ignore
        await window.go.main.App.RenameClip(id, 'flight-after.txt');
        // @ts-ignore
        window.__testHelpers.invalidatePreparedDragItemForTest(id);
        release();
        const item = await pending;
        // @ts-ignore
        const cached = window.__testHelpers.getPreparedDragItemForTest(id);
        return { returned: item?.filename, cached: cached ? cached.filename : null, calls };
      } finally {
        service.PrepareClipForTransfer = original;
      }
    }, id);

    expect(result.returned).toBe('flight-after.txt');
    expect(result.cached).toBe('flight-after.txt');
    expect(result.calls).toBe(2);
  });
});
