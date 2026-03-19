import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
} from '../../helpers/test-data';
import * as path from 'path';

test.describe('Deduplication tree exclusivity', () => {
  test('merging duplicates should not violate tag tree exclusivity', async ({ app }) => {
    // Create two tags under the same root tree
    await app.createTag('project/alpha');
    await app.createTag('project/beta');

    // Upload two identical images (same content = same content_hash = duplicates)
    const imageData = generateTestImage(50, 50, [100, 100, 100]);
    const file1 = await createTempFile(imageData, 'png');
    const file2 = await createTempFile(imageData, 'png');

    await app.uploadFile(file1);
    await app.uploadFile(file2);
    await app.expectClipCount(2);

    const filename1 = path.basename(file1);
    const filename2 = path.basename(file2);

    // Tag clip 1 with project/alpha and clip 2 with project/beta
    // These are in the same tree ("project"), so a single clip should only ever have one.
    await app.addTagToClip(filename1, 'project/alpha');
    await app.addTagToClip(filename2, 'project/beta');

    // Verify each clip has exactly one tag from the "project" tree
    const clip1TagsBefore = await app.page.evaluate(async (fn) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename?.toLowerCase() === fn.toLowerCase());
      if (!clip) throw new Error('Clip not found: ' + fn);
      // @ts-ignore
      const tags = await window.go.main.App.GetClipTags(clip.id);
      return tags.map((t: any) => t.name);
    }, filename1);
    expect(clip1TagsBefore).toEqual(['project/alpha']);

    const clip2TagsBefore = await app.page.evaluate(async (fn) => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      const clip = clips.find((c: any) => c.filename?.toLowerCase() === fn.toLowerCase());
      if (!clip) throw new Error('Clip not found: ' + fn);
      // @ts-ignore
      const tags = await window.go.main.App.GetClipTags(clip.id);
      return tags.map((t: any) => t.name);
    }, filename2);
    expect(clip2TagsBefore).toEqual(['project/beta']);

    // Now merge duplicates — this should keep one clip and merge tags,
    // but must respect tree exclusivity (only one tag from the "project" tree).
    await app.clickMergeDuplicatesInCardMenu(filename1);

    // After merge, only one clip should remain
    await app.expectClipCount(1);

    // Get the survivor clip's tags
    const survivorTags = await app.page.evaluate(async () => {
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], [], '', '');
      if (clips.length !== 1) throw new Error('Expected 1 clip after merge, got ' + clips.length);
      // @ts-ignore
      const tags = await window.go.main.App.GetClipTags(clips[0].id);
      return tags.map((t: any) => t.name);
    });

    // The survivor should have at most ONE tag from the "project" tree.
    // Tree exclusivity says a clip can only belong to one position in a tag tree.
    const projectTags = survivorTags.filter((name: string) => name.startsWith('project'));
    expect(projectTags.length).toBeLessThanOrEqual(1);
  });
});
