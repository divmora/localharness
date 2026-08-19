import { test, expect, clickText, waitForText, fill } from './fixtures';

test.describe('Panel Toggles', () => {
  test('should toggle agent sidebar and terminal panel', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');

    // 1. Submit a prompt to start a session and enter Chat Mode
    await fill(tauriPage, 'textarea[placeholder="What would you like to build?"]', 'test panel toggles');
    await tauriPage.press('textarea[placeholder="What would you like to build?"]', 'Enter');
    
    // Wait for the chat text to appear
    await waitForText(tauriPage, '"test panel toggles"', 10000);

    // 2. Sidebar should be visible initially (we check for "Customizations" in the sidebar)
    let hasSidebar = await tauriPage.evaluate(() => Array.from(document.querySelectorAll('*')).some(e => e.textContent === 'Customizations'));
    expect(hasSidebar).toBe(true);

    // Toggle sidebar off
    await tauriPage.click('[title="Toggle Sidebar"]');
    await tauriPage.waitForTimeout(500); // Wait for transition animation

    hasSidebar = await tauriPage.evaluate(() => Array.from(document.querySelectorAll('*')).some(e => e.textContent === 'Customizations'));
    expect(hasSidebar).toBe(false);

    // Toggle sidebar back on
    await tauriPage.click('[title="Toggle Sidebar"]');
    await tauriPage.waitForTimeout(500);

    hasSidebar = await tauriPage.evaluate(() => Array.from(document.querySelectorAll('*')).some(e => e.textContent === 'Customizations'));
    expect(hasSidebar).toBe(true);

    // 3. Terminal panel should be visible initially
    let hasTerminal = await tauriPage.evaluate(() => !!document.querySelector('.xterm'));
    expect(hasTerminal).toBe(true);

    // Toggle terminal off
    await tauriPage.click('[title="Toggle Terminal Panel"]');
    await tauriPage.waitForTimeout(500);

    hasTerminal = await tauriPage.evaluate(() => !!document.querySelector('.xterm'));
    expect(hasTerminal).toBe(false);

    // Toggle terminal back on
    await tauriPage.click('[title="Toggle Terminal Panel"]');
    await tauriPage.waitForTimeout(500);

    hasTerminal = await tauriPage.evaluate(() => !!document.querySelector('.xterm'));
    expect(hasTerminal).toBe(true);
  });
});
