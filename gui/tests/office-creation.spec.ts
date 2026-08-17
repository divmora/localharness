import { test, expect } from './fixtures';

test.describe('Office Creation & Initial Task', () => {
  test('should create an office with a specific country and assign task', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');
    await tauriPage.click('text=Map');
    await tauriPage.waitForSelector('canvas', 60000);
    
    // Create office
    await tauriPage.selectOption('select', '__CREATE__');
    await tauriPage.fill('input[placeholder="Enter Office name..."]', 'Tokyo HQ');
    await tauriPage.selectOption('select:not([class*="TopBar"])', 'Japan');
    await tauriPage.click('text="Create Office"');
    
    // Verify Manager spawns
    await tauriPage.waitForSelector('text="Office Manager"', 10000);

    // Assign a task
    await tauriPage.fill('textarea', 'Please draft a welcome email.');
    await tauriPage.press('textarea', 'Enter');
    await tauriPage.waitForSelector('text="Please draft a welcome email."', 10000);
  });
});
