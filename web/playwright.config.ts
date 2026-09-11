import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.mjs',
  timeout: 120_000,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: { browserName: 'chromium', headless: true },
  webServer: [
    { command: 'npm run preview -- --host 127.0.0.1 --port 4177 --strictPort', url: 'http://127.0.0.1:4177', reuseExistingServer: false, timeout: 30_000 },
  ],
})
