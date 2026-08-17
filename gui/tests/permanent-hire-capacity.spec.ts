import { test, expect } from './fixtures';
import { expect as playwrightExpect } from '@playwright/test';

test.describe('Permanent Hire Capacity Rule', () => {
  test('Permanent Hire Capacity (max 5 tasks)', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');
    await tauriPage.click('text=Map');
    
    // Setup Office
    await tauriPage.selectOption('select', '__CREATE__');
    await tauriPage.fill('input[placeholder="Enter Office name..."]', 'Permanent Test Office');
    await tauriPage.click('text="Create Office"');
    await tauriPage.waitForSelector('text="Office Manager"', 10000);

    // Blast the manager with 11 tasks to spawn a Junior Developer
    for (let i = 1; i <= 11; i++) {
      await tauriPage.fill('textarea', `Task ${i} for manager`);
      await tauriPage.press('textarea', 'Enter');
      await tauriPage.waitForTimeout(100); 
    }
    await tauriPage.waitForSelector('text="Junior Developer"', { timeout: 15000 });

    // Blast the Junior Developer (permanent, max 5) with 6 tasks
    for (let i = 1; i <= 6; i++) {
      await tauriPage.fill('textarea', `Task ${i} for junior`);
      await tauriPage.press('textarea', 'Enter');
      await tauriPage.waitForTimeout(100); 
    }

    // Since the limit is 5, the 6th task should trigger another hire
    const agents = await tauriPage.$$('text="Consultant"');
    playwrightExpect(agents.length).toBeGreaterThan(0);
  });
});
