import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Rename cascade with multi-byte characters', () => {
  // BUG: UpdateTag cascade rename corrupts child tag names when the parent
  // tag contains multi-byte UTF-8 characters (e.g., CJK, emoji).
  //
  // Root cause: In app.go, UpdateTag builds the SUBSTR offset using Go's
  // len(oldPrefix) which returns byte count, but SQLite's SUBSTR() counts
  // characters (codepoints). For ASCII-only names the two coincide, but for
  // multi-byte characters the byte count is larger than the character count,
  // so SUBSTR skips too many characters and the child tag name gets truncated.
  //
  // Example:
  //   Parent tag: "工作" (2 CJK chars, 6 UTF-8 bytes)
  //   Child tag:  "工作/client1"
  //   Rename "工作" -> "job"
  //
  //   oldPrefix = "工作/" -> len() = 7 bytes
  //   SUBSTR("工作/client1", 8) = "nt1" (character position 8, skipping too far)
  //   Result: "job/nt1" instead of correct "job/client1"
  //
  // The fix should use utf8.RuneCountInString(oldPrefix) instead of len(oldPrefix).

  test('renaming a CJK parent tag should correctly cascade to children', async ({ app }) => {
    // Create parent with multi-byte characters (Chinese for "work")
    await app.createTag('工作/client1');

    // Verify both tags were created
    let tags = await app.getAllTags();
    let names = tags.map(t => t.name);
    expect(names).toContain('工作');
    expect(names).toContain('工作/client1');

    // Rename the parent tag from "工作" to "job"
    const parentTag = tags.find(t => t.name === '工作');
    expect(parentTag).toBeTruthy();

    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.go.main.App.UpdateTag(id, 'job', '');
    }, parentTag!.id);

    // Verify the cascade rename produced correct child names
    tags = await app.getAllTags();
    names = tags.map(t => t.name);

    expect(names).toContain('job');
    expect(names).toContain('job/client1');
    // Should NOT contain corrupted names
    expect(names).not.toContain('工作');
    expect(names).not.toContain('工作/client1');
  });

  test('renaming a CJK parent tag should cascade correctly to deeply nested children', async ({ app }) => {
    // Create a deeper hierarchy with multi-byte parent
    await app.createTag('工作/设计/logo');

    let tags = await app.getAllTags();
    let names = tags.map(t => t.name);
    expect(names).toContain('工作');
    expect(names).toContain('工作/设计');
    expect(names).toContain('工作/设计/logo');

    // Rename root from "工作" to "projects"
    const rootTag = tags.find(t => t.name === '工作');
    expect(rootTag).toBeTruthy();

    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.go.main.App.UpdateTag(id, 'projects', '');
    }, rootTag!.id);

    tags = await app.getAllTags();
    names = tags.map(t => t.name);

    expect(names).toContain('projects');
    expect(names).toContain('projects/设计');
    expect(names).toContain('projects/设计/logo');
    expect(names).not.toContain('工作');
  });

  test('renaming an emoji parent tag should cascade correctly to children', async ({ app }) => {
    // Emoji characters are 4 bytes in UTF-8, maximizing the byte/char discrepancy
    await app.createTag('\u{1F4BC}/tasks');  // Briefcase emoji

    let tags = await app.getAllTags();
    let names = tags.map(t => t.name);
    expect(names).toContain('\u{1F4BC}');
    expect(names).toContain('\u{1F4BC}/tasks');

    const parentTag = tags.find(t => t.name === '\u{1F4BC}');
    expect(parentTag).toBeTruthy();

    await app.page.evaluate(async (id) => {
      // @ts-ignore
      await window.go.main.App.UpdateTag(id, 'work', '');
    }, parentTag!.id);

    tags = await app.getAllTags();
    names = tags.map(t => t.name);

    expect(names).toContain('work');
    expect(names).toContain('work/tasks');
    expect(names).not.toContain('\u{1F4BC}');
  });
});
