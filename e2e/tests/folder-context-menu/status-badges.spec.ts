import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder status badges', () => {
    test('served folder shows emerald globe badge within poll interval', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `serve-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(0);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(1, { timeout: 5000 });
        const badge = app.page.locator(selectors.folderBadgeServedActive(tagName));
        await expect(badge).toHaveAttribute('aria-label', /Served on/);
        await expect(badge).toHaveAttribute('data-tooltip', /Serving on http:\/\//);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ServeService.StopServing(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(0, { timeout: 5000 });
    });

    test('shared folder shows blue chain badge', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `share-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(0);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ShareService.StartShare(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(1, { timeout: 5000 });
        const badge = app.page.locator(selectors.folderBadgeSharedActive(tagName));
        await expect(badge).toHaveAttribute('aria-label', /[Ss]haring/);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ShareService.StopShare(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(0, { timeout: 5000 });
    });

    test('hidden folder has data-hidden and reduced opacity', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `hidden-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        const card = app.page.locator(selectors.folderCard(tagName));
        await expect(card).not.toHaveAttribute('data-hidden', 'true');

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            const existing = await window.go.main.App.GetHiddenTags();
            const newHidden = [...(existing || []), t.id];
            await window.go.main.App.SetHiddenTags(newHidden);
            // Sync frontend in-memory hidden tags state
            if (window.__testHelpers && window.__testHelpers.setHiddenTags) {
                window.__testHelpers.setHiddenTags(newHidden);
            }
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        }, tagName);

        await expect(app.page.locator(selectors.folderHidden(tagName))).toHaveCount(1);
        const opacity = await card.evaluate(el => getComputedStyle(el).opacity);
        expect(parseFloat(opacity)).toBeLessThan(0.7);
    });
    test('unchanged status leaves badge nodes alone across polls', async ({ app }) => {
        const tagName = `steady-${Date.now()}`;
        await app.createTag(tagName);
        await app.enterFolderMode();
        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tagName);
        const badge = app.page.locator(selectors.folderBadgeServedActive(tagName));
        await expect(badge).toHaveCount(1, { timeout: 5000 });
        await badge.evaluate((el: any) => { el.__steadyMark = true; });
        // Two more polls (every 2 s) with the same status.
        await app.page.waitForTimeout(4500);
        expect(await badge.evaluate((el: any) => el.__steadyMark === true)).toBe(true);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ServeService.StopServing(t.id);
        }, tagName);
        await expect(badge).toHaveCount(0, { timeout: 5000 });
    });

    test('poller pauses while the window is hidden and resumes on return', async ({ app }) => {
        await app.createTag(`poll-${Date.now()}`);
        await app.enterFolderMode();
        await expect.poll(() => app.page.evaluate(() => (window as any).folderStatusPoller.isRunning())).toBe(true);

        const setHidden = (hidden: boolean) => app.page.evaluate((h) => {
            Object.defineProperty(document, 'hidden', { configurable: true, get: () => h });
            Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (h ? 'hidden' : 'visible') });
            document.dispatchEvent(new Event('visibilitychange'));
        }, hidden);

        try {
            await setHidden(true);
            expect(await app.page.evaluate(() => (window as any).folderStatusPoller.isRunning())).toBe(false);
            // Wrap the status call: nothing may poll while hidden.
            await app.page.evaluate(() => {
                const w = window as any;
                w.__statusPolls = 0;
                w.__origGetServeStatus = w.wailsGetServeStatus;
                w.wailsGetServeStatus = (...a: unknown[]) => { w.__statusPolls++; return w.__origGetServeStatus(...a); };
            });
            await app.page.waitForTimeout(2500);
            expect(await app.page.evaluate(() => (window as any).__statusPolls)).toBe(0);

            await setHidden(false);
            expect(await app.page.evaluate(() => (window as any).folderStatusPoller.isRunning())).toBe(true);
            // Resuming polls at once rather than waiting out an interval.
            expect(await app.page.evaluate(() => (window as any).__statusPolls)).toBeGreaterThanOrEqual(1);
        } finally {
            await app.page.evaluate(() => {
                const w = window as any;
                if (w.__origGetServeStatus) w.wailsGetServeStatus = w.__origGetServeStatus;
                delete (document as any).hidden;
                delete (document as any).visibilityState;
            });
        }
    });
});
