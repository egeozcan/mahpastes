# Documentation Audit and Rewrite — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Audit all 36 doc files against the codebase, fix inaccuracies, eliminate AI slop, add missing feature docs, and retake all screenshots.

**Architecture:** Two-phase approach. Phase 1 dispatches 7 parallel agents to audit code and docs independently. Conductor consolidates into a Master Audit Report. Phase 2 dispatches 4 parallel writers + 1 screenshot updater, followed by a sequential writing coach pass and final verification.

**Tech Stack:** Docusaurus (docs framework), Playwright (screenshots), Go + JS (source of truth)

---

## Task 1: Pre-flight Checks

**Purpose:** Verify the app builds and screenshots can be captured before starting the audit.

**Step 1: Verify Docusaurus builds**

Run: `cd /Users/egecan/Code/mahpastes/docs && npm run build`
Expected: Build succeeds with no errors.

**Step 2: Verify app builds**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Build succeeds. Binary in `build/bin/`.

**Step 3: Commit checkpoint**

No changes to commit yet — this is a read-only check.

---

## Task 2: Phase 1 — Dispatch All Audit Agents (7 parallel)

**Purpose:** Run all 7 audit agents in parallel. Each produces a report. All agents are read-only — they do not modify any files.

### Agent 2a: Summarizer-Go

**Dispatch as:** Explore subagent

**Prompt:**
```
Scan all Go backend files in the mahpastes project and produce a FEATURE INVENTORY — a structured list of every user-facing feature, its methods, and relevant code locations.

Files to scan:
- /Users/egecan/Code/mahpastes/app.go
- /Users/egecan/Code/mahpastes/database.go
- /Users/egecan/Code/mahpastes/watcher.go
- /Users/egecan/Code/mahpastes/plugins.go
- /Users/egecan/Code/mahpastes/plugin_service.go
- /Users/egecan/Code/mahpastes/clipboard_service.go
- /Users/egecan/Code/mahpastes/clipboard_darwin.go
- /Users/egecan/Code/mahpastes/transfer_service.go
- /Users/egecan/Code/mahpastes/transfer_types.go
- /Users/egecan/Code/mahpastes/temp_clip_store.go
- /Users/egecan/Code/mahpastes/native_drag_darwin.go
- /Users/egecan/Code/mahpastes/backup.go
- /Users/egecan/Code/mahpastes/api_manager.go
- /Users/egecan/Code/mahpastes/serve_manager.go
- /Users/egecan/Code/mahpastes/serve_service.go
- /Users/egecan/Code/mahpastes/main.go
- /Users/egecan/Code/mahpastes/plugin/*.go

For each feature, output this format:
  Feature: [name]
    Backend methods: [list of exported Go methods]
    Related files: [file paths]
    Description: [1-2 sentences of what it does based on code]
    UI-facing: [yes/no — does it have frontend interaction?]

Include ALL exported methods on App, PluginService, ClipboardService, TransferService, ServeService, and APIService structs. Group them by feature area.
```

### Agent 2b: Summarizer-JS

**Dispatch as:** Explore subagent

**Prompt:**
```
Scan all frontend JavaScript files in the mahpastes project and produce a FEATURE INVENTORY — a structured list of every user-facing feature, UI component, and interaction.

Files to scan:
- /Users/egecan/Code/mahpastes/frontend/js/app.js
- /Users/egecan/Code/mahpastes/frontend/js/ui.js
- /Users/egecan/Code/mahpastes/frontend/js/modals.js
- /Users/egecan/Code/mahpastes/frontend/js/editor.js
- /Users/egecan/Code/mahpastes/frontend/js/tags.js
- /Users/egecan/Code/mahpastes/frontend/js/plugins.js
- /Users/egecan/Code/mahpastes/frontend/js/settings.js
- /Users/egecan/Code/mahpastes/frontend/js/shortcuts.js
- /Users/egecan/Code/mahpastes/frontend/js/watch.js
- /Users/egecan/Code/mahpastes/frontend/js/transfer.js
- /Users/egecan/Code/mahpastes/frontend/js/transfer-strategies.js
- /Users/egecan/Code/mahpastes/frontend/js/utils.js
- /Users/egecan/Code/mahpastes/frontend/js/wails-api.js
- /Users/egecan/Code/mahpastes/frontend/js/modal-renderer.js
- /Users/egecan/Code/mahpastes/frontend/js/plugin-icons.js
- /Users/egecan/Code/mahpastes/frontend/js/plugin-review.js
- /Users/egecan/Code/mahpastes/frontend/js/task-queue.js
- /Users/egecan/Code/mahpastes/frontend/js/serve.js
- /Users/egecan/Code/mahpastes/frontend/js/api-settings.js

For each feature/component, output:
  Feature: [name]
    JS module: [filename]
    Key functions: [exported/global function names]
    UI entry point: [how user accesses it — button, menu, shortcut, etc.]
    Wails API calls: [which Go methods it calls via window.go.main.*]
    Description: [1-2 sentences of what it does]
```

### Agent 2c: Summarizer-HTML

**Dispatch as:** Explore subagent

**Prompt:**
```
Scan the frontend HTML and CSS to produce a UI STRUCTURE INVENTORY — every modal, view, panel, button group, and settings section in the app.

Files to scan:
- /Users/egecan/Code/mahpastes/frontend/index.html
- /Users/egecan/Code/mahpastes/frontend/css/main.css
- /Users/egecan/Code/mahpastes/frontend/css/modals.css

For each UI component, output:
  Component: [name]
    HTML location: [line numbers in index.html]
    Type: [modal | view | panel | toolbar | popover | dropdown]
    data-testid: [if present]
    Key elements: [buttons, inputs, sections within it]
    Description: [what it shows/does]

Pay special attention to:
- All modals (settings, plugins, metadata, editor, lightbox, shortcuts, watch, serve, api-settings)
- The header bar and its buttons
- The bottom bar and its controls
- The menu drawer and its items
- Context menus and submenus
- Any tooltips or popovers
```

### Agent 2d: Checker-A

**Dispatch as:** Explore subagent

**Prompt:**
```
You are a documentation accuracy checker. Read each doc file listed below and compare it against the actual codebase. For each file, produce an ISSUE REPORT.

Doc files to check:
1. /Users/egecan/Code/mahpastes/docs/docs/intro.md
2. /Users/egecan/Code/mahpastes/docs/docs/getting-started/installation.md
3. /Users/egecan/Code/mahpastes/docs/docs/getting-started/quick-start.md
4. /Users/egecan/Code/mahpastes/docs/docs/getting-started/keyboard-shortcuts.md
5. /Users/egecan/Code/mahpastes/docs/docs/features/clipboard-management.md
6. /Users/egecan/Code/mahpastes/docs/docs/features/tags.md
7. /Users/egecan/Code/mahpastes/docs/docs/features/metadata.md

For each doc, cross-reference against the actual code:
- Go files: /Users/egecan/Code/mahpastes/app.go, database.go, etc.
- JS files: /Users/egecan/Code/mahpastes/frontend/js/*.js
- HTML: /Users/egecan/Code/mahpastes/frontend/index.html

Issue categories:
- WRONG: doc says X, code does Y (include both the doc text and what code actually does)
- MISSING: feature exists in code but not documented (name the feature and where it lives in code)
- OUTDATED: doc describes old behavior that has changed
- REDUNDANT: content duplicated from another doc page

Output format per file:
  File: [path]
  Issues:
    - [LINE N] [CATEGORY]: [description]
    - ...
  Missing features for this file's scope:
    - [feature name]: [code location]
```

### Agent 2e: Checker-B

**Dispatch as:** Explore subagent

**Prompt:**
```
You are a documentation accuracy checker. Read each doc file listed below and compare it against the actual codebase. For each file, produce an ISSUE REPORT.

Doc files to check:
1. /Users/egecan/Code/mahpastes/docs/docs/features/image-editor.md
2. /Users/egecan/Code/mahpastes/docs/docs/features/image-comparison.md
3. /Users/egecan/Code/mahpastes/docs/docs/features/text-editor.md
4. /Users/egecan/Code/mahpastes/docs/docs/features/auto-delete.md
5. /Users/egecan/Code/mahpastes/docs/docs/features/archive.md
6. /Users/egecan/Code/mahpastes/docs/docs/features/watch-folders.md
7. /Users/egecan/Code/mahpastes/docs/docs/features/bulk-actions.md
8. /Users/egecan/Code/mahpastes/docs/docs/features/deduplication.md
9. /Users/egecan/Code/mahpastes/docs/docs/features/backup-restore.md
10. /Users/egecan/Code/mahpastes/docs/docs/features/drag-and-drop.md

For each doc, cross-reference against the actual code:
- Go files: /Users/egecan/Code/mahpastes/app.go, database.go, backup.go, watcher.go, transfer_service.go, etc.
- JS files: /Users/egecan/Code/mahpastes/frontend/js/*.js
- HTML: /Users/egecan/Code/mahpastes/frontend/index.html

Issue categories:
- WRONG: doc says X, code does Y (include both the doc text and what code actually does)
- MISSING: feature exists in code but not documented
- OUTDATED: doc describes old behavior that has changed
- REDUNDANT: content duplicated from another doc page

Output format per file:
  File: [path]
  Issues:
    - [LINE N] [CATEGORY]: [description]
    - ...
  Missing features for this file's scope:
    - [feature name]: [code location]
```

### Agent 2f: Checker-C

**Dispatch as:** Explore subagent

**Prompt:**
```
You are a documentation accuracy checker. Read each doc file listed below and compare it against the actual codebase. For each file, produce an ISSUE REPORT.

Doc files to check:
1. /Users/egecan/Code/mahpastes/docs/docs/plugins/overview.md
2. /Users/egecan/Code/mahpastes/docs/docs/plugins/installing-plugins.md
3. /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/getting-started.md
4. /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/plugin-manifest.md
5. /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/event-handling.md
6. /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/settings-storage.md
7. /Users/egecan/Code/mahpastes/docs/docs/plugins/api-reference.md
8. /Users/egecan/Code/mahpastes/docs/docs/plugins/example-plugins.md
9. /Users/egecan/Code/mahpastes/docs/docs/tutorials/screenshot-workflow.md
10. /Users/egecan/Code/mahpastes/docs/docs/tutorials/code-snippets.md
11. /Users/egecan/Code/mahpastes/docs/docs/tutorials/automated-imports.md
12. /Users/egecan/Code/mahpastes/docs/docs/developers/architecture.md
13. /Users/egecan/Code/mahpastes/docs/docs/developers/frontend.md
14. /Users/egecan/Code/mahpastes/docs/docs/developers/backend.md
15. /Users/egecan/Code/mahpastes/docs/docs/developers/database-schema.md
16. /Users/egecan/Code/mahpastes/docs/docs/developers/api-reference.md
17. /Users/egecan/Code/mahpastes/docs/docs/developers/contributing.md
18. /Users/egecan/Code/mahpastes/docs/docs/reference/data-storage.md
19. /Users/egecan/Code/mahpastes/docs/docs/reference/troubleshooting.md

For each doc, cross-reference against the actual code:
- Plugin system: /Users/egecan/Code/mahpastes/plugin/*.go
- Plugin service: /Users/egecan/Code/mahpastes/plugin_service.go, plugins.go
- All Go files for architecture/backend docs
- All JS files for frontend docs
- database.go for schema docs

Issue categories:
- WRONG: doc says X, code does Y
- MISSING: feature exists in code but not documented
- OUTDATED: doc describes old behavior that has changed
- REDUNDANT: content duplicated from another doc page

Output format per file:
  File: [path]
  Issues:
    - [LINE N] [CATEGORY]: [description]
    - ...
  Missing features for this file's scope:
    - [feature name]: [code location]
```

### Agent 2g: Writing Coach (Audit)

**Dispatch as:** Explore subagent

**Prompt:**
```
You are a writing quality auditor. Read every documentation file listed below and flag every instance of AI-generated slop. Zero tolerance.

Files to audit (read ALL of them):
- /Users/egecan/Code/mahpastes/docs/docs/intro.md
- /Users/egecan/Code/mahpastes/docs/docs/getting-started/installation.md
- /Users/egecan/Code/mahpastes/docs/docs/getting-started/quick-start.md
- /Users/egecan/Code/mahpastes/docs/docs/getting-started/keyboard-shortcuts.md
- /Users/egecan/Code/mahpastes/docs/docs/features/clipboard-management.md
- /Users/egecan/Code/mahpastes/docs/docs/features/image-editor.md
- /Users/egecan/Code/mahpastes/docs/docs/features/image-comparison.md
- /Users/egecan/Code/mahpastes/docs/docs/features/text-editor.md
- /Users/egecan/Code/mahpastes/docs/docs/features/tags.md
- /Users/egecan/Code/mahpastes/docs/docs/features/metadata.md
- /Users/egecan/Code/mahpastes/docs/docs/features/auto-delete.md
- /Users/egecan/Code/mahpastes/docs/docs/features/archive.md
- /Users/egecan/Code/mahpastes/docs/docs/features/watch-folders.md
- /Users/egecan/Code/mahpastes/docs/docs/features/bulk-actions.md
- /Users/egecan/Code/mahpastes/docs/docs/features/deduplication.md
- /Users/egecan/Code/mahpastes/docs/docs/features/backup-restore.md
- /Users/egecan/Code/mahpastes/docs/docs/features/drag-and-drop.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/overview.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/installing-plugins.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/getting-started.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/plugin-manifest.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/event-handling.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/writing-plugins/settings-storage.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/api-reference.md
- /Users/egecan/Code/mahpastes/docs/docs/plugins/example-plugins.md
- /Users/egecan/Code/mahpastes/docs/docs/tutorials/screenshot-workflow.md
- /Users/egecan/Code/mahpastes/docs/docs/tutorials/code-snippets.md
- /Users/egecan/Code/mahpastes/docs/docs/tutorials/automated-imports.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/architecture.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/frontend.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/backend.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/database-schema.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/api-reference.md
- /Users/egecan/Code/mahpastes/docs/docs/developers/contributing.md
- /Users/egecan/Code/mahpastes/docs/docs/reference/data-storage.md
- /Users/egecan/Code/mahpastes/docs/docs/reference/troubleshooting.md

SLOP RULES (flag ALL of these):
1. Filler phrases: "It's worth noting", "seamlessly", "leverage", "robust", "powerful", "comprehensive", "streamlined", "effortlessly", "intuitive"
2. Over-explanation: explaining obvious things, restating what was just said, padding with context the reader doesn't need
3. Hedging: "you may want to consider", "depending on your needs", "this can be particularly useful when", "feel free to"
4. Marketing language: "elegant", "beautiful", "best-in-class", selling the product instead of documenting it
5. Unnecessary transitions: "Now that we've covered X, let's move on to Y", "As mentioned earlier"
6. Passive voice where active is clearer
7. Redundant modifiers: "completely remove", "fully support", "easily configure"

Output format per file:
  File: [path]
  Slop instances:
    - LINE [N]: "[quoted text]" → REASON: [which rule it violates] → SUGGESTED: "[tighter rewrite]"

If a file is clean, say so. Do not invent issues.
```

**Step: Collect all 7 agent outputs**

Wait for all agents to complete. Save each output — you'll need them for Task 3.

---

## Task 3: Consolidate Master Audit Report

**Purpose:** Merge all 7 agent outputs into a single actionable document.

**Files:**
- Create: `docs/plans/2026-03-07-master-audit-report.md`

**Step 1: Build feature inventory**

Merge outputs from Summarizer-Go, Summarizer-JS, and Summarizer-HTML into a deduplicated feature list. For each feature, mark documentation status:

```markdown
## Feature Inventory

| Feature | Backend | Frontend | Doc Status | Doc File |
|---------|---------|----------|------------|----------|
| Clipboard paste | App.UploadClip | app.js | Documented | features/clipboard-management.md |
| Tag-serve | ServeService.* | serve.js | UNDOCUMENTED | — |
| Open With | App.GetOpenWithApps, App.OpenClipWith | ui.js | UNDOCUMENTED | — |
| Tooltips | — | tooltip.js | UNDOCUMENTED | — |
| ... | ... | ... | ... | ... |
```

**Step 2: Merge issue reports**

Combine Checker-A, Checker-B, and Checker-C outputs. Group by file. Deduplicate.

**Step 3: Merge slop report**

Add Writing Coach slop instances under each file's section.

**Step 4: Create work assignments**

Based on the audit, create the fix assignments:

```markdown
## Phase 2 Work Assignments

### Writer-A (7 files)
- docs/docs/intro.md: [list of issues to fix]
- docs/docs/getting-started/installation.md: [issues]
- docs/docs/getting-started/quick-start.md: [issues]
- docs/docs/getting-started/keyboard-shortcuts.md: [issues]
- docs/docs/features/clipboard-management.md: [issues + add Open With, tooltips]
- docs/docs/features/tags.md: [issues]
- docs/docs/features/metadata.md: [issues]

### Writer-B (10 files)
- docs/docs/features/image-editor.md: [issues]
- ... (remaining features/*)

### Writer-C (19 files)
- docs/docs/plugins/overview.md: [issues]
- ... (plugins/*, tutorials/*, developers/*, reference/*)

### Writer-New
- Create: docs/docs/features/tag-serve.md
- Create: docs/docs/features/rest-api.md (if audit shows it warrants separate page)
- Update: docs/sidebars.js
- Update: docs/docs/intro.md feature table (add new features)
```

**Step 5: Save and commit**

Run:
```bash
git add docs/plans/2026-03-07-master-audit-report.md
git commit -m "docs: add master audit report from Phase 1 agents"
```

---

## Task 4: Phase 2 — Dispatch Writer Agents (4 parallel)

**Purpose:** Fix all documented issues. Each writer gets their file list and the relevant section of the Master Audit Report.

### Agent 4a: Writer-A

**Dispatch as:** Code subagent

**Prompt:**
```
You are a documentation writer. Fix the issues listed below in each file. Rules:

WRITING RULES:
- Fix factual errors to match code behavior
- Add missing features to appropriate sections
- Remove or rewrite outdated content
- Cut redundancy
- Do NOT do prose style rewrites — a writing coach handles that separately
- Keep existing structure (headings, tables, screenshots) unless the structure itself is wrong
- When adding new content, match the style of surrounding content
- Verify your changes against the actual code before writing

FILES AND ISSUES:
[Paste the Writer-A section from the Master Audit Report here]

IMPORTANT: For each file, read the CURRENT content first, then apply fixes. Do not rewrite files from scratch.
```

### Agent 4b: Writer-B

**Dispatch as:** Code subagent

**Prompt:**
```
You are a documentation writer. Fix the issues listed below in each file.

[Same writing rules as Writer-A]

FILES AND ISSUES:
[Paste the Writer-B section from the Master Audit Report here]
```

### Agent 4c: Writer-C

**Dispatch as:** Code subagent

**Prompt:**
```
You are a documentation writer. Fix the issues listed below in each file.

[Same writing rules as Writer-A]

FILES AND ISSUES:
[Paste the Writer-C section from the Master Audit Report here]
```

### Agent 4d: Writer-New

**Dispatch as:** Code subagent

**Prompt:**
```
You are a documentation writer. Create new documentation pages for undocumented features.

WRITING RULES:
- Match the frontmatter pattern of existing docs (sidebar_position, title)
- Match the structure of existing feature pages (intro sentence, sections with ## headings, tables for reference data, screenshot references, Related links at bottom)
- Write factual, terse prose. No filler. No marketing language.
- Cross-reference against the actual code for accuracy
- Include screenshot references using ![alt](/img/screenshots/filename.png) — the screenshots will be created separately

NEW PAGES TO CREATE:
[Paste the Writer-New section from the Master Audit Report here]

ALSO UPDATE:
1. /Users/egecan/Code/mahpastes/docs/sidebars.js — add new page IDs to the appropriate category
2. /Users/egecan/Code/mahpastes/docs/docs/intro.md — add new features to the "Features at a Glance" table

Reference code files:
- Tag-serve backend: /Users/egecan/Code/mahpastes/serve_manager.go, serve_service.go
- Tag-serve frontend: /Users/egecan/Code/mahpastes/frontend/js/serve.js
- REST API backend: /Users/egecan/Code/mahpastes/api_manager.go
- REST API frontend: /Users/egecan/Code/mahpastes/frontend/js/api-settings.js
- HTML structure: /Users/egecan/Code/mahpastes/frontend/index.html
```

**Step: Wait for all 4 writers to complete, then commit**

Run:
```bash
git add docs/docs/ docs/sidebars.js
git commit -m "docs: fix inaccuracies and add missing feature documentation"
```

---

## Task 5: Phase 2 — Retake All Screenshots

**Purpose:** Capture fresh screenshots that reflect current UI state.

**Step 1: Check if new screenshot tests are needed**

Read `/Users/egecan/Code/mahpastes/e2e/tests/screenshots/capture.spec.ts`.

Check if the following UI states have screenshot captures:
- Context menu with submenus (Open With)
- Tag-serve view
- REST API settings modal
- Tooltip visible on a button

If missing, add new screenshot sections to `capture.spec.ts` following the existing pattern (see the file for examples — each section sets up state, waits, then calls `page.screenshot()`).

**Step 2: Run screenshot capture**

Run: `cd /Users/egecan/Code/mahpastes && make screenshots`
Expected: All screenshots saved to `docs/static/img/screenshots/`.

**Step 3: Verify all screenshot references resolve**

Run:
```bash
grep -roh 'img/screenshots/[^)]*' docs/docs/ | sort -u | while read ref; do
  if [ ! -f "docs/static/$ref" ]; then echo "MISSING: $ref"; fi
done
```
Expected: No output (all references resolve).

**Step 4: Commit**

Run:
```bash
git add e2e/tests/screenshots/ docs/static/img/screenshots/
git commit -m "docs: retake all screenshots and add new captures"
```

---

## Task 6: Phase 2 — Writing Coach Final Pass

**Purpose:** Eliminate all AI slop from the edited docs. Sequential — runs after all writers finish.

**Dispatch as:** Code subagent

**Prompt:**
```
You are a writing coach. Read every documentation file below and rewrite any AI slop. You are the final authority on prose quality.

RULES (zero tolerance):
- Kill filler phrases: "It's worth noting", "seamlessly", "leverage", "robust", "powerful", "comprehensive", "streamlined", "effortlessly", "intuitive"
- Kill over-explanation: if the reader can figure it out from context, don't explain it
- Kill hedging: "you may want to", "depending on your needs", "this can be particularly useful", "feel free to"
- Kill marketing language: "elegant", "beautiful", this is documentation not a sales page
- Kill unnecessary transitions: "Now that we've covered X", "As mentioned earlier"
- Kill passive voice where active is clearer
- Kill redundant modifiers: "completely remove", "fully support", "easily configure"
- Prefer short declarative sentences
- DO NOT change technical content, code examples, tables, or screenshot references
- DO NOT restructure sections or move content between files
- Only change the prose itself

FILES TO REVIEW AND EDIT:
[List ALL 36+ doc files — the original 36 plus any new ones created by Writer-New]

For each file:
1. Read the current content
2. Edit in place — fix slop, leave everything else untouched
3. If a file is already clean, do not touch it
```

**Step: Commit after writing coach finishes**

Run:
```bash
git add docs/docs/
git commit -m "docs: writing coach pass — eliminate AI slop"
```

---

## Task 7: Final Verification

**Purpose:** Verify everything works together.

**Step 1: Docusaurus build**

Run: `cd /Users/egecan/Code/mahpastes/docs && npm run build`
Expected: Build succeeds with no errors or warnings.

**Step 2: Check for broken cross-links**

Run:
```bash
grep -roh '\[.*\](\.\/[^)]*\.md)' docs/docs/ | sed 's/.*(\(.*\))/\1/' | sort -u | while read ref; do
  found=0
  for dir in docs/docs docs/docs/features docs/docs/getting-started docs/docs/plugins docs/docs/plugins/writing-plugins docs/docs/tutorials docs/docs/developers docs/docs/reference; do
    if [ -f "$dir/$ref" ]; then found=1; break; fi
  done
  if [ $found -eq 0 ]; then echo "BROKEN LINK: $ref"; fi
done
```
Expected: No broken links.

**Step 3: Verify screenshot references**

Run:
```bash
grep -roh 'img/screenshots/[^)]*' docs/docs/ | sort -u | while read ref; do
  if [ ! -f "docs/static/$ref" ]; then echo "MISSING SCREENSHOT: $ref"; fi
done
```
Expected: No missing screenshots.

**Step 4: Verify sidebars.js includes all doc files**

Run:
```bash
find docs/docs -name '*.md' | sed 's|docs/docs/||; s|\.md||' | sort > /tmp/doc-files.txt
grep -oE "'[^']+'" docs/sidebars.js | tr -d "'" | sort > /tmp/sidebar-refs.txt
diff /tmp/doc-files.txt /tmp/sidebar-refs.txt
```
Expected: All doc files appear in sidebars (intro is referenced differently so may show as a diff — that's ok).

**Step 5: Final commit if any fixes needed**

If any verification step reveals issues, fix them and commit:
```bash
git add docs/
git commit -m "docs: fix verification issues (broken links, missing refs)"
```

---

## Task 8: Summary Commit

**Purpose:** Tag the completed documentation refresh.

**Step 1: Verify clean state**

Run: `git status`
Expected: Clean working tree, all changes committed.

**Step 2: Review commit log**

Run: `git log --oneline -10`
Expected: See the sequence of doc commits from this work.
