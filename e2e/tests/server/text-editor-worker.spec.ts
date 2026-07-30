import { expect, request as apiRequest, test } from '@playwright/test';
import { authedRequestContext, spawnServer } from '../../fixtures/server-fixtures';

// Served web mode is a genuinely different surface from `wails dev`: the assets
// come from the embedded FS through spaHandler, and the page carries the real
// `default-src 'self'` CSP. The desktop suite cannot cover either, so the worker
// probe is asserted here separately.

test('the validator worker loads in served web mode under the SPA CSP', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    // The login POST navigates to the app shell, which loads the whole frontend
    // including the committed text-editor bundle. This step measures ~9s
    // uncontended, so a 10s budget tipped over whenever the main config ran
    // these specs alongside four `wails dev` workers. 30s matches the config's
    // navigationTimeout.
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await page.waitForFunction(() => !!(window as any).MahpastesTextEditor, { timeout: 15000 });

    const cspViolations: string[] = [];
    page.on('console', (msg) => {
      if (/Content Security Policy/i.test(msg.text())) cspViolations.push(msg.text());
    });

    const result = await page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor, probe } = await api.probeWorkerSupport();
      if (!executor) return { probe, handshake: null, kind: null };
      const handshake = await executor.run({ op: api.OP_HANDSHAKE, generation: 1 });
      const kind = executor.kind;
      executor.dispose();
      return { probe, handshake, kind };
    });

    expect(result.probe.kind, `probe reason: ${result.probe.reason}`).toBe('worker');
    expect(result.kind).toBe('worker');
    expect(result.handshake).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
    expect(cspViolations).toEqual([]);
  } finally {
    await server.stop();
  }
});

test('the served CSP declares same-origin workers and no blob allowance', async ({ page }) => {
  const server = await spawnServer();
  try {
    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });

    // Fetch the shell again rather than racing a waitForResponse against the
    // redirect the click already triggered: goto() hands back its own main
    // response, so there is no listener-attached-too-late window.
    const shell = await page.goto(server.url);
    expect(shell?.status()).toBe(200);

    const csp = (await shell!.allHeaders())['content-security-policy'] || '';
    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain("worker-src 'self'");
    // The worker is a static same-origin script; a blob: worker source is not
    // wanted and must not creep in.
    expect(csp).not.toMatch(/worker-src[^;]*blob:/);
  } finally {
    await server.stop();
  }
});

test('the worker script is served with a JavaScript content type', async ({ request }) => {
  const server = await spawnServer();
  try {
    // Public asset, like the CSS and the classic scripts — it must not require a
    // session, or the worker would fail to load before login completes.
    const res = await request.get(`${server.url}/dist/text-validator.worker.js`);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toMatch(/javascript/);
    expect(await res.text()).toContain('GENERATED FILE');
  } finally {
    await server.stop();
  }
});

// Regression test for a real server-mode bug, not a refactor side effect.
//
// rest-glue.js's GetClipData returned raw base64 in `data` and a hardcoded
// `filename: ''`, while editor.js passed `clipData.data` straight through as the
// editor's text. Opening any text clip in server mode therefore showed base64,
// and Markdown detection could never fire because the filename was empty.
test('server mode shows decoded text and the real filename for a text clip', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(apiRequest, server);
    const body = 'plain text, not base64\nsecond line\n';
    const upload = await ctx.post('/api/v1/clips?filename=server-editor.txt', {
      multipart: {
        file: { name: 'server-editor.txt', mimeType: 'text/plain', buffer: Buffer.from(body) },
      },
    });
    expect(upload.status()).toBe(201);
    const clip = await upload.json();
    await ctx.dispose();

    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 30000 });

    // The single-endpoint retrieval: one request carries filename, content type,
    // bytes, and UTF-8 validity, so a concurrent update cannot pair one clip's
    // metadata with another revision's bytes.
    const payload = await page.evaluate((id) => (window as any).go.main.App.GetClipText(id), clip.id);
    expect(payload.filename).toBe('server-editor.txt');
    expect(payload.content_type).toBe('text/plain');
    expect(payload.data_encoding).toBe('base64');
    expect(payload.valid_utf8).toBe(true);

    await page.evaluate((id) => (window as any).openEditor(id), clip.id);
    await page.waitForSelector('#editor-modal.active');
    await page.waitForSelector('#text-editor-view:not(.hidden)');

    // Decoded text, not base64.
    await expect.poll(() => page.evaluate('TextClipEditor.getValue()')).toBe(body);
    // The real filename, not ''.
    await expect(page.locator('#editor-current-filename')).toHaveText('server-editor.txt');
  } finally {
    await server.stop();
  }
});

test('server mode detects Markdown from the real filename', async ({ page }) => {
  const server = await spawnServer();
  try {
    const ctx = await authedRequestContext(apiRequest, server);
    const upload = await ctx.post('/api/v1/clips?filename=server-notes.md', {
      multipart: {
        file: { name: 'server-notes.md', mimeType: 'text/markdown', buffer: Buffer.from('# Heading\n\nBody.\n') },
      },
    });
    expect(upload.status()).toBe(201);
    const clip = await upload.json();
    await ctx.dispose();

    await page.goto(server.url);
    await page.locator('#api-key').fill(server.bootstrapKey);
    await page.getByRole('button', { name: /sign in/i }).click();
    await page.waitForURL(`${server.url}/`, { timeout: 30000 });
    await page.waitForFunction(() => (window as any).__appReady === true, { timeout: 30000 });

    await page.evaluate((id) => (window as any).openEditor(id), clip.id);
    await page.waitForSelector('#editor-modal.active');

    // With filename: '' the tablist stayed hidden and Markdown was unreachable in
    // server mode entirely. Markdown is a Preview-default format, so a generic open
    // must also land on the Preview tab rather than merely offering it.
    await expect(page.locator('#editor-mode-tabs')).toBeVisible();
    await expect(page.locator('#editor-preview-tab')).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('#editor-current-filename')).toHaveText('server-notes.md');
  } finally {
    await server.stop();
  }
});
