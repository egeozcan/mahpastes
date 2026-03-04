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
│                             └─────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                                       │
                                       ▼
                              ┌────────────────┐
                              │   SQLite DB    │
                              │   clips.db     │
                              └────────────────┘
```

## Technology Stack

### Backend (Go)

| Component | Technology | Purpose |
|-----------|------------|---------|
| Framework | Wails v2 | Desktop app framework |
| Database | SQLite 3 | Local data storage |
| Clipboard | golang.design/x/clipboard | Cross-platform clipboard |
| File Watch | fsnotify | Filesystem events |

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
main.go              Entry point, Wails setup
app.go               Core application logic, API methods
database.go          SQLite setup, schema, migrations
watcher.go           Folder watching, file import
backup.go            ZIP backup and restore
clipboard_service.go Clipboard copy service (Wails-bound)
clipboard_darwin.go  macOS clipboard via NSPasteboard (CGo)
transfer_service.go  Drag-out preparation and native drag
transfer_types.go    Transfer system type definitions
app_transfer_helpers.go  Bridge between App and TempClipStore
temp_clip_store.go   Leased temp file management
native_drag_darwin.go    macOS native drag via CGo
plugin_service.go    Plugin frontend API (separate struct for Wails binding limit)
plugins.go           Plugin install/uninstall helpers
plugin/              Lua plugin system
├── manager.go       Plugin lifecycle, event dispatch
├── manifest.go      Manifest parsing, validation
├── sandbox.go       Sandboxed Lua execution
├── scheduler.go     Scheduled/recurring plugin tasks
└── api_*.go         Lua APIs (clips, tags, storage, http, fs, utils, task, toast, image, modal)
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
│   ├── utils.js         Shared utilities
│   └── wails-api.js     Wails bindings wrapper
└── css/
    ├── main.css         Global styles, scrollbars, form styling
    └── modals.css       Modal-specific styles
```

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
    ctx            context.Context
    db             *sql.DB
    tempDir        string
    mu             sync.Mutex
    watcherManager *WatcherManager
    pluginManager  *plugin.Manager
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
runtime.EventsEmit(a.ctx, "watch:import", filename)

// Frontend listens
runtime.EventsOn("watch:import", (filename) => {
    showToast(`Imported: ${filename}`);
    loadClips();
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

### Local Only

- All data stays on the user's machine
- No network requests (except Wails dev mode and plugin HTTP APIs)
- No cloud sync or external APIs (plugins may make network requests with user permission)

### File System Access

- Reads files user explicitly provides
- Watch folders require user configuration
- Temp files in app-specific directory
- Cleaned up on exit

### Database

- SQLite file in user data directory
- Standard file permissions
- No encryption (local data)
