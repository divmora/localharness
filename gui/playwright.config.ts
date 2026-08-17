import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 120000,
  expect: { timeout: 30000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  
  projects: [
    {
      name: 'tauri',
      use: {
        // @ts-ignore - tauri-playwright custom config mode
        mode: 'tauri',
      },
    },
  ],
  webServer: {
    command: 'npm run tauri dev',
    port: 1420,
    reuseExistingServer: !process.env.CI,
    timeout: 120 * 1000,
  },
});
