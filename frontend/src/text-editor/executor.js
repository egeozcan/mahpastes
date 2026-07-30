// Capability probe and executor selection.
//
// The choice between the worker and the bounded main-thread fallback is made
// per surface at startup by probing, never by hardcoding a platform. Because both
// executors sit behind one request/response interface, the choice is a
// construction-time detail invisible to callers.

import { ExecutorError, ERR_UNAVAILABLE } from './protocol.js';
import { createFallbackExecutor } from './fallback-executor.js';
import { createWorkerExecutor, DEFAULT_WORKER_URL } from './worker-executor.js';

// Records why a surface ended up where it did, so a support report can say
// "WKWebView refused the worker" rather than "diagnostics feel slow".
function probeRecord(kind, reason) {
    return { kind, reason: reason || null, workerAvailable: kind === 'worker' };
}

export async function probeWorkerSupport(options = {}) {
    const url = options.url || DEFAULT_WORKER_URL;
    if (options.forceFallback) {
        return { executor: null, probe: probeRecord('fallback', 'worker probe skipped by configuration') };
    }
    try {
        const executor = await createWorkerExecutor({ ...options, url });
        return { executor, probe: probeRecord('worker', null) };
    } catch (err) {
        const code = err instanceof ExecutorError ? err.code : ERR_UNAVAILABLE;
        return { executor: null, probe: probeRecord('fallback', `${code}: ${String((err && err.message) || err)}`) };
    }
}

// Builds a fresh executor. Callers that want the shared per-surface one should
// use getExecutor().
export async function createExecutor(options = {}) {
    const { executor, probe } = await probeWorkerSupport(options);
    if (executor) {
        executor.probe = probe;
        return executor;
    }
    const fallback = createFallbackExecutor(options);
    fallback.probe = probe;
    return fallback;
}

let shared = null;

// One probe per surface per page load. The probe costs a worker construction and
// a single round-trip, so repeating it per validation would be pure waste.
export function getExecutor(options = {}) {
    if (!shared) shared = createExecutor(options);
    return shared;
}

export function resetExecutor() {
    const previous = shared;
    shared = null;
    if (previous) previous.then((executor) => executor.dispose(), () => {});
}
