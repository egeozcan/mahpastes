# E2E Suite Speedup — Implementation Plan

**Date:** 2026-07-28
**Status:** Implemented — see "Outcome" at the end for what changed versus this plan.

**Goal:** Cut `npm test` wall-clock time from ~27 min to ~8 min without reducing
coverage, weakening assertions, or introducing flake.

**Headline finding:** 77% of all test execution time is a single frontend retry
loop sleeping. It is not the tests, not the app, and not parallelism.

---

## Evidence

All numbers below are measured, not estimated. Two sources:

1. The committed Playwright HTML report from the 2026-07-24 run
   (`e2e/playwright-report/index.html`), whose embedded payload contains
   per-test **step-level** timings for all 975 tests.
2. Direct latency probes against a live `wails dev` instance on port 34199.

### Where the time goes

Main suite: 975 tests, **19.1 min wall** at 4 workers, **70.2 min summed**
test time.

Aggregating every test's step tree:

| Phase | Total time | Share |
|---|---|---|
| `Before Hooks` | 34.6 min | 49% |
| `After Hooks` | 30.1 min | 43% |
| **Actual test bodies** | **5.7 min** | **8%** |

The test bodies are already fast. The fixtures are the suite.

Drilling into the hooks, the `app` fixture calls `AppHelper.fastReset()` both
before and after each test — 1724 calls across the run:

```
fastReset evaluate: 1724 calls, 54.1 min total
median 1872ms   p90 1882ms   max 3400ms
```

A median of 1872ms with a p90 of 1882ms is a **fixed** cost. It does not scale
with how many clips or tags the test created, which rules out the database
cleanup being responsible.

### Root cause

Probing each individual operation inside `fastReset` against a live instance:

```
     1.4ms  App.GetClips
     0.8ms  App.GetTags
     0.6ms  PS.GetPlugins
     0.7ms  App.GetWatchedFolders
     0.3ms  App.SetHiddenTags([])
     0.5ms  App.SetSetting
     1.4ms  loadClips()
  1857.4ms  loadPluginUIActions()      <-- here
```

`frontend/js/ui.js:56` — `loadPluginUIActions()` retries with a fixed backoff
and only breaks early when it finds at least one action:

```js
const retryDelays = [0, 100, 250, 500, 1000];
for (let attempt = 0; attempt < retryDelays.length; attempt++) {
    if (retryDelays[attempt] > 0) {
        await new Promise(resolve => setTimeout(resolve, retryDelays[attempt]));
    }
    ...
    const actions = await window.go.main.PluginService.GetPluginUIActions();
    ...
    if (actionCount > 0 || attempt === retryDelays.length - 1) break;
}
```

`0 + 100 + 250 + 500 + 1000 = 1850ms` — matching the measured 1857ms and the
fixture's 1872ms median exactly.

The loop exists for a real reason: on cold startup the frontend can finish
loading before the plugin manager has registered its actions, so it waits for
them to appear. But `fastReset` **removes every plugin first**, then calls it.
The zero-action result is the correct, expected steady state — so every single
call exhausts the full retry budget before giving up.

The underlying binding is effectively free:

```
   0.3ms  GetPluginUIActions (raw)
1858.4ms  loadPluginUIActions() (retry loop)
```

**1724 calls × 1.85s = 53 minutes of the 70-minute suite spent in `setTimeout`.**

### Secondary cost: instance startup

`global-setup.ts` spawns 4 `wails dev` instances sequentially with a 500ms
gap. A warm spawn (Go build cache primed) measured **22s**, so global setup is
~90s. That is 8% of the current run but becomes ~25% once the retry loop is
fixed.

### The share suite

Separate config, `workers: 1`, 28 tests, **7.5 min wall / 7.1 min summed** —
i.e. almost entirely serial-bound. Nine tests run 25–82s each. These are
genuinely slow (real libp2p dials, secondary instance spawns, app restarts),
not artificially slow. Lowest-value target; addressed last and conservatively.

---

## Plan

### Phase 1 — Stop the retry loop from firing during reset

**Impact: 19.1 min → ~6 min. This is the whole win; everything else is cleanup.**

The fix must not weaken the production startup race protection, and must not
break `tests/plugins/ui-actions.spec.ts:250`, which deliberately stubs
`GetPluginUIActions` to return empty once and asserts the retry happens
(`expect(retryCalls).toBeGreaterThanOrEqual(2)`).

**Task 1.1** — Give `loadPluginUIActions` an opt-out parameter, defaulting to
current behavior so both production callers and the retry test are untouched:

```js
async function loadPluginUIActions({ retry = true } = {}) {
    const generation = ++pluginUIActionsLoadGeneration;
    const retryDelays = retry ? [0, 100, 250, 500, 1000] : [0];
    ...
}
```

Called with no arguments everywhere today, so this is behavior-preserving by
construction. `tests/plugins/ui-actions.spec.ts` calls
`window.loadPluginUIActions()` with no args and keeps retrying.

**Task 1.2** — In `e2e/fixtures/test-fixtures.ts:869`, pass the opt-out. The
reset has just deleted every plugin, so there is nothing to wait for:

```js
if (typeof loadPluginUIActions === 'function') {
    await loadPluginUIActions({ retry: false });
}
```

**Task 1.3** — Verify with a timed subset before committing to the full run:

```bash
cd e2e && time npx playwright test tests/clips/ tests/tags/ 2>&1 | tail -20
```

Expect the ~4.3s/test average to drop to well under 1s. `tests/clips/` +
`tests/tags/` was 18.2 min summed across 268 tests; it should land near 2 min.

**Task 1.4** — Full suite run to confirm no behavioral regression. Some tests
may have been incidentally depending on the ~1.9s of settling time that the
retry loop provided between tests — a free implicit `waitForTimeout(1850)`
twice per test. This is the main risk in the whole plan. Any test that starts
failing is revealing a pre-existing race that was masked, and should be fixed
with a real wait condition (`waitForFunction` / `expect.poll`) rather than by
restoring the sleep.

**Task 1.5** — Fix the same latency in the product. `frontend/js/app.js:1228`
and six call sites in `frontend/js/plugins.js` hit this loop too. Removing your
last plugin, or starting the app with none installed, currently stalls the UI
for ~1.9s. Once the plugin manager has signalled readiness the retry is
pointless. Gate it on the existing `plugin:ready` event (already wired at
`frontend/js/ui.js:35`): once that event has fired, set a module flag and skip
the backoff. This is a small user-facing win independent of the test suite.

### Phase 2 — Halve the remaining reset calls

**Impact: ~6 min → ~5 min.**

**Task 2.1** — The `app` fixture resets both before *and* after every test.
After Phase 1 a reset costs ~20ms, so this is no longer urgent, but the
after-test reset is redundant: the next test's before-test reset covers it. The
only thing it buys is cleaner state when a run is interrupted mid-suite.

Drop the after-test `fastReset` (`test-fixtures.ts:3381-3392`), keeping the
before-test one. Retain the failure-screenshot block above it.

Do this as its own commit, after Phase 1 is green, so that if it destabilizes
anything it can be reverted independently.

### Phase 3 — Parallelize instance startup

**Impact: ~5 min → ~4 min, and scales the win from Phase 4.**

**Task 3.1** — `global-setup.ts` spawns instances sequentially with the comment
"Each wails dev instance compiles the app, so parallel spawning causes
conflicts." That is true only for the *first* build. Add an explicit warm-up
`go build` before the loop, then spawn the 4 instances concurrently:

```
Sequential: 4 × 22s + 3 × 500ms  ≈ 90s
Warm build + parallel spawn:      ≈ 35-40s
```

The existing mkdir-based `acquireRestartLock` in `wails-manager.ts` already
serializes the risky path (mid-suite restarts), so it stays as-is.

**Task 3.2** — If Task 3.1 proves flaky in practice, fall back to spawning the
first instance alone (priming the build cache), then the remaining three in
parallel. Strictly safer and still ~50s.

### Phase 4 — Raise worker count

**Impact: ~4 min → ~3 min.**

**Task 4.1** — The machine has 14 cores and 48 GB RAM; the config defaults to
4 workers. After Phase 1, wall time is dominated by `sum / workers`, so workers
become the limiting factor for the first time.

Raise the default from 4 to 6 in `playwright.config.ts`. Each worker is one
`wails dev` process plus a Chromium context, so memory, not CPU, is the ceiling.
Measure RSS during a 6-worker run before considering 8.

Keep `PW_WORKERS` as the override and leave `CI: 2` alone — CI runners are much
smaller than this machine.

### Phase 5 — Share suite (optional, low value)

**Impact: 7.5 min → ~5 min at best. Only worth attempting if Phases 1-4 land cleanly.**

**Task 5.1** — The suite is serial by deliberate design (documented at length in
`playwright.share.config.ts`: OOM kills, mDNS flakes, wails startup timeouts).
That reasoning was sound at 4 primary instances competing for memory. With the
share config spawning only 1 primary, 2 workers may now fit.

Try `workers: 2`. If any flake appears, revert immediately — a 2.5 min saving
is not worth reintroducing nondeterminism into P2P tests.

**Task 5.2** — The 15s unconditional `sleep` in `scripts/between-suites.sh`
exists to let macOS reclaim memory from four exiting wails instances before the
share suite starts. Keep it. It is 15 seconds and it prevents Jetsam SIGKILLs.

---

## Projected result

| Stage | Main suite | Share suite | Total |
|---|---|---|---|
| Today | 19.1 min | 7.5 min | ~27 min |
| After Phase 1 | ~6 min | 7.5 min | ~14 min |
| After Phase 2-4 | ~3 min | 7.5 min | ~11 min |
| After Phase 5 | ~3 min | ~5 min | ~8 min |

Phase 1 alone delivers roughly half the total available win, from a
two-line change.

## Risks

**The masked-race risk (Phase 1, real).** Every test currently gets ~3.7s of
incidental settling time per test from the retry sleeps. Removing it may expose
tests that were passing on timing rather than on correct wait conditions.
Mitigation: Task 1.4 runs the full suite and fixes any exposed race with a
proper wait condition. Do not paper over a failure by reinstating a sleep.

**Startup contention (Phase 3, moderate).** Parallel `wails dev` spawns share a
`build/` directory. Mitigated by the warm-up build and by the Task 3.2 fallback.

**Memory pressure (Phase 4, moderate).** More workers means more concurrent
wails instances. Measure RSS before going past 6.

## Commit sequence

Each phase is independently revertible, in descending value order:

1. `perf(e2e): skip plugin-action retry backoff during test reset` (Phase 1.1-1.4)
2. `perf(ui): skip plugin-action retry once plugin manager is ready` (Phase 1.5)
3. `perf(e2e): drop redundant post-test reset` (Phase 2)
4. `perf(e2e): parallelize wails instance startup` (Phase 3)
5. `perf(e2e): raise default worker count to 6` (Phase 4)
6. `perf(e2e): run share suite with 2 workers` (Phase 5, only if stable)

Run the full suite after each. Per `CLAUDE.md`, pipe through `| tail -50`.

---

## Outcome

`npm test` went from **~27 min to 6:15** (main suite 19.1m → 2.4m, share suite
7.5m → 3.6m), with 974 passing and no reduction in coverage.

Measured at each step, on the same machine:

| Step | Main suite |
|---|---|
| Baseline | 19.1 min |
| Phase 1 (reset skips the backoff) | 4.3 min |
| Fast spawn flags + 6 workers | 3.2 min |
| Backend readiness flag | 2.4 min |

### Where the plan was wrong

**Phase 3 (parallel instance spawn) was not viable.** `wails dev` has no
output-path flag, so every instance builds to the same `build/bin/mahpastes`.
Spawning in parallel would race on that binary — the sequential design was
correct and the warm-up build in the plan would not have helped.

What worked instead: `wails dev` redoes the Tailwind build, binding generation,
embed-file creation and `go mod tidy` on *every* spawn. Passing
`-s -skipbindings -skipembedcreate -m` to every instance after the first cuts a
spawn from 22s to 11s, with no concurrency risk. This also applies to mid-suite
restarts and secondary share instances, which is most of why the share suite
more than halved without touching its worker count.

**Phase 2 (drop the post-test reset) was dropped as not worth it.** Once the
backoff was gone, a reset measured 13ms, so the whole post-test reset totals
0.5 min of summed time — about 7s of wall clock at 6 workers. Not worth giving
up the clean state it guarantees when a run is interrupted.

**Phase 5 (share suite at 2 workers) was declined.** The serialisation is
documented as protection against OOM kills, mDNS flakes and Jetsam SIGKILLs.
With the suite already at 3.6 min, reintroducing nondeterminism into the P2P
tests to save ~1 min is a bad trade.

### The masked race, as predicted

Phase 1.4 anticipated that removing ~3.7s/test of incidental settling time
would expose races. Exactly one surfaced, and only at 6 workers:
`tag-tree-exclusivity` → "folder view shows clip only at its exact tag level".

It was a real defect in the fixture. `toggleFolderMode()` and `clickFolder()`
waited on `__appReady === true`, but `toggleFolderMode()` in `app.js` fires
`loadClips()` from a sync click handler without awaiting it and never touches
`__appReady` — so the flag was already true and the wait returned immediately,
leaving the assertion to run against the pre-click DOM. Fixed with a real
completion signal (a render counter bumped in `loadClips()`'s `finally`), not
by restoring a sleep.

### Product fix, not just a test fix

The root cause was never test-only. `loadPluginUIActions()` conflated "plugins
have not loaded yet" with "no plugins are installed", and `app.js` awaits it
before setting `__appReady`. Any user with no plugins installed paid the full
1850ms on every app start. Cold start with no plugins is now ~0.8s instead of
~2.7s.

The fix has the backend state readiness (`ready` on `UIActionsResponse`, set on
both the success and failure paths of plugin init) rather than having the
frontend infer it from a non-empty result. The backoff still applies during a
genuine startup race, so `tests/plugins/ui-actions.spec.ts` — which stubs an
empty first response and asserts a retry happens — is unaffected.

### Commits

```
8af7352 perf(e2e): skip plugin-action retry backoff during test reset
2ca2d32 fix(e2e): wait for the real gallery re-render, not a stale ready flag
beda994 perf(e2e): reuse the first instance's build for later spawns
64e076d perf(ui): report plugin readiness instead of guessing by retrying
```

### Note for future runs

The 15 `tests/server/` specs need `build/bin/mahpastesd`; run `make mahpastesd`
first or they fail with a missing-binary error under the default config.
