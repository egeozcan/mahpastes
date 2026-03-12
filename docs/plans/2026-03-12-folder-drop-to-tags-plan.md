# Folder Drop to Tags Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When a folder is dragged into the app in folder mode, map its directory structure to tags and upload files with appropriate tags.

**Architecture:** All new logic lives in the frontend JS. The drop handler detects directories via `webkitGetAsEntry()`, traverses them recursively, applies ignore rules (dotfiles + `.gitignore`), shows a confirmation dialog if thresholds are exceeded, then either flattens (non-folder mode) or creates tags and uploads grouped by folder (folder mode). No backend changes needed.

**Tech Stack:** Vanilla JavaScript (frontend only), Playwright (e2e tests)

---

### Task 1: Directory Traversal Helper

Add a recursive directory traversal function that walks `DataTransferItem` entries and returns a flat list of `{relativePath, file}` objects.

**Files:**
- Modify: `frontend/js/app.js` (add after line 525, below `handleFiles`)

**Step 1: Write the traversal functions**

Add these functions after `handleFiles` (line 525) in `frontend/js/app.js`:

```javascript
// --- Folder Drop Helpers ---

// Read all entries from a DirectoryReader (handles batched reads)
function readAllEntries(reader) {
    return new Promise((resolve, reject) => {
        const entries = [];
        function readBatch() {
            reader.readEntries(batch => {
                if (batch.length === 0) {
                    resolve(entries);
                } else {
                    entries.push(...batch);
                    readBatch(); // Keep reading until empty batch
                }
            }, reject);
        }
        readBatch();
    });
}

// Get File from a FileSystemFileEntry
function entryToFile(fileEntry) {
    return new Promise((resolve, reject) => {
        fileEntry.file(resolve, reject);
    });
}

// Recursively traverse a directory entry, collecting {relativePath, file} objects.
// relativePath is relative to the dropped folder root (e.g. "subfolder/file.txt").
// ignoreFn is called with (name, isDirectory) and should return true to skip.
async function traverseEntry(entry, basePath, ignoreFn) {
    const results = [];
    if (entry.isFile) {
        if (ignoreFn && ignoreFn(entry.name, false)) return results;
        const file = await entryToFile(entry);
        results.push({ relativePath: basePath + entry.name, file });
    } else if (entry.isDirectory) {
        if (ignoreFn && ignoreFn(entry.name, true)) return results;
        const reader = entry.createReader();
        const children = await readAllEntries(reader);
        const dirPath = basePath + entry.name + '/';
        for (const child of children) {
            const childResults = await traverseEntry(child, dirPath, ignoreFn);
            results.push(...childResults);
        }
    }
    return results;
}

// Check if any DataTransferItem is a directory
function hasDirectoryEntries(items) {
    for (let i = 0; i < items.length; i++) {
        const entry = items[i].webkitGetAsEntry?.();
        if (entry && entry.isDirectory) return true;
    }
    return false;
}

// Traverse all dropped items, returning {relativePath, file}[] for folder contents
// and plain File[] for loose files. Also returns maxDepth of the traversal.
async function traverseDropItems(items, ignoreFn) {
    const folderFiles = [];  // {relativePath, file} from directories
    const looseFiles = [];   // File objects dropped directly (not inside folders)
    let maxDepth = 0;

    for (let i = 0; i < items.length; i++) {
        const entry = items[i].webkitGetAsEntry?.();
        if (!entry) continue;

        if (entry.isDirectory) {
            if (ignoreFn && ignoreFn(entry.name, true)) continue;
            const reader = entry.createReader();
            const children = await readAllEntries(reader);
            const dirPath = entry.name + '/';
            for (const child of children) {
                const childResults = await traverseEntry(child, dirPath, ignoreFn);
                folderFiles.push(...childResults);
            }
        } else if (entry.isFile) {
            if (ignoreFn && ignoreFn(entry.name, false)) continue;
            const file = await entryToFile(entry);
            looseFiles.push(file);
        }
    }

    // Calculate max nesting depth from relativePaths
    for (const { relativePath } of folderFiles) {
        const depth = relativePath.split('/').length - 1; // segments minus filename
        if (depth > maxDepth) maxDepth = depth;
    }

    return { folderFiles, looseFiles, maxDepth };
}
```

**Step 2: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches without JS console errors.

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: add directory traversal helpers for folder drop"
```

---

### Task 2: Gitignore Parser

Add a basic `.gitignore` parser that converts patterns into a filter function. Supports exact names (`node_modules`), simple globs (`*.pyc`), and directory patterns (`build/`). No negation or nested `.gitignore` support.

**Files:**
- Modify: `frontend/js/app.js` (add before the traversal helpers from Task 1)

**Step 1: Write the parser**

Add this function before the traversal helpers (after `handleFiles`, before `readAllEntries`):

```javascript
// Parse a .gitignore file content into a filter function.
// Returns ignoreFn(name, isDirectory) => boolean.
// Supports: exact names, simple globs (*.ext), directory patterns (name/).
// Does NOT support: negation (!), comments in middle of name, nested .gitignore.
function parseGitignore(content) {
    const patterns = [];
    for (const raw of content.split('\n')) {
        const line = raw.trim();
        if (!line || line.startsWith('#')) continue;

        if (line.endsWith('/')) {
            // Directory-only pattern
            const name = line.slice(0, -1);
            patterns.push({ type: 'dir', name });
        } else if (line.includes('*')) {
            // Simple glob — only support *.ext
            const escaped = line.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*');
            const regex = new RegExp('^' + escaped + '$');
            patterns.push({ type: 'glob', regex });
        } else {
            // Exact name match (matches both files and directories)
            patterns.push({ type: 'exact', name: line });
        }
    }

    return (name, isDirectory) => {
        for (const p of patterns) {
            switch (p.type) {
                case 'dir':
                    if (isDirectory && name === p.name) return true;
                    break;
                case 'glob':
                    if (p.regex.test(name)) return true;
                    break;
                case 'exact':
                    if (name === p.name) return true;
                    break;
            }
        }
        return false;
    };
}

// Build the ignore function: always skip dotfiles/dotfolders,
// and apply .gitignore rules if provided.
function buildIgnoreFn(gitignoreContent) {
    const gitignoreFn = gitignoreContent ? parseGitignore(gitignoreContent) : null;
    return (name, isDirectory) => {
        // Always skip dotfiles and dotfolders
        if (name.startsWith('.')) return true;
        // Apply .gitignore rules
        if (gitignoreFn && gitignoreFn(name, isDirectory)) return true;
        return false;
    };
}
```

**Step 2: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches without JS console errors.

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: add basic gitignore parser for folder drop filtering"
```

---

### Task 3: Read `.gitignore` from Dropped Folder

When traversing a dropped directory, check if it contains a `.gitignore` file and read its content before applying ignore rules.

**Files:**
- Modify: `frontend/js/app.js` (update `traverseDropItems` and add helper)

**Step 1: Add `.gitignore` reader and update traverseDropItems**

Add a helper to read `.gitignore` from a directory entry, and update `traverseDropItems` to use it:

```javascript
// Try to read .gitignore from a directory entry. Returns content string or null.
async function readGitignoreFromDir(dirEntry) {
    return new Promise(resolve => {
        dirEntry.getFile('.gitignore', {}, async fileEntry => {
            try {
                const file = await entryToFile(fileEntry);
                const text = await file.text();
                resolve(text);
            } catch {
                resolve(null);
            }
        }, () => resolve(null));
    });
}
```

Update `traverseDropItems` — after `if (entry.isDirectory)`, before traversing children, check for `.gitignore`:

Replace the directory branch in `traverseDropItems` with:

```javascript
        if (entry.isDirectory) {
            // Check for .gitignore in this top-level dropped folder
            const gitignoreContent = await readGitignoreFromDir(entry);
            const folderIgnoreFn = buildIgnoreFn(gitignoreContent);

            if (folderIgnoreFn(entry.name, true)) continue;
            const reader = entry.createReader();
            const children = await readAllEntries(reader);
            const dirPath = entry.name + '/';
            for (const child of children) {
                const childResults = await traverseEntry(child, dirPath, folderIgnoreFn);
                folderFiles.push(...childResults);
            }
        }
```

Note: The `ignoreFn` parameter on `traverseDropItems` is no longer needed since each dropped folder builds its own ignore function. Update the function signature to remove it, and update `traverseEntry` calls to use `folderIgnoreFn`. The loose file branch should use `buildIgnoreFn(null)` (dotfile-only filtering).

**Step 2: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches.

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: read .gitignore from dropped folders for ignore rules"
```

---

### Task 4: Confirmation Dialog

Add a promise-based confirmation dialog for folder drops. Warns about file count (>50) and nesting depth (>5 in folder mode).

**Files:**
- Modify: `frontend/js/app.js` (add folder drop confirmation function)

**Step 1: Write the confirmation function**

Add after the traversal helpers:

```javascript
// Show a confirmation dialog for folder drops. Returns a promise that resolves
// to true (proceed) or false (cancel).
function confirmFolderDrop(fileCount, maxDepth) {
    const warnings = [];
    if (fileCount > 50) {
        warnings.push(`This folder contains ${fileCount} files.`);
    }
    if (isFolderMode() && maxDepth > 5) {
        warnings.push(`Folder nesting is ${maxDepth} levels deep.`);
    }
    if (warnings.length === 0) return Promise.resolve(true);

    const message = warnings.join(' ') + ' Continue uploading?';

    return new Promise(resolve => {
        showConfirmDialog('Large Folder Drop', message, () => resolve(true));
        // Override the close to also resolve false
        const origClose = closeConfirmDialog;
        const patchedClose = () => {
            closeConfirmDialog = origClose;
            origClose();
            resolve(false);
        };
        closeConfirmDialog = patchedClose;
    });
}
```

**Step 2: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches.

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: add confirmation dialog for large folder drops"
```

---

### Task 5: Folder Drop Handler (Folder Mode)

Add the main `handleFolderDrop` function that creates tags from directory structure and uploads files grouped by their immediate folder tag.

**Files:**
- Modify: `frontend/js/app.js` (add folder drop handler)
- Modify: `frontend/js/wails-api.js` (add `createTagSilent` that doesn't show toast)

**Step 1: Add silent tag creation helper**

In `frontend/js/wails-api.js`, add after the `createTag` function (line 326):

```javascript
// Create tag without showing toast (for bulk operations like folder drops)
async function createTagSilent(name) {
    try {
        return await window.go.main.App.CreateTag(name);
    } catch (error) {
        console.error('Error creating tag:', error);
        return null;
    }
}
```

**Step 2: Add the folder drop handler**

In `frontend/js/app.js`, add after `confirmFolderDrop`:

```javascript
// Handle folder drop in folder mode: create tags from directory structure,
// then upload files grouped by their immediate folder tag.
async function handleFolderDrop(folderFiles, looseFiles) {
    if (isViewingArchive) {
        showToast('Switch to Active view to upload.');
        return;
    }

    // Determine base tag prefix from current folder context
    let basePrefix = '';
    if (activeTagFilters.length > 0) {
        const currentTag = allTags.find(t => t.id === activeTagFilters[activeTagFilters.length - 1]);
        if (currentTag) basePrefix = currentTag.name + '/';
    }

    // Collect all unique directory paths (including intermediates)
    const dirPaths = new Set();
    for (const { relativePath } of folderFiles) {
        const parts = relativePath.split('/');
        // Build all intermediate paths (everything except the filename)
        for (let i = 1; i < parts.length; i++) {
            dirPaths.add(parts.slice(0, i).join('/'));
        }
    }
    // Also add directory paths from folders that only contain subfolders (no direct files)
    // These are already captured by the loop above since folderFiles includes all nested files

    // Create tags parent-first (sort by depth ensures parents created before children)
    const sortedDirs = [...dirPaths].sort((a, b) => a.split('/').length - b.split('/').length);
    const tagNameToId = {};
    for (const dir of sortedDirs) {
        const tagName = basePrefix + dir;
        const tag = await createTagSilent(tagName);
        if (tag) tagNameToId[tagName] = tag.id;
    }

    // Refresh allTags after bulk creation
    const updatedTags = await getAllTags();
    allTags.length = 0;
    allTags.push(...updatedTags);
    renderTagFilterDropdown();

    // Group files by their immediate folder tag
    const groups = {}; // tagName -> File[]
    for (const { relativePath, file } of folderFiles) {
        const parts = relativePath.split('/');
        const dirPath = parts.slice(0, -1).join('/');
        const tagName = basePrefix + dirPath;
        if (!groups[tagName]) groups[tagName] = [];
        groups[tagName].push(file);
    }

    // Upload each group with its tag
    const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;
    for (const [tagName, files] of Object.entries(groups)) {
        const tagId = tagNameToId[tagName] || 0;
        const fileDataArray = [];
        for (const file of files) {
            const fileData = await fileToFileData(file);
            fileDataArray.push(fileData);
        }
        await window.go.main.App.UploadFiles(fileDataArray, minutes, tagId);
    }

    // Handle loose files (files dropped alongside folders) — use current folder tag
    if (looseFiles.length > 0) {
        const looseFileDataArray = [];
        for (const file of looseFiles) {
            const fileData = await fileToFileData(file);
            looseFileDataArray.push(fileData);
        }
        let autoTagID = 0;
        if (activeTagFilters.length > 0) {
            autoTagID = activeTagFilters[activeTagFilters.length - 1];
        }
        await window.go.main.App.UploadFiles(looseFileDataArray, minutes, autoTagID);
    }

    showToast('Folder uploaded!');
    loadClips();
}
```

**Step 3: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches.

**Step 4: Commit**

```bash
git add frontend/js/app.js frontend/js/wails-api.js
git commit -m "feat: add folder drop handler with tag creation and grouped upload"
```

---

### Task 6: Wire Drop Event to New Handlers

Update the `drop` event listener to detect directories, traverse them, show confirmation, and route to the appropriate handler.

**Files:**
- Modify: `frontend/js/app.js:305-314` (replace drop event handler)

**Step 1: Update the drop handler**

Replace the existing drop handler (lines 305-314):

```javascript
document.addEventListener('drop', async e => {
    e.preventDefault();
    if (window.__internalDragActive) return;
    dragCounter = 0;
    dropOverlay.classList.add('opacity-0');
    dropOverlay.classList.remove('opacity-100');

    const items = e.dataTransfer.items;
    if (!items || items.length === 0) return;

    // Check if any item is a directory
    if (hasDirectoryEntries(items)) {
        // Traverse directories to get full structure
        const { folderFiles, looseFiles, maxDepth } = await traverseDropItems(items);
        const totalFiles = folderFiles.length + looseFiles.length;
        if (totalFiles === 0) return;

        // Show confirmation if thresholds exceeded
        const proceed = await confirmFolderDrop(totalFiles, maxDepth);
        if (!proceed) return;

        if (isFolderMode()) {
            await handleFolderDrop(folderFiles, looseFiles);
        } else {
            // Non-folder mode: flatten everything to plain files
            const allFiles = [...looseFiles, ...folderFiles.map(f => f.file)];
            handleFiles(allFiles);
        }
    } else if (e.dataTransfer.files.length > 0) {
        handleFiles(e.dataTransfer.files);
    }
}, false);
```

**Step 2: Test manually**

1. Run `make dev`
2. In folder mode, drag a folder with files into the app
3. Verify tags are created matching the folder structure
4. Verify files appear with correct tags
5. Test in non-folder mode — files should be uploaded flat without tags

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: wire drop event to folder traversal and tag mapping"
```

---

### Task 7: Collect Unique Directory Paths for Empty Folders

Currently the directory paths are derived from `folderFiles` relative paths. But empty folders (directories with no files) won't have entries in `folderFiles`. Update `traverseDropItems` to also track directory paths directly during traversal.

**Files:**
- Modify: `frontend/js/app.js` (update `traverseDropItems` and `traverseEntry`)

**Step 1: Track directory paths during traversal**

Update `traverseDropItems` to also return a `Set` of directory paths. Update `traverseEntry` to accept and populate this set:

Add a `dirPaths` parameter to `traverseEntry`:

```javascript
async function traverseEntry(entry, basePath, ignoreFn, dirPaths) {
    const results = [];
    if (entry.isFile) {
        if (ignoreFn && ignoreFn(entry.name, false)) return results;
        const file = await entryToFile(entry);
        results.push({ relativePath: basePath + entry.name, file });
    } else if (entry.isDirectory) {
        if (ignoreFn && ignoreFn(entry.name, true)) return results;
        const dirPath = basePath + entry.name;
        if (dirPaths) dirPaths.add(dirPath);
        const reader = entry.createReader();
        const children = await readAllEntries(reader);
        for (const child of children) {
            const childResults = await traverseEntry(child, dirPath + '/', ignoreFn, dirPaths);
            results.push(...childResults);
        }
    }
    return results;
}
```

In `traverseDropItems`, create a `dirPaths` set and pass it through:

```javascript
async function traverseDropItems(items) {
    const folderFiles = [];
    const looseFiles = [];
    const dirPaths = new Set();
    let maxDepth = 0;

    for (let i = 0; i < items.length; i++) {
        const entry = items[i].webkitGetAsEntry?.();
        if (!entry) continue;

        if (entry.isDirectory) {
            const gitignoreContent = await readGitignoreFromDir(entry);
            const folderIgnoreFn = buildIgnoreFn(gitignoreContent);
            if (folderIgnoreFn(entry.name, true)) continue;
            dirPaths.add(entry.name);
            const reader = entry.createReader();
            const children = await readAllEntries(reader);
            const dirPath = entry.name + '/';
            for (const child of children) {
                const childResults = await traverseEntry(child, dirPath, folderIgnoreFn, dirPaths);
                folderFiles.push(...childResults);
            }
        } else if (entry.isFile) {
            const dotIgnore = buildIgnoreFn(null);
            if (dotIgnore(entry.name, false)) continue;
            const file = await entryToFile(entry);
            looseFiles.push(file);
        }
    }

    for (const dir of dirPaths) {
        const depth = dir.split('/').length;
        if (depth > maxDepth) maxDepth = depth;
    }

    return { folderFiles, looseFiles, dirPaths, maxDepth };
}
```

**Step 2: Update `handleFolderDrop` to use `dirPaths`**

Change `handleFolderDrop` signature to accept `dirPaths` and use it instead of deriving paths from file entries:

```javascript
async function handleFolderDrop(folderFiles, looseFiles, dirPaths) {
```

Replace the `dirPaths` derivation block with:

```javascript
    // Use the dirPaths from traversal (includes empty directories)
    // Also ensure all intermediate paths are present
    const allDirPaths = new Set(dirPaths);
    for (const dir of dirPaths) {
        const parts = dir.split('/');
        for (let i = 1; i < parts.length; i++) {
            allDirPaths.add(parts.slice(0, i).join('/'));
        }
    }
```

Update the caller in the drop handler to pass `dirPaths`:

```javascript
await handleFolderDrop(folderFiles, looseFiles, dirPaths);
```

**Step 3: Verify no syntax errors**

Run: `cd /Users/egecan/Code/mahpastes && make dev` — confirm app launches.

**Step 4: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: track empty directories during folder traversal for tag creation"
```

---

### Task 8: E2E Tests — Folder Drop in Folder Mode

Write e2e tests for the folder drop feature. Since Playwright can't simulate native directory drag-and-drop with `webkitGetAsEntry`, tests should use `page.evaluate()` to call the internal functions directly.

**Files:**
- Create: `e2e/tests/clips/folder-drop.spec.ts`
- Modify: `frontend/js/app.js` (expose test helpers)

**Step 1: Expose folder drop functions for testing**

In `frontend/js/app.js`, add to the `window.__testHelpers` object (around line 248-273):

```javascript
  handleFolderDrop: (folderFiles, looseFiles, dirPaths) => handleFolderDrop(folderFiles, looseFiles, dirPaths),
  confirmFolderDrop: (fileCount, maxDepth) => confirmFolderDrop(fileCount, maxDepth),
  parseGitignore: (content) => parseGitignore(content),
  buildIgnoreFn: (content) => buildIgnoreFn(content),
```

**Step 2: Write the e2e test file**

Create `e2e/tests/clips/folder-drop.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import {
  createTempFile,
  generateTestImage,
  generateTestText,
} from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs/promises';
import * as os from 'os';

// Helper to create a temporary folder structure and return file data
// for simulating folder drops via page.evaluate
async function createFolderStructure(
  structure: Record<string, Buffer | string>
): Promise<{ base: string; files: Array<{ relativePath: string; name: string; data: string; contentType: string }> }> {
  const base = await fs.mkdtemp(path.join(os.tmpdir(), 'mahpastes-folder-'));
  const files: Array<{ relativePath: string; name: string; data: string; contentType: string }> = [];

  for (const [relPath, content] of Object.entries(structure)) {
    const fullPath = path.join(base, relPath);
    await fs.mkdir(path.dirname(fullPath), { recursive: true });
    if (typeof content === 'string') {
      await fs.writeFile(fullPath, content);
    } else {
      await fs.writeFile(fullPath, content);
    }
    const filename = path.basename(relPath);
    const ext = path.extname(filename).toLowerCase();
    const contentType = ext === '.png' ? 'image/png'
      : ext === '.txt' ? 'text/plain'
      : ext === '.json' ? 'application/json'
      : 'application/octet-stream';
    const data = (typeof content === 'string'
      ? Buffer.from(content)
      : content
    ).toString('base64');
    files.push({ relativePath: relPath, name: filename, data, contentType });
  }

  return { base, files };
}

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
    const img = generateTestImage(10, 10, [255, 0, 0]);
    const { files } = await createFolderStructure({
      'photos/beach.png': img,
      'photos/mountain.png': img,
    });

    await app.toggleFolderMode();

    // Simulate folder drop via test helper
    await app.page.evaluate(async (fileEntries) => {
      const folderFiles = [];
      for (const entry of fileEntries) {
        // Convert base64 back to File
        const binary = atob(entry.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], entry.name, { type: entry.contentType });
        folderFiles.push({ relativePath: entry.relativePath, file });
      }
      const dirPaths = new Set(['photos']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, files);

    // Should have created the "photos" tag
    const tags = await app.getAllTags();
    expect(tags.some(t => t.name === 'photos')).toBe(true);

    // Should have uploaded 2 files
    await app.expectClipCount(2);
  });

  test('folder drop in folder mode with active filter prefixes tags', async ({ app }) => {
    await app.createTag('work');
    await app.toggleFolderMode();
    await app.clickFolder('work');

    const txt = 'hello world';
    const { files } = await createFolderStructure({
      'docs/readme.txt': txt,
    });

    await app.page.evaluate(async (fileEntries) => {
      const folderFiles = [];
      for (const entry of fileEntries) {
        const binary = atob(entry.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], entry.name, { type: entry.contentType });
        folderFiles.push({ relativePath: entry.relativePath, file });
      }
      const dirPaths = new Set(['docs']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, files);

    // Should have created "work/docs" tag
    const tags = await app.getAllTags();
    expect(tags.some(t => t.name === 'work/docs')).toBe(true);

    // Navigate to work/docs folder and verify file is there
    await app.clickFolder('docs');
    await app.expectClipCount(1);
  });

  test('folder drop creates intermediate tags for nested structure', async ({ app }) => {
    const img = generateTestImage(10, 10, [0, 255, 0]);
    const { files } = await createFolderStructure({
      'a/b/c/deep.png': img,
    });

    await app.toggleFolderMode();

    await app.page.evaluate(async (fileEntries) => {
      const folderFiles = [];
      for (const entry of fileEntries) {
        const binary = atob(entry.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], entry.name, { type: entry.contentType });
        folderFiles.push({ relativePath: entry.relativePath, file });
      }
      const dirPaths = new Set(['a', 'a/b', 'a/b/c']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, files);

    const tags = await app.getAllTags();
    const tagNames = tags.map(t => t.name);
    expect(tagNames).toContain('a');
    expect(tagNames).toContain('a/b');
    expect(tagNames).toContain('a/b/c');
  });

  test('folder drop creates tags for empty directories', async ({ app }) => {
    const img = generateTestImage(10, 10, [0, 0, 255]);
    const { files } = await createFolderStructure({
      'project/src/main.txt': 'code',
    });

    await app.toggleFolderMode();

    // Include an empty directory in dirPaths that has no files
    await app.page.evaluate(async (fileEntries) => {
      const folderFiles = [];
      for (const entry of fileEntries) {
        const binary = atob(entry.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], entry.name, { type: entry.contentType });
        folderFiles.push({ relativePath: entry.relativePath, file });
      }
      // Include "project/tests" as an empty dir
      const dirPaths = new Set(['project', 'project/src', 'project/tests']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [], dirPaths);
    }, files);

    const tags = await app.getAllTags();
    const tagNames = tags.map(t => t.name);
    expect(tagNames).toContain('project');
    expect(tagNames).toContain('project/src');
    expect(tagNames).toContain('project/tests'); // empty dir still creates tag
  });

  test('gitignore parser filters patterns correctly', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const gitignore = `
node_modules
*.pyc
build/
# comment
dist
`;
      // @ts-ignore
      const fn = window.__testHelpers.parseGitignore(gitignore);
      return {
        nodeModulesDir: fn('node_modules', true),
        nodeModulesFile: fn('node_modules', false),
        pycFile: fn('test.pyc', false),
        jsFile: fn('app.js', false),
        buildDir: fn('build', true),
        buildFile: fn('build', false),
        distFile: fn('dist', false),
        otherDir: fn('src', true),
      };
    });

    expect(result.nodeModulesDir).toBe(true);
    expect(result.nodeModulesFile).toBe(true);
    expect(result.pycFile).toBe(true);
    expect(result.jsFile).toBe(false);
    expect(result.buildDir).toBe(true);
    expect(result.buildFile).toBe(false); // build/ only matches dirs
    expect(result.distFile).toBe(true);
    expect(result.otherDir).toBe(false);
  });

  test('buildIgnoreFn always skips dotfiles', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      // @ts-ignore
      const fn = window.__testHelpers.buildIgnoreFn(null);
      return {
        dsStore: fn('.DS_Store', false),
        gitDir: fn('.git', true),
        normalFile: fn('readme.txt', false),
        normalDir: fn('src', true),
      };
    });

    expect(result.dsStore).toBe(true);
    expect(result.gitDir).toBe(true);
    expect(result.normalFile).toBe(false);
    expect(result.normalDir).toBe(false);
  });

  test('loose files get current folder tag during folder drop', async ({ app }) => {
    await app.createTag('inbox');
    await app.toggleFolderMode();
    await app.clickFolder('inbox');

    const { files } = await createFolderStructure({
      'sub/nested.txt': 'nested content',
    });

    // Also add a "loose" file
    await app.page.evaluate(async (fileEntries) => {
      const folderFiles = [];
      for (const entry of fileEntries) {
        const binary = atob(entry.data);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        const file = new File([bytes], entry.name, { type: entry.contentType });
        folderFiles.push({ relativePath: entry.relativePath, file });
      }
      // Create a loose file
      const looseFile = new File(['loose content'], 'loose.txt', { type: 'text/plain' });
      const dirPaths = new Set(['sub']);
      // @ts-ignore
      await window.__testHelpers.handleFolderDrop(folderFiles, [looseFile], dirPaths);
    }, files);

    // Loose file should be in "inbox" folder, nested file in "inbox/sub"
    const tags = await app.getAllTags();
    expect(tags.some(t => t.name === 'inbox/sub')).toBe(true);

    // Total clips: 2 (one in inbox, one in inbox/sub)
    // Navigate to root inbox to check loose file
    // The gallery should show the clip directly tagged with inbox
    await app.expectClipCount(1); // loose.txt at inbox level
    await app.expectFolderVisible('sub'); // sub folder card
  });
});
```

**Step 3: Run the tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/clips/folder-drop.spec.ts | tail -50`

Expected: All tests pass.

**Step 4: Fix any failing tests, then commit**

```bash
git add e2e/tests/clips/folder-drop.spec.ts frontend/js/app.js
git commit -m "test: add e2e tests for folder drop to tags feature"
```

---

### Task 9: Run Full Test Suite

Run the entire e2e test suite to ensure no regressions.

**Files:** None (test run only)

**Step 1: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test | tail -50`

Expected: All tests pass.

**Step 2: Fix any regressions**

If any existing tests fail, investigate and fix before proceeding. The drop handler change (Task 6) could affect existing tests that simulate drops — verify these still work.

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve test regressions from folder drop feature"
```
