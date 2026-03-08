import { test, expect } from '../../fixtures/test-fixtures';

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
});
