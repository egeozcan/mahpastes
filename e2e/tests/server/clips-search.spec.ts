import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

/**
 * The web UI's search options reach the backend through these query params, so
 * server mode gets the same two widenings the desktop app does. `search_content`
 * is what switches the endpoint from the older preview-only post-filter to the
 * database-side search — its presence, not its value.
 */
test('search_content decides whether clip bodies are searched', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/clips?filename=minutes.txt', {
        multipart: {
          file: {
            name: 'minutes.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('agenda item: pelican migration'),
          },
        },
      });
      expect(upload.status()).toBe(201);
      const clip = await upload.json();

      const shallow = await ctx.get('/api/v1/clips?search=pelican&search_content=false');
      expect(shallow.status()).toBe(200);
      expect((await shallow.json()).clips.some((c: any) => c.id === clip.id)).toBe(false);

      const deep = await ctx.get('/api/v1/clips?search=pelican&search_content=true');
      expect(deep.status()).toBe(200);
      expect((await deep.json()).clips.some((c: any) => c.id === clip.id)).toBe(true);

      // The filename remains searchable either way.
      const byName = await ctx.get('/api/v1/clips?search=minutes&search_content=false');
      expect((await byName.json()).clips.some((c: any) => c.id === clip.id)).toBe(true);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
