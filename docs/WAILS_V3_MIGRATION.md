# Wails v3 Migration Playbook

Execution recipe for swapping mahpastes from Wails v2 to Wails v3.

**Companion doc:** `docs/WAILS_V3_FRONTEND_CONTRACT.md` — the DOM/binding
surface the v3 shim must reproduce.

**Prep status:** All runtime calls are already routed through
`internal/wailsbridge`. `TestNoRuntimeImportOutsideBridge` guards that
invariant. The plugin and app packages no longer import
`github.com/wailsapp/wails/v2/pkg/runtime` directly.

## 0. Preconditions — DO NOT START UNTIL THESE HOLD

- [ ] Wails v3 is labelled **stable** (v3.0.0) or a trusted **RC** (v3.0.0-rcX)
      by the official release page. As of this playbook's writing (2026-04-19),
      v3 is at `v3.0.0-alpha.77` and the binding generator is explicitly WIP.
      Re-run the version check via `curl -s https://api.github.com/repos/wailsapp/wails/releases/latest | jq -r '.tag_name'`.
- [ ] The v3 docs at `https://v3.wails.io/` cover services, events, dialogs,
      windows, and asset handlers with code examples (not "coming soon").
- [ ] The frontend bindings generator is marked stable in the v3 release
      notes — not "experimental" or "subject to change".
- [ ] A mahpastes test branch has successfully migrated a toy example first
      (even just the Greet example from the v3 repo) so the toolchain works
      on this machine.

If ANY of these is false, stop. The prep in this repo is durable; nothing
requires urgency. Ship on v2.12.0 until v3 matures.

## 1. Dependencies

```bash
# Uninstall v2 CLI (optional — v3 uses wails3 binary, so they can coexist)
rm -f ~/go/bin/wails

# Install v3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
~/go/bin/wails3 version   # expect stable/RC
```

```bash
# Module swap
go get github.com/wailsapp/wails/v3@latest
go get github.com/wailsapp/wails/v2@none   # remove v2
go mod tidy
```

## 2. main.go rewrite

v2 shape (current):

```go
err := wails.Run(&options.App{
    Title:     "mahpastes",
    Width:     1280, Height: 800, MinWidth: 800, MinHeight: 600,
    StartHidden: os.Getenv("MAHPASTES_START_HIDDEN") == "1",
    AssetServer: &assetserver.Options{
        Assets:  assets,
        Handler: transferHandler,
    },
    BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
    OnStartup:        app.startup,
    OnShutdown:       app.shutdown,
    DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: false},
    Bind: []interface{}{app, pluginService, clipboardService, transferService, serveService, apiService, shareService},
    Mac: &mac.Options{
        TitleBar: &mac.TitleBar{TitlebarAppearsTransparent: true, HideTitle: true, HideTitleBar: false, FullSizeContent: true, UseToolbar: false},
        About:    &mac.AboutInfo{Title: "mahpastes", Message: "A local clipboard manager"},
    },
})
```

v3 shape:

```go
app := application.New(application.Options{
    Name: "mahpastes",
    Services: []application.Service{
        application.NewService(app),
        application.NewService(pluginService),
        application.NewService(clipboardService),
        application.NewService(transferService),
        application.NewService(serveService),
        application.NewService(apiService),
        application.NewService(shareService),
    },
    Assets: application.AssetOptions{
        Handler: transferHandler, // same http.Handler interface
    },
    Mac: application.MacOptions{
        ApplicationShouldTerminateAfterLastWindowClosed: true,
    },
})

// Lifecycle hooks. Verify event names against v3 docs at swap time.
app.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
    app.Logger.Info("mahpastes starting")
    // Equivalent of v2's OnStartup: we now have an *application.App instead
    // of a context.Context. See "wailsbridge internals" below.
    appCtx := context.Background() // or app.Context() if v3 exposes one
    a.startup(appCtx)
})
app.OnApplicationEvent(events.Common.ApplicationShutdown, func(_ *application.ApplicationEvent) {
    a.shutdown(context.Background())
})

win := app.Window.NewWithOptions(application.WebviewWindowOptions{
    Title:            "mahpastes",
    Width:            1280, Height: 800,
    MinWidth:         800, MinHeight: 600,
    Hidden:           os.Getenv("MAHPASTES_START_HIDDEN") == "1",
    BackgroundColour: application.NewRGB(248, 250, 252),
    Mac: application.MacWindow{
        TitleBar:                application.MacTitleBarHiddenInset,
        InvisibleTitleBarHeight: 28,
        Backdrop:                application.MacBackdropNormal,
    },
})
_ = win

if err := app.Run(); err != nil {
    log.Fatalf("%v", err)
}
```

Key field-to-field translation:

| v2 field | v3 equivalent |
|---|---|
| `options.App.Title` | `WebviewWindowOptions.Title` (per-window) |
| `options.App.Width/Height` | `WebviewWindowOptions.Width/Height` (per-window) |
| `options.App.MinWidth/MinHeight` | `WebviewWindowOptions.MinWidth/MinHeight` |
| `options.App.StartHidden` | `WebviewWindowOptions.Hidden` |
| `options.App.AssetServer.Assets` | `application.Options.Assets.FS` or auto-detected from `//go:embed` |
| `options.App.AssetServer.Handler` | `application.AssetOptions.Handler` (same `http.Handler` interface) |
| `options.App.BackgroundColour` | `WebviewWindowOptions.BackgroundColour` via `application.NewRGB(...)` |
| `options.App.OnStartup/OnShutdown` | `app.OnApplicationEvent(events.Common.*, ...)` |
| `options.App.DragAndDrop` | `WebviewWindowOptions.Mac.DragAndDrop` or equivalent per-platform (verify against v3 docs) |
| `options.App.Bind` | `application.Options.Services` |
| `mac.Options.TitleBar.{TitlebarAppearsTransparent,HideTitle,FullSizeContent}` | `application.MacWindow.TitleBar = MacTitleBarHiddenInset` (preset) |
| `mac.Options.About` | v3 separate API — verify the About-dialog wiring at swap time |

## 3. `internal/wailsbridge/bridge.go` swap

The whole point of this prep work: one file's internals change; the ~24
call sites that use `a.bridge.Emit` / `a.bridge.OpenFile` stay unchanged.

```go
package wailsbridge

import "github.com/wailsapp/wails/v3/pkg/application"

type Bridge struct {
    app *application.App
}

func New(app *application.App) *Bridge {
    return &Bridge{app: app}
}

func (b *Bridge) active() bool { return b != nil && b.app != nil }

func (b *Bridge) Emit(name string, data ...any) {
    if !b.active() { return }
    // v3: wrap the positional args as the CustomEvent's Data field so the
    // frontend shim can spread them back out.
    b.app.Event.Emit(&application.CustomEvent{Name: name, Data: data})
}

func (b *Bridge) On(name string, cb func(data ...any)) {
    if !b.active() { return }
    b.app.Event.On(name, func(e *application.CustomEvent) {
        if d, ok := e.Data.([]any); ok { cb(d...) } else { cb(e.Data) }
    })
}

// Once / Off: see v3 docs for the equivalent APIs.
```

`dialogs.go` swap:

```go
func (b *Bridge) OpenFile(opts FileDialogOptions) (string, error) {
    if !b.active() { return "", nil }
    d := b.app.Dialog.OpenFile()
    d.SetTitle(opts.Title)
    if opts.DefaultDirectory != "" { d.SetDirectory(opts.DefaultDirectory) }
    for _, f := range opts.Filters {
        d.AddFilter(f.DisplayName, f.Pattern)
    }
    return d.PromptForSingleSelection()
}
```

Same pattern for `OpenFiles` (use `PromptForMultipleSelection`), `SaveFile`
(`app.Dialog.SaveFile()` + `SetFilename`), and `OpenDirectory`
(`app.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true)` or
whatever v3's directory picker shape is at the time).

**Wiring at startup:** whatever glue code runs when the v3 app starts must
now construct the bridge with `*application.App` rather than
`context.Context`:

```go
// old (v2):
a.bridge = wailsbridge.New(ctx)
// new (v3):
a.bridge = wailsbridge.New(app)
```

Everything else — all ~24 call sites in app.go, backup.go,
plugin_service.go, plugin/api_*.go, plugin/manager.go,
plugin/update_checker.go — **stays unchanged**.

## 4. Platform stubs

`open_darwin.go`, `open_windows.go`, `open_other.go` all take a
`*wailsbridge.Bridge` already. The bridge's dialog methods now call into v3;
these files require zero changes.

`chooseApplicationDialog(b *wailsbridge.Bridge)` still looks identical.

## 5. `frontend/js/v3-bindings-shim.js`

See `docs/WAILS_V3_FRONTEND_CONTRACT.md` for the full surface. Skeleton:

```js
// frontend/js/v3-bindings-shim.js
import * as wails from "/wails/runtime.js";
import * as App from "/bindings/go-clipboard/App.js";
import * as ClipboardService from "/bindings/go-clipboard/ClipboardService.js";
import * as PluginService from "/bindings/go-clipboard/PluginService.js";
import * as TransferService from "/bindings/go-clipboard/TransferService.js";
import * as ServeService from "/bindings/go-clipboard/ServeService.js";
import * as APIService from "/bindings/go-clipboard/APIService.js";
import * as ShareService from "/bindings/go-clipboard/ShareService.js";

function wrapService(svc) {
    const out = {};
    for (const [k, v] of Object.entries(svc)) {
        if (typeof v === "function") out[k] = (...args) => v(...args);
    }
    return out;
}

window.go = {
    main: {
        App:              wrapService(App),
        ClipboardService: wrapService(ClipboardService),
        PluginService:    wrapService(PluginService),
        TransferService:  wrapService(TransferService),
        ServeService:     wrapService(ServeService),
        APIService:       wrapService(APIService),
        ShareService:     wrapService(ShareService),
    },
};

window.runtime = {
    EventsOn: (name, cb) => wails.Events.On(name, (e) => {
        const d = e.data;
        if (Array.isArray(d)) cb(...d);
        else cb(d);
    }),
    EventsEmit: (name, ...args) => wails.Events.Emit({ name, data: args }),
    EventsOff: (name) => wails.Events.Off(name),
    OnFileDrop: (cb, useDropTarget) => wails.Window.OnDrop(cb, useDropTarget),
};
```

Load it first in `frontend/index.html`, before every other app script:

```html
<script type="module" src="/js/v3-bindings-shim.js"></script>
<script defer src="/js/utils.js"></script>
<script defer src="/js/wails-api.js"></script>
... rest of the script tags ...
```

Verify every event name, every service method, and the `--wails-draggable` /
`--wails-drop-target` CSS behaviour after the swap using the Phase 5 smoke
matrix below.

## 6. Build templates

v3 changes template variable names for some fields. Open `build/darwin/Info.plist`,
`build/windows/info.json`, and `build/windows/wails.exe.manifest` and diff
against the defaults from `wails3 init -t vanilla`. Merge the v3 template
variables into the existing customized values.

Specific things to watch for:
- New keys for WebView2 loader on Windows.
- macOS usage-description strings may have new IDs.
- Wails-generated icon paths may move.

## 7. `wails.json` → v3 config

At swap time, confirm whether v3 uses:
- The existing `wails.json` (likely deprecated by then),
- A new `Taskfile.yml` (roadmap-stated direction), or
- Something else.

Port each existing field:

```json
{
  "name": "mahpastes",
  "outputfilename": "mahpastes",
  "frontend:install": "npm install",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  ...
}
```

## 8. Verification — swap day

After everything compiles:

- [ ] `go build ./...`
- [ ] `GOOS=windows go build ./...`
- [ ] `GOOS=linux go vet ./...`
- [ ] `go test ./internal/wailsbridge/...` — including the import guard.
- [ ] Full e2e: `cd e2e && set -o pipefail && (npm test 2>&1) | tail -80`
- [ ] 14-row smoke matrix from `Task 10` of `docs/superpowers/plans/` or the
      original prep plan: exercise Add watch folder, Drag-in file, Watch
      error event, Duplicate event, Plugin modal, Plugin toast, Plugin task
      events, Save As, File Open, Plugin import, Backup create, Backup
      restore, Drag clip to Finder, Open With on macOS.

If any flow breaks, the shim is the usual suspect (a missing method name,
a payload-shape mismatch, or event-name drift).

## 9. Rollback

If v3 misbehaves after the swap:

1. `git restore .` before committing the v3 swap.
2. `go get github.com/wailsapp/wails/v2@v2.12.0` to re-pin.
3. `make bindings && make build`.
4. Every call site survives the rollback because the bridge abstraction is
   v2-or-v3 agnostic from the caller's perspective.
