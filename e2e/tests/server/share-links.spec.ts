import { expect, request, test } from '@playwright/test';
import { authedRequestContext, runMP, spawnServer } from '../../fixtures/server-fixtures';

async function uploadClip(ctx: any, name: string, body: string, mimeType = 'text/plain') {
  const res = await ctx.post(`/api/v1/clips?filename=${encodeURIComponent(name)}`, {
    multipart: { file: { name, mimeType, buffer: Buffer.from(body) } },
  });
  expect(res.status()).toBe(201);
  return res.json();
}

test('share link: mint, fetch unauthenticated, revoke -> 404', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const clip = await uploadClip(ctx, 'share.txt', 'public share body');

      const mint = await ctx.post('/api/v1/links', { data: { clip_id: clip.id } });
      expect(mint.status()).toBe(201);
      const link = await mint.json();
      expect(link.token).toBeTruthy();
      expect(link.path).toBe(`/s/${link.token}`);
      // The response must never leak more than a prefix in the info block.
      expect(link.info.token_prefix).toBe(`${link.token.slice(0, 8)}...`);

      // Anonymous fetch (no Authorization header) succeeds and is hardened.
      const anon = await request.newContext();
      try {
        const view = await anon.get(`${server.url}/s/${link.token}`);
        expect(view.status()).toBe(200);
        expect(await view.text()).toBe('public share body');
        expect(view.headers()['x-content-type-options']).toBe('nosniff');
        expect(view.headers()['content-security-policy']).toBe("default-src 'none'; sandbox");
        expect(view.headers()['content-disposition']).toContain('attachment');
        expect(view.headers()['cache-control']).toBe('no-store');

        // Revoke -> the same token now 404s immediately.
        const del = await ctx.delete(`/api/v1/links/${link.info.id}`);
        expect(del.status()).toBe(204);

        const after = await anon.get(`${server.url}/s/${link.token}`);
        expect(after.status()).toBe(404);
      } finally {
        await anon.dispose();
      }
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('share link: max-downloads caps access', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const clip = await uploadClip(ctx, 'capped.txt', 'limited');
      const mint = await ctx.post('/api/v1/links', { data: { clip_id: clip.id, max_downloads: 1 } });
      expect(mint.status()).toBe(201);
      const link = await mint.json();

      const anon = await request.newContext();
      try {
        expect((await anon.get(`${server.url}/s/${link.token}`)).status()).toBe(200);
        // Second fetch exceeds the cap.
        expect((await anon.get(`${server.url}/s/${link.token}`)).status()).toBe(404);
      } finally {
        await anon.dispose();
      }
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('share link: unknown token is a uniform 404', async () => {
  const server = await spawnServer();
  try {
    const anon = await request.newContext();
    try {
      const res = await anon.get(`${server.url}/s/0000000000000000000000000000000000000000000000000000000000000000`);
      expect(res.status()).toBe(404);
    } finally {
      await anon.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('share link: managed via mp CLI', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    let clipID: number;
    try {
      const clip = await uploadClip(ctx, 'cli.txt', 'cli body');
      clipID = clip.id;
    } finally {
      await ctx.dispose();
    }

    // Mint via the CLI in JSON mode and pull the token out.
    const created = await runMP(server, ['link', 'create', String(clipID), '--name', 'cli-link', '--json']);
    const result = JSON.parse(created.stdout);
    expect(result.token).toBeTruthy();
    expect(result.info.name).toBe('cli-link');

    const anon = await request.newContext();
    try {
      expect((await anon.get(`${server.url}/s/${result.token}`)).status()).toBe(200);

      // List shows it (prefix only, never the token).
      const listed = await runMP(server, ['link', 'list', '--json']);
      const links = JSON.parse(listed.stdout);
      expect(links.some((l: any) => l.id === result.info.id)).toBe(true);
      expect(listed.stdout).not.toContain(result.token);

      // Revoke via CLI -> 404.
      await runMP(server, ['link', 'revoke', String(result.info.id)]);
      expect((await anon.get(`${server.url}/s/${result.token}`)).status()).toBe(404);
    } finally {
      await anon.dispose();
    }
  } finally {
    await server.stop();
  }
});
