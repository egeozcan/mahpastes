# Hidden Tags Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow users to hide clips with certain tags from the gallery, with the setting persisted in the database and overridable by explicit tag filters.

**Architecture:** Uses the existing `settings` table to store hidden tag IDs as JSON. `GetClips()` gets a third parameter for hidden tags and uses a LEFT JOIN anti-join to exclude them at the SQL level. Frontend caches hidden tags on startup, passes effective hidden tags (minus active filter overrides) on every `loadClips()` call. Settings modal gets a new section with per-tag toggles.

**Tech Stack:** Go (Wails backend), Vanilla JS, SQLite, Tailwind CSS, Playwright e2e tests

---

### Task 1: Add Go backend methods for hidden tags

**Files:**
- Modify: `app.go:1509-1529` (after existing GetSetting/SetSetting)

**Step 1: Add `GetHiddenTags` and `SetHiddenTags` methods**

Add after `SetSetting` (line 1529) in `app.go`:

```go
// GetHiddenTags returns the list of hidden tag IDs
func (a *App) GetHiddenTags() []int64 {
	value, err := a.GetSetting("hidden_tags")
	if err != nil || value == "" {
		return []int64{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(value), &ids); err != nil {
		log.Printf("Warning: failed to parse hidden_tags setting: %v", err)
		return []int64{}
	}
	return ids
}

// SetHiddenTags saves the list of hidden tag IDs
func (a *App) SetHiddenTags(ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal hidden tags: %w", err)
	}
	return a.SetSetting("hidden_tags", string(data))
}
```

**Step 2: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`

**Step 3: Commit**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat: add GetHiddenTags/SetHiddenTags backend methods"
```

---

### Task 2: Modify GetClips to accept and apply hiddenTags parameter

**Files:**
- Modify: `app.go:244-282` (GetClips method)

**Step 1: Update GetClips signature and add hidden tag filtering**

Change the `GetClips` method signature from:
```go
func (a *App) GetClips(archived bool, tagIDs []int64) ([]ClipPreview, error) {
```
to:
```go
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64) ([]ClipPreview, error) {
```

Then update the query-building logic. The override logic: remove any hiddenTagIDs that are also in tagIDs before applying the exclusion.

For the **no tag filters** path (lines 274-281), when `hiddenTagIDs` is non-empty, change to:

```go
	// Compute effective hidden tags (remove any that are in active filters)
	effectiveHidden := make([]int64, 0)
	tagIDSet := make(map[int64]bool)
	for _, id := range tagIDs {
		tagIDSet[id] = true
	}
	for _, id := range hiddenTagIDs {
		if !tagIDSet[id] {
			effectiveHidden = append(effectiveHidden, id)
		}
	}

	if len(tagIDs) > 0 {
		// Existing tag filter logic (AND logic)
		placeholders := make([]string, len(tagIDs))
		for i, tagID := range tagIDs {
			placeholders[i] = "?"
			args = append(args, tagID)
		}
		args = append(args, archivedInt, len(tagIDs))

		hiddenJoin := ""
		hiddenWhere := ""
		if len(effectiveHidden) > 0 {
			hiddenPlaceholders := make([]string, len(effectiveHidden))
			for i, id := range effectiveHidden {
				hiddenPlaceholders[i] = "?"
				args = append(args, id)
			}
			hiddenJoin = fmt.Sprintf("\n\t\tLEFT JOIN clip_tags ht ON c.id = ht.clip_id AND ht.tag_id IN (%s)", strings.Join(hiddenPlaceholders, ","))
			hiddenWhere = "\n\t\t  AND ht.clip_id IS NULL"
		}

		query = fmt.Sprintf(`
		SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived
		FROM clips c
		INNER JOIN clip_tags ct ON c.id = ct.clip_id%s
		WHERE ct.tag_id IN (%s)
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)%s
		GROUP BY c.id
		HAVING COUNT(DISTINCT ct.tag_id) = ?
		ORDER BY c.created_at DESC
		LIMIT %d`, hiddenJoin, strings.Join(placeholders, ","), hiddenWhere, defaultClipLimit)
	} else if len(effectiveHidden) > 0 {
		// No tag filters but has hidden tags - use LEFT JOIN anti-join
		hiddenPlaceholders := make([]string, len(effectiveHidden))
		for i, id := range effectiveHidden {
			hiddenPlaceholders[i] = "?"
			args = append(args, id)
		}
		args = append(args, archivedInt)

		query = fmt.Sprintf(`
		SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived
		FROM clips c
		LEFT JOIN clip_tags ht ON c.id = ht.clip_id AND ht.tag_id IN (%s)
		WHERE ht.clip_id IS NULL
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		ORDER BY c.created_at DESC
		LIMIT %d`, strings.Join(hiddenPlaceholders, ","), defaultClipLimit)
	} else {
		// No filters, no hidden tags - original simple query
		args = append(args, archivedInt)
		query = fmt.Sprintf(`
		SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived
		FROM clips
		WHERE is_archived = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY created_at DESC
		LIMIT %d`, defaultClipLimit)
	}
```

**Important:** The `args` must be built in the correct order matching the query placeholders. The hidden tag IDs go into the LEFT JOIN, then archived goes into the WHERE clause.

**Step 2: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`

**Step 3: Commit**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat: add hiddenTags parameter to GetClips with LEFT JOIN anti-join"
```

---

### Task 3: Add frontend hidden tags state and loadClips integration

**Files:**
- Modify: `frontend/js/app.js:49-65` (state declarations)
- Modify: `frontend/js/app.js:282-293` (window load handler)
- Modify: `frontend/js/app.js:69-89` (test helpers)
- Modify: `frontend/js/wails-api.js:4-6` (loadClips call)

**Step 1: Add `hiddenTags` state variable in `app.js`**

After line 64 (`let activeTagFilters = [];`), add:

```javascript
let hiddenTags = [];
```

**Step 2: Add test helpers for hiddenTags in `app.js`**

In the `Object.assign(window.__testHelpers, {...})` block (lines 70-89), add these entries:

```javascript
  setHiddenTags: (tags) => {
    hiddenTags.length = 0;
    hiddenTags.push(...tags);
  },
  getHiddenTags: () => hiddenTags,
```

**Step 3: Fetch hidden tags on startup in `app.js`**

In the `window.addEventListener('load', ...)` handler (line 282-293), add `await loadHiddenTags();` before `await loadClips();`. Add the function:

```javascript
async function loadHiddenTags() {
    try {
        hiddenTags = await window.go.main.App.GetHiddenTags();
    } catch (error) {
        console.error('Error loading hidden tags:', error);
        hiddenTags = [];
    }
}
```

**Step 4: Update `loadClips()` in `wails-api.js` to pass hidden tags**

Change line 6 from:
```javascript
const clips = await window.go.main.App.GetClips(isViewingArchive, activeTagFilters);
```
to:
```javascript
        // Compute effective hidden tags: remove any that are actively being filtered
        const effectiveHidden = hiddenTags.filter(id => !activeTagFilters.includes(id));
        const clips = await window.go.main.App.GetClips(isViewingArchive, activeTagFilters, effectiveHidden);
```

**Step 5: Commit**

```bash
git add frontend/js/app.js frontend/js/wails-api.js
git commit -m "feat: add hiddenTags frontend state and pass to GetClips"
```

---

### Task 4: Update fastReset in test fixtures

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts:523-677` (fastReset method)

**Step 1: Reset hidden tags in fastReset**

In the `fastReset()` method, after the block that deletes all tags (around line 560), add:

```typescript
      // Clear hidden tags setting
      try {
        if (App?.SetHiddenTags) {
          await App.SetHiddenTags([]);
        }
      } catch {}
```

And in the JS state reset section (around line 612), add after `helpers.setActiveTagFilters([])`:

```typescript
        if (helpers.setHiddenTags) helpers.setHiddenTags([]);
```

Also update the `getClipCountFromDB` method (line 135-142) to pass the third argument:

```typescript
  async getClipCountFromDB(archived: boolean = false): Promise<number> {
    return this.page.evaluate(async (isArchived) => {
      // @ts-ignore - Wails runtime
      const clips = await window.go.main.App.GetClips(isArchived, [], []);
      return clips?.length || 0;
    }, archived);
  }
```

And update `deleteAllClipsSafe` (line 424-443) to pass `[]` as the third arg:

```typescript
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], []);
      // @ts-ignore
      const archivedClips = await window.go.main.App.GetClips(true, [], []);
```

And in `fastReset` (lines 548-553):

```typescript
          const [clips, archived] = await Promise.all([
            App.GetClips(false, [], []),
            App.GetClips(true, [], []),
          ]);
```

And in `deleteAllClips` (lines 259-279):

```typescript
      // @ts-ignore
      const clips = await window.go.main.App.GetClips(false, [], []);
      // @ts-ignore
      const archivedClips = await window.go.main.App.GetClips(true, [], []);
```

And in `waitForReady` (line 61):

```typescript
      const tags = await window.go.main.App.GetTags();
```
(This one doesn't call GetClips, so no change needed there.)

**Step 2: Run tests to verify nothing is broken**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All existing tests pass (no regressions from the third parameter).

**Step 3: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts
git commit -m "fix: update test fixtures for GetClips third parameter"
```

---

### Task 5: Add Hidden Tags section to settings UI

**Files:**
- Modify: `frontend/index.html:740-763` (settings modal body)
- Modify: `frontend/js/settings.js` (add hidden tags rendering and toggle logic)

**Step 1: Add HTML container in settings modal**

In `frontend/index.html`, inside the settings modal `<div class="p-5 space-y-6">` (line 740), add before the Backup & Restore section (line 742):

```html
                <!-- Hidden Tags -->
                <div>
                    <h3 class="text-xs font-semibold text-stone-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.878 9.878L3 3m6.878 6.878L21 21"/>
                        </svg>
                        Hidden Tags
                    </h3>
                    <p class="text-[11px] text-stone-500 mb-3">
                        Clips with hidden tags won't appear in the gallery unless you explicitly filter by them.
                    </p>
                    <div id="hidden-tags-list" data-testid="hidden-tags-list">
                        <!-- Tags inserted by JS -->
                    </div>
                </div>
```

**Step 2: Add rendering logic in `settings.js`**

At the end of `settings.js`, add the hidden tags rendering and toggle functions:

```javascript
// --- Hidden Tags ---

const hiddenTagsList = document.getElementById('hidden-tags-list');

function renderHiddenTagsSettings() {
    if (!hiddenTagsList) return;

    hiddenTagsList.innerHTML = '';

    if (allTags.length === 0) {
        hiddenTagsList.innerHTML = '<p class="text-stone-400 text-xs">No tags yet</p>';
        return;
    }

    allTags.forEach(tag => {
        const isHidden = hiddenTags.includes(tag.id);
        const row = document.createElement('label');
        row.className = 'flex items-center justify-between py-2 px-1 cursor-pointer hover:bg-stone-50 rounded transition-colors';
        row.dataset.testid = `hidden-tag-row-${tag.name}`;
        row.innerHTML = `
            <div class="flex items-center gap-2">
                <span class="w-2.5 h-2.5 rounded-full flex-shrink-0" style="background-color: ${tag.color}"></span>
                <span class="text-xs font-medium text-stone-700">${escapeHtml(tag.name)}</span>
                <span class="text-[10px] text-stone-400">${tag.count}</span>
            </div>
            <div class="relative inline-flex items-center cursor-pointer">
                <input type="checkbox" class="sr-only peer" data-testid="hidden-tag-toggle-${tag.name}" ${isHidden ? 'checked' : ''}>
                <div class="w-8 h-4 bg-stone-300 rounded-full peer peer-checked:bg-stone-800 after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-3 after:w-3 after:transition-all peer-checked:after:translate-x-4"></div>
            </div>
        `;

        const checkbox = row.querySelector('input');
        checkbox.addEventListener('change', () => toggleHiddenTag(tag.id, checkbox.checked));

        hiddenTagsList.appendChild(row);
    });
}

async function toggleHiddenTag(tagId, hidden) {
    if (hidden) {
        if (!hiddenTags.includes(tagId)) {
            hiddenTags.push(tagId);
        }
    } else {
        const idx = hiddenTags.indexOf(tagId);
        if (idx !== -1) {
            hiddenTags.splice(idx, 1);
        }
    }

    try {
        await window.go.main.App.SetHiddenTags(hiddenTags);
    } catch (error) {
        console.error('Error saving hidden tags:', error);
        showToast('Failed to save hidden tags setting.');
    }
}
```

**Step 3: Call `renderHiddenTagsSettings()` when opening settings**

Modify the `openSettings()` function in `settings.js` to call `renderHiddenTagsSettings()`:

```javascript
function openSettings() {
    renderHiddenTagsSettings();
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    settingsModal.classList.add('opacity-100');
    settingsModal.querySelector(':scope > div').classList.remove('scale-95');
    settingsModal.querySelector(':scope > div').classList.add('scale-100');
}
```

**Step 4: Commit**

```bash
git add frontend/index.html frontend/js/settings.js
git commit -m "feat: add Hidden Tags section in settings modal"
```

---

### Task 6: Dim hidden tags in the filter dropdown

**Files:**
- Modify: `frontend/js/tags.js:35-66` (renderTagFilterDropdown)

**Step 1: Add dimming to hidden tags in filter dropdown**

In `renderTagFilterDropdown()` (line 45-65), update the `allTags.forEach` callback to check if a tag is hidden and apply visual distinction:

```javascript
    allTags.forEach(tag => {
        const isActive = activeTagFilters.includes(tag.id);
        const isHidden = hiddenTags.includes(tag.id);
        const item = document.createElement('label');
        item.className = `flex items-center gap-2 px-3 py-1.5 hover:bg-stone-100 cursor-pointer transition-colors${isHidden ? ' opacity-50' : ''}`;
        item.innerHTML = `
            <input type="checkbox"
                   data-testid="tag-checkbox-${tag.name}"
                   class="rounded border-stone-300 text-stone-600 focus:ring-stone-500"
                   ${isActive ? 'checked' : ''}>
            <span class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium text-white"
                  style="background-color: ${tag.color}">
                ${escapeHtml(tag.name)}
            </span>
            ${isHidden ? '<svg class="w-3 h-3 text-stone-400 ml-auto flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.878 9.878L3 3m6.878 6.878L21 21"/></svg>' : `<span class="text-stone-400 text-[10px] ml-auto">${tag.count}</span>`}
        `;

        const checkbox = item.querySelector('input');
        checkbox.addEventListener('change', () => toggleTagFilter(tag.id));

        tagFilterList.appendChild(item);
    });
```

**Step 2: Commit**

```bash
git add frontend/js/tags.js
git commit -m "feat: dim hidden tags in filter dropdown with eye-slash icon"
```

---

### Task 7: Reload gallery after closing settings

**Files:**
- Modify: `frontend/js/settings.js` (closeSettings function)

**Step 1: Reload clips when closing settings**

Hidden tag toggles take effect immediately in the database, but the gallery needs to refresh. Modify `closeSettings()`:

```javascript
function closeSettings() {
    settingsModal.classList.add('opacity-0', 'pointer-events-none');
    settingsModal.classList.remove('opacity-100');
    settingsModal.querySelector(':scope > div').classList.add('scale-95');
    settingsModal.querySelector(':scope > div').classList.remove('scale-100');
    // Refresh gallery and tag dropdown to reflect hidden tag changes
    renderTagFilterDropdown();
    loadClips();
}
```

**Step 2: Commit**

```bash
git add frontend/js/settings.js
git commit -m "feat: refresh gallery on settings close to apply hidden tags"
```

---

### Task 8: Add AppHelper methods for hidden tags testing

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts` (AppHelper class)
- Modify: `e2e/helpers/selectors.ts` (add hidden tags selectors)

**Step 1: Add selectors for hidden tags UI**

In `e2e/helpers/selectors.ts`, add to the `settings` object:

```typescript
  settings: {
    modal: '#settings-modal',
    closeButton: '#settings-close',
    hiddenTagsList: '[data-testid="hidden-tags-list"]',
    hiddenTagRow: (name: string) => `[data-testid="hidden-tag-row-${name}"]`,
    hiddenTagToggle: (name: string) => `[data-testid="hidden-tag-toggle-${name}"]`,
  },
```

**Step 2: Add AppHelper methods for hidden tags**

In `e2e/fixtures/test-fixtures.ts`, add to the AppHelper class in the Tags section:

```typescript
  async setHiddenTags(tagNames: string[]): Promise<void> {
    await this.page.evaluate(async (names) => {
      // @ts-ignore - Wails runtime
      const allTags = await window.go.main.App.GetTags();
      const ids: number[] = [];
      for (const name of names) {
        const tag = allTags.find((t: any) => t.name === name);
        if (tag) ids.push(tag.id);
      }
      // @ts-ignore
      await window.go.main.App.SetHiddenTags(ids);
      // Update frontend state
      // @ts-ignore
      if (window.__testHelpers?.setHiddenTags) {
        // @ts-ignore
        window.__testHelpers.setHiddenTags(ids);
      }
    }, tagNames);
  }

  async getHiddenTagNames(): Promise<string[]> {
    return this.page.evaluate(async () => {
      // @ts-ignore - Wails runtime
      const hiddenIds = await window.go.main.App.GetHiddenTags();
      // @ts-ignore
      const allTags = await window.go.main.App.GetTags();
      return hiddenIds
        .map((id: number) => allTags.find((t: any) => t.id === id)?.name)
        .filter(Boolean);
    });
  }

  async toggleHiddenTagInSettings(tagName: string): Promise<void> {
    const toggle = this.page.locator(`[data-testid="hidden-tag-toggle-${tagName}"]`);
    await toggle.click();
  }
```

**Step 3: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts e2e/helpers/selectors.ts
git commit -m "feat: add hidden tags test helpers and selectors"
```

---

### Task 9: Write e2e tests for hidden tags

**Files:**
- Create: `e2e/tests/tags/tag-hidden.spec.ts`

**Step 1: Write the test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Hidden Tags', () => {
  test('should hide clips with a hidden tag from gallery', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename1 = path.basename(image1);
    const filename2 = path.basename(image2);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.expectClipCount(2);

    await app.createTag('secret');
    await app.addTagToClip(filename1, 'secret');

    // Hide the tag
    await app.setHiddenTags(['secret']);
    await app.refreshClips();

    // Only the non-hidden clip should be visible
    await app.expectClipCount(1);
    await app.expectClipVisible(filename2);
  });

  test('should hide clip with both hidden and visible tags (hide wins)', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('visible');
    await app.createTag('hidden');
    await app.addTagToClip(filename1, 'visible');
    await app.addTagToClip(filename1, 'hidden');

    // Hide one of the tags
    await app.setHiddenTags(['hidden']);
    await app.refreshClips();

    // Clip should be hidden (hide wins)
    await app.expectClipCount(0);
  });

  test('should show hidden clips when explicitly filtering by hidden tag', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.createTag('private');
    await app.addTagToClip(filename1, 'private');

    // Hide the tag
    await app.setHiddenTags(['private']);
    await app.refreshClips();
    await app.expectClipCount(1);

    // Explicitly filter by the hidden tag - should override hiding
    await app.filterByTag('private');
    await app.expectClipCount(1);
    await app.expectClipVisible(filename1);
  });

  test('should persist hidden tag setting across page reloads', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const image2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');
    const filename2 = path.basename(image2);

    await app.uploadFile(image1);
    await app.uploadFile(image2);
    await app.createTag('persist-test');
    await app.addTagToClip(path.basename(image1), 'persist-test');

    // Hide the tag
    await app.setHiddenTags(['persist-test']);

    // Reload the page
    await app.page.reload();
    await app.waitForReady();

    // Hidden tags should still be in effect
    await app.expectClipCount(1);
    await app.expectClipVisible(filename2);
  });

  test('should toggle hidden tag in settings modal', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('toggle-test');
    await app.addTagToClip(filename1, 'toggle-test');

    // Reload to get fresh tag state in UI
    await app.page.reload();
    await app.waitForReady();
    await app.expectClipCount(1);

    // Open settings and toggle hidden
    await app.openSettingsModal();
    await app.toggleHiddenTagInSettings('toggle-test');
    await app.closeSettingsModal();

    // Clip should now be hidden
    await app.expectClipCount(0);

    // Toggle back
    await app.openSettingsModal();
    await app.toggleHiddenTagInSettings('toggle-test');
    await app.closeSettingsModal();

    // Clip should be visible again
    await app.expectClipCount(1);
  });

  test('should dim hidden tags in filter dropdown', async ({ app }) => {
    const image1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
    const filename1 = path.basename(image1);

    await app.uploadFile(image1);
    await app.createTag('dimmed');
    await app.addTagToClip(filename1, 'dimmed');

    // Hide the tag
    await app.setHiddenTags(['dimmed']);

    // Reload to get fresh UI
    await app.page.reload();
    await app.waitForReady();

    // Open filter dropdown
    await app.openTagFilterDropdown();

    // The hidden tag's label should have opacity-50 class
    const tagLabel = app.page.locator('[data-testid="tag-checkbox-dimmed"]').locator('..');
    await expect(tagLabel).toHaveClass(/opacity-50/);
  });
});
```

**Step 2: Run the tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Hidden Tags"`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add e2e/tests/tags/tag-hidden.spec.ts
git commit -m "test: add e2e tests for hidden tags feature"
```

---

### Task 10: Run full test suite and fix regressions

**Step 1: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass including existing ones.

**Step 2: Fix any failures**

Existing tests that call `GetClips` with only 2 args may fail since the signature now requires 3. The Wails binding should accept an optional third arg (JavaScript is lenient about missing args), but if any test explicitly calls `GetClips(false, [])` in `page.evaluate`, those need updating to `GetClips(false, [], [])`.

Look for these patterns in test-fixtures.ts and any spec files and add the third `[]` parameter.

**Step 3: Commit fixes**

```bash
git add -A
git commit -m "fix: update all GetClips call sites for third parameter"
```

---

### Task 11: Final verification and cleanup

**Step 1: Run full test suite one more time**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass.

**Step 2: Build the app**

Run: `cd /Users/egecan/Code/mahpastes && make build`
Expected: Clean build with no errors.

**Step 3: Commit if any final tweaks**

```bash
git add -A
git commit -m "chore: final cleanup for hidden tags feature"
```
