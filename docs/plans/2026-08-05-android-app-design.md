# Android App — Design Document

**Date:** 2026-08-05
**Status:** Draft

## Problem

mahpastes runs on macOS, Windows, and Linux. Content captured on a phone —
screenshots, photos, shared links, selected text — has no path into the clip
store except manually re-uploading it later from a desktop. That is the single
largest hole in the product: the device that generates the most ephemeral
visual content is the one device with no client.

The naive fix is a thin REST client pointed at a server. That is not what this
document describes. Investigation showed the entire Go core already
cross-compiles for `android/arm64` with one contained exception, which makes a
genuinely standalone peer — local SQLite, local plugin runtime, P2P sync —
achievable at roughly the cost of the thin client.

## Goals

1. **Capture-led** — a first-class `ACTION_SEND` share-sheet target, reachable
   from any app in two taps
2. **Standalone peer** — the phone owns a local database and syncs P2P, exactly
   as a desktop instance does; it is not a remote control for a server
3. **Browse and retrieve** — gallery, tags, search, and copy-back to the Android
   clipboard
4. **Plugins run on-device** — network-class plugins work, `fal-ai.lua` in
   particular
5. **Sync that survives a phone** — no silent clip loss after being offline,
   which today's protocol cannot promise
6. **Reuse the frontend** — the plugin UI surface is not reimplemented

## Non-Goals

- Play Store distribution in the first iteration (sideloaded; Play is a later,
  separate effort)
- Image editor, comparison view, watch folders, drag-out transfer, tag serve
- Filesystem-class plugins (`fs` API) and the plugin scheduler
- Background clipboard monitoring — **not possible**; see Platform Constraints
- Renames, metadata, and tag moves converging over sync (desktop-authoritative)
- iOS

## Platform constraints that shape everything

**Android does not permit background clipboard reads.** Since Android 10,
`ClipboardManager.getPrimaryClip` returns data only to the foreground app or the
active IME. The desktop premise — watch the clipboard, hoard everything — is not
portable. All capture on Android is push-based and user-initiated. Clipboard
*writes* are unrestricted, which is the direction retrieval needs.

**The phone is never dialable.** Carrier NAT, Doze, and a short-lived foreground
service mean inbound connections do not arrive. Any sync design in which a peer
must dial the phone does not work in practice.

**Loopback has no per-app isolation.** Any installed app can connect to
`127.0.0.1` on any port. An unauthenticated local API is a full clip-history
disclosure to every app on the device.

**Scoped storage.** App-private `filesDir` needs no permission and is the only
sane home for the database. Real filesystem paths, which `plugin/api_fs.go`
assumes, do not exist.

## Findings from the existing codebase

These were verified, not assumed.

### The core already builds for Android

`GOOS=android GOARCH=arm64 CGO_ENABLED=0` compiles cleanly for
`github.com/libp2p/go-libp2p`, `go-libp2p-kad-dht`, `modernc.org/sqlite`,
`gopher-lua`, and `golang.org/x/image/draw`.

The **only** failure is `golang.design/x/clipboard`, which has no Android
backend. It is imported in exactly two files that are part of the shared core:

- `internal/app/app.go:31`
- `plugin/api_utils.go:14`

(The other importers — `clipboard_service.go`, `clipboard_darwin.go`,
`desktop_app.go` — are already desktop-only and out of the Android build.)

### `getDataDir` needs an explicit path on Android

`internal/app/database.go:23` switches on `runtime.GOOS`; `android` falls into
the `default:` branch and calls `os.UserHomeDir()`, which yields nothing usable.
A `MAHPASTES_DATA_DIR` override already short-circuits the whole function
(`database.go:25`), and the shim below passes the path explicitly.

### Clip content is a SQLite BLOB

`clips.data BLOB NOT NULL` (`internal/app/database.go:88`). There is no separate
blob store, so a fully-synced image tag grows the phone's app-private database
without bound, and WAL-mode SQLite does not return space on delete. This drives
the caching policy below.

### Sync is libp2p, and the follower dials

`/mahpastes/share/1.0.0` over libp2p with DHT + mDNS + circuit-relay-v2,
1 MiB AES-GCM chunks (`internal/app/share_types.go`).

`followSession` calls `dialByPeerID` then `host.NewStream`
(`share_manager.go:1264-1282`). The publisher only registers
`SetStreamHandler` (`share_manager.go:179`) and waits. **Inbound reachability is
the publisher's burden** — which is exactly what a phone cannot provide.

### Sync loses data silently, today

The publisher's ring keeps `RingTTLSeconds = 3600` / `RingBytesCapPerPub =
256 MiB`. A follower that reconnects past that window receives a `KindGap`, and
the handler at `share_manager.go:1388` does one thing:

```go
case KindGap:
    // ...
    inSeq = p.Seq
```

It advances the cursor. There is no backfill. Missed clips are gone from that
follower permanently. On a desktop this is rare; on a phone asleep overnight it
is the default case. **This is a pre-existing protocol bug that this project
must fix, not an Android concern.**

Compounding it: `share_ring` stores full ciphertext envelopes keyed
`(publication_id, seq)` and there is **no `seq → clip_id` mapping anywhere**, so
"just replay from the database" is not a small patch — the mapping does not
exist.

### The digest primitive already exists

`clips.content_hash TEXT DEFAULT ''` with `idx_clips_content_hash`
(`database.go:105-106`), added for dedup. Reconciliation reuses it rather than
defining a second identity function.

### The share string is QR-sized and is a credential

`mp-share:v1:` + base64url(CBOR{PeerID 32B, Key 32B}) ≈ 100 characters
(`share_codec.go:31-54`). It carries the symmetric key.

### The shared frontend is desktop-only in practice

Correct `<meta viewport>`, but 7 total Tailwind breakpoint prefixes in
`index.html` and one `@media (max-width: 720px)` block in `modals.css`. It is
keyboard-first, drawer-and-modal, hover-driven. It renders on a phone; it is not
usable on one.

## Architecture

### Shape

```
┌─────────────────────────────────────────────┐
│ Android app process                         │
│                                             │
│  ┌───────────────┐      ┌────────────────┐  │
│  │ Kotlin shell  │      │ WebView        │  │
│  │ - share sheet │      │ - gallery/tags │  │
│  │ - pairing/QR  │      │ - plugin UI    │  │
│  │ - settings    │      │ - modals/tasks │  │
│  │ - clipboard   │      └────────┬───────┘  │
│  │ - FGS control │               │          │
│  └───────┬───────┘               │ HTTP     │
│          │ gomobile              │          │
│          ▼                       ▼          │
│  ┌─────────────────────────────────────┐    │
│  │ mobilecore (AAR)                    │    │
│  │   Start(dataDir) → {port, token}    │    │
│  │   ┌───────────────────────────────┐ │    │
│  │   │ internal/app  (the same core) │ │    │
│  │   │  APIManager · SQLite · libp2p │ │    │
│  │   │  plugin runtime               │ │    │
│  │   └───────────────────────────────┘ │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘
```

### Why gomobile-a-thin-shim, not gomobile-bind-everything

`gomobile bind` supports a restricted type set — no maps, no slices of structs,
no variadics. Binding the `App` surface (~100 methods, many returning
`[]Clip`) would require a hand-written JSON DTO layer over nearly all of it. At
that point, loopback HTTP is the *same* shim with better tooling, an already-
proven client shape (`cmd/mp`), and it keeps the WebView option open.

Shipping `mahpastesd` in `jniLibs` and exec'ing it was rejected: Android's W^X
policy makes exec fragile and Play-hostile, and orphan process lifetime is not
manageable from an Activity.

### The `mobilecore` package

New package, gomobile-bound, exposing the minimum surface:

```go
package mobilecore

// Start boots the core against an app-private data directory and returns the
// loopback endpoint plus a per-process bearer token.
func Start(dataDir string) (*Endpoint, error)

// Stop shuts the core down cleanly (flush WAL, close libp2p host).
func Stop() error

// SyncNow triggers one reconciliation pass across all paired tags.
func SyncNow() error

type Endpoint struct {
    Port  int
    Token string
}
```

Everything else the shell needs goes over HTTP to `127.0.0.1:Port`.

### Clipboard: unblock, then bridge

**Phase 1 — unblock the build.** Introduce `internal/clipboardbridge` mirroring
the existing `internal/wailsbridge` pattern, and route the two shared-core
imports through it. Android gets a build-tagged stub returning
`ErrUnsupported`; `plugin/api_utils.go`'s `clipboard_write` surfaces that as a
Lua error.

**Phase 2 — make it real.** An Android implementation calling Kotlin's
`ClipboardManager` through a gomobile reverse-binding callback. Writes only.
This is not optional — "copy this clip's text to my clipboard" is the highest-
value retrieval action on a phone — it is only *sequenced* after phase 1.

The existing `TestNoRuntimeImportOutsideBridge` invariant has a direct analogue
here: nothing outside `clipboardbridge` should import `golang.design/x/clipboard`.
Add the matching test.

### Loopback security

- Bind `127.0.0.1` only, never `0.0.0.0`
- Ephemeral high port, chosen at `Start()`
- Random bearer token minted per process start, returned from `Start()`
- Native calls send `Authorization: Bearer <token>`
- WebView gets an equivalent session cookie seeded via `CookieManager` **before**
  the first load

A Unix domain socket would be strictly safer and unreachable by other apps, but
WebView cannot speak UDS, and the hybrid UI decision depends on it.

## Sync: reconciliation

This is the largest piece of new backend work, and most of it lands in the
shared core rather than in Android-specific code.

### Bidirectional over the phone-initiated session

The phone dials; that one connection reconciles the paired tag in **both**
directions. This is NAT-proof by construction — the only connection that must
succeed is the only one the phone can reliably open.

This changes the conceptual model from publish/follow to a **paired tag**, which
is a `CONTEXT.md` vocabulary change as much as a code one. Existing one-way
publications keep working; pairing is the new bidirectional mode.

### Digest

Each session, each side sends the set of 8-byte truncated `content_hash`
prefixes for the tag. ~40 KB for 5,000 clips — cheap enough that a Merkle or
ranged digest is unjustified complexity. Reuses `idx_clips_content_hash`.

A watermark-plus-tail scheme was rejected: it reintroduces exactly the
"trust the sequence number" assumption that caused the original silent-loss bug.

### Metadata first, blobs on demand

Reconciliation exchanges clip *existence* (hash, filename, content type, size)
always. Blob bytes transfer on demand:

- On open, or on explicit pin
- Cached under a user-visible byte cap, LRU eviction

Because clips are BLOBs in the shared database, full sync would put a desktop's
entire image history into one app-private SQLite file. Browsing old images
consequently requires connectivity — an accepted trade.

### Tombstones, and eviction is not deletion

Deletes propagate via a tombstone table with GC. Critically, an **evicted blob
must reconcile as present** — the clip row survives with its hash and a
`blob_evicted` flag. Without this, the cache cap and bidirectional
reconciliation actively fight: dropping bytes on the phone to save space would
read as a delete and destroy the desktop copy.

Additions-only was rejected for the mirror-image reason: without tombstones, a
clip deleted on the phone is re-offered and silently resurrected on the next
sync.

Renames, metadata, and tag moves stay desktop-authoritative. They are
last-writer-wins at best, and tag moves collide with tree exclusivity in ways
not worth designing speculatively.

### Pairing

Desktop renders its `mp-share:v1:` string as a QR code; the phone scans it.
Manual paste remains as a fallback for when the desktop is not physically
present.

ZXing rather than ML Kit — smaller, no Play Services dependency. Camera
permission is requested **at the pairing screen only**, never at launch.

Because the string carries the symkey, QR is meaningfully safer than routing it
through a messaging app or a shared clipboard. Treat it as a credential in all
UI copy.

## Android shell

### Capture flow

`ACTION_SEND` / `ACTION_SEND_MULTIPLE` for `image/*`, `text/*`, and
`application/*`.

Receipt opens a **quick sheet**: tag picker pre-filled with the last-used tag,
optional rename, one-tap save. Not fire-and-forget — tree exclusivity means one
tag per root, so getting it right at capture is worth a single tap, and a
mis-tagged clip is one you will never go back and fix. Not the full editor
either; that defeats the point of a share sheet.

### Process lifetime

A **short-lived `dataSync` foreground service**, started for the duration of a
capture upload or an open sync session and stopped afterward.

Activity-scoped lifetime alone would drop a large screenshot upload the instant
you swipe away — which is precisely when you swipe away. A persistent service
was rejected on battery and notification grounds.

Android 14+ requires a declared foreground-service type and a matching runtime
justification.

### Sync scheduling

Foreground-only. No `WorkManager` catch-up windows.

This is safe *only because* reconciliation makes offline duration irrelevant.
Under the old ring-based protocol, sporadic sync meant silent data loss, and no
scheduling policy could have fixed it.

## Frontend

### Hybrid, and why

Native shell for: share-sheet receipt, service control, pairing/QR, settings,
clipboard writes. WebView for: gallery, tags, and the entire plugin surface.

`fal-ai.lua` alone needs a password settings field, per-action option forms
(select / range / checkbox), async task progress, and result modals. Every one
of those already exists as `frontend/js/modal-renderer.js`, `task-queue.js`,
`plugins.js`, and `plugin-review.js`. Rebuilding that in Compose is the single
largest cost available in this project and buys nothing.

Conversely the share sheet *must* be native — a WebView cannot be an
`ACTION_SEND` target.

### Responsive scope

Phone-scoped overrides behind a `max-width` breakpoint. The desktop DOM and
behavior stay byte-identical.

`CLAUDE.md` requires the full e2e suite green, and a broad responsive refactor
would put every existing Playwright selector and hover interaction at risk in
one change. Breakpoint-scoped overrides keep the current suite meaningful while
mobile-viewport tests are added alongside it. Expect drift toward a genuine
responsive pass over time; that is fine.

Real work regardless: drawer, modals, context menus, and hover-only affordances.
The upside is that it also fixes the deployed web UI on mobile, which today is
unusable.

## Plugins

Network-class only: `http`, `image`, `task`, `toast`, `modal`, `storage`,
`metadata`, `clips`, `tags`, `utils`, `json`, `base64`.

Stubbed to a denied-permission error: `fs`. Scoped storage has no clean analogue
for `api_fs.go`'s path-based permission prompts, and half-implementing it is
worse than refusing it.

Not included: the plugin scheduler. Recurring tasks on Android means
`WorkManager` and a full Doze-mode conversation, which is out of scope here.

`fal-ai.lua` needs `http` + `image` + `task` + settings + option forms + async,
and no `fs` — it works under this set.

## Project layout

Same repository. The frontend is shared, the gomobile shim is part of this Go
module, and splitting would mean version-locking two halves of one binary.

```
mahpastes/
├── android/                    # NEW
│   ├── app/                    #   Kotlin shell (share sheet, pairing, settings)
│   └── mobilecore.aar          #   built by gomobile, gitignored
├── internal/
│   ├── clipboardbridge/        # NEW: clipboard abstraction + platform impls
│   └── app/
│       ├── share_reconcile.go  # NEW: digest exchange, tombstones, blob fetch
│       └── share_manager.go    #   bidirectional session
└── frontend/css/mobile.css     # NEW: breakpoint-scoped overrides
```

- **minSdk 29** (Android 10) — scoped storage is unconditional there, so the
  legacy-storage branch never gets written, and it is comfortably above
  gomobile's floor
- **targetSdk** at whatever Play currently requires; needed for the `dataSync`
  foreground-service type regardless of distribution channel

## Testing

Go-level tests for the shim and all reconciliation logic, extending the
established pattern: `share_manager_test.go`, `share_protocol_test.go`,
`share_ring_test.go`, `share_codec_test.go`.

Playwright at mobile viewport for the WebView half, as a new project in the
existing suite.

Native shell: manual.

Instrumented Compose/Espresso in CI was rejected — it would spend most of its
runtime and flakiness budget on glue code, which is the part you notice broken
within seconds of opening the app.

Reconciliation cases that must be covered:

- Follower offline beyond ring TTL → no clip loss
- Delete on phone → propagates, does not resurrect
- Blob evicted on phone → reconciles as present, does not propagate as delete
- Fresh pair → full backfill
- Both sides add concurrently → both converge
- Tombstone GC does not resurrect

## Implementation order

1. `internal/clipboardbridge` + Android stub; `android/arm64` builds green
2. Invariant test: no `golang.design/x/clipboard` import outside the bridge
3. `mobilecore` package + `gomobile bind` producing an AAR
4. Kotlin shell: launch, `Start()`, WebView on loopback with cookie auth
5. `frontend/css/mobile.css` — gallery and tags usable on a phone
6. Share-sheet target + quick sheet + `dataSync` foreground service
7. Clipboard bridge phase 2 (Android write implementation)
8. **Reconciliation in the shared core** — digest, tombstones, blob-on-demand
   *(desktop-to-desktop first; testable without any Android involvement)*
9. Bidirectional session over the phone-initiated connection
10. Pairing UI + QR (ZXing)
11. Blob cache cap + LRU eviction + evicted-not-deleted reconcile state
12. Plugin surface in WebView; verify `fal-ai.lua` end to end

Steps 1–7 produce a useful capture-and-browse app on their own. Step 8 is the
riskiest work and is deliberately isolated so it can be built and tested between
two desktops before a phone is involved.

## Open items

Deliberately unresolved — they fall out of implementation rather than forking
the design:

- Default blob cache cap
- Tombstone GC retention window
- Notification copy and app icon
- Whether `SyncNow()` needs a pull-to-refresh affordance in the WebView or stays
  a shell-level action
