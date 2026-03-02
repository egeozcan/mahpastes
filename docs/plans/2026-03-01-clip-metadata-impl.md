# Clip Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add string key-value metadata to clips, accessible via a modal from the card context menu and via a Lua plugin API.

**Architecture:** JSON column (`metadata TEXT DEFAULT '{}'`) on the `clips` table. Go methods handle read-modify-write with 50-pair limit enforcement. Frontend renders a centered modal with editable key-value rows. Plugin API module `metadata` provides get/set/delete/set_bulk.

**Tech Stack:** Go (SQLite, encoding/json), Vanilla JS + Tailwind CSS, Lua plugin API (gopher-lua), Playwright e2e tests.

---

### Task 1: Database Migration — Add metadata column

**Files:**
- Modify: `database.go:96-97` (after content_hash migration)

**Step 1: Add the migration**

After line 97 in `database.go` (the `idx_clips_content_hash` index creation), add:

```go
// Migrate: Add metadata column for key-value metadata (JSON)
_, _ = db.Exec("ALTER TABLE clips ADD COLUMN metadata TEXT DEFAULT '{}'")
```

**Step 2: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: No errors.

**Step 3: Commit**

```bash
git add database.go
git commit -m "feat: add metadata column migration to clips table"
```

---

### Task 2: Go Backend — ClipPreview struct and metadata CRUD methods

**Files:**
- Modify: `app.go:247-258` (ClipPreview struct)
- Modify: `app.go:68-96` (getClipPreview function)
- Modify: `app.go:443-491` (GetClips scan loop + batch load section)

**Step 1: Add Metadata field to ClipPreview**

In `app.go`, add `Metadata map[string]string` to the `ClipPreview` struct at line 258 (before the closing brace):

```go
type ClipPreview struct {
	ID             int64             `json:"id"`
	ContentType    string            `json:"content_type"`
	Filename       string            `json:"filename"`
	CreatedAt      time.Time         `json:"created_at"`
	ExpiresAt      *time.Time        `json:"expires_at"`
	Preview        string            `json:"preview"`
	IsArchived     bool              `json:"is_archived"`
	Tags           []Tag             `json:"tags"`
	Size           int64             `json:"size"`
	DuplicateCount int               `json:"duplicate_count"`
	Metadata       map[string]string `json:"metadata"`
}
```

**Step 2: Add metadata CRUD methods to app.go**

Add these four methods at the end of `app.go` (before the closing of the file). These are the Wails-exposed methods for the frontend:

```go
// GetClipMetadata returns all metadata key-value pairs for a clip
func (a *App) GetClipMetadata(clipID int64) (map[string]string, error) {
	var raw string
	err := a.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM clips WHERE id = ?", clipID).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return map[string]string{}, nil
	}
	return meta, nil
}

// SetClipMetadata sets a single metadata key-value pair on a clip (upsert)
func (a *App) SetClipMetadata(clipID int64, key string, value string) error {
	meta, err := a.GetClipMetadata(clipID)
	if err != nil {
		return err
	}
	if len(meta) >= 50 {
		if _, exists := meta[key]; !exists {
			return fmt.Errorf("metadata limit reached (max 50 pairs)")
		}
	}
	meta[key] = value
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	_, err = a.db.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(raw), clipID)
	return err
}

// DeleteClipMetadata removes a single metadata key from a clip
func (a *App) DeleteClipMetadata(clipID int64, key string) error {
	meta, err := a.GetClipMetadata(clipID)
	if err != nil {
		return err
	}
	delete(meta, key)
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	_, err = a.db.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(raw), clipID)
	return err
}

// SetClipMetadataBulk replaces all metadata on a clip
func (a *App) SetClipMetadataBulk(clipID int64, metadata map[string]string) error {
	if len(metadata) > 50 {
		return fmt.Errorf("metadata limit exceeded (max 50 pairs, got %d)", len(metadata))
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	_, err = a.db.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(raw), clipID)
	return err
}
```

**Step 3: Load metadata in getClipPreview**

In `app.go` `getClipPreview` (around line 75), add `metadata` to the SELECT and scan it:

Change the SELECT at line 75-78 from:
```go
err := a.db.QueryRow(`
    SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived, LENGTH(data)
    FROM clips WHERE id = ?`, id).Scan(
    &clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size)
```

To:
```go
var metadataRaw string
err := a.db.QueryRow(`
    SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived, LENGTH(data), COALESCE(metadata, '{}')
    FROM clips WHERE id = ?`, id).Scan(
    &clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size, &metadataRaw)
```

Then after the existing tag-loading code (around line 95), add:
```go
if err := json.Unmarshal([]byte(metadataRaw), &clip.Metadata); err != nil || clip.Metadata == nil {
    clip.Metadata = map[string]string{}
}
```

**Step 4: Load metadata in GetClips**

In the `GetClips` function, metadata needs to be added to all three SELECT query variants AND the scan loop.

For all three query strings (lines 394-405, 415-424, 428-434), add `COALESCE(c.metadata, '{}')` after `LENGTH(c.data)` and before the duplicate count subquery. Example for the simple query:

```sql
SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived, LENGTH(c.data), COALESCE(c.metadata, '{}'),
       (SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.content_hash != '' AND c2.id != c.id)
FROM clips c
```

In the scan loop (around line 452), add a `var metadataRaw string` variable and scan it:

```go
var metadataRaw string
if err := rows.Scan(&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size, &metadataRaw, &clip.DuplicateCount); err != nil {
```

Then after setting `clip.Tags = []Tag{}` (around line 470), add:

```go
if err := json.Unmarshal([]byte(metadataRaw), &clip.Metadata); err != nil || clip.Metadata == nil {
    clip.Metadata = map[string]string{}
}
```

**Step 5: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: No errors.

**Step 6: Regenerate frontend bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`
Expected: Generates updated bindings in `frontend/wailsjs/`.

**Step 7: Commit**

```bash
git add app.go frontend/wailsjs/
git commit -m "feat: add metadata CRUD methods and ClipPreview field"
```

---

### Task 3: Plugin API — metadata Lua module

**Files:**
- Create: `plugin/api_metadata.go`
- Modify: `plugin/manager.go:238-239` (after imageAPI registration)

**Step 1: Create the metadata API module**

Create `plugin/api_metadata.go`:

```go
package plugin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	lua "github.com/yuin/gopher-lua"
)

const maxMetadataPairs = 50

// MetadataAPI provides clip metadata operations to plugins
type MetadataAPI struct {
	db *sql.DB
}

// NewMetadataAPI creates a new metadata API instance
func NewMetadataAPI(db *sql.DB) *MetadataAPI {
	return &MetadataAPI{db: db}
}

// Register adds the metadata module to the Lua state
func (m *MetadataAPI) Register(L *lua.LState) {
	mod := L.NewTable()

	mod.RawSetString("get", L.NewFunction(m.get))
	mod.RawSetString("set", L.NewFunction(m.set))
	mod.RawSetString("delete", L.NewFunction(m.del))
	mod.RawSetString("set_bulk", L.NewFunction(m.setBulk))

	L.SetGlobal("metadata", mod)
}

// getMetadata is a helper that reads and parses the metadata JSON for a clip
func (m *MetadataAPI) getMetadata(clipID int64) (map[string]string, error) {
	var raw string
	err := m.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM clips WHERE id = ?", clipID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return map[string]string{}, nil
	}
	return meta, nil
}

// saveMetadata is a helper that serializes and writes metadata JSON for a clip
func (m *MetadataAPI) saveMetadata(clipID int64, meta map[string]string) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(raw), clipID)
	return err
}

// get returns all metadata key-value pairs for a clip as a Lua table
func (m *MetadataAPI) get(L *lua.LState) int {
	clipID := L.CheckInt64(1)

	meta, err := m.getMetadata(clipID)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	result := L.NewTable()
	for k, v := range meta {
		result.RawSetString(k, lua.LString(v))
	}

	L.Push(result)
	return 1
}

// set upserts a single key-value pair
func (m *MetadataAPI) set(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	key := L.CheckString(2)
	value := L.CheckString(3)

	meta, err := m.getMetadata(clipID)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	if len(meta) >= maxMetadataPairs {
		if _, exists := meta[key]; !exists {
			L.Push(lua.LFalse)
			L.Push(lua.LString(fmt.Sprintf("metadata limit reached (max %d pairs)", maxMetadataPairs)))
			return 2
		}
	}

	meta[key] = value
	if err := m.saveMetadata(clipID, meta); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// del removes a single key
func (m *MetadataAPI) del(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	key := L.CheckString(2)

	meta, err := m.getMetadata(clipID)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	delete(meta, key)
	if err := m.saveMetadata(clipID, meta); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// setBulk replaces all metadata at once from a Lua table
func (m *MetadataAPI) setBulk(L *lua.LState) int {
	clipID := L.CheckInt64(1)
	tbl := L.CheckTable(2)

	meta := make(map[string]string)
	tbl.ForEach(func(k lua.LValue, v lua.LValue) {
		if ks, ok := k.(lua.LString); ok {
			meta[string(ks)] = v.String()
		}
	})

	if len(meta) > maxMetadataPairs {
		L.Push(lua.LFalse)
		L.Push(lua.LString(fmt.Sprintf("metadata limit exceeded (max %d pairs, got %d)", maxMetadataPairs, len(meta))))
		return 2
	}

	if err := m.saveMetadata(clipID, meta); err != nil {
		log.Printf("metadata.set_bulk: failed: %v", err)
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}
```

**Step 2: Register in plugin manager**

In `plugin/manager.go`, after the `imageAPI` registration (line 239), add:

```go
metadataAPI := NewMetadataAPI(m.db)
metadataAPI.Register(sandbox.GetState())
```

**Step 3: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: No errors.

**Step 4: Commit**

```bash
git add plugin/api_metadata.go plugin/manager.go
git commit -m "feat: add metadata Lua plugin API module"
```

---

### Task 4: Frontend — Metadata modal HTML

**Files:**
- Modify: `frontend/index.html:839` (before settings modal)

**Step 1: Add metadata modal HTML**

Insert the following HTML before the Settings Modal comment (line 839) in `frontend/index.html`:

```html
<!-- Metadata Modal -->
<div id="metadata-modal" data-testid="metadata-modal" role="dialog" aria-modal="true" aria-labelledby="metadata-modal-title"
    class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-stone-900/40 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none">
    <div class="bg-white rounded-lg shadow-xl max-w-md w-full overflow-hidden transform transition-transform duration-200 scale-95">
        <div class="p-5 border-b border-stone-100 flex justify-between items-center">
            <h2 id="metadata-modal-title" class="text-sm font-semibold text-stone-800">Metadata</h2>
            <button id="metadata-close" data-testid="metadata-close" class="p-1 hover:bg-stone-100 rounded-md transition-colors">
                <svg class="w-5 h-5 text-stone-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
        <div id="metadata-list" data-testid="metadata-list" class="p-5 space-y-2 overflow-y-auto max-h-[50vh] min-h-[80px]">
            <!-- Rows inserted by JS -->
        </div>
        <div class="bg-stone-50 px-5 py-3 flex justify-between border-t border-stone-100">
            <button id="metadata-add" data-testid="metadata-add"
                class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">
                Add Field
            </button>
            <button id="metadata-save" data-testid="metadata-save"
                class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 px-5 rounded-md transition-colors">
                Save
            </button>
        </div>
    </div>
</div>
```

**Step 2: Add script tag for metadata.js**

In `frontend/index.html`, add the metadata script before `tags.js` (before line 1187):

```html
<script src="js/metadata.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add metadata modal HTML and script tag"
```

---

### Task 5: Frontend — metadata.js module

**Files:**
- Create: `frontend/js/metadata.js`

**Step 1: Create the metadata module**

Create `frontend/js/metadata.js`:

```javascript
// --- Metadata Module ---

const metadataModal = document.getElementById('metadata-modal');
const metadataCloseBtn = document.getElementById('metadata-close');
const metadataList = document.getElementById('metadata-list');
const metadataAddBtn = document.getElementById('metadata-add');
const metadataSaveBtn = document.getElementById('metadata-save');

let currentMetadataClipId = null;

function openMetadataModal(clipId) {
    currentMetadataClipId = clipId;
    metadataModal.classList.remove('opacity-0', 'pointer-events-none');
    metadataModal.classList.add('opacity-100');
    metadataModal.querySelector(':scope > div').classList.remove('scale-95');
    metadataModal.querySelector(':scope > div').classList.add('scale-100');
    loadMetadata(clipId);
}

function closeMetadataModal() {
    metadataModal.classList.add('opacity-0', 'pointer-events-none');
    metadataModal.classList.remove('opacity-100');
    metadataModal.querySelector(':scope > div').classList.add('scale-95');
    metadataModal.querySelector(':scope > div').classList.remove('scale-100');
    currentMetadataClipId = null;
}

async function loadMetadata(clipId) {
    metadataList.innerHTML = '';
    try {
        const meta = await window.go.main.App.GetClipMetadata(clipId);
        const entries = Object.entries(meta || {});
        if (entries.length === 0) {
            renderEmptyState();
        } else {
            entries.forEach(([key, value]) => renderMetadataRow(key, value));
        }
    } catch (err) {
        console.error('Failed to load metadata:', err);
        renderEmptyState();
    }
}

function renderEmptyState() {
    metadataList.innerHTML = `
        <p data-testid="metadata-empty" class="text-xs text-stone-400 text-center py-4">
            No metadata. Click 'Add Field' to get started.
        </p>`;
}

function renderMetadataRow(key, value) {
    // Remove empty state if present
    const emptyState = metadataList.querySelector('[data-testid="metadata-empty"]');
    if (emptyState) emptyState.remove();

    const row = document.createElement('div');
    row.className = 'flex items-center gap-2';
    row.dataset.testid = 'metadata-row';

    const keyInput = document.createElement('input');
    keyInput.type = 'text';
    keyInput.value = key;
    keyInput.placeholder = 'Key';
    keyInput.dataset.testid = 'metadata-key';
    keyInput.className = 'flex-[2] block border border-stone-200 rounded-md text-xs bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-1.5 px-2';

    const valueInput = document.createElement('input');
    valueInput.type = 'text';
    valueInput.value = value;
    valueInput.placeholder = 'Value';
    valueInput.dataset.testid = 'metadata-value';
    valueInput.className = 'flex-[3] block border border-stone-200 rounded-md text-xs bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-1.5 px-2';

    const deleteBtn = document.createElement('button');
    deleteBtn.dataset.testid = 'metadata-delete-row';
    deleteBtn.className = 'p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors flex-shrink-0';
    deleteBtn.innerHTML = `<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
    </svg>`;
    deleteBtn.addEventListener('click', () => {
        row.remove();
        if (metadataList.children.length === 0) renderEmptyState();
    });

    row.appendChild(keyInput);
    row.appendChild(valueInput);
    row.appendChild(deleteBtn);
    metadataList.appendChild(row);

    return keyInput;
}

function addMetadataRow() {
    const input = renderMetadataRow('', '');
    input.focus();
}

async function saveMetadata() {
    if (!currentMetadataClipId) return;

    const rows = metadataList.querySelectorAll('[data-testid="metadata-row"]');
    const meta = {};
    let hasDuplicate = false;

    rows.forEach(row => {
        const key = row.querySelector('[data-testid="metadata-key"]').value.trim();
        const value = row.querySelector('[data-testid="metadata-value"]').value;
        if (key) {
            if (meta.hasOwnProperty(key)) {
                hasDuplicate = true;
            }
            meta[key] = value;
        }
    });

    if (hasDuplicate) {
        showToast('Duplicate keys found — last value wins', 'error');
    }

    try {
        await window.go.main.App.SetClipMetadataBulk(currentMetadataClipId, meta);
        showToast('Metadata saved');
        closeMetadataModal();
    } catch (err) {
        console.error('Failed to save metadata:', err);
        showToast('Failed to save metadata', 'error');
    }
}

// Event listeners
metadataCloseBtn.addEventListener('click', closeMetadataModal);
metadataAddBtn.addEventListener('click', addMetadataRow);
metadataSaveBtn.addEventListener('click', saveMetadata);
metadataModal.addEventListener('click', (e) => {
    if (e.target === metadataModal) closeMetadataModal();
});
```

**Step 2: Commit**

```bash
git add frontend/js/metadata.js
git commit -m "feat: add metadata.js frontend module"
```

---

### Task 6: Frontend — Wire metadata into card menu and action handler

**Files:**
- Modify: `frontend/js/ui.js:127` (after tags push in builtInActions)
- Modify: `frontend/js/ui.js:78-92` (getMenuIcon icons object)
- Modify: `frontend/js/ui.js:308-315` (handleCardAction switch)

**Step 1: Add metadata icon**

In `frontend/js/ui.js`, in the `getMenuIcon` function's `icons` object (around line 83, after the `'tags'` entry), add:

```javascript
'metadata': '<path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>',
```

**Step 2: Add metadata action to builtInActions**

In `frontend/js/ui.js`, right after the `tags` push (line 127), add:

```javascript
builtInActions.push({ id: 'metadata', label: 'Metadata', icon: 'metadata' });
```

**Step 3: Add metadata case to handleCardAction**

In `frontend/js/ui.js`, in the `handleCardAction` switch (after the `'tags'` case block ending at line 315), add:

```javascript
case 'metadata':
    openMetadataModal(id);
    break;
```

**Step 4: Verify the app loads and the menu works**

Run: `cd /Users/egecan/Code/mahpastes && make dev`
Manual check: Upload a clip, click menu, verify "Metadata" appears between "Tags" and "Set Expiration". Click it, verify modal opens with empty state.

**Step 5: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: wire metadata action into card menu"
```

---

### Task 7: Regenerate Wails bindings

**Files:**
- Modify: `frontend/wailsjs/` (auto-generated)

**Step 1: Regenerate bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`
Expected: Updated binding files in `frontend/wailsjs/`.

**Step 2: Verify the new methods exist**

Check that `GetClipMetadata`, `SetClipMetadata`, `DeleteClipMetadata`, and `SetClipMetadataBulk` appear in the generated bindings.

**Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate wails bindings for metadata methods"
```

---

### Task 8: E2E Tests — Selectors and AppHelper methods

**Files:**
- Modify: `e2e/helpers/selectors.ts:81-85` (after cardMenu section)
- Modify: `e2e/fixtures/test-fixtures.ts` (add metadata helper methods)

**Step 1: Add metadata selectors**

In `e2e/helpers/selectors.ts`, add a new `metadata` section after the `cardMenu` section (around line 85):

```typescript
// Metadata modal
metadata: {
  modal: '[data-testid="metadata-modal"]',
  closeButton: '[data-testid="metadata-close"]',
  list: '[data-testid="metadata-list"]',
  addButton: '[data-testid="metadata-add"]',
  saveButton: '[data-testid="metadata-save"]',
  emptyState: '[data-testid="metadata-empty"]',
  row: '[data-testid="metadata-row"]',
  keyInput: '[data-testid="metadata-key"]',
  valueInput: '[data-testid="metadata-value"]',
  deleteRowButton: '[data-testid="metadata-delete-row"]',
},
```

Also add the metadata action to the `cardMenu` section (after the `mergeDuplicates` line):

```typescript
metadata: '.card-menu-dropdown [data-action="metadata"]',
```

**Step 2: Add AppHelper metadata methods**

In `e2e/fixtures/test-fixtures.ts`, add these methods to the `AppHelper` class (at the end, before the closing brace):

```typescript
// ==================== Metadata ====================

async openMetadataModal(clipFilename: string): Promise<void> {
  const card = this.page.locator(selectors.gallery.clipCardByName(clipFilename));
  await card.locator(selectors.clipActions.menuTrigger).click();
  await this.page.locator(selectors.cardMenu.metadata).click();
  await expect(this.page.locator(selectors.metadata.modal)).not.toHaveClass(/pointer-events-none/);
}

async closeMetadataModal(): Promise<void> {
  await this.page.locator(selectors.metadata.closeButton).click();
  await expect(this.page.locator(selectors.metadata.modal)).toHaveClass(/pointer-events-none/);
}

async addMetadataField(key: string, value: string): Promise<void> {
  await this.page.locator(selectors.metadata.addButton).click();
  const rows = this.page.locator(selectors.metadata.row);
  const lastRow = rows.last();
  await lastRow.locator(selectors.metadata.keyInput).fill(key);
  await lastRow.locator(selectors.metadata.valueInput).fill(value);
}

async saveMetadata(): Promise<void> {
  await this.page.locator(selectors.metadata.saveButton).click();
  await expect(this.page.locator(selectors.metadata.modal)).toHaveClass(/pointer-events-none/);
}

async expectMetadataRow(key: string, value: string): Promise<void> {
  const rows = this.page.locator(selectors.metadata.row);
  const count = await rows.count();
  let found = false;
  for (let i = 0; i < count; i++) {
    const rowKey = await rows.nth(i).locator(selectors.metadata.keyInput).inputValue();
    const rowValue = await rows.nth(i).locator(selectors.metadata.valueInput).inputValue();
    if (rowKey === key && rowValue === value) {
      found = true;
      break;
    }
  }
  expect(found).toBe(true);
}

async expectMetadataEmpty(): Promise<void> {
  await expect(this.page.locator(selectors.metadata.emptyState)).toBeVisible();
}

async expectMetadataRowCount(count: number): Promise<void> {
  await expect(this.page.locator(selectors.metadata.row)).toHaveCount(count);
}

async deleteMetadataRow(index: number): Promise<void> {
  const rows = this.page.locator(selectors.metadata.row);
  await rows.nth(index).locator(selectors.metadata.deleteRowButton).click();
}
```

**Step 3: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: add metadata selectors and AppHelper methods"
```

---

### Task 9: E2E Tests — Metadata test suite

**Files:**
- Create: `e2e/tests/metadata/metadata.spec.ts`

**Step 1: Create metadata test file**

Create the directory and test file:

```bash
mkdir -p e2e/tests/metadata
```

Create `e2e/tests/metadata/metadata.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';
import { generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Metadata', () => {
  test('should open metadata modal from card menu', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.expectClipCount(1);

    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should add a key-value pair and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('author', 'test-user');
    await app.saveMetadata();

    // Reopen and verify it persisted
    await app.openMetadataModal(filename);
    await app.expectMetadataRow('author', 'test-user');
    await app.expectMetadataRowCount(1);
    await app.closeMetadataModal();
  });

  test('should edit an existing value and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add initial metadata
    await app.openMetadataModal(filename);
    await app.addMetadataField('status', 'draft');
    await app.saveMetadata();

    // Edit the value
    await app.openMetadataModal(filename);
    const row = app.page.locator(selectors.metadata.row).first();
    await row.locator(selectors.metadata.valueInput).fill('published');
    await app.saveMetadata();

    // Verify edited value persisted
    await app.openMetadataModal(filename);
    await app.expectMetadataRow('status', 'published');
    await app.closeMetadataModal();
  });

  test('should delete a key-value pair and save', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add two fields
    await app.openMetadataModal(filename);
    await app.addMetadataField('key1', 'value1');
    await app.addMetadataField('key2', 'value2');
    await app.saveMetadata();

    // Delete first row
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(2);
    await app.deleteMetadataRow(0);
    await app.saveMetadata();

    // Verify only one remains
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(1);
    await app.closeMetadataModal();
  });

  test('should show empty state when no metadata exists', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should persist metadata after close and reopen', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('prompt', 'a beautiful sunset');
    await app.addMetadataField('model', 'flux-pro');
    await app.saveMetadata();

    // Reopen and check persistence
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(2);
    await app.expectMetadataRow('prompt', 'a beautiful sunset');
    await app.expectMetadataRow('model', 'flux-pro');
    await app.closeMetadataModal();
  });

  test('should show empty state after deleting all rows', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    // Add a field
    await app.openMetadataModal(filename);
    await app.addMetadataField('temp', 'data');
    await app.saveMetadata();

    // Delete it
    await app.openMetadataModal(filename);
    await app.deleteMetadataRow(0);
    await app.saveMetadata();

    // Verify empty state
    await app.openMetadataModal(filename);
    await app.expectMetadataEmpty();
    await app.closeMetadataModal();
  });

  test('should not save rows with empty keys', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openMetadataModal(filename);
    await app.addMetadataField('', 'orphan-value');
    await app.addMetadataField('valid-key', 'valid-value');
    await app.saveMetadata();

    // Only the valid key should persist
    await app.openMetadataModal(filename);
    await app.expectMetadataRowCount(1);
    await app.expectMetadataRow('valid-key', 'valid-value');
    await app.closeMetadataModal();
  });
});
```

**Step 2: Run the tests to verify they pass**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Metadata"`
Expected: All metadata tests pass.

**Step 3: Commit**

```bash
git add e2e/tests/metadata/
git commit -m "test: add e2e tests for clip metadata feature"
```

---

### Task 10: Run full test suite and fix any failures

**Step 1: Run all e2e tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass, including new metadata tests.

**Step 2: Fix any failures**

If tests fail, investigate and fix. Common issues:
- Backup/restore tests may need updating if they serialize clips (metadata column must be included)
- The `getClipPreview` scan may break if backup restore creates clips without the metadata column

**Step 3: Run tests again after fixes**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass.

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve test failures from metadata integration"
```
