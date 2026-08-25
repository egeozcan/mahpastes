import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import { generateTestImage } from '../../helpers/test-data';
import * as fs from 'fs/promises';
import * as path from 'path';

/**
 * Build a folder covering the shapes the scan has to handle:
 * two importable files at the root, a dotfile, a hidden directory, two
 * subfolders (for tag pre-fill), and a symlink.
 */
async function buildFixture(root: string): Promise<void> {
  await fs.writeFile(path.join(root, 'a.txt'), 'alpha content');
  await fs.writeFile(path.join(root, 'b.png'), generateTestImage(8, 8, '#336699'));
  await fs.writeFile(path.join(root, '.DS_Store'), 'junk');
  await fs.mkdir(path.join(root, '.hidden'), { recursive: true });
  await fs.writeFile(path.join(root, '.hidden', 'secret.txt'), 'secret');
  await fs.mkdir(path.join(root, '2024'), { recursive: true });
  await fs.writeFile(path.join(root, '2024', 'rome.txt'), 'rome');
  await fs.mkdir(path.join(root, '2025'), { recursive: true });
  await fs.writeFile(path.join(root, '2025', 'paris.txt'), 'paris');
  await fs.symlink(path.join(root, 'a.txt'), path.join(root, 'link.txt'));
}

async function exists(p: string): Promise<boolean> {
  try {
    await fs.access(p);
    return true;
  } catch {
    return false;
  }
}

test.describe('Import Folder Wizard', () => {

  test.describe('Scanning', () => {
    test('non-recursive scan skips dotfiles, symlinks and subfolders', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();

      const decisions = await app.getImportDecisions();
      expect(Object.keys(decisions).sort()).toEqual(['a.txt', 'b.png']);
    });

    test('recursive scan includes subfolders but still excludes dotfiles and symlinks', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      await app.openImportWizard(tempDir, { recursive: true });
      await app.startImportWalk();

      const decisions = await app.getImportDecisions();
      expect(Object.keys(decisions).sort()).toEqual([
        '2024/rome.txt', '2025/paris.txt', 'a.txt', 'b.png',
      ]);
      // The hidden directory must not be descended into at all.
      for (const key of Object.keys(decisions)) {
        expect(key).not.toContain('.hidden');
      }
    });

    test('reports what it skipped instead of silently showing a different folder', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      await app.openImportWizard(tempDir, { recursive: true });
      const summary = await app.getImportScanSummary();
      expect(summary).toContain('4 files');
      expect(summary).toContain('skipped');
    });

    test('an empty folder disables Start and says so', async ({ app, tempDir }) => {
      await app.openImportWizard(tempDir, { recursive: false });
      await expect(app.page.locator(selectors.importWizard.scanSummary))
        .toContainText('No importable files');
      await expect(app.page.locator(selectors.importWizard.startButton)).toBeDisabled();
    });
  });

  test.describe('Tag pre-fill', () => {
    test('subfolder name pre-fills the tag, root-level files get none', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      await app.openImportWizard(tempDir, { recursive: true });
      await app.startImportWalk();

      // Sorted order puts 2024/rome.txt first, and its tag box is pre-filled
      // with the subfolder name and editable straight away.
      expect(await app.getImportTagValue()).toBe('2024');
      const tags = await app.getImportTags();
      expect(tags['2024/rome.txt']).toBe('2024');
      expect(tags['2025/paris.txt']).toBe('2025');
      expect(tags['a.txt']).toBe('');
    });
  });

  test.describe('Base tag', () => {
    test('pre-fills every file, and joins with the subfolder when recursive', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, '2024'), { recursive: true });
      await fs.writeFile(path.join(tempDir, 'top.txt'), 'top');
      await fs.writeFile(path.join(tempDir, '2024', 'rome.txt'), 'rome');

      await app.openImportWizard(tempDir, { recursive: true });
      await app.setImportBaseTag('trips');

      const tags = await app.getImportTags();
      expect(tags['top.txt']).toBe('trips');
      expect(tags['2024/rome.txt']).toBe('trips/2024');
    });

    test('applies to files already seeded, but never overwrites a typed tag', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'a.txt'), 'a');
      await fs.writeFile(path.join(tempDir, 'b.txt'), 'b');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();

      // Hand-tag the first file, leave the second alone.
      await app.setImportTag('manual');
      await app.setImportAction('import');

      await app.page.locator(selectors.importWizard.summaryButton).click();
      await app.page.locator(selectors.importWizard.summaryPane).waitFor({ state: 'visible' });
      await app.page.locator(selectors.importWizard.backButton).click();

      await app.page.evaluate(() => {
        // @ts-ignore - app global
        window.__testHelpers.setImportBaseTag('photos');
      });

      const tags = await app.getImportTags();
      expect(tags['a.txt']).toBe('manual');   // typed by hand — untouched
      expect(tags['b.txt']).toBe('photos');   // still on its default — re-seeded
    });

    test('the hint shows what files will actually be tagged', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, '2024'), { recursive: true });
      await fs.writeFile(path.join(tempDir, '2024', 'rome.txt'), 'rome');

      await app.openImportWizard(tempDir, { recursive: true });
      expect(await app.getImportBaseTagHint()).toContain('Leave empty');

      await app.setImportBaseTag('trips');
      expect(await app.getImportBaseTagHint()).toContain('trips/2024');
    });

    test('the base tag survives toggling subfolders', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, 'sub'), { recursive: true });
      await fs.writeFile(path.join(tempDir, 'top.txt'), 'top');
      await fs.writeFile(path.join(tempDir, 'sub', 'deep.txt'), 'deep');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.setImportBaseTag('trips');
      await app.page.locator(selectors.importWizard.recursiveToggle).click();
      await expect(app.page.locator(selectors.importWizard.scanSummary)).toContainText('2 files');

      const tags = await app.getImportTags();
      expect(tags['top.txt']).toBe('trips');
      expect(tags['sub/deep.txt']).toBe('trips/sub');
    });

    test('Apply creates the nested tag and attaches it', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, '2024'), { recursive: true });
      await fs.writeFile(path.join(tempDir, '2024', 'rome.txt'), 'rome bytes');

      await app.openImportWizard(tempDir, { recursive: true });
      await app.setImportBaseTag('trips');
      await app.startImportWalk();
      await app.setImportAction('import');
      await app.applyImport();
      await app.closeImportWizard(false);

      const tags = await app.getAllTags();
      expect(tags.map(t => t.name)).toContain('trips/2024');
    });
  });

  test.describe('Deferred execution', () => {
    /**
     * ★ THE core requirement. Assigning actions must change nothing on disk and
     * nothing in the library — the whole design (single-call ImportApply, no
     * per-file bound method) exists to guarantee this. If this test ever fails,
     * the feature is broken no matter what else passes.
     */
    test('assigning actions changes nothing until Apply', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      const clipsBefore = await app.getClipCountFromDB();

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();

      await app.setImportAction('delete');   // a.txt -> delete, advances
      await app.setImportAction('import');   // b.png -> import, advances to summary

      await app.page.locator(selectors.importWizard.summaryPane).waitFor({ state: 'visible' });

      // Both files still on disk.
      expect(await exists(path.join(tempDir, 'a.txt'))).toBe(true);
      expect(await exists(path.join(tempDir, 'b.png'))).toBe(true);
      // And nothing was imported.
      expect(await app.getClipCountFromDB()).toBe(clipsBefore);

      const decisions = await app.getImportDecisions();
      expect(decisions['a.txt']).toBe('delete');
      expect(decisions['b.png']).toBe('import');
    });

    test('closing with pending actions asks first and changes nothing', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      const clipsBefore = await app.getClipCountFromDB();

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('delete');

      await app.page.locator(selectors.importWizard.closeButton).click();
      await expect(app.page.locator(selectors.confirm.dialog)).toHaveClass(/opacity-100/);

      // Cancel keeps the plan intact.
      await app.cancelDialog();
      expect(await app.isImportWizardOpen()).toBe(true);
      expect((await app.getImportDecisions())['a.txt']).toBe('delete');

      // Discard closes and still touches nothing.
      await app.closeImportWizard(true);
      expect(await exists(path.join(tempDir, 'a.txt'))).toBe(true);
      expect(await app.getClipCountFromDB()).toBe(clipsBefore);
    });
  });

  test.describe('Apply', () => {
    test('imports, tags and deletes exactly what the plan says', async ({ app, tempDir }) => {
      await buildFixture(tempDir);
      await app.openImportWizard(tempDir, { recursive: true });
      await app.startImportWalk();

      // 2024/rome.txt -> import + delete, keeping the pre-filled "2024" tag
      await app.setImportAction('both');
      // 2025/paris.txt -> skip
      await app.setImportAction('skip');
      // a.txt -> import
      await app.setImportAction('import');
      // b.png -> delete (advances to summary)
      await app.setImportAction('delete');

      await app.applyImport();

      // Imported + deleted
      expect(await exists(path.join(tempDir, '2024', 'rome.txt'))).toBe(false);
      // Delete-only
      expect(await exists(path.join(tempDir, 'b.png'))).toBe(false);
      // Import-only leaves the original alone
      expect(await exists(path.join(tempDir, 'a.txt'))).toBe(true);
      // Skipped
      expect(await exists(path.join(tempDir, '2025', 'paris.txt'))).toBe(true);

      await app.closeImportWizard(false);
      await app.refreshClips();
      await app.expectClipVisible('rome.txt');
      await app.expectClipVisible('a.txt');

      const tags = await app.getAllTags();
      expect(tags.map(t => t.name)).toContain('2024');
    });

    test('a delete-only decision creates no clip', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'unwanted.txt'), 'junk');
      const clipsBefore = await app.getClipCountFromDB();

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('delete');
      await app.applyImport();

      expect(await exists(path.join(tempDir, 'unwanted.txt'))).toBe(false);
      expect(await app.getClipCountFromDB()).toBe(clipsBefore);
    });

    /**
     * If the read fails, the file must survive. This is the guarantee that
     * makes "Import + Delete" safe at all: a failed import that still deleted
     * would destroy the only copy.
     */
    test('an unreadable file is never deleted', async ({ app, tempDir }) => {
      test.skip(process.platform === 'win32', 'chmod does not gate reads on Windows');

      const victim = path.join(tempDir, 'locked.txt');
      await fs.writeFile(victim, 'precious');
      await fs.chmod(victim, 0o000);

      try {
        await app.openImportWizard(tempDir, { recursive: false });
        await app.startImportWalk();

        // The wizard itself disables everything but Skip once it sees the read
        // error, so drive the decision straight into the plan to exercise the
        // backend guarantee rather than the UI guard.
        await app.page.evaluate(() => {
          // @ts-ignore - app global
          window.ImportWizard._state.decisions.set('locked.txt',
            { action: 'import_delete', tagName: '', tagEdited: false });
        });
        await app.openImportSummary();
        await app.applyImport();

        expect(await app.getImportRowStatus('locked.txt')).toBe('import failed');
        expect(await exists(victim)).toBe(true);
      } finally {
        await fs.chmod(victim, 0o644).catch(() => {});
      }
    });

    test('a file removed between scan and apply is reported, and the run continues', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'gone.txt'), 'vanishing');
      await fs.writeFile(path.join(tempDir, 'present.txt'), 'still here');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('import');  // gone.txt
      await app.setImportAction('import');  // present.txt -> summary

      await fs.rm(path.join(tempDir, 'gone.txt'));
      await app.applyImport();

      expect(await app.getImportRowStatus('gone.txt')).toBe('missing');
      expect(await app.getImportRowStatus('present.txt')).toBe('done');

      await app.closeImportWizard(false);
      await app.refreshClips();
      await app.expectClipVisible('present.txt');
    });
  });

  test.describe('Repeat last action', () => {
    test('carries the action forward', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'one.txt'), '1');
      await fs.writeFile(path.join(tempDir, 'two.txt'), '2');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('import');
      await app.repeatImportAction();

      const decisions = await app.getImportDecisions();
      expect(decisions['one.txt']).toBe('import');
      expect(decisions['two.txt']).toBe('import');
    });

    /**
     * The subtle half: an untouched tag is re-derived from the new file's own
     * subfolder. A literal copy would drop every 2025 file into the 2024 tag.
     */
    test('re-derives an untouched tag from the new subfolder', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, '2024'), { recursive: true });
      await fs.mkdir(path.join(tempDir, '2025'), { recursive: true });
      await fs.writeFile(path.join(tempDir, '2024', 'rome.txt'), 'rome');
      await fs.writeFile(path.join(tempDir, '2025', 'paris.txt'), 'paris');

      await app.openImportWizard(tempDir, { recursive: true });
      await app.startImportWalk();
      await app.setImportAction('import');       // 2024/rome.txt, tag untouched
      await app.repeatImportAction();            // 2025/paris.txt

      const tags = await app.getImportTags();
      expect(tags['2024/rome.txt']).toBe('2024');
      expect(tags['2025/paris.txt']).toBe('2025');
    });

    /**
     * The review pane paints twice per file — once immediately, once when the
     * inspection lands. A tag typed in between must survive the second paint.
     */
    test('a tag typed before the inspection lands is not clobbered', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'only.txt'), 'x');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportTag('typed-early');
      // Let any in-flight inspection resolve and repaint.
      await app.page.waitForTimeout(300);

      expect(await app.getImportTagValue()).toBe('typed-early');
      await app.setImportAction('import');
      expect((await app.getImportTags())['only.txt']).toBe('typed-early');
    });

    test('carries a manually typed tag verbatim', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, '2024'), { recursive: true });
      await fs.mkdir(path.join(tempDir, '2025'), { recursive: true });
      await fs.writeFile(path.join(tempDir, '2024', 'rome.txt'), 'rome');
      await fs.writeFile(path.join(tempDir, '2025', 'paris.txt'), 'paris');

      await app.openImportWizard(tempDir, { recursive: true });
      await app.startImportWalk();

      // The tag field is editable before the action commits, so type first.
      await app.setImportTag('trip');
      await app.setImportAction('import');   // 2024/rome.txt -> advances
      await app.repeatImportAction();        // 2025/paris.txt

      const tags = await app.getImportTags();
      expect(tags['2024/rome.txt']).toBe('trip');
      expect(tags['2025/paris.txt']).toBe('trip');
    });
  });

  test.describe('Lifecycle', () => {
    /**
     * Decisions are queued behind a promise chain, and inspections are queued
     * behind a concurrency cap. Closing mid-flight used to abandon queued work
     * without settling it, which left the chain pending forever — every action
     * in the *next* session then queued behind it and silently did nothing.
     */
    test('the wizard still accepts decisions after being closed and reopened', async ({ app, tempDir }) => {
      for (const n of ['a.txt', 'b.txt', 'c.txt']) {
        await fs.writeFile(path.join(tempDir, n), n.repeat(2000));
      }

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      // Close immediately, while inspections are plausibly still in flight.
      await app.closeImportWizard(true);

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('import');

      const decisions = await app.getImportDecisions();
      expect(decisions['a.txt']).toBe('import');
    });

    test('toggling subfolders rescans and resets decisions', async ({ app, tempDir }) => {
      await fs.mkdir(path.join(tempDir, 'sub'), { recursive: true });
      await fs.writeFile(path.join(tempDir, 'top.txt'), 'top');
      await fs.writeFile(path.join(tempDir, 'sub', 'deep.txt'), 'deep');

      await app.openImportWizard(tempDir, { recursive: false });
      expect(Object.keys(await app.getImportDecisions())).toEqual(['top.txt']);

      await app.page.locator(selectors.importWizard.recursiveToggle).click();
      await expect(app.page.locator(selectors.importWizard.scanSummary)).toContainText('2 files');
      expect(Object.keys(await app.getImportDecisions()).sort()).toEqual(['sub/deep.txt', 'top.txt']);
    });
  });

  test.describe('Duplicates', () => {
    test('surfaces an existing clip with identical content', async ({ app, tempDir }) => {
      const content = 'duplicate detection content';
      const seeded = path.join(tempDir, 'seed.txt');
      await fs.writeFile(seeded, content);
      await app.uploadFile(seeded);
      await app.expectClipCount(1);

      // A different filename, same bytes — the match is on content, not name.
      const folder = path.join(tempDir, 'folder');
      await fs.mkdir(folder, { recursive: true });
      await fs.writeFile(path.join(folder, 'copy.txt'), content);

      await app.openImportWizard(folder, { recursive: false });
      await app.startImportWalk();

      await expect(app.page.locator(selectors.importWizard.duplicates)).toBeVisible();
      expect(await app.getImportDuplicateCount()).toBe(1);
      await expect(app.page.locator(selectors.importWizard.duplicates)).toContainText('seed.txt');
    });
  });

  test.describe('Summary', () => {
    test('a row jumps back to that file and the changed action sticks', async ({ app, tempDir }) => {
      await fs.writeFile(path.join(tempDir, 'one.txt'), '1');
      await fs.writeFile(path.join(tempDir, 'two.txt'), '2');

      await app.openImportWizard(tempDir, { recursive: false });
      await app.startImportWalk();
      await app.setImportAction('import');
      await app.setImportAction('import');
      await app.page.locator(selectors.importWizard.summaryPane).waitFor({ state: 'visible' });

      await app.clickImportSummaryRow('one.txt');
      await app.setImportAction('skip');
      await app.openImportSummary();

      expect((await app.getImportDecisions())['one.txt']).toBe('skip');
      await expect(app.page.locator(selectors.importWizard.summaryCounts))
        .toContainText('1 to import');
    });
  });
});

test.describe('Import Folder shortcuts', () => {
  test('i/d/b/s assign actions and advance, r repeats', async ({ app, tempDir }) => {
    const fs2 = await import('fs/promises');
    for (const name of ['one.txt', 'two.txt', 'three.txt', 'four.txt', 'five.txt']) {
      await fs2.writeFile(path.join(tempDir, name), name);
    }
    // Sorted: five, four, one, three, two
    await app.openImportWizard(tempDir, { recursive: false });
    await app.startImportWalk();

    await app.pressKey('i');
    await app.pressKey('d');
    await app.pressKey('b');
    await app.pressKey('s');
    await app.pressKey('r');   // repeats the last committed action ('skip')

    expect(await app.getImportDecisions()).toEqual({
      'five.txt': 'import',
      'four.txt': 'delete',
      'one.txt': 'import_delete',
      'three.txt': 'skip',
      'two.txt': 'skip',
    });
  });

  /**
   * ShortcutManager drops single-letter keys while focus sits in an input.
   * That is load-bearing here: typing a tag containing "d" must not silently
   * mark the file for deletion.
   */
  test('typing in the tag field does not trigger action shortcuts', async ({ app, tempDir }) => {
    const fs2 = await import('fs/promises');
    await fs2.writeFile(path.join(tempDir, 'only.txt'), 'x');

    await app.openImportWizard(tempDir, { recursive: false });
    await app.startImportWalk();

    await app.page.locator(selectors.importWizard.tagInput).click();
    await app.page.keyboard.type('docs');

    expect((await app.getImportDecisions())['only.txt']).toBe('skip');
    expect(await app.getImportTagValue()).toBe('docs');
  });
});
