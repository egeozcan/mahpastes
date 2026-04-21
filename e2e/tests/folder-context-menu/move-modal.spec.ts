import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

const MOVE_MODAL = '[data-testid="folder-move-modal"]';
const MOVE_TREE = '[data-testid="folder-move-tree"]';
const MOVE_PREVIEW = '[data-testid="folder-move-preview"]';
const MOVE_ERROR = '[data-testid="folder-move-error"]';
const MOVE_CONFIRM = '[data-testid="folder-move-confirm"]';
const MOVE_CANCEL = '[data-testid="folder-move-cancel"]';

test.describe('Folder move modal', () => {
    async function setupTwoFolders(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const src = `src-${Date.now()}`;
        const dst = `dst-${Date.now()}`;
        await app.createTag(src);
        await app.createTag(dst);
        await app.addTagToClip(path.basename(imagePath), src);
        await app.enterFolderMode();
        return { src, dst };
    }

    test('opens with tree, preview updates on select, Move confirms', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));

        await expect(app.page.locator(MOVE_MODAL)).toBeVisible();
        await expect(app.page.locator(MOVE_TREE)).toBeVisible();

        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await expect(app.page.locator(MOVE_PREVIEW)).toContainText(`${dst}/${src}`);

        await app.page.click(MOVE_CONFIRM);
        await expect(app.page.locator(MOVE_MODAL)).toBeHidden();

        const tags = await app.page.evaluate(() => window.go.main.App.GetTags());
        const renamed = tags.find((t: any) => t.name === `${dst}/${src}`);
        expect(renamed).toBeDefined();
    });

    test('self and descendants are disabled', async ({ app }) => {
        const { src } = await setupTwoFolders(app);
        await app.page.evaluate(async (parent) => {
            await window.go.main.App.CreateTag(`${parent}/child`);
            if (typeof window.loadClips === 'function') await window.loadClips();
        }, src);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));

        const selfRow = app.page.locator(`${MOVE_TREE} [data-dest-path="${src}"]`);
        await expect(selfRow).toHaveAttribute('data-disabled', 'true');

        const childRow = app.page.locator(`${MOVE_TREE} [data-dest-path="${src}/child"]`);
        await expect(childRow).toHaveAttribute('data-disabled', 'true');
    });

    test('Serve-active error is surfaced inline', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, src);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));
        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await app.page.click(MOVE_CONFIRM);

        await expect(app.page.locator(MOVE_MODAL)).toBeVisible();
        await expect(app.page.locator(MOVE_ERROR)).toContainText(/served|serving|server running/i);

        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, src);
    });

    test('Cancel closes modal without moving', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));
        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await app.page.click(MOVE_CANCEL);

        await expect(app.page.locator(MOVE_MODAL)).toBeHidden();

        const tags = await app.page.evaluate(() => window.go.main.App.GetTags());
        const renamed = tags.find((t: any) => t.name === `${dst}/${src}`);
        expect(renamed).toBeUndefined();
    });
});
