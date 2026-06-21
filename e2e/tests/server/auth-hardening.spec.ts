import { APIRequestContext, expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

async function scopedContext(server: { url: string }, key: string): Promise<APIRequestContext> {
  return request.newContext({
    baseURL: server.url,
    extraHTTPHeaders: { Authorization: `Bearer ${key}` },
  });
}

test('tag-scoped keys cannot read foreign clips via folder_tag or open the event stream', async () => {
  const server = await spawnServer();
  try {
    const admin = await authedRequestContext(request, server);
    let scopedKey = '';
    let scopeTagId = 0;
    let foreignTagId = 0;
    try {
      const tScope = await admin.post('/api/v1/tags', { data: { name: 'scope-tag' } });
      expect(tScope.status()).toBe(201);
      scopeTagId = (await tScope.json()).id;

      const tForeign = await admin.post('/api/v1/tags', { data: { name: 'foreign-tag' } });
      expect(tForeign.status()).toBe(201);
      foreignTagId = (await tForeign.json()).id;

      // A clip the scoped key must never see, tagged into the foreign tree.
      const upload = await admin.post('/api/v1/clips?filename=secret.txt', {
        multipart: { file: { name: 'secret.txt', mimeType: 'text/plain', buffer: Buffer.from('top secret') } },
      });
      expect(upload.status()).toBe(201);
      const foreignClipId = (await upload.json()).id;
      const assign = await admin.put(`/api/v1/clips/${foreignClipId}/tags/${foreignTagId}`);
      expect(assign.status()).toBe(204);

      const minted = await admin.post('/api/v1/keys', {
        data: { name: 'scoped-viewer', role: 'viewer', scoped_tag_id: scopeTagId },
      });
      expect(minted.status()).toBe(201);
      scopedKey = (await minted.json()).key;
      expect(scopedKey).toMatch(/^mp_/);
    } finally {
      await admin.dispose();
    }

    const scoped = await scopedContext(server, scopedKey);
    try {
      // H2: folder_tag must respect scope — a foreign folder yields nothing.
      const foreign = await scoped.get(`/api/v1/clips?folder_tag=${foreignTagId}&archived=false`);
      expect(foreign.status()).toBe(200);
      const body = await foreign.json();
      expect(body.clips).toEqual([]);
      expect(body.total).toBe(0);

      // H1: the broadcast event stream is closed to tag-scoped keys.
      const sse = await scoped.get('/api/v1/events');
      expect(sse.status()).toBe(403);
    } finally {
      await scoped.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('clip data is served as a hardened, non-inline download', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/clips?filename=stored.html', {
        multipart: { file: { name: 'stored.html', mimeType: 'text/html', buffer: Buffer.from('<script>alert(document.cookie)</script>') } },
      });
      expect(upload.status()).toBe(201);
      const clipId = (await upload.json()).id;

      const data = await ctx.get(`/api/v1/clips/${clipId}/data`);
      expect(data.status()).toBe(200);
      const headers = data.headers();
      // H3: nosniff + attachment + sandbox CSP keep a stored HTML clip from
      // executing script in the app origin.
      expect(headers['x-content-type-options']).toBe('nosniff');
      expect(headers['content-disposition'] ?? '').toMatch(/^attachment/);
      expect(headers['content-security-policy'] ?? '').toContain('sandbox');
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
