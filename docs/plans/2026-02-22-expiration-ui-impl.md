# Expiration UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add frontend controls for setting/canceling clip expiration at upload time, via context menu, and in bulk.

**Architecture:** Backend gets 3 new methods (`SetExpiration`, `BulkSetExpiration`, `BulkCancelExpiration`). Frontend adds an expiration dropdown near the upload button, expiration items in the card context menu with a preset popover, bulk toolbar buttons, enhanced Temp badge with remaining time, and auto-refresh on window focus.

**Tech Stack:** Go (Wails), Vanilla JS, Tailwind CSS, Playwright e2e tests

---

### Task 1: Backend — Add SetExpiration Method

**Files:**
- Modify: `app.go:712` (after `CancelExpiration`)

**Step 1: Write `SetExpiration` method**

Add after `CancelExpiration` (line 712) in `app.go`:

```go
// SetExpiration sets the expiration for a clip
func (a *App) SetExpiration(id int64, minutes int) error {
	if minutes <= 0 {
		return fmt.Errorf("expiration minutes must be positive")
	}
	expiresAt := time.Now().Add(time.Duration(minutes) * time.Minute)
	_, err := a.db.Exec("UPDATE clips SET expires_at = ? WHERE id = ?", expiresAt, id)
	if err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add app.go
git commit -m "feat: add SetExpiration backend method"
```

---

### Task 2: Backend — Add BulkSetExpiration and BulkCancelExpiration

**Files:**
- Modify: `app.go` (after `BulkArchive`, around line 1079)

**Step 1: Write `BulkSetExpiration` method**

Add after `BulkArchive` in `app.go`, following the same placeholder pattern as `BulkDelete`:

```go
// BulkSetExpiration sets expiration on multiple clips
func (a *App) BulkSetExpiration(ids []int64, minutes int) error {
	if len(ids) == 0 {
		return nil
	}
	if minutes <= 0 {
		return fmt.Errorf("expiration minutes must be positive")
	}

	expiresAt := time.Now().Add(time.Duration(minutes) * time.Minute)

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = expiresAt
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf("UPDATE clips SET expires_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk set expiration: %w", err)
	}
	return nil
}

// BulkCancelExpiration removes expiration from multiple clips
func (a *App) BulkCancelExpiration(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE clips SET expires_at = NULL WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk cancel expiration: %w", err)
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add app.go
git commit -m "feat: add BulkSetExpiration and BulkCancelExpiration backend methods"
```

---

### Task 3: Regenerate Frontend Bindings

**Files:**
- Auto-generated: `frontend/wailsjs/go/main/App.js` and `App.d.ts`

**Step 1: Regenerate bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`

**Step 2: Verify new methods appear in bindings**

Check that `SetExpiration`, `BulkSetExpiration`, and `BulkCancelExpiration` appear in `frontend/wailsjs/go/main/App.js`.

**Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for expiration methods"
```

---

### Task 4: Frontend — Add Expiration API Wrapper Functions

**Files:**
- Modify: `frontend/js/wails-api.js` (after `bulkRemoveTag`, around line 300)

**Step 1: Add wrapper functions**

Add at end of `wails-api.js` (before the closing of the file):

```javascript
// --- Expiration API functions ---

async function setExpiration(id, minutes) {
    try {
        await window.go.main.App.SetExpiration(id, minutes);
        showToast('Expiration set.');
        loadClips();
    } catch (error) {
        console.error('Error setting expiration:', error);
        showToast('Failed to set expiration.');
    }
}

async function cancelExpiration(id) {
    try {
        await window.go.main.App.CancelExpiration(id);
        showToast('Expiration canceled.');
        loadClips();
    } catch (error) {
        console.error('Error canceling expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}

async function bulkSetExpiration(ids, minutes) {
    try {
        await window.go.main.App.BulkSetExpiration(ids, minutes);
        showToast(`Expiration set on ${ids.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk set expiration:', error);
        showToast('Failed to set expiration.');
    }
}

async function bulkCancelExpiration(ids) {
    try {
        await window.go.main.App.BulkCancelExpiration(ids);
        showToast(`Expiration canceled on ${ids.length} clips.`);
        loadClips();
    } catch (error) {
        console.error('Error in bulk cancel expiration:', error);
        showToast('Failed to cancel expiration.');
    }
}
```

**Step 2: Update the `upload` function to accept expiration**

Change the existing `upload` function (line 40-51) to read from a session-scoped variable:

```javascript
async function upload(files) {
    try {
        const minutes = typeof getUploadExpirationMinutes === 'function' ? getUploadExpirationMinutes() : 0;
        await window.go.main.App.UploadFiles(files, minutes);
        showToast('Upload successful!');
        if (!isViewingArchive) {
            loadClips();
        }
    } catch (error) {
        console.error('Error uploading:', error);
        showToast('Upload failed.');
    }
}
```

**Step 3: Commit**

```bash
git add frontend/js/wails-api.js
git commit -m "feat: add expiration API wrapper functions and wire upload expiration"
```

---

### Task 5: Frontend — Add formatTimeRemaining Utility

**Files:**
- Modify: `frontend/js/utils.js`

**Step 1: Add the utility function**

Add `formatTimeRemaining` to `utils.js`:

```javascript
// Format remaining time for expiration badge
// Returns compact string like "23m", "2h", "3d"
function formatTimeRemaining(expiresAt) {
    const now = new Date();
    const expires = new Date(expiresAt);
    const diffMs = expires - now;

    if (diffMs <= 0) return '0m';

    const minutes = Math.ceil(diffMs / 60000);
    if (minutes < 60) return `${minutes}m`;

    const hours = Math.round(diffMs / 3600000);
    if (hours < 24) return `${hours}h`;

    const days = Math.round(diffMs / 86400000);
    return `${days}d`;
}
```

**Step 2: Commit**

```bash
git add frontend/js/utils.js
git commit -m "feat: add formatTimeRemaining utility for expiration badges"
```

---

### Task 6: Frontend — Enhance Temp Badge with Remaining Time

**Files:**
- Modify: `frontend/js/ui.js:600-605` (the `expirationBadge` section in `createClipCard`)

**Step 1: Update the badge rendering**

Replace lines 600-605 in `ui.js`:

```javascript
    let expirationBadge = '';
    if (clip.expires_at) {
        const remaining = formatTimeRemaining(clip.expires_at);
        expirationBadge = `<div class="absolute top-2 left-2 bg-stone-700 text-white text-[8px] font-semibold px-1.5 py-0.5 rounded z-20 uppercase tracking-wide">
            Temp · ${remaining}
        </div>`;
    }
```

**Step 2: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: enhance Temp badge to show remaining time"
```

---

### Task 7: Frontend — Add Expiration Dropdown to Header

**Files:**
- Modify: `frontend/index.html` (after `#header-add-btn`, around line 84)
- Modify: `frontend/js/app.js` (add `getUploadExpirationMinutes` function)

**Step 1: Add the dropdown HTML**

In `index.html`, add after the `#header-add-btn` button (line 84) and before the hamburger menu button:

```html
            <!-- Expiration Dropdown -->
            <select id="upload-expiry-select"
                class="border border-stone-200 hover:border-stone-300 text-stone-500 text-xs font-medium py-2 px-2 rounded-md transition-colors bg-white focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20"
                aria-label="Upload expiration"
                style="--wails-draggable: no-drag"
                title="Set expiration for new uploads">
                <option value="0">No Expiry</option>
                <option value="15">15m</option>
                <option value="60">1h</option>
                <option value="360">6h</option>
                <option value="1440">24h</option>
                <option value="10080">7d</option>
            </select>
```

**Step 2: Add the JS getter function**

In `app.js`, add near the top (after the DOM element constants, around line 60):

```javascript
const uploadExpirySelect = document.getElementById('upload-expiry-select');

function getUploadExpirationMinutes() {
    return parseInt(uploadExpirySelect.value, 10) || 0;
}
```

**Step 3: Commit**

```bash
git add frontend/index.html frontend/js/app.js
git commit -m "feat: add expiration dropdown to header for upload-time expiration"
```

---

### Task 8: Frontend — Add Expiration Items to Context Menu

**Files:**
- Modify: `frontend/js/ui.js:32-47` (add icons to `getMenuIcon`)
- Modify: `frontend/js/ui.js:60-80` (add menu items in `renderCardMenu`)

**Step 1: Add icons to `getMenuIcon`**

In `ui.js`, add to the `icons` object in `getMenuIcon` (around line 33):

```javascript
        'set-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>',
        'cancel-expiration': '<path stroke-linecap="round" stroke-linejoin="round" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"/>',
```

**Step 2: Add menu items between Tags and Archive**

In `renderCardMenu` (around line 78), replace the block:

```javascript
    builtInActions.push({ id: 'tags', label: 'Tags', icon: 'tags' });
    builtInActions.push({ id: 'archive', label: isViewingArchive ? 'Restore' : 'Archive', icon: isViewingArchive ? 'restore' : 'archive' });
```

With:

```javascript
    builtInActions.push({ id: 'tags', label: 'Tags', icon: 'tags' });
    if (clip.expires_at) {
        builtInActions.push({ id: 'cancel-expiration', label: 'Cancel Expiration', icon: 'cancel-expiration' });
    } else {
        builtInActions.push({ id: 'set-expiration', label: 'Set Expiration', icon: 'set-expiration' });
    }
    builtInActions.push({ id: 'archive', label: isViewingArchive ? 'Restore' : 'Archive', icon: isViewingArchive ? 'restore' : 'archive' });
```

**Step 3: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: add expiration menu items to card context menu"
```

---

### Task 9: Frontend — Add Expiration Preset Popover

**Files:**
- Modify: `frontend/js/ui.js` (add popover rendering and logic)
- Modify: `frontend/js/ui.js` (update `handleCardAction`)
- Modify: `frontend/css/main.css` (add popover styles if needed)

**Step 1: Add the expiration popover function**

Add in `ui.js`, after `handleCardAction` (around line 266):

```javascript
// Expiration preset popover
const EXPIRATION_PRESETS = [
    { label: '15m', minutes: 15 },
    { label: '1h', minutes: 60 },
    { label: '6h', minutes: 360 },
    { label: '24h', minutes: 1440 },
    { label: '7d', minutes: 10080 },
];

function openExpirationPopover(clipId, anchorElement) {
    closeExpirationPopover();

    const popover = document.createElement('div');
    popover.className = 'expiration-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Set expiration');

    const row = document.createElement('div');
    row.className = 'flex items-center gap-1.5';

    EXPIRATION_PRESETS.forEach(preset => {
        const btn = document.createElement('button');
        btn.className = 'px-2.5 py-1.5 text-[11px] font-medium text-stone-600 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors';
        btn.textContent = preset.label;
        btn.setAttribute('role', 'menuitem');
        btn.addEventListener('click', () => {
            closeExpirationPopover();
            setExpiration(Number(clipId), preset.minutes);
        });
        row.appendChild(btn);
    });

    popover.appendChild(row);
    document.body.appendChild(popover);

    // Position relative to anchor
    positionExpirationPopover(popover, anchorElement);
}

function positionExpirationPopover(popover, anchor) {
    const rect = anchor.getBoundingClientRect();
    const popoverRect = popover.getBoundingClientRect();
    const pad = 8;

    const spaceBelow = window.innerHeight - rect.bottom - pad;
    let top;
    if (spaceBelow >= popoverRect.height) {
        top = rect.bottom + pad;
    } else {
        top = rect.top - popoverRect.height - pad;
    }

    let left = rect.left + (rect.width / 2) - (popoverRect.width / 2);
    if (left < pad) left = pad;
    if (left + popoverRect.width > window.innerWidth - pad) {
        left = window.innerWidth - popoverRect.width - pad;
    }

    popover.style.left = `${left}px`;
    popover.style.top = `${top}px`;
}

function closeExpirationPopover() {
    const existing = document.querySelector('.expiration-popover');
    if (existing) existing.remove();
}
```

**Step 2: Wire up `handleCardAction` cases**

In `handleCardAction` switch statement (around line 235), add before the `case 'archive':` line:

```javascript
        case 'set-expiration': {
            const card = gallery.querySelector(`li[data-id="${clipId}"]`);
            if (card) {
                const menuBtn = card.querySelector('[data-action="menu"]');
                openExpirationPopover(clipId, menuBtn || triggerButton);
            }
            break;
        }
        case 'cancel-expiration':
            cancelExpiration(id);
            break;
```

**Step 3: Add click-outside listener to close popover**

Add to the existing click-outside listeners section in `app.js` (around line 740):

```javascript
// Close expiration popover when clicking outside
document.addEventListener('click', (e) => {
    const popover = document.querySelector('.expiration-popover');
    if (!popover) return;
    if (!e.target.closest('.expiration-popover') && !e.target.closest('[data-action="set-expiration"]')) {
        closeExpirationPopover();
    }
});
```

**Step 4: Commit**

```bash
git add frontend/js/ui.js frontend/js/app.js
git commit -m "feat: add expiration preset popover for context menu"
```

---

### Task 10: Frontend — Add Bulk Expiration Buttons

**Files:**
- Modify: `frontend/index.html` (add buttons to bulk toolbar, around line 288)
- Modify: `frontend/js/app.js` (wire up event listeners and keyboard shortcuts)
- Modify: `frontend/js/ui.js` (update `updateBulkToolbar`)

**Step 1: Add bulk buttons to HTML**

In `index.html`, add after the `#bulk-tag-btn` button (around line 263) and before `#bulk-copy-btn`:

```html
                <button id="bulk-expiry-btn"
                    class="flex items-center gap-1.5 bg-stone-100 hover:bg-stone-200 text-stone-700 px-3 py-1.5 rounded-md text-xs font-medium transition-colors">
                    <svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    Expire
                </button>
                <button id="bulk-cancel-expiry-btn"
                    class="hidden flex items-center gap-1.5 bg-stone-100 hover:bg-stone-200 text-stone-700 px-3 py-1.5 rounded-md text-xs font-medium transition-colors">
                    <svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    Clear Expiry
                </button>
```

**Step 2: Wire up JS in `app.js`**

Add DOM element constants (near line 60):

```javascript
const bulkExpiryBtn = document.getElementById('bulk-expiry-btn');
const bulkCancelExpiryBtn = document.getElementById('bulk-cancel-expiry-btn');
```

Add event listeners (near line 232):

```javascript
bulkExpiryBtn.addEventListener('click', () => {
    openExpirationPopover(null, bulkExpiryBtn, true);
});
bulkCancelExpiryBtn.addEventListener('click', bulkCancelExpiry);
```

Add the bulk cancel function:

```javascript
async function bulkCancelExpiry() {
    if (selectedIds.size === 0) return;
    await bulkCancelExpiration(Array.from(selectedIds));
    selectedIds.clear();
}
```

**Step 3: Update `openExpirationPopover` to handle bulk mode**

Modify the function signature and callback in `ui.js`:

```javascript
function openExpirationPopover(clipId, anchorElement, isBulk = false) {
    closeExpirationPopover();

    const popover = document.createElement('div');
    popover.className = 'expiration-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Set expiration');

    const row = document.createElement('div');
    row.className = 'flex items-center gap-1.5';

    EXPIRATION_PRESETS.forEach(preset => {
        const btn = document.createElement('button');
        btn.className = 'px-2.5 py-1.5 text-[11px] font-medium text-stone-600 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors';
        btn.textContent = preset.label;
        btn.setAttribute('role', 'menuitem');
        btn.addEventListener('click', () => {
            closeExpirationPopover();
            if (isBulk) {
                bulkSetExpiration(Array.from(selectedIds), preset.minutes);
                selectedIds.clear();
            } else {
                setExpiration(Number(clipId), preset.minutes);
            }
        });
        row.appendChild(btn);
    });

    popover.appendChild(row);
    document.body.appendChild(popover);
    positionExpirationPopover(popover, anchorElement);
}
```

**Step 4: Update `updateBulkToolbar` in `ui.js`**

Add logic to show/hide the "Clear Expiry" button based on selected clips. Inside `updateBulkToolbar` (around line 767), add after the comparison logic:

```javascript
        // Show "Clear Expiry" if any selected clip has expiration
        const bulkCancelExpiryBtn = document.getElementById('bulk-cancel-expiry-btn');
        if (bulkCancelExpiryBtn) {
            const anyExpiring = Array.from(selectedIds).some(id => {
                const card = gallery.querySelector(`li[data-id="${id}"]`);
                return card && card.dataset.expiresAt;
            });
            if (anyExpiring) {
                bulkCancelExpiryBtn.classList.remove('hidden');
            } else {
                bulkCancelExpiryBtn.classList.add('hidden');
            }
        }
```

Also, in `createClipCard` in `ui.js`, add `data-expires-at` to the card element so the toolbar can detect it. Find where `card.dataset.type` is set and add:

```javascript
    if (clip.expires_at) {
        card.dataset.expiresAt = clip.expires_at;
    }
```

**Step 5: Register keyboard shortcut**

In `app.js`, add with the other bulk shortcuts (around line 575):

```javascript
        ShortcutManager.register({
            id: 'bulk-expire', label: 'Set Expiration', category: 'bulk',
            defaultKey: 'x', context: 'bulk',
            callback: () => {
                const btn = document.getElementById('bulk-expiry-btn');
                if (btn) btn.click();
            }
        });
```

**Step 6: Commit**

```bash
git add frontend/index.html frontend/js/app.js frontend/js/ui.js
git commit -m "feat: add bulk expiration buttons to toolbar"
```

---

### Task 11: Frontend — Auto-Refresh on Window Focus

**Files:**
- Modify: `frontend/js/app.js` (add focus listener, around line 737)

**Step 1: Add focus listener**

In `app.js`, inside the `window.addEventListener('load', ...)` block, after the plugin toast event listener (around line 737):

```javascript
    // Auto-refresh clips when window regains focus (clears stale expired clips)
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible' && window.__appReady) {
            loadClips();
        }
    });
```

**Step 2: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: auto-refresh gallery on window focus to clear stale expired clips"
```

---

### Task 12: Update Selectors for E2E Tests

**Files:**
- Modify: `e2e/helpers/selectors.ts`

**Step 1: Add expiration selectors**

Add to the `cardMenu` section:

```typescript
    setExpiration: '.card-menu-dropdown [data-action="set-expiration"]',
    cancelExpiration: '.card-menu-dropdown [data-action="cancel-expiration"]',
```

Add to the `bulk` section:

```typescript
    expiryButton: '#bulk-expiry-btn',
    cancelExpiryButton: '#bulk-cancel-expiry-btn',
```

Add a new `expiration` section:

```typescript
  // Expiration
  expiration: {
    popover: '.expiration-popover',
    presetButton: (label: string) => `.expiration-popover button:has-text("${label}")`,
    uploadSelect: '#upload-expiry-select',
    badge: '.absolute.top-2.left-2',
  },
```

**Step 2: Commit**

```bash
cd /Users/egecan/Code/mahpastes && git add e2e/helpers/selectors.ts
git commit -m "chore: add expiration selectors for e2e tests"
```

---

### Task 13: Update AppHelper Fixture with Expiration Methods

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

**Step 1: Add expiration helper methods**

Add to the `AppHelper` class:

```typescript
  // ==================== Expiration ====================

  async setExpirationViaMenu(filename: string, preset: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.menuTrigger).click();
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    await this.page.locator(selectors.cardMenu.setExpiration).click();
    await this.page.waitForSelector(selectors.expiration.popover);
    await this.page.locator(selectors.expiration.popover).locator(`button`, { hasText: preset }).click();
  }

  async cancelExpirationViaMenu(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.clipActions.menuTrigger).click();
    await this.page.waitForSelector(selectors.cardMenu.dropdown);
    await this.page.locator(selectors.cardMenu.cancelExpiration).click();
  }

  async setUploadExpiration(preset: string): Promise<void> {
    await this.page.locator(selectors.expiration.uploadSelect).selectOption(preset);
  }

  async expectClipHasExpirationBadge(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip.locator(selectors.expiration.badge)).toBeVisible();
    await expect(clip.locator(selectors.expiration.badge)).toContainText('Temp');
  }

  async expectClipHasNoExpirationBadge(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await expect(clip.locator(selectors.expiration.badge)).not.toBeVisible();
  }
```

**Step 2: Commit**

```bash
cd /Users/egecan/Code/mahpastes && git add e2e/fixtures/test-fixtures.ts
git commit -m "chore: add expiration helper methods to AppHelper fixture"
```

---

### Task 14: Write E2E Tests for Expiration Feature

**Files:**
- Create: `e2e/tests/clips/expiration.spec.ts`

**Step 1: Write the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import { selectors } from '../../helpers/selectors.js';
import * as path from 'path';

test.describe('Clip Expiration', () => {
  test.describe('Upload with Expiration', () => {
    test('should upload a clip with expiration and show Temp badge', async ({ app }) => {
      // Set expiration to 15 minutes before uploading
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);
      await app.expectClipHasExpirationBadge(path.basename(imagePath));
    });

    test('should upload without expiration by default', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);
      await app.expectClipHasNoExpirationBadge(path.basename(imagePath));
    });

    test('should persist expiration dropdown selection across uploads', async ({ app }) => {
      await app.setUploadExpiration('60');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      await app.uploadFile(img1);
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFile(img2);
      await app.expectClipCount(2);
      // Both should have expiration badges
      await app.expectClipHasExpirationBadge(path.basename(img1));
      await app.expectClipHasExpirationBadge(path.basename(img2));
    });
  });

  test.describe('Context Menu Expiration', () => {
    test('should set expiration via context menu', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const filename = path.basename(imagePath);
      await app.expectClipHasNoExpirationBadge(filename);

      await app.setExpirationViaMenu(filename, '1h');
      await app.expectClipHasExpirationBadge(filename);
    });

    test('should cancel expiration via context menu', async ({ app }) => {
      // Upload with expiration
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const filename = path.basename(imagePath);
      await app.expectClipHasExpirationBadge(filename);

      // Reset upload dropdown
      await app.setUploadExpiration('0');

      // Cancel expiration
      await app.cancelExpirationViaMenu(filename);
      await app.expectClipHasNoExpirationBadge(filename);
    });

    test('should show Set Expiration for non-expiring clips', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      await clip.hover();
      await clip.locator(selectors.clipActions.menuTrigger).click();
      await app.page.waitForSelector(selectors.cardMenu.dropdown);
      await expect(app.page.locator(selectors.cardMenu.setExpiration)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.cancelExpiration)).not.toBeVisible();
    });

    test('should show Cancel Expiration for expiring clips', async ({ app }) => {
      await app.setUploadExpiration('15');
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      await clip.hover();
      await clip.locator(selectors.clipActions.menuTrigger).click();
      await app.page.waitForSelector(selectors.cardMenu.dropdown);
      await expect(app.page.locator(selectors.cardMenu.cancelExpiration)).toBeVisible();
      await expect(app.page.locator(selectors.cardMenu.setExpiration)).not.toBeVisible();
    });
  });

  test.describe('Temp Badge', () => {
    test('should show remaining time in Temp badge', async ({ app }) => {
      await app.setUploadExpiration('1440'); // 24h
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      const clip = await app.getClipByFilename(path.basename(imagePath));
      const badge = clip.locator(selectors.expiration.badge);
      // Should show "Temp · 24h" or "Temp · 23h" (depending on timing)
      await expect(badge).toContainText(/Temp · \d+h/);
    });
  });

  test.describe('Bulk Expiration', () => {
    test('should set expiration on multiple clips', async ({ app }) => {
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFiles([img1, img2]);
      await app.expectClipCount(2);

      // Select all clips
      await app.page.locator(selectors.bulk.selectAllCheckbox).click();

      // Click bulk expiry button
      await app.page.locator(selectors.bulk.expiryButton).click();
      await app.page.waitForSelector(selectors.expiration.popover);
      await app.page.locator(selectors.expiration.popover).locator('button', { hasText: '1h' }).click();

      // Both clips should now have expiration badges
      await app.expectClipHasExpirationBadge(path.basename(img1));
      await app.expectClipHasExpirationBadge(path.basename(img2));
    });

    test('should cancel expiration on multiple clips', async ({ app }) => {
      // Upload with expiration
      await app.setUploadExpiration('15');
      const img1 = await createTempFile(generateTestImage(100, 100, 'red'), 'png');
      const img2 = await createTempFile(generateTestImage(100, 100, 'blue'), 'png');
      await app.uploadFiles([img1, img2]);
      await app.expectClipCount(2);

      // Reset upload dropdown
      await app.setUploadExpiration('0');

      // Select all clips
      await app.page.locator(selectors.bulk.selectAllCheckbox).click();

      // Cancel expiry button should be visible
      await expect(app.page.locator(selectors.bulk.cancelExpiryButton)).toBeVisible();
      await app.page.locator(selectors.bulk.cancelExpiryButton).click();

      // Badges should be gone
      await app.expectClipHasNoExpirationBadge(path.basename(img1));
      await app.expectClipHasNoExpirationBadge(path.basename(img2));
    });
  });
});
```

**Step 2: Run the tests to check they fail (backend/frontend not wired yet in dev)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Clip Expiration"`

Expected: Tests should fail initially since we haven't started dev mode yet; they will pass after full integration.

**Step 3: Commit**

```bash
cd /Users/egecan/Code/mahpastes && git add e2e/tests/clips/expiration.spec.ts
git commit -m "test: add e2e tests for expiration feature"
```

---

### Task 15: Update Docusaurus Docs

**Files:**
- Modify: `docs/docs/features/auto-delete.md`

**Step 1: Update the docs to remove "not yet implemented" warnings**

Replace the full content of `docs/docs/features/auto-delete.md` with updated content that documents the new UI:

- Remove the `:::warning Not Yet Implemented in UI` block
- Add sections for: upload expiration dropdown, context menu set/cancel, bulk operations, Temp badge with remaining time
- Update the "Canceling Expiration" section to document the context menu option
- Keep the existing sections on automatic cleanup, security considerations, and troubleshooting

**Step 2: Commit**

```bash
cd /Users/egecan/Code/mahpastes && git add docs/docs/features/auto-delete.md
git commit -m "docs: update auto-delete docs with expiration UI instructions"
```

---

### Task 16: Run Full E2E Test Suite

**Step 1: Start the dev server**

Run: `cd /Users/egecan/Code/mahpastes && make dev` (in background)

**Step 2: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

Expected: All tests pass, including the new expiration tests.

**Step 3: Fix any failures**

Address any test failures before proceeding.

**Step 4: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: address test failures from expiration feature"
```
