import { test, expect } from '../../fixtures/test-fixtures';
import { request as playwrightRequest } from '@playwright/test';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';

/**
 * Bug: REST API bulk download silently loses clips with duplicate filenames.
 *
 * The API handler `handleBulkDownload` in `api_manager.go` uses the clip's
 * filename directly as the ZIP entry name. When two clips share a filename
 * (e.g., both called "photo.png"), the second ZIP entry overwrites the first,
 * resulting in silent data loss.
 *
 * Compare with the Wails-side `BulkDownloadToFile` which prefixes each entry
 * with `{id}_` to guarantee uniqueness.
 */
test.describe('API Bulk Download - filename collision', () => {
  let apiPort: number;
  let apiKey: string;

  test.afterEach(async ({ app }) => {
    // Stop the API server
    await app.page.evaluate(async () => {
      try {
        // @ts-ignore - Wails runtime
        await window.go.main.APIService.StopAPI();
      } catch {
        // may not be running
      }
    });
  });

  test('should not lose clips when two clips share the same filename', async ({ app }) => {
    // Upload two images with different content (different colors)
    const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const file2 = await createTempFile(generateTestImage(50, 50, [0, 0, 255]), 'png');

    await app.uploadFiles([file1, file2]);
    await app.expectClipCount(2);

    // Get clip IDs and rename both to the same filename
    const clipIds: number[] = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      return clips.map((c: any) => c.id);
    });

    expect(clipIds).toHaveLength(2);

    // Rename both clips to the same filename
    for (const id of clipIds) {
      await app.page.evaluate(async (clipId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.RenameClip(clipId, 'photo.png');
      }, id);
    }

    // Start the REST API and create an API key
    const apiInfo = await app.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const port = await window.go.main.ServeService.GetRandomPort();
      // @ts-ignore - Wails runtime
      const status = await window.go.main.APIService.StartAPI(port, false);
      // @ts-ignore - Wails runtime
      const keyResult = await window.go.main.APIService.CreateAPIKey('test-key', 'admin', 0);
      return { port: status.port, key: keyResult.key };
    });

    apiPort = apiInfo.port;
    apiKey = apiInfo.key;

    // Call bulk download via the REST API
    const ctx = await playwrightRequest.newContext({
      baseURL: `http://127.0.0.1:${apiPort}`,
      extraHTTPHeaders: {
        Authorization: `Bearer ${apiKey}`,
      },
    });

    try {
      const response = await ctx.post('/api/v1/clips/bulk/download', {
        data: { ids: clipIds },
      });

      expect(response.status()).toBe(200);
      expect(response.headers()['content-type']).toBe('application/zip');

      const zipBuffer = await response.body();
      expect(zipBuffer.length).toBeGreaterThan(0);

      // Parse the ZIP to count entries.
      // In a valid ZIP, each clip should have its own unique entry.
      // The ZIP central directory is at the end; we scan for local file headers (PK\x03\x04).
      const entryNames = parseZipEntryNames(zipBuffer);

      // BUG: The API handler uses the raw filename without deduplication.
      // Both clips are named "photo.png", so we expect 2 distinct entries.
      // Instead, the ZIP contains only 1 entry because the second "photo.png"
      // overwrites the first -- silent data loss.
      //
      // The Wails-side BulkDownloadToFile correctly prefixes with "{id}_",
      // but the API handler handleBulkDownload does not.
      //
      // We must get 2 entries in the ZIP since we requested 2 clips.
      expect(entryNames).toHaveLength(2);

      // And all entry names should be unique (no collision).
      const uniqueNames = new Set(entryNames);
      expect(uniqueNames.size).toBe(entryNames.length);
    } finally {
      await ctx.dispose();
    }
  });
});

/**
 * Parse ZIP entry names from the central directory (at the end of the ZIP).
 * This is more reliable than parsing local headers because Go's zip.Writer
 * uses data descriptors (compressed size = 0 in local headers).
 *
 * Central directory file header signature: PK\x01\x02 (0x02014b50)
 * Filename length at offset 28, filename at offset 46.
 */
function parseZipEntryNames(buf: Buffer): string[] {
  const names: string[] = [];
  const CENTRAL_DIR_SIG = 0x02014b50;

  // Scan for central directory headers
  for (let offset = 0; offset + 46 <= buf.length; offset++) {
    if (buf.readUInt32LE(offset) !== CENTRAL_DIR_SIG) continue;

    const fileNameLen = buf.readUInt16LE(offset + 28);
    if (offset + 46 + fileNameLen > buf.length) break;

    const name = buf.toString('utf8', offset + 46, offset + 46 + fileNameLen);
    names.push(name);

    // Skip past this header to avoid re-matching
    const extraLen = buf.readUInt16LE(offset + 30);
    const commentLen = buf.readUInt16LE(offset + 32);
    offset += 45 + fileNameLen + extraLen + commentLen; // loop will +1
  }

  return names;
}
