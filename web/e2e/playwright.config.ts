import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 60_000,
  retries: 0,
  use: {
    baseURL: process.env.SMOOTHIE_BASE_URL || 'http://127.0.0.1:8787',
    trace: 'on-first-retry',
  },
  webServer: process.env.SMOOTHIE_E2E_EXTERNAL
    ? undefined
    : {
        command: 'bash -c "rm -rf data-e2e && go run ./cmd/smoothie"',
        cwd: '../..',
        url: 'http://127.0.0.1:8787/api/health',
        reuseExistingServer: !process.env.CI,
        timeout: 180_000,
        env: {
          ...process.env,
          SMOOTHIE_LISTEN: '127.0.0.1:8787',
          SMOOTHIE_DATA_DIR: 'data-e2e',
          SMOOTHIE_DB: 'data-e2e/smoothie.db',
        },
      },
});
