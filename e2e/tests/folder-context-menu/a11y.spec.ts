import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu a11y', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `a11y-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Folder card is focusable via Tab and has composite aria-label', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        await expect(card).toHaveAttribute('tabindex', '0');
        const label = await card.getAttribute('aria-label');
        expect(label).toMatch(new RegExp(`Folder: ${tag}`));
        expect(label).toMatch(/\d+ clips?/);
    });

    test('aria-label composition includes active states', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(1, { timeout: 5000 });
        const label = await app.page.locator(selectors.folderCard(tag)).getAttribute('aria-label');
        expect(label).toMatch(/served/);

        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, tag);
    });

    test('Badge has role="img" and descriptive aria-label', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        const badge = app.page.locator(selectors.folderBadgeServedActive(tag));
        await expect(badge).toHaveCount(1, { timeout: 5000 });
        await expect(badge).toHaveAttribute('role', 'img');
        await expect(badge).toHaveAttribute('aria-label', /Served on/);

        const inner = badge.locator('svg');
        await expect(inner).toHaveAttribute('aria-hidden', 'true');
        await expect(inner).toHaveAttribute('focusable', 'false');

        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, tag);
    });

    test('Shift+F10 from focused card opens menu; Esc restores focus', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();

        await app.page.keyboard.press('Shift+F10');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        await app.page.keyboard.press('Escape');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeHidden();

        const activeTestID = await app.page.evaluate(() => document.activeElement?.getAttribute('data-testid'));
        expect(activeTestID).toBe(`folder-card-${tag}`);
    });
});
