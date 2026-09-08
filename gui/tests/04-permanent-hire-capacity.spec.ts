import { test, expect, clickText, waitForText, fill } from './fixtures';
import { expect as playwrightExpect } from '@playwright/test';

test.describe('Permanent Hire Capacity Rule', () => {
  test('Permanent Hire Capacity (max 5 tasks)', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');
    await tauriPage.click('[data-testid="office-tab"]');
    
    // Setup Office
    await tauriPage.selectOption('select', '__CREATE__');
    await fill(tauriPage, 'input[placeholder="Enter Office name..."]', 'Permanent Test Office');
    await clickText(tauriPage, '"Create Office"');
    await waitForText(tauriPage, '"Office Manager"', 10000);

    // Blast the manager with 11 tasks to spawn a Junior Developer
    for (let i = 1; i <= 11; i++) {
      await fill(tauriPage, 'textarea', `Task ${i} for manager`);
      await tauriPage.press('textarea', 'Enter');
      await tauriPage.waitForTimeout(100); 
    }
    await waitForText(tauriPage, '"Junior Developer"', 15000);

    // Blast the Junior Developer (permanent, max 5) with 6 tasks
    for (let i = 1; i <= 6; i++) {
      await fill(tauriPage, 'textarea', `Task ${i} for junior`);
      await tauriPage.press('textarea', 'Enter');
      await tauriPage.waitForTimeout(100); 
    }

    // Since the limit is 5, the 6th task should trigger another hire
    const agents = await tauriPage.evaluate(() => Array.from(document.querySelectorAll("*")).filter(e => e.textContent === "Consultant").length);
    playwrightExpect(agents).toBeGreaterThan(1);
    // removed old expect
    playwrightExpect(agents.length).toBeGreaterThan(0);
  });
});
