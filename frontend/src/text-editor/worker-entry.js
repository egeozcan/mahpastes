// Entrypoint for frontend/dist/text-validator.worker.js.
//
// This artifact is loaded as a *static* same-origin script — never from a
// `blob:` or `data:` URL — so the served-mode `default-src 'self'` policy
// covers it without a blob allowance.

import { ExecutorError, makeFailure, makeResult, ERR_FAILED } from './protocol.js';
import { runTaskToCompletion } from './tasks.js';

self.onmessage = (event) => {
    const request = event.data;
    if (!request || request.kind !== 'request') return;
    const { id, generation, op, payload } = request;
    try {
        const result = runTaskToCompletion(op, payload);
        self.postMessage(makeResult({ id, generation, result }));
    } catch (err) {
        const code = err instanceof ExecutorError ? err.code : ERR_FAILED;
        self.postMessage(makeFailure({ id, generation, code, message: String((err && err.message) || err) }));
    }
};
