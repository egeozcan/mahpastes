import { spawn, ChildProcess } from 'child_process';
import * as path from 'path';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as net from 'net';
import { fileURLToPath } from 'url';

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

// Default timeout for server startup (configurable via WAILS_STARTUP_TIMEOUT env var)
const DEFAULT_STARTUP_TIMEOUT = 120000;

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

  // Wait for server to be ready
  await waitForServer(instance.baseURL);

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
  const port = BASE_PORT + workerIndex;

  // Resolve dataDir: prefer in-process instances map (if this worker spawned
  // the instance itself), fall back to the shared .test-state.json written by
  // global-setup (used when the primary was spawned in a different process).
  let dataDir: string;
  const inProcess = instances.get(workerIndex);
  if (inProcess) {
    dataDir = inProcess.dataDir;
    // Kill via process handle.
    if (inProcess.process && !inProcess.process.killed) {
      inProcess.process.kill('SIGTERM');
      await new Promise((r) => setTimeout(r, 1000));
      if (!inProcess.process.killed) {
        inProcess.process.kill('SIGKILL');
        await new Promise((r) => setTimeout(r, 500));
      }
    }
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
      // Also kill the mahpastes binary by port in case pkill missed it.
      execSync(`lsof -ti tcp:${port} 2>/dev/null | xargs kill -9 2>/dev/null || true`, { shell: true });
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

  await waitForServer(baseURL);
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
  const instance = await spawnWailsInstance(secondaryIndex);

  const cleanup = async () => {
    await killWailsInstance(secondaryIndex);
  };

  return { instance, cleanup };
}

export { BASE_PORT, PROJECT_ROOT };
