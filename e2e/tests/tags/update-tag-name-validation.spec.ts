import { test, expect } from '../../fixtures/test-fixtures';

test.describe('UpdateTag Name Validation - Empty Path Segments', () => {
  // BUG: UpdateTag does not validate for empty path segments, while CreateTag does.
  // CreateTag correctly rejects names with leading/trailing/consecutive slashes and
  // whitespace-only segments, but UpdateTag only checks for the reserved '_api' segment.
  // This allows renaming a valid tag to an invalid name, breaking tag hierarchy logic,
  // folder mode display, and the cascade rename feature.
  //
  // Root cause: In app.go, UpdateTag (line ~1306) iterates path segments but only
  // checks `seg == "_api"`. It is missing the `strings.TrimSpace(seg) == ""` check
  // that CreateTag (line ~1193) has.

  test('UpdateTag should reject names with leading slash', async ({ app }) => {
    // Create a valid tag first
    await app.createTag('update-test-leading');
    const tags = await app.getAllTags();
    const tag = tags.find((t: any) => t.name === 'update-test-leading');
    expect(tag).toBeTruthy();

    // Attempt to rename it with a leading slash
    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, '/leading-slash', '#78716C');
      }, tag!.id);
    } catch {
      errorThrown = true;
    }

    // UpdateTag should reject '/leading-slash' because the first segment is empty
    expect(errorThrown).toBe(true);

    // Verify the tag was NOT renamed to the invalid name
    const updatedTags = await app.getAllTags();
    const badTags = updatedTags.filter((t: any) => t.name.startsWith('/'));
    expect(badTags.length).toBe(0);
  });

  test('UpdateTag should reject names with trailing slash', async ({ app }) => {
    await app.createTag('update-test-trailing');
    const tags = await app.getAllTags();
    const tag = tags.find((t: any) => t.name === 'update-test-trailing');
    expect(tag).toBeTruthy();

    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, 'trailing-slash/', '#78716C');
      }, tag!.id);
    } catch {
      errorThrown = true;
    }

    // UpdateTag should reject 'trailing-slash/' because the last segment is empty
    expect(errorThrown).toBe(true);

    const updatedTags = await app.getAllTags();
    const badTags = updatedTags.filter((t: any) => t.name.endsWith('/'));
    expect(badTags.length).toBe(0);
  });

  test('UpdateTag should reject names with consecutive slashes', async ({ app }) => {
    await app.createTag('update-test-double');
    const tags = await app.getAllTags();
    const tag = tags.find((t: any) => t.name === 'update-test-double');
    expect(tag).toBeTruthy();

    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, 'a//b', '#78716C');
      }, tag!.id);
    } catch {
      errorThrown = true;
    }

    // UpdateTag should reject 'a//b' because there is an empty middle segment
    expect(errorThrown).toBe(true);

    const updatedTags = await app.getAllTags();
    const badTags = updatedTags.filter((t: any) => t.name.includes('//'));
    expect(badTags.length).toBe(0);
  });

  test('UpdateTag should reject a bare slash as name', async ({ app }) => {
    await app.createTag('update-test-bare');
    const tags = await app.getAllTags();
    const tag = tags.find((t: any) => t.name === 'update-test-bare');
    expect(tag).toBeTruthy();

    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, '/', '#78716C');
      }, tag!.id);
    } catch {
      errorThrown = true;
    }

    // UpdateTag should reject '/' as it contains no valid segments
    expect(errorThrown).toBe(true);

    const updatedTags = await app.getAllTags();
    const slashTags = updatedTags.filter((t: any) => t.name === '/');
    expect(slashTags.length).toBe(0);
  });

  test('UpdateTag should reject names with whitespace-only segments', async ({ app }) => {
    await app.createTag('update-test-ws');
    const tags = await app.getAllTags();
    const tag = tags.find((t: any) => t.name === 'update-test-ws');
    expect(tag).toBeTruthy();

    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, 'a/ /b', '#78716C');
      }, tag!.id);
    } catch {
      errorThrown = true;
    }

    // UpdateTag should reject 'a/ /b' because the middle segment is whitespace-only
    expect(errorThrown).toBe(true);

    const updatedTags = await app.getAllTags();
    const badTags = updatedTags.filter((t: any) => t.name === 'a/ /b');
    expect(badTags.length).toBe(0);
  });

  test('UpdateTag with cascade should not create invalid descendant names', async ({ app }) => {
    // Create a parent with a child, then rename parent to have trailing slash.
    // The cascade rename would produce an invalid descendant name.
    await app.createTag('parent');
    await app.createTag('parent/child');
    const tags = await app.getAllTags();
    const parentTag = tags.find((t: any) => t.name === 'parent');
    expect(parentTag).toBeTruthy();

    let errorThrown = false;
    try {
      await app.page.evaluate(async (tagId: number) => {
        // @ts-ignore - Wails runtime
        await window.go.main.App.UpdateTag(tagId, 'bad/', '#78716C');
      }, parentTag!.id);
    } catch {
      errorThrown = true;
    }

    // Should reject because 'bad/' has a trailing empty segment,
    // and cascade would rename child to 'bad//child' (double slash)
    expect(errorThrown).toBe(true);

    // Verify neither parent nor child was renamed to invalid names
    const updatedTags = await app.getAllTags();
    const badTags = updatedTags.filter((t: any) =>
      t.name.includes('//') || t.name.endsWith('/')
    );
    expect(badTags.length).toBe(0);
  });
});
