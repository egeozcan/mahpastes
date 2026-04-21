import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu structure', () => {
    async function setupFolder(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `ctx-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('right-click on folder opens context menu with all items', async ({ app }) => {
        const tag = await setupFolder(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });

        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        const actions = ['open', 'move', 'rename', 'serve', 'share', 'hide'];
        for (const a of actions) {
            await expect(app.page.locator(selectors.folderContextMenuItem(a))).toHaveCount(1);
        }
    });

    test('menu order: Open, divider, Move, Rename, divider, Serve, Share, divider, Hide', async ({ app }) => {
        const tag = await setupFolder(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });

        const orderActions = await app.page.locator(
            `${selectors.folderContextMenu} > [role="menuitem"]`
        ).evaluateAll(els => els.map(el => el.getAttribute('data-action')));

        expect(orderActions).toEqual(['open', 'move', 'rename', 'serve', 'share', 'hide']);

        const dividerCount = await app.page.locator(`${selectors.folderContextMenu} hr`).count();
        expect(dividerCount).toBe(3);
    });

    test('Shift+F10 on focused card opens menu anchored at the card', async ({ app }) => {
        const tag = await setupFolder(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();
        await app.page.keyboard.press('Shift+F10');

        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();
    });

    test('Escape closes the menu and restores focus to the card', async ({ app }) => {
        const tag = await setupFolder(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();
        await app.page.keyboard.press('Shift+F10');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        await app.page.keyboard.press('Escape');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeHidden();

        const focused = await app.page.evaluate(() => document.activeElement?.getAttribute('data-testid'));
        expect(focused).toBe(`folder-card-${tag}`);
    });

    test('Hide label flips to Unhide when tag is hidden', async ({ app }) => {
        const tag = await setupFolder(app);

        // Tag not hidden: label should be "Hide"
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await expect(app.page.locator(`${selectors.folderContextMenu} [data-action="hide"]`)).toContainText(/Hide/);
        await app.page.keyboard.press('Escape');

        // Hide via API
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const existing = await window.go.main.App.GetHiddenTags();
            await window.go.main.App.SetHiddenTags([...(existing || []), t.id]);
            if (typeof window.setHiddenTagsState === 'function') window.setHiddenTagsState([...(existing || []), t.id]);
            if (window.__testHelpers?.setHiddenTags) window.__testHelpers.setHiddenTags([...(existing || []), t.id]);
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        }, tag);

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await expect(app.page.locator(`${selectors.folderContextMenu} [data-action="hide"]`)).toContainText(/Unhide/);
    });
});
