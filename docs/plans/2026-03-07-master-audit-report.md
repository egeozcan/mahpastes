# Master Audit Report — Documentation Review

## Feature Inventory: Documentation Status

### Fully Documented
- Clipboard paste/upload/storage
- Clip deletion, archiving, expiration
- Tag management (CRUD, filtering, hidden tags, bulk)
- Image editor (brush, shapes, text, undo/redo)
- Image comparison (fade, slider, diff modes)
- Text editor
- Watch folders
- Deduplication
- Backup & restore
- Drag-and-drop / transfer system
- Bulk actions
- Metadata (key-value pairs)
- Plugin system (overview, install, manifest, events, storage, API reference, examples)
- Keyboard shortcuts
- Settings
- Search/filtering
- Sorting

### Undocumented (need new pages or sections)
- **Tag Serve** — per-tag HTTP servers serving clips as browsable file listings (ServeService, serve_manager.go, serve.js)
- **REST API** — authenticated JSON API with Bearer token auth, role-based access, 14 endpoints (APIService, api_manager.go, api-settings.js)
- **Open With** — open clips in external applications (App.OpenClipWithDefaultApp, App.OpenClipWithApp, App.ChooseApplication in app.go, context menu submenu in ui.js)
- **Tooltips toggle** — show/hide tooltips setting (settings.js, App.GetSetting/SetSetting)

### Partially Documented (context menu changes)
- **Context menu restructured with submenus** — clipboard-management.md describes old flat menu, missing Open, Open With, Metadata, Merge Duplicates, Set/Cancel Expiration

---

## Issues by File

### docs/docs/intro.md
- LINE 8: **WRONG** — Says "macOS and Windows" but Linux is also supported (experimentally). Contradicts system requirements table at line 79-83.
- **MISSING** — Features table does not list Tag Serve, REST API, Open With, or Tooltips.

### docs/docs/getting-started/keyboard-shortcuts.md
- **MISSING** — "Open Serve View" shortcut (S key, gallery context) not listed. Registered in app.js line 530-532.

### docs/docs/features/clipboard-management.md
- LINE 58: **SLOP** — "Clips display in a responsive grid" → should be "Clips display in a grid"
- LINE 81: **OUTDATED** — Lightbox actions menu description lists "Copy Path, Copy File, Copy Contents, Save, Edit, Tags, Archive, Delete" but actual menu also includes Open, Open With (submenu), Metadata, Merge Duplicates, Set Expiration, Cancel Expiration.
- **MISSING** — No mention of Open With feature (right-click → Open With → app list)
- **MISSING** — No mention of context menu submenus

### docs/docs/features/image-comparison.md
- LINE 94: **WRONG** — Says "The bottom image is forced to 1000x1000px with object-fit: fill" but code uses 100% width/height of container, not hardcoded 1000x1000px.
- LINE 56: **WRONG** — Says threshold range is 1-100 but the UI slider min is 0. Backend clamps <1 to 1, but user can set slider to 0.

### docs/docs/plugins/writing-plugins/getting-started.md
- LINE 99: **WRONG** — `tags.add_to_clip(clip.id, get_or_create_tag("images"))` has reversed parameter order. Correct: `tags.add_to_clip(tag_id, clip_id)` per API spec and code.

### docs/docs/developers/architecture.md
- **MISSING** — Backend components list doesn't include serve_manager.go, serve_service.go, api_manager.go, or api_service.go.
- **MISSING** — Frontend components list doesn't include serve.js, api-settings.js, or tooltips.js (if it exists as separate module).

### docs/docs/getting-started/quick-start.md
- LINE 51: **TERMINOLOGY** — Uses "Download" but the UI/code uses "Save" for this action.

### All other files (28 files)
No issues found.

---

## Slop Report

Only 1 instance found across all 36 files:
- **clipboard-management.md LINE 58**: "responsive grid" → "grid"

The documentation is otherwise clean of AI-generated filler, hedging, marketing language, and over-explanation.

---

## Phase 2 Work Assignments

### Writer-A (7 files)

**docs/docs/intro.md**
- Fix LINE 8: change "macOS and Windows" to "macOS, Windows, and Linux"
- Add Tag Serve, REST API, Open With to Features table

**docs/docs/getting-started/installation.md**
- No changes needed

**docs/docs/getting-started/quick-start.md**
- Fix LINE 51: "Download" → "Save" to match UI

**docs/docs/getting-started/keyboard-shortcuts.md**
- Add "Open Serve View" shortcut (S key, gallery context)

**docs/docs/features/clipboard-management.md**
- Fix LINE 58: "responsive grid" → "grid"
- Fix LINE 81: Update lightbox actions menu to include all items (Open, Open With, Copy Path, Copy File, Copy Contents, Save, Edit, Tags, Metadata, Set/Cancel Expiration, Merge Duplicates, Archive, Delete)
- Add section about Open With feature
- Add note about context menu submenus

**docs/docs/features/tags.md**
- No changes needed

**docs/docs/features/metadata.md**
- No changes needed

### Writer-B (10 files)

**docs/docs/features/image-comparison.md**
- Fix LINE 94: Remove "1000x1000px" claim, describe actual behavior (100% of container)
- Fix LINE 56: Change range from "1-100" to "0-100" (or note that backend clamps <1)

**docs/docs/features/image-editor.md** — No changes
**docs/docs/features/text-editor.md** — No changes
**docs/docs/features/auto-delete.md** — No changes
**docs/docs/features/archive.md** — No changes
**docs/docs/features/watch-folders.md** — No changes
**docs/docs/features/bulk-actions.md** — No changes
**docs/docs/features/deduplication.md** — No changes
**docs/docs/features/backup-restore.md** — No changes
**docs/docs/features/drag-and-drop.md** — No changes

### Writer-C (19 files)

**docs/docs/plugins/writing-plugins/getting-started.md**
- Fix LINE 99: Correct parameter order to `tags.add_to_clip(tag_id, clip_id)`

**docs/docs/developers/architecture.md**
- Add serve_manager.go, serve_service.go, api_manager.go to backend components
- Add serve.js, api-settings.js to frontend components

**All other 17 files** — No changes needed

### Writer-New

**Create: docs/docs/features/tag-serve.md**
- Document per-tag HTTP servers
- Reference code: serve_manager.go, serve_service.go, serve.js
- Cover: starting/stopping servers, port config, local vs network bind, browsable file listings, JSON API
- Add sidebar_position after drag-and-drop

**Create: docs/docs/features/rest-api.md**
- Document REST API with auth
- Reference code: api_manager.go, api-settings.js
- Cover: server lifecycle, API key management (roles: viewer/editor/admin), tag-scoped keys, all 14 endpoints, authentication
- Add sidebar_position after tag-serve

**Update: docs/sidebars.js**
- Add 'features/tag-serve' and 'features/rest-api' to Features category

**Update: docs/docs/intro.md**
- Add Tag Serve and REST API to Features at a Glance table (Writer-A handles this)

### Screenshot Updater
- Retake all 21 existing screenshots via `make screenshots`
- Add new screenshot tests for: serve view, API settings modal
- Verify all screenshot references resolve
