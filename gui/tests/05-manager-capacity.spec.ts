import { test, expect, clickText, waitForText, fill } from './fixtures';
import { expect as playwrightExpect } from '@playwright/test';

test.describe('Manager Capacity Rule', () => {
  test('Manager Capacity (>10 tasks triggers new hire)', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');
    await tauriPage.click('[data-testid="office-tab"]');
    
    // Setup Office
    await tauriPage.selectOption('select', '__CREATE__');
    await fill(tauriPage, 'input[placeholder="Enter Office name..."]', 'Capacity Test Office');
    await clickText(tauriPage, '"Create Office"');
    await waitForText(tauriPage, '"Office Manager"', 10000);

    // Blast the manager with 11 tasks to breach capacity
    for (let i = 1; i <= 11; i++) {
      await fill(tauriPage, 'textarea', `Task ${i} for manager`);
      await tauriPage.press('textarea', 'Enter');
      await tauriPage.waitForTimeout(100); // slight delay to allow UI to update
    }

    // Verify that the manager realizes they are over capacity and hires a new agent
    // We expect a new agent UI element (e.g. "Junior Developer") to appear on the canvas
    await waitForText(tauriPage, '"Junior Developer"', 15000);
  });
});
