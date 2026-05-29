import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

test('REST API keys can be created, listed, and revoked', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const create = await ctx.post('/api/v1/keys', {
        data: { name: 'server-e2e-viewer', role: 'viewer' },
      });
      expect(create.status()).toBe(201);
      const created = await create.json();
      expect(created.key).toMatch(/^mp_[a-f0-9]{32}$/);
      expect(created.info.name).toBe('server-e2e-viewer');
      expect(created.info.role).toBe('viewer');

      const list = await ctx.get('/api/v1/keys');
      expect(list.status()).toBe(200);
      const keys = await list.json();
      expect(keys.some((key: any) => key.id === created.info.id && !key.is_revoked)).toBe(true);

      const revoke = await ctx.delete(`/api/v1/keys/${created.info.id}`);
      expect(revoke.status()).toBe(204);

      const after = await ctx.get('/api/v1/keys');
      const afterKeys = await after.json();
      expect(afterKeys.find((key: any) => key.id === created.info.id)?.is_revoked).toBe(true);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
