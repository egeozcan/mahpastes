# Folder View Drag-and-Drop Reorganization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable drag-and-drop reorganization of clips and folders within folder view — move clips between folders and reparent folders under other folders.

**Architecture:** Pure frontend feature using HTML5 Drag and Drop API with custom drag images via `setDragImage()`. No new backend methods needed — `BulkAddTag`, `BulkRemoveTag`, and `UpdateTag` (which already cascades descendant renames) handle all operations. A new `folder-drag.js` module encapsulates all drag-and-drop logic with event delegation on the gallery and breadcrumb bar.

**Tech Stack:** Vanilla JS, HTML5 DnD API, Wails Go bindings (existing), Playwright e2e tests

---

### Task 1: Add Home Icon to Breadcrumb Bar

**Files:**
- Modify: `frontend/js/tags.js:162-283` (the `updateActiveTagsDisplay()` function)
- Modify: `e2e/helpers/selectors.ts:330-332` (add home icon selector)

**Step 1: Add home icon selector**

In `e2e/helpers/selectors.ts`, add to the `tags` object after the `folderCard` entry (line 332):

```typescript
    folderCard: (name: string) => `[data-testid="folder-card-${name}"]`,
    homeIcon: '[data-testid="folder-home-icon"]',
```

**Step 2: Add home icon to breadcrumb rendering**

In `frontend/js/tags.js`, inside `updateActiveTagsDisplay()`, in the folder mode branch (after line 193 where the label is appended), insert the home icon before the breadcrumb segments. Find this block:

```javascript
            if (typeof isFolderMode === 'function' && isFolderMode()) {
                // Folder mode: breadcrumb-style navigation
                const deepestTag = allTags.find(t => t.id === activeTagFilters[activeTagFilters.length - 1]);
                if (deepestTag) {
                    const segments = deepestTag.name.split('/');
```

Insert the home icon right after `if (deepestTag) {` and before `const segments = ...`:

```javascript
                    // Home icon for root navigation
                    const homeBtn = document.createElement('button');
                    homeBtn.className = 'p-1 rounded transition-colors text-stone-400 hover:text-stone-600 hover:bg-stone-100';
                    homeBtn.setAttribute('data-testid', 'folder-home-icon');
                    homeBtn.setAttribute('data-drop-target', 'root');
                    homeBtn.setAttribute('aria-label', 'Root folder');
                    homeBtn.innerHTML = `
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
                        </svg>
                    `;
                    homeBtn.addEventListener('click', () => {
                        activeTagFilters.length = 0;
                        updateActiveTagsDisplay();
                        renderTagFilterDropdown();
                        loadClips();
                    });
                    activeTagsContainer.appendChild(homeBtn);

                    // Separator after home
                    const homeSep = document.createElement('span');
                    homeSep.className = 'text-stone-300 text-xs';
                    homeSep.textContent = '/';
                    activeTagsContainer.appendChild(homeSep);
```

Also add `data-drop-target` attributes to each breadcrumb pill so they can serve as drop targets. In the same folder-mode branch where pills are created (the `for` loop over segments), add this attribute after the pill is created:

```javascript
                        pill.setAttribute('data-drop-target', String(segTag.id));
```

**Step 3: Run tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/tags/ --reporter=line 2>&1 | tail -30`
Expected: All existing tag tests pass

**Step 4: Commit**

```bash
git add frontend/js/tags.js e2e/helpers/selectors.ts
git commit -m "feat: add home icon to folder breadcrumb bar"
```

---

### Task 2: Make Cards Draggable in Folder Mode

**Files:**
- Modify: `frontend/js/ui.js:1108-1145` (`renderFolderCards()` function)
- Modify: `frontend/js/ui.js:701-703` (`createClipCard()` function)
- Modify: `frontend/js/ui.js:421-446` (`renderDragHandle()` function)

**Step 1: Make folder cards draggable**

In `frontend/js/ui.js`, in `renderFolderCards()`, after the line that sets `data-folder`:

```javascript
        card.setAttribute('data-folder', tag.id);
```

Add:

```javascript
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
```

**Step 2: Make clip cards draggable in folder mode and hide drag-out handle**

In `frontend/js/ui.js`, in `createClipCard()`, after the line `card.setAttribute('aria-label', ...);` (line 712), add:

```javascript
    if (typeof isFolderMode === 'function' && isFolderMode()) {
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
    }
```

In `renderDragHandle()` (line 421), wrap the return with a folder mode check so the drag-out handle is hidden in folder mode:

```javascript
function renderDragHandle(clipId) {
    if (typeof isFolderMode === 'function' && isFolderMode()) {
        return '';
    }
    if (typeof canDragOut !== 'function' || !canDragOut()) {
        return '';
    }
```

**Step 3: Run tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/tags/ --reporter=line 2>&1 | tail -30`
Expected: All existing tag tests pass

**Step 4: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: make clip and folder cards draggable in folder mode"
```

---

### Task 3: Create folder-drag.js — Drag Initiation and Payload

**Files:**
- Create: `frontend/js/folder-drag.js`
- Modify: `frontend/index.html:821-854` (add script tag)

**Step 1: Create the folder-drag.js file with drag initiation logic**

Create `frontend/js/folder-drag.js`:

```javascript
// Folder drag-and-drop reorganization
// Handles internal drag-and-drop of clips and folders within folder view.

// Live region for screen reader announcements
let _folderDragLiveRegion = null;

function _ensureLiveRegion() {
    if (_folderDragLiveRegion) return _folderDragLiveRegion;
    _folderDragLiveRegion = document.createElement('div');
    _folderDragLiveRegion.setAttribute('role', 'status');
    _folderDragLiveRegion.setAttribute('aria-live', 'polite');
    _folderDragLiveRegion.className = 'sr-only';
    document.body.appendChild(_folderDragLiveRegion);
    return _folderDragLiveRegion;
}

function _announceDragResult(message) {
    const region = _ensureLiveRegion();
    region.textContent = '';
    // Force re-announcement by toggling content after a tick
    requestAnimationFrame(() => { region.textContent = message; });
}

// Build a custom drag image element. Returns the DOM element (caller must
// append to document.body before setDragImage and remove on dragend).
function _buildDragImage(type, count, label) {
    const container = document.createElement('div');
    container.style.position = 'fixed';
    container.style.top = '-1000px';
    container.style.left = '-1000px';
    container.style.zIndex = '-1';
    container.style.pointerEvents = 'none';

    if (type === 'clips' && count > 1) {
        // Multi-clip: stacked cards with count badge
        container.innerHTML = `
            <div style="position: relative; width: 120px;">
                <div style="position: absolute; top: 6px; left: 6px; width: 108px; height: 36px;"
                     class="bg-stone-100 rounded-md border border-stone-300"></div>
                <div style="position: absolute; top: 3px; left: 3px; width: 114px; height: 36px;"
                     class="bg-stone-100 rounded-md border border-stone-300"></div>
                <div style="position: relative;" class="bg-white rounded-md border border-stone-800 px-3 py-2">
                    <span class="text-xs font-medium text-stone-800 truncate block">${escapeHTML(label)}</span>
                </div>
                <div style="position: absolute; top: -6px; right: -6px;"
                     class="bg-stone-800 text-white text-[10px] font-medium rounded-full w-5 h-5 flex items-center justify-center">
                    ${count}
                </div>
            </div>
        `;
    } else if (type === 'folder') {
        // Folder: icon + name
        container.innerHTML = `
            <div class="bg-white rounded-md border border-stone-800 px-3 py-2 flex items-center gap-2">
                <svg class="w-4 h-4 text-stone-600" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
                </svg>
                <span class="text-xs font-medium text-stone-800">${escapeHTML(label)}</span>
            </div>
        `;
    } else {
        // Single clip
        container.innerHTML = `
            <div class="bg-white rounded-md border border-stone-800 px-3 py-2">
                <span class="text-xs font-medium text-stone-800 truncate block" style="max-width: 140px;">${escapeHTML(label)}</span>
            </div>
        `;
    }

    return container;
}

// Resolve the drag payload from a dragstart event on a gallery card.
// Returns { type: 'clips'|'folder', ids: number[], folderId: number|null, label: string } or null.
function _resolveDragPayload(card) {
    if (card.hasAttribute('data-folder')) {
        const folderId = Number(card.dataset.folder);
        const tag = allTags.find(t => t.id === folderId);
        const label = tag ? getShortTagName(tag.name) : 'Folder';
        return { type: 'folder', ids: [folderId], folderId: null, label };
    }

    if (card.hasAttribute('data-id')) {
        const clipId = Number(card.dataset.id);
        let ids;
        // Smart behavior: if the dragged clip is part of the selection, move all selected
        if (typeof selectedIds !== 'undefined' && selectedIds.has(clipId) && selectedIds.size > 1) {
            ids = Array.from(selectedIds);
        } else {
            ids = [clipId];
        }

        const filenameEl = card.querySelector('.p-2\\.5 p');
        const label = filenameEl ? filenameEl.getAttribute('title') || filenameEl.textContent : 'Clip';

        // Current folder tag ID (if inside a folder)
        const currentFolderId = activeTagFilters.length > 0
            ? activeTagFilters[activeTagFilters.length - 1]
            : null;

        return { type: 'clips', ids, folderId: currentFolderId, label };
    }

    return null;
}

// Resolve drop target from an event. Returns { tagId: number|null, type: 'folder'|'breadcrumb'|'root' } or null.
function _resolveDropTarget(e) {
    // Walk up from target to find a drop target element
    let el = e.target;
    while (el && el !== document.body) {
        // Folder card in gallery
        if (el.hasAttribute && el.hasAttribute('data-folder')) {
            return { tagId: Number(el.dataset.folder), type: 'folder', element: el };
        }
        // Breadcrumb or home icon drop target
        if (el.hasAttribute && el.hasAttribute('data-drop-target')) {
            const val = el.dataset.dropTarget;
            if (val === 'root') {
                return { tagId: null, type: 'root', element: el };
            }
            return { tagId: Number(val), type: 'breadcrumb', element: el };
        }
        el = el.parentElement;
    }
    return null;
}

// Validate a drop. Returns true if valid.
function _isValidDrop(payload, target) {
    if (!payload || !target) return false;

    if (payload.type === 'clips') {
        // Can't drop clips on the folder they're already in
        if (target.tagId === payload.folderId) return false;
        // Can't drop on root if already at root (folderId is null)
        if (target.tagId === null && payload.folderId === null) return false;
        return true;
    }

    if (payload.type === 'folder') {
        const draggedFolderId = payload.ids[0];
        // Can't drop folder on itself
        if (target.tagId === draggedFolderId) return false;
        // Can't drop on root if already top-level
        const draggedTag = allTags.find(t => t.id === draggedFolderId);
        if (target.tagId === null && draggedTag && !draggedTag.name.includes('/')) return false;
        // Defensive: can't drop folder on its own descendant
        if (target.tagId !== null && draggedTag) {
            const targetTag = allTags.find(t => t.id === target.tagId);
            if (targetTag && isDescendantOf(targetTag.name, draggedTag.name)) return false;
        }
        return true;
    }

    return false;
}

// Execute the drop operation.
async function _executeDrop(payload, target) {
    try {
        if (payload.type === 'clips') {
            if (target.tagId === null) {
                // Moving to root: remove current folder tag
                if (payload.folderId !== null) {
                    await window.go.main.App.BulkRemoveTag(payload.ids, payload.folderId);
                }
            } else {
                // Moving to a folder: BulkAddTag handles tree exclusivity
                await window.go.main.App.BulkAddTag(payload.ids, target.tagId);
            }

            const targetName = target.tagId === null
                ? 'root'
                : (allTags.find(t => t.id === target.tagId)?.name || 'folder');
            _announceDragResult(`Moved ${payload.ids.length} clip${payload.ids.length > 1 ? 's' : ''} to ${targetName}`);

            // Clear selection and reload
            if (typeof selectedIds !== 'undefined') selectedIds.clear();
            if (typeof updateBulkToolbar === 'function') updateBulkToolbar();
            await loadClips();
        } else if (payload.type === 'folder') {
            const draggedTag = allTags.find(t => t.id === payload.ids[0]);
            if (!draggedTag) return;

            const shortName = getShortTagName(draggedTag.name);
            let newName;
            if (target.tagId === null) {
                // Moving to root: just the short name
                newName = shortName;
            } else {
                const targetTag = allTags.find(t => t.id === target.tagId);
                if (!targetTag) return;
                newName = targetTag.name + '/' + shortName;
            }

            // Use UpdateTag which cascades renames to descendants
            await window.go.main.App.UpdateTag(draggedTag.id, newName, draggedTag.color);

            const targetName = target.tagId === null
                ? 'root'
                : (allTags.find(t => t.id === target.tagId)?.name || 'folder');
            _announceDragResult(`Moved folder ${shortName} to ${targetName}`);

            // Reload tags and clips
            if (typeof loadTags === 'function') await loadTags();
            await loadClips();
        }
    } catch (error) {
        console.error('Folder drag-drop failed:', error);
        if (typeof showToast === 'function') {
            showToast(error.message || 'Failed to move item.', 'error');
        }
    }
}

// Highlight/unhighlight drop target
const _DROP_HIGHLIGHT_CLASSES = ['border-stone-800', 'bg-stone-50', 'scale-[1.03]'];
const _BREADCRUMB_HIGHLIGHT_CLASSES = ['bg-stone-100', 'ring-1', 'ring-stone-800', 'rounded'];

function _highlightTarget(target) {
    if (!target || !target.element) return;
    if (target.type === 'folder') {
        target.element.classList.add(..._DROP_HIGHLIGHT_CLASSES);
        // Remove default hover border so highlight is clean
        target.element.classList.remove('border-stone-200');
    } else {
        target.element.classList.add(..._BREADCRUMB_HIGHLIGHT_CLASSES);
    }
    target.element.setAttribute('aria-dropeffect', 'move');
}

function _unhighlightTarget(target) {
    if (!target || !target.element) return;
    if (target.type === 'folder') {
        target.element.classList.remove(..._DROP_HIGHLIGHT_CLASSES);
        target.element.classList.add('border-stone-200');
    } else {
        target.element.classList.remove(..._BREADCRUMB_HIGHLIGHT_CLASSES);
    }
    target.element.removeAttribute('aria-dropeffect');
}

// Track current state for highlight management
let _currentHighlightedTarget = null;
let _currentDragImage = null;
let _currentPayload = null;
let _draggedCards = [];

function initFolderDrag() {
    if (typeof isFolderMode !== 'function' || !isFolderMode()) return;

    const gallery = document.getElementById('gallery');
    const breadcrumb = document.getElementById('active-tags-container');
    if (!gallery) return;

    // --- Gallery drag events (event delegation) ---

    gallery.addEventListener('dragstart', (e) => {
        const card = e.target.closest('li[data-id], li[data-folder]');
        if (!card) { e.preventDefault(); return; }

        const payload = _resolveDragPayload(card);
        if (!payload) { e.preventDefault(); return; }

        _currentPayload = payload;

        // Set drag data
        e.dataTransfer.setData('application/x-folder-drag', JSON.stringify(payload));
        e.dataTransfer.effectAllowed = 'move';

        // Build and set custom drag image
        const dragImage = _buildDragImage(payload.type, payload.ids.length, payload.label);
        document.body.appendChild(dragImage);
        _currentDragImage = dragImage;
        e.dataTransfer.setDragImage(dragImage, 60, 20);

        // Mark dragged cards with opacity
        if (payload.type === 'clips' && payload.ids.length > 1) {
            _draggedCards = payload.ids
                .map(id => gallery.querySelector(`li[data-id="${id}"]`))
                .filter(Boolean);
        } else {
            _draggedCards = [card];
        }
        // Use requestAnimationFrame so the drag image captures before opacity change
        requestAnimationFrame(() => {
            _draggedCards.forEach(c => {
                c.classList.add('opacity-40');
                c.setAttribute('aria-grabbed', 'true');
            });
        });
    });

    gallery.addEventListener('dragend', () => {
        // Clean up drag image
        if (_currentDragImage) {
            _currentDragImage.remove();
            _currentDragImage = null;
        }
        // Restore dragged card opacity
        _draggedCards.forEach(c => {
            c.classList.remove('opacity-40');
            c.setAttribute('aria-grabbed', 'false');
        });
        _draggedCards = [];
        _currentPayload = null;

        // Remove any lingering highlights
        if (_currentHighlightedTarget) {
            _unhighlightTarget(_currentHighlightedTarget);
            _currentHighlightedTarget = null;
        }
    });

    // --- Drop target events for gallery folder cards ---

    gallery.addEventListener('dragover', (e) => {
        if (!_currentPayload) return;
        const target = _resolveDropTarget(e);
        if (target && _isValidDrop(_currentPayload, target)) {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
        }
    });

    gallery.addEventListener('dragenter', (e) => {
        if (!_currentPayload) return;
        const target = _resolveDropTarget(e);

        // Unhighlight previous target if different
        if (_currentHighlightedTarget && (!target || _currentHighlightedTarget.element !== target.element)) {
            _unhighlightTarget(_currentHighlightedTarget);
            _currentHighlightedTarget = null;
        }

        if (target && _isValidDrop(_currentPayload, target)) {
            _highlightTarget(target);
            _currentHighlightedTarget = target;
        }
    });

    gallery.addEventListener('dragleave', (e) => {
        if (!_currentHighlightedTarget) return;
        const target = _resolveDropTarget(e);
        // Only unhighlight if we're actually leaving the target (not entering a child)
        if (target && _currentHighlightedTarget.element === target.element) {
            const rect = target.element.getBoundingClientRect();
            if (e.clientX < rect.left || e.clientX > rect.right || e.clientY < rect.top || e.clientY > rect.bottom) {
                _unhighlightTarget(_currentHighlightedTarget);
                _currentHighlightedTarget = null;
            }
        }
    });

    gallery.addEventListener('drop', async (e) => {
        e.preventDefault();
        if (_currentHighlightedTarget) {
            _unhighlightTarget(_currentHighlightedTarget);
        }

        const target = _resolveDropTarget(e);
        if (!_currentPayload || !target || !_isValidDrop(_currentPayload, target)) return;

        await _executeDrop(_currentPayload, target);
        _currentHighlightedTarget = null;
    });

    // --- Breadcrumb drop target events ---

    if (breadcrumb) {
        breadcrumb.addEventListener('dragover', (e) => {
            if (!_currentPayload) return;
            const target = _resolveDropTarget(e);
            if (target && _isValidDrop(_currentPayload, target)) {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
            }
        });

        breadcrumb.addEventListener('dragenter', (e) => {
            if (!_currentPayload) return;
            const target = _resolveDropTarget(e);

            if (_currentHighlightedTarget && (!target || _currentHighlightedTarget.element !== target.element)) {
                _unhighlightTarget(_currentHighlightedTarget);
                _currentHighlightedTarget = null;
            }

            if (target && _isValidDrop(_currentPayload, target)) {
                _highlightTarget(target);
                _currentHighlightedTarget = target;
            }
        });

        breadcrumb.addEventListener('dragleave', (e) => {
            if (!_currentHighlightedTarget) return;
            const related = e.relatedTarget;
            // Only unhighlight if leaving the breadcrumb container entirely
            if (!breadcrumb.contains(related)) {
                _unhighlightTarget(_currentHighlightedTarget);
                _currentHighlightedTarget = null;
            }
        });

        breadcrumb.addEventListener('drop', async (e) => {
            e.preventDefault();
            if (_currentHighlightedTarget) {
                _unhighlightTarget(_currentHighlightedTarget);
            }

            const target = _resolveDropTarget(e);
            if (!_currentPayload || !target || !_isValidDrop(_currentPayload, target)) return;

            await _executeDrop(_currentPayload, target);
            _currentHighlightedTarget = null;
        });
    }
}
```

**Step 2: Add script tag to index.html**

In `frontend/index.html`, add the script tag after `transfer.js` and before `context-menu.js` (around line 833):

```html
<script src="js/transfer.js"></script>
<script src="js/folder-drag.js"></script>
<script src="js/context-menu.js"></script>
```

**Step 3: Verify file loads without errors**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails dev &` then check browser console for errors. Kill after verification.

**Step 4: Commit**

```bash
git add frontend/js/folder-drag.js frontend/index.html
git commit -m "feat: add folder-drag.js with drag initiation, drop targets, and execution logic"
```

---

### Task 4: Wire Up initFolderDrag() in app.js

**Files:**
- Modify: `frontend/js/wails-api.js:54-56` (call initFolderDrag after renderFolderCards)

**Step 1: Call initFolderDrag after folder cards render**

In `frontend/js/wails-api.js`, inside the `loadClips()` function, after the folder cards are rendered (line 55-56), add the call:

```javascript
        // Render folder cards in folder mode
        if (isFolderMode()) {
            await renderFolderCards();
            if (typeof initFolderDrag === 'function') initFolderDrag();
        }
```

**Step 2: Run dev build to verify**

Run: `cd /Users/egecan/Code/mahpastes && make dev` and test manually that folder mode still works, folder cards and clip cards show as draggable.

**Step 3: Commit**

```bash
git add frontend/js/wails-api.js
git commit -m "feat: wire up initFolderDrag after folder cards render"
```

---

### Task 5: Write E2E Tests — Clip Drag to Folder

**Files:**
- Create: `e2e/tests/folder-drag/folder-drag.spec.ts`

**Step 1: Write the test file for clip drag operations**

Create `e2e/tests/folder-drag/folder-drag.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import path from 'path';

test.describe('Folder Drag and Drop', () => {
  test.describe('Clip drag to folder', () => {
    test('should move a single clip into a folder via drag', async ({ app, page }) => {
      // Setup: create tag hierarchy and upload clips
      await app.createTag('photos');
      await app.createTag('photos/vacation');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      // Tag both clips with 'photos'
      await app.addTagToClip(path.basename(img1), 'photos');
      await app.addTagToClip(path.basename(img2), 'photos');

      // Enter folder mode and navigate to 'photos'
      await app.toggleFolderMode();
      await app.clickFolder('photos');

      // Verify: 2 clips visible + vacation subfolder
      await app.expectClipCount(2);
      await app.expectFolderVisible('vacation');

      // Drag first clip onto vacation folder
      const clipCard = page.locator(`li[data-id]`).first();
      const folderCard = page.locator(selectors.tags.folderCard('vacation'));
      await clipCard.dragTo(folderCard);

      // After drag: only 1 clip in photos folder
      await app.expectClipCount(1);
    });

    test('should move clip to root via home icon', async ({ app, page }) => {
      await app.createTag('work');
      const img = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
      await app.uploadFile(img);
      await app.addTagToClip(path.basename(img), 'work');

      await app.toggleFolderMode();
      await app.clickFolder('work');
      await app.expectClipCount(1);

      // Drag clip onto home icon
      const clipCard = page.locator(`li[data-id]`).first();
      const homeIcon = page.locator(selectors.tags.homeIcon);
      await clipCard.dragTo(homeIcon);

      // Clip should no longer be in 'work' folder — navigate to root
      // After drop, we should be reloaded in the same view but clip is gone
      await app.expectClipCount(0);
    });

    test('should move all selected clips when dragging a selected clip', async ({ app, page }) => {
      await app.createTag('inbox');
      await app.createTag('inbox/done');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      const img3 = await createTempFile(generateTestImage(100, 100, 'green'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.uploadFile(img3);
      await app.addTagToClip(path.basename(img1), 'inbox');
      await app.addTagToClip(path.basename(img2), 'inbox');
      await app.addTagToClip(path.basename(img3), 'inbox');

      await app.toggleFolderMode();
      await app.clickFolder('inbox');
      await app.expectClipCount(3);

      // Select first two clips
      await page.locator(`li[data-id] .clip-checkbox`).first().click();
      await page.locator(`li[data-id] .clip-checkbox`).nth(1).click();

      // Drag first selected clip onto 'done' folder
      const firstClip = page.locator(`li[data-id]`).first();
      const doneFolder = page.locator(selectors.tags.folderCard('done'));
      await firstClip.dragTo(doneFolder);

      // Only 1 clip should remain (the unselected one)
      await app.expectClipCount(1);
    });

    test('should move only dragged clip when it is not selected', async ({ app, page }) => {
      await app.createTag('misc');
      await app.createTag('misc/archive');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img1);
      await app.uploadFile(img2);
      await app.addTagToClip(path.basename(img1), 'misc');
      await app.addTagToClip(path.basename(img2), 'misc');

      await app.toggleFolderMode();
      await app.clickFolder('misc');
      await app.expectClipCount(2);

      // Select first clip only
      await page.locator(`li[data-id] .clip-checkbox`).first().click();

      // Drag second (unselected) clip onto archive folder
      const secondClip = page.locator(`li[data-id]`).nth(1);
      const archiveFolder = page.locator(selectors.tags.folderCard('archive'));
      await secondClip.dragTo(archiveFolder);

      // Only 1 clip moved, 1 remains
      await app.expectClipCount(1);
    });

    test('should move clip to breadcrumb parent', async ({ app, page }) => {
      await app.createTag('projects');
      await app.createTag('projects/alpha');
      const img = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      await app.uploadFile(img);
      await app.addTagToClip(path.basename(img), 'projects/alpha');

      await app.toggleFolderMode();
      await app.clickFolder('projects');
      await app.clickFolder('alpha');
      await app.expectClipCount(1);

      // Drag clip onto the 'projects' breadcrumb pill
      const clipCard = page.locator(`li[data-id]`).first();
      const breadcrumbPill = page.locator('[data-drop-target]').filter({ hasText: 'projects' }).first();
      await clipCard.dragTo(breadcrumbPill);

      // Clip should be gone from alpha
      await app.expectClipCount(0);
    });
  });

  test.describe('Folder drag to folder', () => {
    test('should reparent folder into another folder', async ({ app, page }) => {
      await app.createTag('src');
      await app.createTag('dest');

      await app.toggleFolderMode();
      await app.expectFolderVisible('src');
      await app.expectFolderVisible('dest');

      // Drag 'src' folder onto 'dest' folder
      const srcFolder = page.locator(selectors.tags.folderCard('src'));
      const destFolder = page.locator(selectors.tags.folderCard('dest'));
      await srcFolder.dragTo(destFolder);

      // 'src' should no longer be visible at root
      await app.expectFolderNotVisible('src');

      // Navigate into 'dest' — 'src' should now be a subfolder
      await app.clickFolder('dest');
      await app.expectFolderVisible('src');
    });

    test('should reparent folder with descendants', async ({ app, page }) => {
      await app.createTag('team');
      await app.createTag('team/frontend');
      await app.createTag('archive');

      await app.toggleFolderMode();

      // Drag 'team' onto 'archive'
      const teamFolder = page.locator(selectors.tags.folderCard('team'));
      const archiveFolder = page.locator(selectors.tags.folderCard('archive'));
      await teamFolder.dragTo(archiveFolder);

      // Navigate into archive → team → frontend should exist
      await app.clickFolder('archive');
      await app.expectFolderVisible('team');
      await app.clickFolder('team');
      await app.expectFolderVisible('frontend');
    });

    test('should move folder to root via home icon', async ({ app, page }) => {
      await app.createTag('container');
      await app.createTag('container/inner');

      await app.toggleFolderMode();
      await app.clickFolder('container');
      await app.expectFolderVisible('inner');

      // Drag 'inner' to home icon
      const innerFolder = page.locator(selectors.tags.folderCard('inner'));
      const homeIcon = page.locator(selectors.tags.homeIcon);
      await innerFolder.dragTo(homeIcon);

      // 'inner' should now be at root level
      // Navigate to root first
      await page.locator(selectors.tags.homeIcon).click();
      await app.expectFolderVisible('inner');
      await app.expectFolderVisible('container');
    });

    test('should show error toast on name conflict', async ({ app, page }) => {
      // Create two top-level tags with same leaf name under different parents
      await app.createTag('a');
      await app.createTag('a/shared');
      await app.createTag('shared');

      await app.toggleFolderMode();

      // Drag 'a' folder's 'shared' subfolder to root — conflicts with existing 'shared'
      await app.clickFolder('a');
      await app.expectFolderVisible('shared');

      const sharedFolder = page.locator(selectors.tags.folderCard('shared'));
      const homeIcon = page.locator(selectors.tags.homeIcon);
      await sharedFolder.dragTo(homeIcon);

      // Should show error toast
      await expect(page.locator('.toast, [role="alert"]').filter({ hasText: /already exists|conflict/i })).toBeVisible({ timeout: 5000 });
    });
  });

  test.describe('Edge cases', () => {
    test('should not allow dropping folder on itself', async ({ app, page }) => {
      await app.createTag('self');
      await app.createTag('self/child');

      await app.toggleFolderMode();
      await app.clickFolder('self');

      // Attempt to drag 'child' onto itself — should be a no-op
      const childFolder = page.locator(selectors.tags.folderCard('child'));
      // Drag to same element — this should not cause any change
      await childFolder.dragTo(childFolder);

      // Folder should still be visible (no change)
      await app.expectFolderVisible('child');
    });
  });
});
```

**Step 2: Run the tests to see them fail (TDD red)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-drag/ --reporter=line 2>&1 | tail -30`
Expected: Tests fail because the drag-and-drop functionality may not work end-to-end yet. Some tests may pass if the implementation from Tasks 1-4 is already in place.

**Step 3: Debug and fix any test issues**

If tests fail due to implementation issues (not test-writing issues), fix the implementation in `folder-drag.js`, `tags.js`, or `ui.js` as needed.

**Step 4: Run all tests to verify no regressions**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test --reporter=line 2>&1 | tail -50`
Expected: All tests pass

**Step 5: Commit**

```bash
git add e2e/tests/folder-drag/
git commit -m "test: add e2e tests for folder drag-and-drop reorganization"
```

---

### Task 6: Final Integration Testing and Cleanup

**Files:**
- Possibly modify: `frontend/js/folder-drag.js` (any fixes from test debugging)
- Possibly modify: `frontend/js/tags.js`, `frontend/js/ui.js` (any fixes)

**Step 1: Run the full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test --reporter=line 2>&1 | tail -50`
Expected: All tests pass including new folder-drag tests

**Step 2: Manual smoke test**

Run `make dev` and manually verify:
- Enter folder mode
- Create some tags and clips
- Drag a clip onto a folder card — clip moves
- Drag a folder onto another folder — folder reparents
- Drag clip onto breadcrumb — moves up
- Drag clip onto home icon — moves to root
- Drag multiple selected clips — all move
- Verify drag ghost shows correctly (count badge for multi)
- Verify drop target highlights on hover
- Verify screen reader announcements work

**Step 3: Fix any issues found during manual testing**

Apply fixes as needed and re-run tests.

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: polish folder drag-and-drop from integration testing"
```

**Step 5: Run full test suite one final time**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test --reporter=line 2>&1 | tail -50`
Expected: All tests pass
