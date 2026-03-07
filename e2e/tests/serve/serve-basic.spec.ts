import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, generateTestText, createTempFile } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Serve - Basic', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should start and stop serving a tag', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('serve-test');
    await app.addTagToClip(path.basename(imagePath), 'serve-test');

    const { port, url } = await app.startServingTag('serve-test');
    expect(port).toBeGreaterThan(0);
    expect(url).toContain(String(port));

    const statuses = await app.getServeStatus();
    expect(statuses.length).toBe(1);
    expect(statuses[0].running).toBe(true);

    // Fetch the file
    const response = await app.page.evaluate(async (fetchUrl: string) => {
      const res = await fetch(fetchUrl);
      return { status: res.status, contentType: res.headers.get('content-type') };
    }, `http://127.0.0.1:${port}/${path.basename(imagePath)}`);
    expect(response.status).toBe(200);
    expect(response.contentType).toContain('image/png');

    await app.stopServingTag('serve-test');
    const afterStop = await app.getServeStatus();
    expect(afterStop.length).toBe(0);
  });

  test('should serve directory listing when no index.html', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('dir-test');
    await app.addTagToClip(path.basename(imagePath), 'dir-test');

    const { port } = await app.startServingTag('dir-test');

    // HTML listing
    const htmlResult = await app.page.evaluate(async (fetchUrl: string) => {
      const res = await fetch(fetchUrl);
      return { status: res.status, body: await res.text() };
    }, `http://127.0.0.1:${port}/`);
    expect(htmlResult.status).toBe(200);
    expect(htmlResult.body).toContain(path.basename(imagePath));

    // JSON listing via Accept header
    const jsonResult = await app.page.evaluate(async (fetchUrl: string) => {
      const res = await fetch(fetchUrl, { headers: { 'Accept': 'application/json' } });
      return { status: res.status, body: await res.json() };
    }, `http://127.0.0.1:${port}/`);
    expect(jsonResult.status).toBe(200);
    expect(jsonResult.body.length).toBe(1);
    expect(jsonResult.body[0].name).toBe(path.basename(imagePath));
  });

  test('should return 404 for non-existent file', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(imagePath);
    await app.createTag('404-test');
    await app.addTagToClip(path.basename(imagePath), '404-test');

    const { port } = await app.startServingTag('404-test');

    const result = await app.page.evaluate(async (fetchUrl: string) => {
      const res = await fetch(fetchUrl);
      return { status: res.status };
    }, `http://127.0.0.1:${port}/nonexistent.txt`);
    expect(result.status).toBe(404);
  });

  test('should serve multiple tags simultaneously', async ({ app }) => {
    const img1 = await createTempFile(generateTestImage(100, 100, [255, 0, 0]), 'png');
    const img2 = await createTempFile(generateTestImage(100, 100, [0, 0, 255]), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);

    await app.createTag('multi-1');
    await app.createTag('multi-2');
    await app.addTagToClip(path.basename(img1), 'multi-1');
    await app.addTagToClip(path.basename(img2), 'multi-2');

    const s1 = await app.startServingTag('multi-1');
    const s2 = await app.startServingTag('multi-2');

    expect(s1.port).not.toBe(s2.port);

    const statuses = await app.getServeStatus();
    expect(statuses.length).toBe(2);
  });
});
