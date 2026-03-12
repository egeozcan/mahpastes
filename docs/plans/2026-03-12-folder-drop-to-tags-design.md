# Folder Drop to Tags Design

**Date:** 2026-03-12

## Summary

Drag-and-drop a folder into mahpastes to map its directory structure as tag hierarchy relative to the current folder context.

## Behavior by Mode

- **Folder mode:** Directory tree becomes tags relative to current folder context. Files tagged with their immediate folder. All intermediate tags created (even empty ones).
- **Non-folder mode:** Folder structure ignored. Files flattened and uploaded as individual clips without tags (existing behavior).

## Drop Flow

1. `drop` event — detect directories via `webkitGetAsEntry()`
2. Recursively traverse, collecting `{relativePath, file}` entries
3. Skip dotfiles/dotfolders always. Parse `.gitignore` in dropped folder root (if present) and skip matching entries. Basic glob matching only — no negation or nested `.gitignore` support.
4. **Warning dialog** (before upload):
   - &gt; 50 files — warn about file count
   - &gt; 5 levels deep + folder mode — warn about nesting depth
   - Both — combined warning
   - &lt;= 50 files and &lt;= 5 deep (or not folder mode) — no dialog
5. If folder mode: group files by immediate folder path, create tags parent-first, call `UploadFiles` per group
6. If not folder mode: strip paths, pass flat file list to existing `handleFiles`

## Technical Approach

**Frontend directory traversal via `webkitGetAsEntry`** — all new logic lives in the frontend. No backend changes needed.

### Directory Traversal

New function in `app.js` or `wails-api.js`:
- Use `DataTransferItem.webkitGetAsEntry()` to detect directories
- Recursively walk via `DirectoryReader.readEntries()`
- Return `{relativePath, file}[]` list

### Ignore Rules

- Always skip dotfiles and dotfolders (`.DS_Store`, `.git`, etc.)
- If a `.gitignore` file exists in the dropped folder root, parse it for basic glob patterns (exact names like `node_modules`, simple globs like `*.pyc`, directory patterns like `build/`)
- No support for negation (`!`), nested `.gitignore` files, or complex patterns

### Tag Creation (Folder Mode Only)

- Determine base tag prefix from active folder context (e.g., viewing `a` → prefix is `a/`)
- Collect unique directory paths from entries
- Create tags parent-first using existing `CreateTag` (handles duplicates gracefully)
- Group files by immediate folder tag, call `UploadFiles(group, expiration, tagID)` per group

### Confirmation Dialog

- Trigger: dropping any folder (regardless of mode)
- Threshold: > 50 files for count warning, > 5 nesting levels for depth warning (folder mode only)
- Combined warning when both apply
- Promise-based custom dialog (no browser `confirm()` in Wails)

## Edge Cases

- **Empty folders:** Tags created, no clips uploaded. Ignored in non-folder mode.
- **Mixed drops (files + folders):** Loose files get current folder tag (existing autoTagID). Folders traversed and mapped. In non-folder mode, everything flattens.
- **Duplicate tag names:** `CreateTag` returns existing tag — safe to re-drop same structure.
- **Hidden files:** Skipped during traversal.

## Decisions Made

- All intermediate tags created even if no files at that level (mirrors full folder structure)
- Files tagged with immediate folder only (not all ancestors) — matches folder mode semantics and tree exclusivity
- Folder-to-tag mapping only in folder mode; non-folder mode flattens
- `.gitignore` parsing for ignore rules (no custom settings list)
- Confirmation dialog applies to all folder drops (file count), nesting warning only in folder mode
