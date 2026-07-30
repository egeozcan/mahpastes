// Wire protocol shared by the static validator worker and the main-thread
// fallback executor. Both sides import this file so the two execution contexts
// cannot drift apart.

export const PROTOCOL_VERSION = 1;

// A fixed token the worker echoes during the handshake. The probe requires it
// verbatim, which is what distinguishes "a worker that loaded our script" from
// "a worker that loaded whatever the SPA fallback served for that URL".
export const HANDSHAKE_TOKEN = 'mahpastes:text-validator:v1';

export const OP_HANDSHAKE = 'handshake';
// Deliberate shipped surface, not a leftover: it is the only way to exercise
// deadline, termination, and restart behaviour on a real device before any
// parser exists, which milestone 1's native smoke check requires. It performs
// no I/O and touches no clip data.
export const OP_SELFTEST = 'selftest';
// Authoritative format validation and explicit formatting. Both take
// `{ format, source }` and neither executes source, fetches anything, or touches
// the DOM — they run identically in the worker and in the fallback executor.
export const OP_VALIDATE = 'validate';
export const OP_FORMAT = 'format';

export const ERR_STALE = 'stale';
export const ERR_TIMEOUT = 'timeout';
export const ERR_TERMINATED = 'terminated';
export const ERR_UNAVAILABLE = 'unavailable';
export const ERR_TOO_LARGE = 'too-large';
export const ERR_UNKNOWN_OP = 'unknown-op';
export const ERR_FAILED = 'failed';

// The user-visible notice for every case where no authoritative answer was
// established. It must never be presented as "the document is valid".
export const UNAVAILABLE_NOTICE = 'Validation unavailable within safety limits';

export const EXECUTOR_LIMITS = {
    worker: {
        // A dedicated thread can be killed mid-parse, so the deadline is a hard
        // interrupt and the ceiling is the common enhanced-assistance threshold.
        deadlineMs: 1500,
        maxSourceBytes: 2 * 1024 * 1024,
    },
    fallback: {
        // The UI thread is shared and synchronous parsers cannot be preempted,
        // so the deadline is only observed between units. The byte ceiling —
        // not the deadline — is the real bound on the worst single call.
        deadlineMs: 250,
        maxSourceBytes: 64 * 1024,
    },
};

export function makeRequest({ id, generation, op, payload }) {
    return { kind: 'request', id, generation, op, payload };
}

export function makeResult({ id, generation, result }) {
    return { kind: 'response', id, generation, ok: true, result };
}

export function makeFailure({ id, generation, code, message }) {
    return { kind: 'response', id, generation, ok: false, error: { code, message } };
}

/**
 * The byte size an executor must enforce its ceiling against.
 *
 * Deliberately does NOT trust a caller-supplied `sourceBytes`: the executor is the
 * safety boundary, and on the fallback path that boundary is the only thing
 * standing between a pathological document and a frozen UI thread. A caller that
 * omits the field, or understates it, must not be able to opt out of the ceiling —
 * so the payload's own source is measured and the larger of the two wins.
 */
/**
 * Freezes the parts of a payload the executor's own limits depend on.
 *
 * A caller keeps a reference to the object it passed, so without this the measured
 * source and the executed source can differ: measure `{}`, mutate `payload.source`
 * to something enormous, and the queued task parses the enormous one under a
 * ceiling that approved the small one. Strings are immutable, so copying the
 * reference is enough to pin it.
 */
export function freezePayload(payload) {
    if (!payload || typeof payload !== 'object') return payload;
    return Object.freeze({ ...payload });
}

export function measureSourceBytes(payload, declared) {
    let measured = 0;
    const source = payload && payload.source;
    if (typeof source === 'string') {
        try {
            measured = new TextEncoder().encode(source).length;
        } catch {
            // Worst case, fall back to code units — still a real bound.
            measured = source.length;
        }
    }
    return Number.isFinite(declared) ? Math.max(declared, measured) : measured;
}

export function isResponse(value) {
    return !!value && typeof value === 'object' && value.kind === 'response';
}

// A handshake reply only counts when it is our script answering our protocol.
export function isValidHandshake(value) {
    return (
        isResponse(value) &&
        value.ok === true &&
        !!value.result &&
        value.result.handshake === HANDSHAKE_TOKEN &&
        value.result.protocol === PROTOCOL_VERSION
    );
}

export class ExecutorError extends Error {
    constructor(code, message) {
        super(message || code);
        this.name = 'ExecutorError';
        this.code = code;
    }
}

// True for every failure mode that means "no authoritative result was
// established" rather than "the document has an error".
export function isUnavailableCode(code) {
    return code === ERR_TIMEOUT || code === ERR_TERMINATED || code === ERR_UNAVAILABLE || code === ERR_TOO_LARGE;
}
