import { test, expect } from '../../fixtures/test-fixtures';
import type { Page } from '@playwright/test';

async function mountController(page: Page) {
  return page.evaluate(() => {
    const host = document.createElement('div');
    host.innerHTML = `
      <div data-root inert>
        <div data-viewport aria-busy="false">
          <button data-close>Close</button>
          <button data-prev>Previous</button>
          <div data-pan><img data-image></div>
          <button data-next>Next</button>
          <div data-loading hidden>Loading</div>
          <div data-error hidden><button data-retry>Retry</button></div>
          <p data-caption></p>
          <div data-status></div>
        </div>
        <div data-info></div>
        <button data-zoom-out>−</button>
        <input data-slider type="range" min="0" max="100" value="0">
        <button data-zoom-in>+</button>
        <button data-fit>Fit</button>
        <button data-actual>1:1</button>
        <span data-zoom-info></span>
      </div>`;
    document.body.appendChild(host);

    const pending = new Map<number, { resolve: (value: string) => void; reject: (error: Error) => void }>();
    const loadImage = (clip: { id: number }) => new Promise<string>((resolve, reject) => {
      pending.set(clip.id, { resolve, reject });
    });
    const query = <T extends Element>(selector: string) => host.querySelector(selector) as T;
    // @ts-ignore classic-script factory
    const controller = window.createLightboxController({
      elements: {
        root: query<HTMLElement>('[data-root]'),
        viewport: query<HTMLElement>('[data-viewport]'),
        panLayer: query<HTMLElement>('[data-pan]'),
        image: query<HTMLImageElement>('[data-image]'),
        caption: query<HTMLElement>('[data-caption]'),
        status: query<HTMLElement>('[data-status]'),
        loading: query<HTMLElement>('[data-loading]'),
        error: query<HTMLElement>('[data-error]'),
        retry: query<HTMLButtonElement>('[data-retry]'),
        close: query<HTMLButtonElement>('[data-close]'),
        previous: query<HTMLButtonElement>('[data-prev]'),
        next: query<HTMLButtonElement>('[data-next]'),
        imageInfo: query<HTMLElement>('[data-info]'),
        slider: query<HTMLInputElement>('[data-slider]'),
        zoomOut: query<HTMLButtonElement>('[data-zoom-out]'),
        zoomIn: query<HTMLButtonElement>('[data-zoom-in]'),
        fit: query<HTMLButtonElement>('[data-fit]'),
        actual: query<HTMLButtonElement>('[data-actual]'),
        zoomInfo: query<HTMLElement>('[data-zoom-info]'),
      },
      backgroundRoots: [],
      loadImage,
      trapFocus: () => () => {},
      renderPluginActions: () => {},
      renderFileActions: () => {},
      editClip: () => {},
      copyClip: () => {},
      closeMenus: () => {},
      getOpenerIndex: () => -1,
      restoreFocus: () => {},
      reportError: () => {},
    });
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness = { host, pending, controller };
    return true;
  });
}

test.afterEach(async ({ page }) => {
  if (!page) return;
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness?.controller.close();
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness?.host.remove();
    // @ts-ignore browser-only controller harness
    delete window.__lightboxHarness;
  });
});

test('ignores an older image response that resolves last', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }, { id: 2, filename: 'two.png' }], currentId: 1, opener: null });
    h.controller.command('next');
    h.pending.get(2).resolve('data:image/png;base64,dHdv');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    window.__lightboxHarness.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,dHdv');
});

test('does not reopen after closing during a pending load', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.controller.close();
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-root]')).not.toHaveClass(/active/);
  await expect(page.locator('[data-root]')).toHaveAttribute('inert', '');
});

test('keeps a reopened image when the previous request resolves', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.controller.close();
    void h.controller.open({ clips: [{ id: 2, filename: 'two.png' }], currentId: 2, opener: null });
    h.pending.get(2).resolve('data:image/png;base64,dHdv');
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-caption]')).toHaveText('two.png');
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,dHdv');
});

test('shows an error and retries the selected clip', async ({ page }) => {
  await mountController(page);
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    void h.controller.open({ clips: [{ id: 1, filename: 'one.png' }], currentId: 1, opener: null });
    h.pending.get(1).reject(new Error('load failed'));
  });
  await expect(page.locator('[data-error]')).toBeVisible();
  await page.evaluate(() => {
    // @ts-ignore browser-only controller harness
    const h = window.__lightboxHarness;
    h.controller.command('retry');
    h.pending.get(1).resolve('data:image/png;base64,b25l');
  });
  await expect(page.locator('[data-image]')).toHaveAttribute('src', 'data:image/png;base64,b25l');
});
