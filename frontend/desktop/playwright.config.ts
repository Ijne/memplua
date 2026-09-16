import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './e2e', fullyParallel: true, workers: 2, timeout: 30_000,
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.01, animations: 'disabled' } },
  use: { channel: 'msedge', headless: true, baseURL: 'http://127.0.0.1:5173', viewport: { width: 1180, height: 760 }, trace: 'retain-on-failure', screenshot: 'only-on-failure', timezoneId: 'Europe/Moscow' },
  webServer: { command: 'npm.cmd run dev', url: 'http://127.0.0.1:5173', reuseExistingServer: !process.env.CI, timeout: 30_000 },
});
