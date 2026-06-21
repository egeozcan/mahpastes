import { expect, request, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

const MINIMAL_PLUGIN = `Plugin = {
    name = "E2E Upload Plugin",
    version = "0.1.0",
    description = "Uploaded through the headless REST API in an e2e test.",
    author = "e2e",
}
`;

function pluginMultipart(buffer: Buffer) {
  return {
    file: {
      name: 'e2e-upload-plugin.lua',
      mimeType: 'text/x-lua',
      buffer,
    },
  };
}

test('a local plugin file can be uploaded, reviewed, and confirmed via REST', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      // Step 1: upload the file -> permission preview keyed by an opaque token.
      const upload = await ctx.post('/api/v1/plugins/upload', {
        multipart: pluginMultipart(Buffer.from(MINIMAL_PLUGIN)),
      });
      expect(upload.status()).toBe(200);
      const preview = await upload.json();
      expect(preview.name).toBe('E2E Upload Plugin');
      expect(preview.version).toBe('0.1.0');
      // The token is opaque and must not be a URL or path the server could re-read.
      expect(preview.source).toMatch(/^upload:[0-9a-f]{32}$/);

      // Step 2: confirm with the token -> installs the exact reviewed content.
      const confirm = await ctx.post('/api/v1/plugins/confirm', {
        data: { source: preview.source },
      });
      expect(confirm.status()).toBe(201);
      const info = await confirm.json();
      expect(info.name).toBe('E2E Upload Plugin');
      expect(info.id).toBeGreaterThan(0);

      // Step 3: it shows up in the plugin list, with no update source_url.
      const list = await ctx.get('/api/v1/plugins');
      const body = await list.json();
      const installed = body.plugins.find((p: any) => p.id === info.id);
      expect(installed).toBeTruthy();
      expect(installed.name).toBe('E2E Upload Plugin');

      // Cleanup so a re-run on the same data dir starts clean.
      const del = await ctx.delete(`/api/v1/plugins/${info.id}`);
      expect(del.status()).toBe(204);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('the token is single-use: a second confirm fails', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/plugins/upload', {
        multipart: pluginMultipart(Buffer.from(MINIMAL_PLUGIN)),
      });
      const { source } = await upload.json();

      const first = await ctx.post('/api/v1/plugins/confirm', { data: { source } });
      expect(first.status()).toBe(201);
      const info = await first.json();

      const second = await ctx.post('/api/v1/plugins/confirm', { data: { source } });
      // The pending content was consumed; the token is not a real path/URL, so the
      // re-install attempt fails rather than silently succeeding.
      expect(second.status()).toBe(400);

      await ctx.delete(`/api/v1/plugins/${info.id}`);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});

test('uploading a file with no manifest name is rejected', async () => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(request, server);
    try {
      const upload = await ctx.post('/api/v1/plugins/upload', {
        multipart: pluginMultipart(Buffer.from('-- not a plugin\nlocal x = 1\n')),
      });
      expect(upload.status()).toBe(400);
    } finally {
      await ctx.dispose();
    }
  } finally {
    await server.stop();
  }
});
