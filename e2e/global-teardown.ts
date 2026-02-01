import { FullConfig } from '@playwright/test';
import { killAllInstances } from './helpers/wails-manager.js';
import * as fs from 'fs/promises';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const STATE_FILE = path.resolve(__dirname, '.test-state.json');

async function globalTeardown(config: FullConfig): Promise<void> {
  console.log('\n🧹 Cleaning up Wails instances...\n');

  await killAllInstances();

  // Remove state file
  try {
    await fs.unlink(STATE_FILE);
  } catch {
    // Ignore if doesn't exist
  }

  console.log('✅ All instances stopped and cleaned up\n');
}

export default globalTeardown;
