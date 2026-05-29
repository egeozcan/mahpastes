import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/server',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    headless: true,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 10000,
    navigationTimeout: 30000,
    ...devices['Desktop Chrome'],
    viewport: { width: 1280, height: 800 },
  },
  outputDir: './test-results/server',
  timeout: 60000,
  expect: {
    timeout: 10000,
  },
});
