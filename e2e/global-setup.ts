import { FullConfig } from '@playwright/test';
import { spawnWailsInstance, killAllInstances } from './helpers/wails-manager.js';
import * as fs from 'fs/promises';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// State file to communicate instance info to tests
const STATE_FILE = path.resolve(__dirname, '.test-state.json');

interface TestState {
  instances: Array<{
    workerIndex: number;
    port: number;
    dataDir: string;
    baseURL: string;
  }>;
  startedAt: string;
}

// Delay between spawning instances (configurable via WAILS_SPAWN_DELAY env var)
const DEFAULT_SPAWN_DELAY = 500;

async function globalSetup(config: FullConfig): Promise<void> {
  console.log('\n🚀 Starting Wails instances for parallel testing...\n');

  // Clean up any leftover instances from previous runs
  await killAllInstances();

  // Determine number of workers from config
  const workerCount = config.workers || 4;
  const instances: TestState['instances'] = [];

  // Get spawn delay from environment or use default
  const spawnDelay = process.env.WAILS_SPAWN_DELAY
    ? parseInt(process.env.WAILS_SPAWN_DELAY, 10)
    : DEFAULT_SPAWN_DELAY;

  // Spawn instances SEQUENTIALLY to avoid resource contention
  // Each wails dev instance compiles the app, so parallel spawning causes conflicts
  for (let i = 0; i < workerCount; i++) {
    try {
      const instance = await spawnWailsInstance(i);
      console.log(`  ✅ Worker ${i}: ${instance.baseURL} (data: ${instance.dataDir})`);
      instances.push({
        workerIndex: i,
        port: instance.port,
        dataDir: instance.dataDir,
        baseURL: instance.baseURL,
      });
      // Delay between spawns to let the previous instance finish initial setup
      if (i < workerCount - 1) {
        await new Promise((r) => setTimeout(r, spawnDelay));
      }
    } catch (err: any) {
      console.error(`  ❌ Worker ${i}: Failed to start - ${err.message}`);
      throw err;
    }
  }

  // Write state file for tests to read
  const state: TestState = {
    instances,
    startedAt: new Date().toISOString(),
  };
  await fs.writeFile(STATE_FILE, JSON.stringify(state, null, 2));

  console.log(`\n✅ All ${workerCount} Wails instances started successfully\n`);
}

export default globalSetup;
