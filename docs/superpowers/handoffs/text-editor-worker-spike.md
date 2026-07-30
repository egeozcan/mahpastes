# Text-editor validator worker: spike results and native smoke procedure

Milestone 1 of `docs/superpowers/specs/2026-07-29-structured-text-preview-editor-design.md`.

The spike answers one question per surface: **can a committed static worker load,
exchange a request, blow a deadline, be terminated, and restart?** A surface that
fails switches to the bounded time-sliced main-thread executor. It does not stop
the release, and it does not permit unbounded synchronous parsing on the UI
thread in any configuration.

## Automated results

| Surface | Origin / engine | Worker? | Evidence |
|---|---|---|---|
| `wails dev` (desktop e2e) | Chromium over `http://localhost:3411x` | **yes** | `e2e/tests/clips/text-editor-contracts.spec.ts` — probe, handshake, deadline, terminate, restart, generation guards |
| Served web mode (`mahpastesd`) | Chromium over `http://127.0.0.1:<port>`, embedded FS via `spaHandler`, real `default-src 'self'` CSP | **yes** | `e2e/tests/server/text-editor-worker.spec.ts` — probe under the shipped CSP, no CSP console violations, worker served as `text/javascript` without a session |

Both automated surfaces are Chromium. **Neither is the engine that ships on
desktop.** Playwright cannot reach WKWebView, WebView2, or WebKitGTK, and no
amount of green output above substitutes for the manual runs below.

## Native smoke procedure (manual, per platform)

1. Build production: `make build` (macOS/Linux) or `wails build` (Windows). Add
   `-debug` so the platform inspector is reachable:
   `~/go/bin/wails build -debug`.
2. Launch the app and open the inspector
   (macOS: right-click → Inspect Element; Windows WebView2: F12; Linux
   WebKitGTK: right-click → Inspect Element, requires
   `WEBKIT_INSPECTOR_SERVER` or a debug build).
3. Run each step in the console and record the output:

```js
// 1. Probe: does this surface get a worker at all?
const api = MahpastesTextEditor;
const { executor, probe } = await api.probeWorkerSupport();
probe;                                  // { kind: 'worker' | 'fallback', reason, workerAvailable }

// 2. One round-trip.
await executor.run({ op: api.OP_HANDSHAKE, generation: 1 });
// expect { handshake: 'mahpastes:text-validator:v1', protocol: 1 }

// 3. Forced deadline -> termination.
await executor.run({ op: api.OP_SELFTEST, generation: 2,
                     payload: { units: 1, spinMsPerUnit: 800 }, timeoutMs: 120 })
  .catch(e => e.code);                   // expect 'timeout'
executor.alive;                          // expect false
executor.restartCount;                   // expect 1

// 4. Lazy restart.
await executor.run({ op: api.OP_HANDSHAKE, generation: 3 });
executor.alive;                          // expect true
executor.dispose();
```

4. If step 1 reports `fallback`, record the `reason` verbatim — that string is the
   only evidence of *why* a surface degraded — and confirm the fallback path is
   sound on that surface:

```js
const fb = await api.createExecutor({ forceFallback: true });
await fb.run({ op: api.OP_HANDSHAKE, generation: 1 });
fb.limits;                               // expect { deadlineMs: 250, maxSourceBytes: 65536 }
await fb.run({ op: api.OP_SELFTEST, generation: 2,
               payload: { units: 20, spinMsPerUnit: 30 } })
  .catch(e => e.code);                   // expect 'timeout'
fb.abandonCount;                         // expect 1
fb.dispose();
```

`OP_SELFTEST` exists for exactly this: it is the only way to exercise a deadline,
a termination, and a restart on a real device before any parser is bundled. It
performs no I/O and touches no clip data.

## Native results

Record one row per platform. Unfilled rows are unverified, not passing.

| Platform | OS version | Engine | Probe result | Round-trip | Timeout/terminate/restart | Date | Notes |
|---|---|---|---|---|---|---|---|
| macOS | 15.6.1 | WKWebView | `worker` | pass | pass | 2026-07-30 | Production `wails build`, origin `wails://wails`. All five steps pass. |
| Windows | | WebView2 | | | | | Runs in CI — see below. |
| Linux | | WebKitGTK | | | | | Runs in CI — see below. |

### How this is now run

Manual steps are still valid, but the check is automated. The app takes one opt-in
environment variable, `MAHPASTES_SELFTEST_WORKER=1`, which starts hidden, exercises
probe → handshake → forced timeout → restart → bounded work, prints JSON on stdout
and exits non-zero on failure:

```bash
wails build
MAHPASTES_SELFTEST_WORKER=1 MAHPASTES_DATA_DIR=$(mktemp -d) \
  ./build/bin/mahpastes.app/Contents/MacOS/mahpastes
```

An environment variable rather than a CLI flag because Wails v2 parses `os.Args`
itself, and an unrecognized flag drops it into dev-mode argument handling and fails
with an unrelated "unable to infer the AssetDir" error. `SelfTestService` is bound
only in this mode, and the frontend detects the mode by that binding's *presence*,
so a normal run gains no method and runs no extra code.

`.github/workflows/worker-smoke.yml` runs it on `macos-latest`, `windows-latest` and
`ubuntu-latest` (under `xvfb`), uploading each `result.json`. It is
`continue-on-error` on purpose: a surface that cannot construct a worker is not a
broken build — the capability probe falls back to the bounded time-sliced executor by
design — but it does mean that platform pays the fallback's lower 64 KiB validation
ceiling, which is worth knowing rather than discovering from a user report.

The macOS row above was filled from a local production build, so it is direct
evidence rather than a CI inference. Windows and Linux stay unfilled until that
workflow has actually run on a push; copy the values from the uploaded artifacts.

The verified macOS payload, for comparison:

```json
{
  "ok": true,
  "probe": { "kind": "worker", "reason": null, "workerAvailable": true },
  "steps": [
    { "name": "probe", "ok": true },
    { "name": "handshake", "ok": true },
    { "name": "timeout-terminates", "ok": true },
    { "name": "restart", "ok": true },
    { "name": "bounded-work", "ok": true }
  ],
  "environment": { "origin": "wails://wails" }
}
```

## Bundle size against the budget

Recorded per milestone so a later dependency bump has something to compare
against. Budget is 1,500,000 bytes combined, enforced by
`check:text-editor-bundle`.

| After milestone | `text-editor.bundle.js` | `text-validator.worker.js` | Combined | % of budget |
|---|---|---|---|---|
| 1 (executor only) | 7,020 | 1,424 | 8,444 | 1% |
| 2 (+ classification, codec) | 14,959 | 1,424 | 16,383 | 1% |
| 3 (+ CodeMirror core, search, commands, language) | 325,041 | 1,424 | 326,465 | 22% |

The remaining ~1.17 MB has to cover six language packages and four parsers.

## Notes on CSP

`spaCSP` in `internal/app/api_manager.go` already began `default-src 'self'`, from
which `worker-src` inherited. An explicit `worker-src 'self'` was added as
documentation; it is not what makes the worker load, and a spike failure must not
be attributed to it. No `blob:` allowance is wanted or present — the worker is
never constructed from `blob:` or `data:`.

## Known spec discrepancy

The spec states the fallback byte ceiling twice and disagrees with itself:
[Fallback when the worker is unavailable] says **64 KiB**, with the rationale that
a worst-case single synchronous parse must stay in the low tens of milliseconds;
[Worker, limits, and accessibility] under Test strategy says **256 KiB**. The
implementation uses **64 KiB** — the normative section, and the one whose number
is derived from an argument rather than asserted. If 256 KiB is wanted, the
rationale in the normative section needs revisiting first.
