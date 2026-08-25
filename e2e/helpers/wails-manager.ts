import { spawn, ChildProcess } from 'child_process';
import * as path from 'path';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as net from 'net';
import { fileURLToPath } from 'url';

// ---------------------------------------------------------------------------
// Cross-process restart serialization
// ---------------------------------------------------------------------------
// wails dev uses a single shared build/ directory. Parallel restarts (kill +
// respawn) corrupt the directory and cause compilation failures. We use a
// file-based exclusive lock so that only one restart happens at a time even
// across multiple Playwright worker subprocesses.
//
// Implementation: mkdir-based lock (atomic on POSIX: O_EXCL).  A timestamp
// file inside the lock directory lets waiters detect stale locks (lock held
// for > RESTART_LOCK_STALE_MS) so a crashed holder never blocks the next run.

// Computed lazily after __dirname is set by the fileURLToPath block below.
let RESTART_LOCK = '';
// A lock held for more than this many ms without being released is considered stale.
const RESTART_LOCK_STALE_MS = 5 * 60 * 1000; // 5 minutes

async function acquireRestartLock(timeoutMs = 120000): Promise<() => Promise<void>> {
  // IMPORTANT: Use /tmp (outside the project dir) so wails dev's filesystem
  // watcher does NOT pick it up as a source change and trigger an unwanted
  // recompile/restart while we are trying to restart on purpose.
  if (!RESTART_LOCK) RESTART_LOCK = path.join(os.tmpdir(), 'mahpastes-test-restart-lock');
  const tsFile = path.join(RESTART_LOCK, 'ts');
  const deadline = Date.now() + timeoutMs;
  while (true) {
    try {
      await fs.mkdir(RESTART_LOCK, { recursive: false });
      // Write creation timestamp so other waiters can detect a stale lock.
      await fs.writeFile(tsFile, String(Date.now())).catch(() => {});
      // Lock acquired. Return a release function.
      return async () => {
        try { await fs.rm(RESTART_LOCK, { recursive: true, force: true }); } catch { /* ignore */ }
      };
    } catch (err: any) {
      if (err.code !== 'EEXIST') throw err;
      // Check if the lock is stale (holder crashed without releasing).
      try {
        const raw = await fs.readFile(tsFile, 'utf-8');
        const lockAge = Date.now() - parseInt(raw, 10);
        if (lockAge > RESTART_LOCK_STALE_MS) {
          console.warn(`[wails-manager] Removing stale restart lock (age ${Math.round(lockAge / 1000)}s)`);
          await fs.rm(RESTART_LOCK, { recursive: true, force: true }).catch(() => {});
          await new Promise((r) => setTimeout(r, 200));
          continue;
        }
      } catch { /* lock dir exists but ts file missing — treat as fresh lock */ }
      // Lock held by a live holder — poll.
      if (Date.now() >= deadline) {
        // Timeout: force-remove and retry once.
        console.warn('[wails-manager] Restart lock wait timed out; force-removing');
        await fs.rm(RESTART_LOCK, { recursive: true, force: true }).catch(() => {});
        await new Promise((r) => setTimeout(r, 200));
        continue;
      }
      await new Promise((r) => setTimeout(r, 500));
    }
  }
}

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export interface WailsInstance {
  process: ChildProcess;
  port: number;
  dataDir: string;
  baseURL: string;
}

const PROJECT_ROOT = path.resolve(__dirname, '../..');
const BASE_PORT = 34115;
const instances: Map<number, WailsInstance> = new Map();

// ---------------------------------------------------------------------------
// Terminations that actually wait
// ---------------------------------------------------------------------------
// `wails dev` unlinks the app binary it built from its own exit handler — see
// killProcessAndCleanupBinary in cmd/wails/internal/dev/dev.go. Every instance
// builds to and execs the SAME path (build/bin/mahpastes.app/Contents/MacOS/
// mahpastes), so a kill that returns before that unlink has landed leaves a
// delayed `rm` of the shared binary floating around.
//
// If that rm fires inside another instance's build it lands on the self-sign
// step, and codesign fails with "No such file or directory". `wails dev` then
// swallows the build error (restartApp returns a nil error on build failure)
// and drops into its watcher loop with no app process at all — and since we
// spawn with -nogorebuild -noreload, nothing ever retriggers the build. The
// result is a live `wails dev` with no child, nothing ever binding the port,
// and a full 240 s waitForServer timeout.
//
// So every path that kills an instance waits for it to be genuinely gone,
// which keeps the unlink inside the caller's own window rather than someone
// else's build.

const TERM_GRACE_MS = 15000;
const KILL_GRACE_MS = 5000;

function pidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: any) {
    // EPERM means the process exists but we don't own it — still alive.
    return err.code === 'EPERM';
  }
}

/** Poll until every pid is gone. Returns the pids still alive at timeout. */
async function waitForPidsGone(pids: number[], timeoutMs: number): Promise<number[]> {
  const deadline = Date.now() + timeoutMs;
  let alive = pids.filter(pidAlive);
  while (alive.length > 0 && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 100));
    alive = alive.filter(pidAlive);
  }
  return alive;
}

async function runPidCommand(cmd: string): Promise<number[]> {
  try {
    const { execSync } = await import('child_process');
    return execSync(cmd, { encoding: 'utf-8' })
      .split('\n')
      .map((line) => parseInt(line.trim(), 10))
      .filter((pid) => Number.isInteger(pid) && pid > 0 && pid !== process.pid);
  } catch {
    return []; // pgrep/lsof unavailable, or no matches
  }
}

/** The `wails dev` parent for a port, matched on its -devserver flag. */
async function devserverPids(port: number): Promise<number[]> {
  return runPidCommand(`pgrep -f "devserver localhost:${port}" 2>/dev/null || true`);
}

/**
 * The mahpastes binary bound to a port. `-sTCP:LISTEN` targets only processes
 * LISTENING on it, never clients holding an active connection — that set
 * includes the Playwright worker itself, and killing it crashes the run.
 */
async function listenerPids(port: number): Promise<number[]> {
  return runPidCommand(`lsof -ti tcp:${port} -sTCP:LISTEN 2>/dev/null || true`);
}

async function pidsForPort(port: number): Promise<number[]> {
  const [parents, listeners] = await Promise.all([devserverPids(port), listenerPids(port)]);
  return Array.from(new Set([...parents, ...listeners]));
}

/**
 * SIGTERM everything serving `port`, wait for it to actually exit, then SIGKILL
 * the remainder. Returns only once nothing is alive.
 *
 * SIGTERM before SIGKILL is deliberate: a terminated `wails dev` runs its
 * cleanup, and that cleanup is where the shared-binary unlink happens. We want
 * it to happen here, synchronously, rather than at some arbitrary later moment.
 */
async function terminatePort(port: number): Promise<void> {
  const targets = await pidsForPort(port);
  if (targets.length === 0) return;

  for (const pid of targets) {
    try { process.kill(pid, 'SIGTERM'); } catch { /* already gone */ }
  }
  let alive = await waitForPidsGone(targets, TERM_GRACE_MS);

  if (alive.length > 0) {
    console.warn(`[wails-manager] port ${port}: SIGKILLing ${alive.join(', ')} after ${TERM_GRACE_MS}ms`);
    for (const pid of alive) {
      try { process.kill(pid, 'SIGKILL'); } catch { /* already gone */ }
    }
    await waitForPidsGone(alive, KILL_GRACE_MS);
  }

  // Killing `wails dev` can orphan the mahpastes child it managed — sweep again.
  const stragglers = await pidsForPort(port);
  if (stragglers.length > 0) {
    for (const pid of stragglers) {
      try { process.kill(pid, 'SIGKILL'); } catch { /* already gone */ }
    }
    await waitForPidsGone(stragglers, KILL_GRACE_MS);
  }
}

/**
 * Kill whatever is still serving on a test port. Used before spawning a primary
 * or secondary instance so an orphan from an interrupted run can never end up
 * serving this run's tests.
 */
async function reapPort(port: number): Promise<void> {
  await terminatePort(port);
  // Brief pause to let the OS release the port.
  await new Promise((r) => setTimeout(r, 500));
}

/**
 * Watch a freshly spawned `wails dev` for the failure mode where it stays alive
 * but will never serve: its build failed, restartApp swallowed the error, and
 * -nogorebuild -noreload mean nothing will ever retrigger it. Detecting it
 * turns a silent 240 s timeout into an immediate, accurate error.
 */
function watchForFatalStartup(proc: ChildProcess, label: string): {
  failure: Promise<never>;
  dispose: () => void;
} {
  let reject!: (err: Error) => void;
  let settled = false;
  const failure = new Promise<never>((_, rej) => { reject = rej; });
  // Nothing awaits `failure` once startup succeeds; keep Node from reporting an
  // unhandled rejection in the window before dispose().
  failure.catch(() => {});

  const fail = (msg: string) => {
    if (settled) return;
    settled = true;
    reject(new Error(msg));
  };

  // Output arrives in arbitrary chunks, so match against a rolling tail rather
  // than individual chunks — the markers below straddle chunk boundaries.
  let tail = '';
  const onData = (isErr: boolean) => (data: Buffer) => {
    const text = data.toString();
    if (process.env.DEBUG_WAILS) {
      (isErr ? console.error : console.log)(`[${label}] ${text}`);
    }
    tail = (tail + text.replace(/\x1b\[[0-9;]*m/g, '')).slice(-4000);
    const buildErr = tail.match(/Build error - ([^\n]*)/);
    if (buildErr) {
      fail(`wails dev build failed for ${label}: ${buildErr[1].trim()}`);
    } else if (tail.includes('No version running, build will be retriggered')) {
      fail(
        `wails dev for ${label} came up with no app process (its build failed). ` +
        `It was spawned with -nogorebuild -noreload, so it will never retry.`,
      );
    }
  };

  const onExit = (code: number | null, signal: string | null) => {
    fail(`wails dev for ${label} exited before serving (code=${code}, signal=${signal})`);
  };

  proc.stdout?.on('data', onData(false));
  proc.stderr?.on('data', onData(true));
  proc.on('exit', onExit);

  // Leave the data listeners attached so DEBUG_WAILS keeps logging; `settled`
  // makes them inert.
  const dispose = () => {
    settled = true;
    proc.off('exit', onExit);
  };

  return { failure, dispose };
}

async function isPortAvailable(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.once('error', () => resolve(false));
    server.once('listening', () => {
      server.close();
      resolve(true);
    });
    server.listen(port);
  });
}

// Default timeout for server startup (configurable via WAILS_STARTUP_TIMEOUT env var).
// 4 parallel `wails dev` compilations can easily take 2+ minutes on a loaded machine.
const DEFAULT_STARTUP_TIMEOUT = 180000;

async function waitForServer(url: string, timeoutMs?: number): Promise<void> {
  const timeout = timeoutMs ?? (process.env.WAILS_STARTUP_TIMEOUT
    ? parseInt(process.env.WAILS_STARTUP_TIMEOUT, 10)
    : DEFAULT_STARTUP_TIMEOUT);
  const startTime = Date.now();
  let lastError = '';
  while (Date.now() - startTime < timeout) {
    try {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      const response = await fetch(url, {
        method: 'GET',
        signal: controller.signal,
      });
      clearTimeout(timeout);
      // Any response means server is up
      if (response.status) {
        return;
      }
    } catch (err: any) {
      lastError = err.message || err.name;
      // Connection refused or timeout - server not ready yet
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`Server at ${url} did not start within ${timeout}ms. Last error: ${lastError}`);
}

/**
 * Flags that skip build steps `wails dev` would otherwise redo on every spawn:
 * the Tailwind build, binding generation, embed-file creation and `go mod tidy`.
 *
 * Only safe once a full build has already run in this session — the committed
 * `frontend/dist/output.css` and `frontend/wailsjs/` are reused as-is. The
 * first instance in global-setup always does a full build so that any local
 * Go/CSS edits are picked up; every later spawn (extra workers, mid-suite
 * restarts, secondary share instances) reuses that output.
 *
 * Measured on a warm cache: 22s full spawn vs 11s with these flags.
 */
const FAST_BUILD_FLAGS = ['-s', '-skipbindings', '-skipembedcreate', '-m'];

/**
 * Applied to every test instance.
 *
 * `wails dev` watches the whole project directory, and Playwright writes
 * test-results/ and playwright-report/ inside it. Worker 0 is the only
 * instance without the skip flags above, so a watcher-triggered rebuild makes
 * it regenerate bindings and re-run the Tailwind build — writing back into the
 * very tree being watched, and taking its dev server down long enough that
 * in-flight tests fail with ERR_CONNECTION_REFUSED. Observed twice across ~10
 * full runs, always on worker 0's port.
 *
 * Tests never edit source, so a rebuild mid-run is only ever harmful.
 */
const NO_REBUILD_FLAGS = ['-nogorebuild', '-noreload'];

export async function spawnWailsInstance(
  workerIndex: number,
  opts: { fastBuild?: boolean } = {},
): Promise<WailsInstance> {
  const port = BASE_PORT + workerIndex;
  const dataDir = path.join(os.tmpdir(), `mahpastes-test-${workerIndex}-${Date.now()}`);

  // Reap anything still holding this port. A run that was interrupted (Ctrl-C,
  // a killed CI job) leaves its `wails dev` alive, and killAllInstances only
  // knows about processes THIS process spawned. Worse than a busy port: a
  // stale instance mid-rebuild frees the port just long enough for the check
  // below to pass, then both processes race to bind it and the tests may end
  // up driving the old app — with its old data dir, where fixed-name tags like
  // `create-test-tag` are already shared, so the next StartShare is refused.
  await reapPort(port);

  // Check if port is available
  const available = await isPortAvailable(port);
  if (!available) {
    throw new Error(`Port ${port} is not available for worker ${workerIndex}`);
  }

  // Create fresh data directory
  await fs.mkdir(dataDir, { recursive: true });

  // Find wails binary - check common locations
  const wailsPaths = [
    'wails', // In PATH
    path.join(os.homedir(), 'go', 'bin', 'wails'),
    '/usr/local/bin/wails',
    '/usr/local/go/bin/wails',
  ];

  let wailsBin = 'wails';
  for (const p of wailsPaths) {
    try {
      await fs.access(p);
      wailsBin = p;
      break;
    } catch {
      // Try next path
    }
  }

  // Spawn wails dev with environment override
  const proc = spawn(wailsBin, [
    'dev',
    '-loglevel', 'warning',
    '-devserver', `localhost:${port}`,
    ...NO_REBUILD_FLAGS,
    ...(opts.fastBuild ? FAST_BUILD_FLAGS : []),
  ], {
    cwd: PROJECT_ROOT,
    env: {
      ...process.env,
      MAHPASTES_DATA_DIR: dataDir,
      // Disable public DHT bootstrap peers in tests — mDNS provides same-host
      // discovery and hitting public IPFS nodes is unreliable in CI.
      MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP: '1',
      MAHPASTES_START_HIDDEN: '1',
      // Folder-import "delete" moves files to the Trash. Tests create and
      // delete fixtures constantly, so force a permanent remove — otherwise
      // every run silts up the developer's ~/.Trash with temp files.
      MAHPASTES_TRASH_MODE: 'remove',
      // StartImportSession normally only accepts a folder the user chose in the
      // native picker, which Playwright cannot drive. This opts that check out
      // so tests can scan a temp directory directly.
      MAHPASTES_ALLOW_UNPICKED_IMPORT: '1',
      PATH: `${process.env.PATH}:${path.join(os.homedir(), 'go', 'bin')}`,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
    detached: false,
  });

  const instance: WailsInstance = {
    process: proc,
    port,
    dataDir,
    baseURL: `http://localhost:${port}`,
  };

  proc.on('error', (err) => {
    console.error(`[Worker ${workerIndex}] Process error:`, err);
  });

  instances.set(workerIndex, instance);

  // Wait for server to be ready.
  // Use a longer timeout than the default: secondary instances spawn during
  // active tests when several primary+secondary instances may already be
  // running, increasing compilation time.
  //
  // Race it against the startup watchdog so a build failure surfaces its real
  // error immediately instead of stalling for the whole timeout.
  const { failure, dispose } = watchForFatalStartup(proc, `Worker ${workerIndex}`);
  try {
    await Promise.race([waitForServer(instance.baseURL, 240000), failure]);
  } catch (err) {
    instances.delete(workerIndex);
    // Never leave a wedged `wails dev` behind: it holds a tailwind watcher open
    // and will unlink the shared app binary whenever it finally exits.
    await terminatePort(port).catch(() => {});
    await fs.rm(dataDir, { recursive: true, force: true }).catch(() => {});
    throw err;
  } finally {
    dispose();
  }

  return instance;
}

export async function killWailsInstance(workerIndex: number): Promise<void> {
  const instance = instances.get(workerIndex);
  if (!instance) return;

  // Terminating an instance runs `wails dev`'s cleanup, which unlinks the app
  // binary every instance shares. Hold the same lock the spawn and restart
  // paths take, so that unlink can never land inside another instance's build.
  const releaseKillLock = await acquireRestartLock(300000);
  try {
    const pid = instance.process?.pid;
    // NOTE: do NOT gate this on `instance.process.killed`. In Node that flag
    // means "a signal was successfully SENT", not "the process has exited", so
    // it is always true after the SIGTERM below — the old SIGKILL fallback it
    // guarded could never run, and slow instances survived teardown as orphans.
    if (pid && pidAlive(pid)) {
      try { instance.process.kill('SIGTERM'); } catch { /* already gone */ }
      const alive = await waitForPidsGone([pid], TERM_GRACE_MS);
      if (alive.length > 0) {
        console.warn(`[wails-manager] worker ${workerIndex}: SIGKILLing wails dev ${pid}`);
        try { instance.process.kill('SIGKILL'); } catch { /* already gone */ }
        await waitForPidsGone(alive, KILL_GRACE_MS);
      }
    }
    // `wails dev` can leave the mahpastes child it managed behind — sweep the port.
    await terminatePort(instance.port);
  } finally {
    await releaseKillLock();
  }

  // Clean up data directory
  try {
    await fs.rm(instance.dataDir, { recursive: true, force: true });
  } catch {
    // Ignore cleanup errors
  }

  instances.delete(workerIndex);
}

/**
 * Restart a Wails instance for a given worker, PRESERVING its data directory.
 *
 * This is the low-level helper used by AppHelper.restart(). It:
 *   1. Kills all processes listening on the worker's port (works even when the
 *      primary instance was spawned in the global-setup process, whose `instances`
 *      map is not accessible from inside a test worker subprocess).
 *   2. Reads the .test-state.json file to recover the dataDir for this worker.
 *   3. Spawns a new process on the same port with the same dataDir.
 *   4. Waits for the new server to become responsive.
 *
 * Unlike killWailsInstance, it does NOT delete the dataDir, so persistent
 * state (SQLite DB, share identity key, etc.) survives the restart.
 */
export async function restartWailsInstance(workerIndex: number): Promise<WailsInstance> {
  // Serialize restarts across all Playwright worker processes to prevent
  // parallel `wails dev` compilations from corrupting the shared build/ dir.
  // Use a generous timeout (300 s) so a queued restart can wait for the
  // current one to finish even when many workers compete during the full suite.
  const releaseRestartLock = await acquireRestartLock(300000);

  const port = BASE_PORT + workerIndex;

  // Resolve dataDir: prefer in-process instances map (if this worker spawned
  // the instance itself), fall back to the shared .test-state.json written by
  // global-setup (used when the primary was spawned in a different process).
  let dataDir: string;
  const inProcess = instances.get(workerIndex);
  if (inProcess) {
    dataDir = inProcess.dataDir;
    // Kill wails dev via process handle, then immediately force-kill the
    // mahpastes binary LISTENING on the port. The sequence matters: on macOS,
    // killing wails dev via SIGTERM causes wails dev to send a clean-shutdown
    // IPC message to the mahpastes child binary. The child then runs
    // shutdown() → db.Close() which triggers a SQLite WAL checkpoint.  If the
    // checkpoint is incomplete (the WAL was partially written during the
    // test), the main DB file can end up at the initial page size (4096 bytes)
    // with no table data.
    //
    // By killing the mahpastes binary via SIGKILL immediately (before it can
    // process the IPC shutdown message), we leave the WAL intact so the next
    // process start applies it cleanly — matching the behaviour of the
    // else-branch (pkill → lsof -sTCP:LISTEN SIGKILL).
    //
    // IMPORTANT: use `-sTCP:LISTEN` to target ONLY the server (mahpastes)
    // listening on the port.  A plain `lsof -ti tcp:${port}` also returns
    // client PIDs with active connections to the port, which includes the
    // Playwright worker itself — killing it crashes the whole test run.
    const listeners = await listenerPids(port);
    for (const lp of listeners) {
      try { process.kill(lp, 'SIGKILL'); } catch { /* already gone */ }
    }
    await waitForPidsGone(listeners, KILL_GRACE_MS);

    // Now terminate wails dev itself and WAIT for it: its exit handler unlinks
    // the shared app binary, and that unlink must land here rather than inside
    // the rebuild we are about to start. (`.killed` is not an exit signal — see
    // the note in killWailsInstance.)
    const pid = inProcess.process?.pid;
    if (pid && pidAlive(pid)) {
      try { inProcess.process.kill('SIGTERM'); } catch { /* already gone */ }
      const alive = await waitForPidsGone([pid], TERM_GRACE_MS);
      if (alive.length > 0) {
        try { inProcess.process.kill('SIGKILL'); } catch { /* already gone */ }
        await waitForPidsGone(alive, KILL_GRACE_MS);
      }
    }
    await terminatePort(port);
    instances.delete(workerIndex);
  } else {
    // Read state file to get dataDir.
    const stateFile = path.resolve(__dirname, '../.test-state.json');
    const raw = await fs.readFile(stateFile, 'utf-8');
    const state = JSON.parse(raw) as { instances: Array<{ workerIndex: number; dataDir: string }> };
    const entry = state.instances.find((i) => i.workerIndex === workerIndex);
    if (!entry) {
      throw new Error(`Worker ${workerIndex} not found in .test-state.json`);
    }
    dataDir = entry.dataDir;
    // Kill the wails dev process AND the mahpastes binary it manages. We match
    // by pid lookup rather than a ChildProcess handle, because the primary
    // instance was spawned in a different process (global-setup).
    //
    // Same ordering as the branch above: SIGKILL the mahpastes binary first so
    // it cannot run a clean shutdown (and its WAL checkpoint), then terminate
    // wails dev and wait for it so its shared-binary unlink lands before the
    // rebuild starts.
    const listeners = await listenerPids(port);
    for (const lp of listeners) {
      try { process.kill(lp, 'SIGKILL'); } catch { /* already gone */ }
    }
    await waitForPidsGone(listeners, KILL_GRACE_MS);
    await terminatePort(port);
  }

  // Wait for the port to become free before re-binding.
  await new Promise<void>((resolve) => {
    const deadline = Date.now() + 15000;
    const check = async () => {
      if (await isPortAvailable(port)) {
        resolve();
      } else if (Date.now() < deadline) {
        setTimeout(check, 300);
      } else {
        // Give up waiting and try to spawn anyway.
        resolve();
      }
    };
    check();
  });

  // Find wails binary.
  const wailsPaths = [
    'wails',
    path.join(os.homedir(), 'go', 'bin', 'wails'),
    '/usr/local/bin/wails',
    '/usr/local/go/bin/wails',
  ];
  let wailsBin = 'wails';
  for (const p of wailsPaths) {
    try {
      await fs.access(p);
      wailsBin = p;
      break;
    } catch { /* try next */ }
  }

  // Spawn on the same port + same dataDir. A restart only ever happens mid-run,
  // long after global-setup did a full build, so the skip flags always apply.
  const proc = spawn(wailsBin, [
    'dev',
    '-loglevel', 'warning',
    '-devserver', `localhost:${port}`,
    ...NO_REBUILD_FLAGS,
    ...FAST_BUILD_FLAGS,
  ], {
    cwd: PROJECT_ROOT,
    env: {
      ...process.env,
      MAHPASTES_DATA_DIR: dataDir,
      MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP: '1',
      MAHPASTES_START_HIDDEN: '1',
      // Folder-import "delete" moves files to the Trash. Tests create and
      // delete fixtures constantly, so force a permanent remove — otherwise
      // every run silts up the developer's ~/.Trash with temp files.
      MAHPASTES_TRASH_MODE: 'remove',
      // StartImportSession normally only accepts a folder the user chose in the
      // native picker, which Playwright cannot drive. This opts that check out
      // so tests can scan a temp directory directly.
      MAHPASTES_ALLOW_UNPICKED_IMPORT: '1',
      PATH: `${process.env.PATH}:${path.join(os.homedir(), 'go', 'bin')}`,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
    detached: false,
  });

  proc.on('error', (err) => console.error(`[Worker ${workerIndex} restart] Process error:`, err));

  const baseURL = `http://localhost:${port}`;
  const instance: WailsInstance = { process: proc, port, dataDir, baseURL };
  instances.set(workerIndex, instance);

  const { failure, dispose } = watchForFatalStartup(proc, `Worker ${workerIndex} restart`);
  try {
    // Use a longer timeout for restarts (vs initial spawn) because the
    // lock serializes restarts, so a queued restart may wait up to 60 s
    // for compilation before the port even starts responding.
    //
    // Raced against the startup watchdog so a failed build reports its real
    // error instead of stalling for the whole timeout.
    await Promise.race([waitForServer(baseURL, 240000), failure]);
  } catch (err) {
    instances.delete(workerIndex);
    await terminatePort(port).catch(() => {});
    throw err;
  } finally {
    dispose();
    // Release lock once the server is up (or if waitForServer throws).
    // The compilation phase is done; other restarts can proceed.
    await releaseRestartLock();
  }
  return instance;
}

export async function killAllInstances(): Promise<void> {
  const workerIndices = Array.from(instances.keys());
  await Promise.all(workerIndices.map((idx) => killWailsInstance(idx)));
}

export function getInstance(workerIndex: number): WailsInstance | undefined {
  return instances.get(workerIndex);
}

export function getBaseURL(workerIndex: number): string {
  return `http://localhost:${BASE_PORT + workerIndex}`;
}

// Offset applied to workerIndex for secondary (follower) instances to avoid
// port collisions with the primary pool (workers 0..N-1 use BASE_PORT+0..N-1).
const SECONDARY_INDEX_OFFSET = 1000;

/**
 * Spawn a secondary Wails instance for use inside a single test (e.g. to simulate
 * a follower app alongside the primary publisher app).
 *
 * Returns an object with:
 *   - `instance`  – the raw WailsInstance (port, dataDir, baseURL, process)
 *   - `cleanup()` – must be called at end of test (ideally in a finally block)
 *                   to kill the process and remove the data directory.
 *
 * Port allocation: BASE_PORT + SECONDARY_INDEX_OFFSET + workerIndex, e.g. 35115
 * for worker 0. This avoids the primary range (34115–34118) even for 4 workers.
 */
export async function spawnSecondaryInstance(workerIndex: number): Promise<{
  instance: WailsInstance;
  cleanup: () => Promise<void>;
}> {
  const secondaryIndex = SECONDARY_INDEX_OFFSET + workerIndex;

  // Serialize secondary spawns through the restart lock to avoid multiple
  // concurrent `wails dev` compilations corrupting the shared build/ directory.
  // The lock is released as soon as the server is responsive, so subsequent
  // spawns proceed one at a time rather than all competing for the compiler.
  //
  // Note there is no reap outside this lock: spawnWailsInstance reaps the
  // secondary port itself (it derives the same port from secondaryIndex), and
  // that reap must stay INSIDE the lock. Reaping terminates a stale
  // `wails dev`, whose exit unlinks the shared app binary — do that unlocked
  // and it can land in another worker's build and wedge it.
  const releaseSpawnLock = await acquireRestartLock(300000);
  let instance: WailsInstance;
  try {
    // Secondary instances only spawn mid-test, so the build is already warm.
    instance = await spawnWailsInstance(secondaryIndex, { fastBuild: true });
  } finally {
    await releaseSpawnLock();
  }

  const cleanup = async () => {
    await killWailsInstance(secondaryIndex);
  };

  return { instance, cleanup };
}

export { BASE_PORT, PROJECT_ROOT };
