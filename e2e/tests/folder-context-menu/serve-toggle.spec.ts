import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu: Serve toggle', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `srv-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Serve jumps to Serve view with folder pre-added', async ({ app }) => {
        const tag = await setup(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('serve'));

        // Poll until the serve view is active and the entry is registered
        // (handleAction is async; click() returns before the awaits complete).
        await app.page.waitForFunction(
            () => (window as any).__testHelpers?.getCurrentView?.() === 'serve',
            { timeout: 5000 }
        );

        await app.page.waitForFunction(
            (name) => (window as any).__testHelpers?.hasConfiguredServeEntry?.(name) === true,
            tag,
            { timeout: 5000 }
        );

        const currentView = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(currentView).toBe('serve');

        const hasEntry = await app.page.evaluate(
            (name) => (window as any).__testHelpers?.hasConfiguredServeEntry?.(name),
            tag
        );
        expect(hasEntry).toBe(true);
    });

    test('When serving, menu item flips to Stop serving and stops on click', async ({ app }) => {
        const tag = await setup(app);

        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(1, { timeout: 5000 });

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        const serveItem = app.page.locator(`${selectors.folderContextMenu} [data-action="stop-serve"]`);
        await expect(serveItem).toBeVisible();
        await expect(serveItem).toContainText(/Stop serving/);

        await serveItem.click();

        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(0, { timeout: 5000 });

        const currentView = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(currentView).toBe('clips');
    });
});
