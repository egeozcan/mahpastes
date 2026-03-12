import { test, expect } from '../../fixtures/test-fixtures';
import { request as playwrightRequest } from '@playwright/test';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Helper: create an APIRequestContext with a base URL for the served tag,
 * visit the root to obtain the auth cookie, and return the context.
 */
async function createAuthenticatedContext(port: number) {
  const ctx = await playwrightRequest.newContext({
    baseURL: `http://127.0.0.1:${port}`,
  });
  // Visit root to get the auth cookie set
  await ctx.get('/');
  return ctx;
}

test.describe('Serve - File Upload', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should upload a file to the served tag', async ({ app }) => {
    await app.createTag('upload-basic');
    const { port } = await app.startServingTag('upload-basic', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const imageData = generateTestImage(50, 50, [255, 0, 0]);

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test-upload.png',
            mimeType: 'image/png',
            buffer: imageData,
          },
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.id).toBeGreaterThan(0);
      expect(body.filename).toBe('test-upload.png');
      expect(body.content_type).toBe('image/png');
      expect(body.tag).toBe('upload-basic');

      // Verify the file is now accessible on the server
      const fileRes = await ctx.get('/test-upload.png');
      expect(fileRes.status()).toBe(200);
    } finally {
      await ctx.dispose();
    }
  });

  test('should upload a file with subtag', async ({ app }) => {
    await app.createTag('upload-sub');
    const { port } = await app.startServingTag('upload-sub', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const textContent = Buffer.from('hello world', 'utf-8');

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'note.txt',
            mimeType: 'text/plain',
            buffer: textContent,
          },
          tag: 'child/grandchild',
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.tag).toBe('upload-sub/child/grandchild');
      expect(body.tag_id).toBeGreaterThan(0);

      // Verify the subtag was auto-created and file is accessible there
      const fileRes = await ctx.get('/child/grandchild/note.txt');
      expect(fileRes.status()).toBe(200);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload in read-only mode', async ({ app }) => {
    await app.createTag('upload-readonly');
    const { port } = await app.startServingTag('upload-readonly', 'read');
    const ctx = await createAuthenticatedContext(port);

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      expect(res.status()).toBe(403);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload without auth cookie', async ({ app }) => {
    await app.createTag('upload-noauth');
    const { port } = await app.startServingTag('upload-noauth', 'readwrite');

    // Create context WITHOUT visiting root first (no cookie)
    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      expect(res.status()).toBe(401);
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject invalid tag path with traversal', async ({ app }) => {
    await app.createTag('upload-traverse');
    const { port } = await app.startServingTag('upload-traverse', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
          tag: '../evil',
        },
      });
      expect(res.status()).toBe(400);
      const body = await res.json();
      expect(body.error).toContain('..');
    } finally {
      await ctx.dispose();
    }
  });

  test('should reject upload when apiAccess is none', async ({ app }) => {
    await app.createTag('upload-none');
    const { port } = await app.startServingTag('upload-none', 'none');

    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${port}`,
    });

    try {
      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'test.txt',
            mimeType: 'text/plain',
            buffer: Buffer.from('test'),
          },
        },
      });
      expect(res.status()).toBe(404);
    } finally {
      await ctx.dispose();
    }
  });

  test('should detect content type from file content', async ({ app }) => {
    await app.createTag('upload-detect');
    const { port } = await app.startServingTag('upload-detect', 'readwrite');
    const ctx = await createAuthenticatedContext(port);

    try {
      const htmlContent = Buffer.from('<!DOCTYPE html><html><body>Hello</body></html>');

      const res = await ctx.post('/_api/_upload', {
        multipart: {
          file: {
            name: 'page.html',
            mimeType: 'text/plain',
            buffer: htmlContent,
          },
        },
      });
      expect(res.status()).toBe(201);
      const body = await res.json();
      expect(body.content_type).toBe('text/html');
    } finally {
      await ctx.dispose();
    }
  });
});
