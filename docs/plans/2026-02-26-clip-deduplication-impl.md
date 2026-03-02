# Clip Deduplication Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add SHA-256 content hashing to clips with duplicate detection, visual badges, per-clip merge, and global deduplicate action.

**Architecture:** Add `content_hash` column to clips table, compute SHA-256 at all insert points, surface duplicate count via subquery in GetClips, and provide merge/deduplicate actions in both the card menu and sidebar nav.

**Tech Stack:** Go (crypto/sha256), SQLite migrations, vanilla JS frontend, Playwright e2e tests.

---

### Task 1: Database Migration — Add content_hash Column

**Files:**
- Modify: `database.go:90-92` (after existing migrations)

**Step 1: Add migration statements**

After line 92 in `database.go` (the `expires_at` migration), add:

```go
_, _ = db.Exec("ALTER TABLE clips ADD COLUMN content_hash TEXT DEFAULT ''")
_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_clips_content_hash ON clips(content_hash)")
```

**Step 2: Add backfill function**

Add a new function `backfillContentHashes(db *sql.DB)` in `database.go` that:
1. Queries `SELECT id, data FROM clips WHERE content_hash = ''`
2. For each row, computes `sha256.Sum256(data)` and hex-encodes it
3. Updates `UPDATE clips SET content_hash = ? WHERE id = ?`
4. Logs progress and continues on individual failures

```go
func backfillContentHashes(db *sql.DB) {
	rows, err := db.Query("SELECT id, data FROM clips WHERE content_hash = ''")
	if err != nil {
		log.Printf("Warning: failed to query clips for hash backfill: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int64
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			log.Printf("Warning: failed to scan clip %d for backfill: %v", id, err)
			continue
		}
		hash := sha256.Sum256(data)
		hashHex := hex.EncodeToString(hash[:])
		if _, err := db.Exec("UPDATE clips SET content_hash = ? WHERE id = ?", hashHex, id); err != nil {
			log.Printf("Warning: failed to update hash for clip %d: %v", id, err)
			continue
		}
		count++
	}
	if count > 0 {
		log.Printf("Backfilled content hashes for %d clips", count)
	}
}
```

**Step 3: Call backfill from initDB**

Call `backfillContentHashes(db)` at the end of `initDB()`, after all migrations.

**Step 4: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: Clean build

**Step 5: Commit**

```bash
git add database.go
git commit -m "feat: add content_hash column with backfill migration"
```

---

### Task 2: Compute Hash at All Insert Points

**Files:**
- Modify: `app.go:545-581` (UploadFileAndGetID)
- Modify: `app.go:583-634` (UploadFiles)
- Modify: `plugin/api_clips.go:216-299` (create)
- Modify: `plugin/api_clips.go:346-471` (createFromURL)

**Step 1: Add a helper function in app.go**

Add near the top of app.go (after imports):

```go
func computeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
```

Add `"crypto/sha256"` and `"encoding/hex"` to imports if not present.

**Step 2: Update UploadFileAndGetID (app.go:569)**

Before the INSERT, compute hash. Change the INSERT to include content_hash:

```go
contentHash := computeContentHash(data)
result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
    contentType, data, file.Name, contentHash)
```

After the insert, check for duplicates and emit event:

```go
clipID, _ := result.LastInsertId()

// Check for duplicates
var dupCount int
a.db.QueryRow("SELECT COUNT(*) FROM clips WHERE content_hash = ? AND id != ?", contentHash, clipID).Scan(&dupCount)
if dupCount > 0 {
    runtime.EventsEmit(a.ctx, "clip:duplicate", map[string]interface{}{
        "id":    clipID,
        "count": dupCount,
    })
}
```

**Step 3: Update UploadFiles (app.go:615)**

Same pattern — compute hash before INSERT, include in INSERT, check duplicates after:

```go
contentHash := computeContentHash(data)
result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename, expires_at, content_hash) VALUES (?, ?, ?, ?, ?)",
    contentType, data, file.Name, expiresAt, contentHash)
```

Add duplicate check after the existing plugin event emission block.

**Step 4: Update plugin/api_clips.go create (line 282)**

Add `computeContentHash` as a method or standalone function in the plugin package, or pass it through. Simplest: compute inline.

```go
hash := sha256.Sum256(data)
contentHash := hex.EncodeToString(hash[:])
result, err := c.db.Exec(
    "INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
    contentType, data, filename, contentHash,
)
```

**Step 5: Update plugin/api_clips.go createFromURL (line 454)**

Same pattern:

```go
hash := sha256.Sum256(data)
contentHash := hex.EncodeToString(hash[:])
result, err := c.db.Exec(
    "INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
    contentType, data, filename, contentHash,
)
```

**Step 6: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`

**Step 7: Commit**

```bash
git add app.go plugin/api_clips.go
git commit -m "feat: compute SHA-256 content hash at all clip insert points"
```

---

### Task 3: Add DuplicateCount to ClipPreview and GetClips Query

**Files:**
- Modify: `app.go:238-249` (ClipPreview struct)
- Modify: `app.go:376-413` (GetClips queries)
- Modify: `app.go:431` (Scan call)

**Step 1: Add DuplicateCount field to ClipPreview**

```go
type ClipPreview struct {
	ID             int64      `json:"id"`
	ContentType    string     `json:"content_type"`
	Filename       string     `json:"filename"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Preview        string     `json:"preview"`
	IsArchived     bool       `json:"is_archived"`
	Tags           []Tag      `json:"tags"`
	Size           int64      `json:"size"`
	DuplicateCount int        `json:"duplicate_count"`
}
```

**Step 2: Update all 3 query variants**

Add to each SELECT column list:
```sql
(SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.content_hash != '' AND c2.id != c.id)
```

For **query 1** (tag filters, line ~376), the SELECT becomes:
```sql
SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived, LENGTH(c.data),
       (SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.content_hash != '' AND c2.id != c.id)
```

For **query 2** (hidden tags, line ~396), same addition using `c.` prefix.

For **query 3** (no filters, line ~408), uses bare column names — need to alias the table or use a subquery with the table name:
```sql
SELECT c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived, LENGTH(c.data),
       (SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.content_hash != '' AND c2.id != c.id)
FROM clips c
WHERE c.is_archived = ? AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
ORDER BY c.created_at DESC
```

Note: Add `c2.content_hash != ''` to exclude unbackfilled clips from false-matching on empty string.

**Step 3: Update Scan call (line 431)**

```go
if err := rows.Scan(&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size, &clip.DuplicateCount); err != nil {
```

**Step 4: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`

**Step 5: Commit**

```bash
git add app.go
git commit -m "feat: add duplicate_count to ClipPreview query"
```

---

### Task 4: Frontend — Duplicate Badge on Cards

**Files:**
- Modify: `frontend/js/ui.js:711-731` (card footer in createClipCard)

**Step 1: Add badge HTML to card footer**

In the card footer area (around line 717), after the file type `<span>`, add a duplicate badge. Find the line with `getFriendlyFileType` and add the badge next to it:

```javascript
<span class="text-[9px] font-medium text-stone-400 uppercase tracking-wide">${getFriendlyFileType(clip.content_type, clip.filename)}</span>
${clip.duplicate_count > 0 ? `<span class="text-[9px] font-medium text-stone-400 bg-stone-100 border border-stone-200 rounded px-1">${clip.duplicate_count + 1} copies</span>` : ''}
```

Note: `duplicate_count + 1` because the count is "other copies" — total copies = count + self.

**Step 2: Verify visually**

Run: `make dev`
Upload the same file twice. Second card should show "2 copies" badge.

**Step 3: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: show duplicate count badge on clip cards"
```

---

### Task 5: Frontend — Duplicate Toast on Upload

**Files:**
- Modify: `frontend/js/app.js` (event listener setup)

**Step 1: Add Wails event listener for duplicate notification**

In `app.js`, in the event listener setup area, add:

```javascript
runtime.EventsOn('clip:duplicate', (data) => {
    showToast(`Duplicate clip detected — ${data.count} other ${data.count === 1 ? 'copy' : 'copies'} exist`, 'info');
});
```

**Step 2: Verify**

Upload the same file twice. Toast should appear on second upload.

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: show toast when duplicate clip is uploaded"
```

---

### Task 6: Backend — MergeDuplicates Method

**Files:**
- Modify: `app.go` (add new method)

**Step 1: Add MergeDuplicates method**

```go
func (a *App) MergeDuplicates(clipID int64) error {
	// Get the content_hash for this clip
	var contentHash string
	err := a.db.QueryRow("SELECT content_hash FROM clips WHERE id = ?", clipID).Scan(&contentHash)
	if err != nil {
		return fmt.Errorf("failed to get clip hash: %w", err)
	}
	if contentHash == "" {
		return fmt.Errorf("clip has no content hash")
	}

	// Find all clips with same hash, ordered by id (oldest first)
	rows, err := a.db.Query("SELECT id FROM clips WHERE content_hash = ? ORDER BY id ASC", contentHash)
	if err != nil {
		return fmt.Errorf("failed to find duplicates: %w", err)
	}
	var allIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			allIDs = append(allIDs, id)
		}
	}
	rows.Close()

	if len(allIDs) < 2 {
		return nil // No duplicates
	}

	survivorID := allIDs[0] // Oldest
	duplicateIDs := allIDs[1:]

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Merge tags from all duplicates to survivor
	for _, dupID := range duplicateIDs {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO clip_tags (clip_id, tag_id)
			SELECT ?, tag_id FROM clip_tags WHERE clip_id = ?
		`, survivorID, dupID)
		if err != nil {
			return fmt.Errorf("failed to merge tags from clip %d: %w", dupID, err)
		}
	}

	// Delete clip_tags for duplicates
	for _, dupID := range duplicateIDs {
		tx.Exec("DELETE FROM clip_tags WHERE clip_id = ?", dupID)
	}

	// Delete duplicate clips
	for _, dupID := range duplicateIDs {
		tx.Exec("DELETE FROM clips WHERE id = ?", dupID)
	}

	// Update survivor's created_at to now (moves to top)
	tx.Exec("UPDATE clips SET created_at = CURRENT_TIMESTAMP WHERE id = ?", survivorID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit merge: %w", err)
	}

	// Clean temp files for deleted clips
	a.deleteTempFilesForClipIDs(duplicateIDs)

	// Emit plugin events
	if a.pluginManager != nil {
		for _, dupID := range duplicateIDs {
			a.pluginManager.EmitEvent("clip:deleted", dupID)
		}
	}

	return nil
}
```

**Step 2: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`

**Step 3: Commit**

```bash
git add app.go
git commit -m "feat: add MergeDuplicates backend method"
```

---

### Task 7: Frontend — Merge Duplicates Card Action

**Files:**
- Modify: `frontend/js/ui.js:67-90` (builtInActions in renderCardMenu)
- Modify: `frontend/js/ui.js:35-52` (getMenuIcon)
- Modify: `frontend/js/ui.js:241-287` (handleCardAction)

**Step 1: Add merge icon to getMenuIcon**

Add to the icons object in `getMenuIcon`:

```javascript
'merge': '<path stroke-linecap="round" stroke-linejoin="round" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/>',
```

**Step 2: Add action to builtInActions array**

Before the delete action push (the last push), add conditionally:

```javascript
if (clip.duplicate_count > 0) {
    builtInActions.push({ id: 'merge-duplicates', label: 'Merge Duplicates', icon: 'merge' });
}
```

**Step 3: Add case in handleCardAction**

```javascript
case 'merge-duplicates':
    try {
        await window.go.main.App.MergeDuplicates(id);
        showToast(`Merged duplicates`, 'success');
        loadClips();
    } catch (err) {
        showToast('Failed to merge duplicates', 'error');
    }
    break;
```

**Step 4: Verify visually**

Upload same file twice, open card menu on either — "Merge Duplicates" should appear. Click it — duplicates should merge, gallery reloads, single clip remains at top.

**Step 5: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: add merge duplicates card action"
```

---

### Task 8: Backend — GetDuplicateGroups and DeduplicateAll

**Files:**
- Modify: `app.go` (add two new methods)

**Step 1: Add DuplicateGroup type**

```go
type DuplicateGroup struct {
	ContentHash string `json:"content_hash"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Count       int    `json:"count"`
	OldestID    int64  `json:"oldest_id"`
}
```

**Step 2: Add GetDuplicateGroups method**

```go
func (a *App) GetDuplicateGroups() ([]DuplicateGroup, error) {
	rows, err := a.db.Query(`
		SELECT content_hash, MIN(filename) as filename, MIN(content_type) as content_type, COUNT(*) as cnt, MIN(id) as oldest_id
		FROM clips
		WHERE content_hash != ''
		GROUP BY content_hash
		HAVING cnt > 1
		ORDER BY cnt DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query duplicate groups: %w", err)
	}
	defer rows.Close()

	var groups []DuplicateGroup
	for rows.Next() {
		var g DuplicateGroup
		var filename sql.NullString
		if err := rows.Scan(&filename, &g.Filename, &g.ContentType, &g.Count, &g.OldestID); err != nil {
			continue
		}
		g.ContentHash = filename.String
		// Fix: scan correctly
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []DuplicateGroup{}
	}
	return groups, nil
}
```

Wait — let me fix the Scan order. It should match the SELECT columns:

```go
if err := rows.Scan(&g.ContentHash, &g.Filename, &g.ContentType, &g.Count, &g.OldestID); err != nil {
```

And `Filename` may be NULL so use `sql.NullString`:

```go
var filename sql.NullString
if err := rows.Scan(&g.ContentHash, &filename, &g.ContentType, &g.Count, &g.OldestID); err != nil {
    continue
}
g.Filename = filename.String
```

**Step 3: Add DeduplicateAll method**

```go
func (a *App) DeduplicateAll() (int, error) {
	groups, err := a.GetDuplicateGroups()
	if err != nil {
		return 0, err
	}

	totalRemoved := 0
	for _, group := range groups {
		err := a.MergeDuplicates(group.OldestID)
		if err != nil {
			log.Printf("Warning: failed to merge group %s: %v", group.ContentHash, err)
			continue
		}
		totalRemoved += group.Count - 1
	}

	return totalRemoved, nil
}
```

**Step 4: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`

**Step 5: Commit**

```bash
git add app.go
git commit -m "feat: add GetDuplicateGroups and DeduplicateAll methods"
```

---

### Task 9: Frontend — Deduplicate Button in Sidebar + Confirmation Modal

**Files:**
- Modify: `frontend/index.html:162` (sidebar nav, between Clear All and Settings)
- Modify: `frontend/js/app.js` (click handler and dedup logic)
- Modify: `frontend/js/utils.js:6-25` (showConfirmDialog — support HTML message)

**Step 1: Add button to sidebar nav**

After the Clear All button's closing `</button>` and the `<div class="my-1 border-t border-stone-100"></div>` divider (line 162), add:

```html
<button id="deduplicate-btn"
    class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-500 text-xs font-medium py-2.5 px-3 rounded-md transition-colors flex items-center w-full"
    style="display: none;">
    <svg class="w-4 h-4 mr-2 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4" />
    </svg>
    Deduplicate
</button>
```

Hidden by default, shown when duplicates exist.

**Step 2: Update showConfirmDialog to support HTML**

In `utils.js`, change `messageEl.textContent = message` to `messageEl.innerHTML = message` so we can render a list of duplicate groups.

**Step 3: Add dedup logic in app.js**

```javascript
const deduplicateBtn = document.getElementById('deduplicate-btn');

deduplicateBtn.addEventListener('click', async () => {
    try {
        const groups = await window.go.main.App.GetDuplicateGroups();
        if (!groups || groups.length === 0) {
            showToast('No duplicates found');
            return;
        }

        const totalRemoved = groups.reduce((sum, g) => sum + (g.count - 1), 0);
        const listHTML = groups.map(g =>
            `<span class="block text-left">&middot; ${escapeHTML(g.filename || 'Untitled')} — ${g.count} copies (oldest kept, ${g.count - 1} removed)</span>`
        ).join('');

        const message = `<span class="block mb-2">${groups.length} duplicate group${groups.length > 1 ? 's' : ''} found:</span>` +
            `<span class="block text-[10px] text-stone-400 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>` +
            `<span class="block">Tags will be merged. ${totalRemoved} clip${totalRemoved > 1 ? 's' : ''} will be removed.</span>`;

        showConfirmDialog('Deduplicate All', message, async () => {
            try {
                const removed = await window.go.main.App.DeduplicateAll();
                showToast(`Deduplicated: removed ${removed} clip${removed !== 1 ? 's' : ''}`, 'success');
                loadClips();
                checkDuplicatesExist();
            } catch (err) {
                showToast('Failed to deduplicate', 'error');
            }
        });
    } catch (err) {
        showToast('Failed to check duplicates', 'error');
    }
});
```

**Step 4: Add helper to show/hide deduplicate button**

```javascript
async function checkDuplicatesExist() {
    try {
        const groups = await window.go.main.App.GetDuplicateGroups();
        deduplicateBtn.style.display = (groups && groups.length > 0) ? '' : 'none';
    } catch (e) {
        deduplicateBtn.style.display = 'none';
    }
}
```

Call `checkDuplicatesExist()` after `loadClips()` completes (at end of the loadClips function or after it's called).

**Step 5: Verify visually**

Upload duplicate files. Sidebar should show "Deduplicate" button. Click it — confirmation modal with list — confirm — clips merge.

**Step 6: Commit**

```bash
git add frontend/index.html frontend/js/app.js frontend/js/utils.js
git commit -m "feat: add deduplicate button in sidebar with confirmation modal"
```

---

### Task 10: E2E Tests

**Files:**
- Create: `e2e/tests/clips/deduplication.spec.ts`
- Modify: `e2e/fixtures/test-fixtures.ts` (add helper methods)
- Modify: `e2e/helpers/selectors.ts` (add selectors)

**Step 1: Add selectors**

In `selectors.ts`, add to the appropriate section:

```typescript
dedup: {
    badge: '.dedup-badge',
    deduplicateBtn: '#deduplicate-btn',
},
```

Also update the badge HTML in ui.js to include a class `dedup-badge` for testability.

**Step 2: Add test fixture helpers**

In `test-fixtures.ts`, add:

```typescript
async clickMergeDuplicatesInCardMenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    await this.page.locator('[data-action="merge-duplicates"]').click();
}

async getDuplicateBadgeText(filename: string): Promise<string | null> {
    const clip = await this.getClipByFilename(filename);
    const badge = clip.locator('.dedup-badge');
    if (await badge.count() === 0) return null;
    return badge.textContent();
}

async clickDeduplicateAll(): Promise<void> {
    await this.page.locator('#deduplicate-btn').click();
}
```

**Step 3: Write tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import * as path from 'path';

test.describe('Clip Deduplication', () => {
    test('should show duplicate badge when same content uploaded twice', async ({ app }) => {
        const imageData = generateTestImage(50, 50, [255, 0, 0]);
        const file1 = await createTempFile(imageData, 'png');
        const file2 = await createTempFile(imageData, 'png');

        await app.uploadFile(file1);
        await app.uploadFile(file2);
        await app.expectClipCount(2);

        // Both cards should show "2 copies" badge
        const badge1 = await app.getDuplicateBadgeText(path.basename(file1));
        const badge2 = await app.getDuplicateBadgeText(path.basename(file2));
        expect(badge1).toBe('2 copies');
        expect(badge2).toBe('2 copies');
    });

    test('should not show badge for unique clips', async ({ app }) => {
        const file1 = await createTempFile(generateTestImage(50, 50, [255, 0, 0]), 'png');
        const file2 = await createTempFile(generateTestImage(50, 50, [0, 255, 0]), 'png');

        await app.uploadFile(file1);
        await app.uploadFile(file2);

        const badge = await app.getDuplicateBadgeText(path.basename(file1));
        expect(badge).toBeNull();
    });

    test('should merge duplicates from card menu', async ({ app }) => {
        const imageData = generateTestImage(50, 50, [255, 0, 0]);
        const file1 = await createTempFile(imageData, 'png');
        const file2 = await createTempFile(imageData, 'png');

        await app.uploadFile(file1);
        await app.uploadFile(file2);
        await app.expectClipCount(2);

        await app.clickMergeDuplicatesInCardMenu(path.basename(file1));
        await app.expectClipCount(1);
    });

    test('should deduplicate all from sidebar', async ({ app }) => {
        const img1 = generateTestImage(50, 50, [255, 0, 0]);
        const img2 = generateTestImage(50, 50, [0, 255, 0]);

        // Create 2 duplicate groups
        await app.uploadFile(await createTempFile(img1, 'png'));
        await app.uploadFile(await createTempFile(img1, 'png'));
        await app.uploadFile(await createTempFile(img2, 'png'));
        await app.uploadFile(await createTempFile(img2, 'png'));
        await app.expectClipCount(4);

        await app.clickDeduplicateAll();
        await app.confirmDialog();
        await app.expectClipCount(2);
    });
});
```

**Step 4: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Deduplication"`
Expected: All 4 tests pass

**Step 5: Run full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass

**Step 6: Commit**

```bash
git add e2e/tests/clips/deduplication.spec.ts e2e/fixtures/test-fixtures.ts e2e/helpers/selectors.ts
git commit -m "test: add e2e tests for clip deduplication"
```
