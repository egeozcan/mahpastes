# Docs Overhaul Design

## Goal

Bring all Docusaurus docs (`docs/`) to 100% accuracy, zero AI-slop, and full screenshot coverage.

## Scope

The entire `docs/` directory: 34+ markdown files, sidebars config, screenshots, custom CSS.

Target audiences: end users, plugin developers, contributors.

## Team Composition

Six agents in a two-phase structure.

### Phase 1: Parallel Read-Only Audit

**tech-summarizer-1** — Reads Go backend code. Produces ground truth feature inventory for: clipboard management, drag-out, auto-delete, archive, watch folders, backup/restore, bulk actions, transfer system.

**tech-summarizer-2** — Same role, different scope: plugin system (manifest, APIs, events, actions, modals, scheduling), tags, image editor, image comparison, text editor, keyboard shortcuts, settings, developer-facing internals (architecture, frontend, backend, database schema).

**doc-checker-1** — Reads all 34 docs. Flags: factual inaccuracies, outdated info ("not yet implemented" for shipped features), missing features, structural issues, dead links, inconsistent terminology.

**doc-checker-2** — Reads all docs from coverage angle: features with no docs, missing screenshots, docs that don't match current codebase, sidebar organization issues, cross-reference problems.

### Phase 2: Sequential Edits

**writing-coach** — Takes combined Phase 1 reports. Rewrites every flagged doc. Enforces anti-slop rules. Adds missing docs for undocumented features. Fixes all factual issues.

**screenshot-updater** — Audits which pages need screenshots. Updates `e2e/tests/screenshots/capture.spec.ts` with new scenarios. Runs capture to regenerate all screenshots. Verifies screenshots match docs.

### Conductor (team lead)

Creates team, manages task board, coordinates phase transitions, merges Phase 1 reports, reviews final output.

## Anti-Slop Rules

### Banned Words/Phrases

leverage, seamlessly, robust, empower, cutting-edge, harness, elevate, streamline, game-changer, unlock, dive into, delve, explore (as filler), comprehensive, it's worth noting, importantly, furthermore, additionally, in conclusion, let's

### Style Rules

- Lead with what the feature does, not why it matters
- One idea per paragraph
- Delete any sentence that doesn't add information
- Prefer imperative ("Click X") over "You can click X"
- No excited tone — factual, direct, slightly dry
- Tables over prose for reference data
- Code examples over descriptions when possible
- No unnecessary "best practices" padding
- Each doc should sound like a human wrote it, not a template

## Workflow

```
Phase 1 (parallel, read-only):
  tech-summarizer-1 ──┐
  tech-summarizer-2 ──┤── produce reports ──→ Conductor collects
  doc-checker-1     ──┤
  doc-checker-2     ──┘

Conductor: merges reports into combined doc-fixes task list

Phase 2 (sequential edits):
  writing-coach ──→ rewrites all docs
  screenshot-updater ──→ updates capture.spec.ts + regenerates screenshots

Conductor: final review
```

## Deliverables

- All 34+ docs updated/rewritten to be accurate and slop-free
- New docs for any undocumented features
- Updated `e2e/tests/screenshots/capture.spec.ts` with new screenshot scenarios
- Fresh screenshots for all doc pages
- Updated `sidebars.js` if new pages added
- Updated `docs/src/css/custom.css` if needed

## Known Issues to Fix

- `clipboard-management.md` line 48: says "expiration UI is not yet implemented" but it ships (auto-delete.md documents it fully)
- `intro.md`: says "macOS" only in system requirements, but Windows support exists (via PowerShell clipboard)
- Several features added since last doc update (Feb 22): tooltips, metadata/sorting, bottom bar, deduplication, plugin global actions, drag-out improvements
- Screenshots may be stale (last captured Feb 22)
