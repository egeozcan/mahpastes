/**
 * playwright.share.config.ts
 *
 * Dedicated Playwright config for the encrypted P2P share test suite.
 *
 * Share tests are fundamentally different from the rest of the e2e suite:
 *   - Each two-instance spec spawns a secondary `wails dev` process on a
 *     dedicated port (35115+ range) that compiles and starts a full libp2p
 *     host with mDNS discovery.
 *   - Running more than one of these specs simultaneously causes CPU/RAM
 *     exhaustion: OOM kills, wails dev startup timeouts, and mDNS flakes.
 *   - Serial execution (workers: 1) eliminates all of these contention issues.
 *
 * Global-setup for this config spawns exactly 1 primary wails instance
 * (worker index 0).  Share tests then spawn secondary instances one at a
 * time, under the serialisation lock already in wails-manager.ts.
 *
 * Usage:
 *   npx playwright test --config=playwright.share.config.ts
 *
 * Or via npm script (automatically called by `npm test` after the main suite):
 *   npm run test:share
 */

import { defineConfig, devices } from '@playwright/test';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export default defineConfig({
  testDir: './tests/share',

  // Run share tests one at a time — no parallel secondary spawns.
  workers: 1,
  fullyParallel: false,

  forbidOnly: !!process.env.CI,
  // No retries — we want deterministic results, not masked flakes.
  retries: 0,

  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: 'playwright-report-share' }],
  ],

  globalSetup: path.resolve(__dirname, 'global-setup.ts'),
  globalTeardown: path.resolve(__dirname, 'global-teardown.ts'),

  use: {
    headless: true,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    // Share tests restart wails processes and wait for __appReady across two
    // instances. A 10s action timeout is too tight — raise it here. Individual
    // tests still have their own top-level timeouts (240s–300s).
    actionTimeout: 30000,
    navigationTimeout: 60000,
  },

  outputDir: './test-results-share',

  projects: [
    {
      name: 'share',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 800 },
      },
    },
  ],

  // Share tests set their own timeouts (240s–300s) at the test level.
  // This global timeout is a safety net for any test that forgets.
  timeout: 360000,

  expect: {
    timeout: 10000,
  },
});
