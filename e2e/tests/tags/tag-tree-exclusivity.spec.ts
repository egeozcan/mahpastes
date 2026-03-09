import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Tag Tree Exclusivity', () => {
  test('adding a subtag removes ancestor tags from same tree', async ({ app }) => {
    await app.createTag('a/b');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Tag with parent first
    await app.addTagToClip(filename, 'a');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a');

    // Tag with child — parent should be removed
    await app.addTagToClip(filename, 'a/b');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a/b');
    await app.expectClipDoesNotHaveTag(filename, 'a');
  });

  test('adding a parent tag removes descendant tags from same tree', async ({ app }) => {
    await app.createTag('a/b');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Tag with child first
    await app.addTagToClip(filename, 'a/b');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a/b');

    // Tag with parent — child should be removed
    await app.addTagToClip(filename, 'a');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a');
    await app.expectClipDoesNotHaveTag(filename, 'a/b');
  });

  test('adding a sibling tag removes other siblings from same tree', async ({ app }) => {
    await app.createTag('a/b/c');
    await app.createTag('a/b/d');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.addTagToClip(filename, 'a/b/c');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a/b/c');

    // Tag with sibling — first sibling should be removed
    await app.addTagToClip(filename, 'a/b/d');
    await app.refreshClips();
    await app.expectClipHasTag(filename, 'a/b/d');
    await app.expectClipDoesNotHaveTag(filename, 'a/b/c');
  });

  test('tags from different trees are not affected', async ({ app }) => {
    await app.createTag('a/b');
    await app.createTag('x/y');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'purple'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.addTagToClip(filename, 'a/b');
    await app.addTagToClip(filename, 'x/y');
    await app.refreshClips();

    // Both should coexist — different root trees
    await app.expectClipHasTag(filename, 'a/b');
    await app.expectClipHasTag(filename, 'x/y');
  });

  test('filtering by ancestor tag shows descendant-tagged clips', async ({ app }) => {
    await app.createTag('a/b/d');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'orange'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.addTagToClip(filename, 'a/b/d');
    await app.refreshClips();

    // Filter by ancestor — clip should appear (hierarchical expansion)
    await app.filterByTag('a');
    await app.expectClipVisible(filename);

    // Filter by intermediate ancestor
    await app.clearTagFilters();
    await app.filterByTag('a/b');
    await app.expectClipVisible(filename);
  });

  test('folder view shows clip only at its exact tag level', async ({ app }) => {
    await app.createTag('a/b/d');
    const imagePath = await createTempFile(generateTestImage(100, 100, 'cyan'), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.addTagToClip(filename, 'a/b/d');

    await app.toggleFolderMode();

    // Root: should not show clip (it's tagged)
    await app.expectClipNotVisible(filename);

    // a level: should see b folder but not the clip
    await app.clickFolder('a');
    await app.expectClipNotVisible(filename);

    // a/b level: should see d folder but not the clip
    await app.clickFolder('b');
    await app.expectClipNotVisible(filename);

    // a/b/d level: clip should appear here
    await app.clickFolder('d');
    await app.expectClipVisible(filename);
  });
});
