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

export async function spawnWailsInstance(workerIndex: number): Promise<WailsInstance> {
  const port = BASE_PORT + workerIndex;
  const dataDir = path.join(os.tmpdir(), `mahpastes-test-${workerIndex}-${Date.now()}`);

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
  ], {
    cwd: PROJECT_ROOT,
    env: {
      ...process.env,
      MAHPASTES_DATA_DIR: dataDir,
      // Disable public DHT bootstrap peers in tests — mDNS provides same-host
      // discovery and hitting public IPFS nodes is unreliable in CI.
      MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP: '1',
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

  // Log output for debugging
  proc.stdout?.on('data', (data) => {
    if (process.env.DEBUG_WAILS) {
      console.log(`[Worker ${workerIndex}] ${data}`);
    }
  });

  proc.stderr?.on('data', (data) => {
    if (process.env.DEBUG_WAILS) {
      console.error(`[Worker ${workerIndex}] ${data}`);
    }
  });

  proc.on('error', (err) => {
    console.error(`[Worker ${workerIndex}] Process error:`, err);
  });

  instances.set(workerIndex, instance);

  // Wait for server to be ready.
  // Use a longer timeout than the default: secondary instances spawn during
  // active tests when several primary+secondary instances may already be
  // running, increasing compilation time.
  await waitForServer(instance.baseURL, 240000);

  return instance;
}

export async function killWailsInstance(workerIndex: number): Promise<void> {
  const instance = instances.get(workerIndex);
  if (!instance) return;

  // Kill the process
  if (instance.process && !instance.process.killed) {
    instance.process.kill('SIGTERM');
    // Give it a moment to terminate gracefully
    await new Promise((r) => setTimeout(r, 500));
    if (!instance.process.killed) {
      instance.process.kill('SIGKILL');
    }
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
    try {
      const { execSync } = await import('child_process');
      execSync(`lsof -ti tcp:${port} -sTCP:LISTEN 2>/dev/null | xargs kill -9 2>/dev/null || true`, { shell: true });
    } catch { /* lsof not available; proceed optimistically */ }
    if (inProcess.process && !inProcess.process.killed) {
      inProcess.process.kill('SIGTERM');
      await new Promise((r) => setTimeout(r, 1000));
      if (!inProcess.process.killed) {
        inProcess.process.kill('SIGKILL');
        await new Promise((r) => setTimeout(r, 500));
      }
    }
    await new Promise((r) => setTimeout(r, 500));
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
    // Kill the wails dev process AND the mahpastes binary it manages.
    // We use pkill -f to match by argument string (works even when the primary
    // instance was spawned in a different process and we lack a ChildProcess handle).
    try {
      const { execSync } = await import('child_process');
      // Step 1: SIGTERM to wails dev process matching this devserver port.
      // This lets wails dev do a clean shutdown of its child (mahpastes binary).
      execSync(`pkill -f "devserver localhost:${port}" 2>/dev/null || true`, { shell: true });
      await new Promise((r) => setTimeout(r, 1500));
      // Step 2: SIGKILL any survivors (wails dev or mahpastes binary on the port).
      execSync(`pkill -9 -f "devserver localhost:${port}" 2>/dev/null || true`, { shell: true });
      // Also kill the mahpastes binary LISTENING on the port in case pkill
      // missed it. Use `-sTCP:LISTEN` so we don't accidentally kill clients
      // (including the Playwright worker) that have active connections to it.
      execSync(`lsof -ti tcp:${port} -sTCP:LISTEN 2>/dev/null | xargs kill -9 2>/dev/null || true`, { shell: true });
      await new Promise((r) => setTimeout(r, 500));
    } catch {
      // pkill/lsof not available; proceed optimistically
    }
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

  // Spawn on the same port + same dataDir.
  const proc = spawn(wailsBin, [
    'dev',
    '-loglevel', 'warning',
    '-devserver', `localhost:${port}`,
  ], {
    cwd: PROJECT_ROOT,
    env: {
      ...process.env,
      MAHPASTES_DATA_DIR: dataDir,
      MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP: '1',
      PATH: `${process.env.PATH}:${path.join(os.homedir(), 'go', 'bin')}`,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
    detached: false,
  });

  proc.stdout?.on('data', (data) => {
    if (process.env.DEBUG_WAILS) console.log(`[Worker ${workerIndex} restart] ${data}`);
  });
  proc.stderr?.on('data', (data) => {
    if (process.env.DEBUG_WAILS) console.error(`[Worker ${workerIndex} restart] ${data}`);
  });
  proc.on('error', (err) => console.error(`[Worker ${workerIndex} restart] Process error:`, err));

  const baseURL = `http://localhost:${port}`;
  const instance: WailsInstance = { process: proc, port, dataDir, baseURL };
  instances.set(workerIndex, instance);

  try {
    // Use a longer timeout for restarts (vs initial spawn) because the
    // lock serializes restarts, so a queued restart may wait up to 60 s
    // for compilation before the port even starts responding.
    await waitForServer(baseURL, 240000);
  } finally {
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
  const secondaryPort = BASE_PORT + secondaryIndex;

  // Kill any stale secondary process left over from a previous test run.
  // (Global teardown can't reach secondary instances spawned inside worker
  //  subprocesses, so we clean up defensively before spawning a new one.)
  try {
    const { execSync } = await import('child_process');
    execSync(`pkill -f "devserver localhost:${secondaryPort}" 2>/dev/null || true`, { shell: true });
    // Use -sTCP:LISTEN to only target processes LISTENING on the port, not
    // clients with active connections to it (the Playwright worker counts!).
    execSync(`lsof -ti tcp:${secondaryPort} -sTCP:LISTEN 2>/dev/null | xargs kill -9 2>/dev/null || true`, { shell: true });
    // Brief pause to let OS release the port.
    await new Promise((r) => setTimeout(r, 500));
  } catch { /* ignore — pkill/lsof may not be available */ }

  // Serialize secondary spawns through the restart lock to avoid multiple
  // concurrent `wails dev` compilations corrupting the shared build/ directory.
  // The lock is released as soon as the server is responsive, so subsequent
  // spawns proceed one at a time rather than all competing for the compiler.
  const releaseSpawnLock = await acquireRestartLock(300000);
  let instance: WailsInstance;
  try {
    instance = await spawnWailsInstance(secondaryIndex);
  } finally {
    await releaseSpawnLock();
  }

  const cleanup = async () => {
    await killWailsInstance(secondaryIndex);
  };

  return { instance, cleanup };
}

export { BASE_PORT, PROJECT_ROOT };
