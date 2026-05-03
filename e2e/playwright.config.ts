import { defineConfig, devices } from '@playwright/test';
import { isolateTestEnvironment } from './helpers/isolation';

const BASE_URL = process.env.BASE_URL || 'http://localhost:5173';
const DATA_SERVICE_URL = process.env.DATA_SERVICE_URL || 'http://localhost:8081';
const BACKEND_URL = process.env.BACKEND_URL || 'http://localhost:8085';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: [
    ['html', { open: 'never', outputFolder: 'e2e-report' }],
    ['list'],
  ],
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 30000,
    navigationTimeout: 30000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
