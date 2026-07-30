// Entrypoint for frontend/dist/text-editor.bundle.js.
//
// The app's frontend is ordered classic scripts, so this bundle publishes one
// narrow global. Only the surface the classic scripts (and the Playwright-hosted
// contract tests) actually need is exported; nothing here leaks CodeMirror,
// parser, or bundler internals.

import {
    EXECUTOR_LIMITS,
    HANDSHAKE_TOKEN,
    OP_FORMAT,
    OP_HANDSHAKE,
    OP_SELFTEST,
    OP_VALIDATE,
    PROTOCOL_VERSION,
    UNAVAILABLE_NOTICE,
    isUnavailableCode,
} from './protocol.js';
import { concise, redactQuoted, DIAGNOSTIC_LIMITS, SEVERITY } from './diagnostics.js';
import { createExecutor, getExecutor, probeWorkerSupport, resetExecutor } from './executor.js';
import { createFallbackExecutor } from './fallback-executor.js';
import { createWorkerExecutor, DEFAULT_WORKER_URL } from './worker-executor.js';
import { TextFileTypes } from './text-file-types.js';
import { TextCodec } from './text-codec.js';
import { createCodeEditorAdapter, MAX_SEARCH_MATCHES } from './code-editor-adapter.js';
import { LanguageModes } from './language-modes.js';
import { SourcePreviewRenderer } from './source-preview.js';
import { TablePreviewRenderer } from './table-preview.js';
import { TABLE_LIMITS, analyzeDelimited } from './delimited.js';

// Grouped rather than exported individually: these exist for the contract tests that
// pin the no-source-echo guarantee at its choke point, not for app code, which only
// ever sees an already-sanitized message.
const DIAGNOSTIC_HELPERS = { concise, redactQuoted };

export {
    EXECUTOR_LIMITS,
    HANDSHAKE_TOKEN,
    OP_FORMAT,
    OP_HANDSHAKE,
    OP_SELFTEST,
    OP_VALIDATE,
    PROTOCOL_VERSION,
    UNAVAILABLE_NOTICE,
    DEFAULT_WORKER_URL,
    // Shared with the classic-script diagnostics state: the drawer's presentation
    // cap and the validators' collection cap are the same contract seen from two
    // sides, so they have exactly one definition.
    DIAGNOSTIC_LIMITS,
    // Exported so the no-source-echo guarantee is testable at its choke point.
    DIAGNOSTIC_HELPERS,
    // Table presentation bounds. Held apart from DIAGNOSTIC_LIMITS on purpose: the
    // CSV parse caps in there are validator resource bounds, while these decide how
    // much of a parsed table is worth laying out.
    TABLE_LIMITS,
    SEVERITY,
    isUnavailableCode,
    createExecutor,
    createFallbackExecutor,
    createWorkerExecutor,
    getExecutor,
    probeWorkerSupport,
    resetExecutor,
    TextFileTypes,
    TextCodec,
    createCodeEditorAdapter,
    MAX_SEARCH_MATCHES,
    // The registry's language modes, seen from both panels: the extension the
    // adapter reconfigures to in Edit, the token stream Source Preview paints, and
    // the non-authoritative Lezer recovery findings for HTML/CSS/JS/TS. One
    // tag→token-class table drives all three, so Edit and Preview cannot drift.
    LanguageModes,
    SourcePreviewRenderer,
    TablePreviewRenderer,
    analyzeDelimited,
};
