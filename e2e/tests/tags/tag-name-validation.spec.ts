import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Tag Name Validation - Empty Path Segments', () => {
  test('CreateTag should reject names with leading slash', async ({ app }) => {
    // BUG: CreateTag('/foo') succeeds and auto-creates an ancestor tag with
    // an empty name (''), polluting the tag list and breaking folder mode.
    //
    // Expected: CreateTag should reject '/foo' because splitting on '/'
    // produces an empty first segment.
    // Actual: CreateTag accepts '/foo', auto-creates ancestor tag '' (empty string),
    // and the empty-named tag appears as a blank folder card in folder mode.

    let errorThrown = false;
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('/foo');
      });
    } catch {
      errorThrown = true;
    }

    // The call should have thrown an error rejecting the invalid name
    expect(errorThrown).toBe(true);

    // Even if the call somehow succeeded, verify no empty-named tag was created
    const tags = await app.getAllTags();
    const emptyNameTags = tags.filter((t: any) => t.name === '');
    expect(emptyNameTags.length).toBe(0);
  });

  test('CreateTag should reject names with trailing slash', async ({ app }) => {
    // BUG: CreateTag('foo/') succeeds and creates a tag with an empty leaf
    // segment, which has no meaningful name and breaks UI display.
    //
    // Expected: CreateTag should reject 'foo/' because the last segment
    // after splitting on '/' is empty.
    // Actual: CreateTag accepts 'foo/' and creates the tag with trailing slash.

    let errorThrown = false;
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('trailing/');
      });
    } catch {
      errorThrown = true;
    }

    expect(errorThrown).toBe(true);

    // No tag with trailing slash should exist
    const tags = await app.getAllTags();
    const trailingSlashTags = tags.filter((t: any) => t.name.endsWith('/'));
    expect(trailingSlashTags.length).toBe(0);
  });

  test('CreateTag should reject names with consecutive slashes', async ({ app }) => {
    // BUG: CreateTag('a//b') succeeds and auto-creates intermediate tag 'a/'
    // (with trailing slash), which is an invalid tag name containing an empty
    // path segment.
    //
    // Expected: CreateTag should reject 'a//b' because splitting on '/'
    // produces an empty middle segment.
    // Actual: CreateTag accepts 'a//b' and creates ancestor tag 'a/' with
    // an empty leaf segment.

    let errorThrown = false;
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('double//slash');
      });
    } catch {
      errorThrown = true;
    }

    expect(errorThrown).toBe(true);

    // No tag with consecutive slashes or trailing slash should exist
    const tags = await app.getAllTags();
    const badTags = tags.filter((t: any) =>
      t.name.includes('//') || (t.name.includes('/') && t.name.endsWith('/'))
    );
    expect(badTags.length).toBe(0);
  });

  test('CreateTag should reject a bare slash as tag name', async ({ app }) => {
    // BUG: CreateTag('/') succeeds, creating a tag whose name is just '/'.
    // This is semantically meaningless and breaks hierarchy logic since
    // getRootTagName('/') returns '' (empty string).
    //
    // Expected: CreateTag should reject '/' as it contains no valid segments.
    // Actual: CreateTag accepts '/' as a valid tag name.

    let errorThrown = false;
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('/');
      });
    } catch {
      errorThrown = true;
    }

    expect(errorThrown).toBe(true);

    const tags = await app.getAllTags();
    const slashTags = tags.filter((t: any) => t.name === '/');
    expect(slashTags.length).toBe(0);
  });

  test('leading-slash tag is rejected and no empty-name tag is created', async ({ app }) => {
    // Verify that CreateTag('/orphan-child') is rejected and no garbage
    // empty-named ancestor tag is created in the database.

    let errorThrown = false;
    try {
      await app.page.evaluate(async () => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.CreateTag('/orphan-child');
      });
    } catch {
      errorThrown = true;
    }

    expect(errorThrown).toBe(true);

    // Verify no empty-named tag was created
    const tags = await app.getAllTags();
    const emptyNameTags = tags.filter((t: any) => t.name === '');
    expect(emptyNameTags.length).toBe(0);
  });
});
