# Tag Serve Feature Design

## Overview

Serve clips under a tag as a static HTTP server. Users can start multiple servers (one per tag) on configurable ports, bound locally or to all interfaces. Supports index.html rewrite and directory listing fallback.

## Architecture

### New Go Files

**`serve_manager.go`** — ServeManager struct managing multiple `http.Server` instances keyed by tag ID.

- Each server runs in its own goroutine
- Tracks request count per server (atomic int64)
- All servers stopped on app shutdown
- Serves clip data directly from SQLite on each request (no temp files)

**`serve_service.go`** — ServeService Wails-bound struct with methods:

- `StartServing(tagID int64, port int, bindAll bool) (ServeInfo, error)`
- `StopServing(tagID int64) error`
- `GetServeStatus() []ServeInfo`
- `GetRandomPort() (int, error)` — finds available port via `net.Listen(":0")`

### ServeInfo Struct

```go
type ServeInfo struct {
    TagID        int64  `json:"tag_id"`
    TagName      string `json:"tag_name"`
    Port         int    `json:"port"`
    BindAll      bool   `json:"bind_all"`
    URL          string `json:"url"`
    Running      bool   `json:"running"`
    RequestCount int64  `json:"request_count"`
}
```

### HTTP Handler Logic

Per-request flow for each tag server:

1. Query DB for all clips with the served tag, ordered by `created_at ASC`
2. Build virtual file list, resolving duplicate filenames: first keeps original name, subsequent get `name (2).ext`, `name (3).ext`
3. Route:
   - `/` or `/index.html` with an `index.html` clip present → serve that clip blob with content-type
   - Path matches a clip filename → serve blob with correct content-type header
   - `/` with no `index.html` → serve directory listing
   - Anything else → 404
4. Increment request counter (atomic)

### Directory Listing

- If `Accept` header contains `application/json` → JSON array: `[{name, size, content_type}]`
- Otherwise → stone-themed HTML page with IBM Plex Mono, listing filenames and sizes

### Duplicate Filename Resolution

On each request, build filename list from clips ordered by `created_at ASC`. First occurrence keeps original name, subsequent get `name (2).ext`, `name (3).ext` suffix.

## Frontend

### Navigation Refactor

Replace the current Watch button in drawer nav with a horizontal 3-way toggle strip below the "Menu" header:

```
┌──────────┬──────────┬──────────┐
│  [icon]  │  [icon]  │  [icon]  │
│  Clips   │  Watch   │  Serve   │
└──────────┴──────────┴──────────┘
```

- Each segment: large SVG icon (`w-6 h-6`) + small label (`text-[10px]`)
- Active: `bg-stone-800 text-white`, inactive: `text-stone-400 hover:bg-stone-100`
- Archive button stays separate (it's a filter, not a view)

### Serve View (`#serve-view`)

- Hidden by default, shown when "Serve" tab active
- List of serve cards, one per active/configured server:
  - Tag name + color dot
  - Clickable URL (copies to clipboard)
  - Request count badge
  - Start/Stop toggle button
  - Port input (small, inline)
  - Local/Network toggle (pill switch)
- "+ Serve a Tag" button at bottom — dropdown to pick a tag, auto-generates port

### State Management (`frontend/js/serve.js`)

- `isViewingServe` flag
- Polls `GetServeStatus()` every 2 seconds while serve view is visible
- Start/stop calls update UI immediately, poll confirms

## Wiring & Lifecycle

### `main.go`

- Create ServeService, add to Wails `Bind` list

### `app.go`

- Add `serveManager *ServeManager` field to App struct
- `startup()`: initialize ServeManager
- `shutdown()`: stop all running servers

### View Switching

- Refactor `toggleWatchView()` into `switchView(viewName)` — three states: `"clips"`, `"watch"`, `"serve"`
- Hides all view sections, shows selected, updates toggle styling
- Keyboard shortcuts: `W` → watch, `S` → serve

### Bindings

- Run `make bindings` after adding ServeService

### No Persistence

- Servers are ephemeral — stop when app quits
- No database table needed

## E2E Tests

New `e2e/tests/serve/` directory:

- Start serving a tag, verify HTTP response
- Stop serving, verify server down
- Directory listing (HTML and JSON via Accept header)
- index.html rewrite
- Duplicate filename handling
- Content negotiation
- Multiple simultaneous servers
