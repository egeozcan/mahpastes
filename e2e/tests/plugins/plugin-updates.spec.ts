import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Plugin Update Settings', () => {
    test.beforeEach(async ({ app }) => {
        await app.deleteAllPlugins();
        await app.deleteAllTags();
    });

    test('should get default update check interval', async ({ app }) => {
        const interval = await app.getUpdateCheckInterval();
        expect(interval).toBe('24h');
    });

    test('should set and get update check interval', async ({ app }) => {
        await app.setUpdateCheckInterval('6h');
        const interval = await app.getUpdateCheckInterval();
        expect(interval).toBe('6h');

        // Reset to default
        await app.setUpdateCheckInterval('24h');
    });

    test('should reject invalid interval', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.setUpdateCheckInterval('invalid');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });
});
