# Wails v3 Frontend Contract

This document is the spec the eventual `frontend/js/v3-bindings-shim.js`
must implement on v3 swap day. Every row listed here is a surface the existing
JS files (under `frontend/js/`) consume today from Wails v2's `window.go.main.*`
and `window.runtime.*` globals. The shim's job is to reproduce these surfaces
on top of v3's `@wailsio/runtime` module and generated service bindings.

The migration playbook at `docs/WAILS_V3_MIGRATION.md` is the execution recipe.
This doc is the inventory.

## Constraints

- **No bundler.** The repo's vanilla-JS + Tailwind-only posture must be
  preserved. The shim loads via `<script type="module">` and consumes v3's
  framework-served `/wails/runtime.js` plus the generated service bindings
  (which v3 emits to `assets/bindings/` or equivalent).
- **Load order.** The shim MUST execute before any other `frontend/js/*.js`
  file, since those files assume `window.go.main.*` and `window.runtime.*` are
  populated. Place the `<script type="module" src="/js/v3-bindings-shim.js">`
  tag in `frontend/index.html` before every other app script.
- **Promise shape.** Every service method returns a `Promise`. Under v2, these
  are backed by Wails's CallByID/CallByName bridge; under v3, by
  `@wailsio/runtime`'s Call API. Preserve the promise contract exactly.

## 1. Bindings surface (window.go.main.*)

143 call sites across `frontend/js/`. Six services bound from Go.

### App (full bindings at `frontend/wailsjs/go/main/App.d.ts`)

62+ exported methods. Groups:

- **Clips**: `GetClips`, `GetClipsDirect`, `GetClipData`, `GetFolderClips`,
  `GetUntaggedClips`, `GetClipMetadata`, `SetClipMetadata`, `SetBulkClipMetadata`,
  `DeleteClipMetadata`, `FindClipsByFilenameAndTag`, `RenameClip`, `DeleteClip`,
  `BulkDelete`, `BulkArchive`, `BulkUnarchive`, `ToggleArchiveClip`,
  `SaveClipToFile`, `BulkDownloadToFile`, `UploadFiles`, `UploadFileAndGetID`,
  `SetExpiration`, `CancelExpiration`, `BulkSetExpiration`, `BulkCancelExpiration`
- **Tags**: `GetTags`, `GetTopLevelTags`, `GetChildTags`, `GetClipTags`,
  `CreateTag`, `UpdateTag`, `DeleteTag`, `AddTagToClip`, `RemoveTagFromClip`,
  `BulkAddTag`, `BulkRemoveTag`, `GetHiddenTags`, `SetHiddenTags`,
  `GetDescendantClipCount`, `GetRemovableEmptyTags`, `RemoveEmptyTags`
- **Watch**: `GetWatchedFolders`, `GetWatchedFolderByID`, `AddWatchedFolder`,
  `RemoveWatchedFolder`, `UpdateWatchedFolder`, `RefreshWatches`,
  `GetWatchStatus`, `GetGlobalWatchPaused`, `SetGlobalWatchPaused`,
  `ProcessExistingFilesInFolder`, `SelectFolder`
- **Backup**: `CreateBackup`, `BackupInspect`, `ShowRestoreBackupDialog`,
  `ConfirmRestoreBackup`, `RestoreBackup`
- **Dedup**: `GetDuplicateGroups`, `MergeDuplicates`, `DeduplicateAll`
- **Settings**: `GetSetting`, `SetSetting`
- **Dialogs / File I/O**: `ChooseApplication`, `OpenClipWithApp`,
  `OpenClipWithDefaultApp`, `ReadFileFromPath`, `IsDirectory`,
  `CreateTempFile`, `DeleteAllTempFiles`
- **Clipboard helpers**: `GetClipboardText`, `GetClipboardImage`,
  `CopyToClipboard`
- **Images**: `GetImageDiff`
- **Lifecycle**: `RefreshWatches`

### ClipboardService

- `CopyFileToClipboard(id) → void`
- `CopyClipContents(id) → void`
- `BulkCopyFilesToClipboard(ids) → void`

### PluginService

- Lifecycle: `GetPlugins`, `EnablePlugin`, `DisablePlugin`, `RemovePlugin`,
  `ImportPlugin`, `ImportPluginFromPath`, `PreviewPluginFromPath`,
  `PreviewPluginFromURL`, `ConfirmPluginInstall`, `UpdatePlugin`,
  `ConfirmPluginUpdate`, `CheckForUpdates`, `GetUpdateCheckInterval`,
  `SetUpdateCheckInterval`
- Actions: `GetPluginUIActions`, `ExecutePluginAction`, `TryAcquireModalGuard`
- Permissions: `GetPluginPermissions`, `RevokePluginPermission`,
  `IsPluginURLAllowed`
- Storage: `GetPluginStorage`, `SetPluginStorage`, `GetAllPluginStorage`

### TransferService

- `GetTransferCapabilities() → TransferCapabilities`
- `PrepareClipForTransfer(req) → PreparedTransferItem`
- `GetExistingPreparedClipForTransfer(req) → PreparedTransferItem`
- `StartNativeDragOut(req) → boolean`

### ServeService

- `GetServeStatus() → ServeInfo[]`
- `StartServing(tagID, port, bindAll, apiAccess) → ServeInfo`
- `StopServing(tagID) → void`
- `GetRandomPort() → number`

### APIService

- `GetAPIStatus() → APIStatus`
- `StartAPI(port, bindAll) → APIStatus`
- `StopAPI() → void`
- `CreateAPIKey(name, role, daysTTL) → APIKeyCreateResult`
- `ListAPIKeys() → APIKeyInfo[]`
- `RevokeAPIKey(id) → void`

### ShareService

- `StartShare(tagID) → ShareInfo`
- `StopShare(tagID) → void`
- `GetShareStatus() → ShareStatus`
- `Follow(shareString, localTag) → FollowInfo`
- `FollowWithoutDial(shareString, localTag) → FollowInfo`
- `TestFollowConnection(shareString) → void`
- `Unfollow(id) → void`
- `UpdateFollowTag(id, tag) → FollowInfo`
- Test hooks: `AgeShareRingForTest`, `DisconnectFollowForTest`

**Shim pattern per service**: iterate the v3-generated binding module, copy
each exported function onto `window.go.main.<Service>`, preserving the promise
return shape. Roughly:

```js
import * as App from "/bindings/go-clipboard/App.js";
function wrapService(svc) {
    const out = {};
    for (const [k, v] of Object.entries(svc)) {
        if (typeof v === "function") out[k] = (...args) => v(...args);
    }
    return out;
}
window.go = { main: { App: wrapService(App), /* ... */ } };
```

## 2. Runtime surface (window.runtime.*)

21 call sites across `frontend/js/`. The v2 runtime exposes these as globals;
v3 moves them to the `@wailsio/runtime` module. The shim must proxy.

| Function | Call sites | v3 equivalent (approximate) |
|---|---|---|
| `EventsOn(name, cb)` | watch.js:464,468 / task-queue.js:21-24 / plugins.js:910 / app.js:1493,1499 / share.js:536-546 / modal-renderer.js:261 | `wails.Events.On(name, e => cb(...normalize(e.data)))` |
| `EventsEmit(name, ...args)` | modal-renderer.js:125,150 | `wails.Events.Emit({ name, data: args })` |
| `EventsOff(name)` | (not currently used, but reserve for shim API parity) | `wails.Events.Off(name)` |
| `OnFileDrop(cb, useDropTarget?)` | watch.js:445 | `wails.Window.OnDrop(cb)` (verify v3 API surface at swap time) |

### Event-payload normalization

v2's `EventsOn` callback receives `(...args)` positional, where args are the
data passed to `EventsEmit`. v3's `Events.On` receives a single `CustomEvent`
object with `.name` and `.data`. The shim MUST normalize:

```js
EventsOn: (name, cb) => wails.Events.On(name, (e) => {
    // v2 callback signature: (...args) where args are the emit data
    const d = e.data;
    if (Array.isArray(d)) cb(...d);
    else cb(d);
}),
```

Double-check each existing subscriber at the call sites above to confirm
whether it expects a single arg or spread args. The normalize logic must
handle both cleanly.

## 3. Event name catalog

Every event name crossed between Go (emitter via `a.bridge.Emit`) and JS
(subscriber via `window.runtime.EventsOn`). The shim doesn't care about
names — it's transparent — but this catalog exists to cross-check that
nothing silently goes missing.

| Event | Emitted by (Go) | Consumed by (JS) |
|---|---|---|
| `watch:error` | `app.go` (emitWatchError) | `watch.js:464` |
| `watch:import` | `app.go` (emitWatchImport) | `watch.js:468` |
| `clip:duplicate` | `app.go:783,857` | `app.js:1499` |
| `plugin:modal` | `plugin/api_modal.go`, `plugin/manager.go` | `modal-renderer.js:261` |
| `plugin:modal:acked` | (emitted by JS via `EventsEmit`) | `plugin/api_modal.go` (modalGuard.On) |
| `plugin:modal:closed` | (emitted by JS via `EventsEmit`) | `plugin/api_modal.go` (modalGuard.On) |
| `plugin:toast` | `plugin/api_toast.go`, `plugin/manager.go` | `app.js:1493` |
| `plugin:task:started` | `plugin/api_task.go` | `task-queue.js:21` |
| `plugin:task:progress` | `plugin/api_task.go` | `task-queue.js:22` |
| `plugin:task:completed` | `plugin/api_task.go` | `task-queue.js:23` |
| `plugin:task:failed` | `plugin/api_task.go` | `task-queue.js:24` |
| `plugin:update_available` | `plugin/update_checker.go` | `plugins.js:910` |
| `share:publication-updated` | `share_manager.go` → `sm.eventFn` → `a.bridge.Emit` | `share.js:536` |
| `share:publication-removed` | same | `share.js:537` |
| `share:follow-updated` | same | `share.js:538` |
| `share:follow-removed` | same | `share.js:539` |
| `share:clip-received` | same | `share.js:546` |

`plugin:modal:acked` and `plugin:modal:closed` are the only events emitted
from JS to Go — the shim's `EventsEmit` proxy must handle the direction too.

## 4. CSS contract

v3 keeps the `--wails-draggable` convention for window-chrome drag regions.
The drop-target plumbing changed slightly, but the CSS-variable interface
stays the same.

### `--wails-draggable`

Approximately 15 occurrences in `frontend/index.html`. Regions that should
be grabbable to drag the window (the custom titlebar). Example:

```html
<header style="--wails-draggable: drag">...</header>
<button style="--wails-draggable: no-drag">...</button>
```

Preserve verbatim under v3.

### `--wails-drop-target`

Single occurrence at `frontend/index.html:291` on the add-folder drop zone.
The shim must ensure v3's drop-target subsystem honors this attribute the
same way v2 does — that is, a descendant marked `--wails-drop-target: drop`
becomes a preferred drop destination and receives file-drop events.

If v3 changes the attribute name, the shim MUST either:
- Expose a drop-target API matching v2's contract, or
- Set up a MutationObserver that translates the old attribute to the new one
  at DOM-insert time.

## 5. Asset and HTTP handler contract

`transfer_handler.go` implements `http.Handler` and is currently passed via
`options.App.AssetServer.Handler`. It serves drag-out token-signed file
downloads and participates in the native-drag flow (`temp_clip_store.go`,
`transfer_types.go`).

v3's `application.AssetOptions{Handler: http.Handler}` accepts the same
interface. The migration playbook covers the config change; no shim work is
needed for this surface.

## 6. What the shim is NOT responsible for

- Generating TypeScript types. The v3 binding generator handles this.
- Promise polyfill. Modern browsers handle it.
- Error shaping. v3's errors are expected to shape compatibly with v2's.
- Menu/tray APIs. We don't use them.
- Logging runtime (`runtime.LogDebug` etc.). We don't use them.
