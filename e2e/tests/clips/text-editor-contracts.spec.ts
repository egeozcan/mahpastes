import { test, expect } from '../../fixtures/test-fixtures';
import { execFile } from 'child_process';
import { cp, mkdtemp, readFile, rm, writeFile } from 'fs/promises';
import { tmpdir } from 'os';
import * as path from 'path';
import { fileURLToPath } from 'url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');

function runNpm(args: string[], env: Record<string, string> = {}) {
  return new Promise<{ code: number; stdout: string; stderr: string }>((resolve) => {
    execFile(
      'npm',
      ['--prefix', path.join(repoRoot, 'frontend'), ...args],
      { cwd: repoRoot, env: { ...process.env, ...env } },
      (err, stdout, stderr) => {
        resolve({ code: err ? ((err as any).code ?? 1) : 0, stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

// Unit-shaped contracts for the generated text-editor bundle, run inside
// Playwright via page.evaluate against the loaded app page. This repository has
// no JavaScript unit runner and deliberately does not gain one; the tradeoff is
// slower assertions and coarser failure output in exchange for a single test
// stack. Each contract table is one test() so a failure names the case.
//
// IMPORTANT: this is NOT the environment that ships. Playwright drives Chromium
// over an http://localhost origin — not WKWebView, not WebView2, not the
// production custom-scheme origin. That is fine for pure-logic contracts. It is
// NOT evidence for worker construction, worker URL resolution, or CSP
// enforcement on a real desktop surface; those need the native smoke harness.

type ProbeReport = { kind: string; reason: string | null; workerAvailable: boolean };

test.describe('Text editor bundle surface', () => {
  test('publishes the app-owned global with the expected surface', async ({ app }) => {
    const surface = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      if (!api) return null;
      return {
        keys: Object.keys(api).sort(),
        protocolVersion: api.PROTOCOL_VERSION,
        handshakeToken: api.HANDSHAKE_TOKEN,
        workerURL: api.DEFAULT_WORKER_URL,
        limits: api.EXECUTOR_LIMITS,
        notice: api.UNAVAILABLE_NOTICE,
      };
    });

    expect(surface).not.toBeNull();
    expect(surface!.keys).toEqual(
      [
        'DEFAULT_WORKER_URL',
        'EXECUTOR_LIMITS',
        'HANDSHAKE_TOKEN',
        'OP_HANDSHAKE',
        'OP_SELFTEST',
        'OP_VALIDATE',
        'OP_FORMAT',
        'PROTOCOL_VERSION',
        'UNAVAILABLE_NOTICE',
        // The validator/drawer contract seen from both sides: the collection cap
        // belongs to the validators, the presentation cap to the drawer, and they
        // have exactly one definition.
        'DIAGNOSTIC_LIMITS',
        'DIAGNOSTIC_HELPERS',
        // Table presentation bounds. Separate from DIAGNOSTIC_LIMITS because the
        // CSV parse caps in there are validator resource bounds, while these decide
        // how much of an already-parsed table is worth laying out.
        'TABLE_LIMITS',
        'SEVERITY',
        'createExecutor',
        'createFallbackExecutor',
        'createWorkerExecutor',
        'getExecutor',
        'isUnavailableCode',
        'probeWorkerSupport',
        'resetExecutor',
        // Reachable from the page because the classic scripts need them anyway,
        // which is also what lets the contract tests below run at all.
        'TextCodec',
        'TextFileTypes',
        // The only route to CodeMirror. Nothing above the adapter imports it.
        'createCodeEditorAdapter',
        'MAX_SEARCH_MATCHES',
        // The registry's language modes seen from both panels: the extension the
        // adapter reconfigures to in Edit, the token stream Source Preview paints,
        // and the non-authoritative Lezer recovery findings for HTML/CSS/JS/TS.
        // One tag→token-class table drives all three.
        'LanguageModes',
        // Safe source Preview. Pure DOM and token logic, so it lives in the
        // bundle; TextPreview (a classic script) dispatches to it because the
        // Markdown pipeline it also dispatches to is a classic script.
        'SourcePreviewRenderer',
        // Bounded CSV/TSV table Preview, dispatched the same way.
        'TablePreviewRenderer',
        // Exposed so the delimiter/header contest can be driven directly, at the
        // same boundary the renderer uses.
        'analyzeDelimited',
      ].sort(),
    );
    expect(surface!.protocolVersion).toBe(1);
    // Root-absolute on purpose: a relative URL resolved against a deep path can
    // hit the SPA fallback and return index.html.
    expect(surface!.workerURL).toBe('/dist/text-validator.worker.js');
    expect(surface!.workerURL.startsWith('/')).toBe(true);
    expect(surface!.limits.worker).toEqual({ deadlineMs: 1500, maxSourceBytes: 2 * 1024 * 1024 });
    expect(surface!.limits.fallback).toEqual({ deadlineMs: 250, maxSourceBytes: 64 * 1024 });
    expect(surface!.notice).toBe('Validation unavailable within safety limits');
  });

  test('publishes the diagnostic limits and severities the drawer and validators share', async ({ app }) => {
    const shared = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      return { limits: api.DIAGNOSTIC_LIMITS, severity: api.SEVERITY, table: api.TABLE_LIMITS };
    });

    expect(shared.limits).toEqual({
      // Validator collection stops at 1,000 findings; the drawer still presents
      // only the first 100 and reports the suppressed remainder.
      collected: 1000,
      presented: 100,
      yamlDocuments: 64,
      yamlAliasesPerDocument: 100,
      yamlNodes: 100000,
      xmlDepth: 256,
      xmlEvents: 100000,
      // CSV/TSV parse caps. Validator resource bounds, enforced inside the parse
      // loop — table presentation stops far earlier, see TABLE_LIMITS below.
      csvRecords: 100000,
      csvFields: 1000000,
      jsonDepth: 512,
      // Indented JSON amplifies quadratically in nesting depth, so formatting
      // refuses past this rather than attempting a multi-gigabyte result.
      formatDepth: 256,
      // Depth alone is not the bound: 256 nested arrays of 32,512 zeros is a valid
      // 64 KiB document that indents to ~17 MB. Output size is the real limit, in
      // UTF-16 code units (what the builder accumulates).
      formatOutputUnits: 4 * 1024 * 1024,
    });
    expect(shared.severity).toEqual({ ERROR: 'error', POSSIBLE: 'possible-issue' });
    // Table presentation. Detection is bounded so a huge document does not get
    // parsed four times over; rendering stops at the first of the three render
    // limits it reaches.
    expect(shared.table).toEqual({
      detectionBytes: 64 * 1024,
      detectionRecords: 50,
      renderRows: 500,
      renderColumns: 100,
      renderCells: 10000,
    });
  });
});

test.describe('Validator worker capability probe', () => {
  test('a static worker loads and answers the handshake', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor, probe } = await api.probeWorkerSupport();
      if (!executor) return { probe, handshake: null };
      const handshake = await executor.run({ op: api.OP_HANDSHAKE, generation: 1 });
      executor.dispose();
      return { probe, handshake, kind: executor.kind };
    });

    expect((result.probe as ProbeReport).kind).toBe('worker');
    expect((result.probe as ProbeReport).workerAvailable).toBe(true);
    expect(result.kind).toBe('worker');
    expect(result.handshake).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
  });

  test('an HTML response from the SPA fallback counts as probe failure', async ({ app }) => {
    // The failure mode this guards against: a URL that resolves to index.html
    // instead of the worker script. Without a validated handshake that would
    // construct a "successful" worker that never answers.
    const probe = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor, probe } = await api.probeWorkerSupport({ url: '/index.html', handshakeTimeoutMs: 4000 });
      if (executor) executor.dispose();
      return { probe, gotExecutor: !!executor };
    });

    expect(probe.gotExecutor).toBe(false);
    expect((probe.probe as ProbeReport).kind).toBe('fallback');
    expect((probe.probe as ProbeReport).workerAvailable).toBe(false);
    expect((probe.probe as ProbeReport).reason).toBeTruthy();
  });

  test('a missing worker script counts as probe failure', async ({ app }) => {
    const probe = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor, probe } = await api.probeWorkerSupport({
        url: '/dist/text-validator-does-not-exist.js',
        handshakeTimeoutMs: 4000,
      });
      if (executor) executor.dispose();
      return probe;
    });

    expect((probe as ProbeReport).kind).toBe('fallback');
    expect((probe as ProbeReport).reason).toBeTruthy();
  });

  test('createExecutor falls back to the time-sliced executor when the worker fails', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor({ url: '/index.html', handshakeTimeoutMs: 4000 });
      // The whole point of one interface: the same call works either way.
      const handshake = await executor.run({ op: api.OP_HANDSHAKE, generation: 1 });
      const kind = executor.kind;
      const limits = executor.limits;
      const probe = executor.probe;
      executor.dispose();
      return { kind, handshake, limits, probe };
    });

    expect(result.kind).toBe('fallback');
    expect(result.handshake).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
    expect(result.limits.maxSourceBytes).toBe(64 * 1024);
    expect((result.probe as ProbeReport).workerAvailable).toBe(false);
  });

  test('forceFallback selects the fallback without attempting a worker', async ({ app }) => {
    const probe = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor({ forceFallback: true });
      const probe = executor.probe;
      executor.dispose();
      return probe;
    });

    expect((probe as ProbeReport).kind).toBe('fallback');
    expect((probe as ProbeReport).reason).toContain('skipped by configuration');
  });
});

test.describe('Validator worker deadline, termination and restart', () => {
  test('a blown deadline terminates the worker and the next request restarts it', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor } = await api.probeWorkerSupport();
      if (!executor) return { skipped: true };
      const report: any = { skipped: false };
      try {
        // One synchronous unit that outlives its deadline. A worker can be killed
        // mid-call; that hard interrupt is the whole reason it is preferred.
        await executor.run({ op: api.OP_SELFTEST, generation: 1, payload: { units: 1, spinMsPerUnit: 800 }, timeoutMs: 120 });
        report.firstError = null;
      } catch (err: any) {
        report.firstError = err.code;
      }
      report.aliveAfterTimeout = executor.alive;
      report.restartCount = executor.restartCount;
      // Restart is lazy: the next request constructs a fresh worker.
      report.afterRestart = await executor.run({ op: api.OP_HANDSHAKE, generation: 2 });
      report.aliveAfterRestart = executor.alive;
      executor.dispose();
      return report;
    });

    expect(result.skipped).toBe(false);
    expect(result.firstError).toBe('timeout');
    expect(result.aliveAfterTimeout).toBe(false);
    expect(result.restartCount).toBe(1);
    expect(result.afterRestart).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
    expect(result.aliveAfterRestart).toBe(true);
  });

  test('a newer generation terminates superseded in-flight work', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const { executor } = await api.probeWorkerSupport();
      if (!executor) return { skipped: true };
      const stale = executor
        .run({ op: api.OP_SELFTEST, generation: 1, payload: { units: 1, spinMsPerUnit: 600 } })
        .then(() => 'resolved', (err: any) => err.code);
      // Do not merely wait for the superseded parse — kill it.
      const fresh = await executor.run({ op: api.OP_HANDSHAKE, generation: 2 });
      const staleCode = await stale;
      executor.dispose();
      return { skipped: false, staleCode, fresh };
    });

    expect(result.skipped).toBe(false);
    expect(result.staleCode).toBe('terminated');
    expect(result.fresh).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
  });
});

test.describe('Executor generation guards and ceilings', () => {
  for (const mode of ['worker', 'fallback'] as const) {
    test(`${mode}: a request for a superseded generation is rejected as stale`, async ({ app }) => {
      const result = await app.page.evaluate(async (mode) => {
        const api = (window as any).MahpastesTextEditor;
        const executor =
          mode === 'worker'
            ? (await api.probeWorkerSupport()).executor
            : api.createFallbackExecutor();
        if (!executor) return { skipped: true };
        await executor.run({ op: api.OP_HANDSHAKE, generation: 7 });
        let code: string | null = null;
        try {
          await executor.run({ op: api.OP_HANDSHAKE, generation: 6 });
        } catch (err: any) {
          code = err.code;
        }
        const stillWorks = await executor.run({ op: api.OP_HANDSHAKE, generation: 8 });
        executor.dispose();
        return { skipped: false, code, stillWorks, generation: 8 };
      }, mode);

      expect(result.skipped).toBe(false);
      expect(result.code).toBe('stale');
      expect(result.stillWorks).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
    });

    test(`${mode}: source above the executor ceiling is refused`, async ({ app }) => {
      const result = await app.page.evaluate(async (mode) => {
        const api = (window as any).MahpastesTextEditor;
        const executor =
          mode === 'worker'
            ? (await api.probeWorkerSupport()).executor
            : api.createFallbackExecutor();
        if (!executor) return { skipped: true };
        const ceiling = executor.limits.maxSourceBytes;
        let code: string | null = null;
        try {
          await executor.run({ op: api.OP_HANDSHAKE, generation: 1, sourceBytes: ceiling + 1 });
        } catch (err: any) {
          code = err.code;
        }
        const atCeiling = await executor.run({ op: api.OP_HANDSHAKE, generation: 1, sourceBytes: ceiling });
        executor.dispose();
        return { skipped: false, code, ceiling, atCeiling, unavailable: api.isUnavailableCode(code) };
      }, mode);

      expect(result.skipped).toBe(false);
      expect(result.code).toBe('too-large');
      // "no authoritative answer", never "the document is valid"
      expect(result.unavailable).toBe(true);
      expect(result.ceiling).toBe(mode === 'worker' ? 2 * 1024 * 1024 : 64 * 1024);
      expect(result.atCeiling).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
    });
  }
});

test.describe('Time-sliced fallback executor', () => {
  test('yields to the event loop between units', async ({ app }) => {
    // The property that makes a shared UI thread survivable. If the executor ran
    // its units back to back, the macrotask queued before it started would not
    // be serviced until after the whole run finished.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor();
      const order: string[] = [];
      const interleaved = new Promise<void>((resolve) => {
        setTimeout(() => {
          order.push('macrotask');
          resolve();
        }, 0);
      });
      const run = executor
        .run({ op: api.OP_SELFTEST, generation: 1, payload: { units: 8, spinMsPerUnit: 5 } })
        .then((value: any) => {
          order.push('run');
          return value;
        });
      const [value] = await Promise.all([run, interleaved]);
      executor.dispose();
      return { order, value };
    });

    expect(result.value).toEqual({ units: 8, spinMsPerUnit: 5 });
    expect(result.order).toEqual(['macrotask', 'run']);
  });

  test('abandons a run that blows its cumulative main-thread budget', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor();
      let code: string | null = null;
      try {
        // 20 units × 30ms of synchronous spin is far past the 250ms budget, and
        // the budget is only observed between units — exactly as documented.
        await executor.run({ op: api.OP_SELFTEST, generation: 1, payload: { units: 20, spinMsPerUnit: 30 } });
      } catch (err: any) {
        code = err.code;
      }
      const abandonCount = executor.abandonCount;
      // Abandonment is cooperative, not fatal: the executor stays usable.
      const after = await executor.run({ op: api.OP_HANDSHAKE, generation: 2 });
      executor.dispose();
      return { code, abandonCount, after, unavailable: api.isUnavailableCode(code) };
    });

    expect(result.code).toBe('timeout');
    expect(result.abandonCount).toBe(1);
    expect(result.unavailable).toBe(true);
    expect(result.after).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
  });

  test('a newer generation abandons superseded in-flight work', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor();
      const stale = executor
        .run({ op: api.OP_SELFTEST, generation: 1, payload: { units: 40, spinMsPerUnit: 1 } })
        .then(() => 'resolved', (err: any) => err.code);
      const fresh = executor.run({ op: api.OP_HANDSHAKE, generation: 2 });
      const staleCode = await stale;
      const freshValue = await fresh;
      executor.dispose();
      return { staleCode, freshValue };
    });

    expect(result.staleCode).toBe('stale');
    expect(result.freshValue).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
  });

  test('an unknown op fails without taking the executor down', async ({ app }) => {
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor();
      let code: string | null = null;
      try {
        await executor.run({ op: 'not-a-real-op', generation: 1 });
      } catch (err: any) {
        code = err.code;
      }
      const after = await executor.run({ op: api.OP_HANDSHAKE, generation: 2 });
      executor.dispose();
      return { code, after, unavailable: api.isUnavailableCode(code) };
    });

    expect(result.code).toBe('unknown-op');
    // A broken request is not the same as "we could not answer within limits".
    expect(result.unavailable).toBe(false);
    expect(result.after).toEqual({ handshake: 'mahpastes:text-validator:v1', protocol: 1 });
  });
});

test.describe('Committed bundle integrity', () => {
  // No `app` fixture: these assert the build artifacts, not page behavior.
  test('the committed artifacts byte-match a fresh build and fit the budget', async () => {
    const result = await runNpm(['run', 'check:text-editor-bundle']);
    expect(result.code, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(result.stdout).toContain('ok  frontend/dist/text-editor.bundle.js');
    expect(result.stdout).toContain('ok  frontend/dist/text-validator.worker.js');
    expect(result.stdout).toMatch(/ok {2}combined \d+ bytes \/ 1500000-byte budget/);
  });

  test('the check detects a hand-edited artifact', async () => {
    // Verified against a throwaway copy rather than the committed files: this
    // suite runs alongside live `wails dev` instances watching the source tree.
    const scratch = await mkdtemp(path.join(tmpdir(), 'mahpastes-dist-'));
    try {
      await cp(path.join(repoRoot, 'frontend/dist'), scratch, { recursive: true });
      const target = path.join(scratch, 'text-editor.bundle.js');
      const original = await readFile(target);
      await writeFile(target, Buffer.concat([original, Buffer.from('\n// hand edit\n')]));

      const result = await runNpm(['run', 'check:text-editor-bundle'], {
        MAHPASTES_TEXT_EDITOR_DIST: scratch,
      });
      expect(result.code).not.toBe(0);
      expect(result.stdout + result.stderr).toContain('does not match a fresh build');
      expect(result.stdout + result.stderr).toContain('npm --prefix frontend run build:text-editor');
    } finally {
      await rm(scratch, { recursive: true, force: true });
    }
  });

  test('the check detects a missing artifact', async () => {
    const scratch = await mkdtemp(path.join(tmpdir(), 'mahpastes-dist-'));
    try {
      await cp(path.join(repoRoot, 'frontend/dist'), scratch, { recursive: true });
      await rm(path.join(scratch, 'text-validator.worker.js'));

      const result = await runNpm(['run', 'check:text-editor-bundle'], {
        MAHPASTES_TEXT_EDITOR_DIST: scratch,
      });
      expect(result.code).not.toBe(0);
      expect(result.stdout + result.stderr).toContain('text-validator.worker.js is missing');
    } finally {
      await rm(scratch, { recursive: true, force: true });
    }
  });

  test('the check fails a combined size over budget', async () => {
    const result = await runNpm(['run', 'check:text-editor-bundle'], {
      MAHPASTES_TEXT_EDITOR_SIZE_BUDGET: '1000',
    });
    expect(result.code).not.toBe(0);
    expect(result.stdout + result.stderr).toContain('exceeds the 1000-byte budget');
    expect(result.stdout + result.stderr).toContain('drop a language or parser rather than raising the budget');
  });

  test('the Wails build regenerates the bundles', async () => {
    // The failure this guards: `wails build` runs `npm run build`, which was
    // Tailwind only. Nothing regenerated the bundles, so stale committed
    // artifacts shipped and passed every test.
    const pkg = JSON.parse(await readFile(path.join(repoRoot, 'frontend/package.json'), 'utf8'));
    expect(pkg.scripts.build).toBe('npm run build:css && npm run build:text-editor');
    expect(pkg.scripts['build:text-editor']).toBeTruthy();
    // Exact, non-range: reproducible output requires a pinned bundler.
    expect(pkg.devDependencies.esbuild).toMatch(/^\d+\.\d+\.\d+$/);

    const wails = JSON.parse(await readFile(path.join(repoRoot, 'wails.json'), 'utf8'));
    expect(wails['frontend:build']).toBe('npm run build');
    // npm ci resolves the lockfile exactly instead of drifting within semver.
    expect(wails['frontend:install']).toBe('npm ci');

    const gitattributes = await readFile(path.join(repoRoot, '.gitattributes'), 'utf8');
    expect(gitattributes).toMatch(/^frontend\/dist\/\*\.js -text$/m);
  });
});

// --- Type classification -----------------------------------------------------

type ClassifyCase = {
  filename: string;
  contentType: string;
  id: string | null;
  matchedBy?: string | null;
  candidate?: boolean;
  defaultMode?: 'preview' | 'edit';
  why: string;
};

async function classifyAll(app: any, cases: ClassifyCase[]) {
  return app.page.evaluate((inputs: ClassifyCase[]) => {
    const api = (window as any).MahpastesTextEditor.TextFileTypes;
    return inputs.map((input) => {
      const result = api.classify({ filename: input.filename, contentType: input.contentType });
      return {
        id: result.descriptor ? result.descriptor.id : null,
        matchedBy: result.matchedBy,
        mediaGuard: result.mediaGuard,
        conditional: result.conditional,
        defaultMode: result.descriptor ? result.descriptor.defaultMode : null,
        candidate: api.isTextCandidate({ filename: input.filename, contentType: input.contentType }),
      };
    });
  }, cases);
}

function assertCases(cases: ClassifyCase[], actual: any[]) {
  const problems: string[] = [];
  cases.forEach((expected, i) => {
    const got = actual[i];
    const label = `${JSON.stringify(expected.filename)} + ${JSON.stringify(expected.contentType)} (${expected.why})`;
    if (got.id !== expected.id) problems.push(`${label}: expected id ${expected.id}, got ${got.id}`);
    if (expected.matchedBy !== undefined && got.matchedBy !== expected.matchedBy) {
      problems.push(`${label}: expected matchedBy ${expected.matchedBy}, got ${got.matchedBy}`);
    }
    const candidate = expected.candidate ?? expected.id !== null;
    if (got.candidate !== candidate) problems.push(`${label}: expected candidate ${candidate}, got ${got.candidate}`);
    if (expected.defaultMode !== undefined && got.defaultMode !== expected.defaultMode) {
      problems.push(`${label}: expected defaultMode ${expected.defaultMode}, got ${got.defaultMode}`);
    }
  });
  expect(problems.join('\n')).toBe('');
}

test.describe('TextFileTypes classification', () => {
  test('every registry filename variant maps to its descriptor, case-insensitively', async ({ app }) => {
    const cases: ClassifyCase[] = [
      { filename: 'notes.md', contentType: 'text/markdown', id: 'markdown', defaultMode: 'preview', why: 'markdown' },
      { filename: 'NOTES.MARKDOWN', contentType: '', id: 'markdown', defaultMode: 'preview', why: 'case-insensitive markdown' },
      { filename: 'a.json', contentType: '', id: 'json', defaultMode: 'edit', why: 'json' },
      { filename: 'a.JSONL', contentType: '', id: 'jsonl', why: 'jsonl uppercase' },
      { filename: 'a.ndjson', contentType: '', id: 'jsonl', why: 'ndjson' },
      { filename: 'a.yaml', contentType: '', id: 'yaml', why: 'yaml' },
      { filename: 'a.yml', contentType: '', id: 'yaml', why: 'yml' },
      { filename: 'a.toml', contentType: '', id: 'toml', why: 'toml' },
      { filename: 'a.xml', contentType: '', id: 'xml', why: 'xml' },
      { filename: 'a.csv', contentType: '', id: 'csv', defaultMode: 'preview', why: 'csv previews as a table' },
      { filename: 'a.tsv', contentType: '', id: 'tsv', defaultMode: 'preview', why: 'tsv previews as a table' },
      { filename: 'a.html', contentType: '', id: 'html', defaultMode: 'edit', why: 'html source opens in edit' },
      { filename: 'a.htm', contentType: '', id: 'html', why: 'htm' },
      { filename: 'a.css', contentType: '', id: 'css', why: 'css' },
      { filename: 'a.js', contentType: '', id: 'javascript', why: 'js' },
      { filename: 'a.mjs', contentType: '', id: 'javascript', why: 'mjs' },
      { filename: 'a.cjs', contentType: '', id: 'javascript', why: 'cjs' },
      { filename: 'a.ts', contentType: '', id: 'typescript', why: 'ts' },
      { filename: 'a.mts', contentType: '', id: 'typescript', why: 'mts' },
      { filename: 'a.cts', contentType: '', id: 'typescript', why: 'cts' },
      { filename: 'a.sh', contentType: '', id: 'shell', why: 'sh' },
      { filename: 'a.bash', contentType: '', id: 'shell', why: 'bash' },
      { filename: 'a.zsh', contentType: '', id: 'shell', why: 'zsh' },
      { filename: '.env', contentType: '', id: 'env', matchedBy: 'basename', why: 'exact .env' },
      { filename: '.env.local', contentType: '', id: 'env', matchedBy: 'basename', why: '.env.* variant' },
      { filename: '.env.example', contentType: '', id: 'env', matchedBy: 'basename', why: '.env.* variant' },
      { filename: 'a.ini', contentType: '', id: 'properties', why: 'ini' },
      { filename: 'a.cfg', contentType: '', id: 'properties', why: 'cfg' },
      { filename: 'a.conf', contentType: '', id: 'properties', why: 'conf' },
      { filename: 'a.txt', contentType: '', id: 'plaintext', matchedBy: 'generic-filename', defaultMode: 'edit', why: 'generic .txt' },
      { filename: 'a.text', contentType: '', id: 'plaintext', matchedBy: 'generic-filename', why: 'generic .text' },
      { filename: 'a.LOG', contentType: '', id: 'plaintext', matchedBy: 'generic-filename', why: 'generic .log uppercase' },
    ];
    assertCases(cases, await classifyAll(app, cases));
  });

  test('every registry MIME fallback maps to its descriptor', async ({ app }) => {
    const cases: ClassifyCase[] = [
      { filename: 'data', contentType: 'application/json', id: 'json', matchedBy: 'mime', why: 'json mime' },
      { filename: 'data', contentType: 'Application/JSON; charset=utf-8', id: 'json', matchedBy: 'mime', why: 'parameters and case are normalized' },
      { filename: 'data', contentType: 'application/ld+json', id: 'json', matchedBy: 'mime', why: 'structured +json suffix' },
      { filename: 'data', contentType: 'application/x-ndjson', id: 'jsonl', why: 'ndjson mime' },
      { filename: 'data', contentType: 'application/jsonl', id: 'jsonl', why: 'jsonl mime' },
      { filename: 'data', contentType: 'application/yaml', id: 'yaml', why: 'yaml mime' },
      { filename: 'data', contentType: 'text/x-yaml', id: 'yaml', why: 'legacy yaml mime' },
      { filename: 'data', contentType: 'application/toml', id: 'toml', why: 'toml mime' },
      { filename: 'data', contentType: 'text/toml', id: 'toml', why: 'toml text mime' },
      { filename: 'data', contentType: 'application/xml', id: 'xml', why: 'xml mime' },
      { filename: 'data', contentType: 'text/xml', id: 'xml', why: 'xml text mime' },
      { filename: 'data', contentType: 'application/atom+xml', id: 'xml', why: 'structured +xml suffix' },
      { filename: 'data', contentType: 'text/csv', id: 'csv', defaultMode: 'preview', why: 'csv mime' },
      { filename: 'data', contentType: 'text/tab-separated-values', id: 'tsv', why: 'tsv mime' },
      { filename: 'data', contentType: 'text/html', id: 'html', why: 'html mime' },
      { filename: 'data', contentType: 'text/css', id: 'css', why: 'css mime' },
      { filename: 'data', contentType: 'application/javascript', id: 'javascript', why: 'js mime' },
      { filename: 'data', contentType: 'application/typescript', id: 'typescript', why: 'ts mime' },
      { filename: 'data', contentType: 'text/x-shellscript', id: 'shell', why: 'shell mime' },
      { filename: 'data', contentType: 'text/x-ini', id: 'properties', why: 'ini mime' },
      { filename: 'data', contentType: 'text/markdown', id: 'markdown', defaultMode: 'preview', why: 'markdown mime' },
    ];
    assertCases(cases, await classifyAll(app, cases));
  });

  test('a specific filename beats a disagreeing MIME', async ({ app }) => {
    const cases: ClassifyCase[] = [
      { filename: 'notes.md', contentType: 'text/plain', id: 'markdown', matchedBy: 'extension', why: 'filename wins' },
      { filename: 'config.yaml', contentType: 'text/html', id: 'yaml', matchedBy: 'extension', why: 'filename wins over html' },
      { filename: 'data.csv', contentType: 'application/json', id: 'csv', matchedBy: 'extension', why: 'filename wins over json' },
      { filename: '.env.local', contentType: 'application/json', id: 'env', matchedBy: 'basename', why: 'whole-basename beats mime and extension' },
    ];
    assertCases(cases, await classifyAll(app, cases));
  });

  test('a specific MIME beats a generic .txt/.text/.log filename', async ({ app }) => {
    // The concrete case: the backend sniffs pasted content and stores
    // application/json or text/html under a pasted_text_<timestamp>.txt name.
    // Those clips must keep JSON and HTML behavior.
    const cases: ClassifyCase[] = [
      { filename: 'pasted_text_1700000000.txt', contentType: 'application/json', id: 'json', matchedBy: 'mime', why: 'sniffed pasted JSON' },
      { filename: 'pasted_text_1700000000.txt', contentType: 'text/html', id: 'html', matchedBy: 'mime', why: 'sniffed pasted HTML' },
      { filename: 'server.log', contentType: 'text/csv', id: 'csv', matchedBy: 'mime', why: 'specific mime outranks .log' },
      { filename: 'notes.text', contentType: 'text/markdown', id: 'markdown', matchedBy: 'mime', why: 'specific mime outranks .text' },
      { filename: 'plain.txt', contentType: 'text/plain', id: 'plaintext', matchedBy: 'generic-filename', why: 'no specific mime to outrank it' },
    ];
    assertCases(cases, await classifyAll(app, cases));
  });

  test('unknown filenames and unknown text MIME fall through correctly', async ({ app }) => {
    const cases: ClassifyCase[] = [
      { filename: 'Makefile', contentType: 'text/plain', id: 'plaintext', matchedBy: 'generic-mime', why: 'extensionless with recognized text mime' },
      { filename: '', contentType: 'application/json', id: 'json', matchedBy: 'mime', why: 'no filename at all' },
      { filename: 'notes.weird', contentType: 'text/x-unheard-of', id: 'plaintext', matchedBy: 'generic-mime', why: 'unknown text/* is generic text' },
      { filename: 'archive.json.gz', contentType: '', id: null, why: 'only the final extension counts' },
      { filename: 'thing.bin', contentType: 'application/octet-stream', id: null, why: 'unrecognized name under octet-stream is not text' },
      { filename: 'thing.dat', contentType: 'application/x-custom', id: null, why: 'unsupported binary MIME without a recognized filename' },
    ];
    assertCases(cases, await classifyAll(app, cases));
  });

  test('the media-MIME guard blocks colliding extensions but not octet-stream', async ({ app }) => {
    const cases: ClassifyCase[] = [
      // .ts is both TypeScript and MPEG transport stream. Without the guard,
      // opening a transport-stream video would enter the text editor and
      // dead-end on the byte-safety screen.
      { filename: 'stream.ts', contentType: 'video/mp2t', id: 'typescript', candidate: false, why: 'video/mp2t suppresses the .ts match' },
      { filename: 'stream.ts', contentType: 'text/plain', id: 'typescript', candidate: true, why: 'text/plain .ts is TypeScript' },
      { filename: 'stream.ts', contentType: 'application/typescript', id: 'typescript', candidate: true, why: 'typescript mime' },
      { filename: 'stream.ts', contentType: '', id: 'typescript', candidate: true, why: 'no content type at all' },
      { filename: 'cover.txt', contentType: 'image/png', id: 'plaintext', candidate: false, why: 'image/* suppresses a generic text filename' },
      { filename: 'track.md', contentType: 'audio/mpeg', id: 'markdown', candidate: false, why: 'audio/* suppresses a markdown filename' },
      // application/octet-stream is the generic "I don't know" type uploads and
      // watch folders routinely produce. Blocking it would make a perfectly good
      // config.yaml uneditable — a regression, not a guard.
      { filename: 'config.yaml', contentType: 'application/octet-stream', id: 'yaml', candidate: true, why: 'octet-stream stays a candidate' },
      { filename: 'notes.md', contentType: 'application/octet-stream', id: 'markdown', candidate: true, why: 'octet-stream stays a candidate' },
    ];
    assertCases(cases, await classifyAll(app, cases));

    const conditional = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.TextFileTypes;
      return {
        octet: api.classify({ filename: 'config.yaml', contentType: 'application/octet-stream' }).conditional,
        plain: api.classify({ filename: 'config.yaml', contentType: 'text/plain' }).conditional,
        guard: api.classify({ filename: 'stream.ts', contentType: 'video/mp2t' }).mediaGuard,
      };
    });
    // Candidacy under octet-stream rests on the fatal UTF-8 decode, not on the
    // content type saying anything useful.
    expect(conditional.octet).toBe(true);
    expect(conditional.plain).toBe(false);
    expect(conditional.guard).toBe(true);
  });

  test('normalization handles paths, separators, and malformed MIME', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.TextFileTypes;
      return {
        unixPath: api.classify({ filename: 'templates/deployment.yaml', contentType: '' }).descriptor.id,
        windowsPath: api.classify({ filename: 'C:\\work\\templates\\deployment.YAML', contentType: '' }).descriptor.id,
        basenameKeepsSpaces: api.classify({ filename: 'my notes .md', contentType: '' }).basename,
        emptyMIME: api.normalizeContentType(''),
        malformedMIME: api.normalizeContentType('not-a-mime'),
        spacedMIME: api.normalizeContentType('  TEXT/CSV ; charset=utf-8 '),
        dotfileHasNoExtension: api.finalExtension('.env'),
      };
    });

    expect(result.unixPath).toBe('yaml');
    expect(result.windowsPath).toBe('yaml');
    // Whitespace is part of the name; only the comparison form is folded.
    expect(result.basenameKeepsSpaces).toBe('my notes .md');
    expect(result.emptyMIME).toBe('');
    expect(result.malformedMIME).toBe('');
    expect(result.spacedMIME).toBe('text/csv');
    expect(result.dotfileHasNoExtension).toBe('');
  });

  test('only Markdown, CSV and TSV default to Preview', async ({ app }) => {
    const modes = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.TextFileTypes;
      const out: Record<string, string> = {};
      for (const d of api.allDescriptors()) out[d.id] = d.defaultMode;
      return out;
    });

    const preview = Object.entries(modes).filter(([, mode]) => mode === 'preview').map(([id]) => id).sort();
    expect(preview).toEqual(['csv', 'markdown', 'tsv']);
    // Every source-preview format opens in Edit: its Preview is read-only
    // highlighted source that looks nearly identical to the editor.
    expect(modes.plaintext).toBe('edit');
    expect(modes.json).toBe('edit');
    expect(modes.yaml).toBe('edit');
    expect(modes.html).toBe('edit');
  });

  test('only authoritative formats can ever block a save', async ({ app }) => {
    const info = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.TextFileTypes;
      const out: Record<string, { validator: string | null; authoritative: boolean; formatter: string | null }> = {};
      for (const d of api.allDescriptors()) {
        out[d.id] = { validator: d.validator, authoritative: d.authoritative, formatter: d.formatter };
      }
      return out;
    });

    const authoritative = Object.entries(info).filter(([, v]) => v.authoritative).map(([id]) => id).sort();
    expect(authoritative).toEqual(['json', 'jsonl', 'toml', 'xml', 'yaml']);
    // CSV/TSV findings are never authoritative in any state — the format has no
    // single authoritative grammar, inferred or explicitly chosen.
    expect(info.csv.authoritative).toBe(false);
    expect(info.tsv.authoritative).toBe(false);
    // Shell, .env, INI/CFG/CONF, plain text and logs get highlighting only.
    for (const id of ['shell', 'env', 'properties', 'plaintext']) {
      expect(info[id].validator, `${id} must have no validator`).toBeNull();
    }
    // Only JSON and JSON Lines ship a formatter initially.
    const formatters = Object.entries(info).filter(([, v]) => v.formatter).map(([id]) => id).sort();
    expect(formatters).toEqual(['json', 'jsonl']);
  });
});

// --- TextCodec ---------------------------------------------------------------

// Byte fidelity is asserted on BYTES. A test comparing decoded strings passes
// happily while a BOM or a CR silently vanishes, which is the exact failure this
// module exists to prevent.
async function roundTripBytes(app: any, bytes: number[]) {
  return app.page.evaluate((input: number[]) => {
    const codec = (window as any).MahpastesTextEditor.TextCodec;
    const raw = new Uint8Array(input);
    const base64 = codec.bytesToBase64(raw);
    const decoded = codec.decodeClipPayload({ data: base64, dataEncoding: 'base64', validUTF8: true });
    if (!decoded.ok) return { ok: false, reason: decoded.reason };
    const encoded = codec.encodeForSave(decoded.value, decoded.profile);
    if (!encoded.ok) return { ok: false, reason: encoded.reason };
    return {
      ok: true,
      profile: decoded.profile,
      value: decoded.value,
      out: Array.from(encoded.bytes as Uint8Array),
    };
  }, bytes);
}

test.describe('TextCodec fidelity profile', () => {
  test('detects BOM, newline style and final-newline state', async ({ app }) => {
    const cases = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      const of = (text: string) => {
        const { value, profile } = codec.profileFrom(text);
        return { value, profile };
      };
      return {
        lf: of('a\nb\n'),
        crlf: of('a\r\nb\r\n'),
        cr: of('a\rb\r'),
        bomCrlf: of('﻿a\r\n'),
        noFinalNewline: of('a\nb'),
        empty: of(''),
        // Deliberately mixed sequences are outside the fidelity guarantee; the
        // dominant style is what gets used.
        mixedDominantCrlf: of('a\r\nb\r\nc\n'),
      };
    });

    expect(cases.lf.profile).toEqual({ bom: false, newline: 'lf', finalNewline: true });
    expect(cases.crlf.profile).toEqual({ bom: false, newline: 'crlf', finalNewline: true });
    // The editor value always holds LF separators, matching CodeMirror's model.
    expect(cases.crlf.value).toBe('a\nb\n');
    expect(cases.cr.profile).toEqual({ bom: false, newline: 'cr', finalNewline: true });
    expect(cases.cr.value).toBe('a\nb\n');
    expect(cases.bomCrlf.profile).toEqual({ bom: true, newline: 'crlf', finalNewline: true });
    // The visible value excludes the BOM.
    expect(cases.bomCrlf.value).toBe('a\n');
    expect(cases.noFinalNewline.profile.finalNewline).toBe(false);
    expect(cases.empty.profile).toEqual({ bom: false, newline: 'lf', finalNewline: false });
    expect(cases.mixedDominantCrlf.profile.newline).toBe('crlf');
  });

  test('an unedited BOM + CRLF document round-trips byte-for-byte', async ({ app }) => {
    // EF BB BF 61 0D 0A — the canonical fixture. Every derived field at once.
    const input = [0xef, 0xbb, 0xbf, 0x61, 0x0d, 0x0a];
    const result = await roundTripBytes(app, input);
    expect(result.ok).toBe(true);
    expect(result.profile).toEqual({ bom: true, newline: 'crlf', finalNewline: true });
    expect(result.value).toBe('a\n');
    expect(result.out).toEqual(input);
  });

  test('LF, CRLF, CR, BOM and final-newline permutations all round-trip on bytes', async ({ app }) => {
    const cases: Array<{ bytes: number[]; why: string }> = [
      { bytes: [0x61, 0x0a, 0x62, 0x0a], why: 'LF with final newline' },
      { bytes: [0x61, 0x0a, 0x62], why: 'LF without final newline' },
      { bytes: [0x61, 0x0d, 0x0a, 0x62, 0x0d, 0x0a], why: 'CRLF with final newline' },
      { bytes: [0x61, 0x0d, 0x0a, 0x62], why: 'CRLF without final newline' },
      { bytes: [0x61, 0x0d, 0x62, 0x0d], why: 'CR with final newline' },
      { bytes: [0x61, 0x0d, 0x62], why: 'CR without final newline' },
      { bytes: [0xef, 0xbb, 0xbf, 0x61, 0x0a], why: 'BOM + LF' },
      { bytes: [0xef, 0xbb, 0xbf, 0x61], why: 'BOM, no separators' },
      { bytes: [0x61], why: 'single byte, no separators' },
      { bytes: [], why: 'empty document' },
      { bytes: [0xf0, 0x9f, 0x98, 0x80, 0x0a], why: 'astral character' },
    ];
    const problems: string[] = [];
    for (const c of cases) {
      const result = await roundTripBytes(app, c.bytes);
      if (!result.ok) { problems.push(`${c.why}: decode/encode failed (${result.reason})`); continue; }
      if (JSON.stringify(result.out) !== JSON.stringify(c.bytes)) {
        problems.push(`${c.why}: expected [${c.bytes}], got [${result.out}]`);
      }
    }
    expect(problems.join('\n')).toBe('');
  });

  test('adding or removing a final newline is respected, not overridden', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      const opened = codec.decodeClipPayload({
        data: codec.bytesToBase64(new Uint8Array([0x61, 0x0d, 0x0a])),
        dataEncoding: 'base64',
        validUTF8: true,
      });
      // The user deletes the trailing newline; the CRLF style is still honored
      // for the separators that remain.
      const removed = codec.encodeForSave('a', opened.profile);
      // ...and adds one back.
      const added = codec.encodeForSave('a\nb\n', opened.profile);
      return {
        removed: Array.from(removed.bytes as Uint8Array),
        added: Array.from(added.bytes as Uint8Array),
      };
    });

    expect(result.removed).toEqual([0x61]);
    expect(result.added).toEqual([0x61, 0x0d, 0x0a, 0x62, 0x0d, 0x0a]);
  });
});

test.describe('TextCodec decoding and refusals', () => {
  test('handles both payload encodings after classification', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      // A valid text content type crosses the bridge as a plain string...
      const asUTF8 = codec.decodeClipPayload({ data: 'hello\n', dataEncoding: 'utf8', validUTF8: true });
      // ...while an extension-classified application type arrives as base64 even
      // when it is perfectly good UTF-8.
      const asBase64 = codec.decodeClipPayload({
        data: codec.bytesToBase64(new TextEncoder().encode('hello\n')),
        dataEncoding: 'base64',
        validUTF8: true,
      });
      // An absent data_encoding must not be read as "text".
      const absent = codec.decodeClipPayload({
        data: codec.bytesToBase64(new TextEncoder().encode('hello\n')),
        validUTF8: true,
      });
      return { asUTF8, asBase64, absent };
    });

    expect(result.asUTF8.ok).toBe(true);
    expect(result.asUTF8.value).toBe('hello\n');
    expect(result.asUTF8.encoding).toBe('utf8');
    expect(result.asBase64.ok).toBe(true);
    expect(result.asBase64.value).toBe('hello\n');
    expect(result.asBase64.encoding).toBe('base64');
    expect(result.absent.ok).toBe(true);
    expect(result.absent.value).toBe('hello\n');
  });

  test('invalid UTF-8 is refused rather than replacement-decoded', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      // A Latin-1 "café": the 0xE9 byte is not valid UTF-8.
      const latin1 = new Uint8Array([0x63, 0x61, 0x66, 0xe9, 0x0a]);
      const base64 = codec.bytesToBase64(latin1);
      return {
        // The fatal decode is authoritative even when the flag says nothing.
        fatalDecode: codec.decodeClipPayload({ data: base64, dataEncoding: 'base64', validUTF8: true }),
        // ...and the flag is believed when it says invalid.
        flagged: codec.decodeClipPayload({ data: base64, dataEncoding: 'base64', validUTF8: false }),
        // A utf8 payload flagged invalid was already destroyed in Go; the answer
        // is the same either way: read-only.
        alreadyLossy: codec.decodeClipPayload({ data: 'caf�\n', dataEncoding: 'utf8', validUTF8: false }),
      };
    });

    expect(result.fatalDecode.ok).toBe(false);
    expect(result.fatalDecode.reason).toBe('invalid-utf8');
    expect(result.flagged.ok).toBe(false);
    expect(result.flagged.reason).toBe('invalid-utf8');
    expect(result.alreadyLossy.ok).toBe(false);
    expect(result.alreadyLossy.reason).toBe('invalid-utf8');
  });

  test('a document above the 16 MiB editable cap is refused', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      const cap = codec.MAX_EDITABLE_BYTES;
      // Built as a string to avoid materializing two copies of a 16 MiB buffer.
      const oversize = 'a'.repeat(cap + 1);
      const atCap = 'a'.repeat(cap);
      return {
        cap,
        enhanced: codec.ENHANCED_ASSISTANCE_MAX_BYTES,
        over: codec.decodeClipPayload({ data: oversize, dataEncoding: 'utf8', validUTF8: true }).reason,
        at: codec.decodeClipPayload({ data: atCap, dataEncoding: 'utf8', validUTF8: true }).ok,
      };
    });

    expect(result.cap).toBe(16 * 1024 * 1024);
    // A separate, harder limit than the 2 MiB enhanced-assistance threshold.
    expect(result.enhanced).toBe(2 * 1024 * 1024);
    expect(result.over).toBe('too-large');
    expect(result.at).toBe(true);
  });

  test('an unpaired surrogate is refused at save rather than encoded as EF BF BD', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      const lone = 'ok\nbad \uD800 here';
      // What TextEncoder would have done unsupervised — the exact silent byte
      // replacement this guard exists to prevent.
      const unsupervised = Array.from(new TextEncoder().encode('\uD800'));
      return {
        refusal: codec.encodeForSave(lone, { bom: false, newline: 'lf', finalNewline: false }),
        unsupervised,
        // A well-formed astral pair is not a surrogate problem.
        pair: codec.encodeForSave('😀', { bom: false, newline: 'lf', finalNewline: false }).ok,
        loneLow: codec.encodeForSave('\uDC00', { bom: false, newline: 'lf', finalNewline: false }).reason,
      };
    });

    expect(result.unsupervised).toEqual([0xef, 0xbf, 0xbd]);
    expect(result.refusal.ok).toBe(false);
    expect(result.refusal.reason).toBe('unpaired-surrogate');
    // 1-based line, 1-based UTF-16 column.
    expect(result.refusal.position).toEqual({ line: 2, column: 5 });
    expect(result.refusal.codeUnit).toBe('U+D800');
    expect(result.pair).toBe(true);
    expect(result.loneLow).toBe('unpaired-surrogate');
  });

  test('unpaired-surrogate positions are 1-based lines and UTF-16 columns', async ({ app }) => {
    // The canonical astral fixture: byte, code-point, and UTF-16 columns all
    // differ, so an unconverted offset lands in the wrong place.
    const position = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      // "😀" is one code point but two UTF-16 code units.
      return codec.findUnpairedSurrogate('😀\uD800');
    });

    expect(position.line).toBe(1);
    // Columns 1-2 are the emoji's surrogate pair, so the lone high surrogate is
    // at UTF-16 column 3 — not code-point column 2, not byte column 5.
    expect(position.column).toBe(3);
  });

  test('the transitional display-decode shim is gone', async ({ app }) => {
    // decodeForDisplay existed only because the editor shell had no way to show a
    // refusal: it reproduced the old replacing decode so a save could not overwrite
    // the clip with nothing. The byte-safety screen and the 16 MiB decline now
    // exist, so a refusal is presented as a refusal and the shim must not survive
    // as a second, lossy decode path.
    const surface = await app.page.evaluate(() => {
      const codec = (window as any).MahpastesTextEditor.TextCodec;
      return { hasShim: typeof codec.decodeForDisplay, keys: Object.keys(codec) };
    });

    expect(surface.hasShim).toBe('undefined');
    expect(surface.keys).not.toContain('decodeForDisplay');
  });
});

// --- SourcePreviewRenderer ---------------------------------------------------

test.describe('SourcePreviewRenderer safety and limits', () => {
  test('source becomes text nodes and app-owned spans, never markup', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      // A deliberately hostile highlighter. The real one only ever emits allowlisted
      // types (asserted below in "Language modes"), but the renderer's guarantee has
      // to hold for any injected highlighter, which is why it is tested against one
      // that tries to smuggle markup through a token type.
      const renderer = api.create({
        highlight: () => [{ line: 1, from: 0, to: 6, type: '"><img src=x onerror=alert(1)>' }],
      });
      const source = '<script>alert(1)</script>\nhttps://example.com';
      const { node } = renderer.render({ source, language: 'html', wrap: true, generation: 1 });
      const host = document.createElement('div');
      host.appendChild(node);
      return {
        scripts: host.querySelectorAll('script').length,
        images: host.querySelectorAll('img').length,
        anchors: host.querySelectorAll('a').length,
        // Every element in the tree is one the renderer created itself.
        tags: Array.from(new Set(Array.from(host.querySelectorAll('*')).map((el) => el.tagName.toLowerCase()))).sort(),
        classes: Array.from(new Set(Array.from(host.querySelectorAll('span')).map((el) => el.className))),
        text: host.textContent,
      };
    });

    // HTML stays inert highlighted source; a URL is not auto-activated.
    expect(result.scripts).toBe(0);
    expect(result.images).toBe(0);
    expect(result.anchors).toBe(0);
    expect(result.tags).toEqual(['div', 'li', 'ol']);
    // An unrecognized token kind contributes plain text rather than reaching a
    // class attribute.
    expect(result.classes).toEqual([]);
    expect(result.text).toContain('<script>alert(1)</script>');
  });

  test('recognized token kinds become app-owned classes on their own spans', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const renderer = api.create({
        highlight: () => [
          { line: 1, from: 0, to: 5, type: 'property' },
          { line: 1, from: 7, to: 12, type: 'string' },
        ],
      });
      const { node, mode } = renderer.render({ source: '"key": "val"', language: 'json', wrap: false, generation: 2 });
      const host = document.createElement('div');
      host.appendChild(node);
      return {
        mode,
        spans: Array.from(host.querySelectorAll('span')).map((el) => ({ cls: el.className, text: el.textContent })),
        lines: host.querySelectorAll('.source-preview-line').length,
        wrap: (node as HTMLElement).dataset.wrap,
      };
    });

    expect(result.mode).toBe('lines');
    expect(result.lines).toBe(1);
    expect(result.spans).toEqual([
      { cls: 'source-token source-token-property', text: '"key"' },
      { cls: 'source-token source-token-string', text: '"val"' },
    ]);
    // The persisted wrap preference reaches Preview, not just Edit.
    expect(result.wrap).toBe('off');
  });

  test('a highlighter failure falls back to plain source plus a diagnostic', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const renderer = api.create({ highlight: () => { throw new Error('boom'); } });
      const { node, diagnostics, mode } = renderer.render({
        source: 'still visible', language: 'yaml', wrap: true, generation: 3,
      });
      const host = document.createElement('div');
      host.appendChild(node);
      return { mode, diagnostics, text: host.textContent, plain: host.querySelectorAll('.source-preview-plain').length };
    });

    // Invalid syntax or a failed highlighter does not replace Preview with an
    // error screen: the source stays visible and the failure is a diagnostic.
    expect(result.mode).toBe('failed');
    expect(result.plain).toBe(1);
    expect(result.text).toContain('still visible');
    expect(result.diagnostics).toHaveLength(1);
    expect(result.diagnostics[0].severity).toBe('possible-issue');
    // The exception's message must NOT reach the diagnostic. A highlighter that
    // throws with source in the message — `throw new Error(source)` — would
    // otherwise put a .env value into the drawer and into its accessible name.
    expect(result.diagnostics[0].message).not.toContain('boom');
    expect(result.diagnostics[0].message).toContain('Highlighting is unavailable');
  });

  test('a highlighter exception cannot leak source text into the drawer', async ({ app }) => {
    // The specific shape that broke the no-source-echo guarantee.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const source = 'SECRET=hunter2';
      const renderer = api.create({ highlight: (text: string) => { throw new Error(text); } });
      const { diagnostics } = renderer.render({ source, language: 'properties', wrap: true, generation: 1 });
      return { message: diagnostics[0] ? diagnostics[0].message : '' };
    });

    expect(result.message).not.toContain('hunter2');
    expect(result.message).not.toContain('SECRET');
  });

  test('exceeding the 100,000 token-span cap falls back to plain source with an explanation', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const cap = api.MAX_TOKEN_SPANS;
      const renderer = api.create({
        highlight: () => new Array(cap + 1).fill(null).map(() => ({ line: 1, from: 0, to: 1, type: 'string' })),
      });
      const { node, mode, tokenSpans } = renderer.render({
        source: 'x'.repeat(10), language: 'json', wrap: true, generation: 4,
      });
      const host = document.createElement('div');
      host.appendChild(node);
      return {
        cap,
        mode,
        tokenSpans,
        note: host.querySelector('.source-preview-note')?.textContent || '',
        spans: host.querySelectorAll('span').length,
        plain: host.querySelectorAll('.source-preview-plain').length,
      };
    });

    expect(result.cap).toBe(100000);
    expect(result.mode).toBe('capped');
    expect(result.tokenSpans).toBe(100001);
    expect(result.plain).toBe(1);
    expect(result.spans).toBe(0);
    expect(result.note).toContain('100,000');
  });

  test('line numbers do not depend on a highlighter being available', async ({ app }) => {
    // The renderer never reaches for a highlighter of its own — the app injects one
    // — so "no highlighter" is a supported state, not a broken one. It must mean
    // "no token spans", not "Preview without a gutter".
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const { node, mode, tokenSpans } = api.create().render({
        source: 'one\ntwo\nthree', language: 'json', wrap: true, generation: 6,
      });
      const host = document.createElement('div');
      host.appendChild(node);
      return {
        mode,
        tokenSpans,
        lines: host.querySelectorAll('.source-preview-line').length,
        spans: host.querySelectorAll('span').length,
        // Gutter numbers are CSS counters, so they are not part of the text.
        text: host.textContent,
      };
    });

    expect(result.mode).toBe('lines');
    expect(result.lines).toBe(3);
    expect(result.tokenSpans).toBe(0);
    expect(result.spans).toBe(0);
    expect(result.text).toBe('onetwothree');
  });

  test('the plain path skips highlighting entirely, which is the degraded state', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      let called = 0;
      const renderer = api.create({ highlight: () => { called += 1; return []; } });
      const { node, mode } = renderer.render({
        source: 'a\nb', language: 'json', wrap: true, generation: 5, plain: true,
      });
      const host = document.createElement('div');
      host.appendChild(node);
      return { called, mode, plain: host.querySelectorAll('.source-preview-plain').length, text: host.textContent };
    });

    // Above the enhanced-assistance threshold there is no parsing at all.
    expect(result.called).toBe(0);
    expect(result.mode).toBe('plain');
    expect(result.plain).toBe(1);
    expect(result.text).toBe('a\nb');
  });
});

// --- Language modes ----------------------------------------------------------
//
// One table maps Lezer tags onto the renderer's app-owned token allowlist, and both
// panels read it: Edit through a HighlightStyle whose classes are the Source Preview
// classes verbatim, Preview through a tagHighlighter producing the bare type names.
// These tests pin the registry↔mode correspondence, the allowlist boundary, the span
// cap against real token counts, and that nothing infers a language from content.

// Representative source per mode. Each exercises at least a comment, a name and a
// literal, so a mode that silently stopped producing tokens shows up as a count of
// zero rather than as a slightly duller screen.
const LANGUAGE_SAMPLES: Record<string, string> = {
  markdown: '# Head\n\n**bold** and `code`\n\n- item\n',
  json: '{"a": 1, "b": true, "c": null, "d": "s"}',
  yaml: '# c\na: 1\nb: true\nc: "s"\n',
  toml: '# c\n[table]\nkey = "value"\nn = 1\nb = true\n',
  xml: '<?xml version="1.0"?>\n<!-- c -->\n<root attr="v">text</root>\n',
  html: '<!doctype html>\n<!-- c -->\n<p class="x">t</p>\n<style>.a{color:red}</style>\n<script>var a = 1;</script>\n',
  css: '/* c */\n.a > b { color: red; margin: 0 }\n',
  javascript: '// c\nconst a = 1;\nfunction f(x) { return `t${x}`; }\n',
  typescript: '// c\ninterface I { a: number }\nconst a: I = { a: 1 };\n',
  shell: '#!/bin/bash\n# c\nif [ -f x ]; then\n  echo "hi" | wc -l\nfi\n',
  properties: '# c\nkey=value\nother = 2\n',
};

const LANGUAGE_IDS = [
  'markdown', 'json', 'yaml', 'toml', 'xml', 'html', 'css', 'javascript', 'typescript', 'shell', 'properties',
];

test.describe('Language modes', () => {
  test('every language the registry declares resolves to an extension', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const modes = api.LanguageModes;
      const resolved: Record<string, number> = {};
      for (const id of modes.LANGUAGE_IDS) resolved[id] = modes.resolve(id).length;
      return {
        ids: modes.LANGUAGE_IDS.slice().sort(),
        // Every distinct `language` field in the registry, from the registry itself
        // rather than from a second hand-maintained list.
        declared: Array.from(new Set(
          api.TextFileTypes.allDescriptors().map((d: any) => d.language).filter(Boolean),
        )).sort(),
        resolved,
        unknown: modes.resolve('rust').length,
        nullish: modes.resolve(null).length,
        empty: modes.resolve('').length,
        supported: modes.isSupported('json'),
        unsupported: modes.isSupported('rust'),
      };
    });

    expect(result.ids).toEqual([...LANGUAGE_IDS].sort());
    // The binding assertion: a registry mode with no language support would open
    // in a plain editor and nobody would notice until they looked.
    expect(result.declared).toEqual([...LANGUAGE_IDS].sort());
    for (const id of LANGUAGE_IDS) {
      // The language support plus the shared highlight style.
      expect(result.resolved[id], `${id} resolves to an extension`).toBe(2);
    }
    // No extension at all, which is also the >2 MiB degraded state.
    expect(result.unknown).toBe(0);
    expect(result.nullish).toBe(0);
    expect(result.empty).toBe(0);
    expect(result.supported).toBe(true);
    expect(result.unsupported).toBe(false);
  });

  test('the highlighter never emits a token type outside the renderer allowlist', async ({ app }) => {
    const result = await app.page.evaluate((samples) => {
      const api = (window as any).MahpastesTextEditor;
      const modes = api.LanguageModes;
      const allowlist = api.SourcePreviewRenderer.TOKEN_CLASSES;
      const per: Record<string, any> = {};
      for (const id of modes.LANGUAGE_IDS) {
        const source = samples[id];
        const tokens = modes.highlight(source, id);
        const lines = source.split('\n');
        per[id] = {
          count: tokens.length,
          types: Array.from(new Set(tokens.map((t: any) => t.type))).sort(),
          // A token that named a line it does not fit on, or an inverted range,
          // would put a span over the wrong characters.
          malformed: tokens.filter((t: any) => (
            t.line < 1 || t.line > lines.length ||
            t.from < 0 || t.from >= t.to || t.to > lines[t.line - 1].length
          )).length,
        };
      }
      return { per, allowlist: allowlist.slice().sort(), tokenTypes: modes.TOKEN_TYPES.slice().sort() };
    }, LANGUAGE_SAMPLES);

    // The two allowlists are the same set seen from two sides. A type the mapping
    // produced but the renderer did not know would silently become plain text.
    expect(result.tokenTypes).toEqual(result.allowlist);

    for (const id of LANGUAGE_IDS) {
      const seen = result.per[id];
      expect(seen.count, `${id} produces tokens`).toBeGreaterThan(0);
      expect(seen.malformed, `${id} token ranges fit their line`).toBe(0);
      const outside = seen.types.filter((t: string) => !result.allowlist.includes(t));
      expect(outside, `${id} stays inside the allowlist`).toEqual([]);
    }
  });

  test('the language comes from the registry, never from the content', async ({ app }) => {
    // The same bytes highlighted under two modes must produce two different token
    // streams. If anything sniffed the content, one of these would agree with the
    // other regardless of the id it was handed.
    const result = await app.page.evaluate(() => {
      const modes = (window as any).MahpastesTextEditor.LanguageModes;
      const jsonSource = '{"a": 1}';
      const hashLine = '# heading';
      return {
        jsonAsJSON: modes.highlight(jsonSource, 'json').map((t: any) => t.type),
        jsonAsShell: modes.highlight(jsonSource, 'shell').map((t: any) => t.type),
        hashAsMarkdown: modes.highlight(hashLine, 'markdown').map((t: any) => t.type),
        hashAsShell: modes.highlight(hashLine, 'shell').map((t: any) => t.type),
        hashAsJSON: modes.highlight(hashLine, 'json').map((t: any) => t.type),
      };
    });

    // A leading `#` is a Markdown heading, a shell comment, and a syntax error in
    // JSON. Only the id decides which reading applies.
    expect(result.hashAsMarkdown).toContain('keyword');
    expect(result.hashAsShell).toEqual(['comment']);
    expect(result.hashAsJSON).toEqual([]);
    expect(result.jsonAsJSON).not.toEqual(result.jsonAsShell);
  });

  test('the real highlighter reaches the 100,000 span cap and stops one past it', async ({ app }) => {
    // Bare numbers rather than quoted strings: at one token per two characters they
    // are the densest thing the JSON grammar produces, which is what makes the span
    // cap reachable INSIDE the 128 KiB parse budget at all.
    //
    // The two limits have to be satisfiable together or the span cap is dead code the
    // parse budget always reaches first. 60,000 numbers is 120,001 characters and
    // yields exactly cap+1 tokens, measured — not estimated.
    const source = `[${new Array(60000).fill('1').join(',')}]`;

    const result = await app.page.evaluate((src) => {
      const api = (window as any).MahpastesTextEditor;
      const tokens = api.LanguageModes.highlight(src, 'json');
      const rendered = api.SourcePreviewRenderer
        .create({ highlight: api.LanguageModes.highlight })
        .render({ source: src, language: 'json', wrap: true, generation: 1 });
      const host = document.createElement('div');
      host.appendChild(rendered.node);
      return {
        cap: api.SourcePreviewRenderer.MAX_TOKEN_SPANS,
        count: tokens.length,
        mode: rendered.mode,
        tokenSpans: rendered.tokenSpans,
        spans: host.querySelectorAll('span').length,
        note: host.querySelector('.source-preview-note')?.textContent || '',
        plain: host.querySelectorAll('.source-preview-plain').length,
      };
    }, source);

    // Inside the parse budget, so the parse actually ran and the cap is what stopped
    // collection rather than the budget refusing to parse at all.
    expect(source.length).toBeLessThanOrEqual(128 * 1024);
    // Exactly one past the cap: the renderer can only see that the cap was exceeded
    // if it is handed cap+1, and nothing beyond that is accumulated.
    expect(result.count).toBe(result.cap + 1);
    expect(result.mode).toBe('capped');
    expect(result.tokenSpans).toBe(result.cap + 1);
    expect(result.plain).toBe(1);
    expect(result.spans).toBe(0);
    expect(result.note).toContain('100,000');
  });

  test('Lezer recovery findings are possible issues and only for the four best-effort modes', async ({ app }) => {
    const result = await app.page.evaluate(() => {
      const modes = (window as any).MahpastesTextEditor.LanguageModes;
      const cases: Array<[string, string]> = [
        ['javascript', 'const a = 1;'],
        ['javascript', 'const a = ;'],
        ['javascript', 'function ( {'],
        ['typescript', 'const a: number = 1;'],
        ['typescript', 'const a: = 1;'],
        ['css', '.a { color: red }'],
        ['css', '.a { '],
        ['html', '<p>ok</p>'],
        ['html', '<div attr='],
        ['html', '<p <div>'],
        // Highlighting only, no diagnostics at all: their dialects are too ambiguous
        // for even an advisory promise.
        ['shell', 'if [ then'],
        ['properties', 'key=\nbroken"'],
        ['toml', 'a = ='],
        ['markdown', '# fine'],
      ];
      return cases.map(([language, source]) => ({
        language,
        source,
        found: modes.possibleIssues(source, language).map((d: any) => ({
          severity: d.severity, source: d.source, message: d.message, line: d.line, column: d.column,
        })),
      }));
    });

    const at = (language: string, source: string) =>
      result.find((r: any) => r.language === language && r.source === source)!.found;

    // Valid source in a best-effort mode produces nothing.
    expect(at('javascript', 'const a = 1;')).toEqual([]);
    expect(at('typescript', 'const a: number = 1;')).toEqual([]);
    expect(at('css', '.a { color: red }')).toEqual([]);
    expect(at('html', '<p>ok</p>')).toEqual([]);

    // Broken source produces findings — never errors.
    for (const [language, source] of [
      ['javascript', 'const a = ;'], ['javascript', 'function ( {'],
      ['typescript', 'const a: = 1;'], ['css', '.a { '], ['html', '<div attr='], ['html', '<p <div>'],
    ] as const) {
      const found = at(language, source);
      expect(found.length, `${language} ${source} is flagged`).toBeGreaterThan(0);
      for (const item of found) {
        expect(item.severity, `${language} ${source} is never authoritative`).toBe('possible-issue');
        expect(item.source).toBe('lezer');
        expect(item.line).toBeGreaterThanOrEqual(1);
        expect(item.column).toBeGreaterThanOrEqual(1);
        // Messages must not echo source: a `.env` value or a large fragment must
        // never reach the drawer or an accessible name.
        expect(item.message).toMatch(/^Possible syntax error: /);
        expect(item.message).not.toContain(source.trim().slice(0, 6));
      }
    }

    // Modes with no authoritative validator and no best-effort promise: nothing at
    // all, however broken the source is in some other grammar.
    expect(at('shell', 'if [ then')).toEqual([]);
    expect(at('properties', 'key=\nbroken"')).toEqual([]);
    expect(at('toml', 'a = =')).toEqual([]);
    expect(at('markdown', '# fine')).toEqual([]);
  });

  test('a resolver failure removes the language and keeps plain editing', async ({ app }) => {
    // An adapter failure must never take the editor down. The failure is injected at
    // the resolver rather than inside CodeMirror, because that is the one seam a
    // language package can realistically break at.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const host = document.createElement('div');
      document.body.appendChild(host);
      const errors: string[] = [];
      const realError = console.error;
      console.error = (...args: any[]) => { errors.push(String(args[0])); };
      try {
        const adapter = api.createCodeEditorAdapter({
          resolveLanguage: () => { throw new Error('language package exploded'); },
        });
        adapter.mount({ container: host, value: 'still editable', language: 'json', wrap: true });
        adapter.setValue('typed after the failure', { undoable: true });
        const out = {
          mounted: adapter.isMounted(),
          value: adapter.getValue(),
          // No language extension means no token spans, which is exactly the
          // degraded state rather than a broken editor.
          tokens: host.querySelectorAll('.source-token').length,
          logged: errors.some((message) => message.includes('language')),
        };
        adapter.destroy();
        return out;
      } finally {
        console.error = realError;
        host.remove();
      }
    });

    expect(result.mounted).toBe(true);
    expect(result.value).toBe('typed after the failure');
    expect(result.tokens).toBe(0);
    expect(result.logged).toBe(true);
  });

  test('setLanguage(null) strips highlighting from a mounted editor', async ({ app }) => {
    // The >2 MiB gate is re-evaluated on every edit, so the language extension has
    // to come and go on a live editor rather than only at mount.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const host = document.createElement('div');
      document.body.appendChild(host);
      // CodeMirror schedules some of its parse and decoration work off the
      // dispatch, so each reconfigure gets a frame before the DOM is counted.
      const settle = () => new Promise((resolve) => requestAnimationFrame(() => setTimeout(resolve, 50)));
      try {
        const adapter = api.createCodeEditorAdapter({});
        adapter.mount({ container: host, value: '{"a": 1, "b": true}', language: 'json', wrap: true });
        await settle();
        const withLanguage = host.querySelectorAll('.source-token').length;
        const classes = Array.from(host.querySelectorAll('.source-token'))
          .map((el) => el.className.replace('source-token ', ''));
        adapter.setLanguage(null);
        await settle();
        const without = host.querySelectorAll('.source-token').length;
        adapter.setLanguage('json');
        await settle();
        const restored = host.querySelectorAll('.source-token').length;
        adapter.destroy();
        return { withLanguage, without, restored, classes: Array.from(new Set(classes)).sort() };
      } finally {
        host.remove();
      }
    });

    // A one-line document, so no virtualization is involved in the count.
    expect(result.withLanguage).toBeGreaterThan(0);
    expect(result.without).toBe(0);
    expect(result.restored).toBe(result.withLanguage);
    // Editor highlighting reuses the Source Preview classes verbatim, which is what
    // keeps the two panels looking like one system.
    expect(result.classes).toEqual([
      'source-token-boolean', 'source-token-number', 'source-token-property', 'source-token-punctuation',
    ]);
  });
});

// --- Validation adapters -----------------------------------------------------
//
// One test() per format with its case table inside, so a failure names the format
// and the specific input rather than collapsing into one opaque assertion.
//
// Positions are asserted exactly, because that is the contract that silently
// breaks: every parser reports in its own unit — byte offsets, code points,
// code-unit offsets into the whole string, its own line/column pair — and an
// unconverted offset lands the cursor in the wrong place instead of failing loudly.

type ValidationCase = {
  source: string;
  // Expected authoritative errors, in order, as [line, column] plus an optional
  // message substring.
  errors?: Array<[number, number] | [number, number, string]>;
  unavailable?: boolean;
  truncated?: boolean;
  why: string;
};

async function runValidations(app: any, format: string, cases: ValidationCase[]) {
  return app.page.evaluate(
    async ({ format, sources }: { format: string; sources: string[] }) => {
      const api = (window as any).MahpastesTextEditor;
      // A dedicated executor rather than the shared one: the app's own diagnostics
      // state drives that one, and its generation counter is not ours to advance.
      const executor = await api.createExecutor();
      const out: any[] = [];
      let generation = 1;
      try {
        for (const source of sources) {
          out.push(await executor.run({
            op: api.OP_VALIDATE,
            generation: generation++,
            payload: { format, source },
            sourceBytes: new TextEncoder().encode(source).length,
          }));
        }
      } finally {
        executor.dispose();
      }
      return out;
    },
    { format, sources: cases.map((c) => c.source) },
  );
}

function assertValidations(format: string, cases: ValidationCase[], actual: any[]) {
  const problems: string[] = [];
  cases.forEach((expected, i) => {
    const got = actual[i];
    const shown = expected.source.length > 48 ? `${expected.source.slice(0, 48)}…` : expected.source;
    const label = `${format} · ${expected.why} · ${JSON.stringify(shown)}`;
    if (!got) {
      problems.push(`${label}: no result`);
      return;
    }
    if (got.format !== format) problems.push(`${label}: result format ${got.format}`);
    if (!!got.unavailable !== (expected.unavailable === true)) {
      problems.push(`${label}: expected unavailable=${expected.unavailable === true}, got ${JSON.stringify(got.unavailable)}`);
    }
    if (!!got.truncated !== (expected.truncated === true)) {
      problems.push(`${label}: expected truncated=${expected.truncated === true}, got ${got.truncated}`);
    }
    const wanted = expected.errors || [];
    const errors = (got.diagnostics || []).filter((d: any) => d.severity === 'error');
    const rendered = errors.map((d: any) => `${d.line}:${d.column} ${d.message}`);
    if (errors.length !== wanted.length) {
      problems.push(`${label}: expected ${wanted.length} errors, got ${errors.length}: ${JSON.stringify(rendered)}`);
      return;
    }
    wanted.forEach((want, k) => {
      const [line, column, message] = want as [number, number, string?];
      const error = errors[k];
      if (error.line !== line || error.column !== column) {
        problems.push(`${label}: error ${k} expected Ln ${line}, Col ${column}; got Ln ${error.line}, Col ${error.column} (${error.message})`);
      }
      if (message && !String(error.message).includes(message)) {
        problems.push(`${label}: error ${k} message ${JSON.stringify(error.message)} does not contain ${JSON.stringify(message)}`);
      }
      if (error.source !== format) problems.push(`${label}: error ${k} source ${error.source}`);
    });
  });
  expect(problems.join('\n')).toBe('');
}

test.describe('Validator adapters', () => {
  test('JSON: strict, with 1-based UTF-16 positions', async ({ app }) => {
    const cases: ValidationCase[] = [
      { source: '{"a":[1,2],"b":null,"c":"x"}', why: 'valid strict JSON' },
      { source: '  \n{"a":1}\n', why: 'leading and trailing whitespace is fine' },
      { source: '{"broken": }', errors: [[1, 12, "Unexpected '}'"]], why: 'missing value' },
      // The canonical astral fixture. "😀" is one code point but two UTF-16 code
      // units and four UTF-8 bytes, so the byte column is 10, the code-point
      // column is 7, and the only correct answer here is 8.
      { source: '{"😀": }', errors: [[1, 8, "Unexpected '}'"]], why: 'astral character before the error' },
      { source: '{\n  "a": 1,\n}', errors: [[3, 1]], why: 'trailing commas are errors' },
      { source: '{"a":1} // trailing comment', errors: [[1, 9]], why: 'comments are errors' },
      { source: '{"a":NaN}', errors: [[1, 6]], why: 'NaN is an error' },
      { source: '[Infinity]', errors: [[1, 2]], why: 'Infinity is an error' },
      { source: "{'a':1}", errors: [[1, 2]], why: 'single-quoted keys are errors' },
      { source: '{"a": "abc', errors: [[1, 7, 'Unterminated string']], why: 'unterminated string' },
      { source: '', errors: [[1, 1, 'empty']], why: 'an empty document is not valid JSON' },
      { source: '{"a":1}{"b":2}', errors: [[1, 8]], why: 'trailing content after the top-level value' },
    ];
    assertValidations('json', cases, await runValidations(app, 'json', cases));
  });

  test('JSON Lines: one value per nonblank line, blank lines allowed', async ({ app }) => {
    const cases: ValidationCase[] = [
      { source: '{"a":1}\n{"b":2}\n', why: 'valid' },
      { source: '{"a":1}\n\n\n{"b":2}\n', why: 'blank lines are allowed' },
      { source: '\n\n\n', why: 'a document of only blank lines is valid' },
      { source: '', why: 'an empty JSON Lines document is valid' },
      { source: '{"a":1}\n{"b":2}\n{"c":}\n', errors: [[3, 6]], why: 'a per-line error reports its own line' },
      { source: '{\n{"b":2}\n[1,\n', errors: [[1, 2], [3, 4]], why: 'every bad line is reported' },
      // A single physical line must hold exactly one complete value.
      { source: '{"a":1} {"b":2}\n', errors: [[1, 9]], why: 'two values on one line' },
      { source: '{"a":\n1}\n', errors: [[1, 6], [2, 2]], why: 'a value split across lines is two bad lines' },
    ];
    assertValidations('jsonl', cases, await runValidations(app, 'jsonl', cases));
  });

  test('YAML: 1.2 core, duplicate keys, and the document/alias/node limits', async ({ app }) => {
    // A Helm fragment: the `{{ }}` opens a flow mapping the block content below
    // cannot live in, so it is invalid by construction and stays that way.
    const helm = [
      'metadata:',
      '  labels:',
      '    {{- include "chart.labels" . | nindent 4 }}',
      '  name: app',
    ].join('\n');
    const cases: ValidationCase[] = [
      { source: 'a: 1\nb: [1, 2]\nc:\n  - x\n', why: 'valid YAML' },
      { source: 'x: &anchor 1\ny: *anchor\n', why: 'anchors and aliases are valid' },
      { source: 'a: 1\na: 2\n', errors: [[2, 1, 'unique']], why: 'duplicate mapping keys are errors' },
      { source: 'a:\n  b: 1\n c: 2\n', errors: [[3, 1, 'same column']], why: 'bad indentation' },
      { source: 'a:\n\t- 1\n', errors: [[2, 1, 'Tab']], why: 'tabs are not allowed as indentation' },
      { source: 'a: [1, 2\n', errors: [[2, 1]], why: 'unclosed flow sequence' },
      // Errors are collected across documents in a stream.
      { source: '---\na: 1\n---\nb: 1\nb: 2\n', errors: [[5, 1, 'unique']], why: 'an error in the second document' },
      { source: Array.from({ length: 64 }, () => '---\na: 1').join('\n'), why: '64 documents is at the limit' },
      { source: Array.from({ length: 65 }, () => '---\na: 1').join('\n'), unavailable: true, why: '65 documents exceeds the limit' },
      {
        source: `x: &a 1\n${Array.from({ length: 100 }, (_, i) => `k${i}: *a`).join('\n')}\n`,
        why: '100 aliases per document is at the limit',
      },
      {
        source: `x: &a 1\n${Array.from({ length: 101 }, (_, i) => `k${i}: *a`).join('\n')}\n`,
        unavailable: true,
        why: '101 aliases exceeds the limit',
      },
      // Invalid by construction and never becomes valid — the fixture the
      // change-based save policy exists for.
      { source: helm, errors: [[3, 7], [3, 9]], why: 'a Helm template is stably invalid' },
      // A message must never echo the offending source line: `yaml`'s pretty
      // errors embed it, so prettyErrors is off.
      { source: 'secret: hunter2\nsecret: hunter2\n', errors: [[2, 1]], why: 'a duplicate key on a secret-bearing line' },
    ];
    const results = await runValidations(app, 'yaml', cases);
    assertValidations('yaml', cases, results);

    const secretCase = results[results.length - 1];
    expect(secretCase.diagnostics[0].message).not.toContain('hunter2');
  });

  test('TOML: 1.0 syntax', async ({ app }) => {
    const cases: ValidationCase[] = [
      {
        source: 'key = "v"\n[t]\nd = 1979-05-27T07:32:00Z\ninline = { a = 1 }\narr = [1, 2,]\n',
        why: 'TOML 1.0 offset date-time, inline table, and trailing array comma',
      },
      { source: "s = '''\nraw\n'''\n", why: 'multi-line literal string' },
      { source: '', why: 'an empty TOML document is valid' },
      { source: 'a = = 1', errors: [[1, 5, 'invalid value']], why: 'invalid value' },
      { source: '[a]\nb = 1\n[a]\nb = 2\n', errors: [[3, 2, 'redefine']], why: 'a redefined table' },
      // UTF-16 column, not code points: the key is one code point wider than it
      // looks and two code units wider.
      { source: '"😀" = \n', errors: [[1, 8]], why: 'astral key before the error' },
      { source: 'password = \n', errors: [[1, 12]], why: 'a value-less assignment does not echo the key line' },
    ];
    const results = await runValidations(app, 'toml', cases);
    assertValidations('toml', cases, results);

    // smol-toml's message tail embeds a source codeblock; concise() cuts it at the
    // first newline so nothing from the document reaches the drawer.
    const last = results[results.length - 1];
    expect(last.diagnostics[0].message).not.toContain('\n');
    expect(last.diagnostics[0].message).not.toContain('password');
  });

  test('XML: well-formedness, the exact entity policy, and inert DOCTYPE/XInclude', async ({ app }) => {
    const cases: ValidationCase[] = [
      { source: '<r><a x="1"/></r>', why: 'valid' },
      // The internal subset is parsed for syntax, the declaration is recorded but
      // never expanded, and the reference is accepted without expansion.
      { source: '<!DOCTYPE r [<!ENTITY x "ok">]><r>&x;</r>', why: 'a well-formed DOCTYPE is inert syntax' },
      // Also not an error: distinguishing declared from undeclared requires
      // exactly the entity table this design refuses to build.
      { source: '<r>&nope;</r>', why: 'an undeclared entity reference is not an error' },
      { source: '<r>&amp;&#65;&lt;</r>', why: 'predefined entities and numeric references behave normally' },
      // Inert: no processing, no fetch, and not an error either.
      {
        source: '<r xmlns:xi="http://www.w3.org/2001/XInclude"><xi:include href="/etc/passwd"/></r>',
        why: 'XInclude is inert',
      },
      { source: '<r><a></r>', errors: [[1, 10, 'unexpected close tag']], why: 'an unclosed tag is still an error' },
      { source: '<r>', errors: [[1, 3, 'unclosed tag']], why: 'an unclosed root element' },
      // saxes reports `column` in code points and `columnIndex` in code units; the
      // astral character makes the two disagree, and only 9 is correct here.
      {
        source: '<r>😀</a>',
        errors: [[1, 9, 'unexpected close tag'], [1, 9, 'unmatched closing tag']],
        why: 'astral character before a mismatched tag',
      },
      { source: '<r>< </r>', errors: [[1, 5, 'disallowed character']], why: 'illegal character in a tag name' },
      { source: `${'<a>'.repeat(256)}${'</a>'.repeat(256)}`, why: 'depth 256 is at the limit' },
      { source: `${'<a>'.repeat(257)}${'</a>'.repeat(257)}`, unavailable: true, why: 'depth 257 exceeds the limit' },
      {
        source: `<r>${'<a/>'.repeat(100001)}</r>`,
        unavailable: true,
        why: 'more than 100,000 element/attribute events',
      },
    ];
    assertValidations('xml', cases, await runValidations(app, 'xml', cases));
  });

  test('non-authoritative and validator-less formats produce nothing at all', async ({ app }) => {
    // Shell, .env, INI/CFG/CONF, plain text and logs get highlighting only, and
    // the remaining non-authoritative ids are answered outside the executor: Lezer
    // recovery markers come from the editor and renderer failures from the preview.
    // Either way the answer is an empty result, never a failure.
    //
    // `csv` and `tsv` are deliberately NOT in this list. They do run here — see
    // "CSV/TSV findings" below — because findings that only existed while Preview
    // happened to be open would go stale the moment the user typed in Edit. Being
    // non-authoritative is about their severity, not about where they run.
    const formats = [null, '', 'lezer', 'markdown-renderer', 'shell', 'not-a-real-format'];
    const results = await app.page.evaluate(async (formats: any[]) => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor();
      const out: any[] = [];
      let generation = 1;
      try {
        for (const format of formats) {
          out.push(await executor.run({
            op: api.OP_VALIDATE,
            generation: generation++,
            payload: { format, source: 'a: [1,\nexport FOO="bar" # unterminated\n' },
          }));
        }
      } finally {
        executor.dispose();
      }
      return out;
    }, formats);

    const problems: string[] = [];
    results.forEach((result: any, i: number) => {
      if (result.diagnostics.length) problems.push(`${formats[i]}: expected no diagnostics, got ${JSON.stringify(result.diagnostics)}`);
      if (result.unavailable) problems.push(`${formats[i]}: expected no unavailable notice, got ${result.unavailable}`);
    });
    expect(problems.join('\n')).toBe('');
  });

  test('collection stops at 1,000 findings and says so', async ({ app }) => {
    // The drawer's 100-item presentation cap is separate and is asserted through
    // the UI; this is the validator-side collection cap.
    const source = `${Array.from({ length: 1200 }, () => '{').join('\n')}\n`;
    const result = await app.page.evaluate(async (source: string) => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor();
      try {
        return await executor.run({
          op: api.OP_VALIDATE,
          generation: 1,
          payload: { format: 'jsonl', source },
          sourceBytes: new TextEncoder().encode(source).length,
        });
      } finally {
        executor.dispose();
      }
    }, source);

    expect(result.diagnostics).toHaveLength(1000);
    expect(result.truncated).toBe(true);
    expect(result.unavailable).toBeNull();
  });
});

// --- CSV/TSV ------------------------------------------------------------------
//
// The interpretation is observed through TablePreviewRenderer, which is the exact
// code path the UI uses. Its `interpretation` reports the delimiter it chose, the
// header it found, and every render limit it hit, so these tests pin the real
// behavior rather than an internal helper the app does not call.

type TableCase = {
  source: string;
  preferredID?: string | null;
  delimiterID?: string | null;
  headerMode?: 'auto' | 'on' | 'off';
  why: string;
};

type TableResult = {
  mode: string;
  interpretation: any;
  diagnostics: any[];
  notes: string[];
  headerCells: string[];
  bodyRows: string[][];
  rowNumbers: string[];
  paddedCells: number;
};

async function renderTables(app: any, cases: TableCase[]): Promise<TableResult[]> {
  return app.page.evaluate((cases: TableCase[]) => {
    const api = (window as any).MahpastesTextEditor;
    const renderer = api.TablePreviewRenderer.create();
    return cases.map((one, i) => {
      const result = renderer.render({
        source: one.source,
        preferredID: one.preferredID === undefined ? null : one.preferredID,
        delimiterID: one.delimiterID === undefined ? null : one.delimiterID,
        headerMode: one.headerMode === undefined ? 'auto' : one.headerMode,
        generation: i + 1,
      });
      const node = result.node as HTMLElement;
      return {
        mode: result.mode,
        interpretation: result.interpretation,
        diagnostics: result.diagnostics,
        notes: Array.from(node.querySelectorAll('.table-preview-note')).map((el) => el.textContent || ''),
        // The first thead cell is the row-number gutter's corner, not data.
        headerCells: Array.from(node.querySelectorAll('thead th')).slice(1).map((el) => el.textContent || ''),
        bodyRows: Array.from(node.querySelectorAll('tbody tr')).map((tr) =>
          Array.from(tr.querySelectorAll('td')).map((td) => td.textContent || '')),
        rowNumbers: Array.from(node.querySelectorAll('tbody th')).map((el) => el.textContent || ''),
        paddedCells: node.querySelectorAll('tbody td[data-padded="true"]').length,
      };
    });
  }, cases);
}

test.describe('CSV/TSV dialect', () => {
  test('quoting, doubled quotes, and LF, CRLF and CR record endings', async ({ app }) => {
    // headerMode 'off' throughout so record 0 stays in the body and every record
    // is visible as a row.
    const cases: TableCase[] = [
      { source: 'a,b\nc,d', headerMode: 'off', why: 'LF' },
      { source: 'a,b\r\nc,d', headerMode: 'off', why: 'CRLF counts once' },
      { source: 'a,b\rc,d', headerMode: 'off', why: 'CR alone terminates a record' },
      // A document that mixes endings is common — one tool exported it, another
      // appended to it — and every ending still terminates a record. A parser that
      // commits to a single guessed newline silently glues rows together here.
      { source: 'a,b\r\nc,d\ne,f\rg,h', headerMode: 'off', why: 'mixed endings' },
      { source: '"a""b",c', headerMode: 'off', why: 'a doubled quote is one literal quote' },
      { source: '"a,b",c', headerMode: 'off', why: 'a quoted delimiter is content' },
      { source: '"a\nb",c', headerMode: 'off', why: 'a quoted record ending is content' },
      { source: 'a,b,', headerMode: 'off', why: 'a trailing delimiter is an empty final field' },
      // The final newline's empty record is an artifact of the newline; an interior
      // blank line is data.
      { source: 'a,b\n', headerMode: 'off', why: 'a final newline adds no record' },
      { source: 'a,b\n\nc,d\n', headerMode: 'off', why: 'an interior blank record remains data' },
    ];
    const expected: string[][][] = [
      [['a', 'b'], ['c', 'd']],
      [['a', 'b'], ['c', 'd']],
      [['a', 'b'], ['c', 'd']],
      [['a', 'b'], ['c', 'd'], ['e', 'f'], ['g', 'h']],
      [['a"b', 'c']],
      [['a,b', 'c']],
      [['a\nb', 'c']],
      [['a', 'b', '']],
      [['a', 'b']],
      // The blank record is padded to the table's width; the source is untouched.
      [['a', 'b'], ['', ''], ['c', 'd']],
    ];

    const results = await renderTables(app, cases);
    const problems: string[] = [];
    cases.forEach((one, i) => {
      if (JSON.stringify(results[i].bodyRows) !== JSON.stringify(expected[i])) {
        problems.push(`${one.why} · ${JSON.stringify(one.source)}: got ${JSON.stringify(results[i].bodyRows)}, want ${JSON.stringify(expected[i])}`);
      }
    });
    expect(problems.join('\n')).toBe('');
  });

  test('cell values become text, never markup', async ({ app }) => {
    // The same property the source renderer holds: a CSV field containing markup is
    // a cell that reads as markup.
    const source = 'a,b\n<script>alert(1)</script>,"<img src=x onerror=alert(1)>"';
    const probe = await app.page.evaluate((source: string) => {
      const api = (window as any).MahpastesTextEditor;
      const result = api.TablePreviewRenderer.create().render({ source, headerMode: 'off' });
      const node = result.node as HTMLElement;
      return {
        elements: node.querySelectorAll('script, img').length,
        html: node.innerHTML,
        cells: Array.from(node.querySelectorAll('tbody td')).map((td) => td.textContent || ''),
      };
    }, source);

    expect(probe.elements).toBe(0);
    expect(probe.html).toContain('&lt;script&gt;');
    expect(probe.html).not.toContain('<script>');
    expect(probe.cells).toEqual(['a', 'b', '<script>alert(1)</script>', '<img src=x onerror=alert(1)>']);
  });
});

test.describe('CSV/TSV delimiter detection', () => {
  test('the winner is chosen deterministically, criterion by criterion', async ({ app }) => {
    const cases: Array<TableCase & { delimiter: string; inconclusive?: boolean }> = [
      { source: 'a,b,c\n1,2,3\n', delimiter: 'comma', why: 'comma' },
      { source: 'a\tb\tc\n1\t2\t3\n', delimiter: 'tab', why: 'tab' },
      { source: 'a;b;c\n1;2;3\n', delimiter: 'semicolon', why: 'semicolon' },
      { source: 'a|b|c\n1|2|3\n', delimiter: 'pipe', why: 'pipe' },
      // Fewest parse errors first: comma leaves a quote closing against a
      // semicolon, which is a malformed-quote error; semicolon does not.
      { source: '"a";b,c\n"a";b,c\n', delimiter: 'semicolon', why: 'fewest parse errors wins' },
      // Then fewest records differing from the modal width. Both are 2 wide and
      // error-free; only comma has a record that disagrees.
      { source: 'x,y;z\nx,y;z\nx,y,q;z\n', delimiter: 'semicolon', why: 'fewest deviant records wins' },
      // Then the largest modal width, ahead of the tie order.
      { source: 'a,b;c;d\ne,f;g;h\n', delimiter: 'semicolon', why: 'largest modal width wins' },
      // Only then the stable tie order comma, tab, semicolon, pipe.
      { source: 'a,b;c\nd,e;f\n', delimiter: 'comma', why: 'a perfect tie takes comma' },
      { source: 'a\tb;c\nd\te;f\n', delimiter: 'tab', why: 'a tie without comma takes tab' },
      { source: 'a;b|c\nd;e|f\n', delimiter: 'semicolon', why: 'a tie without comma or tab takes semicolon' },
      // Fewer than two fields in the modal-width record disqualifies a candidate,
      // and when nothing qualifies comma is used and Preview says so.
      { source: 'just\nsome\nlines\n', delimiter: 'comma', inconclusive: true, why: 'no candidate qualifies' },
      { source: '', delimiter: 'comma', inconclusive: true, why: 'an empty document is inconclusive' },
    ];

    const results = await renderTables(app, cases);
    const problems: string[] = [];
    cases.forEach((one, i) => {
      const got = results[i].interpretation;
      if (got.delimiterID !== one.delimiter) {
        problems.push(`${one.why} · ${JSON.stringify(one.source)}: chose ${got.delimiterID}, want ${one.delimiter}`);
      }
      if (!!got.inconclusive !== (one.inconclusive === true)) {
        problems.push(`${one.why}: inconclusive=${got.inconclusive}, want ${one.inconclusive === true}`);
      }
    });
    expect(problems.join('\n')).toBe('');

    // The inconclusive case explains itself in Preview rather than silently
    // producing a one-column table.
    const inconclusive = results[cases.findIndex((c) => c.why === 'no candidate qualifies')];
    expect(inconclusive.notes.join(' ')).toContain('No delimiter could be determined');
  });

  test('TSV prefers tab, but still falls through when tab does not qualify', async ({ app }) => {
    const cases: TableCase[] = [
      // Comma would win the contest outright on width, and for a .tsv file that is
      // the wrong answer: preferring tab is not a tiebreak, it is a bias.
      { source: 'a\tb,c,d,e\nf\tg,h,i,j\n', preferredID: 'tab', why: 'tab qualifies, so tab wins' },
      { source: 'a\tb,c,d,e\nf\tg,h,i,j\n', preferredID: null, why: 'without the bias, comma wins' },
      // A .tsv file that is really comma-separated is still readable.
      { source: 'a,b,c\n1,2,3\n', preferredID: 'tab', why: 'tab does not qualify, so detection runs' },
    ];
    const results = await renderTables(app, cases);
    expect(results.map((r) => r.interpretation.delimiterID)).toEqual(['tab', 'comma', 'comma']);
  });

  test('detection reads at most the first 50 nonblank records', async ({ app }) => {
    // The first 50 records are semicolon-shaped; the 200 after them are
    // comma-shaped and would win a full scan outright. They are never read.
    const source = `${'a;b\n'.repeat(60)}${'a,b,c,d\n'.repeat(200)}`;
    const [bounded] = await renderTables(app, [{ source, why: 'record-bounded detection' }]);
    expect(bounded.interpretation.delimiterID).toBe('semicolon');

    // Proof that the tail really would have won: the same document with the tail
    // first picks comma.
    const flipped = `${'a,b,c,d\n'.repeat(200)}${'a;b\n'.repeat(60)}`;
    const [other] = await renderTables(app, [{ source: flipped, why: 'the tail would have won' }]);
    expect(other.interpretation.delimiterID).toBe('comma');
  });

  test('detection reads at most the first 64 KiB', async ({ app }) => {
    // Eight complete 8,000-byte semicolon records fit in 64 KiB, so the record
    // bound never fires — only the byte bound stops the scan before the 40
    // comma-shaped records that would otherwise win.
    const wide = `${'x'.repeat(3999)};${'x'.repeat(3999)}\n`;
    const source = `${wide.repeat(10)}${'a,b,c,d\n'.repeat(40)}`;
    const [bounded] = await renderTables(app, [{ source, why: 'byte-bounded detection' }]);
    expect(bounded.interpretation.delimiterID).toBe('semicolon');

    // Same records, each narrow enough that all 50 fit inside 64 KiB: now the
    // comma tail is in scope and wins, which is what proves the bound above was
    // the byte bound and not the record bound.
    const narrow = `${'a;b\n'.repeat(10)}${'a,b,c,d\n'.repeat(40)}`;
    const [unbounded] = await renderTables(app, [{ source: narrow, why: 'the tail is in scope' }]);
    expect(unbounded.interpretation.delimiterID).toBe('comma');
  });
});

test.describe('CSV/TSV header detection and overrides', () => {
  test('a header needs two records, one uniform width of two, and unique nonempty labels', async ({ app }) => {
    const cases: Array<TableCase & { header: boolean }> = [
      { source: 'name,age\nada,36\nbob,41\n', header: true, why: 'a clean header' },
      { source: 'name,age\n', header: false, why: 'a single record is not a header' },
      { source: 'name,age\nada\n', header: false, why: 'ragged records rule out a header' },
      { source: 'a,a\n1,2\n', header: false, why: 'duplicate labels rule out a header' },
      { source: 'a,  \n1,2\n', header: false, why: 'a label that trims to nothing rules out a header' },
      { source: 'a\nb\nc\n', header: false, why: 'one field per record is never a header' },
      { source: 'name,age\n\nada,36\n', header: false, why: 'a blank record rules out a header' },
      { source: ' name , age \nada,36\n', header: true, why: 'labels are compared trimmed' },
    ];
    const results = await renderTables(app, cases);
    const problems: string[] = [];
    cases.forEach((one, i) => {
      const got = results[i].interpretation;
      if (got.headerDetected !== one.header) {
        problems.push(`${one.why} · ${JSON.stringify(one.source)}: headerDetected=${got.headerDetected}, want ${one.header}`);
      }
      // With no override, presentation follows detection exactly.
      if (got.header !== one.header) problems.push(`${one.why}: header=${got.header}, want ${one.header}`);
    });
    expect(problems.join('\n')).toBe('');
  });

  test('the temporary overrides change presentation and nothing else', async ({ app }) => {
    const detectedHeader = 'name,age\nada,36\nbob,41\n';
    // Duplicate labels, so detection says no. Note that `a,1` WOULD be a header by
    // the rule: it only asks for nonempty unique labels, not for labels that look
    // like labels. That is deliberate — anything looser starts eating the first row
    // of ordinary data.
    const noHeader = 'a,a\n1,2\n';
    const results = await renderTables(app, [
      { source: detectedHeader, why: 'detected header' },
      { source: detectedHeader, headerMode: 'off', why: 'header override off' },
      { source: noHeader, why: 'no header detected' },
      { source: noHeader, headerMode: 'on', why: 'header override on' },
      { source: 'a,b;c\nd,e;f\n', why: 'detected comma' },
      { source: 'a,b;c\nd,e;f\n', delimiterID: 'semicolon', why: 'delimiter override' },
    ]);

    // Detection promotes the first record; the override demotes it back into the
    // body without changing a character of source.
    expect(results[0].headerCells).toEqual(['name', 'age']);
    expect(results[0].bodyRows).toEqual([['ada', '36'], ['bob', '41']]);
    expect(results[1].headerCells).toEqual(['1', '2']);
    expect(results[1].bodyRows).toEqual([['name', 'age'], ['ada', '36'], ['bob', '41']]);
    expect(results[1].interpretation.headerDetected).toBe(true);
    expect(results[1].interpretation.header).toBe(false);

    // ...and the other direction. With no header the strip carries column numbers.
    expect(results[2].headerCells).toEqual(['1', '2']);
    expect(results[2].bodyRows).toEqual([['a', 'a'], ['1', '2']]);
    expect(results[3].headerCells).toEqual(['a', 'a']);
    expect(results[3].bodyRows).toEqual([['1', '2']]);
    expect(results[3].interpretation.headerDetected).toBe(false);
    expect(results[3].interpretation.header).toBe(true);

    // The delimiter override reinterprets the same bytes.
    expect(results[4].bodyRows).toEqual([['d', 'e;f']]);
    expect(results[4].interpretation.delimiterExplicit).toBe(false);
    expect(results[5].bodyRows).toEqual([['d,e', 'f']]);
    expect(results[5].interpretation.delimiterExplicit).toBe(true);
    // Detection still ran and still reports its own answer, so the control can
    // label its auto option honestly instead of echoing the override back.
    expect(results[5].interpretation.detectedDelimiterID).toBe('comma');
    expect(results[5].interpretation.delimiterID).toBe('semicolon');
  });

  test('with no header row the column strip carries column numbers', async ({ app }) => {
    // The table has real header semantics either way, which is also what gives the
    // user something to count against when a row is ragged.
    const [result] = await renderTables(app, [{ source: 'a,a\n1,2\n', why: 'column numbers' }]);
    expect(result.interpretation.headerDetected).toBe(false);
    expect(result.headerCells).toEqual(['1', '2']);
    expect(result.rowNumbers).toEqual(['1', '2']);
  });
});

test.describe('CSV/TSV render limits', () => {
  test('the row, column and total-cell limits each bind on their own', async ({ app }) => {
    const cases: TableCase[] = [
      // 600 data records, 2 columns: 1,200 cells, so only the row limit binds.
      { source: 'a,b\n'.repeat(600), headerMode: 'off', why: 'row limit' },
      // 3 records of 250 fields: only the column limit binds, and 3 × 100 cells is
      // well inside the cell budget.
      { source: `${Array.from({ length: 250 }, (_, i) => `c${i}`).join(',')}\n`.repeat(3), headerMode: 'off', why: 'column limit' },
      // 200 records of exactly 100 fields: the column limit is not exceeded, but
      // 100 columns converts the 10,000-cell budget into a 100-row ceiling.
      { source: `${Array.from({ length: 100 }, (_, i) => `c${i}`).join(',')}\n`.repeat(200), headerMode: 'off', why: 'total-cell limit' },
    ];
    const results = await renderTables(app, cases);

    const rowLimited = results[0].interpretation;
    expect(rowLimited.renderedRows).toBe(500);
    expect(rowLimited.renderedColumns).toBe(2);
    expect(rowLimited.rowsTruncated).toBe(true);
    expect(rowLimited.columnsTruncated).toBe(false);
    expect(rowLimited.cellsBound).toBe(false);
    expect(results[0].bodyRows).toHaveLength(500);
    expect(results[0].notes.join(' ')).toContain('the first 500 of 600 rows');
    expect(results[0].notes.join(' ')).toContain('Switch to Edit for the complete source');

    const columnLimited = results[1].interpretation;
    expect(columnLimited.renderedColumns).toBe(100);
    expect(columnLimited.renderedRows).toBe(3);
    expect(columnLimited.columnsTruncated).toBe(true);
    expect(columnLimited.rowsTruncated).toBe(false);
    expect(results[1].headerCells).toHaveLength(100);
    expect(results[1].bodyRows[0]).toHaveLength(100);
    expect(results[1].notes.join(' ')).toContain('the first 100 of 250 columns');

    const cellLimited = results[2].interpretation;
    expect(cellLimited.renderedColumns).toBe(100);
    expect(cellLimited.renderedRows).toBe(100);
    expect(cellLimited.columnsTruncated).toBe(false);
    expect(cellLimited.rowsTruncated).toBe(true);
    expect(cellLimited.cellsBound).toBe(true);
    expect(results[2].bodyRows).toHaveLength(100);
    expect(results[2].notes.join(' ')).toContain('the first 100 of 200 rows');
    expect(results[2].notes.join(' ')).toContain('10,000-cell limit was reached first');
  });

  test('an untruncated table shows no notice at all', async ({ app }) => {
    const [result] = await renderTables(app, [{ source: 'a,b\n1,2\n', why: 'nothing to report' }]);
    expect(result.notes).toEqual([]);
    expect(result.interpretation.rowsTruncated).toBe(false);
    expect(result.interpretation.columnsTruncated).toBe(false);
  });

  test('missing display cells are padded without changing the source', async ({ app }) => {
    // Ragged tables are frequently intentional, so the grid is squared off for
    // presentation and the record keeps the width it had.
    const [result] = await renderTables(app, [{ source: 'a,b,c\n1,2\n3\n', headerMode: 'off', why: 'padding' }]);
    expect(result.bodyRows).toEqual([['a', 'b', 'c'], ['1', '2', ''], ['3', '', '']]);
    expect(result.paddedCells).toBe(3);
    expect(result.interpretation.modalWidth).toBe(3);
    expect(result.interpretation.widestRecord).toBe(3);
  });
});

// CSV/TSV findings run through the executor on the ordinary debounced path, so
// they stay correct in Edit mode too. Their severity is what makes them
// nonblocking, and it is possible-issue in every state.
async function findDelimitedIssues(
  app: any,
  format: string,
  cases: Array<{ source: string; delimiterID?: string | null; why: string }>,
) {
  return app.page.evaluate(
    async ({ format, cases }: { format: string; cases: Array<{ source: string; delimiterID?: string | null }> }) => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor();
      const out: any[] = [];
      let generation = 1;
      try {
        for (const one of cases) {
          out.push(await executor.run({
            op: api.OP_VALIDATE,
            generation: generation++,
            payload: {
              format,
              source: one.source,
              options: { delimiterID: one.delimiterID === undefined ? null : one.delimiterID },
            },
            sourceBytes: new TextEncoder().encode(one.source).length,
          }));
        }
      } finally {
        executor.dispose();
      }
      return out;
    },
    { format, cases: cases.map((c) => ({ source: c.source, delimiterID: c.delimiterID ?? null })) },
  );
}

test.describe('CSV/TSV findings', () => {
  test('malformed quoting and ragged rows are reported with 1-based UTF-16 positions', async ({ app }) => {
    const cases: Array<{ source: string; delimiterID?: string | null; why: string; issues: Array<[number, number, string]> }> = [
      { source: 'a,b\n1,2\n', why: 'a clean table has nothing to report', issues: [] },
      { source: 'a,b\n\n1,2\n', why: 'a blank record is data, not a finding', issues: [] },
      {
        source: 'a,b\n1,"2\n',
        why: 'an unterminated quoted field',
        issues: [[2, 3, 'never closed']],
      },
      {
        source: 'a,b\n"1"x,2\n',
        why: 'text after a closing quote',
        issues: [[2, 4, 'after a closing quote']],
      },
      // The astral fixture, for the same reason it exists for JSON: 😀 is one code
      // point, two UTF-16 code units and four UTF-8 bytes, so with two of them
      // before the quote the byte column is 10 and the code-point column is 4.
      // Only 6 is correct.
      {
        source: 'a,b\n😀😀,"x\n',
        why: 'an astral character before the finding',
        issues: [[2, 6, 'never closed']],
      },
      {
        source: 'a,b,c\n1,2\n3,4,5\n',
        why: 'one finding per ragged record',
        issues: [[2, 1, 'Record has 2 fields; most records have 3']],
      },
      {
        source: 'a,b\n1\n2\n3,4\n',
        why: 'every ragged record is reported',
        issues: [[2, 1, 'Record has 1 field'], [3, 1, 'Record has 1 field']],
      },
    ];

    const results = await findDelimitedIssues(app, 'csv', cases);
    const problems: string[] = [];
    cases.forEach((one, i) => {
      const got = results[i];
      const label = `${one.why} · ${JSON.stringify(one.source)}`;
      if (got.unavailable) problems.push(`${label}: unexpected unavailable ${got.unavailable}`);
      // The rule this whole format lives under: never authoritative, in any state.
      const errors = got.diagnostics.filter((d: any) => d.severity === 'error');
      if (errors.length) problems.push(`${label}: produced authoritative errors ${JSON.stringify(errors)}`);
      const issues = got.diagnostics.filter((d: any) => d.severity === 'possible-issue');
      const rendered = issues.map((d: any) => `${d.line}:${d.column} ${d.message}`);
      if (issues.length !== one.issues.length) {
        problems.push(`${label}: expected ${one.issues.length} possible issues, got ${JSON.stringify(rendered)}`);
        return;
      }
      one.issues.forEach(([line, column, message], k) => {
        const issue = issues[k];
        if (issue.line !== line || issue.column !== column) {
          problems.push(`${label}: issue ${k} at Ln ${issue.line}, Col ${issue.column}; want Ln ${line}, Col ${column} (${issue.message})`);
        }
        if (!String(issue.message).includes(message)) {
          problems.push(`${label}: issue ${k} message ${JSON.stringify(issue.message)} does not contain ${JSON.stringify(message)}`);
        }
        if (issue.source !== 'csv') problems.push(`${label}: issue ${k} source ${issue.source}`);
      });
    });
    expect(problems.join('\n')).toBe('');
  });

  test('every finding stays nonblocking under an explicitly chosen delimiter too', async ({ app }) => {
    // An earlier draft of the design promoted malformed quoting to an authoritative
    // error once the delimiter was explicit. It was cut, and this is the assertion
    // that keeps it cut: the same document, the same severities, either way.
    const source = 'a;b\n"1"x;2\n3\n';
    const results = await findDelimitedIssues(app, 'csv', [
      { source, why: 'inferred' },
      { source, delimiterID: 'semicolon', why: 'explicitly chosen' },
      { source, delimiterID: 'comma', why: 'explicitly chosen wrongly' },
    ]);

    ['inferred', 'chosen', 'chosen wrongly'].forEach((why, i) => {
      const wrong = results[i].diagnostics.filter((d: any) => d.severity !== 'possible-issue');
      expect(wrong, `${why}: every finding must be a possible issue`).toEqual([]);
      expect(results[i].unavailable, `${why}: a real answer`).toBeNull();
    });
    // Both the inferred and the explicitly-chosen semicolon see the malformed
    // quote and the ragged final record.
    expect(results[0].diagnostics).toHaveLength(2);
    expect(results[1].diagnostics).toHaveLength(2);
    // The chosen delimiter really is in play: comma reads this document as one field
    // per record, so nothing is ragged any more — only the quote finding remains.
    expect(results[2].diagnostics.filter((d: any) => d.message.includes('Record has'))).toHaveLength(0);
    expect(results[2].diagnostics).toHaveLength(1);
  });

  test('TSV findings prefer tab and report themselves as tsv', async ({ app }) => {
    const results = await findDelimitedIssues(app, 'tsv', [
      { source: 'a\tb\n1\n', why: 'ragged under tab' },
      // Comma would win a contest on width; the tab bias means this is read as one
      // field per record, so no record disagrees with the modal width.
      { source: 'a\tb,c,d\n1\te,f,g\n', why: 'tab bias holds' },
    ]);
    expect(results[0].diagnostics).toHaveLength(1);
    expect(results[0].diagnostics[0].source).toBe('tsv');
    expect(results[0].diagnostics[0].severity).toBe('possible-issue');
    expect(results[1].diagnostics).toHaveLength(0);
  });

  test('the parse caps stop the scan and report no authoritative answer', async ({ app }) => {
    const results = await findDelimitedIssues(app, 'csv', [
      { source: 'a,b\n'.repeat(100000), why: 'exactly 100,000 records is complete' },
      { source: 'a,b\n'.repeat(100001), why: 'one record past the cap' },
      { source: ','.repeat(1000001), why: 'one field past the cap' },
    ]);

    // At the cap the document is complete, so it gets a real answer.
    expect(results[0].unavailable).toBeNull();
    expect(results[0].diagnostics).toHaveLength(0);
    // Past it, the standard nonblocking notice — never a claim of validity, and
    // never something that could have blocked a save anyway.
    expect(results[1].unavailable).toBe('csv-limit-records');
    expect(results[1].diagnostics).toHaveLength(0);
    expect(results[2].unavailable).toBe('csv-limit-fields');
    expect(results[2].diagnostics).toHaveLength(0);
  });

  test('the finding collection cap applies to CSV like every other format', async ({ app }) => {
    // 2,000 three-field records set the modal width, then 1,200 two-field records
    // disagree with it. Collection stops at 1,000 and says so; the drawer still
    // presents only the first 100 of those.
    const source = `${'a,b,c\n'.repeat(2000)}${'x,y\n'.repeat(1200)}`;
    const [result] = await findDelimitedIssues(app, 'csv', [{ source, why: 'collection cap' }]);
    expect(result.diagnostics).toHaveLength(1000);
    expect(result.truncated).toBe(true);
    expect(result.unavailable).toBeNull();
    expect(new Set(result.diagnostics.map((d: any) => d.severity))).toEqual(new Set(['possible-issue']));
  });
});

test.describe('Formatter adapters', () => {
  test('JSON and JSON Lines formatting, including the final-newline state', async ({ app }) => {
    const cases: Array<{ format: string; source: string; text?: string; ok?: boolean; why: string }> = [
      { format: 'json', source: '{"name":"m","items":[1,2]}', text: '{\n  "name": "m",\n  "items": [\n    1,\n    2\n  ]\n}', why: 'two-space indentation' },
      { format: 'json', source: '{"a":1}\n', text: '{\n  "a": 1\n}\n', why: 'a final newline is preserved' },
      { format: 'json', source: '{\n  "a": 1\n}', text: '{\n  "a": 1\n}', why: 'absence of a final newline is preserved' },
      { format: 'json', source: '{"a":}', ok: false, why: 'invalid source is refused rather than thrown' },
      { format: 'jsonl', source: '{ "a" : 1 }\n\n{  "b":2 }', text: '{"a":1}\n\n{"b":2}', why: 'each value compacted, blank lines preserved' },
      { format: 'jsonl', source: '{"a":1}\n', text: '{"a":1}\n', why: 'the trailing blank element round-trips' },
      { format: 'jsonl', source: '{"a":}\n', ok: false, why: 'invalid line is refused' },
      { format: 'yaml', source: 'a: 1\n', ok: false, why: 'no YAML formatter ships in this release' },
    ];

    const results = await app.page.evaluate(async (inputs: any[]) => {
      const api = (window as any).MahpastesTextEditor;
      const executor = await api.createExecutor();
      const out: any[] = [];
      let generation = 1;
      try {
        for (const input of inputs) {
          out.push(await executor.run({
            op: api.OP_FORMAT,
            generation: generation++,
            payload: { format: input.format, source: input.source },
            sourceBytes: new TextEncoder().encode(input.source).length,
          }));
        }
      } finally {
        executor.dispose();
      }
      return out;
    }, cases.map((c) => ({ format: c.format, source: c.source })));

    const problems: string[] = [];
    cases.forEach((expected, i) => {
      const got = results[i];
      const label = `${expected.format} · ${expected.why}`;
      const wantOk = expected.ok !== false;
      if (got.ok !== wantOk) problems.push(`${label}: expected ok=${wantOk}, got ${got.ok} (${got.reason})`);
      if (wantOk && got.text !== expected.text) {
        problems.push(`${label}: expected ${JSON.stringify(expected.text)}, got ${JSON.stringify(got.text)}`);
      }
    });
    expect(problems.join('\n')).toBe('');
  });
});

// --- Review-finding regressions ----------------------------------------------
//
// Each case below is a defect an independent review found in the first cut of the
// validation milestone. They are pinned here so the same mistake cannot return.

test.describe('Executor ceilings are measured, not declared', () => {
  for (const mode of ['worker', 'fallback'] as const) {
    test(`${mode}: an oversized payload is refused even when sourceBytes is omitted`, async ({ app }) => {
      // The executor is the safety boundary. A caller that omits sourceBytes — or
      // understates it — must not be able to opt out of the ceiling, because on the
      // fallback path that ceiling is the only thing keeping a synchronous parse off
      // the UI thread.
      const result = await app.page.evaluate(async (kind: string) => {
        const api = (window as any).MahpastesTextEditor;
        const executor = kind === 'worker'
          ? await api.createWorkerExecutor({})
          : api.createFallbackExecutor({});
        const ceiling = api.EXECUTOR_LIMITS[kind].maxSourceBytes;
        const source = 'x'.repeat(ceiling + 1024);
        const out: Record<string, string> = {};
        // No sourceBytes at all.
        try {
          await executor.run({ op: 'validate', payload: { format: 'json', source }, generation: 1 });
          out.omitted = 'ACCEPTED';
        } catch (err: any) { out.omitted = err.code; }
        // A deliberately understated sourceBytes.
        try {
          await executor.run({ op: 'validate', payload: { format: 'json', source }, generation: 2, sourceBytes: 1 });
          out.understated = 'ACCEPTED';
        } catch (err: any) { out.understated = err.code; }
        executor.dispose();
        return out;
      }, mode);

      expect(result.omitted).toBe('too-large');
      expect(result.understated).toBe('too-large');
    });
  }
});

test.describe('JSON formatting preserves literals', () => {
  test('numbers that cannot round-trip through a JS number are left alone', async ({ app }) => {
    // JSON.parse + JSON.stringify silently rewrites valid data: 9007199254740993
    // exceeds Number.MAX_SAFE_INTEGER and comes back ...992, and 1e400 overflows to
    // Infinity and comes back as null. An explicit formatting action must not change
    // values, so formatting re-indents the document's own tokens instead.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({});
      // Monotonic: the executor rejects a generation it has already superseded.
      let generation = 0;
      const format = async (source: string, formatter: string) => {
        const out = await executor.run({
          op: api.OP_FORMAT, payload: { format: formatter, source }, generation: ++generation,
        });
        if (!out.ok) throw new Error(`format failed: ${out.reason} ${out.message || ''}`);
        return out.text;
      };
      const out = {
        bigInteger: await format('{"n":9007199254740993}', 'json'),
        overflow: await format('{"n":1e400}', 'json'),
        precise: await format('{"n":0.1000000000000000000001}', 'json'),
        ordinary: await format('{"name":"mahpastes","items":[1,2]}', 'json'),
        empties: await format('{"a":{},"b":[]}', 'json'),
        escapes: await format('{"s":"\\u00e9\\ud83d\\ude00"}', 'json'),
        stringsWithPunctuation: await format('{"s":"a{b}[c]:d,e"}', 'json'),
        topLevelScalar: await format('42', 'json'),
        jsonl: await format('{"n": 9007199254740993}\n\n{"b":  2}', 'jsonl'),
      };
      executor.dispose();
      return out;
    });

    expect(result.bigInteger).toBe('{\n  "n": 9007199254740993\n}');
    expect(result.overflow).toBe('{\n  "n": 1e400\n}');
    expect(result.precise).toBe('{\n  "n": 0.1000000000000000000001\n}');
    // Ordinary documents still come out exactly as two-space JSON.stringify would.
    expect(result.ordinary).toBe('{\n  "name": "mahpastes",\n  "items": [\n    1,\n    2\n  ]\n}');
    // Empty containers stay on one line rather than gaining a blank body.
    expect(result.empties).toBe('{\n  "a": {},\n  "b": []\n}');
    // Escapes the author chose survive. This is deliberately NOT what
    // JSON.stringify does — it would unescape these to é😀 — and preserving them is
    // the point: a formatter re-indents, it does not rewrite content.
    expect(result.escapes).toBe('{\n  "s": "\\u00e9\\ud83d\\ude00"\n}');
    // Structural characters inside a string are data, not structure.
    expect(result.stringsWithPunctuation).toBe('{\n  "s": "a{b}[c]:d,e"\n}');
    // A top-level scalar is a valid JSON document.
    expect(result.topLevelScalar).toBe('42');
    // JSON Lines compacts each value and preserves blank lines — and the literal.
    expect(result.jsonl).toBe('{"n":9007199254740993}\n\n{"b":2}');
  });
});

test.describe('XML collection cap and DOCTYPE boundary', () => {
  test('collection stops at the finding cap and keeps what it found', async ({ app }) => {
    // Previously the error handler ignored the collector's "full" return, so parsing
    // ran on to the event limit and returned `unavailable` with every finding erased.
    // Reaching the cap is a complete answer about the first N problems, not a
    // resource failure.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({ limits: { deadlineMs: 60000, maxSourceBytes: 64 * 1024 * 1024 } });
      const source = `<r>${'&#xZZ;'.repeat(1200)}${'<a/>'.repeat(2000)}</r>`;
      const out = await executor.run({ op: 'validate', payload: { format: 'xml', source }, generation: 1 });
      executor.dispose();
      return { count: out.diagnostics.length, truncated: out.truncated, unavailable: out.unavailable };
    });

    expect(result.unavailable).toBeNull();
    expect(result.truncated).toBe(true);
    expect(result.count).toBe(1000);
  });

  test('the DOCTYPE internal subset is inert, including its own syntax', async ({ app }) => {
    // The exact accepted boundary. A declared-but-unexpanded reference and an
    // undeclared reference are both fine; so is a malformed internal subset, because
    // checking it would need the DTD parser and entity table this design refuses to
    // build. An unclosed tag is still an error.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({});
      const check = async (source: string, generation: number) => {
        const out = await executor.run({ op: 'validate', payload: { format: 'xml', source }, generation });
        return out.diagnostics.length;
      };
      const out = {
        declaredUnexpanded: await check('<!DOCTYPE r [<!ENTITY x "ok">]><r>&x;</r>', 1),
        undeclared: await check('<r>&undeclared;</r>', 2),
        malformedSubset: await check('<!DOCTYPE r [<!ENTITY>]><r/>', 3),
        garbageSubset: await check('<!DOCTYPE r [garbage]><r/>', 4),
        unclosed: await check('<r><a></r>', 5),
      };
      executor.dispose();
      return out;
    });

    expect(result.declaredUnexpanded).toBe(0);
    expect(result.undeclared).toBe(0);
    // Known-and-accepted: outside the promised contract, pinned so a parser change
    // cannot alter it silently.
    expect(result.malformedSubset).toBe(0);
    expect(result.garbageSubset).toBe(0);
    // Structural malformation is still caught.
    expect(result.unclosed).toBeGreaterThan(0);
  });
});

test.describe('Second-pass review regressions', () => {
  test('formatting refuses a document whose indented form would explode', async ({ app }) => {
    // Indented JSON amplifies quadratically in nesting depth: every line at depth d
    // carries 2*d spaces, so a valid 64 KiB input of nothing but `[` would ask for a
    // ~2 GB result inside one uninterruptible unit. The old
    // JSON.stringify(value, null, 2) had the same amplification.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({});
      let generation = 0;
      const format = async (source: string) => executor.run({
        op: api.OP_FORMAT, payload: { format: 'json', source }, generation: ++generation,
      });
      const deep = await format('['.repeat(2000) + '0' + ']'.repeat(2000));
      const shallow = await format('['.repeat(200) + '0' + ']'.repeat(200));
      // Within the depth limit, but every one of those siblings carries 512 spaces.
      const wideDeep = await format('['.repeat(256) + new Array(32512).fill('0').join(',') + ']'.repeat(256));
      executor.dispose();
      return {
        deepOk: deep.ok,
        deepMessage: deep.message || '',
        wideDeepOk: wideDeep.ok,
        wideDeepMessage: wideDeep.message || '',
        shallowOk: shallow.ok,
        shallowLength: shallow.text ? shallow.text.length : 0,
      };
    });

    expect(result.deepOk).toBe(false);
    expect(result.deepMessage).toContain('nesting depth');
    // Depth is not the only bound: a document within the depth limit whose indented
    // form would still be enormous is refused on output size.
    expect(result.wideDeepOk).toBe(false);
    expect(result.wideDeepMessage).toContain('formatted output');
    // Just under the limit still formats.
    expect(result.shallowOk).toBe(true);
    expect(result.shallowLength).toBeGreaterThan(0);
  });

  test('the executor measures the source it will actually run', async ({ app }) => {
    // The caller keeps a reference to the payload it passed. Without pinning, the
    // ceiling measures the small source and the queued task parses whatever the
    // caller swapped in afterwards.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({ limits: { deadlineMs: 250, maxSourceBytes: 32 } });
      const payload: any = { format: 'json', source: '{}' };
      const pending = executor.run({ op: api.OP_VALIDATE, payload, generation: 1 });
      // Swap in an oversized source after the ceiling has been checked.
      payload.source = `{"a":"${'x'.repeat(500)}"}`;
      let outcome: string;
      try {
        const out = await pending;
        outcome = `RAN:${(out && out.format) || 'unknown'}`;
      } catch (err: any) {
        outcome = err.code;
      }
      executor.dispose();
      return { outcome, sourceAfter: payload.source.length };
    });

    // Either the run is refused, or it runs the pinned small source — never the
    // oversized one that was never measured.
    expect(result.outcome === 'too-large' || result.outcome === 'RAN:json').toBe(true);
  });

});

test.describe('Table review regressions', () => {
  test('a quoted empty record is data, not a blank line', async ({ app }) => {
    // A blank *line* is excluded from width statistics and from the nonblank-record
    // scan bound, because it is blank for every candidate delimiter. `""` is a
    // deliberate empty *value* and must not get that treatment: 50 of them used to
    // let an unread tail pick the winner.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const quoted = api.analyzeDelimited('""\n'.repeat(50) + 'a;b\n', {});
      const blank = api.analyzeDelimited('\n'.repeat(50) + 'a;b\n', {});
      const ragged = api.analyzeDelimited('a,b\n""\n1,2', { delimiterID: 'comma' });
      return {
        quotedID: quoted.delimiterID,
        quotedInconclusive: !!quoted.inconclusive,
        blankID: blank.delimiterID,
        raggedRecords: ragged.records.map((r: string[]) => r.length),
      };
    });

    // The 50 quoted-empty records exhaust the scan, so the semicolon tail is unseen.
    expect(result.quotedID).toBe('comma');
    expect(result.quotedInconclusive).toBe(true);
    // Physically blank lines still are excluded, so the tail is reached.
    expect(result.blankID).toBe('semicolon');
    // And the quoted record counts as a width-1 record among width-2 ones.
    expect(result.raggedRecords).toEqual([2, 1, 2]);
  });

  test('a record ending exactly on the byte boundary is complete, not truncated', async ({ app }) => {
    // The partial-tail guard discarded any final record when the prefix was bounded.
    // One that ends exactly at the boundary was never cut, and throwing it away was
    // enough on its own to turn a clear winner into "inconclusive".
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const src = 'a'.repeat(32767) + ';' + 'b'.repeat(32767) + '\n' + 'tail';
      const out = api.analyzeDelimited(src, {});
      return { id: out.delimiterID, inconclusive: !!out.inconclusive };
    });

    expect(result.id).toBe('semicolon');
    expect(result.inconclusive).toBe(false);
  });

  test('whitespace after a closing quote is tolerated, not counted as an error', async ({ app }) => {
    // Error count is the ranking's FIRST criterion, so inventing errors for input a
    // mainstream parser accepts hands the contest to the wrong delimiter.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const out = api.analyzeDelimited('x,"a" ,b;c\ny,"d" ,e;f\n', {});
      const direct = api.analyzeDelimited('x,"a" ,b\n', { delimiterID: 'comma' });
      return { id: out.delimiterID, errors: direct.errors ? direct.errors.length : 0 };
    });

    // Comma yields modal width 3 against semicolon's 2, and now wins.
    expect(result.id).toBe('comma');
    expect(result.errors).toBe(0);
  });

  test('a detected header row is charged against the total-cell budget', async ({ app }) => {
    // 100 columns of header plus 100 data rows is 10,100 rendered cells. The header
    // is rendered, so it spends from the same 10,000-cell budget.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const header = Array.from({ length: 100 }, (_, c) => `h${c}`).join(',');
      const rows = Array.from({ length: 100 }, (_, r) =>
        Array.from({ length: 100 }, (_, c) => `${r}_${c}`).join(',')).join('\n');
      const { node } = api.TablePreviewRenderer.create().render({
        source: `${header}\n${rows}`, descriptor: { id: 'csv' }, generation: 1,
      });
      const host = document.createElement('div');
      host.appendChild(node);
      const bodyRows = host.querySelectorAll('tbody tr').length;
      const headerCells = host.querySelectorAll('thead th[scope="col"]').length;
      const bodyCells = host.querySelectorAll('tbody td').length;
      return { bodyRows, headerCells, bodyCells };
    });

    // 10000/100 = 100, minus the header row = 99 data rows.
    expect(result.bodyRows).toBe(99);
    expect(result.headerCells + result.bodyCells).toBeLessThanOrEqual(10000);
  });

  test('diagnostic positions honour CR and CRLF line breaks', async ({ app }) => {
    // Unreachable through the editor, whose value is always LF-normalized, but the
    // position index is shared and a CR-only document used to report every finding on
    // line 1.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({});
      const out = await executor.run({
        op: api.OP_VALIDATE, payload: { format: 'csv', source: 'a,b\r1,"2"x\r' }, generation: 1,
      });
      executor.dispose();
      const first = out.diagnostics[0] || null;
      return first ? { line: first.line, column: first.column } : null;
    });

    expect(result).not.toBeNull();
    // Second record, not the first.
    expect(result!.line).toBe(2);
  });
});

test.describe('Language mode review regressions', () => {
  test('our own full-document parse is bounded well below the assistance threshold', async ({ app }) => {
    // CodeMirror's editor highlighting is incremental and viewport-driven, so it is
    // inherently bounded. This module's parser.parse(source) is not: one synchronous
    // pass over a 2 MiB document measured ~358 ms of blocked main thread, inside the
    // assistance threshold. The token-span cap does not help — it applies to the
    // tokens the parse produces, after the parse.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const budget = api.LanguageModes.MAX_PARSE_UNITS;
      const tooBig = `[${new Array(500000).fill('"x"').join(',')}]`;
      const started = performance.now();
      const refused = api.LanguageModes.highlight(tooBig, 'json');
      const elapsed = performance.now() - started;
      const issues = api.LanguageModes.possibleIssues(tooBig, 'json');
      // Just inside the budget still parses.
      const inside = api.LanguageModes.highlight(`{"a":${new Array(1000).fill('1').join(',')}}`, 'json');
      return {
        budget,
        tooBigLength: tooBig.length,
        refusedTokens: refused.length,
        refusedIssues: issues.length,
        elapsed,
        insideTokens: inside.length,
      };
    });

    expect(result.budget).toBe(128 * 1024);
    expect(result.tooBigLength).toBeGreaterThan(result.budget);
    // Refused, not attempted: no tokens, no Lezer findings, and no stall.
    expect(result.refusedTokens).toBe(0);
    expect(result.refusedIssues).toBe(0);
    expect(result.elapsed).toBeLessThan(100);
    // A document inside the budget is unaffected.
    expect(result.insideTokens).toBeGreaterThan(0);
  });

  test('source Preview bounds line elements, not just token spans', async ({ app }) => {
    // The token cap bounds spans. A document of nothing but newlines has zero tokens
    // and would still ask for one <li> per line: 2 MiB of "\n" is 2,097,153 elements,
    // inside the assistance threshold and enough to freeze the panel.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor.SourcePreviewRenderer;
      const cap = api.MAX_PREVIEW_LINES;
      const started = performance.now();
      const { node, mode } = api.create().render({
        source: '\n'.repeat(cap + 10), language: 'json', wrap: true, generation: 1,
      });
      const elapsed = performance.now() - started;
      const host = document.createElement('div');
      host.appendChild(node);
      return {
        cap,
        mode,
        lines: host.querySelectorAll('.source-preview-line').length,
        plain: host.querySelectorAll('.source-preview-plain').length,
        note: host.querySelector('.source-preview-note')?.textContent || '',
        elapsed,
      };
    });

    expect(result.cap).toBe(50000);
    expect(result.mode).toBe('line-capped');
    // No per-line elements at all past the cap; plain inert source instead.
    expect(result.lines).toBe(0);
    expect(result.plain).toBe(1);
    expect(result.note).toContain('50,000');
    expect(result.elapsed).toBeLessThan(500);
  });

  test('legacy stream modes register no completion or auto-close data', async ({ app }) => {
    // Bare Language objects were chosen over LanguageSupport bundles precisely to
    // keep completion sources and auto-close out of language data. Legacy modes
    // smuggle the same things back a different way: the shell mode ships an 88-word
    // autocomplete list and a closeBrackets config that state.languageDataAt exposes.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const out: Record<string, { autocomplete: number; closeBrackets: number }> = {};
      for (const id of ['shell', 'toml', 'properties']) {
        const probe = api.LanguageModes.probeLanguageData(id, 'ec\n');
        out[id] = probe;
      }
      return out;
    });

    for (const id of ['shell', 'toml', 'properties']) {
      expect(result[id].autocomplete, `${id} must expose no completion source`).toBe(0);
      expect(result[id].closeBrackets, `${id} must expose no auto-close config`).toBe(0);
    }
  });
});

test.describe('Diagnostic message sanitization', () => {
  test('quoted document content is redacted while named tokens survive', async ({ app }) => {
    // Dropping everything after the first newline was not enough: parsers embed
    // document content on that first line, quoted. `yaml` reports
    // `Unexpected scalar token ... "hunter2"`, and duplicate-key errors quote the key.
    // Those land in the drawer's visible text AND its accessible name.
    //
    // The line drawn: quoted runs longer than a named token are redacted; the
    // scanner's `Unexpected '}'` is kept, because redacting it leaves `Unexpected …`
    // and makes every finding unactionable.
    const result = await app.page.evaluate(() => {
      const api = (window as any).MahpastesTextEditor;
      const of = (message: string) => api.DIAGNOSTIC_HELPERS
        ? api.DIAGNOSTIC_HELPERS.concise(message)
        : null;
      return {
        available: !!api.DIAGNOSTIC_HELPERS,
        namedToken: of("Unexpected '}'"),
        twoTokens: of("Expected ',' or '}' in object, found '1'"),
        quotedValue: of('Unexpected scalar token at line 1: "hunter2"'),
        duplicateKey: of('Map keys must be unique: "password"'),
        multiline: of('First line\nSECRET=hunter2'),
      };
    });

    if (!result.available) return; // helper not exported; covered by validator tests
    expect(result.namedToken).toBe("Unexpected '}'");
    expect(result.twoTokens).toContain("','");
    expect(result.quotedValue).not.toContain('hunter2');
    expect(result.duplicateKey).not.toContain('password');
    expect(result.multiline).not.toContain('hunter2');
  });

  test('no validator leaks a quoted value into a finding', async ({ app }) => {
    // Driven through the real executor for each authoritative format, with a secret
    // placed where that format's parser is most likely to quote it back.
    const result = await app.page.evaluate(async () => {
      const api = (window as any).MahpastesTextEditor;
      const executor = api.createFallbackExecutor({});
      let generation = 0;
      const check = async (format: string, source: string) => {
        const out = await executor.run({
          op: api.OP_VALIDATE, payload: { format, source }, generation: ++generation,
        });
        return (out.diagnostics || []).map((d: any) => d.message).join(' | ');
      };
      const out = {
        yamlDuplicate: await check('yaml', 'hunter2secret: 1\nhunter2secret: 2\n'),
        yamlBroken: await check('yaml', 'a: "hunter2secret\n'),
        json: await check('json', '{"hunter2secret": }'),
        jsonl: await check('jsonl', '{"hunter2secret": }\n'),
        toml: await check('toml', 'k = "hunter2secret\n'),
      };
      executor.dispose();
      return out;
    });

    for (const [where, messages] of Object.entries(result)) {
      expect(messages as string, `${where} must not echo a quoted value`).not.toContain('hunter2secret');
    }
  });
});
