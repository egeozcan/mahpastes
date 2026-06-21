# Headless Server (`mahpastesd`) — Implementation Plan

**Date:** 2026-05-27 (rev 6 — incorporates fifth-pass review)
**Status:** Draft — awaiting review

**Goal:** Create a headless server binary (`mahpastesd`) that reuses the existing
Go core to provide a REST API, web UI, P2P sharing, watch folders, tag serve,
and the plugin system — all without a Wails dependency.

**Architecture:** Extract the shared core into `internal/app/` (importable
package). The desktop entry (`main.go`, `wails.json`, `frontend/`) stays at
the repo root so Wails v2's expected layout is preserved. The new server entry
lives at `cmd/mahpastesd/`. Both binaries import `internal/app/`. The bridge
is abstracted behind a `bridgeiface.Bridge` interface; desktop passes a
`*wailsbridge.Bridge` and server passes the SSE broker.

**Design doc:** `docs/plans/2026-05-27-headless-server-design.md`

## Project layout after this plan

```
mahpastes/
├── wails.json                  # unchanged — Wails v2 needs this at root
├── main.go                     # desktop entry, now imports internal/app
├── desktop_app.go              # NEW — type App embeds *app.App + dialog methods
├── plugin_service.go           # stays at root (uses bridge dialog)
├── share_service.go            # stays at root (Wails binding lives at window.go.main.ShareService)
├── serve_service.go            # stays at root (ditto)
├── api_service.go              # stays at root (ditto)
├── clipboard_service.go        # stays at root (calls platform code in same package)
├── clipboard_darwin.go         # stays at root (platform)
├── clipboard_windows.go        #   ditto
├── clipboard_other.go          #   ditto
├── transfer_service.go         # stays at root (platform code + uses moved transfer types via app.X)
├── native_drag_darwin.go       #   ditto
├── native_drag_windows.go      #   ditto
├── native_drag_other.go        #   ditto
├── open_darwin.go              # stays at root (platform)
├── open_windows.go             #   ditto
├── open_other.go               #   ditto
├── frontend/                   # unchanged location
│   ├── embed.go                # NEW: package webui
│   ├── sse_broker.go           # NEW
│   ├── login.html              # NEW
│   └── ...
├── internal/
│   ├── app/                    # NEW — shared core (package app)
│   │   ├── app.go
│   │   ├── database.go
│   │   ├── api_manager.go
│   │   ├── share_*.go
│   │   ├── serve_*.go
│   │   ├── watcher.go
│   │   ├── backup.go
│   │   ├── maintenance.go
│   │   ├── plugins.go
│   │   ├── tag_hierarchy.go
│   │   ├── temp_clip_store.go
│   │   ├── transfer_handler.go
│   │   ├── transfer_types.go
│   │   ├── app_transfer_helpers.go
│   │   ├── instance_lock*.go
│   │   ├── session.go          # NEW (Task 7)
│   │   └── bootstrap_key.go    # NEW (Task 8)
│   ├── bridgeiface/            # NEW
│   │   └── bridge.go
│   └── wailsbridge/            # unchanged
└── cmd/
    └── mahpastesd/             # NEW — headless server
        └── main.go
```

## Task ordering rationale

`App.startup()` is the risky piece — ~100 LOC of inline orchestration
(`app.go:138-255`). Both binaries need this orchestration. Task 1 extracts it
into a `Bootstrap(ctx, opts)` method in place, while the code is still in
`package main` at the root and Wails still drives the lifecycle. That lets
e2e verify behavior is unchanged before any move.

Task 2 narrows only `plugin/` to `bridgeiface.Bridge`. App's `bridge` field
stays `*wailsbridge.Bridge` until Task 4 — because app.go still has dialog
calls (`a.bridge.SaveFile`, `OpenFile`, `OpenDirectory`) that don't exist on
the narrow interface. Those dialog calls migrate to the `desktop_app.go`
wrapper in Task 4, and only then is App's bridge field narrowed.

Task 3 creates the frontend Go package (embed + SSE broker). Task 4 moves the
core to `internal/app/`. Each step is independently buildable and e2e-passable.

---

## Task 1: Refactor `App.startup()` into `Bootstrap()` + setters + accessors (in place)

**Purpose:** Extract the inline `startup` body into a reusable orchestrator
and add the exported accessors that root-level services will need once App
moves to `internal/app/` (Task 4). Everything is still in `package main` at
the repo root; the desktop app keeps working because `startup` becomes a
thin wrapper that calls `Bootstrap`.

**Files modified:** `app.go`, `database.go`, `share_manager.go`, `backup.go`,
`main.go`.

### Step 1: Add setters and accessors to App

```go
// app.go — additions

// ClipboardCopier is the surface api_manager.go's clipboard handlers need.
// Defined here so App can hold the concrete *ClipboardService (at root)
// without internal/app importing the root package — *ClipboardService at
// root satisfies this interface, mahpastesd passes nil and handlers return
// 501 Not Implemented when the field is nil.
type ClipboardCopier interface {
    CopyFileToClipboard(id int64) error
    BulkCopyFilesToClipboard(ids []int64) error
    CopyClipContents(id int64) error
}

// Setters wired by Bootstrap and the desktop main.
// SetClipboardService takes the interface so the field type doesn't reference
// the root *ClipboardService once App moves to internal/app/ in Task 4.
func (a *App) SetClipboardService(s ClipboardCopier)     { a.clipboardService = s }
func (a *App) SetTransferHandler(h *TransferFileHandler) { a.transferHandler = h }

// Accessors used by root-level services and the future cmd/mahpastesd entry.
// These exist now (returning current fields) so that when App moves to
// internal/app/ in Task 4, the services at root can switch from a.field
// to a.Field() with a single sed pass.
func (a *App) DB() *sql.DB                       { return a.db }
func (a *App) PluginManager() *plugin.Manager    { return a.pluginManager }
func (a *App) ShareManager() *ShareManager       { return a.shareManager }
func (a *App) ServeManager() *ServeManager       { return a.serveManager }
func (a *App) WatcherManager() *WatcherManager   { return a.watcherManager }
func (a *App) APIManager() *APIManager           { return a.apiManager }
func (a *App) TempStore() *TempClipStore         { return a.tempStore }
func (a *App) TempDir() string                   { return a.tempDir }

// Existing unexported helper, promoted because clipboard_service and
// transfer_service call it.
func (a *App) PrepareClipTransferItem(id int64, source string) (*PreparedTransferItem, error) {
    return a.prepareClipTransferItem(id, source)
}

// InitTempStore replaces initTempDir + the NewTempClipStore wiring from startup().
func (a *App) InitTempStore(dataDir string) error {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.tempDir = filepath.Join(dataDir, "clip_temp_files")
    if err := os.MkdirAll(a.tempDir, 0755); err != nil {
        return fmt.Errorf("create temp dir %q: %w", a.tempDir, err)
    }
    a.tempStore = NewTempClipStore(a.db, a.tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
    return nil
}
```

`initTempDir()` is removed (its only caller was `startup`). The existing
`app.clipboardService = clipboardService` and `app.transferHandler = transferHandler`
assignments in `main.go:42,46` become setter calls — required because after
Task 4 they will be cross-package assignments to an unexported field.

Also add an exported constructor for `TransferFileHandler` (currently built
with a struct literal `&TransferFileHandler{app: app}` in `main.go:46`, which
won't compile cross-package after the move):

```go
// transfer_handler.go — addition
func NewTransferFileHandler(a *App) *TransferFileHandler {
    return &TransferFileHandler{app: a}
}
```

### Step 2: Extract `BootstrapOptions` + `Bootstrap()`

```go
type BootstrapOptions struct {
    DB                 *sql.DB
    DataDir            string
    Bridge             *wailsbridge.Bridge // type narrows in Task 4 to bridgeiface.Bridge
    InitClipboard      bool                // false for headless servers
    PermissionCallback func(pluginName, permType, requestedPath string) string
}

// Bootstrap replaces the body of startup(). Required: DB and PermissionCallback.
func (a *App) Bootstrap(ctx context.Context, opts BootstrapOptions) error {
    if opts.DB == nil || opts.PermissionCallback == nil {
        return fmt.Errorf("Bootstrap: DB and PermissionCallback are required")
    }
    a.ctx = ctx
    a.bridge = opts.Bridge
    a.db = opts.DB

    StartCleanupJob(a.db) // renamed in Step 4

    if err := a.InitTempStore(opts.DataDir); err != nil {
        log.Printf("warning: temp store: %v", err)
    } else if err := a.tempStore.Prune(true); err != nil {
        log.Printf("warning: temp prune on startup: %v", err)
    }

    if opts.InitClipboard {
        if err := clipboard.Init(); err != nil {
            log.Printf("warning: clipboard init: %v", err)
        }
    }

    if wm, err := NewWatcherManager(a); err != nil {
        log.Printf("warning: watcher manager: %v", err)
    } else {
        a.watcherManager = wm
        if err := wm.Start(); err != nil {
            log.Printf("warning: watcher start: %v", err)
        }
    }

    a.serveManager = NewServeManager(a)

    if sm, err := NewShareManager(ctx, a.db, opts.DataDir); err != nil {
        log.Printf("warning: share manager: %v", err)
    } else {
        a.shareManager = sm
        sm.SetEventFn(a.bridge.Emit)
        if err := sm.ResumeAll(); err != nil {
            log.Printf("warning: ShareManager.ResumeAll: %v", err)
        }
        sm.StartSweepers() // renamed in Step 4
    }

    a.apiManager = NewAPIManager(a)

    pluginsDir := filepath.Join(opts.DataDir, "plugins")
    if pm, err := plugin.NewManager(ctx, a.bridge, a.db, pluginsDir); err != nil {
        log.Printf("warning: plugin manager: %v", err)
    } else {
        a.pluginManager = pm
        pm.SetMetadataFuncs(a.GetClipMetadata, a.updateClipMetadata)
        pm.SetTagCreateFunc(func(name string) (*plugin.TagCreateResult, error) {
            tag, err := a.CreateTag(name)
            if err != nil {
                return nil, err
            }
            return &plugin.TagCreateResult{ID: tag.ID, Name: tag.Name, Color: tag.Color}, nil
        })
        pm.SetPermissionCallback(opts.PermissionCallback)
        if err := pm.LoadPlugins(); err != nil {
            log.Printf("warning: load plugins: %v", err)
        }
        pm.EmitEvent("app:startup", nil)
        uc := plugin.NewUpdateChecker(a.ctx, a.bridge, a.db, pm)
        pm.SetUpdateChecker(uc)
        if interval := a.getUpdateCheckInterval(); interval != "disabled" {
            uc.Start(parseUpdateInterval(interval))
        }
    }
    return nil
}
```

### Step 3: Reduce `startup()` to a Bootstrap caller

```go
func (a *App) startup(ctx context.Context) {
    bridge := wailsbridge.New(ctx)
    dataDir, err := getDataDir()
    if err != nil {
        log.Fatalf("data dir: %v", err)
    }
    db, err := initDB()
    if err != nil {
        log.Fatalf("db: %v", err)
    }
    err = a.Bootstrap(ctx, BootstrapOptions{
        DB: db, DataDir: dataDir, Bridge: bridge, InitClipboard: true,
        PermissionCallback: func(pluginName, permType, requestedPath string) string {
            path, err := bridge.OpenDirectory(wailsbridge.FileDialogOptions{
                Title:                fmt.Sprintf("Plugin %q requests %s access", pluginName, permType),
                DefaultDirectory:     filepath.Dir(requestedPath),
                CanCreateDirectories: permType == "fs_write",
            })
            if err != nil || path == "" {
                return ""
            }
            return path
        },
    })
    if err != nil {
        log.Fatalf("bootstrap: %v", err)
    }
}
```

### Step 4: Export `startCleanupJob` and `startSweepers`

| Old | New | Existing call sites |
|-----|-----|---------------------|
| `startCleanupJob` (`database.go:439`) | `StartCleanupJob` | `app.go:150` (now in `Bootstrap`) |
| `startSweepers` (method on `*ShareManager`, `share_manager.go:1561`) | `StartSweepers` | `app.go:195` (now in `Bootstrap`), `backup.go:794` |

Verify with `grep -rn "startCleanupJob\|startSweepers" .` → no hits to the
lowercase forms after refactor.

### Step 5: Update `main.go` to use the new setters

```go
// main.go — change these two lines
app.clipboardService = clipboardService  // OLD
app.transferHandler  = transferHandler    // OLD
// →
app.SetClipboardService(clipboardService)
app.SetTransferHandler(transferHandler)

// and:
transferHandler := &TransferFileHandler{app: app}  // OLD (unexported field)
// →
transferHandler := NewTransferFileHandler(app)
```

### Step 6: Verification

```bash
go build ./...
GOOS=linux  GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
go test ./...
cd e2e && set -o pipefail && npm test 2>&1 | tee /tmp/e2e.log | tail -100
```

Expected: all builds pass on all platforms, all tests pass. Desktop app boots
and behaves identically — `Bootstrap` is a pure refactor and the accessors are
unused so far.

### Step 7: Commit

```bash
git add app.go database.go share_manager.go backup.go main.go transfer_handler.go
git commit -m "refactor: extract App.Bootstrap, add accessors, export StartCleanupJob/StartSweepers"
```

---

## Task 2: Add `bridgeiface.Bridge` interface (plugin/ only)

**Purpose:** Decouple `plugin/` from the concrete `*wailsbridge.Bridge` so the
plugin manager can accept either the Wails bridge (desktop) or the SSE broker
(server). App's `bridge` field is NOT narrowed yet — that happens in Task 4
once dialog calls migrate to the desktop wrapper.

**Files:**
- Create: `internal/bridgeiface/bridge.go`
- Modify: `plugin/manager.go`, `plugin/api_task.go`, `plugin/api_toast.go`, `plugin/api_modal.go`, `plugin/update_checker.go`

### Step 1: Create the interface

```go
// internal/bridgeiface/bridge.go
package bridgeiface

type Bridge interface {
    Emit(name string, data ...any)
    On(name string, cb func(data ...any))
}

type NoOp struct{}

func (NoOp) Emit(name string, data ...any)        {}
func (NoOp) On(name string, cb func(data ...any)) {}
```

`*wailsbridge.Bridge` already has both methods with the matching signatures
(`internal/wailsbridge/bridge.go:53,69`), so it satisfies the interface
without modification.

### Step 2: Update `plugin/` files

Replace `*wailsbridge.Bridge` with `bridgeiface.Bridge` at each of these sites
(field type + constructor parameter):

| File | Lines |
|------|-------|
| `plugin/manager.go` | `122` (field), `140` (NewManager) |
| `plugin/api_task.go` | `20`, `39` |
| `plugin/api_toast.go` | `20`, `28` |
| `plugin/api_modal.go` | `62` (newModalGuard), `81` (field), `88` (NewModalAPI) |
| `plugin/update_checker.go` | `50`, `59` |

Update imports in each from `internal/wailsbridge` to `internal/bridgeiface`.
None of these call dialog methods (only `Emit` / `On`), so the narrower
interface is sufficient.

**Do not** change `app.go`'s bridge field type here. Dialog calls at
`app.go:228, 2452, 2509, 2588, 2612, 2636, 2979` and `plugin_service.go:115`
still require `*wailsbridge.Bridge`. Those callers migrate in Task 4.

### Step 3: Modal guard server-mode timeout

`plugin/api_modal.go:62` `newModalGuard` subscribes via `bridge.On` to
`plugin:modal:acked` and `plugin:modal:closed`. With `NoOp.On` (or an SSE-only
`On`), the subscription is dropped and the guard would deadlock all
subsequent modal calls.

Add a timeout-based release. Wherever the guard waits on the ack channel,
add a parallel `time.After` case:

```go
const modalGuardServerTimeout = 5 * time.Second // tunable

select {
case <-mg.ackCh:
    // normal: frontend acked
case <-time.After(modalGuardServerTimeout):
    log.Printf("plugin: modal guard timed out (likely headless — no frontend ack)")
    mg.releaseLocked()
}
```

Desktop semantics are unchanged (frontend acks within ms); server modal calls
no longer deadlock.

### Step 4: Verification

```bash
go build ./...
GOOS=linux  GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
go test ./...
```

App still depends on wailsbridge directly — that's expected until Task 4.
This task only decouples plugin/.

### Step 5: Commit

```bash
git add internal/bridgeiface/ plugin/
git commit -m "feat: introduce bridgeiface.Bridge, plugin/ uses it; modal guard timeout for headless"
```

---

## Task 3: Create `frontend/` Go package (embed + SSE broker)

**Files:**
- Create: `frontend/embed.go`, `frontend/sse_broker.go`

### Step 1: Asset embed

```go
// frontend/embed.go
package webui

import "embed"

//go:embed all:js all:css all:dist all:vendor all:wailsjs all:*.html
var Assets embed.FS
```

**Critical:** `all:wailsjs` is required. The desktop app loads
`/wailsjs/runtime/runtime.js` and the generated bindings from this embed;
omitting it breaks the Wails runtime. The list-form (not `all:*`) explicitly
excludes `node_modules/`, `src/`, `tailwind.config.js`, and `package*.json`.

`login.html` (added in Task 10) lives at `frontend/login.html` and is matched
by `all:*.html`.

### Step 2: SSE broker

```go
// frontend/sse_broker.go
package webui

import (
    "encoding/json"
    "fmt"
    "net/http"
    "sync"
    "time"
)

type SSEBroker struct {
    mu       sync.RWMutex
    channels map[chan string]struct{}
}

func NewSSEBroker() *SSEBroker {
    return &SSEBroker{channels: make(map[chan string]struct{})}
}

func (b *SSEBroker) Emit(name string, data ...any) {
    var payload []byte
    if len(data) == 1 {
        payload, _ = json.Marshal(data[0])
    } else {
        payload, _ = json.Marshal(data)
    }
    frame := fmt.Sprintf("event: %s\ndata: %s\n\n", name, payload)
    b.mu.RLock()
    defer b.mu.RUnlock()
    for ch := range b.channels {
        select {
        case ch <- frame:
        default:
            // Slow consumer — drop. UI may call /api/v1/status after reconnect
            // to resync state if continuity matters for a given event class.
        }
    }
}

// On is a no-op: the browser cannot push events upstream via SSE.
// Headless plugin modals time out via Task 2 Step 3.
func (b *SSEBroker) On(name string, cb func(data ...any)) {}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    ch := make(chan string, 64)
    b.mu.Lock()
    b.channels[ch] = struct{}{}
    b.mu.Unlock()
    defer func() {
        b.mu.Lock()
        delete(b.channels, ch)
        b.mu.Unlock()
    }()

    // Reverse proxies (nginx, Caddy) close idle connections after ~60s.
    ping := time.NewTicker(30 * time.Second)
    defer ping.Stop()

    for {
        select {
        case frame := <-ch:
            fmt.Fprint(w, frame)
            flusher.Flush()
        case <-ping.C:
            fmt.Fprint(w, ": ping\n\n")
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}
```

`*SSEBroker` satisfies `bridgeiface.Bridge`.

### Step 3: Verification

```bash
go build ./frontend/
```

No Wails dependency.

### Step 4: Commit

```bash
git add frontend/embed.go frontend/sse_broker.go
git commit -m "feat: add webui package (embedded assets + SSE broker)"
```

---

## Task 4: Move shared core to `internal/app/`

Tasks 1–3 made this safe: Bootstrap is decoupled, `plugin/` is bridge-agnostic,
the frontend embed is its own package.

**Layout intent (Option A):** desktop entry stays at the repo root; the
shared core moves to `internal/app/`. The `App` type in the new
`desktop_app.go` is in `package main` at the root, embedding `*app.App` and
adding dialog methods. Wails generates bindings at `window.go.main.App`
because the type is named `App` — preserving the existing frontend surface.

### Step 1: Create directories

```bash
mkdir -p internal/app internal/bridgeiface cmd/mahpastesd
```

### Step 2: Move core files (use `git mv`)

```bash
git mv app.go database.go internal/app/
git mv api_manager.go internal/app/
git mv backup.go maintenance.go plugins.go internal/app/
git mv share_assembler.go share_codec.go share_identity.go share_logs.go \
       share_manager.go share_protocol.go share_ring.go share_types.go \
       internal/app/
git mv serve_file_upload.go serve_json_api.go serve_manager.go internal/app/
git mv tag_hierarchy.go temp_clip_store.go internal/app/
git mv transfer_handler.go transfer_types.go app_transfer_helpers.go \
       internal/app/
git mv watcher.go internal/app/
git mv instance_lock.go instance_lock_unix.go instance_lock_windows.go \
       internal/app/
```

`transfer_types.go` is included — `PreparedTransferItem` is used by
`app_transfer_helpers.go` and exposed via the `PrepareClipTransferItem`
accessor on App, so its type must be reachable from root-level services.
Root `transfer_service.go` will need its unqualified references to these
types rewritten as `app.X` (Step 7).

`api_service.go`, `share_service.go`, `serve_service.go` do **not** move.
Wails generates JS bindings under `window.go.<package>.<TypeName>`; moving
them to `package app` would emit `window.go.app.ShareService` and the
existing frontend (and generated `frontend/wailsjs/go/main/ShareService.js`)
calls `window.go.main.ShareService`. Keep them at root in `package main`
and have them use the App accessors (`core.ShareManager()`,
`core.ServeManager()`, `core.APIManager()`) added in Task 1.

### Step 3: Move test files

```bash
git mv api_manager_test.go backup_test.go database_test.go maintenance_test.go \
       internal/app/
git mv serve_manager_test.go serve_file_upload_test.go internal/app/
git mv share_assembler_test.go share_codec_test.go share_identity_test.go \
       share_manager_test.go share_protocol_test.go share_ring_test.go \
       internal/app/
git mv tag_hierarchy_test.go tag_merge_test.go tag_reference_integrity_test.go \
       temp_clip_store_test.go transfer_handler_test.go server_security_test.go \
       internal/app/
git mv test_helpers_test.go instance_lock_test.go internal/app/
```

`transfer_service_test.go` stays at root because it tests
`TransferService` which stays at root (uses platform native drag).

### Step 4: Files that stay at the root

These keep `package main` and remain Wails-bound from `main.go`:

- `main.go` — desktop entry
- `desktop_app.go` — created in Step 6
- `plugin_service.go` — uses `bridge.OpenFile` dialog
- `share_service.go` — preserves `window.go.main.ShareService` binding
- `serve_service.go` — preserves `window.go.main.ServeService` binding
- `api_service.go` — preserves `window.go.main.APIService` binding
- `clipboard_service.go` + `clipboard_darwin.go` + `clipboard_windows.go` + `clipboard_other.go` — platform clipboard code (CGo on darwin)
- `transfer_service.go` + `native_drag_darwin.go` + `native_drag_windows.go` + `native_drag_other.go` — platform native drag
- `open_darwin.go` + `open_windows.go` + `open_other.go` — platform file-open helpers used by desktop dialog methods

`share_service.go`, `serve_service.go`, `api_service.go` lose their direct
access to `a.shareManager` / `a.serveManager` / `a.apiManager` once App is in
`internal/app/`. Update them mechanically: `s.app.shareManager` → `s.app.ShareManager()`,
and likewise for the other two. The methods on the moved-out types
(`*app.ShareManager`, `*app.ServeManager`, `*app.APIManager`) are
unchanged; only the field access path changes.

The return types of `share_service.go` (`ShareStatus`, `ShareInfo`,
`FollowInfo`) are defined in `share_types.go` which moves to `internal/app/`.
After the move the types become `app.ShareStatus` etc. Update the function
signatures in `share_service.go` accordingly.

### Step 5: Change package declarations in moved files

```bash
sed -i.bak 's/^package main$/package app/' internal/app/*.go
rm internal/app/*.go.bak
git diff internal/app/ | head -40  # sanity check
```

### Step 6: Create `desktop_app.go` at the root

```go
// desktop_app.go
package main

import (
    "fmt"
    "path/filepath"

    app "go-clipboard/internal/app"
    "go-clipboard/internal/wailsbridge"
)

// App embeds the core app.App and adds Wails-bound desktop-only methods.
// Wails generates bindings at window.go.main.App from the type name, so
// keeping this type named `App` preserves every existing frontend call site.
type App struct {
    *app.App
    bridge *wailsbridge.Bridge
}

func NewApp() *App { return &App{App: app.NewApp()} }

// SetBridge is called from the Wails OnStartup hook so the wrapper holds the
// bridge for its dialog methods.
func (d *App) SetBridge(b *wailsbridge.Bridge) {
    d.bridge = b
    d.App.SetBridge(b) // bridgeiface — *wailsbridge.Bridge satisfies it
}
```

Then migrate the dialog-using methods from the old `app.go` into this file.
Identify them with:

```bash
grep -n 'a\.bridge\.\(Open\|Save\|Choose\)\|chooseApplicationDialog' internal/app/app.go
```

Expected hits (from prior grep): `228, 2452, 2509, 2588, 2612, 2636, 2979`.
For each, cut the enclosing method out of `internal/app/app.go`, paste it
into `desktop_app.go`, change the receiver to `*App` (the wrapper), and
rewrite `a.bridge.X` as `d.bridge.X` and `a.<other field>` as
`d.App.<other accessor>()`. Run the grep and only move methods that actually
contain a dialog call — the candidates include:

- `ShowCreateBackupDialog` — calls `SaveFile`
- `ShowRestoreBackupDialog` — calls `OpenFile`
- `OpenClipWithDefaultApp`, `OpenClipWithApp`, `ChooseApplication`
- `SelectFolder`, `IsDirectory`
- `SaveClipToFile`
- `CopyToClipboard`

The plugin permission `OpenDirectory` callback at line 228 stays as a
closure inside `startup()`/the new OnStartup lambda (Step 8), not as a method.

**Keep in `internal/app/`** — these don't use dialogs:

- `ConfirmRestoreBackup` (`app.go:2660`) is a thin wrapper around `RestoreBackup`
  with no dialog. The REST restore handler at `api_manager.go:2728` calls it,
  so it must remain reachable from `internal/app/`.
- `UpdateClipData` (`app.go:882`) takes the new content as parameters; no
  dialog. The editor (`editor.js:229`) and duplicate-overwrite upload flow
  (`app.js:955, 993`; `wails-api.js:119`) both call it. Stays as a method on
  `*app.App` and gets a REST route in Task 6 + a glue entry in Task 11.

Any local helper like `chooseApplicationDialog(bridge)` (currently at root)
that's called only by dialog methods moves with them.

After the move, `internal/app/app.go` has no `wailsbridge` import and no
dialog calls. The `bridge` field there can be narrowed:

```go
// internal/app/app.go
import "go-clipboard/internal/bridgeiface"

type App struct {
    // ...
    bridge bridgeiface.Bridge
    // ...
}

func NewApp() *App { return &App{bridge: bridgeiface.NoOp{}} }

func (a *App) SetBridge(b bridgeiface.Bridge) { a.bridge = b }
```

The `NoOp` default guarantees `a.bridge.Emit(...)` calls at lines like 86,
94, 790, 864 never panic if Bootstrap hasn't run.

Also narrow `BootstrapOptions.Bridge` to `bridgeiface.Bridge`. Desktop passes
`*wailsbridge.Bridge` (which satisfies it); server passes `*SSEBroker`.

### Step 7: Update root-level services to use accessors

Now that App is in `internal/app/`, root services can't reach unexported
fields. Apply mechanical substitutions across `plugin_service.go`,
`clipboard_service.go`, `transfer_service.go`, `share_service.go`,
`serve_service.go`, `api_service.go`:

| Old | New |
|-----|-----|
| `s.app.db` | `s.app.DB()` |
| `s.app.pluginManager` | `s.app.PluginManager()` |
| `s.app.shareManager` | `s.app.ShareManager()` |
| `s.app.serveManager` | `s.app.ServeManager()` |
| `s.app.apiManager` | `s.app.APIManager()` |
| `s.app.tempStore` | `s.app.TempStore()` |
| `s.app.tempDir` | `s.app.TempDir()` |
| `s.app.prepareClipTransferItem(id, src)` | `s.app.PrepareClipTransferItem(id, src)` |

**`transfer_service.go` type qualifications.** The file currently uses
`TransferCapabilities`, `PrepareTransferRequest`, `PreparedTransferItem`,
and `StartNativeDragRequest` unqualified (`transfer_service.go:24,66,78,90`).
After `transfer_types.go` moves, these become `app.TransferCapabilities`
etc. Rewrite the file and its tests (`transfer_service_test.go:63, 117, 134,
147, 154, 209, 217, 222`). The cross-platform build will not pass until
this is done.

**`plugin_service.go` bridge wiring.** `plugin_service.go:115` calls
`s.app.bridge.OpenFile`, but `App.bridge` is now `bridgeiface.Bridge` (no
dialogs) and the field is unexported anyway. Hold the bridge on
PluginService itself, with a `SetBridge` setter so the type can be
constructed (and Wails-bound) before the bridge exists:

```go
// plugin_service.go
type PluginService struct {
    app    *app.App
    bridge *wailsbridge.Bridge // nil until OnStartup
}

func NewPluginService(a *app.App) *PluginService { return &PluginService{app: a} }

func (s *PluginService) SetBridge(b *wailsbridge.Bridge) { s.bridge = b }

// Methods that don't use dialogs use App accessors:
func (s *PluginService) EnablePlugin(id int64) error {
    pm := s.app.PluginManager()
    if pm == nil {
        return errors.New("plugin manager not initialized")
    }
    return pm.EnablePlugin(id)
}

// Dialog method guards on bridge readiness:
func (s *PluginService) ImportPluginFromPath() (*app.Plugin, error) {
    if s.bridge == nil {
        return nil, errors.New("bridge not initialized yet")
    }
    path, err := s.bridge.OpenFile(wailsbridge.FileDialogOptions{ /* ... */ })
    // ...
    return s.app.PluginManager().ImportPlugin(path)
}
```

This shape — construct early, wire bridge late — solves the Wails `Bind`
ordering problem in Step 8 (Wails reflects `Bind` slots eagerly and rejects
nil-typed pointers, so we can't defer construction).

`TransferFileHandler{app: app}` literals must become
`app.NewTransferFileHandler(core)` (constructor added in Task 1).

### Step 7b: clipboard handlers gate on a nil interface

`internal/app/api_manager.go` has six call sites that dereference
`am.app.clipboardService` (`api_manager.go:1857, 1862, 2754, 2759, 2783,
2788`). After Task 1 the field is `ClipboardCopier` interface — desktop
sets it via `core.SetClipboardService(clipboardService)`, mahpastesd never
sets it. Each handler must guard:

```go
if am.app.clipboardService == nil {
    am.jsonError(w, http.StatusNotImplemented, "clipboard not available on this server")
    return
}
```

If you'd rather add an accessor and gate at registration time, you can
conditionally skip registering `/api/v1/clipboard/*` routes in
`registerRoutes()` when the field is nil — but that's brittle because the
field is set after `NewAPIManager` runs. The per-handler guard is simpler.

### Step 8: Rewrite root `main.go` to use `internal/app/`

```go
// main.go (at repo root, unchanged location — wails.json sits next to it)
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"

    app "go-clipboard/internal/app"
    "go-clipboard/internal/wailsbridge"
    webui "go-clipboard/frontend"

    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
    "github.com/wailsapp/wails/v2/pkg/options/assetserver"
    "github.com/wailsapp/wails/v2/pkg/options/mac"
)

func main() {
    log.SetFlags(log.LstdFlags | log.Lshortfile)

    dataDir, err := app.GetDataDir()
    if err != nil {
        log.Fatalf("data dir: %v", err)
    }
    instanceLock, err := app.AcquireInstanceLock(dataDir)
    if err != nil {
        log.Fatalf("%v", err)
    }
    defer instanceLock.Release()

    desktopApp := NewApp() // wrapper at root; embeds *app.App
    core := desktopApp.App

    // Construct all services eagerly. Wails reflects Bind slots immediately
    // and rejects nil typed pointers. Services that need the bridge expose
    // SetBridge and we call it in OnStartup once wailsbridge.New(ctx) is
    // available.
    pluginService := NewPluginService(core)        // SetBridge later
    clipboardService := NewClipboardService(core)
    transferService := NewTransferService(core)
    shareService := NewShareService(core)          // stays at root
    serveService := NewServeService(core)          // stays at root
    apiService := NewAPIService(core)              // stays at root

    core.SetClipboardService(clipboardService) // satisfies ClipboardCopier
    transferHandler := app.NewTransferFileHandler(core)
    core.SetTransferHandler(transferHandler)

    err = wails.Run(&options.App{
        Title: "mahpastes", Width: 1280, Height: 800, MinWidth: 800, MinHeight: 600,
        StartHidden: os.Getenv("MAHPASTES_START_HIDDEN") == "1",
        AssetServer: &assetserver.Options{
            Assets:  webui.Assets, // embed.FS satisfies fs.FS — Wails v2 expects that, not http.FS(...)
            Handler: transferHandler,
        },
        BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
        OnStartup: func(ctx context.Context) {
            bridge := wailsbridge.New(ctx)
            desktopApp.SetBridge(bridge)
            pluginService.SetBridge(bridge)
            db, dbErr := app.InitDB()
            if dbErr != nil {
                log.Fatalf("db: %v", dbErr)
            }
            err := core.Bootstrap(ctx, app.BootstrapOptions{
                DB: db, DataDir: dataDir, Bridge: bridge, InitClipboard: true,
                PermissionCallback: func(name, kind, p string) string {
                    path, err := bridge.OpenDirectory(wailsbridge.FileDialogOptions{
                        Title:                fmt.Sprintf("Plugin %q requests %s access", name, kind),
                        DefaultDirectory:     filepath.Dir(p),
                        CanCreateDirectories: kind == "fs_write",
                    })
                    if err != nil || path == "" {
                        return ""
                    }
                    return path
                },
            })
            if err != nil {
                log.Fatalf("bootstrap: %v", err)
            }
        },
        OnShutdown: func(ctx context.Context) { core.Shutdown(ctx) },
        DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: false},
        Bind: []interface{}{
            desktopApp,        // window.go.main.App
            pluginService,     // window.go.main.PluginService (bridge wired via SetBridge in OnStartup)
            clipboardService,  // window.go.main.ClipboardService
            transferService,   // window.go.main.TransferService
            serveService,      // window.go.main.ServeService
            apiService,        // window.go.main.APIService
            shareService,      // window.go.main.ShareService
        },
        Mac: &mac.Options{ /* unchanged */ },
    })
    if err != nil {
        log.Fatalf("wails: %v", err)
    }
}
```

**Bind-time ordering (confirmed in Wails v2.12.0).** `wails.Run` calls
`binding.NewBindings(...)` before the frontend/startup flow. `NewBindings`
iterates `appoptions.Bind` immediately; any entry that fails `Add` triggers
`logger.Fatal`. The reflect check requires a non-nil pointer whose
`Elem().Kind() == Struct` — a nil typed pointer fails with "not a pointer
to a struct" and kills the process before `OnStartup` runs. So every
service in `Bind` must exist before `wails.Run`. The eager-construct +
`SetBridge` pattern above is the correct shape, not a defensive workaround.
Dialog methods on a service whose bridge is still nil return an error,
which is safe: Wails won't dispatch frontend method calls before
`OnStartup` returns.

### Step 9: Export the remaining cross-package symbols on App

| Current | Exported | Why |
|---------|----------|-----|
| `App.shutdown` | `App.Shutdown` | Called from main.go OnShutdown and cmd/mahpastesd on signal |
| `getDataDir` | `GetDataDir` | Both binaries call it |
| `initDB` | `InitDB` | Both binaries call it |
| `updateClipMetadata` | `UpdateClipMetadata` | Plugin metadata callback |
| `StartCleanupJob`, `StartSweepers` | (already done in Task 1) | |
| Other unexported helpers referenced from root services | Exported as discovered | Surface via `go build ./...` errors |

### Step 10: Update `internal/app/test_helpers_test.go`

Most tests in `internal/app/` are `package app` and continue to see all
symbols — only the package declaration changed (Step 5). Any test that
previously imported `package main` types needs updating; the audit step is a
`go test ./internal/app/...` and follow the error messages.

### Step 11: Cross-platform build verification

```bash
go build ./...
GOOS=linux  GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=darwin  GOARCH=arm64 go build ./...
go test ./...
```

Cross-builds catch `*_other.go` / `*_unix.go` / `*_windows.go` drift that
darwin-only builds miss. `instance_lock_*.go` is now in `internal/app/`;
`clipboard_*.go`, `native_drag_*.go`, `open_*.go` are at root. The build
must succeed on each platform.

### Step 12: Wails build/dev/generate still work

`wails.json` and `main.go` did not move. Run each Wails command to confirm:

```bash
~/go/bin/wails doctor                                 # sanity check
~/go/bin/wails generate module                        # bindings — should regenerate frontend/wailsjs/go/main/
make dev                                              # smoke
make build                                            # production build of the desktop app
```

If `wails generate module` produces bindings that look different (e.g., loses
methods that moved from `*App` to `*app.App` but are promoted via embedding),
the frontend will start hitting "method not found" errors. Sanity check by
diffing `frontend/wailsjs/go/main/App.js` before and after — the method list
should still contain everything the frontend calls.

### Step 13: e2e

```bash
cd e2e && set -o pipefail && npm test 2>&1 | tee /tmp/e2e.log | tail -100
```

### Step 14: Commit

```bash
git status                          # verify no stray *.go files at unexpected locations
git add internal/ desktop_app.go main.go plugin_service.go \
        clipboard_service.go transfer_service.go frontend/wailsjs/
git commit -m "refactor: move shared core to internal/app/, root services use accessors"
```

---

## Task 5: APIManager refactor — owned mux, MountWebUI, auth-aware SPA handler

**Files:** `internal/app/api_manager.go`

### Step 1: Add `mux` field

```go
type APIManager struct {
    app          *App
    mux          *http.ServeMux    // NEW
    server       *http.Server
    signingKey   []byte            // NEW (Task 7)
    loginLimiter *loginRateLimiter // NEW (Task 7)
    mu           sync.RWMutex
    running      bool
    port         int
    bindAll      bool
    requestCount int64
}
```

### Step 2: Move route registration to a constructor helper

All `mux.HandleFunc(...)` calls currently in `Start()` (`api_manager.go:104-194`)
move into `registerRoutes()`. `Start()` then only wraps the mux with CORS and
listens.

```go
func NewAPIManager(app *App) *APIManager {
    am := &APIManager{app: app, mux: http.NewServeMux()}
    am.registerRoutes()
    return am
}
```

### Step 3: `MountWebUI`

```go
import "io/fs"

// MountWebUI adds SPA + SSE routes. Server-only — desktop uses Wails'
// AssetServer to serve assets, not this mux.
// Must be called before Start().
func (am *APIManager) MountWebUI(assets fs.FS, sseHandler http.Handler) {
    am.mux.Handle("GET /api/v1/events", am.authMiddleware(http.HandlerFunc(sseHandler.ServeHTTP)))
    am.mux.Handle("/", newSPAHandler(assets, am))
}
```

### Step 4: SPA handler — static assets are public, only the shell requires auth

The earlier draft required auth for every non-`/login.html` path, which broke
the login page styling (the page links to `/dist/output.css` but that
redirected to login). Correct behavior: static assets that actually exist in
the embed are always public (they're not secrets — anyone who downloads the
page can grab them via the SPA itself). Only the SPA shell (index.html, and
unknown deep-link paths) requires a valid session.

```go
import (
    "bytes"
    "io/fs"
    "net/http"
    "path"
    "strings"
    "time"
)

type spaHandler struct {
    assets   fs.FS
    api      *APIManager
    indexBuf []byte
    modTime  time.Time
}

func newSPAHandler(assets fs.FS, api *APIManager) *spaHandler {
    h := &spaHandler{assets: assets, api: api, modTime: time.Now()}
    if data, err := fs.ReadFile(assets, "index.html"); err == nil {
        h.indexBuf = data
    }
    return h
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

    // Root: serve the SPA shell, redirecting unauthenticated requests.
    if name == "" || name == "." {
        if !h.api.hasValidSession(r) {
            http.Redirect(w, r, "/login.html", http.StatusFound)
            return
        }
        h.writeIndex(w, r)
        return
    }

    // Anything that actually exists in the embed is public. This includes
    // /login.html, /dist/output.css, /js/rest-glue.js, fonts, etc. — none
    // of these are secret, and gating them behind auth breaks the login
    // page (it can't load its own CSS).
    if _, err := fs.Stat(h.assets, name); err == nil {
        h.serveFile(w, r, name)
        return
    }

    // Unknown path — SPA deep link. Auth required.
    if !h.api.hasValidSession(r) {
        http.Redirect(w, r, "/login.html", http.StatusFound)
        return
    }
    h.writeIndex(w, r)
}

func (h *spaHandler) writeIndex(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    http.ServeContent(w, r, "index.html", h.modTime, bytes.NewReader(h.indexBuf))
}

func (h *spaHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
    data, err := fs.ReadFile(h.assets, name)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    http.ServeContent(w, r, name, h.modTime, bytes.NewReader(data))
}
```

`fs.ReadFile` + `bytes.NewReader` gives a real `io.ReadSeeker` (embed.FS
doesn't guarantee `Seek` on the raw `fs.File`). `path.Clean` neutralises `..`
segments.

### Step 5: Verification + commit

```bash
go build ./... && go test ./internal/app/...
git add internal/app/api_manager.go
git commit -m "refactor(api): owned mux, registerRoutes split, SPA handler with public static assets"
```

---

## Task 6: Add new REST API routes (and extend `handleListClips`)

**Files:** `internal/app/api_manager.go`

### Step 0: Extend `handleListClips` to match `App.GetClips`

The current `handleListClips` (`api_manager.go:538`, called from the route at
line 105) parses only `archived`, a single numeric `tag`, `content_type`, and
`search`. The Wails-side `App.GetClips(archived, tagIDs, hiddenTagIDs,
sortField, sortDir)` accepts multiple tag IDs, hidden tag IDs, and explicit
sort. The REST glue layer (Task 11) passes the full Wails signature, so the
REST handler must support it — otherwise server-mode filtering and sort are
silently wrong.

Extend the handler to accept:

| Query param | Type | Maps to |
|-------------|------|---------|
| `archived` | bool | existing |
| `tag` | repeated, integer | `tagIDs` — collect via `r.URL.Query()["tag"]` and parse each |
| `hidden` | repeated, integer | `hiddenTagIDs` |
| `sort` | string | `sortField` (whitelist: `created_at`, `filename`, `size`) |
| `dir` | string | `sortDir` (whitelist: `asc`, `desc`) |
| `content_type` | string | existing |
| `search` | string | existing |
| `untagged` | bool | folder-mode untagged-only branch (calls `GetUntaggedClips`) |
| `folder_tag` | int | folder-mode single tag (calls `GetFolderClips`) |

Validate the sort whitelist server-side — passing untrusted strings into SQL
ORDER BY is an injection vector.

Add to `registerRoutes()`:

```go
// API keys (admin)
am.mux.HandleFunc("POST /api/v1/keys",        am.authMiddleware(am.requireRole("admin", am.handleCreateKey)))
am.mux.HandleFunc("GET /api/v1/keys",         am.authMiddleware(am.requireRole("admin", am.handleListKeys)))
am.mux.HandleFunc("DELETE /api/v1/keys/{id}", am.authMiddleware(am.requireRole("admin", am.handleRevokeKey)))

// Share — publish
am.mux.HandleFunc("GET /api/v1/share",                              am.authMiddleware(am.requireRole("viewer", am.handleGetShareStatus)))
am.mux.HandleFunc("POST /api/v1/share/publish",                     am.authMiddleware(am.requireRole("admin", am.handleStartShare)))
am.mux.HandleFunc("DELETE /api/v1/share/publish/{tagId}",           am.authMiddleware(am.requireRole("admin", am.handleStopShare)))
am.mux.HandleFunc("PUT /api/v1/share/publish/{tagId}/pause",        am.authMiddleware(am.requireRole("admin", am.handlePauseShare)))
am.mux.HandleFunc("DELETE /api/v1/share/publish/{tagId}/pause",     am.authMiddleware(am.requireRole("admin", am.handleResumeShare)))

// Share — follow
am.mux.HandleFunc("POST /api/v1/share/follow",                      am.authMiddleware(am.requireRole("admin", am.handleFollow)))
am.mux.HandleFunc("POST /api/v1/share/test-follow",                 am.authMiddleware(am.requireRole("admin", am.handleTestFollowConnection)))
am.mux.HandleFunc("POST /api/v1/share/follow-direct",               am.authMiddleware(am.requireRole("admin", am.handleFollowWithoutDial)))
am.mux.HandleFunc("DELETE /api/v1/share/follow/{id}",               am.authMiddleware(am.requireRole("admin", am.handleUnfollow)))
am.mux.HandleFunc("POST /api/v1/share/follow/{id}/reconnect",       am.authMiddleware(am.requireRole("admin", am.handleReconnectFollow)))
am.mux.HandleFunc("PUT /api/v1/share/follow/{id}/pause",            am.authMiddleware(am.requireRole("admin", am.handlePauseFollow)))
am.mux.HandleFunc("DELETE /api/v1/share/follow/{id}/pause",         am.authMiddleware(am.requireRole("admin", am.handleResumeFollow)))
am.mux.HandleFunc("PUT /api/v1/share/follow/{id}/tag",              am.authMiddleware(am.requireRole("admin", am.handleUpdateFollowTag)))
am.mux.HandleFunc("GET /api/v1/share/logs",                         am.authMiddleware(am.requireRole("viewer", am.handleGetShareLogs)))

// Settings
am.mux.HandleFunc("GET /api/v1/settings/{key}",  am.authMiddleware(am.requireRole("viewer", am.handleGetSetting)))
am.mux.HandleFunc("PUT /api/v1/settings/{key}",  am.authMiddleware(am.requireRole("admin", am.handleSetSetting)))

// Maintenance — extended
am.mux.HandleFunc("GET /api/v1/maintenance/empty-tags",   am.authMiddleware(am.requireRole("admin", am.handleListEmptyTags)))
am.mux.HandleFunc("POST /api/v1/maintenance/empty-tags",  am.authMiddleware(am.requireRole("admin", am.handleRemoveEmptyTags)))
am.mux.HandleFunc("GET /api/v1/maintenance/database",     am.authMiddleware(am.requireRole("admin", am.handleDatabaseSize)))
am.mux.HandleFunc("POST /api/v1/maintenance/database",    am.authMiddleware(am.requireRole("admin", am.handleCompactDatabase)))

// Plugins — extended
am.mux.HandleFunc("GET /api/v1/plugins/{id}/permissions",          am.authMiddleware(am.requireRole("admin", am.handleGetPluginPermissions)))
// Filesystem paths contain slashes — cannot fit in a single ServeMux {param}.
// Body: {"type": "fs_read", "path": "/Users/x/y"}
am.mux.HandleFunc("POST /api/v1/plugins/{id}/permissions/revoke",  am.authMiddleware(am.requireRole("admin", am.handleRevokePluginPermission)))
am.mux.HandleFunc("POST /api/v1/plugins/{id}/url-check",           am.authMiddleware(am.requireRole("editor", am.handlePluginURLCheck)))
am.mux.HandleFunc("POST /api/v1/plugins/preview",                  am.authMiddleware(am.requireRole("admin", am.handlePreviewPluginFromURL)))
am.mux.HandleFunc("POST /api/v1/plugins/confirm",                  am.authMiddleware(am.requireRole("admin", am.handleConfirmPluginInstall)))

// Clip content overwrite (used by image editor and duplicate-overwrite upload).
// MUST enforceTagScope on the clip — same rule as DELETE/archive
// (see api_manager.go:894). Body is JSON+base64; cap with
// http.MaxBytesReader at ~150MB so the 100MB raw-clip ceiling survives
// base64 inflation (~133%). After decoding, sanity-check the decoded
// byte length against a 100MB ceiling to match the multipart upload
// handler (api_manager.go:800).
am.mux.HandleFunc("PUT /api/v1/clips/{id}/data",   am.authMiddleware(am.requireRole("editor", am.handleUpdateClipData)))

// Tag merge preview (non-mutating dry-run).
// MUST enforce tag scope on BOTH source (URL {id}) and destination
// (body dest_id), the same way handleMergeTag does for the mutating
// route (api_manager.go:1127). Otherwise a scoped key could enumerate
// counts/blockers for tags it can't otherwise see.
am.mux.HandleFunc("POST /api/v1/tags/{id}/merge-preview", am.authMiddleware(am.requireRole("admin", am.handlePreviewMergeTag)))

// Filename + tag lookup (batch — body holds the filenames array).
// Exact-match semantics; no fuzzy/substring. tag_id is REQUIRED.
// For scoped keys: tag_id must equal the scoped tag or be a descendant
// (use isTagInScope), and the returned matches must be filtered to
// clips actually within that tag. A scoped key must NOT be allowed to
// probe untagged clips or out-of-scope filenames/hashes.
am.mux.HandleFunc("POST /api/v1/clips/find",       am.authMiddleware(am.requireRole("viewer", am.handleFindClipsByFilenameAndTag)))

// Misc
am.mux.HandleFunc("GET /api/v1/status",            am.handleAPIStatus) // unauthenticated
am.mux.HandleFunc("GET /api/v1/serve/random-port", am.authMiddleware(am.requireRole("admin", am.handleRandomPort)))
am.mux.HandleFunc("POST /api/v1/clips/{id}/temp",  am.authMiddleware(am.requireRole("editor", am.handleCreateTempFile)))
am.mux.HandleFunc("DELETE /api/v1/temp",           am.authMiddleware(am.requireRole("editor", am.handleDeleteAllTempFiles)))
am.mux.HandleFunc("POST /api/v1/clips/diff",       am.authMiddleware(am.requireRole("viewer", am.handleImageDiff)))
am.mux.HandleFunc("POST /api/v1/watch/refresh",    am.authMiddleware(am.requireRole("admin", am.handleRefreshWatches)))

// Web UI auth — login is rate-limited; see Task 7.
am.mux.HandleFunc("POST /api/v1/auth/login",  am.rateLimitLogin(am.handleLogin))
am.mux.HandleFunc("POST /api/v1/auth/logout", am.handleLogout)
```

Each handler delegates to the existing App/manager method. Example:

```go
func (am *APIManager) handleGetShareStatus(w http.ResponseWriter, r *http.Request) {
    sm := am.app.ShareManager()
    if sm == nil {
        am.jsonOK(w, ShareStatus{Shares: []ShareInfo{}, Follows: []FollowInfo{}})
        return
    }
    am.jsonOK(w, sm.GetShareStatus())
}
```

### Verification + commit

```bash
go build ./... && go test ./internal/app/...
git add internal/app/
git commit -m "feat: REST routes for keys, share, settings, maintenance, plugins extended, auth"
```

---

## Task 7: Auth middleware — cookie + Bearer, signing key, rate limiting

**Files:** `internal/app/api_manager.go`, `internal/app/session.go`

### Step 1: Persistent signing key

```go
// internal/app/session.go
package app

import (
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"
)

const sessionTTL = 24 * time.Hour

func loadOrCreateSigningKey(dataDir string) ([]byte, error) {
    keyPath := filepath.Join(dataDir, "session.key")
    if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
        return data, nil
    }
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, err
    }
    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return nil, err
    }
    return key, nil
}

// Token: "{apiKeyID}.{expiresUnix}.{base64url(hmac)}"
func signSessionToken(key []byte, apiKeyID int64, expires time.Time) string {
    payload := fmt.Sprintf("%d.%d", apiKeyID, expires.Unix())
    mac := hmac.New(sha256.New, key)
    mac.Write([]byte(payload))
    return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifySessionToken(key []byte, token string) (int64, error) {
    parts := strings.Split(token, ".")
    if len(parts) != 3 {
        return 0, fmt.Errorf("malformed token")
    }
    payload := parts[0] + "." + parts[1]
    sig, err := base64.RawURLEncoding.DecodeString(parts[2])
    if err != nil {
        return 0, err
    }
    mac := hmac.New(sha256.New, key)
    mac.Write([]byte(payload))
    if !hmac.Equal(mac.Sum(nil), sig) {
        return 0, fmt.Errorf("bad signature")
    }
    id, err := strconv.ParseInt(parts[0], 10, 64)
    if err != nil {
        return 0, err
    }
    exp, err := strconv.ParseInt(parts[1], 10, 64)
    if err != nil {
        return 0, err
    }
    if time.Now().Unix() > exp {
        return 0, fmt.Errorf("expired")
    }
    return id, nil
}
```

Take `dataDir` as a `NewAPIManager` parameter:

```go
func NewAPIManager(app *App, dataDir string) *APIManager {
    key, err := loadOrCreateSigningKey(dataDir)
    if err != nil {
        log.Printf("api: signing key: %v — web UI sessions disabled", err)
    }
    am := &APIManager{
        app: app, mux: http.NewServeMux(),
        signingKey:   key,
        loginLimiter: newLoginRateLimiter(),
    }
    am.registerRoutes()
    return am
}
```

Update `Bootstrap` to pass `opts.DataDir` to `NewAPIManager`.

### Step 2: `authMiddleware` accepts Bearer or cookie

```go
func (am *APIManager) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
            key := strings.TrimPrefix(h, "Bearer ")
            keyCtx, err := am.validateAPIKey(key)
            if err != nil {
                am.jsonError(w, http.StatusUnauthorized, "invalid API key")
                return
            }
            next(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, keyCtx)))
            return
        }
        if c, err := r.Cookie("_mp_session"); err == nil && am.signingKey != nil {
            if id, err := verifySessionToken(am.signingKey, c.Value); err == nil {
                if keyCtx, err := am.loadKeyContextByID(id); err == nil {
                    next(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey, keyCtx)))
                    return
                }
            }
        }
        am.jsonError(w, http.StatusUnauthorized, "authentication required")
    }
}

func (am *APIManager) hasValidSession(r *http.Request) bool {
    if am.signingKey == nil {
        return false
    }
    c, err := r.Cookie("_mp_session")
    if err != nil {
        return false
    }
    _, err = verifySessionToken(am.signingKey, c.Value)
    return err == nil
}
```

Add `loadKeyContextByID(id int64) (*apiKeyContext, error)` — loads the key
context by `api_keys.id`. The cookie stores the integer ID, not the
plaintext key.

### Step 3: Login / logout

```go
func (am *APIManager) handleLogin(w http.ResponseWriter, r *http.Request) {
    // If loadOrCreateSigningKey failed at startup, signingKey is nil and we
    // can't issue a session cookie. The Bearer-token auth path still works
    // (it doesn't depend on signingKey), but the web UI is unusable until
    // an operator resolves the underlying file-permission issue.
    if am.signingKey == nil {
        am.jsonError(w, http.StatusServiceUnavailable, "web UI sessions unavailable: signing key not initialized")
        return
    }
    var req struct{ Key string `json:"key"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        am.jsonError(w, http.StatusBadRequest, "invalid request")
        return
    }
    keyCtx, err := am.validateAPIKey(req.Key)
    if err != nil {
        am.jsonError(w, http.StatusUnauthorized, "invalid API key")
        return
    }
    token := signSessionToken(am.signingKey, keyCtx.KeyID, time.Now().Add(sessionTTL))
    http.SetCookie(w, &http.Cookie{
        Name: "_mp_session", Value: token, Path: "/",
        HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
        MaxAge: int(sessionTTL.Seconds()),
    })
    am.jsonOK(w, map[string]string{"status": "ok"})
}

func (am *APIManager) handleLogout(w http.ResponseWriter, r *http.Request) {
    http.SetCookie(w, &http.Cookie{Name: "_mp_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
    am.jsonOK(w, map[string]string{"status": "ok"})
}
```

### Step 4: Login rate limiter

```go
type loginRateLimiter struct {
    mu      sync.Mutex
    buckets map[string][]time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
    return &loginRateLimiter{buckets: make(map[string][]time.Time)}
}

func (l *loginRateLimiter) allow(ip string, max int, window time.Duration) bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    now := time.Now()
    cutoff := now.Add(-window)
    attempts := l.buckets[ip]
    n := 0
    for _, t := range attempts {
        if t.After(cutoff) {
            attempts[n] = t
            n++
        }
    }
    attempts = attempts[:n]
    if len(attempts) >= max {
        l.buckets[ip] = attempts
        return false
    }
    l.buckets[ip] = append(attempts, now)
    return true
}

func (am *APIManager) rateLimitLogin(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        ip, _, _ := net.SplitHostPort(r.RemoteAddr)
        if !am.loginLimiter.allow(ip, 5, time.Minute) {
            am.jsonError(w, http.StatusTooManyRequests, "too many login attempts")
            return
        }
        next(w, r)
    }
}
```

### Verification + commit

```bash
go build ./... && go test ./internal/app/...
git add internal/app/
git commit -m "feat(api): cookie+Bearer auth, HMAC session token, login rate limit"
```

---

## Task 8: Bootstrap API key

**Files:** `internal/app/bootstrap_key.go`

```go
package app

import (
    "log"
    "os"
)

// BootstrapAPIKey creates a first admin key when no keys exist.
// Suppressed by MAHPASTESD_BOOTSTRAP_KEY=false; any other value (or unset) → enabled.
func BootstrapAPIKey(apiMgr *APIManager) {
    if os.Getenv("MAHPASTESD_BOOTSTRAP_KEY") == "false" {
        return
    }
    keys, err := apiMgr.ListKeys()
    if err != nil {
        log.Printf("api: bootstrap check failed: %v", err)
        return
    }
    if len(keys) > 0 {
        return
    }
    result, err := apiMgr.CreateKey("bootstrap", "admin", 0)
    if err != nil {
        log.Printf("api: failed to bootstrap key: %v", err)
        return
    }
    log.Println("api: no API keys found — bootstrapping admin key")
    log.Printf("api: bootstrap admin key: %s", result.Key)
    log.Println("api: store this key securely. it will not be printed again.")
}
```

`CreateKey` returns a key in the form `mp_` + 32 hex chars (no `admin_`
infix — `api_manager.go:295`). All grep/match patterns in later tasks use
`mp_[a-f0-9]+`.

### Verification + commit

```bash
go build ./...
git add internal/app/bootstrap_key.go
git commit -m "feat: bootstrap admin API key on fresh server install"
```

---

## Task 9: Server entry point (`cmd/mahpastesd/main.go`)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "path/filepath"
    "strconv"
    "strings"
    "syscall"

    app "go-clipboard/internal/app"
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

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    broker := webui.NewSSEBroker()
    core := app.NewApp()

    err = core.Bootstrap(ctx, app.BootstrapOptions{
        DB:                 db,
        DataDir:            dataDir,
        Bridge:             broker, // satisfies bridgeiface.Bridge
        InitClipboard:      false,  // headless: no display
        PermissionCallback: headlessPermissionCallback(dataDir),
    })
    if err != nil {
        log.Fatalf("bootstrap: %v", err)
    }

    core.APIManager().MountWebUI(webui.Assets, broker)
    app.BootstrapAPIKey(core.APIManager())

    port := 44557 // shares the desktop default; set MAHPASTESD_PORT to coexist
    if p := os.Getenv("MAHPASTESD_PORT"); p != "" {
        parsed, err := strconv.Atoi(p)
        if err != nil {
            log.Fatalf("MAHPASTESD_PORT=%q is not an integer", p)
        }
        port = parsed
    }
    bindAll := os.Getenv("MAHPASTESD_BIND_ALL") == "1"
    if bindAll {
        log.Println("WARNING: listening on 0.0.0.0 without TLS.")
        log.Println("API keys and session cookies are sent in plaintext. Use a reverse")
        log.Println("proxy (nginx, Caddy) that terminates TLS before network exposure.")
    }

    status, err := core.APIManager().Start(port, bindAll)
    if err != nil {
        log.Fatalf("api: %v", err)
    }
    log.Printf("listening on %s", status.URL)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    log.Println("shutting down...")
    cancel()
    core.Shutdown(ctx)
}

// headlessPermissionCallback grants filesystem access only inside dataDir.
// Uses filepath.Rel for proper boundary checks — a simple HasPrefix lets
// "/var/lib/mahpastes2/foo" through when dataDir is "/var/lib/mahpastes".
//
// EvalSymlinks fails on paths whose leaf doesn't exist yet (which is the
// common case for fs_write to a new file). Resolving the nearest existing
// ancestor and rejoining the unresolved suffix prevents a plugin from
// writing to a symlinked directory that escapes dataDir — see
// plugin/api_fs.go:247 (os.WriteFile follows symlinks in the parent).
func headlessPermissionCallback(dataDir string) func(name, kind, p string) string {
    absData, err := filepath.Abs(dataDir)
    if err != nil {
        return func(name, kind, p string) string { return "" }
    }
    if resolved, err := filepath.EvalSymlinks(absData); err == nil {
        absData = resolved
    }
    return func(name, kind, p string) string {
        canonical, err := canonicalizeViaAncestor(p)
        if err != nil {
            return ""
        }
        rel, err := filepath.Rel(absData, canonical)
        if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
            return ""
        }
        return p
    }
}

// canonicalizeViaAncestor returns p with symlinks in its existing prefix
// resolved. It walks parents until it finds one that exists, EvalSymlinks
// on that, then re-joins the leaf segments that didn't exist on disk.
// The leaf segments are literal basenames pulled from the cleaned input,
// so they can't reintroduce "..".
func canonicalizeViaAncestor(p string) (string, error) {
    abs, err := filepath.Abs(p)
    if err != nil {
        return "", err
    }
    cur := filepath.Clean(abs)
    var suffix []string
    for {
        if _, err := os.Lstat(cur); err == nil {
            resolved, err := filepath.EvalSymlinks(cur)
            if err != nil {
                return "", err
            }
            for i := len(suffix) - 1; i >= 0; i-- {
                resolved = filepath.Join(resolved, suffix[i])
            }
            return filepath.Clean(resolved), nil
        }
        parent := filepath.Dir(cur)
        if parent == cur {
            return "", fmt.Errorf("no existing ancestor for %q", p)
        }
        suffix = append(suffix, filepath.Base(cur))
        cur = parent
    }
}
```

### Verification

```bash
go build -o bin/mahpastesd ./cmd/mahpastesd/
./bin/mahpastesd 2>&1 | tee /tmp/mahpastesd.log &
SERVER_PID=$!
sleep 1
BOOTSTRAP_KEY=$(grep -oE 'mp_[a-f0-9]+' /tmp/mahpastesd.log | head -1)
test -n "$BOOTSTRAP_KEY" || { echo "no bootstrap key in log"; kill $SERVER_PID; exit 1; }
MP_API_URL=http://localhost:44557 MP_API_KEY="$BOOTSTRAP_KEY" mp api status
MP_API_URL=http://localhost:44557 MP_API_KEY="$BOOTSTRAP_KEY" mp clip list
kill $SERVER_PID
```

### Commit

```bash
git add cmd/mahpastesd/
git commit -m "feat: headless server entry point (mahpastesd)"
```

---

## Task 10: Web UI login page

**Files:** `frontend/login.html`

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>mahpastes — sign in</title>
    <link href="/dist/output.css" rel="stylesheet">
</head>
<body class="bg-stone-50 min-h-screen flex items-center justify-center font-mono">
    <div class="bg-white rounded-lg border border-stone-200 shadow-sm p-8 w-full max-w-sm">
        <h1 class="text-sm font-semibold text-stone-800 uppercase tracking-wide text-center mb-6">mahpastes</h1>
        <form id="login-form" class="space-y-4">
            <div>
                <label for="api-key" class="block text-xs font-medium text-stone-600 mb-1.5">API Key</label>
                <input id="api-key" type="password" required autocomplete="off"
                    class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 px-3 py-2.5 transition-colors"
                    placeholder="mp_...">
            </div>
            <button type="submit" class="w-full bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 rounded-md transition-colors">Sign in with API key</button>
        </form>
        <p id="login-error" class="text-[11px] text-red-500 text-center mt-4 hidden"></p>
    </div>
    <script>
    document.getElementById('login-form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const key = document.getElementById('api-key').value;
        const errEl = document.getElementById('login-error');
        try {
            const res = await fetch('/api/v1/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                credentials: 'same-origin',
                body: JSON.stringify({ key }),
            });
            if (res.ok) {
                window.location.href = '/';
            } else if (res.status === 429) {
                errEl.textContent = 'Too many attempts. Wait a minute, then try again.';
                errEl.classList.remove('hidden');
            } else {
                errEl.textContent = 'Invalid API key.';
                errEl.classList.remove('hidden');
            }
        } catch {
            errEl.textContent = 'Connection error. Is the server running?';
            errEl.classList.remove('hidden');
        }
    });
    </script>
</body>
</html>
```

Loads `/dist/output.css` directly — public per the SPA handler in Task 5.

### Commit

```bash
git add frontend/login.html
git commit -m "feat: web UI login page"
```

---

## Task 11: REST glue layer

**Files:** `frontend/js/rest-glue.js`, `frontend/index.html`

### Step 1: Glue file (self-gating IIFE)

The script is always loaded by `index.html`. On desktop it returns early
because Wails has already populated `window.go.main.*`. On the server it
installs the `fetch()`-backed shims.

```javascript
// frontend/js/rest-glue.js
(function() {
    if (window.mahpastesMode !== 'server') {
        return; // desktop: Wails provides window.go.main and window.runtime
    }

    const fetchJSON = async (url, opts = {}) => {
        const res = await fetch(url, { ...opts, credentials: 'same-origin' });
        if (res.status === 401) { window.location = '/login.html'; throw new Error('Unauthorized'); }
        if (res.status === 429) throw new Error('Rate limited');
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        if (res.status === 204) return null;
        return res.json();
    };
    const postJSON = (url, body) => fetchJSON(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const putJSON  = (url, body) => fetchJSON(url, { method: 'PUT',  headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const del      = (url)       => fetchJSON(url, { method: 'DELETE' });

    window.go = { main: {} };

    // URLSearchParams stringifies arrays as comma-joined values, so build
    // the query manually for any repeated-key parameter.
    const buildClipsQuery = (archived, tagIds, hidden, sort, dir) => {
        const q = new URLSearchParams();
        if (archived !== undefined) q.set('archived', String(archived));
        for (const t of tagIds || []) q.append('tag', String(t));
        for (const h of hidden || []) q.append('hidden', String(h));
        if (sort) q.set('sort', sort);
        if (dir)  q.set('dir', dir);
        return q.toString();
    };

    window.go.main.App = {
        GetClips: (archived, tagIds, hidden, sort, dir) =>
            fetchJSON(`/api/v1/clips?${buildClipsQuery(archived, tagIds, hidden, sort, dir)}`),

        UpdateClipData: (id, contentType, base64Data, filename) =>
            putJSON(`/api/v1/clips/${id}/data`, { content_type: contentType, data: base64Data, filename }),

        PreviewMergeTag: (sourceID, destID) =>
            postJSON(`/api/v1/tags/${sourceID}/merge-preview`, { dest_id: destID }),

        FindClipsByFilenameAndTag: (filenames, tagID) =>
            postJSON(`/api/v1/clips/find`, { filenames, tag_id: tagID }),

        // ... every other method classified "Expose in server UI" in the design doc matrix
    };
    window.go.main.PluginService    = { /* ... */ };
    window.go.main.ServeService     = { /* ... */ };
    window.go.main.ShareService     = { /* ... */ };
    window.go.main.APIService       = { /* ... */ };
    // Hidden surfaces — throw loudly if accidentally called
    const desktopOnly = (name) => new Proxy({}, {
        get: () => () => { throw new Error(`${name} is desktop-only in server mode`); }
    });
    window.go.main.ClipboardService = desktopOnly('ClipboardService');
    window.go.main.TransferService  = desktopOnly('TransferService');

    // Runtime shim (SSE)
    const sse = new EventSource('/api/v1/events');
    const sseHandlers = new Map();
    window.runtime = {
        EventsOn(name, cb) {
            const wrapped = (e) => cb(JSON.parse(e.data));
            sseHandlers.set(cb, { name, wrapped });
            sse.addEventListener(name, wrapped);
        },
        EventsOff(name) {
            for (const [cb, info] of sseHandlers) {
                if (info.name === name) {
                    sse.removeEventListener(name, info.wrapped);
                    sseHandlers.delete(cb);
                }
            }
        },
        EventsEmit() {},
        OnFileDrop() { return () => {}; },
    };
})();
```

### Step 2: Load order in `index.html`

Use static `<script>` tags. Classic script tags execute in document order, so
the glue runs before any subsequent classic script touches `window.go.main`.
No `document.write`, no dynamic `<script>` injection (which doesn't reliably
block subsequent inline scripts).

Add to `<head>`, immediately after the existing `<link>` tags and **before**
any other `<script>`:

```html
<script>
    window.mahpastesMode = (window.location.hostname === 'wails.localhost') ? 'desktop' : 'server';
    if (window.mahpastesMode === 'server') {
        document.documentElement.classList.add('server-mode');
    }
</script>
<script src="js/rest-glue.js"></script>
<!-- existing classic <script> tags follow -->
```

On desktop, `rest-glue.js` is fetched but its IIFE returns immediately. The
ms-scale overhead is acceptable; the alternative (conditional fetch via
dynamic injection) doesn't guarantee execution order in modern browsers.

If `frontend/js/app.js` (or any other script that calls `window.go.main`)
ever becomes `<script type="module">`, this approach stops working — module
scripts are always deferred and run after parsing. In that case, switch to a
`window.mahpastesReady` Promise that the glue resolves and that the modules
await before first use. Today everything is classic; document this constraint
inline.

### Step 3: Audit call sites

```bash
grep -rn 'window\.go\.main\.' frontend/js/ frontend/index.html
grep -rn 'window\.runtime\.' frontend/js/ frontend/index.html
```

For every match, classify per the design doc's Wails-to-REST matrix:
1. Backed by REST endpoint (handled by glue)
2. Browser-adapted (e.g., backup download via `<a href>`)
3. Hidden in server mode (gated by `window.mahpastesMode === 'server'` or `.server-mode` CSS)

### Commit

```bash
git add frontend/js/rest-glue.js frontend/index.html
git commit -m "feat: REST glue layer + SSE runtime shim for web UI"
```

---

## Task 12: Hide desktop-only UI in server mode

**Files:** `frontend/js/api-settings.js`, `frontend/js/watch.js`,
`frontend/js/plugins.js`, `frontend/js/ui.js`, `frontend/css/main.css`

### Step 1: CSS rule

```css
/* frontend/css/main.css */
.server-mode .desktop-only { display: none !important; }
```

### Step 2: Mark elements

Add `desktop-only` to elements that should hide in server mode (per the
server-mode UI policy table in the design doc):

- API start/stop button + bind-all toggle (`api-settings.js`)
- Native plugin import button (`plugins.js`)
- Native folder picker buttons in watch UI (`watch.js`)
- "Copy as file" and "Drag to desktop" affordances (`ui.js`)
- "Open with default app" / "Choose application" entries in card context menu (`ui.js`)

### Step 3: JS gating for runtime-created controls

```javascript
if (window.mahpastesMode === 'server') return; // skip render
```

at the top of render functions that build desktop-only controls.

### Step 4: Verify visually

Start the server, sign in, walk every modal/panel listed in the design doc.
Each "Hidden" row must not show a control. "Replaced" rows (e.g., backup)
must use the browser-adapted mechanism.

### Commit

```bash
git add frontend/
git commit -m "feat: hide desktop-only UI in server mode"
```

---

## Task 13: Makefile targets

**Files:** `Makefile`

```makefile
mahpastesd: ## Build headless server for current platform
	go build -o bin/mahpastesd ./cmd/mahpastesd/

mahpastesd-cross: ## Cross-compile server for all platforms
	GOOS=linux  GOARCH=amd64 go build -o bin/mahpastesd-linux-amd64  ./cmd/mahpastesd/
	GOOS=linux  GOARCH=arm64 go build -o bin/mahpastesd-linux-arm64  ./cmd/mahpastesd/
	GOOS=darwin GOARCH=amd64 go build -o bin/mahpastesd-darwin-amd64 ./cmd/mahpastesd/
	GOOS=darwin GOARCH=arm64 go build -o bin/mahpastesd-darwin-arm64 ./cmd/mahpastesd/
```

The existing `build`, `dev`, `bindings`, `install` targets continue to work
unchanged — `main.go` and `wails.json` stayed at the repo root.

Update the help text to list the new targets.

### Commit

```bash
git add Makefile
git commit -m "build: add mahpastesd build targets"
```

---

## Task 14: `mp` CLI — REST-backed key commands

**Files:** `cmd/mp/api.go`

Replace the "desktop app only" error paths with REST calls:

```go
var apiKeyCreateCmd = &cobra.Command{
    Use:   "create [name]",
    Short: "Create an API key",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        role, _ := cmd.Flags().GetString("role")
        scoped, _ := cmd.Flags().GetInt64("scoped-tag")
        var out APIKeyCreateResult
        if err := client.PostJSON("/api/v1/keys", map[string]any{"name": args[0], "role": role, "scoped_tag_id": scoped}, &out); err != nil {
            return err
        }
        if jsonOutput {
            return printJSON(out)
        }
        printKeyValue([]kv{{"id", out.ID}, {"name", out.Name}, {"key", out.Key}, {"role", out.Role}})
        fmt.Fprintln(os.Stderr, "(save this key — it cannot be retrieved again)")
        return nil
    },
}
// Same shape for list and revoke.
```

### Verification

```bash
./bin/mahpastesd 2>&1 | tee /tmp/mahpastesd.log &
SERVER_PID=$!; sleep 1
BOOTSTRAP_KEY=$(grep -oE 'mp_[a-f0-9]+' /tmp/mahpastesd.log | head -1)
MP_API_KEY="$BOOTSTRAP_KEY" mp api key create my-key --role admin
MP_API_KEY="$BOOTSTRAP_KEY" mp api key list
kill $SERVER_PID
```

### Commit

```bash
git add cmd/mp/api.go
git commit -m "feat(mp): REST-backed api key create/list/revoke"
```

---

## Task 15: E2E tests for server

**Files:** `e2e/fixtures/server-fixtures.ts`, `e2e/tests/server/*.spec.ts`

### Step 1: Server fixture

```typescript
// e2e/fixtures/server-fixtures.ts
import { spawn } from 'child_process';
import { mkdtemp } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';

export async function spawnServer(opts: { port?: number } = {}) {
    const dataDir = await mkdtemp(join(tmpdir(), 'mahpastesd-e2e-'));
    const port = opts.port ?? 44600;
    const proc = spawn('./bin/mahpastesd', [], {
        env: { ...process.env, MAHPASTES_DATA_DIR: dataDir, MAHPASTESD_PORT: String(port) },
        stdio: ['ignore', 'pipe', 'pipe'],
    });
    const bootstrapKey = await waitForLogMatch(proc, /bootstrap admin key: (mp_[a-f0-9]+)/);
    return { proc, dataDir, port, bootstrapKey, url: `http://localhost:${port}` };
}
```

### Step 2: Minimum acceptance tests

```typescript
// e2e/tests/server/bootstrap.spec.ts — server starts, prints key
// e2e/tests/server/api-key-crud.spec.ts — create/list/revoke via REST
// e2e/tests/server/clips-rest.spec.ts — upload, list, delete via REST
// e2e/tests/server/web-ui-login.spec.ts — login form sets cookie, redirects
// e2e/tests/server/sse-events.spec.ts — CLI upload → SSE event in browser
```

Minimum bar: server boots, bootstrap key is printed, `mp api status` and
`mp clip list` work end-to-end. Web UI + SSE coverage may ship as a follow-up
if Playwright wiring takes longer than expected — flag during execution
rather than block this plan on it.

### Run

```bash
make mahpastesd
cd e2e && set -o pipefail && npm test -- --grep "server" 2>&1 | tee /tmp/server-e2e.log | tail -100
```

### Commit

```bash
git add e2e/
git commit -m "test: e2e coverage for mahpastesd"
```

---

## Task 16: Final integration

### Step 1: Both binaries build, cross-platform

```bash
make build
make mahpastesd
GOOS=linux  GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux  GOARCH=arm64 go build ./cmd/mahpastesd/
```

### Step 2: Run server

```bash
MAHPASTESD_PORT=44600 ./bin/mahpastesd 2>&1 | tee /tmp/mahpastesd.log &
SERVER_PID=$!
sleep 1
BOOTSTRAP_KEY=$(grep -oE 'mp_[a-f0-9]+' /tmp/mahpastesd.log | head -1)
```

### Step 3: REST API smoke

```bash
export MP_API_URL=http://localhost:44600
export MP_API_KEY="$BOOTSTRAP_KEY"
mp api status
mp clip list
mp tag create test-tag
echo "hello" | mp clip upload - --tag test-tag --name test.txt
mp clip list --tag test-tag
mp tag delete test-tag
```

### Step 4: Web UI smoke

Open `http://localhost:44600`. Verify: login page renders with full styling
(CSS loads without auth), signing in with `$BOOTSTRAP_KEY` lands at `/`,
main UI loads, server-mode-only controls are hidden, uploading a clip via
CLI surfaces a `watch:import` (or equivalent) event in the browser within
~1s.

### Step 5: P2P sync smoke (optional)

Two servers + one desktop on different data dirs. Publish + follow propagates
between all three.

### Step 6: Full e2e

```bash
cd e2e && set -o pipefail && npm test 2>&1 | tee /tmp/e2e.log | tail -100
```

Desktop tests must pass unchanged. Server tests must pass.

### Step 7: Final commit + cleanup

```bash
kill $SERVER_PID
git status                          # nothing should be unexpectedly uncommitted
git add Makefile docs/              # explicit paths only — never `git add .`
git commit -m "feat: complete headless server implementation" || echo "nothing to commit"
```

---

## What changed between rev 5 and rev 6

| Finding | Fix |
|---------|-----|
| [P1] New routes in Task 6 needed explicit tag-scope rules — the existing handlers (`api_manager.go:894` delete/archive, `1127` merge) call `enforceTagScope`/`isTagInScope`, but the plan only said "delegate" | Task 6 now annotates each new route inline: `PUT /clips/{id}/data` must `enforceTagScope(clipID)`; `POST /tags/{id}/merge-preview` must scope-check both source and destination tags (mirroring `handleMergeTag`); `POST /clips/find` requires `tag_id`, must validate the tag is in the scoped key's scope, and must filter results to that scope so a scoped key cannot probe untagged or out-of-scope filenames/hashes. |
| [P2] Design doc matrix was stale — still classified `FindClipsByFilenameAndTag` as an existing GET, `PreviewMergeTag` as sharing the mutating merge route, and `UpdateClipData` as hidden/desktop-only | Updated three rows in `2026-05-27-headless-server-design.md` to match the plan: dedicated `POST /clips/find` (exact filename match, not fuzzy), dedicated non-mutating `POST /tags/{id}/merge-preview`, and `PUT /clips/{id}/data` with size note. Task 11's "use the matrix" instruction is now safe. |
| [P2] `PUT /clips/{id}/data` had no body size limit; existing multipart upload caps at 100MB (`api_manager.go:800`) | Annotated the route with the recipe: wrap the request body with `http.MaxBytesReader` at ~150MB (to accommodate base64 inflation ~133% on a 100MB clip), then sanity-check the decoded byte length against the same 100MB ceiling as the multipart handler. |
| [P2] If `loadOrCreateSigningKey` fails, the plan logged a warning but `handleLogin` would still call `signSessionToken(nil, ...)` | Added an explicit guard at the top of `handleLogin` returning `503 Service Unavailable` when `am.signingKey == nil`. Bearer-token auth (used by `mp` CLI) continues to work; only the web UI session path is affected. |

## What changed between rev 4 and rev 5

| Finding | Fix |
|---------|-----|
| [P1] `UpdateClipData` is pure DB logic, not a dialog method — the image editor and duplicate-overwrite flow depend on it | Removed from the desktop-wrapper migration list in Task 4 Step 6. Stays on `*app.App`. New `PUT /api/v1/clips/{id}/data` route in Task 6 + glue entry in Task 11. |
| [P1] Task 7 session code used `keyCtx.ID`, but the actual struct field is `KeyID` (`api_manager.go:59`) | Renamed throughout Task 7 — `signSessionToken(am.signingKey, keyCtx.KeyID, ...)` etc. |
| [P1] Glue serialized `tagIds`/`hidden` arrays as comma strings via `URLSearchParams({tag: array})`, but Task 6 expects repeated keys | Glue now builds the query string with `q.append('tag', id)` per element. Multi-tag filtering, hidden tag exclusion, and the SQL injection guards on `sort`/`dir` all line up. |
| [P2] `PreviewMergeTag` and `FindClipsByFilenameAndTag` were claimed as "existing REST" in the design matrix but no routes exist; mapping preview to the merge route would mutate state | Added `POST /api/v1/tags/{id}/merge-preview` (non-mutating) and `POST /api/v1/clips/find` (batch filename lookup; body carries the filename array since GET-with-list is awkward). Glue entries in Task 11. |
| [P2] `canonicalizeViaAncestor` used `fmt.Errorf` but the server entry's import block didn't include `fmt`; verification ran `./bin/mahpastesd` after `go build ./cmd/mahpastesd/` which writes `./mahpastesd` instead | Added `"fmt"` to the import list. Verification now uses `go build -o bin/mahpastesd ./cmd/mahpastesd/` matching the Makefile target. |

## What changed between rev 3 and rev 4

| Finding | Fix |
|---------|-----|
| [P1] Wails JS binding namespace is keyed by Go package — moving services to `internal/app/` would emit `window.go.app.ShareService` and break the existing frontend that calls `window.go.main.ShareService` | `api_service.go`, `share_service.go`, `serve_service.go` stay at root in `package main`. They use the App accessors (`ShareManager()`, `ServeManager()`, `APIManager()`) added in Task 1. |
| [P1] `App.clipboardService *ClipboardService` field can't reference the root type from `internal/app/` | Introduced `ClipboardCopier` interface in `internal/app/app.go`; `SetClipboardService` takes the interface. Root `*ClipboardService` satisfies it; mahpastesd passes nothing. The six clipboard handlers (`api_manager.go:1857,1862,2754,2759,2783,2788`) gate on nil with a 501. |
| [P1] Root `transfer_service.go` uses moved types unqualified | Task 4 Step 7 lists the explicit `app.X` rewrites for `TransferCapabilities`, `PrepareTransferRequest`, `PreparedTransferItem`, `StartNativeDragRequest` and the matching test updates. |
| [P1] `pluginService` was constructed inside `OnStartup` while bound nil in `Bind` | `NewPluginService(core)` runs before `wails.Run`. Bridge is wired in `OnStartup` via `pluginService.SetBridge(bridge)`. Dialog method guards on `s.bridge == nil`. |
| [P1] `ConfirmRestoreBackup` is pure logic called by the REST restore handler — it cannot move to the desktop wrapper | Removed from the dialog-method migration list in Task 4 Step 6. Stays in `internal/app/`. |
| [P1] `handleListClips` doesn't accept multi-tag / hidden / sort / dir, but REST glue assumes the full Wails surface | Task 6 Step 0 adds the missing query params with whitelist validation, plus the `untagged` and `folder_tag` branches. |
| [P2] `Assets: http.FS(webui.Assets)` doesn't compile — Wails v2 wants `fs.FS` | Task 4 Step 8 uses `Assets: webui.Assets` directly. |
| [P2] `headlessPermissionCallback` lets symlink escapes through when the leaf doesn't exist yet (typical for `fs_write` to a new file) | Task 9 adds `canonicalizeViaAncestor` — walks up to the nearest existing directory, calls `EvalSymlinks` on that, then re-joins the unresolved leaf segments. Plugins writing to symlinked parents are blocked. |

## What changed between rev 2 and rev 3

| Finding | Fix |
|---------|-----|
| [P1] Task 2 won't compile (App.bridge narrows while dialog calls still exist) | Task 2 narrows only `plugin/*`. App's bridge field stays `*wailsbridge.Bridge` until Task 4 — narrowed only after dialog methods move to `desktop_app.go`. |
| [P1] `DesktopApp` breaks Wails binding name | Wrapper renamed to `App` (in `package main` at root, embedding `*app.App`). Wails generates `window.go.main.App` from the type name — preserved. |
| [P1] Services have unexported access after move | Layout switched to Option A: root services stay at root. Task 1 adds exported accessors (`DB`, `PluginManager`, `TempStore`, `TempDir`, `PrepareClipTransferItem`) and `NewTransferFileHandler`. `share_service.go`, `serve_service.go`, `api_service.go` move to `internal/app/` where they keep direct access. |
| [P1] Wails build layout unverified | `main.go` and `wails.json` stay at the repo root — no Wails reconfiguration. Task 4 Step 12 confirms `wails doctor` / `wails dev` / `wails build` / `wails generate module` all still pass. |
| [P1] `strings.HasPrefix(p, dataDir)` is unsafe | Replaced with `filepath.Abs` + `filepath.EvalSymlinks` + `filepath.Rel` boundary check in `headlessPermissionCallback` (Task 9). |
| [P2] `transfer_types.go` missing from move list | Added to Task 4 Step 2. |
| [P2] `TransferFileHandler{App: core}` uses unexported field | `NewTransferFileHandler(a *App)` constructor added in Task 1; root `main.go` uses it. |
| [P2] Bootstrap key regex (`mp_admin_...`) doesn't match `CreateKey` output | All grep patterns now use `mp_[a-f0-9]+` (Tasks 9, 14, 15, 16). |
| [P2] Login CSS auth-gated | SPA handler in Task 5 serves any existing static file without auth; only the SPA shell + unknown deep links require a session. |
| [P2] REST glue script load order race | `rest-glue.js` loads via static `<script>` tag in `index.html`. The IIFE no-ops in desktop mode. Classic script ordering is deterministic across browsers. |
