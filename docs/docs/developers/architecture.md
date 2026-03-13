---
sidebar_position: 1
---

# Architecture Overview

mahpastes is built using Wails, a framework for building desktop applications with Go backends and web frontends.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Desktop App (Wails)                     │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────────┐    ┌─────────────────────────────┐ │
│  │      Frontend       │    │          Backend            │ │
│  │  (HTML/CSS/JS)      │◄──►│           (Go)              │ │
│  │                     │    │                             │ │
│  │  • Vanilla JS       │    │  • Wails v2                 │ │
│  │  • Tailwind CSS     │    │  • SQLite                   │ │
│  │  • No framework     │    │  • Clipboard integration    │ │
│  └─────────────────────┘    │  • File system watcher      │ │
│                             │  • REST API server          │ │
│                             │  • Tag HTTP servers         │ │
│                             └─────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                  │                          │
                  ▼                          ▼
         ┌────────────────┐       ┌──────────────────┐
         │   SQLite DB    │       │  HTTP listeners   │
         │   clips.db     │       │  (REST API, Tag   │
         └────────────────┘       │   serve servers)  │
                                  └──────────────────┘
```

## Technology Stack

### Backend (Go)

| Component | Technology | Purpose |
|-----------|------------|---------|
| Framework | Wails v2 | Desktop app framework |
| Database | SQLite 3 | Local data storage |
| Clipboard | golang.design/x/clipboard | Cross-platform clipboard |
| File Watch | fsnotify | Filesystem events |
| Plugins | gopher-lua | Lua scripting sandbox |
| CLI | cobra | `mp` CLI command framework |
| EXIF | goexif | Image metadata extraction |
| COM (Windows) | go-ole | OLE drag-and-drop interop |

### Frontend (Web)

| Component | Technology | Purpose |
|-----------|------------|---------|
| Markup | HTML5 | Structure |
| Styling | Tailwind CSS | Utility-first CSS |
| Logic | Vanilla JavaScript | No framework overhead |
| Font | IBM Plex Mono | Consistent typography |

## Component Overview

### Backend Components

```
main.go                  Entry point, Wails setup
app.go                   Core application logic, API methods
database.go              SQLite setup, schema, migrations
watcher.go               Folder watching, file import
backup.go                ZIP backup and restore
clipboard_service.go     Clipboard copy service (Wails-bound)
clipboard_darwin.go      macOS clipboard via NSPasteboard (CGo)
clipboard_windows.go     Windows clipboard via PowerShell
clipboard_other.go       Unsupported platform stub
transfer_service.go      Drag-out preparation and native drag
transfer_handler.go      HTTP handler for transfer file serving (DownloadURL)
transfer_types.go        Transfer system type definitions
app_transfer_helpers.go  Bridge between App and TempClipStore
temp_clip_store.go       Leased temp file management
native_drag_darwin.go    macOS native drag via CGo
native_drag_windows.go   Windows native drag (full COM OLE DoDragDrop implementation)
native_drag_other.go     Unsupported platform stub
open_darwin.go           Open file with default app (macOS)
open_windows.go          Open file with default app (Windows)
open_other.go            Unsupported platform stub
plugin_service.go        Plugin frontend API (Wails-bound)
plugins.go               Plugin install/uninstall helpers
serve_manager.go         Tag serve HTTP server lifecycle and routing
serve_json_api.go        JSON API handler for served tags (/_api prefix)
serve_file_upload.go     File upload handler for served tags (/_api/_upload)
serve_service.go         Tag serve Wails service (start/stop/status)
api_manager.go           REST API HTTP server, route registration, key management
api_service.go           REST API Wails service (start/stop/keys)
tag_hierarchy.go         Tag tree helpers (parent, root, ancestor, descendant checks)
plugin/                  Lua plugin system
├── manager.go           Plugin lifecycle, event dispatch
├── manifest.go          Manifest parsing, validation
├── sandbox.go           Sandboxed Lua execution
├── scheduler.go         Scheduled/recurring plugin tasks
├── fetch.go             Plugin fetching and downloading
├── semver.go            Semantic versioning helpers
├── update_checker.go    Plugin update checking
├── permission_diff.go   Permission diff logic for plugin updates
├── fonts/               Bundled fonts for image overlay
└── api_*.go             Lua APIs (clips, tags, storage, http, fs, utils, task, toast, image, modal, metadata)
```

### Frontend Components

```
frontend/
├── index.html           Main UI structure
├── js/
│   ├── app.js           Core application logic
│   ├── ui.js            Card rendering, gallery management
│   ├── editor.js        Image editor canvas logic
│   ├── modals.js        All modal/lightbox/editor logic
│   ├── tags.js          Tag management UI
│   ├── settings.js      Settings modal
│   ├── plugins.js       Plugin management UI
│   ├── plugin-icons.js  Plugin icon rendering
│   ├── task-queue.js    Plugin task progress UI
│   ├── watch.js         Watch folders UI
│   ├── transfer.js      Drag-out transfer state management
│   ├── transfer-strategies.js  Platform-specific drag data adapters
│   ├── modal-renderer.js       Plugin result modal rendering
│   ├── metadata.js      Clip metadata UI
│   ├── sort.js          Gallery sorting controls
│   ├── tooltips.js      Tooltip management
│   ├── shortcuts.js     Keyboard shortcut registration and context handling
│   ├── plugin-review.js Plugin permission review UI
│   ├── context-menu.js  Shared context menu module
│   ├── folder-drag.js   Folder drag-and-drop for moving clips between folders
│   ├── roving-tabindex.js  Reusable roving tabindex keyboard navigation
│   ├── utils.js         Shared utilities
│   ├── wails-api.js     Wails bindings wrapper
│   ├── serve.js         Tag serve view UI
│   └── api-settings.js  REST API settings modal
└── css/
    ├── main.css         Global styles, scrollbars, form styling
    └── modals.css       Modal-specific styles
```

## Wails-Bound Services

The backend exposes multiple Go structs to the frontend via Wails bindings. As the source code comment in `main.go` notes, services are separate structs to stay under the Wails ~49 method binding limit. In practice the `App` struct has grown to 70 exported methods and all bind correctly, so the limit may not be enforced. The separate services remain as an organizational boundary, grouping related functionality into dedicated files and structs.

| Service | File | Frontend access | Purpose |
|---------|------|-----------------|---------|
| `App` | `app.go` | `window.go.main.App.*` | Core clip, tag, watch, backup, and settings operations |
| `PluginService` | `plugin_service.go` | `window.go.main.PluginService.*` | Plugin lifecycle, UI actions, storage, updates |
| `ClipboardService` | `clipboard_service.go` | `window.go.main.ClipboardService.*` | Copy clips as files or raw content to system clipboard |
| `TransferService` | `transfer_service.go` | `window.go.main.TransferService.*` | Drag-out preparation and native OS drag |
| `ServeService` | `serve_service.go` | `window.go.main.ServeService.*` | Start/stop tag HTTP servers, status queries |
| `APIService` | `api_service.go` | `window.go.main.APIService.*` | REST API server lifecycle, API key management |

All six structs are bound in `main.go` via the `Bind` option.

## REST API Server

The `api_manager.go` file manages a standalone HTTP server that exposes clip, tag, watch, plugin, and backup operations over a REST interface (`/api/v1/*`). The `mp` CLI and other external tools authenticate with API keys (stored in the `api_keys` table).

Key details:
- The server listens on a user-configured port (default off).
- Authentication uses Bearer tokens (`Authorization: Bearer mp_...`).
- API keys have a name, role, and optional tag scope.
- `api_service.go` wraps the manager as a Wails-bound struct so the frontend can start/stop the server and manage keys from the **Settings** UI.

## Data Flow

### Adding a Clip

```
User Action (paste/drop)
        │
        ▼
Frontend (app.js)
  - Capture event
  - Convert to FileData
  - Call backend API
        │
        ▼
Backend (app.go)
  - UploadFiles()
  - Detect content type
  - Insert into SQLite
        │
        ▼
Frontend
  - Refresh gallery
  - Display new clip
```

### Retrieving a Clip

```
User clicks Copy
        │
        ▼
Frontend
  - GetClipData(id)
        │
        ▼
Backend
  - Query SQLite
  - Return base64 data
        │
        ▼
Frontend
  - Decode if needed
  - Write to clipboard
```

### Watch Folder Import

```
File created in watched folder
        │
        ▼
fsnotify event
        │
        ▼
WatcherManager
  - Debounce (500ms)
  - Check filter
  - Read file
        │
        ▼
Backend
  - Import as clip
  - Delete original
  - Emit event
        │
        ▼
Frontend
  - Receive event
  - Refresh gallery
  - Show toast
```

## Key Design Decisions

### Why Wails?

- Single binary distribution
- Native performance (Go backend)
- Web technologies for UI (familiar, flexible)
- Cross-platform support
- Good developer experience

### Why SQLite?

- Zero configuration
- Single file database
- Excellent performance for local data
- WAL mode for concurrent access
- No external dependencies

### Why Vanilla JavaScript?

- Small codebase doesn't need framework complexity
- Fast load times
- No build step for development
- Easy to understand and modify
- Reduced bundle size

### Why Base64 for Binary Data?

Wails communication between Go and JavaScript uses JSON. Binary data must be serialized:

- **Text content**: Passed as-is (UTF-8 strings)
- **Binary content**: Base64 encoded for transport
- Frontend decodes when displaying

## State Management

### Frontend State

```javascript
// Global state variables in app.js
let isViewingArchive = false;    // Current view mode
let selectedIds = new Set();      // Multi-select state
let imageClips = [];              // Lightbox navigation
let currentLightboxIndex = -1;    // Current lightbox position
```

### Backend State

```go
// App struct in app.go
type App struct {
    ctx              context.Context
    db               *sql.DB
    tempDir          string
    tempStore        *TempClipStore
    transferHandler  *TransferFileHandler
    mu               sync.Mutex
    watcherManager   *WatcherManager
    serveManager     *ServeManager
    apiManager       *APIManager
    pluginManager    *plugin.Manager
    clipboardService *ClipboardService
}
```

## Communication Protocol

### Frontend → Backend

JavaScript calls Go functions via Wails bindings:

```javascript
// Generated bindings in wailsjs/go/main/App.js
import { GetClips, UploadFiles } from '../wailsjs/go/main/App';

// GetClips takes archived flag, tag filter IDs, hidden tag IDs, sort field, and sort direction
const clips = await GetClips(false, [], [], 'date', 'desc');
```

### Backend → Frontend

Go emits events that JavaScript listens for:

```go
// Backend emits
runtime.EventsEmit(a.ctx, "watch:import", clip)

// Frontend listens
window.runtime.EventsOn("watch:import", (clip) => {
    showToast(`Imported: ${clip.filename}`);
    // Update gallery if clip matches current view filters
});
```

## Error Handling

### Backend Errors

Go functions return errors that are propagated to JavaScript:

```go
func (a *App) GetClipData(id int64) (*ClipData, error) {
    // ...
    if err != nil {
        return nil, fmt.Errorf("failed to get clip: %w", err)
    }
    return clip, nil
}
```

### Frontend Errors

JavaScript handles errors with try/catch:

```javascript
try {
    const clip = await GetClipData(id);
    // Use clip
} catch (error) {
    showToast('Failed to load clip');
    console.error(error);
}
```

## Security Considerations

### Local First

- All data stays on the user's machine
- No cloud sync or external APIs
- Optional HTTP servers: REST API (authenticated with Bearer tokens) and tag serve servers (cookie-based auth) can listen on configurable ports
- Plugins may make network requests with user permission

### File System Access

- Reads files user explicitly provides
- Watch folders require user configuration
- Temp files in app-specific directory
- Cleaned up on exit

### Database

- SQLite file in user data directory
- Standard file permissions
- No encryption (local data)
