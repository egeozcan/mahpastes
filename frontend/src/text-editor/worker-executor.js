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

    // One request on the thread at a time. The worker runs requests serially
    // anyway; sending them one by one is what lets a request that is superseded
    // while still waiting be dropped without ever being parsed, and lets each
    // deadline measure the request's own run rather than its time in the queue.
    //
    // An entry is { id, resolve, reject, timer, deadline, generation,
    // superseded, message }. A superseded entry has already been rejected as
    // stale; while it is on the thread it stays as `current` only so its
    // deadline can still interrupt a run that never returns.
    let current = null;
    const queue = [];

    function settleError(entry, code, message) {
        if (!entry.superseded) entry.reject(new ExecutorError(code, message));
    }

    function terminateWorker() {
        if (worker) {
            worker.onmessage = null;
            worker.onerror = null;
            worker.onmessageerror = null;
            worker.terminate();
            worker = null;
        }
    }

    function killWorker(code, message) {
        terminateWorker();
        const entries = current ? [current, ...queue] : [...queue];
        if (current) clearTimeout(current.timer);
        current = null;
        queue.length = 0;
        for (const entry of entries) settleError(entry, code, message);
    }

    function onDeadline(entry) {
        if (current !== entry) return;
        // Terminating is the interruption boundary: a synchronous parser
        // mid-call cannot be asked to stop, only killed. The next request lazily
        // constructs a fresh worker.
        restartCount++;
        if (!entry.superseded) {
            // The caller's own request blew its deadline: everything waiting
            // shares the verdict, as before.
            killWorker(ERR_TIMEOUT, `no response within ${entry.deadline}ms`);
            return;
        }
        // A superseded run outlived its deadline. Nobody wants its result, but
        // the live requests behind it do: they go to a fresh thread.
        terminateWorker();
        current = null;
        pump();
    }

    // Sends the next queued request when the thread is free.
    function pump() {
        while (!current && queue.length > 0) {
            const entry = queue.shift();
            let target;
            try {
                target = ensureWorker();
            } catch (err) {
                entry.reject(err);
                continue;
            }
            current = entry;
            entry.timer = setTimeout(() => onDeadline(entry), entry.deadline);
            try {
                target.postMessage(entry.message);
            } catch (err) {
                clearTimeout(entry.timer);
                current = null;
                entry.reject(new ExecutorError(ERR_FAILED, `postMessage failed: ${String((err && err.message) || err)}`));
            }
        }
    }

    function handleMessage(event) {
        const response = event.data;
        if (!isResponse(response)) return;
        const entry = current;
        if (!entry || entry.id !== response.id) return;
        clearTimeout(entry.timer);
        current = null;
        // Already rejected as stale when it was superseded; the late result is
        // simply dropped.
        if (!entry.superseded) {
            if (Number.isFinite(response.generation) && response.generation < highestGeneration) {
                // A result for a superseded generation is dropped rather than applied.
                entry.reject(new ExecutorError(ERR_STALE, `result for generation ${response.generation} superseded by ${highestGeneration}`));
            } else if (response.ok) {
                entry.resolve(response.result);
            } else {
                entry.reject(new ExecutorError((response.error && response.error.code) || ERR_FAILED, response.error && response.error.message));
            }
        }
        pump();
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

    function supersede(generation) {
        // Superseded work is not killed: re-spawning the thread (and re-loading
        // its parsers) on every edit past a slow parse cost more than letting
        // it finish. Its callers are told now; a queued request is dropped
        // unsent, and the one on the thread finishes into the void — or is
        // killed by its own deadline.
        const message = `superseded by generation ${generation}`;
        if (current && Number.isFinite(current.generation) && current.generation < generation && !current.superseded) {
            current.superseded = true;
            current.reject(new ExecutorError(ERR_STALE, message));
        }
        for (let i = queue.length - 1; i >= 0; i--) {
            const entry = queue[i];
            if (Number.isFinite(entry.generation) && entry.generation < generation) {
                queue.splice(i, 1);
                entry.reject(new ExecutorError(ERR_STALE, message));
            }
        }
    }

    function post({ op, payload, generation, sourceBytes, timeoutMs }) {
        if (disposed) return Promise.reject(new ExecutorError(ERR_UNAVAILABLE, 'executor disposed'));

        if (Number.isFinite(generation)) {
            if (generation < highestGeneration) {
                return Promise.reject(new ExecutorError(ERR_STALE, `generation ${generation} already superseded by ${highestGeneration}`));
            }
            if (generation > highestGeneration) {
                highestGeneration = generation;
                supersede(generation);
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

        // Surface an unusable worker synchronously, as before, rather than
        // after the request has been queued.
        try {
            ensureWorker();
        } catch (err) {
            return Promise.reject(err);
        }

        const id = nextRequestID++;
        const deadline = Number.isFinite(timeoutMs) ? timeoutMs : limits.deadlineMs;
        return new Promise((resolve, reject) => {
            queue.push({
                id,
                resolve,
                reject,
                timer: null,
                deadline,
                generation,
                superseded: false,
                message: makeRequest({ id, generation, op, payload: pinned }),
            });
            pump();
        });
    }

    const executor = {
        kind: 'worker',
        url,
        limits,
        run: (request) => post(request),
        get generation() { return highestGeneration; },
        get restartCount() { return restartCount; },
        get inFlight() { return (current && !current.superseded ? 1 : 0) + queue.length; },
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
