import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';

// The browser drives the real desktop runtime: assertions read the native
// window state, not the browser viewport or a simulated maximize toggle.
test.describe('Title bar double-click', () => {
  test.beforeEach(async ({ app }) => {
    await app.page.evaluate(() => (window as any).runtime.WindowUnmaximise());
    await expect.poll(() => app.page.evaluate(() =>
      (window as any).runtime.WindowIsMaximised())).toBe(false);
  });

  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => (window as any).runtime.WindowUnmaximise());
  });

  test('fills the desktop then restores without entering fullscreen', async ({ app }) => {
    const originalSize = await app.page.evaluate(() => (window as any).runtime.WindowGetSize());
    const header = app.page.locator(selectors.header.root);

    // The header's top padding is empty draggable space at every viewport size.
    await header.dblclick({ position: { x: 200, y: 5 } });
    await expect.poll(() => app.page.evaluate(() =>
      (window as any).runtime.WindowIsMaximised())).toBe(true);
    expect(await app.page.evaluate(() => (window as any).runtime.WindowIsFullscreen())).toBe(false);

    // This point hits the inner layout div, which inherits the drag property.
    await header.dblclick({ position: { x: 5, y: 20 } });
    await expect.poll(() => app.page.evaluate(() =>
      (window as any).runtime.WindowIsMaximised())).toBe(false);
    expect(await app.page.evaluate(() => (window as any).runtime.WindowGetSize())).toEqual(originalSize);
  });

  for (const clickCount of [2, 3]) {
    test(`${clickCount} clicks on header space do not select the nearby search placeholder`, async ({ app }) => {
      const input = app.page.locator(selectors.header.searchInput);
      await input.fill('');
      const bounds = (await input.boundingBox())!;
      // Empty padding above Search still selects its nearest word on mouse-down
      // unless selection is prevented BEFORE dblclick (reproduces in WebKit too).
      await app.page.mouse.click(bounds.x + 60, bounds.y - 5, { clickCount });
      expect(await app.page.evaluate(() => window.getSelection()!.toString())).toBe('');
      await expect.poll(() => app.page.evaluate(() =>
        (window as any).runtime.WindowIsMaximised())).toBe(true);
    });
  }

  test('double-clicking search selects text without maximizing', async ({ app }) => {
    const input = app.page.locator(selectors.header.searchInput);
    await input.fill('example');
    await input.dblclick();
    expect(await input.evaluate((el: HTMLInputElement) =>
      el.value.slice(el.selectionStart!, el.selectionEnd!))).toBe('example');
    expect(await app.page.evaluate(() => (window as any).runtime.WindowIsMaximised())).toBe(false);
  });

  test('ignores controls, single clicks, other buttons and the bottom drag bar', async ({ app }) => {
    const header = app.page.locator(selectors.header.root);
    await header.click({ position: { x: 200, y: 5 } });
    expect(await app.page.evaluate(() => (window as any).runtime.WindowIsMaximised())).toBe(false);
    // Dispatch only dblclick to isolate hit-testing from the controls' click actions.
    // Check each separately so two accidental toggles cannot cancel each other out.
    for (const target of [
      app.page.locator(selectors.header.title),
      app.page.locator(selectors.header.drawerToggle).locator('svg'),
      app.page.locator(selectors.header.clipControls),
      app.page.locator(selectors.bottomBar.root),
    ]) {
      await target.dispatchEvent('dblclick', { button: 0 });
      expect(await app.page.evaluate(() => (window as any).runtime.WindowIsMaximised())).toBe(false);
    }
    for (const button of [1, 2]) {
      await header.dispatchEvent('dblclick', { button });
      expect(await app.page.evaluate(() => (window as any).runtime.WindowIsMaximised())).toBe(false);
    }
  });

  test('leaves double-click alone in server mode, even without a window runtime', async ({ app }) => {
    const errors: string[] = [];
    const onError = (error: Error) => errors.push(error.message);
    app.page.on('pageerror', onError);
    try {
      await app.page.evaluate((selector) => {
        const runtime = (window as any).runtime;
        const mode = (window as any).mahpastesMode;
        try {
          (window as any).mahpastesMode = 'server';
          (window as any).runtime = undefined;
          document.querySelector(selector)!.dispatchEvent(new MouseEvent('dblclick', {
            button: 0, bubbles: true, cancelable: true,
          }));
        } finally {
          (window as any).runtime = runtime;
          (window as any).mahpastesMode = mode;
        }
      }, selectors.header.root);
      expect(errors).toEqual([]);
      expect(await app.page.evaluate(() => (window as any).runtime.WindowIsMaximised())).toBe(false);
    } finally {
      app.page.off('pageerror', onError);
    }
  });
});
