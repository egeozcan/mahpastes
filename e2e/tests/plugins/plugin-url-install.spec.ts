import { test, expect } from '../../fixtures/test-fixtures';
import * as http from 'http';

const urlPlugin = `
Plugin = {
    name = "URL Test Plugin",
    version = "1.0.0",
    description = "Installed from URL",
    author = "URL Author",
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

let server: http.Server;
let serverURL: string;

test.describe('Plugin URL Install', () => {
    test.beforeAll(async () => {
        server = http.createServer((req, res) => {
            res.writeHead(200, { 'Content-Type': 'text/plain' });
            res.end(urlPlugin);
        });
        await new Promise<void>((resolve) => {
            server.listen(0, '127.0.0.1', () => {
                const addr = server.address();
                if (addr && typeof addr !== 'string') {
                    serverURL = `http://127.0.0.1:${addr.port}/test-plugin.lua`;
                }
                resolve();
            });
        });
    });

    test.afterAll(async () => {
        if (server) {
            await new Promise<void>((resolve) => server.close(() => resolve()));
        }
    });

    test.beforeEach(async ({ app }) => {
        await app.deleteAllPlugins();
        await app.deleteAllTags();
    });

    test('should preview plugin from URL', async ({ app }) => {
        const preview = await app.previewPluginFromURL(serverURL);
        expect(preview).toBeTruthy();
        expect(preview.name).toBe('URL Test Plugin');
        expect(preview.version).toBe('1.0.0');
        expect(preview.source).toBe(serverURL);
    });

    test('should install plugin from URL after confirm', async ({ app }) => {
        const result = await app.confirmPluginInstall(serverURL);
        expect(result).toBeTruthy();
        expect(result.name).toBe('URL Test Plugin');
        await app.expectPluginCount(1);
    });

    test('should reject invalid URL scheme', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.previewPluginFromURL('ftp://invalid.com/plugin.lua');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });

    test('should reject unreachable URL', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.previewPluginFromURL('http://127.0.0.1:1/nonexistent.lua');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });
});
