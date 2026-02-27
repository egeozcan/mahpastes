# mahpastes

A Wails desktop clipboard manager for macOS, Windows, and Linux with image editing, comparison, and watch folder features.

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

### Test Organization

Tests are organized by feature in `e2e/tests/`:
- `backup/` - Backup and restore operations
- `bulk/` - Multi-select operations
- `clips/` - Upload, view, delete, archive operations
- `edge-cases/` - Error handling, expiration
- `images/` - Lightbox, editor, comparison
- `plugins/` - Plugin system (install, events, APIs, scheduling)
- `search/` - Filtering functionality
- `screenshots/` - Documentation screenshot capture
- `shortcuts/` - Keyboard shortcut tests
- `tags/` - Tag CRUD, filtering, hidden tags
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
  - Success indicator: `emerald-500` (watch indicator only)

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
├── app_transfer_helpers.go # Bridge between App and TempClipStore
├── backup.go             # ZIP backup and restore
├── clipboard_service.go  # Clipboard copy service (separate struct for Wails binding limit)
├── clipboard_darwin.go   # macOS clipboard via NSPasteboard (CGo)
├── clipboard_windows.go  # Windows clipboard via PowerShell
├── clipboard_other.go    # Unsupported platform stub
├── database.go           # SQLite operations
├── main.go               # Entry point
├── native_drag_darwin.go # macOS native file drag via CGo
├── native_drag_other.go  # Unsupported platform stub
├── plugin_service.go     # Plugin frontend API (separate struct for Wails binding limit)
├── plugins.go            # Plugin install/uninstall helpers
├── temp_clip_store.go    # Leased temp file management for transfers
├── transfer_service.go   # Drag-out preparation and native drag initiation
├── transfer_types.go     # Transfer system type definitions
├── watcher.go            # Watch folder implementation
├── plugin/               # Lua plugin system
│   ├── manager.go        # Plugin lifecycle, event dispatch
│   ├── manifest.go       # Manifest parsing, validation
│   ├── sandbox.go        # Sandboxed Lua execution
│   ├── scheduler.go      # Scheduled/recurring plugin tasks
│   └── api_*.go          # Lua APIs (clips, tags, storage, http, fs, utils, task, toast, image, modal)
├── plugins/              # Example/bundled plugins
│   └── fal-ai.lua        # FAL.AI image processing plugin
├── frontend/
│   ├── index.html        # Single HTML file with all markup
│   ├── js/
│   │   ├── app.js        # Main app initialization, event handlers
│   │   ├── editor.js     # Image editor canvas logic
│   │   ├── modal-renderer.js # Plugin result modal rendering
│   │   ├── modals.js     # All modal/lightbox/editor logic
│   │   ├── plugin-icons.js # Plugin icon rendering
│   │   ├── plugin-review.js # Plugin permission review UI
│   │   ├── plugins.js    # Plugin management UI
│   │   ├── settings.js   # Settings modal
│   │   ├── shortcuts.js  # ShortcutManager for keyboard shortcut registration and context handling
│   │   ├── tags.js       # Tag management UI
│   │   ├── task-queue.js # Plugin task progress UI
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
└── e2e/                  # Playwright tests
    ├── tests/            # Test files by feature
    ├── fixtures/         # Test fixtures (AppHelper)
    └── helpers/          # Test utilities and selectors
```

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
- `plugin/api_*.go` - Lua APIs exposed to plugins (clips, tags, storage, http, fs, utils, task, toast, image, modal)
- `plugin/scheduler.go` - Scheduled/recurring plugin tasks
- `plugin_service.go` - Frontend API for plugin management (separate from App due to Wails method limit)

### Wails Method Binding Limit

**CRITICAL**: Wails has a ~49 method limit per bound struct. Multiple services exist as separate structs to stay under this limit:
- `PluginService` in `plugin_service.go` - Plugin-related APIs (TryAcquireModalGuard, IsPluginURLAllowed, GetPluginUIActions, ExecutePluginAction, ImportPluginFromPath, GetPluginPermissions, RevokePluginPermission, GetPluginStorage, SetPluginStorage, GetAllPluginStorage, etc.)
- `ClipboardService` in `clipboard_service.go` - Clipboard copy operations
- `TransferService` in `transfer_service.go` - Drag-out/transfer operations

Frontend accesses via `window.go.main.PluginService.*`, `window.go.main.ClipboardService.*`, `window.go.main.TransferService.*`.

### Event System

Events are emitted via `pluginManager.EmitEvent(eventName, data)`. Plugins subscribe to events in their manifest and implement handlers like `on_clip_created(data)`.

**Current events** (defined in `plugin/manifest.go:ValidEvents()`):
- `app:startup`, `app:shutdown` - App lifecycle
- `clip:created`, `clip:deleted`, `clip:archived`, `clip:unarchived` - Clip operations
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
- `AddTagToClip(clipID, tagID)` → `error`
- `RemoveTagFromClip(clipID, tagID)` → `error`
- `BulkAddTag(clipIDs, tagID)` → `error`
- `BulkRemoveTag(clipIDs, tagID)` → `error`
- `GetClipTags(clipID)` → `[]Tag, error`
- `GetHiddenTags()` → `[]int64, error`
- `SetHiddenTags(ids)` → `error`

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

## Transfer/Drag-Out System

The transfer system handles copying clips as files to the system clipboard and dragging clips out of the app window into other applications.

### Architecture

Three separate Wails-bound services (`ClipboardService`, `TransferService`, `PluginService`) exist alongside `App` to stay under the Wails ~49 method limit per struct.

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
- `native_drag_other.go` - Unsupported platform stub

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
| Drag out | NSView.dragFile + file-uri-v1 | Planned | Planned |

## Common Tasks

### Adding a new feature

1. Run e2e tests to verify baseline
2. Add Go backend method if needed
3. Update `frontend/wailsjs/` bindings (run `wails generate module`)
4. Add UI in appropriate JS file
5. Add CSS if needed (prefer Tailwind utilities)
6. Add e2e tests for the feature
7. Run all tests and fix any failures, regardless of what caused them
