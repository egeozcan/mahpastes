import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * In-place card patches (refreshClipInPlace) apply only their newest response:
 * an older GetClipPreview answer that lands after a newer patch of the same
 * clip, or after a reload, must not repaint the card with older state.
 *
 * And a refocus that finds the library unchanged re-checks the change counter
 * once the expiry reaper has had a chance to run, so a clip that expired off
 * screen (an unloaded page, a subfolder) still updates the summaries.
 */

async function seedTextClip(app: any, name: string): Promise<number> {
  return app.page.evaluate(async (name: string) => {
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles([{ name, content_type: 'text/plain', data: btoa(`order ${name}`) }], 0, 0);
    // @ts-ignore
    await loadClips();
    const card = document.querySelector(`#gallery > li[data-filename="${name.toLowerCase()}"]`) as HTMLElement;
    return Number(card.dataset.id);
  }, name);
}

// The next GetClipPreview call reads the row now but answers only when
// window.__releasePreview() is called.
async function holdNextPreview(app: any): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    if (!w.__origGetClipPreview) w.__origGetClipPreview = w.go.main.App.GetClipPreview;
    let held = false;
    w.__previewHeld = false;
    w.go.main.App.GetClipPreview = async (...args: unknown[]) => {
      const row = await w.__origGetClipPreview(...args);
      if (held) return row;
      held = true;
      w.__previewHeld = true;
      await new Promise(resolve => { w.__releasePreview = resolve; });
      return row;
    };
  });
}

async function restore(app: any): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    if (w.__origGetClipPreview) w.go.main.App.GetClipPreview = w.__origGetClipPreview;
    delete w.__origGetClipPreview;
    if (w.__origGetLibraryVersion) w.go.main.App.GetLibraryVersion = w.__origGetLibraryVersion;
    delete w.__origGetLibraryVersion;
    if (w.__origLoadClips) w.loadClips = w.__origLoadClips;
    delete w.__origLoadClips;
    delete w.__refocusRecheckMs;
  });
}

test.describe('In-place patch ordering', () => {
  test.afterEach(async ({ app }) => {
    await restore(app);
  });

  test('a cancelled expiry stays cancelled when the older set-expiry preview lands last', async ({ app }) => {
    const id = await seedTextClip(app, 'expiry-order.txt');
    const card = app.page.locator(selectors.gallery.clipCardByName('expiry-order.txt'));
    await holdNextPreview(app);

    await app.page.evaluate((id: number) => {
      // @ts-ignore - app global; not awaited, its preview is held
      (window as any).__setDone = setExpiration(id, 60);
    }, id);
    await expect.poll(() => app.page.evaluate(() => (window as any).__previewHeld)).toBe(true);

    await app.page.evaluate(async (id: number) => {
      // @ts-ignore - app global
      await cancelExpiration(id);
    }, id);
    await expect(card).not.toHaveAttribute('data-expires-at', /.+/);

    await app.page.evaluate(async () => {
      const w = window as any;
      w.__releasePreview();
      await w.__setDone;
    });
    await expect(card).not.toHaveAttribute('data-expires-at', /.+/);
    await expect(card.locator('.clip-expiration-badge')).toHaveCount(0);
  });

  test('a preview answered after a reload does not repaint the card with older data', async ({ app }) => {
    const id = await seedTextClip(app, 'before-reload.txt');
    await holdNextPreview(app);

    await app.page.evaluate(async (id: number) => {
      const w = window as any;
      await w.go.main.App.RenameClip(id, 'first-name.txt');
      // @ts-ignore - app global; its preview reads 'first-name.txt' and is held
      w.__patchDone = refreshClipInPlace(id);
    }, id);
    await expect.poll(() => app.page.evaluate(() => (window as any).__previewHeld)).toBe(true);

    await app.page.evaluate(async (id: number) => {
      const w = window as any;
      await w.go.main.App.RenameClip(id, 'second-name.txt');
      // @ts-ignore - app global
      await loadClips();
    }, id);
    await expect(app.page.locator(selectors.gallery.clipCardByName('second-name.txt'))).toBeVisible();

    const patched = await app.page.evaluate(async () => {
      const w = window as any;
      w.__releasePreview();
      return w.__patchDone;
    });
    // Superseded, not failed: the caller has nothing to reload.
    expect(patched).toBe(true);
    await expect(app.page.locator(selectors.gallery.clipCardByName('second-name.txt'))).toBeVisible();
    await expect(app.page.locator(selectors.gallery.clipCardByName('first-name.txt'))).toHaveCount(0);
  });

  test('a refocus that finds nothing changed rechecks the library once the reaper has run', async ({ app }) => {
    await seedTextClip(app, 'recheck.txt');
    // Let reloads queued by earlier work settle before counting.
    await app.page.waitForTimeout(700);
    await app.page.evaluate(() => {
      const w = window as any;
      w.__refocusRecheckMs = 300;
      w.__loadClipsCalls = 0;
      if (!w.__origLoadClips) w.__origLoadClips = w.loadClips;
      w.loadClips = (...args: unknown[]) => {
        w.__loadClipsCalls++;
        return w.__origLoadClips(...args);
      };
    });

    const outcome = await app.page.evaluate(() => (window as any).refreshGalleryOnRefocus());
    expect(outcome).toBe('none');
    expect(await app.page.evaluate(() => (window as any).__loadClipsCalls)).toBe(0);

    // The reaper deleting an off-screen expired clip moves the counter
    // without any frontend event; stand that in by bumping what it reports.
    await app.page.evaluate(() => {
      const w = window as any;
      w.__origGetLibraryVersion = w.go.main.App.GetLibraryVersion;
      w.go.main.App.GetLibraryVersion = async () => (await w.__origGetLibraryVersion()) + 1;
    });
    await expect.poll(() => app.page.evaluate(() => (window as any).__loadClipsCalls), { timeout: 3000 }).toBeGreaterThanOrEqual(1);
  });
});
