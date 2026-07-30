// Main-thread wrapper around the static validator worker.
//
// The worker is the preferred execution context because its deadline is a hard
// interrupt: a synchronous parser that will not return can be killed by
// terminating the thread. That is the one thing the fallback executor cannot do,
// and the reason the two carry very different byte ceilings.

import {
    ExecutorError,
    EXECUTOR_LIMITS,
    HANDSHAKE_TOKEN,
    OP_HANDSHAKE,
    ERR_FAILED,
    ERR_STALE,
    ERR_TERMINATED,
    ERR_TIMEOUT,
    ERR_TOO_LARGE,
    ERR_UNAVAILABLE,
    freezePayload,
    measureSourceBytes,
    isResponse,
    isValidHandshake,
    makeRequest,
} from './protocol.js';

// Root-absolute on purpose. A relative URL resolved against a deep path can hit
// the SPA fallback and return index.html, which constructs a "successful" worker
// that never answers.
export const DEFAULT_WORKER_URL = '/dist/text-validator.worker.js';

export function createWorkerExecutor(options = {}) {
    const url = options.url || DEFAULT_WORKER_URL;
    const limits = { ...EXECUTOR_LIMITS.worker, ...(options.limits || {}) };
    const handshakeTimeoutMs = options.handshakeTimeoutMs || 3000;
    const WorkerCtor = options.WorkerConstructor || (typeof Worker !== 'undefined' ? Worker : null);

    let worker = null;
    let disposed = false;
    let nextRequestID = 1;
    let highestGeneration = -Infinity;
    let restartCount = 0;
    const pending = new Map();

    function rejectAll(code, message) {
        const entries = [...pending.values()];
        pending.clear();
        for (const entry of entries) {
            clearTimeout(entry.timer);
            entry.reject(new ExecutorError(code, message));
        }
    }

    function killWorker(code, message) {
        if (worker) {
            worker.onmessage = null;
            worker.onerror = null;
            worker.onmessageerror = null;
            worker.terminate();
            worker = null;
        }
        rejectAll(code, message);
    }

    function handleMessage(event) {
        const response = event.data;
        if (!isResponse(response)) return;
        const entry = pending.get(response.id);
        if (!entry) return;
        pending.delete(response.id);
        clearTimeout(entry.timer);
        // A result for a superseded generation is dropped rather than applied.
        if (Number.isFinite(response.generation) && response.generation < highestGeneration) {
            entry.reject(new ExecutorError(ERR_STALE, `result for generation ${response.generation} superseded by ${highestGeneration}`));
            return;
        }
        if (response.ok) entry.resolve(response.result);
        else entry.reject(new ExecutorError((response.error && response.error.code) || ERR_FAILED, response.error && response.error.message));
    }

    function ensureWorker() {
        if (disposed) throw new ExecutorError(ERR_UNAVAILABLE, 'executor disposed');
        if (worker) return worker;
        if (!WorkerCtor) throw new ExecutorError(ERR_UNAVAILABLE, 'Worker is not available on this surface');
        let created;
        try {
            created = new WorkerCtor(url);
        } catch (err) {
            throw new ExecutorError(ERR_UNAVAILABLE, `worker construction failed: ${String((err && err.message) || err)}`);
        }
        created.onmessage = handleMessage;
        // A non-JS response (the SPA fallback serving index.html for a missing
        // path) surfaces here rather than at construction on most engines.
        created.onerror = (event) => {
            if (event && typeof event.preventDefault === 'function') event.preventDefault();
            killWorker(ERR_UNAVAILABLE, `worker error: ${(event && event.message) || 'load or runtime failure'}`);
        };
        created.onmessageerror = () => killWorker(ERR_UNAVAILABLE, 'worker message could not be deserialized');
        worker = created;
        return worker;
    }

    function post({ op, payload, generation, sourceBytes, timeoutMs }) {
        if (disposed) return Promise.reject(new ExecutorError(ERR_UNAVAILABLE, 'executor disposed'));

        if (Number.isFinite(generation)) {
            if (generation < highestGeneration) {
                return Promise.reject(new ExecutorError(ERR_STALE, `generation ${generation} already superseded by ${highestGeneration}`));
            }
            if (generation > highestGeneration) {
                highestGeneration = generation;
                // Everything in flight belongs to an older generation, so kill the
                // thread rather than waiting for work whose result will be discarded.
                if (pending.size > 0) killWorker(ERR_TERMINATED, 'superseded by a newer generation');
            }
        }

        // Pinned before measuring, so the source that was measured is the source
        // that is posted: the caller still holds a reference to the object it passed.
        const pinned = freezePayload(payload);
        // Measured, not taken on trust: a caller that omits sourceBytes must not
        // be able to opt out of the ceiling that keeps synchronous parsing bounded.
        const effectiveBytes = measureSourceBytes(pinned, sourceBytes);
        if (effectiveBytes > limits.maxSourceBytes) {
            return Promise.reject(new ExecutorError(ERR_TOO_LARGE, `source of ${effectiveBytes} bytes exceeds the ${limits.maxSourceBytes}-byte executor ceiling`));
        }

        let target;
        try {
            target = ensureWorker();
        } catch (err) {
            return Promise.reject(err);
        }

        const id = nextRequestID++;
        const deadline = Number.isFinite(timeoutMs) ? timeoutMs : limits.deadlineMs;
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => {
                // Terminating is the interruption boundary: a synchronous parser
                // mid-call cannot be asked to stop, only killed. The next request
                // lazily constructs a fresh worker.
                restartCount++;
                killWorker(ERR_TIMEOUT, `no response within ${deadline}ms`);
            }, deadline);
            pending.set(id, { resolve, reject, timer, generation });
            try {
                target.postMessage(makeRequest({ id, generation, op, payload: pinned }));
            } catch (err) {
                pending.delete(id);
                clearTimeout(timer);
                reject(new ExecutorError(ERR_FAILED, `postMessage failed: ${String((err && err.message) || err)}`));
            }
        });
    }

    const executor = {
        kind: 'worker',
        url,
        limits,
        run: (request) => post(request),
        get generation() { return highestGeneration; },
        get restartCount() { return restartCount; },
        get inFlight() { return pending.size; },
        get alive() { return !!worker; },
        dispose() {
            disposed = true;
            killWorker(ERR_TERMINATED, 'executor disposed');
        },
    };

    // The handshake is the capability probe. Failure, timeout, and a response
    // that is not our handshake are all the same answer: no worker here.
    return post({ op: OP_HANDSHAKE, timeoutMs: handshakeTimeoutMs })
        .then((result) => {
            if (!isValidHandshake({ kind: 'response', ok: true, result })) {
                executor.dispose();
                throw new ExecutorError(ERR_UNAVAILABLE, `handshake mismatch: expected ${HANDSHAKE_TOKEN}`);
            }
            return executor;
        })
        .catch((err) => {
            executor.dispose();
            throw err instanceof ExecutorError ? err : new ExecutorError(ERR_UNAVAILABLE, String((err && err.message) || err));
        });
}
