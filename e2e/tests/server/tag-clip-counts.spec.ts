import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

// Folder cards in the web UI read every card's count from one request
// (rest-glue GetDescendantClipCounts → GET /api/v1/tags/clip-counts).
test('clip-counts returns live descendant counts for many tags at once', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const mkTag = async (name: string) => {
        const res = await ctx.post('/api/v1/tags', { data: { name } });
        expect(res.status()).toBe(201);
        return (await res.json()).id as number;
      };
      const upload = async (name: string) => {
        const res = await ctx.post(`/api/v1/clips?filename=${name}`, {
          multipart: { file: { name, mimeType: 'text/plain', buffer: Buffer.from(name) } },
        });
        expect(res.status()).toBe(201);
        return (await res.json()).id as number;
      };
      const work = await mkTag('work');
      const sub = await mkTag('work/sub');
      const other = await mkTag('other');

      const a = await upload('a.txt');
      const b = await upload('b.txt');
      const c = await upload('c.txt');
      expect((await ctx.put(`/api/v1/clips/${a}/tags/${work}`)).ok()).toBe(true);
      expect((await ctx.put(`/api/v1/clips/${b}/tags/${sub}`)).ok()).toBe(true);
      expect((await ctx.put(`/api/v1/clips/${c}/tags/${sub}`)).ok()).toBe(true);
      expect((await ctx.put(`/api/v1/clips/${c}/archive`, { data: {} })).ok()).toBe(true);

      const live = await ctx.get(`/api/v1/tags/clip-counts?archived=false&tag=${work}&tag=${sub}&tag=${other}`);
      expect(live.status()).toBe(200);
      expect(await live.json()).toEqual({ [work]: 2, [sub]: 1, [other]: 0 });

      const archived = await ctx.get(`/api/v1/tags/clip-counts?archived=true&tag=${work}&tag=${sub}`);
      expect(await archived.json()).toEqual({ [work]: 1, [sub]: 1 });

      const bad = await ctx.get('/api/v1/tags/clip-counts?tag=nope');
      expect(bad.status()).toBe(400);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
