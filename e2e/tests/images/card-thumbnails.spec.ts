import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

// Image cards render a backend thumbnail (internal/app/thumbnail.go) served by
// URL, not the full original as base64 through GetClipData. The lightbox,
// editor and comparison view still load the full image.

async function countGetClipData(page: any): Promise<void> {
  await page.evaluate(() => {
    const w = window as any;
    const original = w.go.main.App.GetClipData;
    w.__thumbOriginalGetClipData = original;
    w.__thumbGetClipDataCalls = [];
    w.go.main.App.GetClipData = async (...args: any[]) => {
      w.__thumbGetClipDataCalls.push(args[0]);
      return original(...args);
    };
  });
}

function cardImage(app: any, filename: string) {
  return app.page.locator(`${selectors.gallery.clipCardByName(filename)} img[data-clip-id]:not(.video-thumb)`);
}

async function loadedSize(image: any): Promise<{ w: number; h: number } | null> {
  return image.evaluate((el: HTMLImageElement) =>
    el.complete && el.naturalWidth > 0 ? { w: el.naturalWidth, h: el.naturalHeight } : null);
}

async function currentHash(page: any, filename: string): Promise<string> {
  return page.evaluate(async (name: string) => {
    const w = window as any;
    const res = await w.go.main.App.ListClipsPage({
      mode: 'all', archived: false, tag_ids: [], hidden_tag_ids: [], sort_field: 'created_at', sort_dir: 'desc', offset: 0, limit: 50,
    });
    return res.clips.find((c: any) => c.filename === name)?.content_hash || '';
  }, filename);
}

test.describe('Image card thumbnails', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      if (w.__thumbOriginalGetClipData) {
        w.go.main.App.GetClipData = w.__thumbOriginalGetClipData;
        delete w.__thumbOriginalGetClipData;
      }
    });
  });

  test('a large image card renders a bounded thumbnail from a URL, without GetClipData', async ({ app }) => {
    const file = await createTempFile(generateTestImage(1600, 1200, [40, 120, 200]), 'png');
    const filename = path.basename(file);
    await countGetClipData(app.page);

    await app.uploadFile(file);
    const image = cardImage(app, filename);
    await expect.poll(() => loadedSize(image)).toEqual({ w: 512, h: 384 });

    const hash = await currentHash(app.page, filename);
    expect(hash).toMatch(/^[0-9a-f]{64}$/);
    const src = await image.getAttribute('src');
    expect(src).toMatch(/^\/thumb\/[0-9a-f]+\/\d+\/[0-9a-f]{64}$/);
    expect(src).toContain(hash);
    await expect(image).toHaveAttribute('loading', 'lazy');
    await expect(image).not.toHaveClass(/opacity-0/);
    await expect(app.page.locator(`${selectors.gallery.clipCardByName(filename)} .loading-spinner`)).toHaveCount(0);

    expect(await app.page.evaluate(() => (window as any).__thumbGetClipDataCalls.length)).toBe(0);

    // The thumbnail response is cacheable by URL.
    const headers = await app.page.evaluate(async (url: string) => {
      const res = await fetch(url);
      return { status: res.status, type: res.headers.get('content-type'), cache: res.headers.get('cache-control'), nosniff: res.headers.get('x-content-type-options') };
    }, src!);
    expect(headers.status).toBe(200);
    expect(headers.type).toBe('image/jpeg');
    expect(headers.cache).toContain('immutable');
    expect(headers.nosniff).toBe('nosniff');
  });

  test('the lightbox still shows the full-size image', async ({ app }) => {
    const file = await createTempFile(generateTestImage(1600, 1200, [200, 60, 60]), 'png');
    const filename = path.basename(file);
    await app.uploadFile(file);
    await expect.poll(() => loadedSize(cardImage(app, filename))).toEqual({ w: 512, h: 384 });

    await app.openLightbox(filename);
    const full = app.page.locator(selectors.lightbox.image);
    await expect.poll(() => loadedSize(full)).toEqual({ w: 1600, h: 1200 });
    await expect(full).toHaveAttribute('src', /^data:image\/png;base64,/);
    await app.closeLightbox();
  });

  test('a small image is served unchanged as its own thumbnail', async ({ app }) => {
    const file = await createTempFile(generateTestImage(120, 90, [10, 200, 10]), 'png');
    const filename = path.basename(file);
    await app.uploadFile(file);
    await expect.poll(() => loadedSize(cardImage(app, filename))).toEqual({ w: 120, h: 90 });
  });

  test('a card falls back to the full image when its thumbnail fails', async ({ app }) => {
    const file = await createTempFile(generateTestImage(1400, 700, [90, 90, 20]), 'png');
    const filename = path.basename(file);
    await app.page.route('**/thumb/**', route => route.abort());
    try {
      await app.uploadFile(file);
      const image = cardImage(app, filename);
      await expect.poll(() => loadedSize(image)).toEqual({ w: 1400, h: 700 });
      await expect(image).toHaveAttribute('src', /^data:image\/png;base64,/);
    } finally {
      await app.page.unroute('**/thumb/**');
    }
  });

  test('saving an edit in place updates the card thumbnail', async ({ app }) => {
    const file = await createTempFile(generateTestImage(1600, 1200, [255, 255, 255]), 'png');
    const filename = path.basename(file);
    await app.uploadFile(file);
    const image = cardImage(app, filename);
    await expect.poll(() => loadedSize(image)).toEqual({ w: 512, h: 384 });
    const before = await image.getAttribute('src');
    const hashBefore = await currentHash(app.page, filename);

    await app.openImageEditor(filename);
    await app.selectTool('brush');
    await app.setEditorColor('#000000');
    await app.setBrushSize(50);
    const box = await app.page.locator(selectors.editor.canvas).boundingBox();
    if (!box) throw new Error('editor canvas not visible');
    for (let y = 0.2; y <= 0.8; y += 0.1) {
      await app.drawOnCanvas({ x: box.width * 0.1, y: box.height * y }, { x: box.width * 0.9, y: box.height * y });
    }
    await app.page.locator('#editor-save-in-place').click();
    await app.page.waitForSelector(`${selectors.editor.modal}:not(.active)`);

    await expect.poll(() => currentHash(app.page, filename)).not.toBe(hashBefore);
    const hashAfter = await currentHash(app.page, filename);
    const updated = cardImage(app, filename);
    await expect(updated).toHaveAttribute('src', new RegExp(`${hashAfter}`));
    expect(await updated.getAttribute('src')).not.toBe(before);
    await expect.poll(() => loadedSize(updated)).toEqual({ w: 512, h: 384 });

    // The new thumbnail shows the strokes: it is no longer all white.
    const dark = await updated.evaluate((el: HTMLImageElement) => {
      const canvas = document.createElement('canvas');
      canvas.width = el.naturalWidth;
      canvas.height = el.naturalHeight;
      const ctx = canvas.getContext('2d')!;
      ctx.drawImage(el, 0, 0);
      const data = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      let n = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i] < 128) n++;
      return n;
    });
    expect(dark).toBeGreaterThan(1000);
  });

  test('the full-image cache is bounded by bytes and evicts the least recently used', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const w = window as any;
      w.clearMediaCaches();
      const max = w.__imageCacheStats().maxBytes;
      const chunk = 'x'.repeat(Math.floor(max / 4) - 16); // four fit, the fifth evicts
      const ids = [900001, 900002, 900003, 900004, 900005];
      for (const id of ids.slice(0, 4)) w.imageCacheSet(id, `${chunk}${id}`);
      w.imageCacheGet(900001); // most recently used now
      w.imageCacheSet(900005, `${chunk}900005`);
      const present = ids.filter(id => w.imageCacheGet(id) !== undefined);
      const stats = w.__imageCacheStats();
      w.imageCacheSet(900006, 'y'.repeat(max + 1)); // larger than the whole cache: not kept
      const oversized = w.imageCacheGet(900006) !== undefined;
      w.clearMediaCaches();
      return { present, bytes: stats.bytes, max, oversized, cleared: w.__imageCacheStats() };
    });
    expect(result.present).toEqual([900001, 900003, 900004, 900005]);
    expect(result.bytes).toBeLessThanOrEqual(result.max);
    expect(result.oversized).toBe(false);
    expect(result.cleared).toMatchObject({ entries: 0, bytes: 0 });
  });

  test('card media work runs near the viewport, a few at a time', async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      w.__sched = { ran: [] as number[], resolvers: [] as Array<() => void>, maxActive: 0, farRan: false, detachedRan: false };
      const host = document.createElement('div');
      host.id = 'sched-test-host';
      document.body.prepend(host);
      for (let i = 0; i < 10; i++) {
        const el = document.createElement('div');
        el.style.cssText = 'width:10px;height:10px';
        host.appendChild(el);
        w.scheduleCardMedia(el, () => new Promise<void>(resolve => {
          w.__sched.ran.push(i);
          w.__sched.maxActive = Math.max(w.__sched.maxActive, w.__cardMediaStats().active);
          w.__sched.resolvers.push(resolve);
        }));
      }
      const far = document.createElement('div');
      far.id = 'sched-test-far';
      far.style.cssText = 'position:absolute;top:30000px;width:10px;height:10px';
      document.body.appendChild(far);
      w.scheduleCardMedia(far, () => { w.__sched.farRan = true; });
      const detached = document.createElement('div');
      detached.style.cssText = 'width:10px;height:10px';
      host.appendChild(detached);
      w.scheduleCardMedia(detached, () => { w.__sched.detachedRan = true; });
      detached.remove();
    });

    await expect.poll(() => app.page.evaluate(() => (window as any).__sched.ran.length)).toBe(4);
    await app.page.waitForTimeout(200);
    expect(await app.page.evaluate(() => (window as any).__sched.ran.length)).toBe(4);

    // Finishing tasks lets the queue drain, never more than four at once.
    for (let round = 0; round < 3; round++) {
      await app.page.evaluate(() => {
        const s = (window as any).__sched;
        s.resolvers.splice(0).forEach((r: () => void) => r());
      });
      await app.page.waitForTimeout(50);
    }
    await expect.poll(() => app.page.evaluate(() => (window as any).__sched.ran.length)).toBe(10);
    const state = await app.page.evaluate(() => (window as any).__sched);
    expect(state.maxActive).toBeLessThanOrEqual(4);
    expect(state.farRan).toBe(false);
    expect(state.detachedRan).toBe(false);

    // Bringing the offscreen card near the viewport starts its work.
    await app.page.evaluate(() => document.getElementById('sched-test-far')!.scrollIntoView());
    await expect.poll(() => app.page.evaluate(() => (window as any).__sched.farRan)).toBe(true);
    await app.page.evaluate(() => {
      document.getElementById('sched-test-host')?.remove();
      document.getElementById('sched-test-far')?.remove();
      window.scrollTo(0, 0);
    });
  });
  test('queued card media is dropped when its card is detached or the gallery is rebuilt', async ({ app }) => {
    const ran = await app.page.evaluate(async () => {
      const w = window as any;
      const tick = () => new Promise(r => setTimeout(r, 50));
      const until = async (cond: () => boolean) => {
        for (let i = 0; i < 100 && !cond(); i++) await tick();
        if (!cond()) throw new Error('timed out waiting for the scheduler');
      };
      const host = document.createElement('div');
      host.id = 'sched-detach-host';
      document.body.prepend(host);
      const resolvers: Array<() => void> = [];
      const ran: string[] = [];
      const card = (name: string, hold: boolean) => {
        const el = document.createElement('div');
        el.style.cssText = 'width:10px;height:10px';
        host.appendChild(el);
        w.scheduleCardMedia(el, () => {
          ran.push(name);
          return hold ? new Promise<void>(r => resolvers.push(r)) : undefined;
        });
        return el;
      };
      // Fill every slot with work that has not finished.
      for (let i = 0; i < 4; i++) card(`busy${i}`, true);
      await until(() => w.__cardMediaStats().active === 4);

      // Queued behind the full pool: one is detached before its slot opens.
      const gone = card('detached', false);
      card('kept', false);
      await until(() => w.__cardMediaStats().queued === 2);
      gone.remove();
      resolvers.shift()!();
      await until(() => ran.includes('kept'));

      // A rebuild (clearRenderedClips) forgets everything still queued.
      card('busy4', true);
      await until(() => w.__cardMediaStats().active === 4);
      card('rebuilt-away', false);
      await until(() => w.__cardMediaStats().queued === 1);
      w.clearRenderedClips();
      const queuedAfterReset = w.__cardMediaStats().queued;
      resolvers.splice(0).forEach(r => r());
      await until(() => w.__cardMediaStats().active === 0);
      await tick();
      host.remove();
      return { ran, queuedAfterReset };
    });
    expect(ran.ran).toContain('kept');
    expect(ran.ran).not.toContain('detached');
    expect(ran.ran).not.toContain('rebuilt-away');
    expect(ran.queuedAfterReset).toBe(0);
  });
});
