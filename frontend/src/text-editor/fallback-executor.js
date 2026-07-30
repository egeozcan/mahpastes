// Time-sliced main-thread executor, used only on a surface where the capability
// probe could not construct a working worker.
//
// The deadline here is best-effort, not a hard interrupt, and this module does
// not pretend otherwise: the bundled parsers are synchronous and cannot be
// preempted mid-call, so a budget is observed only *between* units. The real
// bound on the worst single call is therefore the much lower byte ceiling in
// EXECUTOR_LIMITS.fallback, not the deadline.

import {
    ExecutorError,
    EXECUTOR_LIMITS,
    ERR_FAILED,
    ERR_STALE,
    ERR_TERMINATED,
    ERR_TIMEOUT,
    ERR_TOO_LARGE,
    ERR_UNAVAILABLE,
    freezePayload,
    measureSourceBytes,
    ERR_UNKNOWN_OP,
} from './protocol.js';
import { createTask } from './tasks.js';

// A macrotask yield. setTimeout(0) is clamped to several milliseconds in some
// engines, which would dominate the budget; a MessageChannel hop is not.
function yieldToEventLoop() {
    return new Promise((resolve) => {
        const channel = new MessageChannel();
        channel.port1.onmessage = () => {
            channel.port1.close();
            resolve();
        };
        channel.port2.postMessage(0);
    });
}

const now = () => (typeof performance !== 'undefined' && performance.now ? performance.now() : Date.now());

export function createFallbackExecutor(options = {}) {
    const limits = { ...EXECUTOR_LIMITS.fallback, ...(options.limits || {}) };

    let disposed = false;
    let highestGeneration = -Infinity;
    let abandonCount = 0;
    // The UI thread is shared, so requests are serialized rather than
    // interleaved: two concurrent runs would each get half a budget.
    let chain = Promise.resolve();
    const active = new Set();

    function abandonOlderThan(generation) {
        for (const entry of active) {
            if (Number.isFinite(entry.generation) && entry.generation < generation) entry.abandoned = true;
        }
    }

    async function drive(entry) {
        const { op, payload, deadlineMs } = entry;
        let task;
        try {
            task = createTask(op, payload);
        } catch (err) {
            throw err instanceof ExecutorError ? err : new ExecutorError(ERR_UNKNOWN_OP, String((err && err.message) || err));
        }

        // Only time spent inside units counts; the yield gaps belong to the UI.
        let spent = 0;
        let units = 0;
        for (;;) {
            if (disposed) throw new ExecutorError(ERR_TERMINATED, 'executor disposed');
            if (entry.abandoned) {
                throw new ExecutorError(ERR_STALE, `generation ${entry.generation} superseded by ${highestGeneration}`);
            }
            const started = now();
            let step;
            try {
                step = task.next();
            } catch (err) {
                throw err instanceof ExecutorError ? err : new ExecutorError(ERR_FAILED, String((err && err.message) || err));
            }
            spent += now() - started;
            units++;
            if (step.done) {
                entry.units = units;
                return step.value;
            }
            if (spent > deadlineMs) {
                // Cooperative abandonment replaces worker termination as the
                // interruption boundary. Everything else — the notice, the
                // generation guards — carries over verbatim.
                abandonCount++;
                if (typeof task.return === 'function') task.return(undefined);
                throw new ExecutorError(ERR_TIMEOUT, `abandoned after ${Math.round(spent)}ms of main-thread time across ${units} units`);
            }
            await yieldToEventLoop();
        }
    }

    function run({ op, payload, generation, sourceBytes, timeoutMs }) {
        if (disposed) return Promise.reject(new ExecutorError(ERR_UNAVAILABLE, 'executor disposed'));

        if (Number.isFinite(generation)) {
            if (generation < highestGeneration) {
                return Promise.reject(new ExecutorError(ERR_STALE, `generation ${generation} already superseded by ${highestGeneration}`));
            }
            if (generation > highestGeneration) {
                highestGeneration = generation;
                abandonOlderThan(generation);
            }
        }

        // Pinned before measuring, so the source that was measured is the source
        // that runs: the caller still holds a reference to the object it passed.
        const pinned = freezePayload(payload);
        // Measured, not taken on trust: a caller that omits sourceBytes must not
        // be able to opt out of the ceiling that keeps synchronous parsing bounded.
        const effectiveBytes = measureSourceBytes(pinned, sourceBytes);
        if (effectiveBytes > limits.maxSourceBytes) {
            return Promise.reject(new ExecutorError(ERR_TOO_LARGE, `source of ${effectiveBytes} bytes exceeds the ${limits.maxSourceBytes}-byte fallback ceiling`));
        }

        const entry = {
            op,
            payload: pinned,
            generation,
            abandoned: false,
            deadlineMs: Number.isFinite(timeoutMs) ? timeoutMs : limits.deadlineMs,
        };
        active.add(entry);

        const result = chain.then(() => drive(entry));
        // Keep the queue moving after a rejection without turning the queue
        // itself into an unhandled rejection.
        chain = result.then(() => undefined, () => undefined);
        return result.finally(() => active.delete(entry));
    }

    return {
        kind: 'fallback',
        limits,
        run,
        get generation() { return highestGeneration; },
        get abandonCount() { return abandonCount; },
        get inFlight() { return active.size; },
        dispose() {
            disposed = true;
            for (const entry of active) entry.abandoned = true;
        },
    };
}
