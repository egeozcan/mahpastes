# Docs Overhaul Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring all Docusaurus docs to 100% accuracy, zero AI-slop, full screenshot coverage using a 6-agent team.

**Architecture:** Two-phase agent team. Phase 1: four parallel read-only agents audit codebase and docs, producing reports. Phase 2: writing-coach rewrites all flagged docs, screenshot-updater refreshes screenshots. Conductor coordinates.

**Tech Stack:** Agent teams, Docusaurus markdown, Playwright screenshot capture, Tailwind CSS.

---

### Task 1: Create the Agent Team

**Files:**
- Reference: `docs/plans/2026-03-04-docs-overhaul-design.md`

**Step 1: Create the team**

Use `TeamCreate` with name `docs-overhaul`.

**Step 2: Create all Phase 1 tasks on the task board**

Create these tasks using `TaskCreate`:

1. "Produce ground truth feature inventory (backend features)" — assigned to tech-summarizer-1
2. "Produce ground truth feature inventory (plugins, editor, shortcuts)" — assigned to tech-summarizer-2
3. "Audit all docs for factual accuracy" — assigned to doc-checker-1
4. "Audit all docs for coverage gaps" — assigned to doc-checker-2

**Step 3: Spawn Phase 1 agents in parallel (all with isolation: worktree)**

Launch all four agents simultaneously using the Agent tool. Each runs in a worktree (read-only, no edits).

---

### Task 2: tech-summarizer-1 — Backend Feature Inventory

**Agent type:** general-purpose, worktree isolation

**Prompt for the agent:**

```
You are tech-summarizer-1 on the docs-overhaul team. Your job is READ-ONLY research — do NOT edit any files.

Read the codebase and produce a ground truth feature inventory for these features. For each, document the EXACT current behavior, all options/settings, edge cases, and any recent changes.

Features to cover:
1. **Clipboard management** — Read `app.go` methods: GetClips, UploadFiles, UploadFileAndGetID, DeleteClip, BulkDelete, CopyToClipboard, GetClipData, SaveClipToFile, BulkDownloadToFile. Note supported content types, upload flow, metadata captured.

2. **Expiration/auto-delete** — Read `app.go` methods: SetExpiration, BulkSetExpiration, CancelExpiration, BulkCancelExpiration. Read `database.go` for cleanup job. Note: the expiration UI IS implemented (presets: 15m, 1h, 6h, 24h, 7d). Document exact presets, visual indicators, bulk operations.

3. **Archive** — Read `app.go` ToggleArchive, BulkArchive. Document interaction with expiration.

4. **Watch folders** — Read `watcher.go` and `app.go` watch methods. Document: AddWatchedFolder, RemoveWatchedFolder, UpdateWatchedFolder, filter modes, presets, regex, per-folder pause, global pause, ProcessExistingFilesInFolder, debounce timing.

5. **Backup/restore** — Read `backup.go`. Document what's included/excluded, format, restore flow.

6. **Bulk actions** — Read `app.go` bulk methods. Document all available bulk operations.

7. **Transfer/drag-out** — Read `transfer_service.go`, `transfer_types.go`, `native_drag_darwin.go`, `native_drag_other.go`, `clipboard_service.go`, `clipboard_darwin.go`, `clipboard_windows.go`. Document EXACT platform support:
   - macOS: native drag + NSPasteboard clipboard
   - Windows: DataTransfer DownloadURL drag + PowerShell clipboard
   - Linux: status
   Note: Windows drag-out WAS shipped. Document temp file lifecycle (60-min lease, 10-min prune).

8. **Metadata system** (NEW) — Read `app.go` GetClipMetadata, SetClipMetadata, DeleteClipMetadata, SetClipMetadataBulk. Read `database.go` for schema. Document: system metadata vs custom metadata, 50-pair limit.

9. **Deduplication** (NEW) — Read `app.go` GetDuplicateGroups, DeduplicateAll. Document SHA-256 hashing, merge behavior.

10. **Sort interface** (NEW) — Read `app.go` GetClips for sort parameters. Read `frontend/js/app.js` or `frontend/js/ui.js` for sort UI.

11. **Settings** — Read `app.go` GetSetting, SetSetting. Read `frontend/js/settings.js` for all settings options.

OUTPUT FORMAT: Write your report as a structured markdown document. For each feature, include:
- Feature name
- Exact current behavior
- All options/presets/settings
- Platform support status
- Recent changes (if identifiable from code comments or git)
- Edge cases or gotchas

Send your completed report back to the conductor via SendMessage when done.
```

---

### Task 3: tech-summarizer-2 — Plugin, Editor, Shortcuts Inventory

**Agent type:** general-purpose, worktree isolation

**Prompt for the agent:**

```
You are tech-summarizer-2 on the docs-overhaul team. Your job is READ-ONLY research — do NOT edit any files.

Read the codebase and produce a ground truth feature inventory for these features:

1. **Plugin system** — Read ALL files in `plugin/` directory:
   - `manager.go` — lifecycle, event dispatch
   - `manifest.go` — ValidEvents(), manifest structure, UI action definitions
   - `sandbox.go` — execution limits, sandboxing
   - `scheduler.go` — scheduled tasks
   - `api_clips.go`, `api_tags.go`, `api_storage.go`, `api_http.go`, `api_fs.go`, `api_utils.go`, `api_task.go`, `api_toast.go`, `api_image.go`, `api_modal.go` — all Lua API functions
   - Check for any NEW api files (e.g., api_metadata.go)
   Document every function signature, parameters, return values.

2. **Plugin service** — Read `plugin_service.go`. Document all PluginService methods, especially:
   - Plugin install from URL, update checking
   - Permission model
   - Plugin global actions (NEW) — actions in hamburger menu
   - Modal guard system

3. **Tags** — Read `app.go` tag methods + `frontend/js/tags.js`. Document: CreateTag, UpdateTag, DeleteTag, tag colors, hidden tags, filtering.

4. **Image editor** — Read `frontend/js/editor.js`. Document: all tools, canvas limits, undo depth, color picker, opacity, save behavior.

5. **Image comparison** — Read `frontend/js/modals.js` (comparison section). Document: modes (fade, slider, diff), swap, image info, A/B labels (NEW enhancements).

6. **Text editor** — Read `frontend/js/modals.js` (text editor section). Document capabilities and limitations.

7. **Keyboard shortcuts** — Read `frontend/js/shortcuts.js`. Document EVERY registered shortcut with its action and context. This is critical — there are 45+ shortcuts.

8. **Tooltips** (NEW) — Read relevant code for tooltip toggle and behavior.

9. **Bottom bar** (NEW) — Read `frontend/index.html` and JS for the fixed bottom bar with add button and expiry selector.

10. **Plugin global actions** (NEW) — Read `frontend/js/plugins.js` and `plugin_service.go` for actions accessible from hamburger menu.

11. **Developer internals** — Read `app.go`, `database.go`, `main.go` for architecture overview. Note database schema, migration strategy, build system.

OUTPUT FORMAT: Structured markdown. For each feature:
- Feature name
- Exact current behavior with all options
- Function signatures (for APIs)
- Keyboard shortcuts (exact keys and contexts)
- Recent changes
- Edge cases

Send your completed report back to the conductor via SendMessage when done.
```

---

### Task 4: doc-checker-1 — Factual Accuracy Audit

**Agent type:** general-purpose, worktree isolation

**Prompt for the agent:**

```
You are doc-checker-1 on the docs-overhaul team. Your job is READ-ONLY research — do NOT edit any files.

Read ALL 34 documentation files in `docs/docs/` and cross-reference against the actual codebase. For each doc, check:

1. **Factual accuracy** — Are the described behaviors correct? Check against actual Go methods and JS code.
2. **Outdated claims** — Flag any "not yet implemented", "planned", "coming soon" for features that now exist.
3. **Missing features** — Does the doc omit features that exist in the code it covers?
4. **Incorrect details** — Wrong keyboard shortcuts, wrong menu names, wrong file paths, wrong defaults.
5. **Inconsistent terminology** — Same thing called different names across docs.
6. **Dead links** — Internal cross-references that point to non-existent pages.
7. **Incorrect platform claims** — e.g., docs saying "macOS only" when Windows is supported.

KNOWN ISSUES TO VERIFY:
- `docs/docs/features/clipboard-management.md` line 48: says "expiration UI is not yet implemented" — verify this is stale
- `docs/docs/getting-started/quick-start.md`: similar stale expiration claim
- `docs/docs/intro.md`: says "macOS 10.15 or later" only — Windows has clipboard/drag support now
- `docs/docs/features/drag-and-drop.md`: says Windows "Planned" — Windows drag-out shipped

Read every doc file. Read the corresponding code. Flag every issue.

OUTPUT FORMAT: For each doc file, list:
- File path
- Issue type (stale, inaccurate, missing, inconsistent, dead link)
- Exact line or quote with the problem
- What it should say instead (based on code)
- Severity (critical = factually wrong, medium = missing info, low = style/terminology)

Send your completed report back to the conductor via SendMessage when done.
```

---

### Task 5: doc-checker-2 — Coverage Gap Audit

**Agent type:** general-purpose, worktree isolation

**Prompt for the agent:**

```
You are doc-checker-2 on the docs-overhaul team. Your job is READ-ONLY research — do NOT edit any files.

Audit documentation coverage by comparing what features exist in the codebase against what's documented.

CHECK THESE AREAS:

1. **Undocumented features** — Features in code with NO doc page:
   - Metadata system (GetClipMetadata, SetClipMetadata, etc.) — likely needs a new doc page
   - Sort interface — likely needs section in clipboard-management or new page
   - Deduplication (GetDuplicateGroups, DeduplicateAll) — needs doc page
   - Tooltips toggle — needs mention somewhere
   - Bottom bar — needs mention in quick-start or clipboard-management
   - Plugin global actions (hamburger menu) — needs section in plugins docs
   - Plugin metadata Lua API module — needs addition to api-reference

2. **Missing screenshots** — Check which doc pages reference screenshots vs which screenshots exist in `docs/static/img/screenshots/`. Identify:
   - Doc pages that should have screenshots but don't
   - Screenshots that exist but no doc references them
   - Screenshots that are likely stale (pre-dating recent UI changes like bottom bar, metadata panel, sort popover)

3. **Sidebar organization** — Read `docs/sidebars.js`. Check if:
   - New features need sidebar entries
   - Category grouping still makes sense
   - Order is logical for new users

4. **Cross-reference completeness** — Check that docs link to related docs where appropriate:
   - clipboard-management should link to auto-delete, drag-and-drop, bulk-actions
   - Plugin docs should cross-reference each other
   - Tutorials should link to feature docs they use

5. **Keyboard shortcuts completeness** — Compare `frontend/js/shortcuts.js` registered shortcuts against `docs/docs/getting-started/keyboard-shortcuts.md`. Flag any missing shortcuts.

6. **Plugin API completeness** — Compare `plugin/api_*.go` files against `docs/docs/plugins/api-reference.md`. Flag any undocumented APIs.

OUTPUT FORMAT:
- Section 1: Missing doc pages needed (with suggested title and location)
- Section 2: Missing screenshots needed (with suggested scenario)
- Section 3: Sidebar changes needed
- Section 4: Missing cross-references
- Section 5: Missing keyboard shortcuts in docs
- Section 6: Missing plugin APIs in docs
- Section 7: Any other coverage gaps

Send your completed report back to the conductor via SendMessage when done.
```

---

### Task 6: Conductor — Merge Phase 1 Reports

**Step 1: Wait for all 4 Phase 1 agents to complete**

Monitor task board. All 4 agents must send their reports.

**Step 2: Merge reports into a combined task list**

Create Phase 2 tasks on the task board, organized by priority:

1. Critical factual fixes (wrong info that could confuse users)
2. New doc pages for undocumented features
3. Stale claim updates
4. Coverage gap fills (missing sections, cross-references)
5. Anti-slop rewrites (every doc gets a pass)
6. Screenshot updates

**Step 3: Spawn Phase 2 agents**

Launch writing-coach first (needs to finish before screenshot-updater runs, since screenshots should match final docs).

---

### Task 7: writing-coach — Rewrite All Docs

**Agent type:** general-purpose (NOT worktree — edits main branch)

**Prompt for the agent:**

```
You are the writing-coach on the docs-overhaul team. Your job is to EDIT documentation files to fix all issues and eliminate AI-slop.

You will receive a merged report from the conductor with all issues found by Phase 1 agents. Work through every issue.

## ANTI-SLOP RULES (MANDATORY)

### Banned Words/Phrases — NEVER use these:
leverage, seamlessly, robust, empower, cutting-edge, harness, elevate, streamline, game-changer, unlock, dive into, delve, explore (as filler), comprehensive, it's worth noting, importantly, furthermore, additionally, in conclusion, let's, effortlessly, intuitive, powerful

### Style Rules:
- Lead with what the feature does, not why it matters
- One idea per paragraph
- Delete any sentence that doesn't add information
- Prefer imperative ("Click X") over "You can click X"
- No excited tone — factual, direct, slightly dry
- Tables over prose for reference data
- Code examples over descriptions when possible
- No unnecessary "best practices" padding
- Each doc should sound like a human wrote it, not a template
- No marketing language in technical docs
- "mahpastes" is always lowercase

### For each doc file:
1. Fix all factual issues from the Phase 1 report
2. Add missing features/sections
3. Run every sentence through the anti-slop filter
4. Ensure cross-references are correct
5. Keep the existing Docusaurus frontmatter (sidebar_position, etc.)
6. Keep existing JSX components (keyboard-key spans, etc.)

### New doc pages to create:
Based on Phase 1 coverage audit, create new pages as needed in `docs/docs/features/` or appropriate category. Update `docs/sidebars.js` to include them.

### Commit after each doc category:
- `git commit -m "docs: fix accuracy issues in getting-started docs"`
- `git commit -m "docs: rewrite features docs for accuracy and clarity"`
- `git commit -m "docs: update plugin docs with new APIs"`
- etc.

When done with all docs, send a message to the conductor listing all changes made.
```

---

### Task 8: screenshot-updater — Refresh All Screenshots

**Agent type:** general-purpose (NOT worktree — edits main branch)

**Prompt for the agent:**

```
You are the screenshot-updater on the docs-overhaul team. Your job is to update the Playwright screenshot capture test and regenerate all documentation screenshots.

## Current State
- Screenshot capture test: `e2e/tests/screenshots/capture.spec.ts`
- Screenshots output to: `docs/static/img/screenshots/`
- 18 existing screenshots covering 13 scenarios

## What to Do

1. Read the current `capture.spec.ts` to understand existing scenarios.

2. Read the Phase 1 coverage report to identify missing screenshots.

3. Add new capture scenarios to `capture.spec.ts` for:
   - Auto-delete UI (expiration badges, expiration popover on context menu)
   - Metadata panel (clip metadata modal with system + custom metadata)
   - Sort interface (sort popover with options)
   - Deduplication (duplicate detection dialog or badge)
   - Bottom bar (the fixed bottom bar with add button and expiry selector)
   - Drag handle states (hover → preparing → ready sequence) if capturable
   - Plugin global actions in hamburger menu
   - Any other UI states identified as needing screenshots

4. Follow the existing patterns in capture.spec.ts:
   - Use AppHelper fixture methods
   - Use `app.page.waitForTimeout()` before captures
   - Save to `SCREENSHOTS_DIR` with descriptive names
   - Setup test data as needed

5. Run the screenshot capture: `cd e2e && npx playwright test tests/screenshots/capture.spec.ts`

6. Verify all screenshots were generated in `docs/static/img/screenshots/`.

7. Check that all screenshot references in docs match actual filenames.

8. Commit: `git commit -m "docs: update screenshot capture and regenerate all screenshots"`

When done, send a message to the conductor listing new screenshots added and any issues.
```

---

### Task 9: Conductor — Final Review

**Step 1: Run e2e tests**

```bash
cd e2e && npm test
```

Ensure all tests pass including the screenshot capture.

**Step 2: Build docs site**

```bash
cd docs && npm run build
```

Verify no broken links or build errors.

**Step 3: Spot-check anti-slop compliance**

Grep all doc files for banned words:

```bash
grep -riE 'leverage|seamlessly|robust|empower|cutting-edge|harness|elevate|streamline|game-changer|unlock|dive into|delve|comprehensive|it.s worth noting|importantly|furthermore|additionally|in conclusion' docs/docs/
```

Any matches must be fixed.

**Step 4: Verify all screenshots referenced in docs exist**

```bash
grep -roh '/img/screenshots/[^)]*' docs/docs/ | sort -u
ls docs/static/img/screenshots/
```

Cross-reference — every referenced screenshot must exist.

**Step 5: Final commit**

If any fixes needed from review:
```bash
git commit -m "docs: final review fixes for docs overhaul"
```

**Step 6: Shut down team**

Send shutdown requests to all remaining agents. Delete team.
