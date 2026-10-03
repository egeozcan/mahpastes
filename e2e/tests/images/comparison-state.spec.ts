import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

async function openPair(app: any): Promise<void> {
  const a = await createTempFile(generateTestImage(60, 60, [255, 0, 0]), 'png');
  const b = await createTempFile(generateTestImage(60, 60, [250, 10, 0]), 'png');
  await app.uploadFile(a);
  await app.uploadFile(b);
  await app.selectClips([path.basename(a), path.basename(b)]);
  await app.openComparison();
}

test.describe('Comparison state', () => {
  test.afterEach(async ({ app }) => {
    await app.page.evaluate(() => {
      const w = window as any;
      w.__restoreGetImageDiff?.();
      delete w.__restoreGetImageDiff;
      delete w.__diffCalls;
    });
  });

  test('dragging the diff threshold asks for one diff, for the value it settled on', async ({ app }) => {
    await openPair(app);
    await app.setComparisonMode('diff');
    await expect(app.page.locator(selectors.comparison.similarity)).toBeVisible();

    await app.page.evaluate(() => {
      const api = (window as any).go.main.App;
      const original = api.GetImageDiff;
      const calls: number[] = [];
      (window as any).__diffCalls = calls;
      (window as any).__restoreGetImageDiff = () => { api.GetImageDiff = original; };
      api.GetImageDiff = (a: number, b: number, threshold: number) => {
        calls.push(threshold);
        return original(a, b, threshold);
      };
      const range = document.getElementById('comparison-range') as HTMLInputElement;
      for (let v = 41; v <= 60; v++) {
        range.value = String(v);
        range.dispatchEvent(new Event('input', { bubbles: true }));
      }
    });

    // Requests for earlier ticks would be issued before the settled one, so
    // once any call has landed, any extra ones already show in the list.
    await expect.poll(() => app.page.evaluate(() => (window as any).__diffCalls.length)).toBeGreaterThanOrEqual(1);
    const calls = await app.page.evaluate(() => (window as any).__diffCalls);
    expect(calls).toEqual([60]);
    await app.closeComparison();
  });

  test('re-opening right after closing keeps the new comparison intact', async ({ app }) => {
    await openPair(app);
    await app.page.evaluate(() => {
      (window as any).closeComparisonModal();
      (window as any).openComparisonModal();
    });
    await app.page.waitForSelector(`${selectors.comparison.modal}.active`);
    // Wait until the old close's delayed cleanup is cancelled or has run
    // (it clears comparisonCloseTimer itself before blanking the images).
    // @ts-ignore - modals.js top-level let
    await expect.poll(() => app.page.evaluate(() => comparisonCloseTimer)).toBeNull();
    const src = await app.page.locator('#comparison-img-bottom').getAttribute('src');
    expect(src || '').toMatch(/^data:image/);
    await app.closeComparison();
  });
});
