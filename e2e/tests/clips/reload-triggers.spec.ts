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
    if (w.__origLoadTags) w.loadTags = w.__origLoadTags;
    delete w.__origLoadTags;
    if (w.__origNavigateToFolder) w.navigateToFolder = w.__origNavigateToFolder;
    delete w.__origNavigateToFolder;
    if (w.__origGetShareStatus) w.go.main.ShareService.GetShareStatus = w.__origGetShareStatus;
    delete w.__origGetShareStatus;
    if (w.__origHandleTagRef) w.handleTagReferenceEvent = w.__origHandleTagRef;
    delete w.__origHandleTagRef;
  });
}

// Count loadTags and navigateToFolder calls (restored by restoreLoads).
// delayFirstLoadTagsMs makes the next loadTags slow, to land events while a
// batch is reading the tags.
async function spyTagCalls(app: any, delayFirstLoadTagsMs = 0): Promise<void> {
  await app.page.evaluate((delay: number) => {
    const w = window as any;
    w.__loadTagsCalls = 0;
    w.__navigateCalls = [];
    if (!w.__origLoadTags) w.__origLoadTags = w.loadTags;
    if (!w.__origNavigateToFolder) w.__origNavigateToFolder = w.navigateToFolder;
    let pendingDelay = delay;
    w.loadTags = async (...args: unknown[]) => {
      w.__loadTagsCalls++;
      if (pendingDelay > 0) {
        const d = pendingDelay;
        pendingDelay = 0;
        await new Promise(r => setTimeout(r, d));
      }
      return w.__origLoadTags(...args);
    };
    w.navigateToFolder = (...args: unknown[]) => {
      w.__navigateCalls.push(args[0]);
      return w.__origNavigateToFolder(...args);
    };
  }, delayFirstLoadTagsMs);
}

// Run backend tag operations with the frontend's tag reference handler
// holding their events back; replayTagEvents then delivers them. This puts
// the whole burst in one batch (or at chosen times) regardless of how fast
// the backend emitted it.
async function holdTagEvents(app: any, run: (...args: any[]) => Promise<void>, arg?: unknown): Promise<void> {
  await app.page.evaluate(() => {
    const w = window as any;
    w.__heldTagEvents = [];
    if (!w.__origHandleTagRef) w.__origHandleTagRef = w.handleTagReferenceEvent;
    w.handleTagReferenceEvent = (name: string, payload: unknown) => { w.__heldTagEvents.push([name, payload]); };
  });
  await app.page.evaluate(run, arg);
  // Events are emitted after the backend call returns; let them all land.
  await app.page.waitForTimeout(300);
  await app.page.evaluate(() => {
    const w = window as any;
    w.handleTagReferenceEvent = w.__origHandleTagRef;
    delete w.__origHandleTagRef;
  });
}

// The folder being viewed (the deepest active filter), by name. The header's
// breadcrumb shows every ancestor too, so a pill alone cannot say where the
// view is.
async function viewedFolder(app: any): Promise<string | null> {
  return app.page.evaluate(() => {
    // @ts-ignore - app globals
    if (!isFolderMode() || activeTagFilters.length === 0) return null;
    // @ts-ignore
    const id = activeTagFilters[activeTagFilters.length - 1];
    // @ts-ignore
    const tag = allTags.find((t: any) => t.id === id);
    return tag ? tag.name : `#${id}`;
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
      const w = window as any;
      w.__shareStatusCalls = 0;
      w.__origGetShareStatus = w.go.main.ShareService.GetShareStatus;
      w.go.main.ShareService.GetShareStatus = (...args: unknown[]) => {
        w.__shareStatusCalls++;
        return w.__origGetShareStatus(...args);
      };
      for (let i = 0; i < 6; i++) w.runtime.EventsEmit('share:clip-received', { follow_id: 1 });
    });
    await expect.poll(() => loads(app), { timeout: 3000 }).toBe(1);
    await app.expectToast('Received 6 shared clips');
    // Nothing trails behind the burst.
    await app.page.waitForTimeout(600);
    expect(await loads(app)).toBe(1);
    // The share view / nav indicator refresh still runs, once per burst.
    expect(await app.page.evaluate(() => (window as any).__shareStatusCalls)).toBe(1);
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
    // Let the trailing flush land inside this test, not the next one.
    await app.page.waitForTimeout(500);
    expect(await loads(app)).toBeLessThanOrEqual(3);
  });

  test('a burst of tag events reloads tags and the gallery once', async ({ app }) => {
    await app.createTag('burst');
    await countLoads(app);
    await spyTagCalls(app);
    await app.page.evaluate(() => {
      const w = window as any;
      for (let i = 0; i < 5; i++) w.runtime.EventsEmit('tag:updated', { id: 1, name: 'burst', old_name: 'burst' });
      w.runtime.EventsEmit('tag:deleted', { id: 999999, name: 'nope' });
      w.runtime.EventsEmit('tag:merged', { source_id: 999998, dest_id: 999997 });
    });
    await expect.poll(() => loads(app), { timeout: 3000 }).toBe(1);
    await app.page.waitForTimeout(400);
    expect(await loads(app)).toBe(1);
    expect(await app.page.evaluate(() => (window as any).__loadTagsCalls)).toBe(1);
  });

  async function viewLeafFolder(app: any): Promise<void> {
    await app.createTag('top');
    await app.createTag('top/mid');
    await app.createTag('top/mid/leaf');
    await app.toggleFolderMode();
    await app.clickFolder('top');
    await app.clickFolder('mid');
    await app.clickFolder('leaf');
    await app.expectFolderHeader('top/mid/leaf');
  }

  async function deleteLeafAndMid(): Promise<void> {
    // @ts-ignore - Wails runtime
    const tags = await window.go.main.App.GetTags();
    const leaf = tags.find((t: any) => t.name === 'top/mid/leaf');
    const mid = tags.find((t: any) => t.name === 'top/mid');
    // @ts-ignore
    await window.go.main.App.DeleteTag(leaf.id);
    // @ts-ignore
    await window.go.main.App.DeleteTag(mid.id);
  }

  test('deleting a folder and its parent in one burst lands on the nearest live ancestor', async ({ app }) => {
    await viewLeafFolder(app);
    await holdTagEvents(app, deleteLeafAndMid);
    await spyTagCalls(app);
    // Both deletes reach the handler back to back: one batch.
    await app.page.evaluate(() => {
      const w = window as any;
      for (const [name, payload] of w.__heldTagEvents) w.handleTagReferenceEvent(name, payload);
    });

    await expect.poll(() => app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
    expect(await viewedFolder(app)).toBe('top');
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'true');
    await app.page.waitForTimeout(300);
    expect(await app.page.evaluate(() => (window as any).__loadTagsCalls)).toBe(1);
    expect(await app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
  });

  test('tag events arriving while a batch reads the tags join it instead of racing it', async ({ app }) => {
    await viewLeafFolder(app);
    await holdTagEvents(app, deleteLeafAndMid);
    // The batch's tag read takes 400 ms; the second delete lands in the middle
    // of it, and its own flush timer fires while the first batch is running.
    await spyTagCalls(app, 400);
    await app.page.evaluate(async () => {
      const w = window as any;
      const [first, second] = w.__heldTagEvents.filter((e: any[]) => e[0] === 'tag:deleted');
      w.handleTagReferenceEvent(first[0], first[1]);
      await new Promise(r => setTimeout(r, 150));
      w.handleTagReferenceEvent(second[0], second[1]);
    });

    await expect.poll(() => app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
    expect(await viewedFolder(app)).toBe('top');
    await expect(app.page.locator(selectors.tags.folderModeButton)).toHaveAttribute('aria-pressed', 'true');
    await app.page.waitForTimeout(300);
    // One batch: the tags were read again to take in the late event, and the
    // view moved once.
    expect(await app.page.evaluate(() => (window as any).__loadTagsCalls)).toBe(2);
    expect(await app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
  });

  test('chained merges in one burst land on the final destination', async ({ app }) => {
    await app.createTag('alpha');
    await app.createTag('beta');
    await app.createTag('gamma');
    await app.toggleFolderMode();
    await app.clickFolder('alpha');
    await app.expectFolderHeader('alpha');

    await holdTagEvents(app, async () => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const id = (n: string) => tags.find((t: any) => t.name === n).id;
      // @ts-ignore
      await window.go.main.App.MergeTag(id('alpha'), id('beta'));
      // @ts-ignore
      await window.go.main.App.MergeTag(id('beta'), id('gamma'));
    });
    await spyTagCalls(app);
    await app.page.evaluate(() => {
      const w = window as any;
      for (const [name, payload] of w.__heldTagEvents) w.handleTagReferenceEvent(name, payload);
    });

    await expect.poll(() => app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
    expect(await viewedFolder(app)).toBe('gamma');
    await app.expectFolderHeader('gamma');
    await app.page.waitForTimeout(300);
    expect(await app.page.evaluate(() => (window as any).__navigateCalls.length)).toBe(1);
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

  test('refocusing after an out-of-band write reloads and shows it', async ({ app }) => {
    await seedTextClips(app, ['oob-a.txt']);
    await app.expectClipCount(1);
    // Let earlier reloads settle first, so none of them picks the write up.
    await countLoads(app);
    // A write that emits no frontend event (as REST, mp or a plugin would).
    await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      await window.go.main.App.UploadFiles([{ name: 'oob-b.txt', content_type: 'text/plain', data: btoa('out of band') }], 0, 0);
    });
    await app.page.waitForTimeout(200);
    await app.expectClipCount(1);
    expect(await loads(app)).toBe(0);
    await refocus(app);
    await expect.poll(() => loads(app)).toBe(1);
    await app.expectClipCount(2);
    // Refocusing again with nothing new changes nothing.
    await app.page.waitForTimeout(300);
    await refocus(app);
    await app.page.waitForTimeout(300);
    expect(await loads(app)).toBe(1);
  });

  test('an expired card disappears on refocus without a reload', async ({ app }) => {
    await seedTextClips(app, ['expiring.txt', 'lasting.txt']);
    await app.expectClipCount(2);
    // Settle first: a reload landing later would re-render the card below.
    await countLoads(app);
    // Stand in for time passing: the card's expiry is now behind us.
    await app.page.locator(selectors.gallery.clipCardByName('expiring.txt')).evaluate((card: HTMLElement) => {
      card.dataset.expiresAt = new Date(Date.now() - 1000).toISOString();
    });
    await refocus(app);
    await app.expectClipCount(1);
    await expect(app.page.locator(selectors.gallery.clipCardByName('expiring.txt'))).toHaveCount(0);
    await expect(app.page.locator(selectors.bottomBar.clipCount)).toHaveText('1 clip');
    expect(await loads(app)).toBe(0);
  });
});
