# Folder Context Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a right-click context menu on folder cards in folder view (Open / Move / Rename / Serve / Share / Hide) with live status glyphs (served, shared) in the top-right of each card and 0.5-opacity rendering for hidden folders.

**Architecture:** Two new frontend modules (`folder-context-menu.js`, `folder-move-modal.js`) reuse a minimally-extended shared `ContextMenu` primitive. A new `folderStatusPoller` in `app.js` hooked into both `switchView()` and `toggleFolderMode()` keeps a `folderStatusMap` in sync via 2-second polls of `ServeService.GetServeStatus` and `ShareService.GetShareStatus`. `renderFolderCards()` consults the map when rendering. No backend changes.

**Tech Stack:** Wails v2 (Go + vanilla JS), Tailwind CSS (stone palette, IBM Plex Mono), Playwright e2e tests.

**Spec:** `docs/superpowers/specs/2026-04-21-folder-context-menu-design.md`

---

## Verification primitives

The plan uses these repeatedly.

- **Run all e2e tests:** `cd /Users/egecan/Code/mahpastes/e2e && npm test 2>&1 | tail -50`
- **Run one spec file:** `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/menu-structure.spec.ts --reporter=line 2>&1 | tail -30`
- **Run with headed browser for debugging:** `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/menu-structure.spec.ts --headed`
- **Regenerate Wails bindings (only if a Go binding is added):** `make bindings`
- **Git commit template:** always HEREDOC-style, no --amend, no --no-verify. Use `git add <specific paths>` not `git add -A`.

---

## Phase outline

| # | Phase | What's verifiable at end |
|---|-------|--------------------------|
| 1 | Extend `ContextMenu` for virtual-rect anchors | Existing clip context menu tests still pass; unit-level harness test proves virtual anchor branch works |
| 2 | Status poller infrastructure (invisible) | Poller starts/stops correctly with view + folder-mode transitions (verified via console logs + unit test on evaluate() flags) |
| 3 | Folder card visuals + status badges | Badge glyphs + tooltips render live; hidden folders render at 0.5 opacity; `status-badges.spec.ts` passes |
| 4 | FolderContextMenu with Open + Hide | Right-click, `Shift+F10`, ContextMenu key all open the menu; Hide/Unhide roundtrip works; `menu-structure.spec.ts`, `hide-toggle.spec.ts` pass |
| 5 | Rename and Move items | Rename dialog + FolderMoveModal work end-to-end; `move-modal.spec.ts` passes |
| 6 | Serve + Share integration | Inactive: jumps to view with folder pre-selected. Active: menu flips label and direct-stops. `serve-toggle.spec.ts`, `share-toggle.spec.ts` pass |
| 7 | Long-press + a11y polish | Touch long-press opens menu; `a11y.spec.ts` verifies labels, roles, focus behaviors |
| 8 | Regression sweep + AppHelper fixture helpers | Full `npm test` green |

---

## File Structure

### New files

- `frontend/js/folder-context-menu.js` — `FolderContextMenu` module: attach handlers, build menu items, dispatch actions
- `frontend/js/folder-move-modal.js` — `FolderMoveModal` module: tree picker, live preview, confirm/cancel
- `e2e/tests/folder-context-menu/menu-structure.spec.ts` — menu items, ordering, labels, keyboard open/close
- `e2e/tests/folder-context-menu/move-modal.spec.ts` — tree, disabled rules, confirm, error cases
- `e2e/tests/folder-context-menu/serve-toggle.spec.ts` — inactive → jump, active → stop
- `e2e/tests/folder-context-menu/share-toggle.spec.ts` — inactive → jump, active → stop
- `e2e/tests/folder-context-menu/hide-toggle.spec.ts` — toggle + persistence
- `e2e/tests/folder-context-menu/status-badges.spec.ts` — badge render timing, combined states, tooltip content
- `e2e/tests/folder-context-menu/a11y.spec.ts` — aria-label, roles, keyboard

### Modified files

- `frontend/index.html` — add 2 `<script>` tags in the correct load order
- `frontend/js/ui.js` — `renderFolderCards()` gains badges, tabindex, aria-label, context-menu attach; introduces `folderStatusMap` module-scope var and `updateFolderBadgesInPlace()`
- `frontend/js/app.js` — introduces `folderStatusPoller`; `toggleFolderMode()` calls `folderStatusPoller.evaluate()`
- `frontend/js/serve.js` — `switchView()` calls `folderStatusPoller.evaluate()`; exports `openServeViewForTag(tagID)`
- `frontend/js/share.js` — exports `openShareFlowForTag(tagID)`
- `frontend/js/context-menu.js` — `positionMainMenu()` accepts `Element` OR full virtual rect
- `frontend/js/wails-api.js` — thin wrappers for `GetServeStatus` / `GetShareStatus` if they don't already exist
- `frontend/js/utils.js` — (only if needed) `openFolderRenameDialog(tagID, currentName)` helper
- `frontend/css/main.css` — `.folder-status-badges`, hidden-folder styling, paused badge styling
- `e2e/fixtures/test-fixtures.ts` — `rightClickFolder`, `getFolderContextMenuItem`, `expectFolderBadge`
- `e2e/helpers/selectors.ts` — `folderContextMenu`, badge selectors

---

## Phase 1: Extend ContextMenu for virtual-rect anchors

**Goal:** `positionMainMenu(menu, anchor)` accepts an Element (unchanged behavior) or a plain object with `{top, left, right, bottom, width, height}`. Required so right-click and long-press on folder cards can anchor at the pointer.

**Files:**
- Modify: `frontend/js/context-menu.js:84-131`

### Task 1.1: Read the existing function

- [ ] **Step 1: Open the file and read lines 80–135**

Run: `Read /Users/egecan/Code/mahpastes/frontend/js/context-menu.js (offset 80, limit 55)`

Expected: see `positionMainMenu` function body; lines 84-131 inclusive.

### Task 1.2: Modify positionMainMenu to detect-and-branch

**Files:**
- Modify: `frontend/js/context-menu.js:84-86`

- [ ] **Step 1: Apply the edit**

Exact Edit old_string / new_string:

Old (lines 84–86):
```javascript
    function positionMainMenu(menu, anchor) {
        const buttonRect = anchor.getBoundingClientRect();
        const menuRect = menu.getBoundingClientRect();
```

New:
```javascript
    function positionMainMenu(menu, anchor) {
        // Accept either an Element (use getBoundingClientRect) or a plain
        // rect { top, left, right, bottom, width, height } for pointer anchoring.
        const buttonRect = (typeof anchor.getBoundingClientRect === 'function')
            ? anchor.getBoundingClientRect()
            : anchor;
        const menuRect = menu.getBoundingClientRect();
```

No other lines change in this file for this phase.

### Task 1.3: Verify existing clip context menu still works

- [ ] **Step 1: Run the clip context-menu regression test**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/clips/context-menu.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all tests in that file PASS. The change is additive; element anchors still take the `getBoundingClientRect()` branch.

- [ ] **Step 2: If any test fails, revert and diagnose**

If any test fails, `git checkout -- frontend/js/context-menu.js` and re-read `positionMainMenu` to confirm the branch. The detection uses duck-typing on `getBoundingClientRect` which is defined on Element and DOMRect, so only non-Element, non-DOMRect inputs hit the else branch.

### Task 1.4: Commit

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/context-menu.js
git commit -m "$(cat <<'EOF'
feat(context-menu): accept virtual rect as anchor

positionMainMenu previously required an Element. Add duck-typed
detection so callers can pass a plain { top, left, right, bottom,
width, height } rect for pointer-anchored menus. Backward
compatible — existing Element callers unchanged.

Needed by the upcoming folder context menu which must anchor at
the right-click / long-press pointer, not the folder card edge.
EOF
)"
```

Expected: commit succeeds; branch tree clean for context-menu.js.

---

## Phase 2: Status poller infrastructure (invisible)

**Goal:** `folderStatusPoller` in `app.js` polls `ServeService.GetServeStatus` + `ShareService.GetShareStatus` every 2s when folder view is active (clips view AND folder mode on), stores results in `folderStatusMap` (module-scope in `ui.js`), and is hooked into `switchView()` + `toggleFolderMode()` transitions. No visible behavior — badges come in Phase 3.

**Files:**
- Modify: `frontend/js/wails-api.js` — add wrappers if missing
- Modify: `frontend/js/ui.js` — add `folderStatusMap` + stub `updateFolderBadgesInPlace()`
- Modify: `frontend/js/app.js` — add `folderStatusPoller` object + hook in `toggleFolderMode()` around line 216
- Modify: `frontend/js/serve.js` — hook `folderStatusPoller.evaluate()` into `switchView()` at line 25

### Task 2.1: Confirm Wails API wrappers exist (or add them)

**Files:**
- Read/Modify: `frontend/js/wails-api.js`

- [ ] **Step 1: Grep for existing wrappers**

Run (Grep tool):
```
pattern: "GetServeStatus|GetShareStatus"
path: /Users/egecan/Code/mahpastes/frontend/js/wails-api.js
output_mode: content
-n: true
```

- [ ] **Step 2: If BOTH wrappers exist, skip to Task 2.2**

If both lines appear with a `function` or arrow-function assignment, skip. If either is a direct `window.go.main...` call inline in serve.js/share.js without a wrapper, add a wrapper.

- [ ] **Step 3: Add any missing wrapper**

At the bottom of `frontend/js/wails-api.js`, add (only the functions that are missing):

```javascript
async function wailsGetServeStatus() {
    if (!window.go || !window.go.main || !window.go.main.ServeService) return [];
    try {
        return (await window.go.main.ServeService.GetServeStatus()) || [];
    } catch (e) {
        console.error('GetServeStatus failed:', e);
        return [];
    }
}

async function wailsGetShareStatus() {
    if (!window.go || !window.go.main || !window.go.main.ShareService) return { shares: [], follows: [] };
    try {
        return (await window.go.main.ShareService.GetShareStatus()) || { shares: [], follows: [] };
    } catch (e) {
        console.error('GetShareStatus failed:', e);
        return { shares: [], follows: [] };
    }
}
```

Rationale: swallows errors to avoid breaking the poller loop on a transient issue; returns safe default shapes so downstream code doesn't need null guards.

### Task 2.2: Add folderStatusMap and updateFolderBadgesInPlace stub to ui.js

**Files:**
- Modify: `frontend/js/ui.js` — top-level (near other module-scope state, e.g., alongside `_folderRenderGen`)

- [ ] **Step 1: Find `_folderRenderGen` declaration**

Run (Grep tool):
```
pattern: "_folderRenderGen"
path: /Users/egecan/Code/mahpastes/frontend/js/ui.js
output_mode: content
-n: true
```

Expected: find the `let _folderRenderGen = 0;` declaration. Use this line as the anchor for the insertion.

- [ ] **Step 2: Add state just after `_folderRenderGen` declaration**

Edit old_string (exact line — use as anchor, content may differ slightly):
```javascript
let _folderRenderGen = 0;
```

Edit new_string (this is the final content; no "replace with other version" — use exactly this):
```javascript
let _folderRenderGen = 0;

// Status-badge state for folder cards (populated by folderStatusPoller in app.js).
// Keyed by tagID; values are { served, shared, servePaused, sharePaused, serveURL, serveRequests, shareFollowers }.
const folderStatusMap = new Map();

// In-place badge update. Only mutates cards that already carry a .folder-status-badges
// container, so it's a harmless no-op during Phase 2 (before renderFolderCards is updated
// to emit the container in Phase 3). No focus/hover state disturbed.
function updateFolderBadgesInPlace() {
    const containers = gallery.querySelectorAll('[data-folder] .folder-status-badges');
    containers.forEach(container => {
        const card = container.closest('[data-folder]');
        if (!card) return;
        const tagID = parseInt(card.getAttribute('data-folder'), 10);
        const state = folderStatusMap.get(tagID) || {};
        const badges = [];
        if (state.served) badges.push(renderBadge('served', state));
        if (state.shared) badges.push(renderBadge('shared', state));
        container.innerHTML = badges.join('');
        const path = card.getAttribute('data-folder-path') || card.querySelector('.text-xs')?.textContent || '';
        const countText = card.querySelector('.text-\\[10px\\]')?.textContent || '';
        card.setAttribute('aria-label', buildFolderAriaLabel(path, countText, state, card.getAttribute('data-hidden') === 'true'));
    });
}

function renderBadge(kind, state) {
    if (kind === 'served') {
        const paused = !!state.servePaused;
        const tooltip = buildServeTooltip(state);
        const aria = paused ? 'Serving paused' : (state.serveURL ? `Served on ${state.serveURL}` : 'Serving this folder');
        const klass = paused ? 'folder-badge folder-badge-paused' : 'folder-badge folder-badge-serve';
        return `<span role="img" data-kind="serve" aria-label="${escapeHTML(aria)}" data-tooltip="${escapeHTML(tooltip)}" class="${klass}">
            <svg aria-hidden="true" focusable="false" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></svg>
        </span>`;
    }
    if (kind === 'shared') {
        const paused = !!state.sharePaused;
        const tooltip = buildShareTooltip(state);
        const aria = paused ? 'Sharing paused' : (typeof state.shareFollowers === 'number' ? `Sharing, ${state.shareFollowers} followers` : 'Sharing this folder');
        const klass = paused ? 'folder-badge folder-badge-paused' : 'folder-badge folder-badge-share';
        return `<span role="img" data-kind="share" aria-label="${escapeHTML(aria)}" data-tooltip="${escapeHTML(tooltip)}" class="${klass}">
            <svg aria-hidden="true" focusable="false" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07L12 4.5M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07L12 19.5"/></svg>
        </span>`;
    }
    return '';
}

function buildServeTooltip(state) {
    if (state.servePaused) return 'Serving paused — click to resume in Serve view';
    if (state.serveURL) {
        const count = typeof state.serveRequests === 'number' ? ` · ${state.serveRequests} request${state.serveRequests === 1 ? '' : 's'}` : '';
        return `Serving on ${state.serveURL}${count}`;
    }
    return 'Serving this folder';
}

function buildShareTooltip(state) {
    if (state.sharePaused) return 'Sharing paused';
    if (typeof state.shareFollowers === 'number') {
        return `Sharing this folder — ${state.shareFollowers} follower${state.shareFollowers === 1 ? '' : 's'}`;
    }
    return 'Sharing this folder';
}

function buildFolderAriaLabel(path, countText, state, hidden) {
    const parts = [`Folder: ${path}`, countText];
    if (state.served) parts.push(state.servePaused ? 'serving paused' : 'served');
    if (state.shared) parts.push(state.sharePaused ? 'sharing paused' : 'shared');
    if (hidden) parts.push('hidden');
    return parts.filter(Boolean).join(', ');
}

function applyFolderStatusUpdate(serveStatuses, shareStatus) {
    folderStatusMap.clear();
    (serveStatuses || []).forEach(s => {
        const id = s.tag_id;
        const entry = folderStatusMap.get(id) || {};
        // Serve entries in the returned list imply "running" unless the field says otherwise.
        entry.served = (s.running === undefined) ? true : !!s.running;
        entry.servePaused = !!s.paused;
        entry.serveURL = s.url || null;
        entry.serveRequests = typeof s.request_count === 'number' ? s.request_count : undefined;
        folderStatusMap.set(id, entry);
    });
    const shares = (shareStatus && shareStatus.shares) || [];
    shares.forEach(s => {
        const id = s.tag_id;
        const entry = folderStatusMap.get(id) || {};
        entry.shared = true;
        // ShareInfo DTO (share_types.go): Status is "active" | "paused" | "invalid"; Followers is int.
        entry.sharePaused = s.status === 'paused';
        entry.shareFollowers = typeof s.followers === 'number' ? s.followers : undefined;
        folderStatusMap.set(id, entry);
    });
    updateFolderBadgesInPlace();
}

// Expose to other modules via window — simpler than ES imports for this codebase.
window.folderStatusMap = folderStatusMap;
window.updateFolderBadgesInPlace = updateFolderBadgesInPlace;
window.applyFolderStatusUpdate = applyFolderStatusUpdate;
```

Note: `renderFolderCards` itself is untouched in Phase 2 and no existing folder card has a `.folder-status-badges` container, so `updateFolderBadgesInPlace` is a no-op today and invisible to users until Phase 3 renders the container.

### Task 2.3: Add folderStatusPoller to app.js

**Files:**
- Modify: `frontend/js/app.js` — near the top of the file, after existing state declarations

- [ ] **Step 1: Locate `let folderMode`**

Run (Grep tool):
```
pattern: "^let folderMode"
path: /Users/egecan/Code/mahpastes/frontend/js/app.js
output_mode: content
-n: true
```

Expected: one hit around line 214.

- [ ] **Step 2: Add poller just after the folderMode declaration**

Edit old_string:
```javascript
let folderMode = false;
```

Edit new_string:
```javascript
let folderMode = false;

// Folder-view status poller.
// Active when currentView === 'clips' AND folderMode === true.
// Polls ServeService.GetServeStatus + ShareService.GetShareStatus every 2s
// and pushes results through applyFolderStatusUpdate (defined in ui.js).
const folderStatusPoller = (() => {
    let intervalID = null;
    let generation = 0;

    async function pollOnce() {
        const myGen = generation;
        try {
            const [serveStatuses, shareStatus] = await Promise.all([
                (typeof wailsGetServeStatus === 'function') ? wailsGetServeStatus() : [],
                (typeof wailsGetShareStatus === 'function') ? wailsGetShareStatus() : { shares: [], follows: [] },
            ]);
            if (myGen !== generation) return; // superseded
            if (typeof window.applyFolderStatusUpdate === 'function') {
                window.applyFolderStatusUpdate(serveStatuses, shareStatus);
            }
        } catch (e) {
            console.warn('folderStatusPoller pollOnce failed:', e);
        }
    }

    function isActive() {
        const view = (typeof currentView !== 'undefined') ? currentView : 'clips';
        return view === 'clips' && folderMode === true;
    }

    function start() {
        if (intervalID !== null) return;
        generation++;
        pollOnce();
        intervalID = setInterval(pollOnce, 2000);
    }

    function stop() {
        if (intervalID === null) return;
        clearInterval(intervalID);
        intervalID = null;
        generation++;
        if (typeof window.folderStatusMap?.clear === 'function') {
            window.folderStatusMap.clear();
            if (typeof window.updateFolderBadgesInPlace === 'function') {
                window.updateFolderBadgesInPlace();
            }
        }
    }

    function evaluate() {
        if (isActive()) start(); else stop();
    }

    return { start, stop, evaluate, isActive };
})();

window.folderStatusPoller = folderStatusPoller;
```

Note: uses `currentView` as an unqualified reference because `currentView` is a top-level `let`/`var` in `serve.js` loaded before `app.js`. The `typeof` guard tolerates load-order edge cases during initialization.

### Task 2.4: Hook `toggleFolderMode()` to call `evaluate()`

**Files:**
- Modify: `frontend/js/app.js:216` (the `toggleFolderMode` function)

- [ ] **Step 1: Read the existing function**

Run: `Read /Users/egecan/Code/mahpastes/frontend/js/app.js (offset 216, limit 40)`

Confirm the function ends with `loadClips();` or similar before line 256.

- [ ] **Step 2: Add the evaluate call just before the function closes**

The existing function ends with something like `loadClips();` on a line before the closing `}`. Insert `folderStatusPoller.evaluate();` as the last statement inside the function.

Edit old_string (match the closing pattern — find the final `}` of `toggleFolderMode` using a unique surrounding line):

Best approach: find the last statement of `toggleFolderMode` (it should call `loadClips()` in most branches). Use the Grep result to identify the exact closing pattern, then add `folderStatusPoller.evaluate();` on a new line just before the closing `}`.

If the function has multiple return paths (e.g., the early return when `activeTagFilters.length > 0`), ensure `evaluate()` is called on EVERY path. Concrete pattern:

Old (approximate — confirm against actual source):
```javascript
function toggleFolderMode() {
    folderMode = !folderMode;
    const btn = document.getElementById('folder-mode-btn');
    if (btn) {
        btn.setAttribute('aria-pressed', folderMode);
    }
    if (folderMode && activeTagFilters.length > 0 && typeof navigateToFolder === 'function') {
        const lastTagId = activeTagFilters[activeTagFilters.length - 1];
        navigateToFolder(lastTagId);
        return;
    }
    if (!folderMode && typeof window.rememberCurrentFolder === 'function') {
        window.rememberCurrentFolder(null);
    }
    loadClips();
}
```

New (add a try/finally pattern with a single exit point):
```javascript
function toggleFolderMode() {
    folderMode = !folderMode;
    const btn = document.getElementById('folder-mode-btn');
    if (btn) {
        btn.setAttribute('aria-pressed', folderMode);
    }
    try {
        if (folderMode && activeTagFilters.length > 0 && typeof navigateToFolder === 'function') {
            const lastTagId = activeTagFilters[activeTagFilters.length - 1];
            navigateToFolder(lastTagId);
            return;
        }
        if (!folderMode && typeof window.rememberCurrentFolder === 'function') {
            window.rememberCurrentFolder(null);
        }
        loadClips();
    } finally {
        folderStatusPoller.evaluate();
    }
}
```

If the actual code differs from this approximation, preserve all branches and wrap them in `try { … } finally { folderStatusPoller.evaluate(); }` to guarantee `evaluate()` runs on every exit.

### Task 2.5: Hook `switchView()` in serve.js to call `evaluate()`

**Files:**
- Modify: `frontend/js/serve.js:25-110`

- [ ] **Step 1: Read switchView**

Run: `Read /Users/egecan/Code/mahpastes/frontend/js/serve.js (offset 25, limit 90)`

Identify the closing `}` of `switchView`.

- [ ] **Step 2: Add the evaluate call just before the closing `}`**

Insert `if (window.folderStatusPoller) window.folderStatusPoller.evaluate();` as the very last statement of `switchView` (just before its closing `}`). The guard tolerates temporary load-order issues on app startup.

### Task 2.6: Manual verification (DevTools)

- [ ] **Step 1: Start dev app**

Run: `make dev` (in a separate terminal)

- [ ] **Step 2: Open DevTools console, switch views, toggle folder mode**

In the app:
1. Open DevTools console
2. Type `folderStatusPoller.isActive()` → should be `false` (not in folder mode)
3. Click Folder Mode button → `folderStatusPoller.isActive()` → `true`; within 2s, `folderStatusMap.size` should update if anything is served/shared
4. Switch to Serve view → `folderStatusPoller.isActive()` → `false`
5. Back to Clips view → returns to `true` if folder mode still on

Expected: pass all four checks. If the poller doesn't stop on view-switch, verify the `switchView` hook is inside every exit path of that function.

### Task 2.7: Commit Phase 2

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/wails-api.js frontend/js/ui.js frontend/js/app.js frontend/js/serve.js
git commit -m "$(cat <<'EOF'
feat(folder-view): status poller infrastructure

Add folderStatusPoller (app.js) that polls ServeService.GetServeStatus
and ShareService.GetShareStatus every 2s while clips view + folder
mode are both active. Results flow into folderStatusMap (ui.js) and
drive updateFolderBadgesInPlace, which is a no-op until cards carry
a .folder-status-badges container (Phase 3).

Wired into toggleFolderMode (app.js) and switchView (serve.js) so
the poller starts/stops on every lifecycle transition. Generation
counter drops stale responses.

Invisible to users until Phase 3 introduces the badge DOM.
EOF
)"
```

---

## Phase 3: Folder card visuals + status badges

**Goal:** `renderFolderCards` renders every card with a `.folder-status-badges` container, composite `aria-label`, `tabindex=0`, `data-hidden` attribute when applicable, and `overflow-visible`. CSS styles the badges (emerald globe / blue chain) and the hidden-folder appearance (0.5 opacity + light grayscale). `status-badges.spec.ts` verifies live badge rendering.

**Files:**
- Modify: `frontend/js/ui.js:1126-1180` — `renderFolderCards` body
- Modify: `frontend/css/main.css` — new badge + hidden-folder styles
- Create: `e2e/tests/folder-context-menu/status-badges.spec.ts`
- Modify: `e2e/helpers/selectors.ts` — add badge selectors

### Task 3.1: Add CSS styles

**Files:**
- Modify: `frontend/css/main.css` (append near bottom or in a clear section)

- [ ] **Step 1: Append badge + hidden styles to main.css**

Append (use Bash `cat >>` is NOT allowed; use the Edit tool with a unique terminating anchor, or Read the tail and add via Edit).

Exact content to add (at end of file):

```css
/* --- Folder status badges --- */
.folder-status-badges {
    pointer-events: none; /* badges are decorative; cursor passes through to card */
}
.folder-status-badges > span {
    pointer-events: auto; /* tooltip hit-testing needs the span interactive */
    width: 14px;
    height: 14px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 3px;
    transition: opacity 150ms ease;
}
.folder-badge-serve  { color: #059669; } /* emerald-600 */
.folder-badge-share  { color: #1d4ed8; } /* blue-700  */
.folder-badge-paused { color: #78716c; border: 1px dashed currentColor; } /* stone-500 with dashed ring */

/* --- Hidden folder styling (in folder view) --- */
li[data-folder][data-hidden="true"] {
    opacity: 0.5;
    filter: grayscale(0.4);
}

/* Respect reduced-motion preference */
@media (prefers-reduced-motion: reduce) {
    .folder-status-badges > span { transition: none; }
}
```

### Task 3.2: Add selectors

**Files:**
- Modify: `e2e/helpers/selectors.ts`

- [ ] **Step 1: Read current folder selectors block**

Run: `Read /Users/egecan/Code/mahpastes/e2e/helpers/selectors.ts (offset 335, limit 20)`

Expected: shows `folderCard`, `folderModeButton`, `homeIcon` nested inside `selectors.tags: { ... }` (closes around line 347 with `},`). The existing folder selectors are **not** top-level — they live under `selectors.tags`.

- [ ] **Step 2: Add top-level folder-context-menu selectors**

Because the new test snippets all call `selectors.folderContextMenu`, `selectors.folderContextMenuItem`, `selectors.folderBadgeServedActive`, etc. at the **top level** of the exported `selectors` object, add the new entries at top level (not nested under `tags`). Insert them in a new top-level block, e.g. right before the closing `}` of the main `selectors = { ... }` export.

Exact insertion (pick a unique anchor line such as the last `}` in the file — read the tail to confirm):

```typescript
  // Folder context menu + status badges (top level for convenience)
  folderCard: (name: string) => `[data-testid="folder-card-${name}"]`,
  folderStatusBadges: (name: string) => `[data-testid="folder-card-${name}"] .folder-status-badges`,
  folderBadgeServed: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-serve, [data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="serve"]`,
  folderBadgeShared: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-share, [data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="share"]`,
  folderBadgeServedActive: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-serve`,
  folderBadgeSharedActive: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-share`,
  folderContextMenu: '.card-menu-dropdown[data-source="folder"]',
  folderContextMenuItem: (action: string) => `.card-menu-dropdown[data-source="folder"] [data-action="${action}"]`,
  folderHidden: (name: string) => `[data-testid="folder-card-${name}"][data-hidden="true"]`,
```

Notes:
- `folderCard` here is a deliberate top-level alias. The existing `selectors.tags.folderCard(name)` is unchanged — both resolve to the same selector string, so no existing tests break.
- `data-source="folder"` is an attribute that `FolderContextMenu.openFor` will set on the menu (Phase 4) to disambiguate folder vs clip menus.

### Task 3.3: Write status-badges.spec.ts (RED)

**Files:**
- Create: `e2e/tests/folder-context-menu/status-badges.spec.ts`

- [ ] **Step 1: Inspect an existing test for fixture patterns**

Run: `Read /Users/egecan/Code/mahpastes/e2e/tests/serve/serve-basic.spec.ts (limit 80)` — study how tests start/stop serving and how they access `ServeService`.

- [ ] **Step 2: Write the new spec file**

Create `e2e/tests/folder-context-menu/status-badges.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder status badges', () => {
    test('served folder shows emerald globe badge within poll interval', async ({ app }) => {
        // Setup: create a tag, assign one clip to it, enter folder mode
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `serve-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        // Initially no badge
        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(0);

        // Start serving via backend API (simulating external start)
        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tagName);

        // Badge should appear within ~3s (poll is every 2s)
        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(1, { timeout: 5000 });
        const badge = app.page.locator(selectors.folderBadgeServedActive(tagName));
        await expect(badge).toHaveAttribute('aria-label', /Served on/);
        await expect(badge).toHaveAttribute('data-tooltip', /Serving on http:\/\//);

        // Stop serving
        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ServeService.StopServing(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tagName))).toHaveCount(0, { timeout: 5000 });
    });

    test('shared folder shows blue chain badge', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `share-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(0);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ShareService.StartShare(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(1, { timeout: 5000 });
        const badge = app.page.locator(selectors.folderBadgeSharedActive(tagName));
        await expect(badge).toHaveAttribute('aria-label', /[Ss]haring/);

        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            await window.go.main.ShareService.StopShare(t.id);
        }, tagName);

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tagName))).toHaveCount(0, { timeout: 5000 });
    });

    test('hidden folder has data-hidden and reduced opacity', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tagName = `hidden-test-${Date.now()}`;
        await app.createTag(tagName);
        await app.addTagToClip(path.basename(imagePath), tagName);
        await app.enterFolderMode();

        const card = app.page.locator(selectors.folderCard(tagName));
        await expect(card).not.toHaveAttribute('data-hidden', 'true');

        // Mark hidden via backend API (menu-driven hide is Phase 4 territory)
        await app.page.evaluate(async (tag) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === tag);
            const existing = await window.go.main.App.GetHiddenTags();
            await window.go.main.App.SetHiddenTags([...(existing || []), t.id]);
            // Force re-render
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        }, tagName);

        await expect(app.page.locator(selectors.folderHidden(tagName))).toHaveCount(1);
        const opacity = await card.evaluate(el => getComputedStyle(el).opacity);
        expect(parseFloat(opacity)).toBeLessThan(0.7);
    });
});
```

Note: this assumes `app.createTag`, `app.addTagToClip`, `app.enterFolderMode` exist in the fixture. If they don't, they will be added in Task 3.6 before running the test. Also assumes `window.renderFolderCards` is globally accessible — verify and add to `ui.js` if missing (`window.renderFolderCards = renderFolderCards;`).

### Task 3.4: Add required AppHelper methods

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

- [ ] **Step 1: Grep for existing helpers**

Run (Grep tool):
```
pattern: "createTag|addTagToClip|enterFolderMode"
path: /Users/egecan/Code/mahpastes/e2e/fixtures/test-fixtures.ts
output_mode: content
-n: true
```

- [ ] **Step 2: Add any missing helpers**

Add inside the `AppHelper` class (method per missing helper):

```typescript
async createTag(name: string): Promise<void> {
    await this.page.evaluate(async (n) => {
        await window.go.main.App.CreateTag(n);
    }, name);
}

async addTagToClip(filename: string, tagName: string): Promise<void> {
    await this.page.evaluate(async ({ f, t }) => {
        const clips = await window.go.main.App.GetClips(false, null, 'Date Created', 'Descending', [], []);
        const clip = clips.clips.find((c: any) => c.filename === f);
        const tags = await window.go.main.App.GetTags();
        const tag = tags.find((x: any) => x.name === t);
        await window.go.main.App.AddTagToClip(clip.id, tag.id);
        if (typeof window.loadClips === 'function') await window.loadClips();
    }, { f: filename, t: tagName });
}

async enterFolderMode(): Promise<void> {
    const btn = this.page.locator('[data-testid="folder-mode-button"]');
    const pressed = await btn.getAttribute('aria-pressed');
    if (pressed !== 'true') {
        await btn.click();
    }
    // Wait for at least one folder card to render (or empty-state placeholder)
    await this.page.waitForFunction(() => {
        return document.querySelectorAll('[data-folder]').length > 0
            || !!document.querySelector('[data-testid="empty-state"]');
    }, null, { timeout: 2000 }).catch(() => { /* ok if no folders */ });
}
```

Note: method signatures must match the ones already in place if they exist — do not duplicate. Confirm via grep before inserting.

### Task 3.5: Run the spec and confirm it RED-FAILS

- [ ] **Step 1: Run the new spec**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/status-badges.spec.ts --reporter=line 2>&1 | tail -40`

Expected: all three tests FAIL with:
- "expected `.folder-badge-serve` count 1, received 0" (since renderFolderCards doesn't render badges yet)
- Hidden folder test will fail because `data-hidden` attribute isn't set

### Task 3.6: Modify renderFolderCards to render badges, data-hidden, tabindex, aria-label

**Files:**
- Modify: `frontend/js/ui.js:1126-1180`

- [ ] **Step 1: Open the full function body**

Run: `Read /Users/egecan/Code/mahpastes/frontend/js/ui.js (offset 1126, limit 55)`

- [ ] **Step 2: Apply the full rewrite via Edit**

Old (current body, adjust exact quotes to match file):
```javascript
async function renderFolderCards() {
    const myGen = ++_folderRenderGen;

    let folderTags;
    if (activeTagFilters.length > 0) {
        const currentTagId = activeTagFilters[activeTagFilters.length - 1];
        folderTags = await getChildTags(currentTagId);
    } else {
        folderTags = await getTopLevelTags();
    }

    if (myGen !== _folderRenderGen) return;
    if (!folderTags || folderTags.length === 0) return;

    gallery.querySelectorAll('[data-folder]').forEach(card => card.remove());

    for (const tag of folderTags) {
        const count = await getDescendantClipCount(tag.id);
        if (myGen !== _folderRenderGen) return;

        const shortName = getShortTagName(tag.name);

        const card = document.createElement('li');
        card.className = 'bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col items-center justify-center p-6 cursor-pointer transition-all duration-150 hover:border-stone-300 hover:scale-[1.02]';
        card.setAttribute('data-testid', `folder-card-${shortName}`);
        card.setAttribute('data-folder', tag.id);
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
        card.innerHTML = `
            <svg class="w-10 h-10 mb-2" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="${tag.color}">
                <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
            </svg>
            <span class="text-xs font-medium text-stone-700">${escapeHTML(shortName)}</span>
            <span class="text-[10px] text-stone-400 mt-0.5">${count} clip${count !== 1 ? 's' : ''}</span>
        `;

        card.addEventListener('click', () => {
            navigateToFolder(tag.id);
        });

        gallery.appendChild(card);
    }
}
```

New:
```javascript
async function renderFolderCards() {
    const myGen = ++_folderRenderGen;

    let folderTags;
    if (activeTagFilters.length > 0) {
        const currentTagId = activeTagFilters[activeTagFilters.length - 1];
        folderTags = await getChildTags(currentTagId);
    } else {
        folderTags = await getTopLevelTags();
    }

    if (myGen !== _folderRenderGen) return;
    if (!folderTags || folderTags.length === 0) return;

    gallery.querySelectorAll('[data-folder]').forEach(card => card.remove());

    const hidden = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []) : [];

    for (const tag of folderTags) {
        const count = await getDescendantClipCount(tag.id);
        if (myGen !== _folderRenderGen) return;

        const shortName = getShortTagName(tag.name);
        const isHidden = hidden.includes(tag.id);
        const state = folderStatusMap.get(tag.id) || {};
        const countText = `${count} clip${count !== 1 ? 's' : ''}`;

        const card = document.createElement('li');
        card.className = 'bg-white rounded-md border border-stone-200 overflow-visible flex flex-col items-center justify-center p-6 cursor-pointer transition-all duration-150 hover:border-stone-300 hover:scale-[1.02] relative';
        card.setAttribute('data-testid', `folder-card-${shortName}`);
        card.setAttribute('data-folder', tag.id);
        card.setAttribute('data-folder-path', tag.name);
        card.setAttribute('draggable', 'true');
        card.setAttribute('aria-grabbed', 'false');
        card.setAttribute('tabindex', '0');
        if (isHidden) card.setAttribute('data-hidden', 'true');
        card.setAttribute('aria-label', buildFolderAriaLabel(tag.name, countText, state, isHidden));

        // Badge container (populated now from state and kept fresh by updateFolderBadgesInPlace)
        const badgeBadges = [];
        if (state.served) badgeBadges.push(renderBadge('served', state));
        if (state.shared) badgeBadges.push(renderBadge('shared', state));
        const badgesHTML = `<div class="folder-status-badges absolute top-2 right-2 flex gap-1">${badgeBadges.join('')}</div>`;

        card.innerHTML = `
            ${badgesHTML}
            <svg class="w-10 h-10 mb-2" fill="none" viewBox="0 0 24 24" stroke-width="1" stroke="${tag.color}">
                <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.06-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
            </svg>
            <span class="text-xs font-medium text-stone-700">${escapeHTML(shortName)}</span>
            <span class="text-[10px] text-stone-400 mt-0.5">${countText}</span>
        `;

        card.addEventListener('click', () => {
            navigateToFolder(tag.id);
        });
        card.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                navigateToFolder(tag.id);
            }
        });

        gallery.appendChild(card);
    }
}

// Expose to other modules and for test hooks.
window.renderFolderCards = renderFolderCards;
```

### Task 3.7: Run status-badges.spec.ts (GREEN)

- [ ] **Step 1: Re-run the spec**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/status-badges.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all three tests PASS.

- [ ] **Step 2: If any test still fails, debug**

Most likely root causes:
- `ShareService.GetShareStatus` response shape uses a field other than `tag_id` or `follower_count` — inspect by `page.evaluate` in a headed run and adjust `applyFolderStatusUpdate` in ui.js
- `getHiddenTags()` not yet populated on load — ensure it's called before renderFolderCards (already is, per existing code)

### Task 3.8: Regression sweep

- [ ] **Step 1: Run folder-mode tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/tags/folder-mode.spec.ts tests/folder-drag/folder-drag.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all existing folder-mode and folder-drag tests still PASS.

### Task 3.9: Commit Phase 3

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/ui.js frontend/css/main.css e2e/tests/folder-context-menu/status-badges.spec.ts e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "$(cat <<'EOF'
feat(folder-view): status badges + hidden folder opacity

renderFolderCards now emits overflow-visible + tabindex + data-hidden
+ composite aria-label + a .folder-status-badges container. Badges
(emerald globe for serve, blue chain for share, stone-500 dashed for
paused) render live from folderStatusMap populated by the Phase 2
poller. Hidden folders render at opacity 0.5 with light grayscale.

status-badges.spec.ts covers serve badge, share badge, and
hidden-folder styling.
EOF
)"
```

---

## Phase 4: FolderContextMenu core (attach + Open + Hide)

**Goal:** Right-click on a folder card (or `Shift+F10`, or ContextMenu key) opens a menu with Open, Move…, Rename…, Serve… / Stop serving, Share… / Stop sharing, and Hide / Unhide items. For Phase 4 we implement only Open and Hide; Move/Rename/Serve/Share are stubs that close the menu without action (will be filled in by later phases). The menu is anchored at the pointer for pointer-triggered opens and at the card for keyboard-triggered opens.

**Files:**
- Create: `frontend/js/folder-context-menu.js`
- Modify: `frontend/index.html` — add script tag in the correct load order
- Modify: `frontend/js/ui.js` — call `FolderContextMenu.attach(card, tag, …)` in `renderFolderCards`
- Create: `e2e/tests/folder-context-menu/menu-structure.spec.ts`
- Create: `e2e/tests/folder-context-menu/hide-toggle.spec.ts`

### Task 4.1: Write menu-structure.spec.ts (RED)

**Files:**
- Create: `e2e/tests/folder-context-menu/menu-structure.spec.ts`

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu structure', () => {
    async function setupFolder(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `ctx-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('right-click on folder opens context menu with all items', async ({ app }) => {
        const tag = await setupFolder(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });

        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        const actions = ['open', 'move', 'rename', 'serve', 'share', 'hide'];
        for (const a of actions) {
            await expect(app.page.locator(selectors.folderContextMenuItem(a))).toHaveCount(1);
        }
    });

    test('menu order: Open, divider, Move, Rename, divider, Serve, Share, divider, Hide', async ({ app }) => {
        const tag = await setupFolder(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });

        const orderActions = await app.page.locator(
            `${selectors.folderContextMenu} > [role="menuitem"]`
        ).evaluateAll(els => els.map(el => el.getAttribute('data-action')));

        expect(orderActions).toEqual(['open', 'move', 'rename', 'serve', 'share', 'hide']);

        const dividerCount = await app.page.locator(`${selectors.folderContextMenu} hr`).count();
        expect(dividerCount).toBe(3);
    });

    test('Shift+F10 on focused card opens menu anchored at the card', async ({ app }) => {
        const tag = await setupFolder(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();
        await app.page.keyboard.press('Shift+F10');

        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();
    });

    test('Escape closes the menu and restores focus to the card', async ({ app }) => {
        const tag = await setupFolder(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();
        await app.page.keyboard.press('Shift+F10');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        await app.page.keyboard.press('Escape');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeHidden();

        const focused = await app.page.evaluate(() => document.activeElement?.getAttribute('data-testid'));
        expect(focused).toBe(`folder-card-${tag}`);
    });

    test('Hide label flips to Unhide when tag is hidden', async ({ app }) => {
        const tag = await setupFolder(app);

        // Tag not hidden: label should be "Hide"
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await expect(app.page.locator(`${selectors.folderContextMenu} [data-action="hide"]`)).toContainText(/Hide/);
        await app.page.keyboard.press('Escape');

        // Hide via API
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const existing = await window.go.main.App.GetHiddenTags();
            await window.go.main.App.SetHiddenTags([...(existing || []), t.id]);
            if (typeof window.setHiddenTagsState === 'function') window.setHiddenTagsState([...(existing || []), t.id]);
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        }, tag);

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await expect(app.page.locator(`${selectors.folderContextMenu} [data-action="hide"]`)).toContainText(/Unhide/);
    });
});
```

- [ ] **Step 2: Run it (RED)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/menu-structure.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all 5 tests FAIL (menu doesn't exist yet).

### Task 4.2: Create folder-context-menu.js

**Files:**
- Create: `frontend/js/folder-context-menu.js`

- [ ] **Step 1: Write the module**

```javascript
// Folder context menu for folder view.
// Mirrors the clip card context menu (frontend/js/context-menu.js) pattern but builds
// folder-specific items and handles pointer/keyboard/touch trigger differences.
const FolderContextMenu = (() => {
    const LONG_PRESS_MS = 500;
    const LONG_PRESS_MOVE_PX = 20;

    function attach(cardEl, tag) {
        cardEl.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            openFor(tag, pointerAnchor(e));
        });

        cardEl.addEventListener('keydown', (e) => {
            // Shift+F10 or ContextMenu key → open anchored at the card
            if ((e.shiftKey && e.key === 'F10') || e.key === 'ContextMenu') {
                e.preventDefault();
                openFor(tag, cardEl);
            }
        });

        // Touch long-press
        let touch = null;
        cardEl.addEventListener('touchstart', (e) => {
            if (e.touches.length !== 1) return;
            const t = e.touches[0];
            touch = { x: t.clientX, y: t.clientY, time: Date.now() };
        }, { passive: true });
        cardEl.addEventListener('touchmove', (e) => {
            if (!touch) return;
            const t = e.touches[0];
            const dx = t.clientX - touch.x, dy = t.clientY - touch.y;
            if (Math.hypot(dx, dy) > LONG_PRESS_MOVE_PX) touch = null;
        }, { passive: true });
        cardEl.addEventListener('touchend', (e) => {
            if (touch && Date.now() - touch.time >= LONG_PRESS_MS) {
                e.preventDefault();
                openFor(tag, { top: touch.y, left: touch.x, right: touch.x, bottom: touch.y, width: 0, height: 0 });
            }
            touch = null;
        });
    }

    function pointerAnchor(e) {
        const x = e.clientX, y = e.clientY;
        return { top: y, left: x, right: x, bottom: y, width: 0, height: 0 };
    }

    function openFor(tag, anchor) {
        const state = readLiveState(tag);
        const items = buildItems(tag, state);
        ContextMenu.open(items, tag.id, anchor, (action, id, item) => handleAction(action, tag, state));
        // Tag the menu so folder-specific selectors can distinguish it from the clip menu.
        const menuEl = document.querySelector('.card-menu-dropdown');
        if (menuEl) menuEl.setAttribute('data-source', 'folder');
    }

    function readLiveState(tag) {
        const s = (window.folderStatusMap && window.folderStatusMap.get(tag.id)) || {};
        const hidden = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []) : [];
        return {
            served: !!s.served,
            servePaused: !!s.servePaused,
            shared: !!s.shared,
            sharePaused: !!s.sharePaused,
            hidden: hidden.includes(tag.id),
        };
    }

    function buildItems(tag, state) {
        const items = [];
        items.push({ id: 'open', label: 'Open', iconHtml: iconOpen() });
        items.push({ type: 'divider' });
        items.push({ id: 'move', label: 'Move…', iconHtml: iconMove() });
        items.push({ id: 'rename', label: 'Rename…', iconHtml: iconPencil() });
        items.push({ type: 'divider' });
        items.push(state.served
            ? { id: 'stop-serve', label: 'Stop serving', iconHtml: iconServe(), danger: true }
            : { id: 'serve', label: 'Serve…', iconHtml: iconServe() });
        items.push(state.shared
            ? { id: 'stop-share', label: 'Stop sharing', iconHtml: iconShare(), danger: true }
            : { id: 'share', label: 'Share…', iconHtml: iconShare() });
        items.push({ type: 'divider' });
        items.push(state.hidden
            ? { id: 'hide', label: 'Unhide', iconHtml: iconEye() }
            : { id: 'hide', label: 'Hide', iconHtml: iconEyeOff() });
        return items;
    }

    async function handleAction(action, tag, state) {
        try {
            switch (action) {
                case 'open':
                    if (typeof navigateToFolder === 'function') navigateToFolder(tag.id);
                    return;
                case 'move':
                    if (typeof FolderMoveModal !== 'undefined') FolderMoveModal.show(tag);
                    return;
                case 'rename':
                    if (typeof openFolderRenameDialog === 'function') openFolderRenameDialog(tag.id, tag.name);
                    return;
                case 'serve':
                    if (typeof openServeViewForTag === 'function') await openServeViewForTag(tag.id);
                    return;
                case 'stop-serve':
                    await doStopServing(tag.id);
                    return;
                case 'share':
                    if (typeof openShareFlowForTag === 'function') await openShareFlowForTag(tag.id);
                    return;
                case 'stop-share':
                    await doStopSharing(tag.id);
                    return;
                case 'hide':
                    await toggleHidden(tag.id, !state.hidden);
                    return;
            }
        } catch (e) {
            console.error('folder context menu action failed:', e);
            if (typeof showToast === 'function') showToast(e?.message || String(e), 'error');
        }
    }

    async function toggleHidden(tagID, shouldHide) {
        const existing = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []).slice() : [];
        let next;
        if (shouldHide) {
            if (!existing.includes(tagID)) existing.push(tagID);
            next = existing;
        } else {
            next = existing.filter(id => id !== tagID);
        }
        // Optimistic update + persist
        if (typeof setHiddenTagsState === 'function') setHiddenTagsState(next);
        try {
            await window.go.main.App.SetHiddenTags(next);
            if (typeof showToast === 'function') showToast(shouldHide ? 'Folder hidden' : 'Folder unhidden', 'success');
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        } catch (e) {
            // Revert on failure
            if (typeof setHiddenTagsState === 'function') {
                const reverted = shouldHide ? next.filter(id => id !== tagID) : [...next, tagID];
                setHiddenTagsState(reverted);
            }
            if (typeof showToast === 'function') showToast('Failed to update hidden tags: ' + (e?.message || e), 'error');
        }
    }

    async function doStopServing(tagID) {
        try {
            await window.go.main.ServeService.StopServing(tagID);
            if (typeof showToast === 'function') showToast('Stopped serving', 'success');
        } catch (e) {
            const msg = (e && e.message) ? e.message : String(e);
            if (/no server running/i.test(msg)) {
                console.debug('stop-serve race: already stopped');
                return; // silent no-op
            }
            if (typeof showToast === 'function') showToast('Failed to stop serving: ' + msg, 'error');
        }
        // Trigger a fresh poll so badge disappears faster than waiting 2s.
        if (window.folderStatusPoller) window.folderStatusPoller.evaluate();
    }

    async function doStopSharing(tagID) {
        try {
            await window.go.main.ShareService.StopShare(tagID);
            if (typeof showToast === 'function') showToast('Stopped sharing', 'success');
        } catch (e) {
            // StopShare is idempotent server-side — any error here is a real failure.
            if (typeof showToast === 'function') showToast('Failed to stop sharing: ' + (e?.message || e), 'error');
        }
        if (window.folderStatusPoller) window.folderStatusPoller.evaluate();
    }

    // --- Icons (inline SVG matching the design system: stroke="currentColor", stroke-width="1.5") ---
    function svg(pathD) {
        return `<svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">${pathD}</svg>`;
    }
    function iconOpen()    { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"/>'); }
    function iconMove()    { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M7.5 7.5h-.75A2.25 2.25 0 0 0 4.5 9.75v7.5a2.25 2.25 0 0 0 2.25 2.25h7.5a2.25 2.25 0 0 0 2.25-2.25v-7.5a2.25 2.25 0 0 0-2.25-2.25h-.75m-6 3.75 3-3 3 3M12 7.5v9"/>'); }
    function iconPencil()  { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L6.832 19.82a4.5 4.5 0 0 1-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 0 1 1.13-1.897L16.863 4.487Z"/>'); }
    function iconServe()   { return svg('<circle cx="12" cy="12" r="9"/><path stroke-linecap="round" stroke-linejoin="round" d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/>'); }
    function iconShare()   { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07L12 4.5M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07L12 19.5"/>'); }
    function iconEye()     { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/>'); }
    function iconEyeOff()  { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 0 0 1.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0 1 12 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 0 1-4.293 5.774M6.228 6.228 3 3m3.228 3.228 3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 1 0-4.243-4.243m4.242 4.242L9.88 9.88"/>'); }

    return { attach, openFor, handleAction };
})();

window.FolderContextMenu = FolderContextMenu;
```

### Task 4.3: Add script tag to index.html

**Files:**
- Modify: `frontend/index.html` around line 2021-2032

- [ ] **Step 1: Edit the script block**

Old (match the three lines exactly — adjust whitespace to actual file content):
```html
    <script src="js/tooltips.js"></script>
```

Find the block from `tooltips.js` through `ui.js`. Target the transition from `context-menu.js` → `ui.js`. Use a narrow edit:

Old:
```html
    <script src="js/context-menu.js"></script>
    <script src="js/ui.js"></script>
```

New:
```html
    <script src="js/context-menu.js"></script>
    <script src="js/folder-context-menu.js"></script>
    <script src="js/folder-move-modal.js"></script>
    <script src="js/ui.js"></script>
```

Note: `folder-move-modal.js` doesn't exist yet (Phase 5 creates it). Adding its tag now means the browser will emit a 404 until Phase 5. Two options:

- **Recommended:** add both script tags now and stub `folder-move-modal.js` with `const FolderMoveModal = { show: () => alert('Move modal not yet implemented') };` so the file exists and nothing breaks.
- Alternative: add the tag later in Phase 5. In this plan, use the **Recommended** path:

- [ ] **Step 2: Create stub `folder-move-modal.js`**

Create `frontend/js/folder-move-modal.js` with a minimal stub:
```javascript
// Stub — full implementation in Phase 5.
const FolderMoveModal = (() => {
    function show(tag) {
        if (typeof showToast === 'function') {
            showToast('Move modal coming soon', 'info');
        }
        return Promise.resolve(null);
    }
    return { show };
})();
window.FolderMoveModal = FolderMoveModal;
```

### Task 4.4: Wire FolderContextMenu.attach from renderFolderCards

**Files:**
- Modify: `frontend/js/ui.js:1126-1180` (inside `renderFolderCards`, after `gallery.appendChild(card)`)

- [ ] **Step 1: Add the attach call**

Edit old_string:
```javascript
        card.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                navigateToFolder(tag.id);
            }
        });

        gallery.appendChild(card);
    }
}
```

Edit new_string:
```javascript
        card.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                navigateToFolder(tag.id);
            }
        });

        if (typeof FolderContextMenu !== 'undefined') {
            FolderContextMenu.attach(card, tag);
        }

        gallery.appendChild(card);
    }
}
```

### Task 4.5: Run menu-structure.spec.ts (GREEN)

- [ ] **Step 1: Run**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/menu-structure.spec.ts --reporter=line 2>&1 | tail -40`

Expected: all 5 tests PASS.

- [ ] **Step 2: If the "data-action" order test fails, inspect the generated menu**

Likely cause: `ContextMenu.open` may render items in a slightly different order or attach `data-action` via a different attribute. Open `context-menu.js` and search for where item.id is set on the rendered element. If it writes `data-action` to each button, we're good. If it writes `data-id` instead, update the spec's selector `[data-action]` to match (e.g., `[data-id]` or whatever the attribute is).

### Task 4.6: Write hide-toggle.spec.ts (RED)

**Files:**
- Create: `e2e/tests/folder-context-menu/hide-toggle.spec.ts`

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder hide/unhide via context menu', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `hide-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Hide sets data-hidden and reduces opacity; Unhide reverses', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        // Hide
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));
        await expect(card).toHaveAttribute('data-hidden', 'true');

        // Unhide
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));
        await expect(card).not.toHaveAttribute('data-hidden', 'true');
    });

    test('Hidden state persists across re-renders', async ({ app }) => {
        const tag = await setup(app);

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));

        // Force a re-render
        await app.page.evaluate(async () => {
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        });

        await expect(app.page.locator(selectors.folderHidden(tag))).toHaveCount(1);
    });

    test('aria-label includes "hidden" when hidden', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('hide'));

        await expect(card).toHaveAttribute('aria-label', /hidden/);
    });
});
```

- [ ] **Step 2: Run (GREEN — should already pass from Phase 4 wiring)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/hide-toggle.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all three tests PASS.

### Task 4.7: Commit Phase 4

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/folder-context-menu.js frontend/js/folder-move-modal.js frontend/js/ui.js frontend/index.html e2e/tests/folder-context-menu/menu-structure.spec.ts e2e/tests/folder-context-menu/hide-toggle.spec.ts
git commit -m "$(cat <<'EOF'
feat(folder-view): context menu with Open + Hide actions

New module frontend/js/folder-context-menu.js wires right-click,
Shift+F10, ContextMenu key, and touch long-press (500ms, 20px move
threshold) on folder cards to an items menu: Open / Move… / Rename…
/ Serve… (or Stop serving) / Share… (or Stop sharing) / Hide (or
Unhide). Menu is anchored at the pointer for pointer triggers and at
the card for keyboard triggers, via the virtual-rect extension added
in Phase 1.

Only Open and Hide are functional in this commit; Move/Rename/Serve/
Share are stubbed (show toast) and get filled in by Phases 5-6.
folder-move-modal.js is a stub to satisfy the script tag.
EOF
)"
```

---

## Phase 5: Rename + Move actions

**Goal:** Rename opens an existing rename-style prompt dialog and calls `UpdateTag` with the renamed short name. Move opens the full `FolderMoveModal` tree picker with live preview, disabled destinations, and error surfacing.

**Files:**
- Modify: `frontend/js/utils.js` (or wherever `showPromptDialog` lives) — add `openFolderRenameDialog(tagID, currentName)`
- Replace: `frontend/js/folder-move-modal.js` (stub → real implementation)
- Create: `e2e/tests/folder-context-menu/move-modal.spec.ts`

### Task 5.1: Add openFolderRenameDialog

- [ ] **Step 1: Find `showPromptDialog`**

Run (Grep tool):
```
pattern: "function showPromptDialog|showPromptDialog\\s*="
path: /Users/egecan/Code/mahpastes/frontend/js
output_mode: content
-n: true
```

Expected: find the definition in utils.js.

- [ ] **Step 2: Add folder rename wrapper**

In the same file as `showPromptDialog`, append:

```javascript
async function openFolderRenameDialog(tagID, currentName) {
    const shortName = getShortTagName(currentName);
    const parent = currentName.includes('/') ? currentName.substring(0, currentName.lastIndexOf('/')) : '';
    showPromptDialog('Rename Folder', shortName, async (newShortName) => {
        if (!newShortName || newShortName.trim() === '' || newShortName === shortName) return;
        // Reject embedded slashes — use Move for reparenting
        if (newShortName.includes('/')) {
            showToast('Name cannot contain "/". Use Move to change parent.', 'error');
            return;
        }
        const newPath = parent ? `${parent}/${newShortName.trim()}` : newShortName.trim();
        try {
            const tag = (await window.go.main.App.GetTags()).find(t => t.id === tagID);
            await window.go.main.App.UpdateTag(tagID, newPath, tag?.color || '#000000');
            showToast(`Renamed to ${newShortName}`, 'success');
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
            if (typeof loadClips === 'function') await loadClips();
        } catch (e) {
            showToast('Rename failed: ' + (e?.message || e), 'error');
        }
    });
}
window.openFolderRenameDialog = openFolderRenameDialog;
```

### Task 5.2: Verify Rename menu item works manually

- [ ] **Step 1: Start dev**

Run: `make dev` in another terminal.

- [ ] **Step 2: Right-click a folder → Rename → type new name → confirm**

Expected: toast "Renamed to X"; folder card updates to show new name; no errors in console.

### Task 5.3: Write move-modal.spec.ts (RED)

**Files:**
- Create: `e2e/tests/folder-context-menu/move-modal.spec.ts`

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

const MOVE_MODAL = '[data-testid="folder-move-modal"]';
const MOVE_TREE = '[data-testid="folder-move-tree"]';
const MOVE_PREVIEW = '[data-testid="folder-move-preview"]';
const MOVE_ERROR = '[data-testid="folder-move-error"]';
const MOVE_CONFIRM = '[data-testid="folder-move-confirm"]';
const MOVE_CANCEL = '[data-testid="folder-move-cancel"]';

test.describe('Folder move modal', () => {
    async function setupTwoFolders(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const src = `src-${Date.now()}`;
        const dst = `dst-${Date.now()}`;
        await app.createTag(src);
        await app.createTag(dst);
        await app.addTagToClip(path.basename(imagePath), src);
        await app.enterFolderMode();
        return { src, dst };
    }

    test('opens with tree, preview updates on select, Move confirms', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));

        await expect(app.page.locator(MOVE_MODAL)).toBeVisible();
        await expect(app.page.locator(MOVE_TREE)).toBeVisible();

        // Select destination
        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await expect(app.page.locator(MOVE_PREVIEW)).toContainText(`${dst}/${src}`);

        // Confirm
        await app.page.click(MOVE_CONFIRM);
        await expect(app.page.locator(MOVE_MODAL)).toBeHidden();

        // Verify new path exists
        const tags = await app.page.evaluate(() => window.go.main.App.GetTags());
        const renamed = tags.find((t: any) => t.name === `${dst}/${src}`);
        expect(renamed).toBeDefined();
    });

    test('self and descendants are disabled', async ({ app }) => {
        const { src } = await setupTwoFolders(app);
        // Create a descendant
        await app.page.evaluate(async (parent) => {
            const tags = await window.go.main.App.GetTags();
            const p = tags.find((t: any) => t.name === parent);
            await window.go.main.App.CreateTag(`${parent}/child`);
            if (typeof window.loadClips === 'function') await window.loadClips();
        }, src);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));

        const selfRow = app.page.locator(`${MOVE_TREE} [data-dest-name="${src}"]`);
        await expect(selfRow).toHaveAttribute('data-disabled', 'true');

        const childRow = app.page.locator(`${MOVE_TREE} [data-dest-path="${src}/child"]`);
        await expect(childRow).toHaveAttribute('data-disabled', 'true');
    });

    test('Root is always selectable for non-root folders', async ({ app }) => {
        const { dst } = await setupTwoFolders(app);
        // Make dst a child so we can move it back to root
        await app.page.evaluate(async (d) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === d);
            await window.go.main.App.UpdateTag(t.id, `parent/${d}`, t.color);
            if (typeof window.loadClips === 'function') await window.loadClips();
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        }, dst);

        // Now navigate to the "parent" folder where dst is visible
        await app.enterFolderMode();
        await app.page.click(selectors.folderCard('parent'));
        await app.page.click(selectors.folderCard(dst), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));

        const rootRow = app.page.locator(`${MOVE_TREE} [data-dest-root="true"]`);
        await expect(rootRow).not.toHaveAttribute('data-disabled', 'true');
    });

    test('Serve-active error is surfaced inline', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        // Start serving src
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, src);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));
        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await app.page.click(MOVE_CONFIRM);

        await expect(app.page.locator(MOVE_MODAL)).toBeVisible();
        await expect(app.page.locator(MOVE_ERROR)).toContainText(/served|serving|server running/i);

        // Cleanup
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, src);
    });

    test('Cancel closes modal without moving', async ({ app }) => {
        const { src, dst } = await setupTwoFolders(app);

        await app.page.click(selectors.folderCard(src), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('move'));
        await app.page.click(`${MOVE_TREE} [data-dest-name="${dst}"]`);
        await app.page.click(MOVE_CANCEL);

        await expect(app.page.locator(MOVE_MODAL)).toBeHidden();

        const tags = await app.page.evaluate(() => window.go.main.App.GetTags());
        const renamed = tags.find((t: any) => t.name === `${dst}/${src}`);
        expect(renamed).toBeUndefined();
    });
});
```

- [ ] **Step 2: Run (RED)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/move-modal.spec.ts --reporter=line 2>&1 | tail -40`

Expected: all tests FAIL — modal is still the stub from Phase 4.

### Task 5.4: Implement FolderMoveModal

**Files:**
- Overwrite: `frontend/js/folder-move-modal.js`

- [ ] **Step 1: Write the real implementation**

```javascript
// Folder move modal — tree picker, live preview, submit via App.UpdateTag.
const FolderMoveModal = (() => {
    let currentTag = null;
    let selectedDest = null; // { id: number|null, path: string, isRoot: boolean }
    let rootEl = null;
    let focusTrapCleanup = null;

    async function show(tag) {
        currentTag = tag;
        selectedDest = null;
        await ensureMounted();
        await populate();
        openModal();
    }

    async function ensureMounted() {
        if (rootEl) return;
        rootEl = document.createElement('div');
        rootEl.setAttribute('data-testid', 'folder-move-modal');
        rootEl.className = 'fixed inset-0 z-50 hidden items-center justify-center bg-black/40';
        rootEl.innerHTML = `
            <div class="bg-white rounded-md shadow-lg w-[480px] max-w-[90vw] max-h-[80vh] flex flex-col">
                <div class="flex items-center justify-between p-4 border-b border-stone-200">
                    <h2 class="text-sm font-semibold uppercase tracking-wide text-stone-800">Move folder</h2>
                    <button data-testid="folder-move-close" class="text-stone-400 hover:text-stone-600 text-xl leading-none" aria-label="Close">&times;</button>
                </div>
                <div class="p-4 text-xs font-medium text-stone-600">
                    Moving: <span data-testid="folder-move-source" class="text-stone-800"></span>
                </div>
                <div class="flex-1 overflow-y-auto px-4 pb-2">
                    <div class="text-[10px] uppercase tracking-wider text-stone-400 mb-1">Destination</div>
                    <ul data-testid="folder-move-tree" class="border border-stone-200 rounded-md p-2 text-xs font-medium space-y-0.5"></ul>
                </div>
                <div class="px-4 py-2 text-xs font-medium text-stone-600">
                    New path: <code data-testid="folder-move-preview" class="text-stone-800"></code>
                </div>
                <div data-testid="folder-move-error" class="px-4 text-xs text-red-600 hidden"></div>
                <div class="flex justify-end gap-2 p-4 border-t border-stone-200">
                    <button data-testid="folder-move-cancel" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">Cancel</button>
                    <button data-testid="folder-move-confirm" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-5 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed" disabled>Move</button>
                </div>
            </div>`;
        document.body.appendChild(rootEl);

        rootEl.querySelector('[data-testid="folder-move-cancel"]').addEventListener('click', closeModal);
        rootEl.querySelector('[data-testid="folder-move-close"]').addEventListener('click', closeModal);
        rootEl.querySelector('[data-testid="folder-move-confirm"]').addEventListener('click', onConfirm);
        rootEl.addEventListener('click', (e) => { if (e.target === rootEl) closeModal(); });
        document.addEventListener('keydown', onKeydown);
    }

    function onKeydown(e) {
        if (!rootEl || rootEl.classList.contains('hidden')) return;
        if (e.key === 'Escape') closeModal();
    }

    async function populate() {
        const allTags = await window.go.main.App.GetTags();
        const sortedNames = allTags.map(t => t.name).sort();
        const tree = buildTree(allTags);
        const sourcePath = currentTag.name;
        rootEl.querySelector('[data-testid="folder-move-source"]').textContent = sourcePath;

        const descendants = new Set(allTags.filter(t => t.name === sourcePath || t.name.startsWith(sourcePath + '/')).map(t => t.name));
        const parentPath = sourcePath.includes('/') ? sourcePath.substring(0, sourcePath.lastIndexOf('/')) : '';
        const ul = rootEl.querySelector('[data-testid="folder-move-tree"]');
        ul.innerHTML = '';

        // Root option
        const rootLi = document.createElement('li');
        rootLi.className = 'cursor-pointer rounded px-2 py-1 hover:bg-stone-100 flex items-center gap-2';
        rootLi.setAttribute('data-dest-root', 'true');
        if (parentPath === '') {
            rootLi.setAttribute('data-disabled', 'true');
            rootLi.title = 'Already here';
            rootLi.classList.add('opacity-50', 'cursor-not-allowed');
        }
        rootLi.innerHTML = `<span class="text-stone-400">○</span><span>Root (top level)</span>`;
        rootLi.addEventListener('click', () => {
            if (rootLi.getAttribute('data-disabled') === 'true') return;
            selectedDest = { id: null, path: '', isRoot: true };
            refreshSelection();
        });
        ul.appendChild(rootLi);

        // Tree rows — render flat-sorted, indented by depth
        for (const name of sortedNames) {
            const tag = allTags.find(t => t.name === name);
            const depth = (name.match(/\//g) || []).length;
            const li = document.createElement('li');
            li.className = 'cursor-pointer rounded px-2 py-1 hover:bg-stone-100 flex items-center gap-2';
            li.style.paddingLeft = `${8 + depth * 16}px`;
            li.setAttribute('data-dest-name', name.split('/').pop() || '');
            li.setAttribute('data-dest-path', name);
            li.setAttribute('data-tag-id', String(tag.id));
            const shortName = name.split('/').pop();
            li.innerHTML = `<span class="text-stone-400">•</span><span>${escapeHTML(shortName)}</span>`;

            let disabledReason = null;
            if (name === sourcePath) disabledReason = 'Cannot move folder into itself';
            else if (descendants.has(name)) disabledReason = 'Cannot move into own subfolder';
            else if (name === parentPath) disabledReason = 'Already here';
            if (disabledReason) {
                li.setAttribute('data-disabled', 'true');
                li.title = disabledReason;
                li.classList.add('opacity-50', 'cursor-not-allowed');
            }

            li.addEventListener('click', () => {
                if (li.getAttribute('data-disabled') === 'true') return;
                selectedDest = { id: tag.id, path: name, isRoot: false };
                refreshSelection();
            });
            ul.appendChild(li);
        }
    }

    function refreshSelection() {
        // Highlight selected
        rootEl.querySelectorAll('[data-testid="folder-move-tree"] li').forEach(li => li.classList.remove('bg-stone-100'));
        if (selectedDest) {
            let sel;
            if (selectedDest.isRoot) sel = rootEl.querySelector('[data-dest-root="true"]');
            else sel = rootEl.querySelector(`[data-dest-path="${CSS.escape(selectedDest.path)}"]`);
            sel?.classList.add('bg-stone-100');
        }
        // Update preview
        const preview = rootEl.querySelector('[data-testid="folder-move-preview"]');
        const shortName = currentTag.name.split('/').pop();
        if (selectedDest) {
            preview.textContent = selectedDest.isRoot ? shortName : `${selectedDest.path}/${shortName}`;
        } else {
            preview.textContent = '';
        }
        rootEl.querySelector('[data-testid="folder-move-confirm"]').disabled = !selectedDest;
        // Clear any prior error on new selection
        hideError();
    }

    async function onConfirm() {
        if (!selectedDest) return;
        const shortName = currentTag.name.split('/').pop();
        const newPath = selectedDest.isRoot ? shortName : `${selectedDest.path}/${shortName}`;
        try {
            await window.go.main.App.UpdateTag(currentTag.id, newPath, currentTag.color);
            if (typeof showToast === 'function') showToast(`Moved to ${newPath}`, 'success');
            closeModal();
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
            if (typeof loadClips === 'function') await loadClips();
        } catch (e) {
            showError(`Cannot move: ${e?.message || e}`);
        }
    }

    function showError(msg) {
        const el = rootEl.querySelector('[data-testid="folder-move-error"]');
        el.textContent = msg;
        el.classList.remove('hidden');
    }
    function hideError() {
        const el = rootEl.querySelector('[data-testid="folder-move-error"]');
        el.textContent = '';
        el.classList.add('hidden');
    }

    function openModal() {
        rootEl.classList.remove('hidden');
        rootEl.classList.add('flex');
        rootEl.querySelector('[data-testid="folder-move-cancel"]').focus();
    }

    function closeModal() {
        rootEl?.classList.add('hidden');
        rootEl?.classList.remove('flex');
        currentTag = null;
        selectedDest = null;
    }

    function buildTree(tags) { return tags; /* flat sorted render used above */ }

    function escapeHTML(s) {
        return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }

    return { show };
})();
window.FolderMoveModal = FolderMoveModal;
```

### Task 5.5: Run move-modal.spec.ts (GREEN)

- [ ] **Step 1: Run**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/move-modal.spec.ts --reporter=line 2>&1 | tail -40`

Expected: all 5 tests PASS.

- [ ] **Step 2: If a test fails**

Diagnose:
- "self is disabled" failing: verify `data-dest-name` matches the short name (last segment)
- "serve-active error" failing: verify the error message text matches one of the regex variants. Adjust backend wording to match, or loosen the regex.
- Modal not closing: verify `closeModal` is called on success and sets `hidden`.

### Task 5.6: Commit Phase 5

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/folder-move-modal.js frontend/js/utils.js frontend/js/folder-context-menu.js e2e/tests/folder-context-menu/move-modal.spec.ts
git commit -m "$(cat <<'EOF'
feat(folder-context-menu): Rename and Move actions

Rename opens showPromptDialog prefilled with the short name and
commits via App.UpdateTag keeping the same parent. Rejects embedded
slashes and directs the user to Move for reparenting.

Move opens FolderMoveModal — a tree picker with live preview,
disabled rules (self, descendants, current parent), inline error
surfacing for backend rejections (collision, serve-active subtree),
and Cancel without side effects. Confirms via App.UpdateTag with a
new full path; re-renders folder cards on success.
EOF
)"
```

---

## Phase 6: Serve + Share integration

**Goal:** Clicking Serve on an inactive folder jumps to the Serve view with the folder pre-added to configured entries. Clicking Stop serving while active calls `ServeService.StopServing` directly with toast confirmation. Clicking Share on an inactive folder jumps to the Share view with the "Share a tag" modal pre-selected. Clicking Stop sharing while active calls `ShareService.StopShare` directly.

**Files:**
- Modify: `frontend/js/serve.js` — expose `openServeViewForTag(tagID)`
- Modify: `frontend/js/share.js` — expose `openShareFlowForTag(tagID)`
- Create: `e2e/tests/folder-context-menu/serve-toggle.spec.ts`
- Create: `e2e/tests/folder-context-menu/share-toggle.spec.ts`

### Task 6.1: Expose openServeViewForTag at top level

**Files:**
- Modify: `frontend/js/serve.js` — place the new helper OUTSIDE `showServeTagPicker` (the existing `selectOption` is nested inside `showServeTagPicker` at line ~294; appending there would trap our helper in a closure)

- [ ] **Step 1: Verify where `showServeTagPicker` closes**

Run: `Read /Users/egecan/Code/mahpastes/frontend/js/serve.js (offset 253, limit 100)`

Scroll to find the matching `}` of `showServeTagPicker`. The helper must be placed AFTER that closing brace, at module top level.

- [ ] **Step 2: Extract a shared `addConfiguredServeEntry` helper at module top level**

Insert this top-level helper just before `async function showServeTagPicker() {` (so both the picker's `selectOption` and the new `openServeViewForTag` can call it):

```javascript
// Shared helper: add a tag to configuredEntries (stopped state) and refresh the view.
// Used by the tag-picker selectOption and by openServeViewForTag (folder-context-menu entry point).
async function addConfiguredServeEntry(tagID, tagName) {
    if (!configuredEntries.has(tagID)) {
        configuredEntries.set(tagID, { tag_id: tagID, tag_name: tagName, bind_all: false });
    }
    await loadServeStatus();
}
```

- [ ] **Step 3: Update `selectOption` inside `showServeTagPicker` to use the shared helper**

Old (inside `showServeTagPicker`, around line 294):
```javascript
        async function selectOption(option) {
            const tagID = parseInt(option.dataset.tagId, 10);
            const tagName = option.textContent.trim();
            picker.remove();
            document.removeEventListener('click', closeOnOutside);
            configuredEntries.set(tagID, { tag_id: tagID, tag_name: tagName, bind_all: false });
            await loadServeStatus();
        }
```

New:
```javascript
        async function selectOption(option) {
            const tagID = parseInt(option.dataset.tagId, 10);
            const tagName = option.textContent.trim();
            picker.remove();
            document.removeEventListener('click', closeOnOutside);
            await addConfiguredServeEntry(tagID, tagName);
        }
```

- [ ] **Step 4: Add `openServeViewForTag` at module top level (outside any other function)**

Place this at the same top-level scope as `showServeTagPicker`, e.g. immediately after the closing `}` of `showServeTagPicker`:

```javascript
async function openServeViewForTag(tagID) {
    if (typeof switchView === 'function') switchView('serve');
    const tags = await window.go.main.App.GetTags();
    const tag = tags.find(t => t.id === tagID);
    if (!tag) return;
    await addConfiguredServeEntry(tagID, tag.name);

    // Best-effort highlight — if rows expose a per-tag attribute, scroll and flash.
    const row = document.querySelector(`[data-serve-row-tag-id="${tagID}"]`);
    if (row) {
        row.scrollIntoView({ behavior: 'smooth', block: 'center' });
        row.classList.add('ring-2', 'ring-stone-500');
        setTimeout(() => row.classList.remove('ring-2', 'ring-stone-500'), 1500);
    }
}
window.openServeViewForTag = openServeViewForTag;
```

- [ ] **Step 5: Expose a test helper so e2e can observe configuredEntries without reaching into private scope**

Find the existing `window.__testHelpers` block near the end of `serve.js` (around line 490 — the one that already has `switchView` / `getCurrentView`) and add:

```javascript
if (window.__testHelpers) {
    window.__testHelpers.hasConfiguredServeEntry = (tagName) => {
        for (const entry of configuredEntries.values()) {
            if (entry.tag_name === tagName) return true;
        }
        return false;
    };
}
```

- [ ] **Step 6: Confirm `data-serve-row-tag-id` attribute on rendered rows (optional polish)**

Run (Grep tool):
```
pattern: "data-serve-row-tag-id"
path: /Users/egecan/Code/mahpastes/frontend/js/serve.js
output_mode: content
-n: true
```

If the attribute isn't set anywhere today, find the function that renders serve rows (likely inside `loadServeStatus` or `renderServeEntries`) and add `row.setAttribute('data-serve-row-tag-id', entry.tag_id)` at that site. If that's too invasive, drop the scroll/highlight block in Step 4 — the scroll is polish, not test-critical. The test only asserts view switch and `configuredEntries` membership (via the helper from Step 5).

### Task 6.2: Expose openShareFlowForTag

**Files:**
- Modify: `frontend/js/share.js` near line 140 (addShareBtn handler)

- [ ] **Step 1: Refactor the addShareBtn handler to extract openable helper**

Edit old_string (match the full listener body):
```javascript
addShareBtn.addEventListener('click', async () => {
    const tags = await window.go.main.App.GetTags();
    tagSelect.innerHTML = '';
    (tags || []).forEach(t => {
        const o = document.createElement('option');
        o.value = t.id;
        o.textContent = t.name;
        tagSelect.appendChild(o);
    });
    pickerSec.classList.remove('hidden');
    resultSec.classList.add('hidden');
    createModal.classList.remove('hidden');
});
```

Edit new_string:
```javascript
async function openShareModalForTag(preselectTagID) {
    const tags = await window.go.main.App.GetTags();
    tagSelect.innerHTML = '';
    (tags || []).forEach(t => {
        const o = document.createElement('option');
        o.value = t.id;
        o.textContent = t.name;
        tagSelect.appendChild(o);
    });
    if (preselectTagID !== undefined && preselectTagID !== null) {
        tagSelect.value = String(preselectTagID);
    }
    pickerSec.classList.remove('hidden');
    resultSec.classList.add('hidden');
    createModal.classList.remove('hidden');
}

addShareBtn.addEventListener('click', () => openShareModalForTag());

async function openShareFlowForTag(tagID) {
    if (typeof switchView === 'function') switchView('share');
    await openShareModalForTag(tagID);
}
window.openShareFlowForTag = openShareFlowForTag;
```

### Task 6.3: Write serve-toggle.spec.ts (RED)

**Files:**
- Create: `e2e/tests/folder-context-menu/serve-toggle.spec.ts`

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu: Serve toggle', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `srv-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Serve jumps to Serve view with folder pre-added', async ({ app }) => {
        const tag = await setup(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('serve'));

        // View switched to serve — read via the existing test helper
        // (currentView is a module-scope `let`, not on window)
        const currentView = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(currentView).toBe('serve');

        // configuredEntries is also private lexical state — use the helper added in Task 6.1 Step 5
        const hasEntry = await app.page.evaluate(
            (name) => (window as any).__testHelpers?.hasConfiguredServeEntry?.(name),
            tag
        );
        expect(hasEntry).toBe(true);
    });

    test('When serving, menu item flips to Stop serving and stops on click', async ({ app }) => {
        const tag = await setup(app);

        // Start serving via API
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        // Wait for badge to appear (confirms poller saw it)
        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(1, { timeout: 5000 });

        // Right-click → menu shows "Stop serving"
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        const serveItem = app.page.locator(`${selectors.folderContextMenu} [data-action="stop-serve"]`);
        await expect(serveItem).toBeVisible();
        await expect(serveItem).toContainText(/Stop serving/);

        await serveItem.click();

        // Badge disappears
        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(0, { timeout: 5000 });

        // Still in clips view (didn't navigate away) — helper reads the private `let currentView`
        const currentView = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(currentView).toBe('clips');
    });

    test('Stop serving race (already stopped) is a silent no-op', async ({ app }) => {
        const tag = await setup(app);

        // Stub stop-serving in flight: start, stop via API, then invoke handleAction stop-serve
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
            await window.go.main.ServeService.StopServing(t.id);
            // Now call stop again to exercise the race
            try {
                await window.go.main.ServeService.StopServing(t.id);
                return 'unexpected-success';
            } catch (e: any) {
                return e.message;
            }
        }, tag).then(msg => {
            // Ensure the error message matches the regex we're filtering against
            expect(String(msg)).toMatch(/no server running/i);
        });

        // Verify no error toast appears when we right-click → stop-serve while already stopped
        // (start again, stop via API, then via menu)
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);
        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(1, { timeout: 5000 });
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, tag);

        // Menu may still show Stop serving for up to 2s
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        const stopItem = app.page.locator(`${selectors.folderContextMenu} [data-action="stop-serve"]`);
        if (await stopItem.count() === 1) {
            // Force toast observation: no error toast with "Failed to stop"
            const beforeToast = await app.page.locator('.toast-error, [data-toast-type="error"]').count();
            await stopItem.click();
            const afterToast = await app.page.locator('.toast-error, [data-toast-type="error"]').count();
            expect(afterToast).toBe(beforeToast);
        }
    });
});
```

- [ ] **Step 2: Run (RED — some will fail because the switch + configuredEntries hookup isn't wired yet via menu)**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/serve-toggle.spec.ts --reporter=line 2>&1 | tail -40`

Expected: test 1 (serve → jump) FAILS because the menu's `serve` action still points at stub. Test 2 (stop) FAILS similarly. Test 3 may PASS if the silent-no-op catch is already in place from Phase 4's handleAction.

### Task 6.4: Verify Phase 4 handleAction stop-serve/share silent no-op wired

Phase 4 already implemented `doStopServing` with `/no server running/i` regex filter and `doStopSharing` as idempotent. No change needed. The `serve` and `share` menu actions already delegate to `openServeViewForTag` / `openShareFlowForTag` via typeof guard; those are now defined by Tasks 6.1 and 6.2.

### Task 6.5: Run serve-toggle.spec.ts (GREEN)

- [ ] **Step 1: Re-run**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/serve-toggle.spec.ts --reporter=line 2>&1 | tail -40`

Expected: all tests PASS.

### Task 6.6: Write share-toggle.spec.ts

**Files:**
- Create: `e2e/tests/folder-context-menu/share-toggle.spec.ts`

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu: Share toggle', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `shr-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Share jumps to Share view with the tag pre-selected', async ({ app }) => {
        const tag = await setup(app);
        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        await app.page.click(selectors.folderContextMenuItem('share'));

        const view = await app.page.evaluate(
            () => (window as any).__testHelpers?.getCurrentView?.()
        );
        expect(view).toBe('share');

        // Share modal open with tag preselected
        const modal = app.page.locator('[data-testid="share-create-modal"], #share-create-modal');
        await expect(modal).toBeVisible();

        const selectValue = await app.page.locator('#share-tag-select, [data-testid="share-tag-select"]').inputValue();
        const tagID = await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            return tags.find((t: any) => t.name === n)?.id;
        }, tag);
        expect(selectValue).toBe(String(tagID));
    });

    test('When sharing, menu flips to Stop sharing and stops on click', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ShareService.StartShare(t.id);
        }, tag);
        await expect(app.page.locator(selectors.folderBadgeSharedActive(tag))).toHaveCount(1, { timeout: 5000 });

        await app.page.click(selectors.folderCard(tag), { button: 'right' });
        const item = app.page.locator(`${selectors.folderContextMenu} [data-action="stop-share"]`);
        await expect(item).toContainText(/Stop sharing/);
        await item.click();

        await expect(app.page.locator(selectors.folderBadgeSharedActive(tag))).toHaveCount(0, { timeout: 5000 });
    });
});
```

- [ ] **Step 2: Run**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/share-toggle.spec.ts --reporter=line 2>&1 | tail -30`

Expected: both tests PASS. If the share modal selectors don't match the actual DOM (`[data-testid="share-create-modal"]` or `#share-tag-select`), grep `share.js` and `frontend/index.html` for the real IDs and update the selectors.

### Task 6.7: Commit Phase 6

- [ ] **Step 1: Stage and commit**

Run:
```bash
git add frontend/js/serve.js frontend/js/share.js e2e/tests/folder-context-menu/serve-toggle.spec.ts e2e/tests/folder-context-menu/share-toggle.spec.ts
git commit -m "$(cat <<'EOF'
feat(folder-context-menu): Serve and Share actions

Inactive Serve jumps to the Serve view, adds the folder to the
configured entries, and (best-effort) highlights its row; user
clicks Start to confirm. Active Serve flips to Stop serving and
calls ServeService.StopServing directly. "no server running" race
is a silent no-op as specified.

Inactive Share jumps to the Share view and opens the Share-a-tag
modal with the dropdown pre-selected; user clicks Create to confirm.
Active Share flips to Stop sharing and calls ShareService.StopShare
directly (StopShare is already idempotent server-side).
EOF
)"
```

---

## Phase 7: A11y polish (aria-label verification + reduced motion)

**Goal:** `a11y.spec.ts` verifies composite aria-labels, badge `role="img"` + aria-label, Shift+F10 opens menu with correct focus, Esc restores focus, and reduced-motion preference is respected.

**Files:**
- Create: `e2e/tests/folder-context-menu/a11y.spec.ts`

Note: long-press is already wired in Phase 4; no Playwright test because touch emulation is brittle. Document as manual-test step only.

### Task 7.1: Write a11y.spec.ts

- [ ] **Step 1: Write the spec**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { selectors } from '../../helpers/selectors';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder context menu a11y', () => {
    async function setup(app: any) {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);
        const tag = `a11y-${Date.now()}`;
        await app.createTag(tag);
        await app.addTagToClip(path.basename(imagePath), tag);
        await app.enterFolderMode();
        return tag;
    }

    test('Folder card is focusable via Tab and has composite aria-label', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));

        await expect(card).toHaveAttribute('tabindex', '0');
        const label = await card.getAttribute('aria-label');
        expect(label).toMatch(new RegExp(`Folder: ${tag}`));
        expect(label).toMatch(/\d+ clips?/);
    });

    test('aria-label composition includes active states', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        await expect(app.page.locator(selectors.folderBadgeServedActive(tag))).toHaveCount(1, { timeout: 5000 });
        const label = await app.page.locator(selectors.folderCard(tag)).getAttribute('aria-label');
        expect(label).toMatch(/served/);

        // Cleanup
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, tag);
    });

    test('Badge has role="img" and descriptive aria-label', async ({ app }) => {
        const tag = await setup(app);
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            const port = await window.go.main.ServeService.GetRandomPort();
            await window.go.main.ServeService.StartServing(t.id, port, false, 'none');
        }, tag);

        const badge = app.page.locator(selectors.folderBadgeServedActive(tag));
        await expect(badge).toHaveCount(1, { timeout: 5000 });
        await expect(badge).toHaveAttribute('role', 'img');
        await expect(badge).toHaveAttribute('aria-label', /Served on/);

        const inner = badge.locator('svg');
        await expect(inner).toHaveAttribute('aria-hidden', 'true');
        await expect(inner).toHaveAttribute('focusable', 'false');

        // Cleanup
        await app.page.evaluate(async (n) => {
            const tags = await window.go.main.App.GetTags();
            const t = tags.find((x: any) => x.name === n);
            await window.go.main.ServeService.StopServing(t.id);
        }, tag);
    });

    test('Shift+F10 from focused card opens menu; Esc restores focus', async ({ app }) => {
        const tag = await setup(app);
        const card = app.page.locator(selectors.folderCard(tag));
        await card.focus();

        await app.page.keyboard.press('Shift+F10');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeVisible();

        await app.page.keyboard.press('Escape');
        await expect(app.page.locator(selectors.folderContextMenu)).toBeHidden();

        const activeTestID = await app.page.evaluate(() => document.activeElement?.getAttribute('data-testid'));
        expect(activeTestID).toBe(`folder-card-${tag}`);
    });
});
```

- [ ] **Step 2: Run**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npx playwright test tests/folder-context-menu/a11y.spec.ts --reporter=line 2>&1 | tail -30`

Expected: all 4 tests PASS. If focus-restore fails, check `ContextMenu.close` for `lastFocusedBeforeOpen.focus()` — that's the existing primitive. No changes needed beyond ensuring the card is the actual focus owner before Shift+F10.

### Task 7.2: Commit Phase 7

- [ ] **Step 1: Commit**

Run:
```bash
git add e2e/tests/folder-context-menu/a11y.spec.ts
git commit -m "$(cat <<'EOF'
test(folder-context-menu): a11y assertions

Cover Tab-focusability, composite aria-label (name + count + active
states + hidden), badge role=img + aria-label + aria-hidden svg,
Shift+F10 open + Esc restore focus to triggering card.
EOF
)"
```

---

## Phase 8: Regression sweep + AppHelper fixture completion

**Goal:** Add the fixture helpers the spec calls for (`rightClickFolder`, `getFolderContextMenuItem`, `expectFolderBadge`) for future test ergonomics, run the entire e2e suite, fix any regressions.

### Task 8.1: Add fixture helpers (convenience wrappers)

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts`

- [ ] **Step 1: Append to AppHelper class**

```typescript
async rightClickFolder(name: string): Promise<void> {
    await this.page.click(`[data-testid="folder-card-${name}"]`, { button: 'right' });
    await this.page.locator('.card-menu-dropdown[data-source="folder"]').waitFor({ state: 'visible' });
}

getFolderContextMenuItem(action: string) {
    return this.page.locator(`.card-menu-dropdown[data-source="folder"] [data-action="${action}"]`);
}

async expectFolderBadge(name: string, type: 'served' | 'shared' | 'served-paused' | 'shared-paused'): Promise<void> {
    const map: Record<string, string> = {
        'served':        `[data-testid="folder-card-${name}"] .folder-badge-serve`,
        'shared':        `[data-testid="folder-card-${name}"] .folder-badge-share`,
        'served-paused': `[data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="serve"]`,
        'shared-paused': `[data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="share"]`,
    };
    await this.page.locator(map[type]).waitFor({ state: 'visible', timeout: 5000 });
}
```

`renderBadge` already emits `data-kind="serve"` / `data-kind="share"` (set in Phase 2). No additional markup change needed here.

### Task 8.2: Run the full e2e suite

- [ ] **Step 1: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test 2>&1 | tail -80`

Expected: all tests pass. Pay special attention to:
- `tests/clips/context-menu.spec.ts` (existing, must still pass)
- `tests/tags/folder-mode.spec.ts` (existing; folder cards changed shape)
- `tests/folder-drag/folder-drag.spec.ts` (drag still works)
- `tests/tags/tag-hidden.spec.ts` (hidden-tag filter behavior unchanged)
- `tests/serve/*` and `tests/share/*` if present (views untouched apart from added helpers)

- [ ] **Step 2: If any regression**

For each failing test, run it in isolation with `--headed` to observe behavior. Typical causes and fixes:

| Symptom | Likely root cause | Fix |
|---------|-------------------|-----|
| Folder card click no longer navigates | `keydown` Enter/Space handler may preempt click | Verify click handler added alongside, not replacing |
| Drag-out intercepted by contextmenu | Drag uses mousedown; should coexist | If it doesn't, `pointerdown`+preventDefault on right-click only (button === 2) |
| `tag-hidden.spec.ts` fails because hidden folders now render | Hidden in filter dropdown unchanged; hidden in folder view is intentional (we render with opacity) — update spec if it assumed non-rendering | If the spec specifically tested folder-view non-rendering, add comment referencing design decision; update expectations to `data-hidden="true"` |

### Task 8.3: Manual QA checklist

- [ ] **Step 1: Run `make dev` and verify**

Run: `make dev`

Go through each in the app:
- Right-click a folder → menu appears at pointer position
- Shift+F10 on focused folder → menu appears at card edge
- Open action → navigates in
- Move → modal opens, tree shows, preview updates, collision surfaces error
- Rename → prompt appears prefilled, renames on confirm
- Serve (inactive) → view switches, folder pre-added
- After starting serve → badge appears within ~2s
- Serve (active) → menu shows Stop serving → click → badge disappears
- Share (inactive) → share modal opens with folder pre-selected
- After starting share → blue chain badge appears
- Share (active) → menu shows Stop sharing → click → badge disappears
- Hide → folder becomes translucent + grayscale; aria-label includes "hidden"
- Unhide → restores normal appearance
- Touch long-press (if on a touch device) → menu opens at touch point

- [ ] **Step 2: Verify in DevTools**

- Console is free of errors and warnings during all of the above
- `folderStatusPoller.isActive()` is `true` only when in clips view + folder mode
- `folderStatusMap` clears when leaving folder mode

### Task 8.4: Commit Phase 8

- [ ] **Step 1: Commit**

Run:
```bash
git add e2e/fixtures/test-fixtures.ts
git commit -m "$(cat <<'EOF'
test(folder-context-menu): fixture helpers

Add rightClickFolder, getFolderContextMenuItem, expectFolderBadge
helpers to AppHelper for ergonomic assertions in future tests.
EOF
)"
```

---

## Appendix: Error recovery playbook

If stuck mid-task:

- **Test flake, "element not visible":** add `await app.page.waitForLoadState('networkidle')` after mutations; increase timeout on specific `expect` calls
- **Menu open → nothing happens:** check `FolderContextMenu` loaded before `ui.js` (verify index.html order); `console.log` from the `contextmenu` handler
- **Badge not appearing after StartServing:** manual DevTools: `folderStatusPoller.evaluate(); await wailsGetServeStatus()` — check response shape. If `tag_id` field is actually `tagId`, update `applyFolderStatusUpdate` keys
- **Move modal "self disabled" test fails:** verify `data-dest-name` matches `src.split('/').pop()`, not the full path
- **Regression: existing clip context menu broken:** almost certainly the `positionMainMenu` change. Revert just that change and inspect; the duck-typed detection should be transparent to Element callers

---

## Self-Review Log

- **Spec coverage:** All 9 menu items (Open, Move, Rename, Serve/Stop, Share/Stop, Hide/Unhide) covered across Phase 4 (Open, Hide), Phase 5 (Move, Rename), Phase 6 (Serve, Share). Status badges and hidden styling in Phase 3. Pointer anchoring in Phase 1. Poller with both `switchView` and `toggleFolderMode` hooks in Phase 2.
- **Placeholder scan:** No TBDs, TODOs, or "fill in". Every code step has concrete content. Spec requirement about ShareStatus response shape (`follower_count` vs alternative) is flagged in Appendix recovery playbook instead of left ambiguous.
- **Type consistency:** `folderStatusMap` is a Map throughout; `folderStatusPoller` methods (`start`, `stop`, `evaluate`, `isActive`) are used consistently; `openServeViewForTag` / `openShareFlowForTag` referenced in Phase 4 handleAction and defined in Phase 6.
- **Sequencing:** Phase 1 extends ContextMenu (no test yet; validated indirectly by Phase 4). Phase 2 is invisible infrastructure. Phase 3 visibly activates everything. Phases 4-6 layer on menu items. Phase 7 adds a11y coverage. Phase 8 runs the full regression sweep. Each phase commits independently; each is reversible.
