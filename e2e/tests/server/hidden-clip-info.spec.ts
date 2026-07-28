import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

test('hidden-info reports clips a tag filter matched but hidden tags withheld', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const mkTag = async (name: string) => {
        const res = await ctx.post('/api/v1/tags', { data: { name } });
        expect(res.status()).toBe(201);
        return (await res.json()).id as number;
      };
      const contacts = await mkTag('contacts');
      const web = await mkTag('web');
      const webContacts = await mkTag('web/contacts');

      const upload = await ctx.post('/api/v1/clips?filename=card.html', {
        multipart: {
          file: { name: 'card.html', mimeType: 'text/html', buffer: Buffer.from('<p>card</p>') },
        },
      });
      expect(upload.status()).toBe(201);
      const clipID = (await upload.json()).id;
      expect((await ctx.put(`/api/v1/clips/${clipID}/tags/${contacts}`)).ok()).toBe(true);
      expect((await ctx.put(`/api/v1/clips/${clipID}/tags/${webContacts}`)).ok()).toBe(true);

      // Filtering by contacts with web hidden: the clip drops out of the list...
      const list = await ctx.get(`/api/v1/clips?tag=${contacts}&hidden=${web}`);
      expect(list.status()).toBe(200);
      expect((await list.json()).clips).toHaveLength(0);

      // ...and hidden-info accounts for exactly that clip.
      const info = await ctx.get(`/api/v1/clips/hidden-info?tag=${contacts}&hidden=${web}`);
      expect(info.status()).toBe(200);
      expect(await info.json()).toEqual({ count: 1, tags: ['web/contacts'] });

      // Nothing hidden, nothing to report.
      const none = await ctx.get(`/api/v1/clips/hidden-info?tag=${contacts}`);
      expect(await none.json()).toEqual({ count: 0, tags: [] });
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
