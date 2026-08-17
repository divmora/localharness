import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 600000, // 10 minutes for slow docker cargo builds
  expect: { timeout: 30000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1, // Reduce to 1 worker to avoid multiple app startups
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
    timeout: 600 * 1000, // 10 minutes for slow docker cargo builds
  },
});
