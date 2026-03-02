# System Metadata & Clip Sorting Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show read-only system metadata (date, filename, type, size) in the metadata modal and add a sort popover to the gallery header with persistent preferences.

**Architecture:** Server-side sorting via parameterized `ORDER BY` in `GetClips()`. System metadata rendered as fixed rows at the top of the existing metadata modal. Sort preferences persisted in the settings table.

**Tech Stack:** Go (Wails), Vanilla JS, Tailwind CSS, SQLite, Playwright e2e tests

---

### Task 1: Add `formatFileSize` utility

**Files:**
- Modify: `frontend/js/utils.js` (after `formatTimeRemaining` ~line 140)

**Step 1: Add the utility function**

Add after `formatTimeRemaining`:

```javascript
function formatFileSize(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(i === 0 ? 0 : 1)) + ' ' + sizes[i];
}
```

**Step 2: Commit**

```bash
git add frontend/js/utils.js
git commit -m "feat: add formatFileSize utility"
```

---

### Task 2: Add system metadata rows to the metadata modal

**Files:**
- Modify: `frontend/js/metadata.js` — change `openMetadataModal` signature and `loadMetadata` to render fixed rows
- Modify: `frontend/js/ui.js:318` — pass clip data when calling `openMetadataModal`
- Modify: `frontend/index.html` — add a container for system info above the editable list

**Step 1: Add system info container to HTML**

In `frontend/index.html`, after the metadata modal header (line 850) and before `metadata-list` (line 851), add:

```html
            <div id="metadata-system-info" data-testid="metadata-system-info" class="px-5 pt-4 pb-3 bg-stone-50 border-b border-stone-100 space-y-1.5">
                <!-- System metadata rows inserted by JS -->
            </div>
```

**Step 2: Update `openMetadataModal` to accept clip data**

In `frontend/js/metadata.js`, change the function signature and body (lines 11-18):

```javascript
function openMetadataModal(clipId, clipData) {
    currentMetadataClipId = clipId;
    metadataModal.classList.remove('opacity-0', 'pointer-events-none');
    metadataModal.classList.add('opacity-100');
    metadataModal.querySelector(':scope > div').classList.remove('scale-95');
    metadataModal.querySelector(':scope > div').classList.add('scale-100');
    renderSystemInfo(clipData);
    loadMetadata(clipId);
}
```

Add the `renderSystemInfo` function and a DOM reference at the top of the file:

```javascript
const metadataSystemInfo = document.getElementById('metadata-system-info');

function renderSystemInfo(clipData) {
    metadataSystemInfo.innerHTML = '';
    if (!clipData) {
        metadataSystemInfo.classList.add('hidden');
        return;
    }
    metadataSystemInfo.classList.remove('hidden');

    const fields = [
        { label: 'Date added', value: clipData.created_at ? new Date(clipData.created_at).toLocaleString() : '—' },
        { label: 'Filename', value: clipData.filename || '—' },
        { label: 'Type', value: clipData.content_type || '—' },
        { label: 'Size', value: formatFileSize(clipData.size) },
    ];

    fields.forEach(({ label, value }) => {
        const row = document.createElement('div');
        row.className = 'flex items-center gap-2 text-[11px]';
        row.dataset.testid = 'metadata-system-row';
        row.innerHTML = `<span class="text-stone-400 font-medium w-20 flex-shrink-0">${label}</span><span class="text-stone-600 truncate">${value}</span>`;
        metadataSystemInfo.appendChild(row);
    });
}
```

**Step 3: Pass clip data from the card menu action**

In `frontend/js/ui.js`, find the `case 'metadata':` handler (~line 318). The `handleCardAction` function only receives `(action, clipId, triggerButton)`, but we need clip data. The clip data is stored on the card's DOM. Update:

```javascript
        case 'metadata': {
            const card = gallery.querySelector(`li[data-id="${id}"]`);
            const clipData = card ? {
                filename: card.querySelector('.p-2\\.5 p')?.getAttribute('title') || '',
                content_type: card.dataset.type || '',
                size: Number(card.dataset.size) || 0,
                created_at: card.dataset.createdAt || '',
            } : null;
            openMetadataModal(id, clipData);
            break;
        }
```

**Step 4: Store `created_at` on the card DOM**

In `frontend/js/ui.js`, in `createClipCard` (~line 706), after `card.dataset.size = clip.size || 0;`, add:

```javascript
    card.dataset.createdAt = clip.created_at || '';
```

**Step 5: Commit**

```bash
git add frontend/js/metadata.js frontend/js/ui.js frontend/index.html
git commit -m "feat: show system metadata in metadata modal"
```

---

### Task 3: Add sort parameters to `GetClips` backend

**Files:**
- Modify: `app.go` — update `GetClips` signature, add sort field validation, update all 3 SQL query variants

**Step 1: Add sort column mapping**

Above `GetClips` (~line 348), add a helper:

```go
func sortColumn(field string) string {
	switch field {
	case "name":
		return "c.filename"
	case "size":
		return "LENGTH(c.data)"
	case "type":
		return "c.content_type"
	default:
		return "c.created_at"
	}
}
```

**Step 2: Update `GetClips` signature**

Change from:
```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64) ([]ClipPreview, error) {
```
To:
```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
```

**Step 3: Build ORDER BY clause**

At the start of `GetClips`, after the `archivedInt` line, add:

```go
	col := sortColumn(sortField)
	dir := "DESC"
	if sortDir == "asc" {
		dir = "ASC"
	}
	orderClause := fmt.Sprintf("ORDER BY %s %s", col, dir)
	if col != "c.created_at" {
		orderClause += ", c.created_at DESC, c.id DESC"
	} else if dir == "DESC" {
		orderClause += ", c.id DESC"
	} else {
		orderClause += ", c.id ASC"
	}
```

**Step 4: Replace all 3 hardcoded ORDER BY clauses**

Replace the `ORDER BY c.created_at DESC` at lines ~409, ~428, ~438 with `%s` and inject `orderClause` via `fmt.Sprintf`. Each query string that currently ends with:

```
ORDER BY c.created_at DESC
LIMIT ?
```

Should become:

```
%s
LIMIT ?
```

And the `fmt.Sprintf` call wraps the query with `orderClause` as the format arg. Alternatively, just string-concatenate since the value is built from a validated whitelist.

**Step 5: Commit**

```bash
git add app.go
git commit -m "feat: add sort parameters to GetClips"
```

---

### Task 4: Update frontend to pass sort parameters

**Files:**
- Modify: `frontend/js/wails-api.js` — update `loadClips` to pass sort field/dir
- Modify: `frontend/js/app.js` — add sort state variables, load saved preferences on startup

**Step 1: Add sort state variables**

In `frontend/js/app.js`, near the top where other state vars are declared (look for `isViewingArchive`, `activeTagFilters`), add:

```javascript
let currentSortField = 'date';
let currentSortDir = 'desc';
```

**Step 2: Load sort preferences on startup**

In `frontend/js/app.js`, in the DOMContentLoaded or init block, add:

```javascript
    // Load sort preferences
    try {
        const savedField = await window.go.main.App.GetSetting('sort_field');
        const savedDir = await window.go.main.App.GetSetting('sort_dir');
        if (['date', 'name', 'size', 'type'].includes(savedField)) currentSortField = savedField;
        if (['asc', 'desc'].includes(savedDir)) currentSortDir = savedDir;
    } catch (e) { /* use defaults */ }
```

**Step 3: Update `loadClips` call**

In `frontend/js/wails-api.js`, change the `GetClips` call (~line 8) from:

```javascript
const clips = await window.go.main.App.GetClips(isViewingArchive, activeTagFilters, effectiveHidden);
```

To:

```javascript
const clips = await window.go.main.App.GetClips(isViewingArchive, activeTagFilters, effectiveHidden, currentSortField, currentSortDir);
```

**Step 4: Add a `setSort` function** (in `frontend/js/app.js` or `frontend/js/wails-api.js`, wherever loadClips is accessible)

```javascript
async function setSort(field, dir) {
    currentSortField = field;
    currentSortDir = dir;
    await window.go.main.App.SetSetting('sort_field', field);
    await window.go.main.App.SetSetting('sort_dir', dir);
    await loadClips();
}
```

**Step 5: Regenerate Wails bindings**

Since `GetClips` signature changed, regenerate frontend bindings:

```bash
make bindings
```

**Step 6: Commit**

```bash
git add frontend/js/wails-api.js frontend/js/app.js frontend/wailsjs/
git commit -m "feat: pass sort params to GetClips from frontend"
```

---

### Task 5: Add sort button and popover UI

**Files:**
- Modify: `frontend/index.html` — add sort button after archive button
- Create: `frontend/js/sort.js` — sort popover logic
- Modify: `frontend/index.html` — add script tag for sort.js

**Step 1: Add sort button to header**

In `frontend/index.html`, after the archive button (~line 74) and before the hamburger menu button, add:

```html
            <!-- Sort Button -->
            <div class="relative">
                <button id="sort-btn" data-testid="sort-button"
                    class="relative border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 p-2 rounded-md transition-colors"
                    aria-label="Sort clips" aria-haspopup="true" aria-expanded="false">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M3 7.5L7.5 3m0 0L12 7.5M7.5 3v13.5m13.5 0L16.5 21m0 0L12 16.5m4.5 4.5V7.5" />
                    </svg>
                </button>
            </div>
```

**Step 2: Create `frontend/js/sort.js`**

```javascript
// --- Sort Module ---

const sortBtn = document.getElementById('sort-btn');

const sortFields = [
    { id: 'date', label: 'Date added' },
    { id: 'name', label: 'Filename' },
    { id: 'size', label: 'File size' },
    { id: 'type', label: 'Content type' },
];

function openSortPopover() {
    closeSortPopover();

    const popover = document.createElement('div');
    popover.className = 'sort-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Sort options');
    popover.dataset.testid = 'sort-popover';

    // Sort field options
    sortFields.forEach(({ id, label }) => {
        const btn = document.createElement('button');
        const isActive = currentSortField === id;
        btn.className = `w-full text-left px-3 py-1.5 text-xs font-medium rounded transition-colors flex items-center justify-between gap-4 ${isActive ? 'bg-stone-100 text-stone-800' : 'text-stone-500 hover:bg-stone-50 hover:text-stone-700'}`;
        btn.setAttribute('role', 'menuitem');
        btn.dataset.testid = `sort-option-${id}`;
        btn.dataset.sort = id;

        const labelSpan = document.createElement('span');
        labelSpan.textContent = label;
        btn.appendChild(labelSpan);

        if (isActive) {
            const dirIcon = document.createElement('span');
            dirIcon.className = 'text-[10px] text-stone-400';
            dirIcon.textContent = currentSortDir === 'asc' ? '↑ Asc' : '↓ Desc';
            btn.appendChild(dirIcon);
        }

        btn.addEventListener('click', () => {
            if (isActive) {
                // Toggle direction
                setSort(id, currentSortDir === 'asc' ? 'desc' : 'asc');
            } else {
                // Switch field, default desc
                setSort(id, 'desc');
            }
            closeSortPopover();
        });

        popover.appendChild(btn);
    });

    document.body.appendChild(popover);
    positionSortPopover(popover);
}

function positionSortPopover(popover) {
    const btnRect = sortBtn.getBoundingClientRect();
    const pad = 8;
    popover.style.top = `${btnRect.bottom + pad}px`;
    popover.style.left = `${btnRect.right - popover.offsetWidth}px`;

    // Constrain to viewport
    const rect = popover.getBoundingClientRect();
    if (rect.left < pad) popover.style.left = `${pad}px`;
    if (rect.bottom > window.innerHeight - pad) {
        popover.style.top = `${btnRect.top - rect.height - pad}px`;
    }
}

function closeSortPopover() {
    const existing = document.querySelector('.sort-popover');
    if (existing) existing.remove();
}

sortBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    const existing = document.querySelector('.sort-popover');
    if (existing) {
        closeSortPopover();
    } else {
        openSortPopover();
    }
});

document.addEventListener('click', (e) => {
    if (!e.target.closest('.sort-popover') && !e.target.closest('#sort-btn')) {
        closeSortPopover();
    }
});
```

**Step 3: Add script tag**

In `frontend/index.html`, add `<script src="js/sort.js"></script>` after the existing script tags (near the bottom), ensuring it loads after `app.js` (which defines `currentSortField`, `currentSortDir`, `setSort`).

**Step 4: Commit**

```bash
git add frontend/index.html frontend/js/sort.js
git commit -m "feat: add sort button with popover UI"
```

---

### Task 6: Add test selectors and AppHelper methods

**Files:**
- Modify: `e2e/helpers/selectors.ts` — add sort and system metadata selectors
- Modify: `e2e/fixtures/test-fixtures.ts` — add sort and system metadata helper methods

**Step 1: Add selectors**

In `e2e/helpers/selectors.ts`, add to the selectors object:

```typescript
    sort: {
        button: '[data-testid="sort-button"]',
        popover: '[data-testid="sort-popover"]',
        option: (field: string) => `[data-testid="sort-option-${field}"]`,
    },
```

Add to the existing `metadata` section:

```typescript
        systemInfo: '[data-testid="metadata-system-info"]',
        systemRow: '[data-testid="metadata-system-row"]',
```

**Step 2: Add AppHelper methods**

In `e2e/fixtures/test-fixtures.ts`, add to the `AppHelper` class:

```typescript
  // ==================== Sort ====================

  async openSortPopover(): Promise<void> {
    await this.page.locator(selectors.sort.button).click();
    await this.page.waitForSelector(selectors.sort.popover);
  }

  async closeSortPopover(): Promise<void> {
    // Click outside
    await this.page.locator('body').click({ position: { x: 0, y: 0 } });
    await expect(this.page.locator(selectors.sort.popover)).toHaveCount(0);
  }

  async selectSort(field: string): Promise<void> {
    await this.page.locator(selectors.sort.option(field)).click();
    // Popover closes and clips reload
    await expect(this.page.locator(selectors.sort.popover)).toHaveCount(0);
  }

  async getClipFilenames(): Promise<string[]> {
    const cards = this.page.locator(selectors.gallery.clipCard);
    const count = await cards.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      const title = await cards.nth(i).locator('.p-2\\.5 p').getAttribute('title');
      names.push(title || '');
    }
    return names;
  }

  // ==================== System Metadata ====================

  async expectSystemMetadataVisible(): Promise<void> {
    await expect(this.page.locator(selectors.metadata.systemInfo)).toBeVisible();
  }

  async expectSystemMetadataRowCount(count: number): Promise<void> {
    await expect(this.page.locator(selectors.metadata.systemRow)).toHaveCount(count);
  }

  async getSystemMetadataValue(label: string): Promise<string> {
    const rows = this.page.locator(selectors.metadata.systemRow);
    const count = await rows.count();
    for (let i = 0; i < count; i++) {
      const rowLabel = await rows.nth(i).locator('span').first().textContent();
      if (rowLabel?.trim() === label) {
        return (await rows.nth(i).locator('span').nth(1).textContent()) || '';
      }
    }
    return '';
  }
```

**Step 3: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: add sort and system metadata selectors and helpers"
```

---

### Task 7: Write e2e tests for system metadata

**Files:**
- Modify: `e2e/tests/metadata/metadata.spec.ts` — add system metadata tests

**Step 1: Add tests**

Add to the existing `test.describe('Metadata', ...)` block:

```typescript
  test('should show system metadata rows', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.expectSystemMetadataVisible();
    await app.expectSystemMetadataRowCount(4);
    await app.closeMetadataModal();
  });

  test('should show correct filename in system metadata', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    const filenameValue = await app.getSystemMetadataValue('Filename');
    expect(filenameValue).toBe(filename);
    await app.closeMetadataModal();
  });

  test('should show correct content type in system metadata', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    const typeValue = await app.getSystemMetadataValue('Type');
    expect(typeValue).toBe('image/png');
    await app.closeMetadataModal();
  });
```

**Step 2: Run tests**

```bash
cd e2e && npm test -- --grep "system metadata"
```

Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/metadata/metadata.spec.ts
git commit -m "test: add e2e tests for system metadata display"
```

---

### Task 8: Write e2e tests for sorting

**Files:**
- Create: `e2e/tests/sort/sort.spec.ts`

**Step 1: Create test directory and file**

```bash
mkdir -p e2e/tests/sort
```

**Step 2: Write sort tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Sorting', () => {
  test('should show sort button in header', async ({ app }) => {
    await expect(app.page.locator(selectors.sort.button)).toBeVisible();
  });

  test('should open and close sort popover', async ({ app }) => {
    await app.openSortPopover();
    await expect(app.page.locator(selectors.sort.popover)).toBeVisible();
    await app.closeSortPopover();
  });

  test('should sort by filename', async ({ app }) => {
    // Upload files with predictable names
    const img1 = await createTempFile(generateTestImage(10, 10, 'red'), 'png');
    const img2 = await createTempFile(generateTestImage(20, 20, 'blue'), 'png');
    await app.uploadFile(img1);
    await app.uploadFile(img2);
    await app.expectClipCount(2);

    // Sort by name ascending
    await app.openSortPopover();
    await app.selectSort('name');
    // Default is desc, so first click sets name desc
    // Click again to toggle to asc
    await app.openSortPopover();
    await app.selectSort('name');

    const names = await app.getClipFilenames();
    const sorted = [...names].sort((a, b) => a.localeCompare(b));
    expect(names).toEqual(sorted);
  });

  test('should persist sort preference across reload', async ({ app }) => {
    const img = await createTempFile(generateTestImage(), 'png');
    await app.uploadFile(img);

    // Set sort to name
    await app.openSortPopover();
    await app.selectSort('name');

    // Reload
    await app.page.reload();
    await app.page.waitForLoadState('networkidle');

    // Verify sort button still works (preference loaded)
    await app.openSortPopover();
    const nameOption = app.page.locator(selectors.sort.option('name'));
    // The active option should have the active styling
    await expect(nameOption).toHaveClass(/bg-stone-100/);
    await app.closeSortPopover();
  });
});
```

**Step 3: Run tests**

```bash
cd e2e && npm test -- --grep "Sorting"
```

Expected: PASS

**Step 4: Commit**

```bash
git add e2e/tests/sort/
git commit -m "test: add e2e tests for clip sorting"
```

---

### Task 9: Run full e2e test suite

**Step 1: Run all tests**

```bash
cd e2e && npm test
```

Expected: All tests PASS. Fix any failures before proceeding.

**Step 2: Final commit if any fixes needed**

---

### Task 10: Add sort to keyboard shortcut blocking

**Files:**
- Modify: `frontend/js/shortcuts.js` — ensure shortcuts don't fire when sort popover is open

**Step 1: Check if needed**

The sort popover is a dynamically-created element. Since it closes on outside click and keyboard events bubble, this may not be needed. However, if the popover has input elements, we should block shortcuts. Since it's buttons only, skip this task — no input elements means no keyboard conflict.

**Step 2: Verify manually that Escape closes the popover**

Add Escape key handling in `sort.js`:

```javascript
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeSortPopover();
});
```

**Step 3: Commit if changed**

```bash
git add frontend/js/sort.js
git commit -m "fix: close sort popover on Escape key"
```
