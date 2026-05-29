# Headless Server (`mahpastesd`) — Design Document

**Date:** 2026-05-27
**Status:** Draft

## Problem

mahpastes is a Wails desktop app. There is no way to run it on a headless server
as an always-on peer for P2P sharing, or to access it remotely via a web UI. A
user who wants a central clip repository must keep a desktop machine running.

## Goals

1. **Headless binary** (`mahpastesd`) — same Go core, no Wails dependency
2. **Web UI** — the existing frontend served over HTTP with API-key auth
3. **Always-on P2P peer** — keeps publications and follows alive 24/7
4. **Full REST API** — all existing API endpoints available on the server
5. **Plugin system** — plugins run headlessly (UI-only APIs emit via SSE if a web UI is connected, and desktop-only UI panels are hidden)
6. **Watch folders, tag serve, backup** — all non-GUI features intact
7. **Bootstrap story** — a fresh server install can create its first API key
   without needing a desktop app

## Non-Goals

- Multi-user data isolation (deployment-per-user via `MAHPASTES_DATA_DIR`)
- System clipboard operations (no desktop clipboard on server)
- Native drag-out / file dialogs (no windowing environment)
- Replacing the desktop app — this is a complement, not a rewrite

## Architecture

### The core problem: `package main` is not importable

The entire codebase lives in `package main`. A new `cmd/mahpastesd/` binary
cannot import `App`, `initDB`, `AcquireInstanceLock`, `NewAPIManager`, etc.
because Go does not allow importing `main` packages.

**Solution:** Extract the shared core into `internal/app/`, an importable
package. Both `cmd/mahpastes/` (desktop) and `cmd/mahpastesd/` (server)
import it.

### Project layout

```
mahpastes/
├── internal/
│   ├── app/                # NEW: shared core (package app)
│   │   ├── app.go          #   App struct, startup/shutdown, clip CRUD, tags, etc.
│   │   ├── database.go     #   getDataDir, initDB, schema
│   │   ├── api_manager.go  #   REST API HTTP server + routes
│   │   ├── share_*.go      #   P2P sharing (manager, protocol, codec, etc.)
│   │   ├── serve_*.go      #   Tag HTTP serving
│   │   ├── watcher.go      #   Watch folders
│   │   ├── backup.go       #   Backup/restore
│   │   ├── tag_hierarchy.go
│   │   ├── temp_clip_store.go
│   │   ├── transfer_*.go
│   │   ├── lock.go         #   Instance lock
│   │   └── bridgeiface/    #   NEW: event emitter interface
│   │       └── bridge.go
│   ├── wailsbridge/        # Wails runtime wrapper (desktop only)
│   │   ├── bridge.go       #   implements bridgeiface.Bridge
│   │   └── dialogs.go      #   file dialog helpers (desktop only)
│   └── plugin/             # Plugin system (most files unchanged)
│       └── ...
├── cmd/
│   ├── mahpastes/          # Desktop app entry point
│   │   └── main.go         #   wails.Run(), wires wailsbridge
│   ├── mahpastesd/         # NEW: headless server entry point
│   │   └── main.go
│   └── mp/                 # CLI (unchanged)
├── frontend/               # Shared by desktop AND server web UI
│   ├── index.html
│   ├── login.html          # NEW: login page for web UI auth
│   ├── js/
│   │   ├── wails-glue.js   # NEW: Wails bridge shim (desktop)
│   │   ├── rest-glue.js    # NEW: REST bridge shim (server web UI)
│   │   ├── wails-api.js    # Light refactor: calls through glue
│   │   ├── ui.js, modals.js, ... (unchanged)
│   └── css/
├── app.go                  # DELETED (moved to internal/app/)
├── database.go             # DELETED (moved to internal/app/)
├── api_manager.go          # DELETED (moved to internal/app/)
├── ... (all root .go files moved to internal/app/)
```

### What moves where

**To `internal/app/`** (importable core, no Wails imports):
- `app.go` — minus desktop-only methods (file dialogs, clipboard write, open-with).
  Gains `SetBridge(bridgeiface.Bridge)` so watch events can be wired to SSE.
- `database.go`
- `api_manager.go` — gains `mux` field, API key CRUD routes, and share CRUD routes
- `share_*.go` — share_manager already has `eventFn` for decoupled emits
- `serve_*.go`
- `watcher.go`
- `backup.go`
- `tag_hierarchy.go`, `temp_clip_store.go`, `transfer_*.go`, `lock.go`
- `api_service.go` → repurposed: not a Wails service, just a convenience
  constructor for the headless server to wire up API key endpoints

**To `frontend/` as `package webui`** (NEW Go files):
- `embed.go` — `//go:embed all:*` — embeds all HTML/JS/CSS assets
- `sse_broker.go` — SSE broker implementing `bridgeiface.Bridge`

**Stays at root level, becomes `cmd/mahpastes/` internal** (desktop-only):
- `main.go` → `cmd/mahpastes/main.go`
- `plugin_service.go` (Wails-bound plugin UI: file dialogs, import)
- `clipboard_service.go` + `clipboard_*.go` (platform clipboard)
- `transfer_service.go` + `native_drag_*.go` (drag-out)
- `open_*.go` (file-open dialogs)
- `share_service.go` (Wails-bound share UI)

**`plugin/`** — most files unchanged. The bridge parameter changes
from `*wailsbridge.Bridge` to `bridgeiface.Bridge`.

### Bridge / event emitter decoupling

Current Wails dependency footprint through the bridge:

| Bridge usage | Call sites | Server behavior |
|--------------|-----------|-----------------|
| `Emit(name, data...)` | `app.go` (watch events, dup detection), `plugin/manager.go` (modals, toasts), `plugin/api_task.go`, `plugin/api_toast.go`, `plugin/api_modal.go`, `plugin/update_checker.go`, `share_manager.go` (via `eventFn`) | SSE broker (web UI) or no-op |
| `On(name, cb)` | `plugin/api_modal.go:62` — `newModalGuard` subscribes to `plugin:modal:acked` and `plugin:modal:closed` from the frontend | Server: no-op (no frontend to send ack/close) |
| `OpenFile/SaveFile/OpenDirectory` | `app.go` (export, open-with), `plugin_service.go` (import dialog) | Desktop-only code, excluded from server build |

The two-way bridge surface (Emit + On) is extracted into an interface:

```go
// internal/app/bridgeiface/bridge.go
package bridgeiface

// Bridge is the bidirectional event surface between the Go core and a
// frontend (desktop WebView or browser SSE).
type Bridge interface {
    // Emit pushes an event from Go toward the frontend.
    Emit(name string, data ...any)
    // On subscribes to events sent from the frontend toward Go.
    On(name string, cb func(data ...any))
}

// NoOp discards outgoing events and ignores incoming subscriptions.
type NoOp struct{}

func (NoOp) Emit(name string, data ...any)            {}
func (NoOp) On(name string, cb func(data ...any))     {}
```

`internal/wailsbridge.Bridge` satisfies this interface (it already has both
`Emit` and `On`). The server passes `NoOp`; its modal guard will never block
because there is no frontend to ack/close modals. Plugins calling `modal.show()`
on the server complete immediately (the modal data is emitted via SSE if a web
UI is connected, but no ack is required).

Files that need dialog methods (`OpenFile`, `SaveFile`, `OpenDirectory`) in
addition to `Emit`/`On` keep the concrete `*wailsbridge.Bridge` type. These are
all desktop-only code paths excluded from the server binary.

### Frontend asset embedding

`//go:embed` patterns cannot contain `..` and are relative to the package
directory. The embed must live in a package whose directory physically
contains the assets. Placing the embed file directly in `frontend/` solves this:

```go
// frontend/embed.go
package webui

import "embed"

//go:embed all:*
var Assets embed.FS
```

The `frontend/` directory becomes a Go package (`package webui`) with two
files: `embed.go` (assets) and `sse_broker.go` (SSE broker implementing
`bridgeiface.Bridge`). Both `cmd/mahpastes/` and `cmd/mahpastesd/` import
`go-clipboard/frontend` and use `webui.Assets`.

Note: `internal/webui/` as a separate package is removed from the layout.
Everything webui-related lives in `frontend/` as `package webui`.

### Desktop-only methods in `App`

The `App` struct in `internal/app/` has methods that call platform-specific or
dialog functions. These are kept in `cmd/mahpastes/desktop_app.go` as a Wails-
bindable `DesktopApp` wrapper type that embeds `*app.App` and adds the dialog-
using methods. The frontend calls `window.go.main.App.*` — the wrapper preserves
exactly that binding surface:

```go
// cmd/mahpastes/desktop_app.go
type DesktopApp struct {
    *app.App
    bridge *wailsbridge.Bridge
}

func (d *DesktopApp) ShowCreateBackupDialog() (string, error) { ... }
func (d *DesktopApp) ShowRestoreBackupDialog() (*app.BackupManifest, string, error) { ... }
func (d *DesktopApp) ConfirmRestoreBackup(backupPath, identityPolicy string) error { ... }
func (d *DesktopApp) OpenClipWithDefaultApp(id int64) error { ... }
func (d *DesktopApp) OpenClipWithApp(id int64, appPath string) error { ... }
func (d *DesktopApp) ChooseApplication() (string, error) { ... }
func (d *DesktopApp) SelectFolder() (string, error) { ... }
func (d *DesktopApp) SaveClipToFile(id int64) error { ... }
func (d *DesktopApp) CopyToClipboard(text string) error { ... }
```

Methods that stay in `internal/app/` (no dialog, pure logic):
`AddWatchedFolder`, `CreateBackup`, `RestoreBackup`, all clip/tag CRUD.

This is cleaner than build tags and keeps the core package Wails-free.

### API key bootstrap

**Problem:** Today, API keys are created exclusively through the Wails desktop
UI (`api_service.go` → `APIManager.CreateKey`). The REST API has no key
management routes. The CLI help text for `mp api key create` says "API keys
must be created from the mahpastes desktop application." A headless server
needs another path.

**Solution (two parts):**

#### Part 1: Add API key CRUD to the REST API

Register these routes in `APIManager` (alongside all existing routes):

| Method | Endpoint | Role |
|--------|----------|------|
| `POST` | `/api/v1/keys` | admin |
| `GET` | `/api/v1/keys` | admin |
| `DELETE` | `/api/v1/keys/{id}` | admin |

The `mp` CLI is updated to call these endpoints instead of printing
"desktop app only."

#### Part 2: Bootstrap key on first run

When the server starts and the `api_keys` table is empty, generate an
admin key and print it to stdout:

```
$ mahpastesd
api: no API keys found — bootstrapping admin key
api: bootstrap admin key: mp_admin_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
api: store this key securely. it will not be printed again.
api: listening on http://127.0.0.1:44557
```

Uses `MAHPASTESD_BOOTSTRAP_KEY=false` to suppress (for scripted deploys
where the key is created via a config management tool).

#### Part 3: Key for mp CLI access

With the REST API key routes, `mp api key create` works against a running
server. The bootstrap key is used once to create a permanent admin key,
then optionally revoked.

```
$ export MP_API_KEY=mp_admin_xxxx
$ mp api key create my-key --role admin
$ export MP_API_KEY=<output from above>
$ mp api key revoke <bootstrap-key-id>
```

### APIManager refactor: owned mux

**Problem:** `APIManager.Start()` creates the `http.ServeMux` locally and
immediately starts serving. There is no way to add routes (web UI, SSE)
after `Start()` returns.

**Solution:** Add a `mux` field to `APIManager` and split route registration
from server start:

```go
type APIManager struct {
    app          *App
    mux          *http.ServeMux    // NEW: owned mux
    server       *http.Server
    mu           sync.RWMutex
    running      bool
    port         int
    bindAll      bool
    requestCount int64
}

func NewAPIManager(app *App) *APIManager {
    am := &APIManager{app: app}
    am.mux = http.NewServeMux()
    am.registerRoutes()    // all /api/v1/* routes
    return am
}

func (am *APIManager) Start(port int, bindAll bool) (APIStatus, error) {
    // ... validation ...
    handler := am.corsMiddleware(am.mux)
    am.server = &http.Server{Addr: addr, Handler: handler}
    // ... listen and serve ...
}

// MountWebUI adds static file + SSE routes. Must be called before Start().
func (am *APIManager) MountWebUI(assets fs.FS, sseHandler http.Handler) {
    am.mux.Handle("GET /api/v1/events", sseHandler)
    am.mux.Handle("/", spaHandler{assets: assets})
}
```

`Start()` no longer creates the mux — route registration happens in the
constructor, and web UI mounting happens before the server is started.

### Watcher initialization

**Problem:** The design sketch called `NewWatcherManager(ctx, db, app.handleWatchFile)`,
but `NewWatcherManager` actually takes `*App` directly and needs a subsequent
`wm.Start()` call.

**Fixed:**

```go
wm, err := app.NewWatcherManager(app)  // app is *internal/app.App
if err != nil {
    log.Printf("Warning: watcher manager: %v", err)
} else {
    app.WatcherManager = wm
    if err := wm.Start(); err != nil {
        log.Printf("Warning: watcher start: %v", err)
    }
}
```

## Web UI

### Strategy: glue layer, not replace wails-api.js

The desktop frontend has ~170 `window.go.main.*` call sites, but only ~30 of
them are in `wails-api.js`. The remaining ~140 are direct calls in:
`ui.js`, `plugins.js`, `serve.js`, `settings.js`, `maintenance.js`,
`shortcuts.js`, `modals.js`, `watch.js`, `share.js`, `metadata.js`,
`tag-autocomplete.js`, `tooltips.js`, `folder-move-modal.js`,
`folder-context-menu.js`.

Rewriting every call site is brittle and diverges the two frontends.

**Better approach:** Inject a glue object at the same location `window.go.main.*`
occupies in the desktop app. The glue provides the same method signatures,
backed by `fetch()` calls to the REST API.

```
Desktop:  window.go.main.App.GetClips(...)   → Wails → Go method
Server:   window.go.main.App.GetClips(...)   → fetch → REST API → Go method
```

The HTML page selects the glue at load time:

```html
<!-- In index.html, before any other JS -->
<script>
  // Server web UI: inject REST-backed glue before app code loads
  if (window.location.hostname !== 'wails.localhost') {
    document.write('<script src="/js/rest-glue.js"><\/script>');
  }
</script>
```

#### Glue registration pattern

`rest-glue.js` registers the shared `window.go.main.*` namespaces and methods
that the server UI keeps visible. Desktop-only methods are not treated as
successful no-ops; the corresponding panels/actions are hidden in server mode.

```javascript
// rest-glue.js — mimics the Wails binding surface via fetch()
const API = '/api/v1';
const AUTH = () => ({ 'Authorization': 'Bearer ' + getSessionKey() });

window.go = { main: {} };

// App methods
window.go.main.App = {
    GetClips: (archived, tagIds, hiddenIds, sortField, sortDir) =>
        getJSON(`/api/v1/clips?${params}`),

    DeleteClip: (id) =>
        del(`/api/v1/clips/${id}`),

    UploadFiles: (files, expiration, autoTagID) => {
        const fd = new FormData();
        files.forEach(f => fd.append('files', f));
        return postJSON('/api/v1/clips', fd);
    },

    // ... every method used by visible server-mode UI
};

// Service namespaces
window.go.main.PluginService = { ... };
window.go.main.ServeService = { ... };
window.go.main.ShareService = { ... };
window.go.main.ClipboardService = { ... };   // compatibility only; UI hidden
window.go.main.TransferService = { ... };     // compatibility only; UI hidden
window.go.main.APIService = { ... };
```

#### Server-mode UI policy

The web UI is not required to expose every desktop affordance. Server mode adds
a small capability flag (`window.mahpastesMode = "server"` and a `server-mode`
body class) before app scripts load. UI modules use that flag to hide actions
whose semantics depend on native desktop APIs.

| Desktop surface | Server-mode behavior |
|-----------------|----------------------|
| System clipboard copy | Hidden. Browser clipboard support may be added later as an explicit browser feature. |
| Native drag-out / transfer service | Hidden. Use HTTP downloads instead. |
| Open with default app / choose application | Hidden. Browser downloads or previews files. |
| Native backup save/open dialogs | Replaced with HTTP backup download/upload flows, not fake dialog APIs. |
| API server start/stop controls | Hidden. `mahpastesd` owns the API listener; the UI keeps API key management and logout. |
| Local plugin import dialog | Hidden. URL install/update and plugin management remain available. |
| Watch folder picker / file-drop path capture | Hidden. Watch folders are configured by typing server-side paths. |

The glue layer may still provide harmless runtime compatibility shims (for
example `window.runtime.OnFileDrop` and `EventsEmit`) so existing scripts can
load, but user-facing desktop-only actions should not appear in server mode.

#### Server REST coverage matrix

Before implementing `rest-glue.js`, generate a current inventory of
`window.go.main.*` and `window.runtime.*` call sites and classify each as:

1. **Expose in server UI** — backed by an existing or new REST endpoint.
2. **Expose with browser semantics** — e.g. download/upload instead of native
   save/open dialogs.
3. **Hide in server UI** — desktop-only; glue may throw a clear
   `desktop-only in server mode` error if accidentally called.

The server UI is considered complete when all visible controls map to category
1 or 2 and all category 3 controls are hidden by the server-mode flag.

#### Events via SSE

The desktop app receives events via Wails' `EventsOn`:

```javascript
// desktop: wails runtime
window.runtime.EventsOn('watch:import', (data) => { ... });
window.runtime.EventsOn('plugin:toast', (data) => { ... });
```

The server glue provides equivalent SSE-based listeners:

```javascript
// rest-glue.js
const eventSource = new EventSource('/api/v1/events');
window.runtime = {
    EventsOn: (name, cb) => {
        eventSource.addEventListener(name, (e) => cb(JSON.parse(e.data)));
    },
    EventsOff: (name) => { ... },
    EventsEmit: () => {},
    OnFileDrop: () => () => {},
};
```

### Static file serving

Assets are embedded via `frontend/embed.go` (`//go:embed all:*`) in `package webui`
and mounted at `/` via the SPA handler registered in `APIManager.MountWebUI()`.

Unauthenticated requests to non-API paths redirect to `/login.html`.

### Authentication for web UI

**Design constraint:** The browser's native `EventSource` (SSE) cannot set
custom headers, so it cannot send `Authorization: Bearer <key>`. And an
HTTP-only cookie cannot be read by JavaScript, so `rest-glue.js` cannot
extract it to set the Authorization header.

**Solution:** The auth middleware accepts **two** credential types:

1. `Authorization: Bearer <key>` header (used by `mp` CLI and external tools)
2. `_mp_session` cookie (used by the web UI and SSE connections)

The login flow sets the cookie; all subsequent requests (fetch and SSE)
automatically include it. The glue layer never touches the API key directly.

1. **`/login.html`** — a simple form (Stone design, IBM Plex Mono) asking for
   an API key. Submitted via `POST /api/v1/auth/login`.
2. `POST /api/v1/auth/login` validates the key and sets a session cookie
   (`_mp_session`, HTTP-only, SameSite=Strict, signed with the server's
   HMAC to prevent forgery).
3. The SPA handler checks the session cookie; unauthenticated requests
   redirect to `/login.html`.
4. All REST API calls from `rest-glue.js` go through `fetch()` without any
   `Authorization` header — the browser sends the cookie automatically.
5. The SSE endpoint (`GET /api/v1/events`) validates the session cookie
   the same way — `EventSource` sends cookies automatically.
6. `POST /api/v1/auth/logout` clears the session cookie.

```javascript
// rest-glue.js — no Authorization header needed
async function fetchJSON(url, opts = {}) {
    const res = await fetch(url, {
        ...opts,
        credentials: 'same-origin',  // send _mp_session cookie
    });
    if (res.status === 401) {
        window.location = '/login.html';
        throw new Error('Unauthorized');
    }
    return res.json();
}
```

The login page is minimal — a centered card with a single input field and
a "Sign in with API key" button. Matches the existing Stone design language.

## Plugin behavior on the server

| Plugin API | Desktop | Server |
|------------|---------|--------|
| `clips.*` | Full access | Full access |
| `tags.*` | Full access | Full access |
| `storage.*` | Full access | Full access |
| `http.*` | Full access | Full access |
| `fs.*` | Permission prompt via native dialog | Permission via REST API endpoint |
| `image.*` | Full access | Full access |
| `utils.*` | Full access | `clipboard_write` is no-op |
| `metadata.*` | Full access | Full access |
| `task.*` | Progress in desktop UI | Events via SSE (if web UI connected) |
| `toast.*` | Toast in desktop UI | Events via SSE |
| `modal.*` | Modal in desktop UI | Events via SSE |

Plugin event handlers run identically. Only the output surface differs.

## Sync via P2P

No changes to the share system. The server runs `shareManager.ResumeAll()` at
startup, re-establishing all publications and follows from the database.

```
┌──────────────┐     libp2p      ┌──────────────┐     libp2p      ┌──────────────┐
│  Desktop A   │ ←────────────→ │  mahpastesd  │ ←────────────→ │  Desktop B   │
│  (publisher) │    pub/sub     │  (always-on) │    pub/sub     │  (follower)  │
└──────────────┘                └──────────────┘                └──────────────┘
```

- Desktop publishes a tag → server follows it (clips replicate to server)
- Server publishes a tag → desktop follows it (clips replicate to desktop)
- The server's always-on nature means peers can sync even when other desktops
  are offline
- No new protocol, no hub-and-spoke — the existing P2P system scales naturally

## Per-user isolation

**Phase 1 (this design):** Deployment-per-user. Each user runs their own
`mahpastesd` instance with a separate `MAHPASTES_DATA_DIR`. The binary is
identical; isolation comes from separate data directories and ports.

```bash
# User alice
MAHPASTES_DATA_DIR=/var/lib/mahpastes/alice mahpastesd

# User bob
MAHPASTES_DATA_DIR=/var/lib/mahpastes/bob MAHPASTESD_PORT=44558 mahpastesd
```

**Phase 2 (future):** Multi-tenant with a `user_id` column on clips, tags,
and shares. Scoped API keys per user. Not in scope for this design.

## Server entry point (`cmd/mahpastesd/main.go`)

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "strconv"
    "strings"
    "syscall"
    "time"

    app "go-clipboard/internal/app"
    "go-clipboard/plugin"
    webui "go-clipboard/frontend"
)

func main() {
    log.SetFlags(log.LstdFlags | log.Lshortfile)

    dataDir, err := app.GetDataDir()
    if err != nil {
        log.Fatalf("data dir: %v", err)
    }

    lock, err := app.AcquireInstanceLock(dataDir)
    if err != nil {
        log.Fatalf("%v", err)
    }
    defer lock.Release()

    db, err := app.InitDB()
    if err != nil {
        log.Fatalf("db: %v", err)
    }
    defer db.Close()

    core := app.NewApp()
    core.SetDB(db)
    core.SetDataDir(dataDir)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // SSE broker (single instance shared across all event producers)
    broker := webui.NewSSEBroker()

    // Wire bridge so App watch/duplicate events go to SSE
    core.SetBridge(broker)

    // Expired-clip cleanup (same as desktop)
    app.StartCleanupJob(db)

    // Temp clip store (needed by transfer API)
    tempDir := filepath.Join(dataDir, "temp")
    if err := os.MkdirAll(tempDir, 0755); err != nil {
        log.Printf("Warning: temp dir: %v", err)
    } else {
        core.InitTempStore(db, tempDir)
    }

    // Plugin manager (wired to SSE broker)
    pluginsDir := filepath.Join(dataDir, "plugins")
    pluginMgr, err := plugin.NewManager(ctx, broker, db, pluginsDir)
    if err != nil {
        log.Printf("Warning: plugin manager: %v", err)
    } else {
        core.SetPluginManager(pluginMgr)
        // Wire callbacks required by plugin Lua APIs
        pluginMgr.SetMetadataFuncs(core.GetClipMetadata, core.UpdateClipMetadata)
        pluginMgr.SetTagCreateFunc(func(name string) (*plugin.TagCreateResult, error) {
            tag, err := core.CreateTag(name)
            if err != nil {
                return nil, err
            }
            return &plugin.TagCreateResult{ID: tag.ID, Name: tag.Name, Color: tag.Color}, nil
        })
        pluginMgr.SetPermissionCallback(func(pluginName, permType, requestedPath string) string {
            // On headless: auto-grant if within data dir, otherwise deny
            if strings.HasPrefix(requestedPath, dataDir) {
                return requestedPath
            }
            return ""
        })
        // Load plugins and wire update checker
        if err := pluginMgr.LoadPlugins(); err != nil {
            log.Printf("Warning: load plugins: %v", err)
        }
        pluginMgr.EmitEvent("app:startup", nil)
        uc := plugin.NewUpdateChecker(ctx, broker, db, pluginMgr)
        pluginMgr.SetUpdateChecker(uc)
        uc.Start(24 * time.Hour) // check daily
    }

    // Share manager (wired to same SSE broker)
    shareMgr, err := app.NewShareManager(ctx, db, dataDir)
    if err != nil {
        log.Printf("Warning: share manager: %v", err)
    } else {
        core.SetShareManager(shareMgr)
        shareMgr.SetEventFn(broker.Emit)
        shareMgr.ResumeAll()
        shareMgr.StartSweepers() // ring + staging cleanup
    }

    // Watcher manager
    wm, err := app.NewWatcherManager(core)
    if err != nil {
        log.Printf("Warning: watcher manager: %v", err)
    } else {
        core.SetWatcherManager(wm)
        if err := wm.Start(); err != nil {
            log.Printf("Warning: watcher start: %v", err)
        }
    }

    // Serve manager
    core.SetServeManager(app.NewServeManager(core))

    // REST API + web UI (same broker for /events endpoint)
    apiMgr := app.NewAPIManager(core)
    apiMgr.MountWebUI(webui.Assets, broker)

    // Bootstrap API key if none exist
    app.BootstrapAPIKey(apiMgr)

    port := 44557
    if p := os.Getenv("MAHPASTESD_PORT"); p != "" {
        if parsed, err := strconv.Atoi(p); err == nil {
            port = parsed
        }
    }
    bindAll := os.Getenv("MAHPASTESD_BIND_ALL") == "1"

    // Warn if binding to all interfaces without TLS
    if bindAll {
        log.Println("WARNING: listening on 0.0.0.0 without TLS.")
        log.Println("API keys are sent in plaintext. Place behind a reverse proxy")
        log.Println("(nginx, Caddy) that terminates TLS before exposing to a network.")
    }

    status, err := apiMgr.Start(port, bindAll)
    if err != nil {
        log.Fatalf("api: %v", err)
    }
    log.Printf("listening on %s", status.URL)

    // Block
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    log.Println("shutting down...")
    if pluginMgr != nil {
        pluginMgr.EmitEvent("app:shutdown", nil)
    }
    apiMgr.Stop()
}
```

## Server-mode UI policy

Not every desktop affordance belongs in the web UI. Server mode sets a
capability flag (`window.mahpastesMode = "server"` and a `server-mode` CSS
class on `<body>`) before app scripts load. UI modules check this flag to
hide actions that depend on native desktop APIs.

| Desktop surface | Server-mode behavior |
|-----------------|----------------------|
| System clipboard copy | Hidden |
| Native drag-out / transfer service | Hidden; use HTTP downloads |
| Open with default app / choose application | Hidden; browser downloads or previews |
| Native backup save/open dialogs | Replaced with HTTP download/upload flows |
| API server start/stop controls | Hidden; server owns its listener |
| Local plugin import dialog | Hidden; URL install/update remain |
| Watch folder file-drop path capture | Hidden; paths typed as server-side values |

### Wails-to-REST method matrix

The `rest-glue.js` shim implements the `window.go.main.*` surface for visible
server-mode UI. Desktop-only methods map to no-ops or are hidden by the
server-mode flag.

#### App methods

| Wails method | REST endpoint | Notes |
|-------------|-------------|-------|
| `GetClips`, `GetFolderClips`, `GetUntaggedClips` | `GET /api/v1/clips?archived=&tag=&hidden=&sort=&dir=` | Existing |
| `GetClipData` | `GET /api/v1/clips/{id}/data` | Existing |
| `UploadFiles` | `POST /api/v1/clips` (multipart) | Existing |
| `DeleteClip` | `DELETE /api/v1/clips/{id}` | Existing |
| `RenameClip` | `PATCH /api/v1/clips/{id}` | Existing |
| `ToggleArchive` | `PUT/DELETE /api/v1/clips/{id}/archive` | Existing |
| `BulkDelete`, `BulkArchive`, `BulkUnarchive` | `POST /api/v1/clips/bulk/*` | Existing |
| `BulkDownloadToFile` | `POST /api/v1/clips/bulk/download` | Existing |
| `BulkAddTag`, `BulkRemoveTag` | `POST /api/v1/clips/bulk/tag`, `/untag` | Existing |
| `SetExpiration`, `CancelExpiration` | `PUT/DELETE /api/v1/clips/{id}/expiration` | Existing |
| `BulkSetExpiration` | `POST /api/v1/clips/bulk/expire` | Existing |
| `BulkCancelExpiration` | `POST /api/v1/clips/bulk/cancel-expire` | Existing |
| `GetTags`, `CreateTag`, `UpdateTag`, `DeleteTag` | `GET/POST/PUT/DELETE /api/v1/tags` | Existing |
| `AddTagToClip`, `RemoveTagFromClip` | `PUT/DELETE /api/v1/clips/{id}/tags/{tagId}` | Existing |
| `GetChildTags`, `GetTopLevelTags`, `GetDescendantClipCount`, `GetClipsDirect` | `GET /api/v1/tags/{id}/children`, `/clips` | Existing |
| `GetHiddenTags`, `SetHiddenTags` | `GET/PUT /api/v1/tags/hidden` | Existing |
| `GetClipMetadata` | `GET /api/v1/clips/{id}/metadata` | Existing |
| `SetClipMetadataBulk` | `PUT /api/v1/clips/{id}/metadata` | Existing |
| `GetSetting`, `SetSetting` | `GET/PUT /api/v1/settings/{key}` | **NEW** |
| `GetDuplicateGroups`, `DeduplicateAll`, `MergeDuplicates` | `GET /api/v1/dedup`, `POST /api/v1/dedup/all`, `/{clipId}/merge` | Existing |
| `GetRemovableEmptyTags`, `RemoveEmptyTags` | `GET/POST /api/v1/maintenance/empty-tags` | **NEW** |
| `GetDatabaseSize`, `CompactDatabase` | `GET/POST /api/v1/maintenance/database` | **NEW** |
| `GetStaleFiles`, `CleanStaleFiles` | `GET/POST /api/v1/maintenance/stale-files` | Existing |
| `GetOrphanDBRows`, `CleanOrphanDBRows` | `GET/POST /api/v1/maintenance/orphan-rows` | Existing |
| `FindClipsByFilenameAndTag` | `POST /api/v1/clips/find` (body: `{filenames, tag_id}`; exact filename match, not fuzzy) | **NEW** |
| `GetImageDiff` | `POST /api/v1/clips/diff` | **NEW** |
| `PreviewMergeTag` | `POST /api/v1/tags/{id}/merge-preview` (non-mutating dry-run; separate from merge route to avoid accidental mutation) | **NEW** |
| `MergeTag` | `POST /api/v1/tags/{id}/merge` | Existing |
| `GetWatchStatus`, `GetWatchedFolders` | `GET /api/v1/watch`, `/status` | Existing |
| `GetGlobalWatchPaused` | `GET /api/v1/watch/status` | Existing |
| `SetGlobalWatchPaused` | `PUT/DELETE /api/v1/watch/global-pause` | Existing |
| `AddWatchedFolder` | `POST /api/v1/watch` | Existing |
| `RemoveWatchedFolder` | `DELETE /api/v1/watch/{id}` | Existing |
| `UpdateWatchedFolder` | `PUT /api/v1/watch/{id}` | Existing |
| `SetFolderPaused` | `PUT/DELETE /api/v1/watch/{id}/pause` | Existing |
| `ProcessExistingFilesInFolder` | `POST /api/v1/watch/{id}/process` | Existing |
| `RefreshWatches` | `POST /api/v1/watch/refresh` | **NEW** |
| `SelectFolder` | Hidden (desktop file dialog; server uses typed paths) | Desktop-only |
| `IsDirectory` | Hidden (desktop file-system check; server uses typed paths) | Desktop-only |
| `SaveClipToFile` | Browser download via `GET /api/v1/clips/{id}/data` | Adapted |
| `UploadFileAndGetID` | `POST /api/v1/clips` (multipart, returns `{id}`) | Existing |
| `CreateTempFile` | `POST /api/v1/clips/{id}/temp` | **NEW** |
| `DeleteAllTempFiles` | `DELETE /api/v1/temp` | **NEW** |
| `CopyToClipboard` | Hidden (server-mode flag) | Desktop-only |
| `OpenClipWithDefaultApp`, `OpenClipWithApp`, `ChooseApplication` | Hidden | Desktop-only |
| `ShowCreateBackupDialog`, `ShowRestoreBackupDialog`, `BackupInspect`, `ConfirmRestoreBackup` | `GET /api/v1/backup` + `POST /api/v1/backup/restore` via browser dialogs | Adapted |
| `UpdateClipData` | `PUT /api/v1/clips/{id}/data` (JSON: `{content_type, data, filename}`; capped at ~150MB request to fit a 100MB clip after base64 inflation) | **NEW** |

#### Service namespaces

| Wails method | REST endpoint | Notes |
|-------------|-------------|-------|
| **ServeService** | | |
| `GetServeStatus` | `GET /api/v1/serve` | Existing |
| `StartServing` | `POST /api/v1/serve` | Existing |
| `StopServing` | `DELETE /api/v1/serve/{tagId}` | Existing |
| `GetRandomPort` | `GET /api/v1/serve/random-port` | **NEW** |
| **ShareService** | | |
| `GetShareStatus` | `GET /api/v1/share` | **NEW** |
| `StartShare`, `StopShare`, `PauseShare`, `ResumeShare` | `POST/DELETE/PUT/DELETE /api/v1/share/publish/*` | **NEW** |
| `Follow`, `Unfollow`, `PauseFollow`, `ResumeFollow`, `ReconnectFollow` | `POST/DELETE/PUT/DELETE/POST /api/v1/share/follow/*` | **NEW** |
| `TestFollowConnection`, `FollowWithoutDial` | `POST /api/v1/share/test-follow`, `/follow-direct` | **NEW** |
| `UpdateFollowTag` | `PUT /api/v1/share/follow/{id}/tag` | **NEW** |
| `GetShareLogs` | `GET /api/v1/share/logs` | **NEW** |
| **PluginService** | | |
| `GetPlugins` | `GET /api/v1/plugins` | Existing |
| `GetPluginUIActions` | `GET /api/v1/plugins/actions` | Existing |
| `ExecutePluginAction` | `POST /api/v1/plugins/{id}/actions/{actionId}` | Existing |
| `ImportPlugin`, `ConfirmPluginInstall` | `POST /api/v1/plugins` + `POST /api/v1/plugins/confirm` | Existing / **NEW** |
| `PreviewPluginFromURL` | `POST /api/v1/plugins/preview` | **NEW** |
| `RemovePlugin` | `DELETE /api/v1/plugins/{id}` | Existing |
| `EnablePlugin`, `DisablePlugin` | `PUT /api/v1/plugins/{id}/enable`, `/disable` | Existing |
| `UpdatePlugin`, `ConfirmPluginUpdate` | `POST /api/v1/plugins/{id}/update` | Existing |
| `GetPluginPermissions` | `GET /api/v1/plugins/{id}/permissions` | **NEW** |
| `RevokePluginPermission` | `DELETE /api/v1/plugins/{id}/permissions/{type}/{path}` | **NEW** |
| `SetPluginStorage`, `GetAllPluginStorage` | `PUT /api/v1/plugins/{id}/storage/{key}`, `GET /api/v1/plugins/{id}/storage` | Existing |
| `TryAcquireModalGuard` | Local guard in `rest-glue.js` | Client-side |
| `IsPluginURLAllowed` | `POST /api/v1/plugins/{id}/url-check` | **NEW** |
| `GetUpdateCheckInterval`, `SetUpdateCheckInterval` | `GET/PUT /api/v1/settings/plugin_update_interval` | Via settings |
| **ClipboardService** | Hidden | Desktop-only |
| **TransferService** | Hidden | Desktop-only |
| **APIService** | | |
| `GetAPIStatus` | `GET /api/v1/status` | **NEW** |
| `StartAPI`, `StopAPI` | Hidden (server owns its listener) | |
| `CreateAPIKey`, `ListAPIKeys`, `RevokeAPIKey` | `POST/GET/DELETE /api/v1/keys` | **NEW** |

### Runtime shim

The browser doesn't have Wails' `window.runtime.*`. The shim provides:

| Runtime method | Server shim |
|---------------|-------------|
| `EventsOn(name, cb)` | SSE `addEventListener(name, cb)` |
| `EventsOff(name)` | SSE `removeEventListener(name)` |
| `EventsEmit(name, data)` | No-op (`plugin:modal:acked`/`closed` not needed; server modal guard never blocks) |
| `OnFileDrop(cb)` | No-op (browser uses existing button/dropzone for uploads) |

```javascript
// rest-glue.js — runtime shim
const _eventSource = new EventSource('/api/v1/events');
const _listeners = new Map();

window.runtime = {
    EventsOn(name, cb) {
        const wrapper = (e) => cb(JSON.parse(e.data));
        _listeners.set(cb, { name, wrapper });
        _eventSource.addEventListener(name, wrapper);
    },
    EventsOff(name) {
        for (const [cb, info] of _listeners) {
            if (info.name === name) {
                _eventSource.removeEventListener(name, info.wrapper);
                _listeners.delete(cb);
            }
        }
    },
    EventsEmit() { /* no-op */ },
    OnFileDrop() { /* no-op */ },
};
```

## Security: default to localhost

By default, `mahpastesd` binds to `127.0.0.1` (localhost only). The API key
is sent over plain HTTP — acceptable on loopback but dangerous on a network
interface.

**Remote access requires explicit steps:**

1. Set `MAHPASTESD_BIND_ALL=1` to listen on `0.0.0.0`
2. Place the server behind a reverse proxy (nginx, Caddy) that terminates TLS

| Env var | Default | Purpose |
|---------|---------|---------|
| `MAHPASTES_DATA_DIR` | OS-appropriate data dir | Database, plugins, identity |
| `MAHPASTESD_PORT` | `44557` | HTTP listen port |
| `MAHPASTESD_BIND_ALL` | unset (localhost) | Set to `1` to listen on all interfaces |
| `MAHPASTESD_BOOTSTRAP_KEY` | auto | Set to `false` to suppress bootstrap key |

Built-in TLS (`MAHPASTESD_TLS_CERT` / `MAHPASTESD_TLS_KEY`) is deferred to a
future phase. For now, remote access uses a reverse proxy for TLS termination.

The bootstrap key is printed to stdout once. The startup banner includes a
warning when binding to all interfaces without TLS.

## Implementation order

1. **Extract `internal/app/`** — move all root `.go` files into
   `internal/app/`, change `package main` → `package app`. Fix imports.
   Create `cmd/mahpastes/main.go` that imports `internal/app` and wires
   Wails. Run e2e tests to confirm desktop app works identically.

2. **Add `bridgeiface.Bridge`** — create the interface (`Emit` + `On`),
   change `app.go` and `plugin/` to use it instead of `*wailsbridge.Bridge`.
   Desktop passes the wailsbridge (which satisfies the interface); server
   passes `NoOp` or the SSE broker.

3. **Add `frontend/` Go files** — `embed.go` (`//go:embed all:*`) and
   `sse_broker.go` (SSE broker implementing `bridgeiface.Bridge`), both
   in `package webui`.

4. **APIManager refactor** — add `mux` field, split `registerRoutes()`
   and `Start()`, add `MountWebUI()`. Add API key CRUD routes
   (`/api/v1/keys`). Add share CRUD routes (see Share REST API routes).
   Update auth middleware to accept both `Authorization: Bearer` header
   and `_mp_session` cookie.

5. **Bootstrap key** — `BootstrapAPIKey()` in `internal/app/`. Update
   `cmd/mp/api.go` to call the REST endpoints instead of printing
   "desktop app only."

6. **Server entry point** — `cmd/mahpastesd/main.go` as shown above.
   Verify with `mp` CLI against a running server.

7. **Web UI glue + server-mode gating** — inventory all `window.go.main.*`
   and `window.runtime.*` call sites, classify them with the server REST
   coverage matrix, hide desktop-only panels/actions behind the server-mode
   flag, and implement `frontend/js/rest-glue.js` for the visible shared
   surface via `fetch()` (no Authorization header, cookie-based).
   `frontend/login.html` handles auth.

8. **Build targets** — Makefile additions for `mahpastesd`,
   `mahpastesd-cross`. Update `make bindings` if needed.

## Share REST API routes

The existing REST API has no share endpoints. `share_service.go` exposes 15
methods to the Wails frontend, but none are reachable via HTTP. The server
needs these routes to support the share UI in the web frontend:

| Method | Endpoint | Role | Maps to |
|--------|----------|------|---------|
| `GET` | `/api/v1/share` | viewer | `ShareManager.GetShareStatus()` → `ShareStatus` |
| `POST` | `/api/v1/share/publish` | admin | `ShareManager.StartShare(tagID)` |
| `DELETE` | `/api/v1/share/publish/{tagId}` | admin | `ShareManager.StopShare(tagID)` |
| `PUT` | `/api/v1/share/publish/{tagId}/pause` | admin | `ShareManager.PauseShare(tagID)` |
| `DELETE` | `/api/v1/share/publish/{tagId}/pause` | admin | `ShareManager.ResumeShare(tagID)` |
| `POST` | `/api/v1/share/follow` | admin | `ShareManager.Follow(shareString, localTagName)` |
| `POST` | `/api/v1/share/test-follow` | admin | `ShareManager.TestFollowConnection(shareString)` |
| `POST` | `/api/v1/share/follow-direct` | admin | `ShareManager.FollowWithoutDial(shareString, localTagName)` |
| `DELETE` | `/api/v1/share/follow/{id}` | admin | `ShareManager.Unfollow(followID)` |
| `POST` | `/api/v1/share/follow/{id}/reconnect` | admin | `ShareManager.ReconnectFollow(followID)` |
| `PUT` | `/api/v1/share/follow/{id}/pause` | admin | `ShareManager.PauseFollow(followID)` |
| `DELETE` | `/api/v1/share/follow/{id}/pause` | admin | `ShareManager.ResumeFollow(followID)` |
| `PUT` | `/api/v1/share/follow/{id}/tag` | admin | `ShareManager.UpdateFollowTag(followID, newLocalTagName)` |
| `GET` | `/api/v1/share/logs` | viewer | `ShareManager.GetShareLogs(followID, publicationID)` |

The share glue in `rest-glue.js` maps `window.go.main.ShareService.*` to these
endpoints. The share manager is already headless-capable — these are just HTTP
wrappers around existing method calls.

## What this does NOT include

- No multi-user database isolation (phase 2)
- No Docker image (trivial to add — single static binary)
- No systemd service file (trivial to add)
- No changes to the share protocol or P2P system
- No replacing the desktop app — `cmd/mahpastes/` continues to exist
- No changes to the frontend UI design — the Stone design language,
  IBM Plex Mono, and all component patterns remain identical
