import { expect, request, test } from '@playwright/test';
import { authedRequestContext, runMP, spawnServer } from '../../fixtures/server-fixtures';

test('mahpastesd starts, persists a bootstrap key to a file (not the log), and answers REST + CLI status', async () => {
  const server = await spawnServer();
  try {
    expect(server.bootstrapKey).toMatch(/^mp_[a-f0-9]{32}$/);
    // L1: the durable admin secret must not be written to the log.
    expect(server.logs()).not.toContain(server.bootstrapKey);

    const ctx = await authedRequestContext(request, server);
    try {
      const res = await ctx.get('/api/v1/status');
      expect(res.status()).toBe(200);
      const status = await res.json();
      expect(status.running).toBe(true);
      expect(status.url).toBe(server.url);
    } finally {
      await ctx.dispose();
    }

    const status = await runMP(server, ['api', 'status']);
    expect(status.stdout).toContain('Connected to mahpastes API');

    const list = await runMP(server, ['clip', 'list', '--json']);
    const parsed = JSON.parse(list.stdout);
    expect(parsed.clips).toEqual([]);
  } finally {
    await server.stop();
  }
});
