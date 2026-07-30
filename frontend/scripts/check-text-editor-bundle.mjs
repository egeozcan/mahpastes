#!/usr/bin/env node
// Verifies the committed text-editor artifacts.
//
// This is the net for the case tests cannot catch: a hand-edited or
// partially-rebuilt artifact that behaves plausibly and passes every suite while
// no longer corresponding to its source. It builds into a temporary directory and
// byte-compares, then enforces the combined size budget.

import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { buildArtifacts, DEFAULT_OUTDIR, SIZE_BUDGET_BYTES, totalSize } from './build-text-editor.mjs';

// Both overrides exist so the failure paths themselves are testable: a test can
// verify stale detection against a throwaway copy of dist instead of mutating
// the committed artifacts, and can prove the budget actually fails a build
// without waiting for the real bundle to outgrow it.
const distDir = process.env.MAHPASTES_TEXT_EDITOR_DIST || DEFAULT_OUTDIR;
const budget = process.env.MAHPASTES_TEXT_EDITOR_SIZE_BUDGET
    ? Number(process.env.MAHPASTES_TEXT_EDITOR_SIZE_BUDGET)
    : SIZE_BUDGET_BYTES;

const failures = [];
const scratch = await mkdtemp(path.join(tmpdir(), 'mahpastes-text-editor-'));

try {
    const built = await buildArtifacts(scratch);

    for (const artifact of built) {
        const committedPath = path.join(distDir, artifact.name);
        let committed;
        try {
            committed = await readFile(committedPath);
        } catch {
            failures.push(`frontend/dist/${artifact.name} is missing — run: npm --prefix frontend run build:text-editor`);
            continue;
        }
        if (!committed.equals(artifact.contents)) {
            failures.push(
                `frontend/dist/${artifact.name} does not match a fresh build ` +
                `(committed ${committed.length} bytes, rebuilt ${artifact.contents.length} bytes) — ` +
                'run: npm --prefix frontend run build:text-editor',
            );
            continue;
        }
        console.log(`ok  frontend/dist/${artifact.name} (${artifact.contents.length} bytes)`);
    }

    const total = totalSize(built);
    if (total > budget) {
        failures.push(
            `combined bundle size ${total} bytes exceeds the ${budget}-byte budget by ${total - budget} bytes — ` +
            'drop a language or parser rather than raising the budget',
        );
    } else {
        const pct = Math.round((total / budget) * 100);
        console.log(`ok  combined ${total} bytes / ${budget}-byte budget (${pct}%)`);
    }
} finally {
    await rm(scratch, { recursive: true, force: true });
}

if (failures.length > 0) {
    for (const failure of failures) console.error(`FAIL ${failure}`);
    process.exit(1);
}
