// --- Native worker self-test ---
//
// Runs only when the app was started with `--selftest-worker`, which is the one
// case where `window.go.main.SelfTestService` is bound. In every normal run that
// binding is absent and this file does nothing at all.
//
// It exists because the e2e suites cannot answer the question milestone 1 asks.
// Playwright drives Chromium over an http://localhost origin; production runs
// WKWebView on macOS, WebView2 on Windows and WebKitGTK on Linux, from a custom
// scheme. Whether a static worker loads there is engine- and origin-dependent, so
// green Playwright output is not evidence for it. This exercises the same five
// things the design names — load, one round-trip, a forced timeout, termination,
// and restart — and reports JSON that CI can assert on.

(() => {
    const service = () => window.go?.main?.SelfTestService;
    if (!service()) return;

    const STEP_TIMEOUT_MS = 20000;

    function api() {
        return window.MahpastesTextEditor;
    }

    async function waitForBundle(deadlineMs) {
        const until = Date.now() + deadlineMs;
        while (Date.now() < until) {
            if (api() && typeof api().probeWorkerSupport === 'function') return true;
            await new Promise((resolve) => setTimeout(resolve, 100));
        }
        return false;
    }

    async function run() {
        const steps = [];
        const record = (name, ok, detail) => {
            steps.push({ name, ok, detail: detail === undefined ? null : detail });
            return ok;
        };

        if (!await waitForBundle(STEP_TIMEOUT_MS)) {
            return { ok: false, reason: 'bundle-missing', steps };
        }

        const shared = api();
        let executor = null;
        let probe = null;

        try {
            // 1. Construct from the root-absolute URL, exactly as the app does. A
            //    relative URL resolved against a deep path can hit the SPA fallback
            //    and return index.html, which yields a "successful" worker that
            //    never answers — so the handshake token is what actually decides.
            const probed = await shared.probeWorkerSupport();
            executor = probed.executor;
            probe = probed.probe;
            record('probe', !!executor, probe);

            if (!executor) {
                // Not a failure of the app: the fallback executor is a designed
                // outcome. It IS a failure of the worker on this surface, which is
                // the thing being measured, so it is reported as such.
                return {
                    ok: false,
                    reason: 'worker-unavailable',
                    fallbackIsDesigned: true,
                    probe,
                    steps,
                };
            }

            // 2. One real round-trip.
            const handshake = await executor.run({ op: shared.OP_HANDSHAKE, generation: 1 });
            const handshakeOK = !!handshake &&
                handshake.handshake === shared.HANDSHAKE_TOKEN &&
                handshake.protocol === shared.PROTOCOL_VERSION;
            if (!record('handshake', handshakeOK, handshake)) {
                return { ok: false, reason: 'handshake-failed', probe, steps };
            }

            // 3. A forced deadline. The selftest op burns synchronous CPU, which is
            //    the shape of a real parser call, so this proves the deadline can
            //    interrupt work that cannot cooperate.
            const deadlineMs = shared.EXECUTOR_LIMITS.worker.deadlineMs;
            let timedOut = false;
            let timeoutCode = null;
            try {
                await executor.run({
                    op: shared.OP_SELFTEST,
                    generation: 2,
                    payload: { units: 1, spinMsPerUnit: deadlineMs + 2000 },
                    timeoutMs: 300,
                });
            } catch (err) {
                timeoutCode = err && err.code;
                timedOut = timeoutCode === 'timeout';
            }
            if (!record('timeout-terminates', timedOut, timeoutCode)) {
                return { ok: false, reason: 'timeout-not-enforced', probe, steps };
            }

            // 4. Restart. The timeout killed the thread; the next request has to
            //    lazily construct a fresh one, or one pathological document would
            //    disable validation for the rest of the session.
            const afterRestart = await executor.run({ op: shared.OP_HANDSHAKE, generation: 3 });
            const restartOK = !!afterRestart && afterRestart.handshake === shared.HANDSHAKE_TOKEN;
            if (!record('restart', restartOK, afterRestart)) {
                return { ok: false, reason: 'restart-failed', probe, steps };
            }

            // 5. A completed bounded run, so "it only ever times out" cannot pass.
            const work = await executor.run({
                op: shared.OP_SELFTEST,
                generation: 4,
                payload: { units: 3, spinMsPerUnit: 1 },
            });
            const workOK = !!work && work.units === 3;
            if (!record('bounded-work', workOK, work)) {
                return { ok: false, reason: 'work-failed', probe, steps };
            }

            return { ok: true, probe, steps };
        } catch (err) {
            record('unexpected-error', false, String((err && err.message) || err));
            return { ok: false, reason: 'exception', probe, steps };
        } finally {
            try { if (executor) executor.dispose(); } catch (_) { /* nothing left to do */ }
        }
    }

    function describeEnvironment() {
        return {
            userAgent: navigator.userAgent,
            origin: window.location.origin,
            href: window.location.href,
        };
    }

    window.addEventListener('load', () => {
        run()
            .catch((err) => ({ ok: false, reason: 'harness-error', detail: String((err && err.message) || err) }))
            .then((result) => {
                const payload = JSON.stringify({ ...result, environment: describeEnvironment() });
                const target = service();
                if (target) target.ReportWorkerSelfTest(payload);
            });
    });
})();
