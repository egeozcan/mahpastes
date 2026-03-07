# Documentation Audit and Rewrite

## Goal

Bring all 30+ documentation files in `docs/docs/` to parity with the current codebase. Eliminate AI-generated prose. Retake all screenshots. Document missing features.

## Scope

All files in `docs/docs/` across all 6 categories (getting-started, features, plugins, tutorials, developers, reference), plus `sidebars.js` and screenshot assets.

## Approach: Two-Phase Audit → Fix

### Phase 1: Audit (parallel)

Seven agents run simultaneously:

**Technical Summarizers (3)** — scan code, produce a feature inventory.

| Agent | Scope |
|-------|-------|
| Summarizer-Go | `app.go`, `database.go`, `watcher.go`, `plugins.go`, `clipboard_*.go`, `transfer_*.go`, `tag_serve*.go`, `plugin/` |
| Summarizer-JS | `frontend/js/*.js` |
| Summarizer-HTML | `frontend/index.html`, `frontend/css/`, settings modal, shortcuts |

Each produces structured entries:
```
Feature: Open With
  Backend: App.GetOpenWithApps(), App.OpenClipWith()
  Frontend: context menu submenu in ui.js
  UI entry: right-click clip → Open With → [app list]
  Doc coverage: NONE
```

**Doc Checkers (3)** — read assigned docs, flag issues.

| Agent | Files |
|-------|-------|
| Checker-A | intro, getting-started/*, features/clipboard-management, features/tags, features/metadata |
| Checker-B | features/* (remaining 10 pages) |
| Checker-C | plugins/*, tutorials/*, developers/*, reference/* |

Issue categories: factually wrong, missing feature, outdated, irrelevant/redundant, structural.

**Writing Coach (1)** — reads all 30 files, produces a slop report with line numbers.

Slop rules (zero tolerance):
- No filler phrases ("It's worth noting", "seamlessly", "leverage", "robust", "powerful")
- No over-explanation of obvious things
- No hedging ("you may want to", "depending on your needs", "this can be particularly useful")
- No restating what was just said
- No padding or unnecessary context
- Short declarative sentences preferred

### Consolidation

The conductor merges all Phase 1 output into a Master Audit Report:
- Feature inventory with documentation status (documented / undocumented / partially documented)
- Issues by file (from checkers)
- Slop instances by file (from writing coach)

This report is the work order for Phase 2.

### Phase 2: Fix (parallel → sequential tail)

**Writers (4, parallel)** — edit docs based on the audit report.

| Agent | Files |
|-------|-------|
| Writer-A | intro, getting-started/*, features/clipboard-management, features/tags, features/metadata |
| Writer-B | remaining features/* (10 pages) |
| Writer-C | plugins/*, tutorials/*, developers/*, reference/* |
| Writer-New | new pages (tag-serve REST API, others as audit reveals) |

Writer rules:
- Fix factual errors to match code behavior
- Add missing features to appropriate pages
- Remove or rewrite outdated content
- Cut redundancy (one home per topic)
- Do NOT rewrite prose style — that's the writing coach's job

Writer-New also updates `sidebars.js` for any new pages.

**Screenshot Updater (1, parallel with writers):**
- Run `make build` as pre-check
- Run `make screenshots` to retake all 21 screenshots
- Check if new features (tag-serve, Open With, tooltips) need new screenshot tests
- Add new Playwright screenshot tests if needed, then re-run
- Verify all screenshot references in docs point to existing files

**Writing Coach (1, sequential after writers):**
- Reads every edited file
- Kills any slop the writers introduced
- Tightens sentences, ensures consistent voice
- Does NOT change technical content

**Conductor final review:**
- No file conflicts or broken cross-links
- `sidebars.js` includes new pages
- `cd docs && npm run build` succeeds with no warnings
- All screenshot references resolve

## Known Features Needing Documentation

From git log since March 4 (last docs update):
- Tag-serve HTTP servers + REST API with key-based auth → new page
- Open With + context menu submenus → fold into clipboard-management.md
- Tooltips system with settings toggle → fold into relevant pages
- Card footer tooltip fixes → no doc impact, UI-only

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Audit misses a feature | Inventory built from code, not docs |
| Writer introduces new slop | Writing coach final pass |
| Screenshot tests miss new UI | Screenshot updater adds test cases |
| New page breaks sidebar | Conductor runs Docusaurus build |
| Tag-serve too new to document | Confirm with user before writing |

## Definition of Done

1. Every codebase feature has corresponding documentation
2. Zero AI slop across all files (writing coach sign-off)
3. All screenshots are fresh and match current UI
4. `cd docs && npm run build` succeeds clean
5. No broken cross-links
6. `sidebars.js` includes any new pages
