import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import path from 'path';

test.describe('Subtag CRUD', () => {
  test.afterEach(async ({ app }) => {
    await app.deleteAllTags();
  });

  test('creating a subtag auto-creates intermediate tags', async ({ app }) => {
    await app.createTag('work/client1/projectABC');
    const tags = await app.getAllTags();
    const names = tags.map(t => t.name);
    expect(names).toContain('work');
    expect(names).toContain('work/client1');
    expect(names).toContain('work/client1/projectABC');
  });

  test('intermediate tags inherit parent color', async ({ app }) => {
    await app.createTag('work');
    const workTag = (await app.getAllTags()).find(t => t.name === 'work');
    await app.createTag('work/client1');
    const tags = await app.getAllTags();
    const client1Tag = tags.find(t => t.name === 'work/client1');
    expect(client1Tag!.color).toBe(workTag!.color);
  });

  test('renaming parent cascades to children', async ({ app }) => {
    await app.createTag('work/client1/projectABC');
    const workTag = (await app.getAllTags()).find(t => t.name === 'work');
    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.go.main.App.UpdateTag(id, 'job', '');
    }, workTag!.id);
    const tags = await app.getAllTags();
    const names = tags.map(t => t.name);
    expect(names).toContain('job');
    expect(names).toContain('job/client1');
    expect(names).toContain('job/client1/projectABC');
    expect(names).not.toContain('work');
  });

  test('deleting parent does not delete children', async ({ app }) => {
    await app.createTag('work/client1');
    const workTag = (await app.getAllTags()).find(t => t.name === 'work');
    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.go.main.App.DeleteTag(id);
    }, workTag!.id);
    const tags = await app.getAllTags();
    const names = tags.map(t => t.name);
    expect(names).toContain('work/client1');
    expect(names).not.toContain('work');
  });

  test('parent with children is not auto-deleted when it has 0 clips', async ({ app }) => {
    await app.createTag('work/client1');
    const tags = await app.getAllTags();
    expect(tags.map(t => t.name)).toContain('work');
  });

  test('subtag is not auto-deleted when removed from last clip', async ({ app }) => {
    await app.createTag('work/client1');
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.addTagToClip(filename, 'work/client1');

    // Remove the subtag from the clip
    await app.removeTagFromClip(filename, 'work/client1');

    // Subtag should still exist (not auto-deleted)
    const tags = await app.getAllTags();
    const names = tags.map(t => t.name);
    expect(names).toContain('work/client1');
    expect(names).toContain('work');
  });
});
