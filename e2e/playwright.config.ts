import { defineConfig, devices } from '@playwright/test';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Read worker count from environment or use default
const workers = process.env.PW_WORKERS ? parseInt(process.env.PW_WORKERS) : 4;

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : workers,
  reporter: [
    ['list'],
    ['html', { open: 'never' }],
  ],

  globalSetup: path.resolve(__dirname, 'global-setup.ts'),
  globalTeardown: path.resolve(__dirname, 'global-teardown.ts'),

  use: {
    headless: true,
    // Worker-scoped page: trace/video/screenshot are handled manually in the fixture
    // (per-test contexts are not used, so Playwright's built-in capture won't work)
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 10000,
    navigationTimeout: 30000,
  },

  // Store worker state in a file so tests can read their assigned port
  outputDir: './test-results',

  // Only test in Chromium - Wails uses native WebView, not actual browsers
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 800 },
      },
    },
  ],

  // Timeout for each test
  timeout: 60000,

  // Expect timeout
  expect: {
    timeout: 10000,
  },
});
