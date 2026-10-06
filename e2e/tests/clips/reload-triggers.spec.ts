import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

/**
 * Events that used to reload the whole gallery once each — a burst of shared
 * clips, a burst of tag changes, every window refocus — are coalesced or
 * skipped. Reloads are counted by wrapping the global loadClips, which every
 * caller resolves at call time.
 */

// Start counting once earlier work has settled: the previous test's cleanup
// and this test's setup (tag creation, deletes) emit events whose coalesced
// reloads land up to a few hundred ms later.
async function countLoads(app: any): Promise<void> {
  await app.page.waitForTimeout(700);
  await app.page.evaluate(() => {
    const w = window as any;
    if (!w.__origLoadClips) w.__origLoadClips = w.loadClips;
    w.__loadClipsCalls = 0;
    w.loadClips = (...args: unknown[]) => {
      w.__loadClipsCalls++;
      return w.__origLoadClips(...args);
    };
  });
}

async function loads(app: any): Promise<number> {
  return app.page.evaluate(() => (window as any).__loadClipsCalls);
}

async function restoreLoads(app: any): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    if (w.__origLoadClips) w.loadClips = w.__origLoadClips;
    delete w.__origLoadClips;
    delete w.__refocusReloadStaleMs;
  });
}

async function seedTextClips(app: any, names: string[]): Promise<void> {
  await app.page.evaluate(async (names: string[]) => {
    const files = names.map((name, i) => ({ name, content_type: 'text/plain', data: btoa(`reload clip ${i} ${name}`) }));
    // @ts-ignore - Wails runtime
    await window.go.main.App.UploadFiles(files, 0, 0);
    // @ts-ignore
    await loadClips();
  }, names);
}

async function refocus(app: any): Promise<void> {
  await app.page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
}

test.describe('Coalesced reload triggers', () => {
  test.afterEach(async ({ app }) => {
    await restoreLoads(app);
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('a burst of share:clip-received reloads once with one summary toast', async ({ app }) => {
    await countLoads(app);
    await app.page.evaluate(() => {
      for (let i = 0; i < 6; i++) (window as any).runtime.EventsEmit('share:clip-received', { follow_id: 1 });
    });
    await expect.poll(() => loads(app), { timeout: 3000 }).toBe(1);
    await app.expectToast('Received 6 shared clips');
    // Nothing trails behind the burst.
    await app.page.waitForTimeout(600);
    expect(await loads(app)).toBe(1);
  });

  test('a steady stream of shared clips still reloads within the max wait', async ({ app }) => {
    await countLoads(app);
    // One event every 150 ms for ~2.7 s never lets the 300 ms debounce settle.
    await app.page.evaluate(async () => {
      for (let i = 0; i < 18; i++) {
        (window as any).runtime.EventsEmit('share:clip-received', {});
        await new Promise(r => setTimeout(r, 150));
      }
    });
    expect(await loads(app)).toBeGreaterThanOrEqual(1);
    expect(await loads(app)).toBeLessThanOrEqual(3);
  });

  test('a burst of tag events reloads tags and the gallery once', async ({ app }) => {
    await app.createTag('burst');
    await countLoads(app);
    await app.page.evaluate(() => {
      const w = window as any;
      w.__loadTagsCalls = 0;
      w.__origLoadTags = w.loadTags;
      w.loadTags = (...args: unknown[]) => { w.__loadTagsCalls++; return w.__origLoadTags(...args); };
      for (let i = 0; i < 5; i++) w.runtime.EventsEmit('tag:updated', { id: 1, name: 'burst', old_name: 'burst' });
      w.runtime.EventsEmit('tag:deleted', { id: 999999, name: 'nope' });
      w.runtime.EventsEmit('tag:merged', { source_id: 999998, dest_id: 999997 });
    });
    await expect.poll(() => loads(app), { timeout: 3000 }).toBe(1);
    await app.page.waitForTimeout(400);
    expect(await loads(app)).toBe(1);
    const tagLoads = await app.page.evaluate(() => {
      const w = window as any;
      w.loadTags = w.__origLoadTags;
      return w.__loadTagsCalls;
    });
    expect(tagLoads).toBe(1);
  });

  test('deleting a folder and its parent in one burst lands on the nearest live ancestor', async ({ app }) => {
    await app.createTag('top');
    await app.createTag('top/mid');
    await app.createTag('top/mid/leaf');
    await app.toggleFolderMode();
    await app.clickFolder('top');
    await app.clickFolder('mid');
    await app.clickFolder('leaf');
    await app.expectFolderHeader('top/mid/leaf');

    await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const leaf = tags.find((t: any) => t.name === 'top/mid/leaf');
      const mid = tags.find((t: any) => t.name === 'top/mid');
      // @ts-ignore
      await window.go.main.App.DeleteTag(leaf.id);
      // @ts-ignore
      await window.go.main.App.DeleteTag(mid.id);
    });

    await app.expectFolderHeader('top');
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'true');
  });

  test('refocusing a fresh gallery does not reload it', async ({ app }) => {
    await seedTextClips(app, ['fresh-a.txt', 'fresh-b.txt']);
    await app.expectClipCount(2);
    await countLoads(app);
    await refocus(app);
    await app.page.waitForTimeout(300);
    expect(await loads(app)).toBe(0);
    await app.expectClipCount(2);
  });

  test('refocusing a stale gallery reloads it and picks up out-of-band writes', async ({ app }) => {
    await seedTextClips(app, ['stale-a.txt']);
    await app.expectClipCount(1);
    // A write that emits no frontend event (as REST, mp or a plugin would).
    await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.UploadFiles([{ name: 'stale-b.txt', content_type: 'text/plain', data: btoa('out of band') }], 0, 0);
    });
    await app.expectClipCount(1);
    await countLoads(app);
    await app.page.evaluate(() => { (window as any).__refocusReloadStaleMs = 1; });
    await app.page.waitForTimeout(20);
    await refocus(app);
    await expect.poll(() => loads(app)).toBe(1);
    await app.expectClipCount(2);
  });

  test('an expired card disappears on refocus without a reload', async ({ app }) => {
    await seedTextClips(app, ['expiring.txt', 'lasting.txt']);
    await app.expectClipCount(2);
    // Stand in for time passing: the card's expiry is now behind us.
    await app.page.locator(selectors.gallery.clipCardByName('expiring.txt')).evaluate((card: HTMLElement) => {
      card.dataset.expiresAt = new Date(Date.now() - 1000).toISOString();
    });
    await countLoads(app);
    await refocus(app);
    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('expiring.txt'))).toHaveCount(0);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('1 clip');
    expect(await loads(app)).toBe(0);
  });
});
