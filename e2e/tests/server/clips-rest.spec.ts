import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

test('clips can be uploaded, listed, and deleted through REST', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/clips?filename=hello.txt', {
        multipart: {
          file: {
            name: 'hello.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('hello from server e2e'),
          },
        },
      });
      expect(upload.status()).toBe(201);
      const clip = await upload.json();
      expect(clip.filename).toBe('hello.txt');
      expect(clip.content_type).toBe('text/plain');

      const list = await ctx.get('/api/v1/clips?limit=10');
      expect(list.status()).toBe(200);
      const body = await list.json();
      expect(body.clips.some((item: any) => item.id === clip.id)).toBe(true);

      const del = await ctx.delete(`/api/v1/clips/${clip.id}`);
      expect(del.status()).toBe(204);

      const after = await ctx.get('/api/v1/clips?limit=10');
      const afterBody = await after.json();
      expect(afterBody.clips.some((item: any) => item.id === clip.id)).toBe(false);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
