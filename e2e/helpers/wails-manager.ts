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
