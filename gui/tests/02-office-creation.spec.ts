import { test, expect, clickText, waitForText, fill } from './fixtures';

test.describe('Office Creation & Initial Task', () => {
  test('should create an office with a specific country and assign task', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');
    await tauriPage.click('[data-testid="office-tab"]');
    await tauriPage.waitForSelector('canvas', 60000);
    
    // Create office
    await tauriPage.selectOption('select', '__CREATE__');
    await fill(tauriPage, 'input[placeholder="Enter Office name..."]', 'Tokyo HQ');
    await tauriPage.selectOption('select:not([class*="TopBar"])', 'Japan');
    await clickText(tauriPage, '"Create Office"');
    
    // Verify Manager spawns
    await waitForText(tauriPage, '"Office Manager"', 10000);

    // Assign a task
    await fill(tauriPage, 'textarea', 'Please draft a welcome email.');
    await tauriPage.press('textarea', 'Enter');
    await waitForText(tauriPage, '"Please draft a welcome email."', 10000);
  });
});
