import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, generateTestText } from '../../helpers/test-data';

test.describe('Folder Drop', () => {
  test.afterEach(async ({ app }) => {
    // Exit folder mode if active
    const btn = app.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed === 'true') {
      await btn.click();
    }
    await app.clearTagFilters();
    await app.deleteAllTags();
  });

  test('folder drop in folder mode creates tags and uploads files', async ({ app }) => {
    // Enable folder mode
    await app.toggleFolderMode();

    // Prepare serializable file data: a folder "photos" with 2 images
    const img1Base64 = generateTestImage(50, 50, [255, 0, 0]).toString('base64');
    const img2Base64 = generateTestImage(50, 50, [0, 255, 0]).toString('base64');

    const fileEntries = [
      { relativePath: 'photos/red.png', name: 'red.png', data: img1Base64, contentType: 'image/png' },
      { relativePath: 'photos/green.png', name: 'green.png', data: img2Base64, contentType: 'image/png' },
    ];

    await app.page.evaluate(async (entries) => {
      const folderFiles = entries.map(e => {
        const binary = atob(e.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], e.name, { type: e.contentType });
        return { relativePath: e.relativePath, file };
      });
      const dirPaths = new Set(
        entries.map(e => e.relativePath.split('/').slice(0, -1).join('/'))
          .filter(p => p.length > 0)
      );
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, fileEntries);

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Verify that the "photos" tag was created
    const tags = await app.getAllTags();
    const photosTag = tags.find(t => t.name === 'photos');
    expect(photosTag).toBeTruthy();

    // Navigate into the photos folder and verify 2 clips
    await app.clickFolder('photos');
    await app.expectClipCount(2);
  });

  test('folder drop in folder mode with active filter prefixes tags', async ({ app }) => {
    // Create a parent tag "work" and navigate into it in folder mode
    await app.createTag('work');
    await app.toggleFolderMode();
    await app.clickFolder('work');

    // Drop a folder "docs" with one file
    const imgBase64 = generateTestImage(50, 50, [0, 0, 255]).toString('base64');

    const fileEntries = [
      { relativePath: 'docs/report.png', name: 'report.png', data: imgBase64, contentType: 'image/png' },
    ];

    await app.page.evaluate(async (entries) => {
      const folderFiles = entries.map(e => {
        const binary = atob(e.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], e.name, { type: e.contentType });
        return { relativePath: e.relativePath, file };
      });
      const dirPaths = new Set(
        entries.map(e => e.relativePath.split('/').slice(0, -1).join('/'))
          .filter(p => p.length > 0)
      );
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, fileEntries);

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Verify that "work/docs" tag exists (prefixed with current folder)
    const tags = await app.getAllTags();
    const workDocsTag = tags.find(t => t.name === 'work/docs');
    expect(workDocsTag).toBeTruthy();
  });

  test('folder drop creates intermediate tags for nested structure', async ({ app }) => {
    await app.toggleFolderMode();

    // Drop a deeply nested file: a/b/c/deep.png
    const imgBase64 = generateTestImage(50, 50, [128, 128, 0]).toString('base64');

    const fileEntries = [
      { relativePath: 'a/b/c/deep.png', name: 'deep.png', data: imgBase64, contentType: 'image/png' },
    ];

    await app.page.evaluate(async (entries) => {
      const folderFiles = entries.map(e => {
        const binary = atob(e.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], e.name, { type: e.contentType });
        return { relativePath: e.relativePath, file };
      });
      const dirPaths = new Set(
        entries.map(e => e.relativePath.split('/').slice(0, -1).join('/'))
          .filter(p => p.length > 0)
      );
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, fileEntries);

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Verify intermediate tags a, a/b, a/b/c all exist
    const tags = await app.getAllTags();
    const tagNames = tags.map(t => t.name);
    expect(tagNames).toContain('a');
    expect(tagNames).toContain('a/b');
    expect(tagNames).toContain('a/b/c');
  });

  test('folder drop creates tags for empty directories', async ({ app }) => {
    await app.toggleFolderMode();

    // Drop no files, but include an empty directory in dirPaths
    await app.page.evaluate(async () => {
      const dirPaths = new Set(['emptydir']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop([], [], dirPaths);
    });

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Verify the empty directory created a tag
    const tags = await app.getAllTags();
    const emptyTag = tags.find(t => t.name === 'emptydir');
    expect(emptyTag).toBeTruthy();

    // Verify the folder card is visible
    await app.expectFolderVisible('emptydir');
  });

  test('gitignore parser filters patterns correctly', async ({ app }) => {
    const results = await app.page.evaluate(() => {
      // @ts-ignore
      const ignoreFn = window.__testHelpers.parseGitignore(
        'node_modules\n*.log\nbuild/\n# comment\n\n'
      );
      return {
        nodeModulesFile: ignoreFn('node_modules', false),
        nodeModulesDir: ignoreFn('node_modules', true),
        errorLog: ignoreFn('error.log', false),
        appJs: ignoreFn('app.js', false),
        buildDir: ignoreFn('build', true),
        buildFile: ignoreFn('build', false),
        srcDir: ignoreFn('src', true),
      };
    });

    // node_modules matches as exact name for both file and dir
    expect(results.nodeModulesFile).toBe(true);
    expect(results.nodeModulesDir).toBe(true);

    // *.log glob matches .log files
    expect(results.errorLog).toBe(true);

    // app.js should not be ignored
    expect(results.appJs).toBe(false);

    // build/ is dir-only pattern
    expect(results.buildDir).toBe(true);
    expect(results.buildFile).toBe(false);

    // src should not be ignored
    expect(results.srcDir).toBe(false);
  });

  test('buildIgnoreFn always skips dotfiles', async ({ app }) => {
    const results = await app.page.evaluate(() => {
      // @ts-ignore — buildIgnoreFn with null gitignore content
      const ignoreFn = window.__testHelpers.buildIgnoreFn(null);
      return {
        dotGit: ignoreFn('.git', true),
        dotEnv: ignoreFn('.env', false),
        dotDS: ignoreFn('.DS_Store', false),
        normalFile: ignoreFn('readme.md', false),
        normalDir: ignoreFn('src', true),
      };
    });

    expect(results.dotGit).toBe(true);
    expect(results.dotEnv).toBe(true);
    expect(results.dotDS).toBe(true);
    expect(results.normalFile).toBe(false);
    expect(results.normalDir).toBe(false);
  });

  test('loose files get current folder tag during folder drop', async ({ app }) => {
    // Create a tag and navigate into it
    await app.createTag('inbox');
    await app.toggleFolderMode();
    await app.clickFolder('inbox');

    // Drop a folder with a file AND a loose file
    const folderImgBase64 = generateTestImage(50, 50, [200, 100, 50]).toString('base64');
    const looseImgBase64 = generateTestImage(50, 50, [50, 100, 200]).toString('base64');

    await app.page.evaluate(async ({ folderEntry, looseEntry }) => {
      // Build the folder file
      const folderBinary = atob(folderEntry.data);
      const folderBytes = new Uint8Array(folderBinary.length);
      for (let i = 0; i < folderBinary.length; i++) folderBytes[i] = folderBinary.charCodeAt(i);
      const folderFile = new File([folderBytes], folderEntry.name, { type: folderEntry.contentType });
      const folderFiles = [{ relativePath: folderEntry.relativePath, file: folderFile }];

      // Build the loose file
      const looseBinary = atob(looseEntry.data);
      const looseBytes = new Uint8Array(looseBinary.length);
      for (let i = 0; i < looseBinary.length; i++) looseBytes[i] = looseBinary.charCodeAt(i);
      const looseFile = new File([looseBytes], looseEntry.name, { type: looseEntry.contentType });

      const dirPaths = new Set(['subfolder']);

      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [looseFile], dirPaths);
    }, {
      folderEntry: { relativePath: 'subfolder/inner.png', name: 'inner.png', data: folderImgBase64, contentType: 'image/png' },
      looseEntry: { name: 'loose.png', data: looseImgBase64, contentType: 'image/png' },
    });

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Verify both clips uploaded — the loose file should be in current "inbox" folder
    // and the folder file should be in "inbox/subfolder"
    const tags = await app.getAllTags();
    const tagNames = tags.map(t => t.name);
    expect(tagNames).toContain('inbox/subfolder');

    // Check that the loose file has the "inbox" tag by looking at it from the inbox folder level
    // We're currently in the "inbox" folder — the loose file should be visible here
    await app.refreshClips();
    await app.expectClipCount(1); // only the loose file is directly in inbox
  });
});
