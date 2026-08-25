# mahpastes

A Wails desktop clipboard manager for macOS, Windows, and Linux with image editing, comparison, and watch folder features.

## Wails v3 migration

- Current: Wails v2.12.0. All Wails runtime calls are routed through `internal/wailsbridge` — the only package in this module that imports `github.com/wailsapp/wails/v2/pkg/runtime`. `TestNoRuntimeImportOutsideBridge` enforces that invariant. Do NOT add a direct runtime import elsewhere.
- Future v3 swap: see `docs/WAILS_V3_MIGRATION.md` for the execution playbook and `docs/WAILS_V3_FRONTEND_CONTRACT.md` for the frontend-shim spec. Do not start until v3 reaches stable or a trusted RC — it's still alpha at v3.0.0-alpha.77.

## Tech Stack

- **Backend**: Go with Wails framework
- **Frontend**: Vanilla JavaScript + Tailwind CSS (no build step except Tailwind)
- **Database**: SQLite (via Wails)
- **Testing**: Playwright e2e tests

## Development Setup

- **Wails CLI**: `~/go/bin/wails`
- Use `make` targets for common operations:

```bash
make dev          # Start dev server with hot reload
make build        # Clean production build
make clean        # Remove build artifacts
make install      # Build, kill running app, install to /Applications, launch
make uninstall    # Remove from /Applications
make bindings     # Regenerate frontend bindings after Go changes
make test         # Run e2e tests
make test-headed  # Run e2e tests with visible browser
make test-debug   # Run e2e tests with Playwright inspector
make screenshots  # Capture documentation screenshots
make mp            # Build mp CLI for current platform
make mp-install    # Install mp to a user bin dir (or GOBIN if set)
make mp-cross      # Cross-compile mp for all platforms
make help         # Show all targets
```

## E2E Testing Requirements

**CRITICAL: All changes MUST pass e2e tests. Run tests before and after any modification.**

### Test Workflow

1. **Before starting work**: Run `cd e2e && npm test` to verify baseline
2. **Fix any failing tests first**: Even if unrelated to your changes, fix them before proceeding
3. **After making changes**: Run tests again and ensure all pass
4. **Add tests for new functionality**: Every feature/bugfix must have corresponding test coverage

### Running Tests

```bash
cd e2e
npm test              # Run all tests
npm run test:headed   # Run with browser visible (for debugging)
npm run test:debug    # Debug mode with Playwright inspector
npm run test:ui       # Interactive UI mode
```

**IMPORTANT**: When running the full test suite, always pipe through `| tail -N` (e.g. `tail -50`) to avoid truncated output. The full output is very long.

### Test Organization

Tests are organized by feature in `e2e/tests/`:
- `backup/` - Backup and restore operations
- `bulk/` - Multi-select operations
- `clips/` - Upload, view, delete, archive operations
- `edge-cases/` - Error handling, expiration
- `folder-drag/` - Folder drag operations
- `images/` - Lightbox, editor, comparison
- `metadata/` - Clip metadata CRUD
- `plugins/` - Plugin system (install, events, APIs, scheduling)
- `search/` - Filtering functionality
- `screenshots/` - Documentation screenshot capture
- `serve/` - Tag HTTP serving and API features
- `shortcuts/` - Keyboard shortcut tests
- `sort/` - Clip sorting functionality
- `tags/` - Tag CRUD, filtering, hidden tags, folder mode, tree exclusivity
- `watch/` - Watch folders feature

### Test Abstractions

Use the established patterns in the codebase for consistency:

**AppHelper fixture** (`e2e/fixtures/test-fixtures.ts`): Provides high-level methods for all app interactions:
```typescript
// Example usage
test('should upload and delete a clip', async ({ app }) => {
  const imagePath = await createTempFile(generateTestImage(), 'png');
  await app.uploadFile(imagePath);
  await app.expectClipCount(1);
  await app.deleteClip(path.basename(imagePath));
  await app.expectClipCount(0);
});
```

**Test data helpers** (`e2e/helpers/test-data.ts`): Generate test files:
- `generateTestImage(width, height, color)` - Creates PNG images
- `generateTestText(prefix)` - Creates text content
- `generateTestJSON()` / `generateTestHTML()` - Structured content
- `createTempFile(content, extension)` - Writes to temp location

**Selectors** (`e2e/helpers/selectors.ts`): Centralized DOM selectors - use these instead of hardcoding selectors in tests.

### Writing Good Tests

- Use descriptive test names that explain the behavior being tested
- One assertion focus per test when possible
- Use `app.expectClipVisible()`, `app.expectClipCount()` for assertions
- Clean up is automatic via the fixture's `afterEach`
- Tests run in parallel - each worker gets its own app instance

## Design System

**CRITICAL: All UI changes must match the existing design language exactly.**

### Color Palette (Stone-based)

The app uses Tailwind's `stone` color scale exclusively:
- **Background**: `bg-stone-50` (main), `bg-white` (cards)
- **Text**: `text-stone-800` (primary), `text-stone-600` (secondary), `text-stone-400` (muted)
- **Borders**: `border-stone-200` (default), `border-stone-300` (hover)
- **Interactive**: `bg-stone-800` (buttons), `hover:bg-stone-700`
- **Accents**: Only `stone` variants - no blue, green, or other colors except:
  - Error states: `red-500`, `red-50`
  - Success indicator: `emerald-500` (watch/serve indicator)
  - Warnings: `amber-50`, `amber-100`, `amber-200`, `amber-500`, `amber-700` (backup restore, shortcut conflicts, write permission badges)
  - Info: `blue-100`, `blue-700` (read permission badges)

### Typography

- **Font**: IBM Plex Mono (monospace throughout)
- **Sizes**:
  - Headers: `text-sm font-semibold uppercase tracking-wide`
  - Body: `text-xs font-medium`
  - Micro: `text-[9px]`, `text-[10px]`, `text-[11px]` for labels/badges

### Component Patterns

**Buttons**:
```html
<!-- Primary -->
<button class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 px-5 rounded-md transition-colors">

<!-- Secondary -->
<button class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">
```

**Cards**:
```html
<li class="bg-white rounded-md border border-stone-200 overflow-hidden flex flex-col transition-all duration-150 hover:border-stone-300 relative group">
```

**Icon buttons** (action buttons in cards):
```html
<button class="p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors">
```

**Form inputs**:
```html
<input class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors">
```

### Icons

- Use inline SVG with `stroke="currentColor"` and `stroke-width="1.5"`
- Standard size: `w-4 h-4` with `opacity-60` when paired with text
- Small: `w-3 h-3` for action buttons

### Accessibility

- Use semantic HTML (`<header>`, `<main>`, `<nav>`, `<section>`)
- Include `aria-label`, `aria-pressed`, `role` attributes
- Support keyboard navigation (focus states, tab order)
- Screen reader text with `sr-only` class

### Animation

- Use `transition-all duration-150` or `duration-200` for interactions
- `cubic-bezier(0.4, 0, 0.2, 1)` timing (set globally in CSS)
- Hover effects: `hover:scale-[1.02]` for subtle zoom

## File Structure

```
mahpastes/
├── Makefile              # Build, install, test targets
├── app.go                # Main Wails app logic
├── api_manager.go        # REST API server (/api/v1/*) for mp CLI
├── api_service.go        # API management Wails binding (start/stop/keys)
├── app_transfer_helpers.go # Bridge between App and TempClipStore
├── backup.go             # ZIP backup and restore
├── clipboard_service.go  # Clipboard copy service (separate struct)
├── clipboard_darwin.go   # macOS clipboard via NSPasteboard (CGo)
├── clipboard_windows.go  # Windows clipboard via PowerShell
├── clipboard_other.go    # Unsupported platform stub
├── database.go           # SQLite operations
├── main.go               # Entry point
├── native_drag_darwin.go # macOS native file drag via CGo
├── native_drag_windows.go # Windows native drag via DataTransfer
├── native_drag_other.go  # Unsupported platform stub
├── open_darwin.go        # macOS file-open helper
├── open_windows.go       # Windows file-open helper
├── open_other.go         # Unsupported platform stub
├── plugin_service.go     # Plugin frontend API (separate struct)
├── plugins.go            # Plugin install/uninstall helpers
├── serve_json_api.go     # JSON API handler for served tags (/_api prefix)
├── serve_file_upload.go  # File upload handler for served tags (/_api/_upload)
├── serve_manager.go      # Tag HTTP server lifecycle and routing
├── serve_service.go      # Serve frontend API (separate struct)
├── temp_clip_store.go    # Leased temp file management for transfers
├── transfer_handler.go   # HTTP handler for drag-out file transfers
├── transfer_service.go   # Drag-out preparation and native drag initiation
├── transfer_types.go     # Transfer system type definitions
├── tag_hierarchy.go      # Tag tree helpers (parent, root, ancestor, descendant checks)
├── watcher.go            # Watch folder implementation
├── plugin/               # Lua plugin system
│   ├── manager.go        # Plugin lifecycle, event dispatch
│   ├── manifest.go       # Manifest parsing, validation
│   ├── sandbox.go        # Sandboxed Lua execution
│   ├── scheduler.go      # Scheduled/recurring plugin tasks
│   ├── fetch.go          # Plugin fetching from URLs
│   ├── semver.go         # Semantic version parsing
│   ├── update_checker.go # Plugin update checking
│   ├── permission_diff.go # Permission change detection
│   └── api_*.go          # Lua APIs (clips, tags, storage, http, fs, utils, task, toast, image, modal, metadata)
├── plugins/              # Bundled plugins
│   ├── ascii-art.lua     # ASCII art conversion
│   ├── auto-tagger.lua   # Automatic clip tagging
│   ├── exif-viewer.lua   # EXIF metadata viewer
│   ├── expiring-clips.lua # Auto-expire old clips
│   ├── fal-ai.lua        # FAL.AI image processing
│   ├── mahresources.lua  # Mahresources integration
│   ├── palette-extractor.lua # Color palette extraction
│   ├── qr-code.lua       # QR code generation
│   └── watermarker.lua   # Image watermarking
├── cmd/mp/               # CLI binary (stateless REST API client)
├── frontend/
│   ├── index.html        # Single HTML file with all markup
│   ├── js/
│   │   ├── app.js        # Main app initialization, event handlers
│   │   ├── api-settings.js # API settings management UI
│   │   ├── context-menu.js # Context menu functionality
│   │   ├── editor/       # Modular image editor
│   │   │   ├── editor-core.js   # Core editor canvas logic
│   │   │   ├── tool-anonymize.js # Anonymize/blur tool
│   │   │   ├── tool-arrow.js    # Arrow drawing tool
│   │   │   ├── tool-brush.js    # Brush/freehand tool
│   │   │   ├── tool-crop.js     # Crop tool
│   │   │   ├── tool-eyedropper.js # Color picker tool
│   │   │   ├── tool-select.js   # Selection tool
│   │   │   ├── tool-shapes.js   # Shape drawing tool
│   │   │   ├── tool-text.js     # Text overlay tool
│   │   │   ├── tool-transform.js # Transform/resize tool
│   │   │   └── tool-zoom.js     # Zoom/pan tool
│   │   ├── folder-drag.js # Folder drag interaction
│   │   ├── metadata.js   # Clip metadata handling
│   │   ├── modal-renderer.js # Plugin result modal rendering
│   │   ├── modals.js     # All modal/lightbox/editor logic
│   │   ├── plugin-icons.js # Plugin icon rendering
│   │   ├── plugin-review.js # Plugin permission review UI
│   │   ├── plugins.js    # Plugin management UI
│   │   ├── roving-tabindex.js # Accessible roving tabindex pattern
│   │   ├── serve.js      # Tag serve UI
│   │   ├── settings.js   # Settings modal
│   │   ├── shortcuts.js  # ShortcutManager for keyboard shortcut registration and context handling
│   │   ├── sort.js       # Clip sorting UI
│   │   ├── tags.js       # Tag management UI
│   │   ├── task-queue.js # Plugin task progress UI
│   │   ├── tooltips.js   # Tooltip system
│   │   ├── transfer.js   # Drag-out transfer state management
│   │   ├── transfer-strategies.js # Platform-specific drag data adapters
│   │   ├── ui.js         # Card rendering, gallery management
│   │   ├── utils.js      # Shared utilities
│   │   ├── wails-api.js  # Wails bindings wrapper
│   │   └── watch.js      # Watch folders UI
│   ├── css/
│   │   ├── main.css      # Global styles, scrollbars, form styling
│   │   └── modals.css    # Modal-specific styles
│   └── wailsjs/          # Generated Wails bindings
├── examples/             # Example plugins and SPAs
│   ├── plugins/          # Example plugin scripts
│   └── SPAs/             # Example single-page apps for tag serve
├── docs/                 # Documentation source
└── e2e/                  # Playwright tests
    ├── tests/            # Test files by feature
    ├── fixtures/         # Test fixtures (AppHelper)
    └── helpers/          # Test utilities and selectors
```

## CLI (`mp`)

The `mp` binary is a stateless CLI for mahpastes, talking to the REST API (`/api/v1/*`).

### Setup

```bash
make mp                              # Build
export MP_API_KEY=mp_your_key_here   # Required
export MP_API_URL=http://localhost:44557  # Optional, this is the default
```

### Command Groups

| Group | Purpose |
|-------|---------|
| `mp clip` | List, upload, get, delete, rename, archive, expire, download, metadata |
| `mp tag` | Create, update, delete, assign/remove, list clips, hidden tags |
| `mp dedup` | List duplicate groups, merge, deduplicate all |
| `mp watch` | Add/remove/update watch folders, pause/resume, status |
| `mp plugin` | Install/remove/enable/disable, storage, run actions, updates |
| `mp serve` | Start/stop/list tag HTTP servers |
| `mp link` | Create/list/revoke revocable public share links for a clip |
| `mp api` | Check API connectivity, create/list/revoke API keys (`mp api key ...`, admin only) |
| `mp backup` | Create/restore backup ZIPs |
| `mp clipboard` | Copy clip content or file reference to system clipboard |

### Key Patterns

- `--json` flag on any command for machine-readable output
- `--stdin` flag on bulk commands reads IDs from stdin
- Tags can be referenced by name or ID
- `mp clip data <id> > file.png` outputs raw content to stdout
- Auth via `MP_API_KEY` env var (Bearer token), no config files
- Exit codes: 0 success, 1 general error, 2 connection error, 3 auth error

### Architecture

```
cmd/mp/
├── main.go        # Root command, --json flag
├── client/
│   └── client.go  # HTTP client (auth, errors, JSON/multipart helpers)
├── output.go      # Human/JSON formatting (printTable, printJSON, printKeyValue)
├── clip.go        # clip subcommands
├── tag.go         # tag subcommands
├── dedup.go       # dedup subcommands
├── watch.go       # watch subcommands
├── plugin.go      # plugin subcommands
├── serve.go       # serve subcommands
├── api.go         # api subcommands
├── backup.go      # backup subcommands
└── clipboard.go   # clipboard subcommands
```

Pure Go, no CGo — cross-compiles for macOS, Linux, Windows.

## REST API (`/api/v1/*`)

The app exposes a REST API via `api_manager.go` that the `mp` CLI and external tools consume.

### Key Files

- `api_manager.go` - HTTP server with ~40+ endpoints, auth middleware, CORS
- `api_service.go` - Wails-bound `APIService` struct for frontend control (start/stop API, manage keys)

### Authentication

API keys with roles (`viewer`, `editor`, `admin`). Auth via `Authorization: Bearer <key>` header or `MP_API_KEY` env var in the CLI.

Revoking a key is a soft delete: `is_revoked = 1` plus a `revoked_at` stamp. Auth denial is instant (every lookup filters `is_revoked = 0`); the row itself is hard-deleted by the `StartCleanupJob` sweep `revokedKeyRetentionDays` (7) after revocation, so the key list doesn't accumulate dead entries forever. Deleting a scoped tag NULLs `scoped_tag_id`, and the `api_keys_revoke_on_scope_null` trigger revokes and stamps the key so it ages out on the same schedule.

### Endpoints

Routes cover all major features: clips, tags, watch folders, plugins, backup, dedup, clipboard, metadata, and serve management. All under `/api/v1/`.

### Share Links

Revocable, single-clip public download links, distinct from the peer-to-peer `share_*.go` tag-sync feature (which is named `share`/`/api/v1/share/*` — do **not** reuse that namespace).

- **Public route**: `GET /s/{token}` — unauthenticated; the 256-bit `crypto/rand` token in the path is the only capability. Streams the clip through the same hardened path as `GET /api/v1/clips/{id}/data` (`writeClipBytes`: `nosniff` + CSP sandbox + `Content-Disposition: attachment` + `Cache-Control: no-store`). Rate-limited per client IP; every reject (missing/revoked/expired/exhausted/clip-archived) is a uniform 404.
- **Management** (admin-only): `POST /api/v1/links` (mint, returns the token once), `GET /api/v1/links` (list, prefixes only), `DELETE /api/v1/links/{id}` (revoke — instant, re-checked in SQL per request, no cache).
- Optional `expires_in_seconds` and `max_downloads` (atomic cap). Tag-scoped admin keys can only mint links inside their subtree (`enforceTagScope`). Only the token's SHA-256 hash is stored.
- **Key files**: `share_link.go` (table-backed logic, token gen, handlers, `handleShareView`), `database.go` (`share_links` table + expiry GC in `StartCleanupJob`), `api_manager.go` (route registration + `writeClipBytes`), `link_service.go` (Wails `LinkService` for the desktop binding), `cmd/mp/link.go` (CLI), `frontend/js/rest-glue.js` (headless shim) + `ui.js`/`modals.js` (the server-mode "Copy → Public Link" affordance).

## Code Style

### JavaScript

- Vanilla JS, no framework
- Module pattern with function scope
- DOM elements cached at top of file
- Event delegation where appropriate
- Use `const`/`let`, never `var`

### Go

- Standard Go formatting (gofmt)
- Wails app methods exposed via `wailsjs/`

### CSS

- Tailwind utility classes preferred
- Custom CSS only when utilities insufficient
- CSS custom properties for dynamic values (e.g., modals)

## Plugin System Architecture

The app has a Lua-based plugin system that allows extending functionality.

### Key Files

- `plugin/manager.go` - Plugin lifecycle management, event dispatch
- `plugin/sandbox.go` - Sandboxed Lua execution environment
- `plugin/manifest.go` - Plugin manifest parsing, event validation
- `plugin/api_*.go` - Lua APIs exposed to plugins (clips, tags, storage, http, fs, utils, task, toast, image, modal, metadata)
- `plugin/scheduler.go` - Scheduled/recurring plugin tasks
- `plugin_service.go` - Frontend API for plugin management (separate from App due to Wails method limit)

### Wails Method Binding Limit

**NOTE**: The App struct currently has 97+ exported methods and all bind correctly. Multiple services exist as separate structs for organizational clarity:
- `PluginService` in `plugin_service.go` - Plugin-related APIs (TryAcquireModalGuard, IsPluginURLAllowed, GetPluginUIActions, ExecutePluginAction, ImportPluginFromPath, GetPluginPermissions, RevokePluginPermission, GetPluginStorage, SetPluginStorage, GetAllPluginStorage, etc.)
- `ClipboardService` in `clipboard_service.go` - Clipboard copy operations
- `TransferService` in `transfer_service.go` - Drag-out/transfer operations
- `ServeService` in `serve_service.go` - Tag HTTP serving operations
- `APIService` in `api_service.go` - REST API management (start/stop, API keys)

Frontend accesses via `window.go.main.PluginService.*`, `window.go.main.ClipboardService.*`, `window.go.main.TransferService.*`, `window.go.main.ServeService.*`, `window.go.main.APIService.*`.

### Event System

Events are emitted via `pluginManager.EmitEvent(eventName, data)`. Plugins subscribe to events in their manifest and implement handlers like `on_clip_created(data)`.

**Current events** (defined in `plugin/manifest.go:ValidEvents()`):
- `app:startup`, `app:shutdown` - App lifecycle
- `clip:created`, `clip:deleted`, `clip:archived`, `clip:unarchived`, `clip:renamed` - Clip operations
- `watch:file_detected`, `watch:import_complete` - Watch folder events
- `tag:created`, `tag:updated`, `tag:deleted`, `tag:added_to_clip`, `tag:removed_from_clip` - Tag operations

**To add a new event**:
1. Add to `ValidEvents()` in `plugin/manifest.go`
2. Call `pluginManager.EmitEvent("event:name", data)` where the event occurs
3. Handler name convention: `clip:created` → `on_clip_created`

### Lua APIs

APIs are registered in `plugin/manager.go` when loading plugins. Each API module is a global table:

| Module | File | Functions |
|--------|------|-----------|
| `clips` | `api_clips.go` | list, get, get_data, create, create_from_url, update, delete, delete_many, archive, unarchive |
| `tags` | `api_tags.go` | list, get, create, update, delete, add_to_clip, remove_from_clip, get_for_clip |
| `storage` | `api_storage.go` | get, set, delete, list (plugin-scoped key-value storage) |
| `http` | `api_http.go` | get, post, put, patch, delete (network requests with domain restrictions) |
| `fs` | `api_fs.go` | read, write, list, exists (filesystem with permission prompts) |
| `task` | `api_task.go` | start, progress, complete, fail (long-running task progress UI) |
| `toast` | `api_toast.go` | show (display toast notifications) |
| `image` | `api_image.go` | info, resize, overlay_text, composite, dominant_colors, grayscale_pixels, metadata, diff, convert (Go-side image processing) |
| `utils` | `api_utils.go` | time, sha256, hmac_sha256, url_encode, url_decode, clipboard_write (requires `clipboard = true` in manifest) |
| `log` | `api_utils.go` | Global function for logging (not a module) |
| `json` | `api_utils.go` | encode, decode |
| `base64` | `api_utils.go` | encode, decode |
| `metadata` | `api_metadata.go` | get, set, delete, set_bulk (clip metadata key-value pairs) |
| `modal` | `api_modal.go` | show (display plugin result modal with markdown/image/text content) |

**To add a new API module**:
1. Create `plugin/api_<name>.go` with struct and `Register(L *lua.LState)` method
2. Register in `plugin/manager.go` in the plugin loading section
3. Document the API in example plugins

### Tag Functions (in app.go)

Tag operations exposed to the frontend (and to plugins via `plugin/api_tags.go`):
- `CreateTag(name)` → `*Tag, error`
- `UpdateTag(id, name, color)` → `error`
- `DeleteTag(id)` → `error`
- `GetTags()` → `[]Tag, error`
- `AddTagToClip(clipID, tagID)` → `error` — enforces tree exclusivity (removes same-tree tags first)
- `RemoveTagFromClip(clipID, tagID)` → `error`
- `BulkAddTag(clipIDs, tagID)` → `error` — enforces tree exclusivity per clip
- `BulkRemoveTag(clipIDs, tagID)` → `error`
- `GetClipTags(clipID)` → `[]Tag, error`
- `GetHiddenTags()` → `[]int64, error`
- `SetHiddenTags(ids)` → `error`

### Tag Hierarchy & Folder Mode

Tags form hierarchical trees using `/` as separator (e.g., `work/client1/projectA`). Key behaviors:

**Tree exclusivity**: A clip can only have ONE tag per root tree. Adding `a/b/d` automatically removes any existing tags under the same root (`a`, `a/b`, `a/b/c`, etc.). Tags from different trees (e.g., `a/b` and `x/y`) coexist freely. Enforced in `AddTagToClip` and `BulkAddTag` via `removeSameTreeTags()`.

**Hierarchical filtering** (normal mode): Filtering by `a` shows clips tagged with `a`, `a/b`, `a/b/c`, etc. via `getDescendantTagIDs()` expansion in `GetClips`. Multiple filters use AND logic.

**Folder mode**: Shows clips only at their exact tag level. `GetFolderClips` excludes descendants; `GetUntaggedClips` shows only clips with zero tags at root. The folder mode toggle uses the same `bg-stone-800` active style as the archive toggle.

**Hidden tags in folder mode**: Hiding only dims folder cards (`data-hidden="true"`); it never filters folder contents. `GetFolderClips` takes no hidden-tag list, so a clip tagged `contacts` and `web/contacts` still appears in the `contacts` folder while `web` is hidden. Normal (non-folder) mode keeps the blanket anti-join — any hidden tag hides the clip. `GetDescendantClipCount(tagID, archived)` powers the folder card count and applies the same archive/expiry filters as the listing, so a card's count always matches what opening it shows.

**Hidden-clip note (normal mode)**: While tag filters are active, `#hidden-clips-note` under the gallery reads e.g. `2 clips hidden by other tags (web/contacts)`. It is fed by `GetHiddenClipInfo(archived, tagIDs, hiddenTagIDs)` (REST: `GET /api/v1/clips/hidden-info`), which runs the listing's filter expansion with the hidden anti-join flipped into a requirement. Both that counter and `getClipsInternal` resolve their tag scope through `buildClipFilterScope`, so the note cannot disagree with the list above it. The note is suppressed in folder mode and when no filter is active.

**Auto-tagging on upload**: `UploadFiles(files, expirationMinutes, autoTagID)` accepts an optional tag ID. When in folder mode with an active folder, the frontend passes the current folder's tag ID so new clips are auto-tagged into the folder.

**Tag filter in folder mode**: Checking a tag in the filter dropdown navigates to that tag's folder (via `navigateToFolder`) instead of appending. Unchecking navigates up to the parent. Entering folder mode with multiple unrelated filters normalizes to the last selected tag's path.

**Tag display**: Clip card pills always show the full tag path (e.g., `work/client1` not `client1`).

**Key files**: `tag_hierarchy.go` (tree helpers), `app.go` (exclusivity + queries), `frontend/js/tags.js` (filter UI), `frontend/js/ui.js` (`navigateToFolder`, `renderFolderCards`), `frontend/js/wails-api.js` (`loadClips` folder mode branches).

### Tag Serve JSON API

HTML clips served from a tag can read/write JSON clips in the same tag via REST semantics at the `/_api` prefix.

**Routing**: `/_api/{clipStem}/{jsonPath...}` — first segment maps to `{clipStem}.json` clip, remaining segments navigate into JSON (object keys by name, array elements by `id` field).

**HTTP verbs**: GET (read), POST (append to array with auto-increment id), PUT (replace/upsert), PATCH (JSON Merge Patch RFC 7396), DELETE (remove key/element).

**Cookie auth**: Tag server sets `_mp_serve_key` HTTP-only SameSite=Strict cookie on every response when API is enabled. `/_api` handler validates this cookie — 401 without it.

**Permission model**: `StartServing(tagID, port, bindAll, apiAccess)` where `apiAccess` is `"none"` (404, default), `"read"` (GET only), or `"readwrite"` (full CRUD).

**Concurrency**: Per-clip `sync.Mutex` serializes writes to the same JSON clip.

**Reserved tag names**: `CreateTag` rejects any tag where a path segment equals `_api` (e.g., `_api`, `work/_api`). Substrings are fine (e.g., `my_api_stuff`).

**Key files**: `serve_json_api.go` (JSON handler, path navigation, CRUD operations), `serve_manager.go` (cookie setting, `/_api` routing, `tagServer` struct fields).

### Tag Serve File Upload API

HTML apps served from a tag can upload files as new clips via `POST /_api/_upload`.

**Endpoint**: `POST /_api/_upload` with `Content-Type: multipart/form-data`. Requires `apiAccess == "readwrite"` and valid `_mp_serve_key` cookie.

**Form fields**:
- `file` (required) — the file to upload (10 MB max)
- `tag` (optional) — relative subtag path under the served tag (e.g., `child/grandchild`). Auto-creates tags if needed.
- `content_type` (optional) — override content type auto-detection

**Tag resolution**: If serving tag `a/b` and tag field is `c/d`, the clip is tagged `a/b/c/d`. Empty tag field → served tag itself. Path traversal (`..`) and `_api` segments are rejected.

**Response**: `201 Created` with `{"id", "filename", "content_type", "tag", "tag_id"}`.

**Key files**: `serve_file_upload.go` (upload handler, tag validation), `serve_manager.go` (`/_api/_upload` routing).

### Plugin UI Actions

Plugins can define UI actions that appear in the lightbox and card context menu.

**Manifest structure**: Define actions under `ui.lightbox_buttons` and `ui.card_actions` with fields:
- `id` - Unique action identifier
- `label` - Display text
- `icon` - Icon name
- `async` - Run in background goroutine (bool)
- `file_types` - MIME type filter (e.g., `["image/*"]`)
- `max_size` - Maximum clip size in bytes
- `options` - Form fields shown before execution

**Form field types**: `text`, `password`, `checkbox`, `select`, `range`. Each has `id`, `label`, `required`, `default`. Select adds `choices`; range adds `min`, `max`, `step`.

**Action results**: Actions return `ActionResult` with `success`, `error`, `result_clip_id`, `modal`. The `modal` field triggers the result modal system.

**Async actions**: When `async = true`, action returns immediately, runs in a goroutine, and shows modal or toast on completion.

**Modal system**: One-modal-at-a-time guard via `TryAcquireModalGuard`. `modal.show()` takes `title`, `content`, `format` (`markdown`/`image`/`text`), plus optional `copy_data`, `paste_data`, `paste_name`, `paste_content_type`.

## Import Folder Wizard

A desktop-only triage flow reached from the nav drawer (`#open-import-btn`). It scans a
user-picked folder, walks the files one at a time showing preview + details + EXIF +
duplicate clips, records an action per file, then executes the reviewed plan.

**The invariant**: nothing touches disk or the library until Apply. This is structural, not
a UI convention — there is no bound method that imports or deletes a single file.
`ImportApply` takes the entire plan in one call, so a half-executed plan is not
representable, and the import-before-trash ordering cannot be broken by a JS exception or a
closed modal.

### Session containment

`internal/app/import_wizard.go` holds one `importSession` per run (root + the set of
relPaths the scan emitted). `resolveSessionPath` gates every path-taking method: membership
in that set first, then `isInsideDir` (reused from `paste_paths.go`), then an `os.Lstat`
regular-file check and a post-scan `EvalSymlinks` containment re-check (closes the TOCTOU
window where a scanned file is swapped for a symlink before Apply).

**These methods are deliberately not exposed over REST.** A route would hand every API-key
holder a remote directory listing, arbitrary-file read, and move-to-trash primitive.
`TestNoRESTRoutesForImportWizard` fails the build if `api_manager.go` ever references them.
Server mode gets throwing stubs in `rest-glue.js` plus the `.desktop-only` class on the
drawer entry.

### Key files

- `internal/app/import_wizard.go` — session, scan, inspect, apply
- `internal/app/trash_{darwin,windows,other}.go` — `moveToTrash`; macOS uses
  `NSFileManager trashItemAtURL:` (no main-queue dispatch needed, unlike `startNativeFileDrag`).
  Non-darwin falls back to `os.Remove` and reports `trashIsRecoverable() == false`, which
  drives a UI warning. `MAHPASTES_TRASH_MODE=remove` forces permanent delete; the e2e
  launcher sets it so runs don't fill `~/.Trash`.
- `internal/imagemeta/` — EXIF extraction from bytes, shared by the wizard and the Lua
  `image.metadata` binding (which previously read only from clip IDs).
- `frontend/js/import-wizard.js` — three panes (setup / review / summary), decision map keyed
  by relPath, `import-wizard` shortcut context.

### What guards what

Deletion is the destructive half, so several checks exist purely to make sure the
file deleted is the file reviewed:

- **Picker approval** — `StartImportSession` refuses any root not passed to
  `ApproveImportRoot`, which is called only by `BeginImportSession` through the
  unexported `core` field (so it is not JS-reachable). Approval is revoked by
  `EndImportSession`. `MAHPASTES_ALLOW_UNPICKED_IMPORT=1` opts out; the e2e launcher
  sets it because Playwright cannot drive a native dialog.
- **`importGeneration`** — captured with the approval under one lock, re-checked before
  the scan installs itself, so a scan finishing after the wizard closed is discarded.
- **`openImportFile`** — `O_NOFOLLOW` on unix; every read goes through the handle, never
  by re-opening the path. `readImportFile` re-stats the handle after reading to catch a
  write that landed mid-read.
- **`importSession.reviewed`** — the `os.FileInfo` *and* content hash shown to the user.
  `ImportApply` refuses (status `changed`) anything that no longer matches. Files never
  inspected have no baseline and are not checked.
- **`trashVerified`** — re-opens, fstats and re-hashes before unlinking. Stat data alone
  misses a same-length in-place rewrite with a preserved mtime, which is exactly what
  timestamp-preserving sync tools produce.

Two races are documented as deliberately accepted in the file header: an
intermediate-directory symlink swap (needs `openat` per component, no Windows
equivalent), and the microsecond gap inside `trashVerified` between the final check and
the unlink (no portable API unlinks an inode).

### Gotchas

- `UploadFileAndGetID` does **not** emit `clip:created`; `ImportApply` emits it itself
  (after tagging, matching `UploadFiles`) or plugins never fire for wizard imports.
- Repeat-last copies the action always but the tag only when the user typed one — otherwise
  walking from `2024/` into `2025/` silently tags everything `2024`.
- Suggested tags come from one function, `suggestedTagFor(entry)`: the setup pane's base tag
  joined with the file's own subfolder (`trips` + `2024/rome.jpg` → `trips/2024`). Seeding,
  the base-tag field and Repeat all route through it so they cannot drift. Editing the base
  tag re-seeds every decision whose `tagEdited` flag is false and leaves hand-typed tags alone.
- Wizard imports are permanent: `UploadFileAndGetID` never sets `expires_at`, so the bottom
  bar's expiry select is intentionally not replicated here.
- E2E cannot dismiss a native folder picker. `BeginImportSession` (picker) is split from
  `StartImportSession` (scan) so tests drive `window.__testHelpers.openImportWizard(path)`.

## Transfer/Drag-Out System

The transfer system handles copying clips as files to the system clipboard and dragging clips out of the app window into other applications.

### Architecture

Five separate Wails-bound services (`ClipboardService`, `TransferService`, `PluginService`, `ServeService`, `APIService`) exist alongside `App` for organizational clarity.

### Key Files

**Go (root)**:
- `clipboard_service.go` - Clipboard copy service struct
- `clipboard_darwin.go` - macOS clipboard via NSPasteboard (CGo)
- `clipboard_windows.go` - Windows clipboard via PowerShell
- `clipboard_other.go` - Unsupported platform stub
- `transfer_service.go` - Drag-out preparation and native drag initiation
- `transfer_types.go` - Transfer system type definitions
- `app_transfer_helpers.go` - Bridge between App and TempClipStore
- `temp_clip_store.go` - Leased temp file management for transfers
- `native_drag_darwin.go` - macOS native file drag via CGo
- `native_drag_windows.go` - Windows drag via DataTransfer API
- `native_drag_other.go` - Unsupported platform stub
- `transfer_handler.go` - HTTP handler for drag-out file transfers

**Frontend JS**:
- `transfer.js` - Drag-out transfer state management
- `transfer-strategies.js` - Platform-specific drag data adapters

### How Drag-Out Works

1. Frontend calls `TransferService.PrepareClipForTransfer` to materialize a temp file
2. On `dragstart`, sets `DataTransfer` types via platform strategy adapter AND calls native drag
3. Temp files have 60-min leases, pruned every 10 min
4. Platform-specific: macOS uses CGo/NSPasteboard for clipboard, NSView.dragFile for drag

### Platform Support

| Operation | macOS | Windows | Linux |
|-----------|-------|---------|-------|
| Copy as file | NSPasteboard (CGo) | PowerShell SetFileDropList | Not supported |
| Copy raw content | golang.design/x/clipboard | Same | Same |
| Drag out | NSView.dragFile + file-uri-v1 | DataTransfer file-uri-v1 | Planned |

## Common Tasks

### Adding a new feature

1. Run e2e tests to verify baseline
2. Add Go backend method if needed
3. Update `frontend/wailsjs/` bindings (run `wails generate module`)
4. Add UI in appropriate JS file
5. Add CSS if needed (prefer Tailwind utilities)
6. Add e2e tests for the feature
7. Run all tests and fix any failures, regardless of what caused them
