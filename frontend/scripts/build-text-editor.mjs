#!/usr/bin/env node
// Builds the two committed text-editor artifacts.
//
// Reproducibility is part of the artifact contract, not a nice-to-have: the
// outputs are committed, embedded into the binary by frontend/embed.go, and
// byte-compared by `npm run check:text-editor-bundle`. That only holds if every
// input is pinned — the esbuild version (exact, non-range, in package.json), the
// entrypoints (named one-per-output, never globbed), and every option below.

import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import * as esbuild from 'esbuild';

const here = path.dirname(fileURLToPath(import.meta.url));
export const FRONTEND_DIR = path.resolve(here, '..');
export const DEFAULT_OUTDIR = path.join(FRONTEND_DIR, 'dist');

// Combined minified budget for both artifacts. CodeMirror core, the language
// packages, and the parsers appear across these two files, and every dependency
// bump re-commits them, so this is a repository-churn constraint as much as a
// runtime one. Exceeding it is a signal to drop a language, not to raise the
// number.
export const SIZE_BUDGET_BYTES = 1_500_000;

// The oldest engine among the surfaces that run this code: WKWebView on macOS,
// WebView2 on Windows, WebKitGTK on Linux, and supported desktop browsers.
//
// es2020 is not an arbitrary floor — the hand-written frontend already uses
// optional chaining and nullish coalescing throughout, so every surface that
// runs the app at all is already an ES2020 engine. The named versions are the
// oldest each vendor shipped with full ES2020 support, except Safari: esbuild
// refuses `safari14` because Safari 14 has a parameter-destructuring bug it
// would have to lower and cannot, so the Safari floor is 15.
const TARGET = ['es2020', 'safari15', 'chrome91', 'firefox91'];

const BANNER = [
    '/*',
    ' * GENERATED FILE — DO NOT EDIT BY HAND.',
    ' * Regenerate with: npm --prefix frontend run build:text-editor',
    ' * Verify with:     npm --prefix frontend run check:text-editor-bundle',
    ' * Source:          frontend/src/text-editor/',
    ' */',
].join('\n');

export const ARTIFACTS = [
    {
        name: 'text-editor.bundle.js',
        entry: path.join(FRONTEND_DIR, 'src/text-editor/index.js'),
        // The app's scripts are classic and ordered, so the bundle publishes one
        // global rather than expecting module semantics.
        globalName: 'MahpastesTextEditor',
        platform: 'browser',
    },
    {
        name: 'text-validator.worker.js',
        entry: path.join(FRONTEND_DIR, 'src/text-editor/worker-entry.js'),
        globalName: undefined,
        platform: 'browser',
    },
];

function optionsFor(artifact, outdir) {
    return {
        entryPoints: [artifact.entry],
        outfile: path.join(outdir, artifact.name),
        bundle: true,
        format: 'iife',
        globalName: artifact.globalName,
        platform: artifact.platform,
        target: TARGET,
        minify: true,
        sourcemap: false,
        // Bundled third-party MIT/BSD notices are licences, not incidental
        // comments; they ship inside the artifact.
        legalComments: 'inline',
        banner: { js: BANNER },
        charset: 'utf8',
        logLevel: 'silent',
        write: false,
        metafile: false,
    };
}

/**
 * Builds every artifact and returns [{ name, path, contents }] without writing.
 */
export async function buildArtifacts(outdir) {
    const built = [];
    for (const artifact of ARTIFACTS) {
        const result = await esbuild.build(optionsFor(artifact, outdir));
        const [output] = result.outputFiles;
        built.push({ name: artifact.name, path: output.path, contents: Buffer.from(output.contents) });
    }
    return built;
}

export async function writeArtifacts(outdir) {
    const built = await buildArtifacts(outdir);
    await mkdir(outdir, { recursive: true });
    for (const artifact of built) await writeFile(artifact.path, artifact.contents);
    return built;
}

export function totalSize(built) {
    return built.reduce((sum, artifact) => sum + artifact.contents.length, 0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
    const built = await writeArtifacts(DEFAULT_OUTDIR);
    for (const artifact of built) {
        console.log(`wrote frontend/dist/${artifact.name} (${artifact.contents.length} bytes)`);
    }
    const total = totalSize(built);
    console.log(`combined ${total} bytes of a ${SIZE_BUDGET_BYTES}-byte budget`);
    if (total > SIZE_BUDGET_BYTES) {
        console.error(`error: combined size ${total} exceeds the ${SIZE_BUDGET_BYTES}-byte budget`);
        process.exit(1);
    }
}
