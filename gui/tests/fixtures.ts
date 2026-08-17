import { createTauriTest } from '@srsholmes/tauri-playwright';

export const { test, expect } = createTauriTest({
  devUrl: 'http://localhost:1420',
  startTimeout: 600000, // 10 minutes for slow docker cargo builds
});
