import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';

const simplePlugin = `
Plugin = {
    name = "Review Test Plugin",
    version = "1.0.0",
    description = "A plugin for testing the review flow",
    author = "Test Author",
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

const networkPlugin = `
Plugin = {
    name = "Network Plugin",
    version = "1.0.0",
    description = "Plugin with network access",
    author = "Test Author",
    network = {
        ["api.example.com"] = {"GET", "POST"},
    },
    clipboard = true,
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

test.describe('Plugin Review Flow', () => {
    test.beforeEach(async ({ app }) => {
        await app.deleteAllPlugins();
        await app.deleteAllTags();
    });

    test('should show review modal with plugin details on file import', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(simplePlugin), 'lua');

        // Use the preview API directly (can't trigger file dialog in e2e)
        const preview = await app.page.evaluate(async (p) => {
            // @ts-ignore
            return window.go.main.PluginService.PreviewPluginFromPath(p);
        }, pluginPath);

        expect(preview).toBeTruthy();
        expect(preview.name).toBe('Review Test Plugin');
        expect(preview.version).toBe('1.0.0');
        expect(preview.author).toBe('Test Author');
        expect(preview.source).toBe(pluginPath);
    });

    test('should install plugin after confirm', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(simplePlugin), 'lua');

        const result = await app.confirmPluginInstall(pluginPath);
        expect(result).toBeTruthy();
        expect(result.name).toBe('Review Test Plugin');

        await app.expectPluginCount(1);
    });

    test('should show network permissions in preview', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(networkPlugin), 'lua');

        const preview = await app.page.evaluate(async (p) => {
            // @ts-ignore
            return window.go.main.PluginService.PreviewPluginFromPath(p);
        }, pluginPath);

        expect(preview.network).toBeTruthy();
        expect(preview.network['api.example.com']).toContain('GET');
        expect(preview.network['api.example.com']).toContain('POST');
        expect(preview.clipboard).toBe(true);
    });
});
