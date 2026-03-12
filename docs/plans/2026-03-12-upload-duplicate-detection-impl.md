# Upload Duplicate Detection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Before uploading files, detect filename conflicts in the target tag and let users choose to overwrite, keep both, or skip.

**Architecture:** Frontend-driven pre-upload check. A new Go method `FindClipsByFilenameAndTag` returns existing clips matching incoming filenames. Frontend computes SHA-256 hashes of incoming files, compares against existing clip hashes, silently skips identical files, and shows a three-button conflict dialog for different-content matches. Overwrite uses the existing `UpdateClipData` method.

**Tech Stack:** Go (backend query), Vanilla JS (conflict logic + dialog), Tailwind CSS (dialog styling), Playwright (e2e tests)

---

### Task 1: Backend — `FindClipsByFilenameAndTag` method

**Files:**
- Modify: `app.go` (add struct + method after `UpdateClipData` at line ~854)

**Step 1: Add the ClipMatch struct and method**

Add after the `UpdateClipData` method (line 854) in `app.go`:

```go
// ClipMatch represents a clip matching a filename search.
type ClipMatch struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentHash string `json:"content_hash"`
}

// FindClipsByFilenameAndTag returns clips matching any of the given filenames
// within a specific tag. When tagID is 0, matches untagged clips only.
func (a *App) FindClipsByFilenameAndTag(filenames []string, tagID int64) ([]ClipMatch, error) {
	if len(filenames) == 0 {
		return nil, nil
	}

	// Build placeholder string for IN clause
	placeholders := make([]string, len(filenames))
	args := make([]interface{}, len(filenames))
	for i, fn := range filenames {
		placeholders[i] = "?"
		args[i] = fn
	}
	inClause := strings.Join(placeholders, ", ")

	var query string
	if tagID > 0 {
		query = fmt.Sprintf(`
			SELECT c.id, c.filename, c.content_hash
			FROM clips c
			JOIN clip_tags ct ON c.id = ct.clip_id
			WHERE ct.tag_id = ? AND c.filename IN (%s)
			AND c.archived = 0`, inClause)
		args = append([]interface{}{tagID}, args...)
	} else {
		query = fmt.Sprintf(`
			SELECT c.id, c.filename, c.content_hash
			FROM clips c
			WHERE c.filename IN (%s)
			AND c.archived = 0
			AND c.id NOT IN (SELECT clip_id FROM clip_tags)`, inClause)
	}

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to find clips by filename: %w", err)
	}
	defer rows.Close()

	var matches []ClipMatch
	for rows.Next() {
		var m ClipMatch
		if err := rows.Scan(&m.ID, &m.Filename, &m.ContentHash); err != nil {
			return nil, fmt.Errorf("failed to scan clip match: %w", err)
		}
		matches = append(matches, m)
	}
	return matches, nil
}
```

**Step 2: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`

**Step 3: Commit**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat: add FindClipsByFilenameAndTag backend method for upload duplicate detection"
```

---

### Task 2: Frontend — Conflict dialog HTML

**Files:**
- Modify: `frontend/index.html` (add dialog after the prompt dialog, around line 532)

**Step 1: Add the conflict dialog HTML**

Insert after the closing `</div>` of the prompt dialog (line 532) and before the `<!-- Restore Backup Confirm Dialog -->` comment (line 534):

```html

    <!-- Upload Conflict Dialog -->
    <div id="conflict-dialog" role="alertdialog" aria-modal="true" aria-labelledby="conflict-title"
        aria-describedby="conflict-message" tabindex="-1" inert
        class="fixed inset-0 z-[120] flex items-center justify-center p-4 bg-stone-900/40 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none">
        <div class="bg-white rounded-lg shadow-xl max-w-sm w-full overflow-hidden transform transition-transform duration-200 scale-95">
            <div class="p-5">
                <div class="flex items-center justify-center w-10 h-10 mx-auto bg-amber-50 rounded-full mb-3">
                    <svg class="w-5 h-5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"
                        xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z">
                        </path>
                    </svg>
                </div>
                <h2 id="conflict-title" class="text-sm font-semibold text-stone-800 text-center mb-1">Duplicate Files Found</h2>
                <p id="conflict-message" class="text-xs text-stone-500 text-center mb-3"></p>
                <ul id="conflict-file-list" class="text-xs text-stone-600 font-mono bg-stone-50 rounded-md border border-stone-200 p-2 max-h-32 overflow-y-auto space-y-0.5">
                </ul>
            </div>
            <div class="bg-stone-50 px-5 py-3 flex gap-2 justify-end border-t border-stone-100">
                <button id="conflict-skip-btn"
                    class="bg-white border border-stone-200 hover:bg-stone-50 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Skip
                </button>
                <button id="conflict-keep-btn"
                    class="bg-white border border-stone-200 hover:bg-stone-50 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Keep Both
                </button>
                <button id="conflict-overwrite-btn"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Overwrite
                </button>
            </div>
        </div>
    </div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add conflict dialog HTML for upload duplicate detection"
```

---

### Task 3: Frontend — Conflict dialog JS logic + `checkAndResolveConflicts`

**Files:**
- Modify: `frontend/js/utils.js` (add conflict dialog show/close + `checkAndResolveConflicts` after `closePromptDialog`, around line ~110)

**Step 1: Add conflict dialog functions and `checkAndResolveConflicts`**

Add after `closePromptDialog` function in `utils.js`:

```javascript
let conflictResolveCallback = null;
let conflictFocusTrapCleanup = null;

function showConflictDialog(filenames, onResolve) {
    const dialog = document.getElementById('conflict-dialog');
    const dialogContent = dialog.querySelector('div');
    const messageEl = document.getElementById('conflict-message');
    const fileList = document.getElementById('conflict-file-list');

    messageEl.textContent = `${filenames.length} file${filenames.length === 1 ? '' : 's'} already exist${filenames.length === 1 ? 's' : ''} with different content:`;
    fileList.innerHTML = filenames.map(f => `<li class="truncate">${f}</li>`).join('');
    conflictResolveCallback = onResolve;

    dialog.removeAttribute('inert');
    dialog.classList.remove('opacity-0', 'pointer-events-none');
    dialog.classList.add('opacity-100');
    dialogContent.classList.remove('scale-95');
    dialogContent.classList.add('scale-100');

    lastFocusedElement = document.activeElement;
    if (conflictFocusTrapCleanup) conflictFocusTrapCleanup();
    conflictFocusTrapCleanup = trapFocus(dialog);
    setTimeout(() => document.getElementById('conflict-skip-btn').focus(), 100);
}

function closeConflictDialog(resolution) {
    const dialog = document.getElementById('conflict-dialog');
    const dialogContent = dialog.querySelector('div');

    if (conflictFocusTrapCleanup) {
        conflictFocusTrapCleanup();
        conflictFocusTrapCleanup = null;
    }
    dialog.classList.remove('opacity-100');
    dialog.classList.add('opacity-0', 'pointer-events-none');
    dialogContent.classList.remove('scale-100');
    dialogContent.classList.add('scale-95');
    dialog.setAttribute('inert', '');

    if (lastFocusedElement) lastFocusedElement.focus();

    const cb = conflictResolveCallback;
    conflictResolveCallback = null;
    if (cb) cb(resolution);
}
```

**Step 2: Add button event listeners**

Add in the `window.addEventListener('load', ...)` in `app.js` or at the end of `utils.js` (whichever initializes button listeners — check existing pattern). The confirm dialog buttons are wired in `utils.js` at the bottom, so add there:

```javascript
document.getElementById('conflict-overwrite-btn').addEventListener('click', () => closeConflictDialog('overwrite'));
document.getElementById('conflict-keep-btn').addEventListener('click', () => closeConflictDialog('keep'));
document.getElementById('conflict-skip-btn').addEventListener('click', () => closeConflictDialog('skip'));
```

**Step 3: Add `computeFileHash` and `checkAndResolveConflicts` in `wails-api.js`**

Add after the `fileToFileData` function (line ~282) in `wails-api.js`:

```javascript
/**
 * Compute SHA-256 hex hash of base64-encoded data.
 * Uses Web Crypto API to match the Go backend's computeContentHash.
 */
async function computeFileHash(base64Data) {
    const binary = atob(base64Data);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    const hashBuffer = await crypto.subtle.digest('SHA-256', bytes);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    return hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
}

/**
 * Check incoming files against existing clips in the target tag.
 * Silently skips identical-content files; prompts user for different-content conflicts.
 *
 * @param {Array<{name: string, content_type: string, data: string}>} fileDataArray
 * @param {number} tagID - Target tag ID (0 for untagged)
 * @returns {Promise<{toUpload: Array, toOverwrite: Array<{clipID: number, fileData: object}>, skippedCount: number} | null>}
 *          Returns null if the user cancelled / closed the dialog.
 */
async function checkAndResolveConflicts(fileDataArray, tagID) {
    const filenames = fileDataArray.map(f => f.name);
    let matches;
    try {
        matches = await window.go.main.App.FindClipsByFilenameAndTag(filenames, tagID);
    } catch (err) {
        console.error('Error checking for duplicates:', err);
        // On error, fall through to upload everything
        return { toUpload: fileDataArray, toOverwrite: [], skippedCount: 0 };
    }

    if (!matches || matches.length === 0) {
        return { toUpload: fileDataArray, toOverwrite: [], skippedCount: 0 };
    }

    // Build a map: filename → { clipID, existingHash }
    const matchMap = {};
    for (const m of matches) {
        matchMap[m.filename] = { clipID: m.id, existingHash: m.content_hash };
    }

    // Separate files into: no conflict, identical (skip), different content (conflict)
    const noConflict = [];
    const identical = [];
    const conflicts = []; // { fileData, clipID }

    for (const fd of fileDataArray) {
        const match = matchMap[fd.name];
        if (!match) {
            noConflict.push(fd);
            continue;
        }
        const incomingHash = await computeFileHash(fd.data);
        if (incomingHash === match.existingHash) {
            identical.push(fd);
        } else {
            conflicts.push({ fileData: fd, clipID: match.clipID });
        }
    }

    // If all matches were identical content, skip silently
    if (conflicts.length === 0) {
        if (identical.length > 0) {
            showToast(`${identical.length} identical file${identical.length === 1 ? '' : 's'} skipped.`);
        }
        return { toUpload: noConflict, toOverwrite: [], skippedCount: identical.length };
    }

    // Show conflict dialog and wait for user choice
    const resolution = await new Promise(resolve => {
        showConflictDialog(conflicts.map(c => c.fileData.name), resolve);
    });

    if (resolution === 'overwrite') {
        return {
            toUpload: noConflict,
            toOverwrite: conflicts.map(c => ({ clipID: c.clipID, fileData: c.fileData })),
            skippedCount: identical.length,
        };
    } else if (resolution === 'keep') {
        return {
            toUpload: [...noConflict, ...conflicts.map(c => c.fileData)],
            toOverwrite: [],
            skippedCount: identical.length,
        };
    } else {
        // 'skip'
        return {
            toUpload: noConflict,
            toOverwrite: [],
            skippedCount: identical.length + conflicts.length,
        };
    }
}
```

**Step 4: Commit**

```bash
git add frontend/js/utils.js frontend/js/wails-api.js
git commit -m "feat: add conflict dialog logic and checkAndResolveConflicts function"
```

---

### Task 4: Frontend — Integrate conflict check into `upload()` and `handleText()`

**Files:**
- Modify: `frontend/js/wails-api.js:106-122` (the `upload` function)
- Modify: `frontend/js/app.js:935-949` (the `handleText` function)

**Step 1: Update `upload()` in `wails-api.js`**

Replace the current `upload` function (lines 106-122) with:

```javascript
async function upload(files) {
    try {
        const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;
        let autoTagID = 0;
        if (isFolderMode() && activeTagFilters.length > 0) {
            autoTagID = activeTagFilters[activeTagFilters.length - 1];
        }

        const result = await checkAndResolveConflicts(files, autoTagID);
        if (!result) return; // user cancelled

        // Overwrite existing clips
        for (const { clipID, fileData } of result.toOverwrite) {
            await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
        }

        // Upload new files
        if (result.toUpload.length > 0) {
            await window.go.main.App.UploadFiles(result.toUpload, minutes, autoTagID);
        }

        const totalProcessed = result.toUpload.length + result.toOverwrite.length;
        if (totalProcessed > 0 || result.skippedCount > 0) {
            const parts = [];
            if (result.toUpload.length > 0) parts.push(`${result.toUpload.length} uploaded`);
            if (result.toOverwrite.length > 0) parts.push(`${result.toOverwrite.length} overwritten`);
            if (result.skippedCount > 0) parts.push(`${result.skippedCount} skipped`);
            showToast(parts.join(', ') + '.');
        }

        if (!isViewingArchive) {
            loadClips();
        }
    } catch (error) {
        console.error('Error uploading:', error);
        showToast('Upload failed.');
    }
}
```

**Step 2: Update `handleText()` in `app.js`**

Replace the current `handleText` function (lines 935-950) with:

```javascript
async function handleText(text) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }

    // Convert text to base64
    const base64 = btoa(unescape(encodeURIComponent(text)));
    const fileData = {
        name: 'pasted_text.txt',
        content_type: 'text/plain',
        data: base64
    };

    upload([fileData]);
}
```

Note: `handleText` already calls `upload()`, so no change is actually needed here — the conflict check is picked up automatically from the `upload()` function. Keep this as-is.

**Step 3: Commit**

```bash
git add frontend/js/wails-api.js
git commit -m "feat: integrate conflict check into upload function"
```

---

### Task 5: Frontend — Integrate conflict check into `handleFolderDrop()`

**Files:**
- Modify: `frontend/js/app.js:875-918` (the upload loop in `handleFolderDrop`)

**Step 1: Update the folder group upload loop**

Replace the upload section of `handleFolderDrop` (lines 875-918) with:

```javascript
    // Upload each group with its tag, checking for conflicts
    let uploadedCount = 0;
    let skippedCount = 0;
    let overwrittenCount = 0;
    let failedCount = 0;
    for (const [tagName, files] of Object.entries(groups)) {
        if (failedTags.has(tagName)) {
            skippedCount += files.length;
            console.warn(`Skipping ${files.length} file(s) for failed tag "${tagName}"`);
            continue;
        }
        const tagId = tagNameToId[tagName] || 0;
        const fileDataArray = [];
        for (const file of files) {
            const fileData = await fileToFileData(file);
            fileDataArray.push(fileData);
        }

        const result = await checkAndResolveConflicts(fileDataArray, tagId);
        if (!result) {
            skippedCount += files.length;
            continue;
        }

        // Overwrite existing clips
        for (const { clipID, fileData } of result.toOverwrite) {
            try {
                await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
                overwrittenCount++;
            } catch (error) {
                console.error('Error overwriting clip:', error);
                failedCount++;
            }
        }

        skippedCount += result.skippedCount;

        // Upload new files
        if (result.toUpload.length > 0) {
            try {
                await window.go.main.App.UploadFiles(result.toUpload, minutes, tagId);
                uploadedCount += result.toUpload.length;
            } catch (error) {
                console.error('Error uploading folder group:', error);
                failedCount += result.toUpload.length;
            }
        }
    }

    // Handle loose files with current folder's autoTagID
    if (looseFiles.length > 0) {
        let autoTagID = 0;
        if (activeTagFilters.length > 0) {
            autoTagID = activeTagFilters[activeTagFilters.length - 1];
        }
        const fileDataArray = [];
        for (const file of looseFiles) {
            const fileData = await fileToFileData(file);
            fileDataArray.push(fileData);
        }

        const result = await checkAndResolveConflicts(fileDataArray, autoTagID);
        if (result) {
            for (const { clipID, fileData } of result.toOverwrite) {
                try {
                    await window.go.main.App.UpdateClipData(clipID, fileData.content_type, fileData.data, fileData.name);
                    overwrittenCount++;
                } catch (error) {
                    console.error('Error overwriting clip:', error);
                    failedCount++;
                }
            }
            skippedCount += result.skippedCount;
            if (result.toUpload.length > 0) {
                try {
                    await window.go.main.App.UploadFiles(result.toUpload, minutes, autoTagID);
                    uploadedCount += result.toUpload.length;
                } catch (error) {
                    console.error('Error uploading loose files:', error);
                    failedCount += result.toUpload.length;
                }
            }
        } else {
            skippedCount += looseFiles.length;
        }
    }

    // Report results accurately
    if (failedCount > 0 || skippedCount > 0 || overwrittenCount > 0) {
        const parts = [];
        if (uploadedCount > 0) parts.push(`${uploadedCount} uploaded`);
        if (overwrittenCount > 0) parts.push(`${overwrittenCount} overwritten`);
        if (skippedCount > 0) parts.push(`${skippedCount} skipped`);
        if (failedCount > 0) parts.push(`${failedCount} failed`);
        showToast(`Folder import: ${parts.join(', ')}.`);
    } else if (uploadedCount > 0) {
        showToast('Folder uploaded!');
    } else {
        showToast('No files to upload.');
    }
    loadClips();
```

**Step 2: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: integrate conflict check into handleFolderDrop"
```

---

### Task 6: Add selectors to test helpers

**Files:**
- Modify: `e2e/helpers/selectors.ts` (add conflict dialog selectors after the `confirm` block, around line 288)

**Step 1: Add conflict dialog selectors**

Add after the `confirm` block (line 288):

```typescript
  // Conflict dialog (upload duplicate detection)
  conflict: {
    dialog: '#conflict-dialog',
    title: '#conflict-title',
    message: '#conflict-message',
    fileList: '#conflict-file-list',
    overwriteButton: '#conflict-overwrite-btn',
    keepButton: '#conflict-keep-btn',
    skipButton: '#conflict-skip-btn',
  },
```

**Step 2: Commit**

```bash
git add e2e/helpers/selectors.ts
git commit -m "test: add conflict dialog selectors"
```

---

### Task 7: E2E tests for upload duplicate detection

**Files:**
- Create: `e2e/tests/clips/upload-duplicates.spec.ts`

**Step 1: Write the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';

test.describe('Upload Duplicate Detection', () => {
  test('identical content files are silently skipped', async ({ app }) => {
    // Upload a file
    const img = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Upload the same file again — should be silently skipped
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Verify toast says skipped
    await expect(app.page.locator(selectors.toast.container)).toContainText('skipped');
  });

  test('different content same name shows conflict dialog', async ({ app }) => {
    // Upload a file
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath1 = await createTempFile(img1, 'png');
    await app.uploadFile(filePath1);
    await app.expectClipCount(1);

    // Upload a different file with the same filename
    const img2 = generateTestImage(50, 50, [0, 255, 0]);
    const filePath2 = await createTempFile(img2, 'png', filePath1);
    await app.uploadFile(filePath2);

    // Conflict dialog should appear
    await expect(app.page.locator(selectors.conflict.dialog)).not.toHaveAttribute('inert');
  });

  test('conflict dialog — overwrite replaces existing clip content', async ({ app }) => {
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    // Upload different content with same name
    const img2 = generateTestImage(50, 50, [0, 255, 0]);
    const filePath2 = await createTempFile(img2, 'png', filePath);
    await app.uploadFile(filePath2);

    // Click overwrite
    await app.page.locator(selectors.conflict.overwriteButton).click();

    // Should still be 1 clip (overwritten, not duplicated)
    await app.expectClipCount(1);
  });

  test('conflict dialog — keep both creates a duplicate', async ({ app }) => {
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    const img2 = generateTestImage(50, 50, [0, 255, 0]);
    const filePath2 = await createTempFile(img2, 'png', filePath);
    await app.uploadFile(filePath2);

    // Click keep both
    await app.page.locator(selectors.conflict.keepButton).click();

    // Should be 2 clips now
    await app.expectClipCount(2);
  });

  test('conflict dialog — skip does not upload', async ({ app }) => {
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const filePath = await createTempFile(img1, 'png');
    await app.uploadFile(filePath);
    await app.expectClipCount(1);

    const img2 = generateTestImage(50, 50, [0, 255, 0]);
    const filePath2 = await createTempFile(img2, 'png', filePath);
    await app.uploadFile(filePath2);

    // Click skip
    await app.page.locator(selectors.conflict.skipButton).click();

    // Should still be 1 clip
    await app.expectClipCount(1);
  });

  test('folder drop with duplicate files shows conflict dialog', async ({ app }) => {
    // Enable folder mode
    await app.toggleFolderMode();

    // First, upload a file to a "photos" folder
    const img1 = generateTestImage(50, 50, [255, 0, 0]);
    const img1Base64 = img1.toString('base64');

    // Create folder with initial file via handleFolderDrop
    await app.page.evaluate(async (data) => {
      const binary = atob(data);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const file = new File([bytes], 'red.png', { type: 'image/png' });
      const folderFiles = [{ relativePath: 'photos/red.png', file }];
      const dirPaths = new Set(['photos']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, img1Base64);

    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });

    // Drop the same folder again with different content
    const img2 = generateTestImage(50, 50, [0, 0, 255]);
    const img2Base64 = img2.toString('base64');

    await app.page.evaluate(async (data) => {
      const binary = atob(data);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const file = new File([bytes], 'red.png', { type: 'image/png' });
      const folderFiles = [{ relativePath: 'photos/red.png', file }];
      const dirPaths = new Set(['photos']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, img2Base64);

    // Conflict dialog should appear
    await expect(app.page.locator(selectors.conflict.dialog)).not.toHaveAttribute('inert');

    // Click overwrite
    await app.page.locator(selectors.conflict.overwriteButton).click();

    // Navigate into photos folder and verify still 1 clip
    await app.page.waitForFunction(() => (window as any).__appReady === true, { timeout: 10000 });
  });
});
```

**NOTE:** The `createTempFile` helper may need a third argument to control the filename (so both temp files get the same name). Check `e2e/helpers/test-data.ts` for the signature. If `createTempFile` generates a random filename, you may need to create both files manually with `fs.writeFileSync` using the same path, or modify the helper. The key requirement is that both files share the same filename but have different content.

**Step 2: Run the tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/clips/upload-duplicates.spec.ts --reporter=list 2>&1 | tail -30`

**Step 3: Fix any failures, then commit**

```bash
git add e2e/tests/clips/upload-duplicates.spec.ts
git commit -m "test: add e2e tests for upload duplicate detection"
```

---

### Task 8: Run full test suite and fix regressions

**Step 1: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test 2>&1 | tail -50`

**Step 2: Fix any regressions**

Existing tests that upload files (especially in `folder-drop.spec.ts`, `folder-mode.spec.ts`) should not be affected since they don't upload files with the same filename twice. But if any test does re-upload with the same name, it may trigger the conflict dialog unexpectedly — fix by using distinct filenames or dismissing the dialog.

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve test regressions from upload duplicate detection"
```

---

### Task 9: Rebuild Tailwind CSS

**Step 1: Rebuild the output CSS**

Run: `cd /Users/egecan/Code/mahpastes && npx tailwindcss -i frontend/css/main.css -o frontend/dist/output.css --minify`

**Step 2: Commit**

```bash
git add frontend/dist/output.css
git commit -m "build: rebuild tailwind CSS for conflict dialog"
```
