import { test, expect } from '../../fixtures/test-fixtures';
import { request as playwrightRequest } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

/**
 * Helper: create a temp directory with a JSON file of the given name and content.
 * Returns the absolute path to the file.
 */
function createJSONFile(filename: string, data: any): string {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'mahpastes-jsonapi-'));
  const filePath = path.join(tmpDir, filename);
  fs.writeFileSync(filePath, JSON.stringify(data));
  return filePath;
}

/**
 * Helper: create an APIRequestContext with a base URL for the served tag,
 * visit the root to obtain the auth cookie, and return the context.
 * The context maintains cookies automatically.
 */
async function createAuthenticatedContext(port: number) {
  const ctx = await playwrightRequest.newContext({
    baseURL: `http://127.0.0.1:${port}`,
  });
  // Visit root to get the auth cookie set
  await ctx.get('/');
  return ctx;
}

test.describe('Serve - JSON API', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should return 404 when apiAccess is none', async ({ app }) => {
    const filePath = createJSONFile('items.json', [{ id: 1, name: 'Alice' }]);
    await app.uploadFile(filePath);
    await app.createTag('api-none');
    await app.addTagToClip('items.json', 'api-none');

    const { port } = await app.startServingTag('api-none', 'none');

    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });
    try {
      const res = await ctx.get('/_api/items');
      expect(res.status()).toBe(404);
    } finally {
      await ctx.dispose();
    }
  });

  test('should require cookie auth', async ({ app }) => {
    const filePath = createJSONFile('items.json', [{ id: 1, name: 'Alice' }]);
    await app.uploadFile(filePath);
    await app.createTag('api-auth');
    await app.addTagToClip('items.json', 'api-auth');

    const { port } = await app.startServingTag('api-auth', 'readwrite');

    // Make request WITHOUT visiting root first (no cookie)
    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });
    try {
      const res = await ctx.get('/_api/items');
      expect(res.status()).toBe(401);
    } finally {
      await ctx.dispose();
    }
  });

  test('should GET full JSON clip', async ({ app }) => {
    const data = [
      { id: 1, name: 'Alice' },
      { id: 2, name: 'Bob' },
    ];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-get');
    await app.addTagToClip('users.json', 'api-get');

    const { port } = await app.startServingTag('api-get', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const res = await ctx.get('/_api/users');
      expect(res.status()).toBe(200);
      const body = await res.json();
      expect(body).toHaveLength(2);
      expect(body[0].name).toBe('Alice');
      expect(body[1].name).toBe('Bob');
    } finally {
      await ctx.dispose();
    }
  });

  test('should GET element by id', async ({ app }) => {
    const data = [
      { id: 1, name: 'Alice' },
      { id: 2, name: 'Bob' },
    ];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-getid');
    await app.addTagToClip('users.json', 'api-getid');

    const { port } = await app.startServingTag('api-getid', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const res = await ctx.get('/_api/users/2');
      expect(res.status()).toBe(200);
      const body = await res.json();
      expect(body.id).toBe(2);
      expect(body.name).toBe('Bob');
    } finally {
      await ctx.dispose();
    }
  });

  test('should POST to array with auto-increment id', async ({ app }) => {
    const data = [{ id: 1, name: 'Alice' }];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-post');
    await app.addTagToClip('users.json', 'api-post');

    const { port } = await app.startServingTag('api-post', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const postRes = await ctx.post('/_api/users', {
        data: { name: 'Bob' },
      });
      expect(postRes.status()).toBe(201);
      const postBody = await postRes.json();
      expect(postBody.id).toBe(2);
      expect(postBody.name).toBe('Bob');

      // Verify the array now has 2 elements
      const getRes = await ctx.get('/_api/users');
      expect(getRes.status()).toBe(200);
      const getBody = await getRes.json();
      expect(getBody).toHaveLength(2);
    } finally {
      await ctx.dispose();
    }
  });

  test('should PUT to replace element', async ({ app }) => {
    const data = [{ id: 1, name: 'Alice', role: 'dev' }];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-put');
    await app.addTagToClip('users.json', 'api-put');

    const { port } = await app.startServingTag('api-put', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const putRes = await ctx.put('/_api/users/1', {
        data: { name: 'Alicia', role: 'lead' },
      });
      expect(putRes.status()).toBe(200);

      // Verify the replacement (id should be preserved)
      const getRes = await ctx.get('/_api/users/1');
      expect(getRes.status()).toBe(200);
      const body = await getRes.json();
      expect(body.name).toBe('Alicia');
      expect(body.role).toBe('lead');
      expect(body.id).toBe(1);
    } finally {
      await ctx.dispose();
    }
  });

  test('should PATCH with merge semantics', async ({ app }) => {
    const data = [{ id: 1, name: 'Alice', role: 'dev' }];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-patch');
    await app.addTagToClip('users.json', 'api-patch');

    const { port } = await app.startServingTag('api-patch', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const patchRes = await ctx.patch('/_api/users/1', {
        data: { role: 'lead' },
      });
      expect(patchRes.status()).toBe(200);

      // Verify merge: original fields preserved, role updated
      const getRes = await ctx.get('/_api/users/1');
      expect(getRes.status()).toBe(200);
      const body = await getRes.json();
      expect(body.name).toBe('Alice');
      expect(body.role).toBe('lead');
      expect(body.id).toBe(1);
    } finally {
      await ctx.dispose();
    }
  });

  test('should DELETE element from array', async ({ app }) => {
    const data = [
      { id: 1, name: 'Alice' },
      { id: 2, name: 'Bob' },
    ];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-delete');
    await app.addTagToClip('users.json', 'api-delete');

    const { port } = await app.startServingTag('api-delete', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      const delRes = await ctx.delete('/_api/users/1');
      expect(delRes.status()).toBe(204);

      // Verify the element was removed
      const getRes = await ctx.get('/_api/users');
      expect(getRes.status()).toBe(200);
      const body = await getRes.json();
      expect(body).toHaveLength(1);
      expect(body[0].id).toBe(2);
      expect(body[0].name).toBe('Bob');
    } finally {
      await ctx.dispose();
    }
  });

  test('should enforce read-only mode', async ({ app }) => {
    const data = [{ id: 1, name: 'Alice' }];
    const filePath = createJSONFile('users.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-readonly');
    await app.addTagToClip('users.json', 'api-readonly');

    const { port } = await app.startServingTag('api-readonly', 'read');
    const ctx = await createAuthenticatedContext(port);
    try {
      // GET should work
      const getRes = await ctx.get('/_api/users');
      expect(getRes.status()).toBe(200);
      const body = await getRes.json();
      expect(body).toHaveLength(1);

      // POST should be forbidden
      const postRes = await ctx.post('/_api/users', {
        data: { name: 'Bob' },
      });
      expect(postRes.status()).toBe(403);

      // PUT should be forbidden
      const putRes = await ctx.put('/_api/users/1', {
        data: { name: 'Alicia' },
      });
      expect(putRes.status()).toBe(403);

      // DELETE should be forbidden
      const delRes = await ctx.delete('/_api/users/1');
      expect(delRes.status()).toBe(403);
    } finally {
      await ctx.dispose();
    }
  });

  test('should navigate nested object paths', async ({ app }) => {
    const data = {
      theme: {
        colors: {
          primary: '#ff0000',
          secondary: '#00ff00',
        },
        font: 'monospace',
      },
      version: 1,
    };
    const filePath = createJSONFile('config.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-nested');
    await app.addTagToClip('config.json', 'api-nested');

    const { port } = await app.startServingTag('api-nested', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      // GET nested path
      const getColors = await ctx.get('/_api/config/theme/colors');
      expect(getColors.status()).toBe(200);
      const colors = await getColors.json();
      expect(colors.primary).toBe('#ff0000');
      expect(colors.secondary).toBe('#00ff00');

      // GET deep nested value
      const getPrimary = await ctx.get('/_api/config/theme/colors/primary');
      expect(getPrimary.status()).toBe(200);
      const primary = await getPrimary.json();
      expect(primary).toBe('#ff0000');

      // PUT to update a deep nested value (string must be sent as JSON body)
      const putRes = await ctx.put('/_api/config/theme/colors/primary', {
        headers: { 'Content-Type': 'application/json' },
        data: JSON.stringify('#0000ff'),
      });
      expect(putRes.status()).toBe(200);

      // Verify the update
      const getUpdated = await ctx.get('/_api/config/theme/colors/primary');
      expect(getUpdated.status()).toBe(200);
      const updated = await getUpdated.json();
      expect(updated).toBe('#0000ff');

      // Verify other fields are untouched
      const getFont = await ctx.get('/_api/config/theme/font');
      expect(getFont.status()).toBe(200);
      const font = await getFont.json();
      expect(font).toBe('monospace');
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject _api tag names', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      try {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('_api');
        return { success: true, error: '' };
      } catch (e: any) {
        return { success: false, error: e.message || String(e) };
      }
    });
    expect(result.success).toBe(false);
    expect(result.error).toContain('reserved');
  });

  test('PATCH array element should not return 500 when patch includes id field', async ({ app }) => {
    // Bug: handleJSONPatch calls mergePatch(obj, patch) which modifies the array
    // element in-place (since navigateJSON returns a reference into the data
    // structure). Then it calls setJSON(&data, jsonPath, obj) to "write back"
    // the modified element. setJSON navigates to the parent array and looks for
    // the element by matching its "id" field against the path segment.
    //
    // When the patch body includes an "id" field with a DIFFERENT value,
    // mergePatch changes obj["id"] in-place BEFORE setJSON tries to locate the
    // element by the ORIGINAL id from the URL path. setJSON searches for an
    // element with the original id, but the element now has the new id, so the
    // lookup fails with "no element with id <original> in array" and the handler
    // returns 500 Internal Server Error.
    //
    // Expected behavior: either reject the id change with 400, or apply it and
    // return 200. A 500 is never the correct response for valid user input.
    const data = [
      { id: 1, name: 'Alice', role: 'dev' },
      { id: 2, name: 'Bob', role: 'dev' },
    ];
    const filePath = createJSONFile('team.json', data);
    await app.uploadFile(filePath);
    await app.createTag('api-patch-idbug');
    await app.addTagToClip('team.json', 'api-patch-idbug');

    const { port } = await app.startServingTag('api-patch-idbug', 'readwrite');
    const ctx = await createAuthenticatedContext(port);
    try {
      // PATCH element with id=1, including an id change in the body.
      // The server should NOT return 500.
      const patchRes = await ctx.patch('/_api/team/1', {
        data: { id: 99, role: 'lead' },
      });
      // A well-behaved server returns either:
      //   200 if the id change is accepted, or
      //   400 if id changes are disallowed.
      // It must NOT return 500 (Internal Server Error).
      expect(patchRes.status()).not.toBe(500);

      // The server strips the id field from the patch (id is immutable),
      // so the element stays at id=1 with the other fields patched.
      if (patchRes.status() === 200) {
        const getOriginal = await ctx.get('/_api/team/1');
        expect(getOriginal.status()).toBe(200);
        const body = await getOriginal.json();
        expect(body.id).toBe(1);       // id is preserved
        expect(body.role).toBe('lead'); // role was patched
        expect(body.name).toBe('Alice'); // name unchanged
      }
    } finally {
      await ctx.dispose();
    }
  });
});
