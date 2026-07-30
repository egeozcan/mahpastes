// The single implementation of executable work, shared by the worker and the
// main-thread fallback. Nothing here touches the DOM, `window`, or the network.
//
// Every task is a generator that yields once per unit of work and returns its
// result. The driver — not the task — decides whether to run the next unit, so
// the fallback executor can yield to the event loop between units while the
// worker simply drains the generator. Splitting work into units is therefore the
// mechanism that makes a shared UI thread survivable; a task with no natural
// sub-division is one unit, which is exactly why the fallback also carries a
// much lower byte ceiling.

import {
    ExecutorError,
    HANDSHAKE_TOKEN,
    OP_FORMAT,
    OP_HANDSHAKE,
    OP_SELFTEST,
    OP_VALIDATE,
    PROTOCOL_VERSION,
    ERR_UNKNOWN_OP,
} from './protocol.js';
import { formatterFor, validatorFor } from './validators/index.js';

function* handshakeTask() {
    yield;
    return { handshake: HANDSHAKE_TOKEN, protocol: PROTOCOL_VERSION };
}

// Burns a bounded amount of synchronous CPU per unit. Used by the native smoke
// harness and the e2e suite to force a deadline, prove the fallback yields
// between units, and exercise terminate/restart.
function* selftestTask(payload) {
    const units = Math.max(1, Math.min(10000, Number(payload && payload.units) || 1));
    const spinMs = Math.max(0, Math.min(60000, Number(payload && payload.spinMsPerUnit) || 0));
    let completed = 0;
    for (let i = 0; i < units; i++) {
        if (spinMs > 0) {
            const until = Date.now() + spinMs;
            // Deliberately synchronous: this is the shape of a real parser call.
            while (Date.now() < until) { /* spin */ }
        }
        completed++;
        yield;
    }
    return { units: completed, spinMsPerUnit: spinMs };
}

function sourceOf(payload) {
    const value = payload && payload.source;
    return typeof value === 'string' ? value : '';
}

// Presentation options a validator needs in order to agree with what the user is
// looking at. Only CSV/TSV use them, and only for the temporary delimiter choice:
// findings that described a different delimiter than the rendered table would be
// worse than no findings at all.
function optionsOf(payload) {
    const value = payload && payload.options;
    return value && typeof value === 'object' ? value : null;
}

/**
 * Runs the authoritative validator for a format.
 *
 * `yield*` delegates the adapter's own yields to the driver, so JSON Lines still
 * splits per batch of lines and YAML still splits per document even though the
 * task boundary is here.
 *
 * A format with no validator returns an empty result rather than failing: shell,
 * `.env`, INI/CFG/CONF, plain text and logs deliberately produce no diagnostics,
 * and the remaining non-authoritative sources (Lezer recovery markers, renderer
 * failures) are merged in above this layer.
 */
function* validateTask(payload) {
    const format = (payload && payload.format) || null;
    const validator = validatorFor(format);
    if (!validator) return { format, diagnostics: [], truncated: false, unavailable: null };
    return yield* validator(sourceOf(payload), optionsOf(payload));
}

/**
 * Formats a document. Explicit only — nothing calls this during preview or save.
 *
 * A parse failure is returned as `{ ok: false }` rather than thrown: the caller
 * has already disabled the action while authoritative errors exist, so reaching
 * here with invalid source is a race, not a bug worth taking the executor down for.
 */
function* formatTask(payload) {
    const format = (payload && payload.format) || null;
    const formatter = formatterFor(format);
    if (!formatter) return { ok: false, format, reason: 'unsupported' };
    try {
        const { text } = yield* formatter(sourceOf(payload));
        return { ok: true, format, text };
    } catch (err) {
        return { ok: false, format, reason: 'invalid', message: String((err && err.message) || err) };
    }
}

const TASKS = {
    [OP_HANDSHAKE]: handshakeTask,
    [OP_SELFTEST]: selftestTask,
    [OP_VALIDATE]: validateTask,
    [OP_FORMAT]: formatTask,
};

export function hasTask(op) {
    return Object.prototype.hasOwnProperty.call(TASKS, op);
}

export function createTask(op, payload) {
    const factory = TASKS[op];
    if (!factory) throw new ExecutorError(ERR_UNKNOWN_OP, `unknown executor op: ${op}`);
    return factory(payload);
}

// Drains a task to completion with no yielding. Correct only off the UI thread.
export function runTaskToCompletion(op, payload) {
    const task = createTask(op, payload);
    let step = task.next();
    while (!step.done) step = task.next();
    return step.value;
}
