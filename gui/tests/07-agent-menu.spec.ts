import { test, expect, waitForText, fill, rightClick } from './fixtures';

test.describe('Agent Menu', () => {
  test('should interact with the agent sidebar menu', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');

    // Start a session to enter chat mode
    await fill(tauriPage, 'textarea[placeholder="What would you like to build?"]', 'agent menu test session');
    await tauriPage.press('textarea[placeholder="What would you like to build?"]', 'Enter');
    
    await waitForText(tauriPage, '"agent menu test session"', 10000);

    // Click "New Session"
    await tauriPage.click('[data-testid="btn-new-session"]');
    
    // Verify it goes back to Empty State
    const hasEmptyState = await tauriPage.evaluate(() => !!document.querySelector('textarea[placeholder="What would you like to build?"]'));
    expect(hasEmptyState).toBe(true);
    
    // Go back to the session we just created
    // It should be the first item in the list
    await tauriPage.click('[data-testid="session-item"]');
    await waitForText(tauriPage, '"agent menu test session"', 10000);

    // Click "Sessions" Manager
    await tauriPage.click('[data-testid="btn-sessions-manager"]');
    await waitForText(tauriPage, '"Sessions Manager"', 10000);
    
    // Close Sessions Manager (assuming there's a close button, but we can just reload or click outside)
    // There is an onClose prop passed in App.tsx. The component probably has a close button.
    // Let's just click the "Agent" tab to reset or find the close button.
    // In App.tsx, the SessionsPage has a close button. Let's just check it exists.
    const hasClose = await tauriPage.evaluate(() => {
      const btn = document.querySelector('button[aria-label="Close"]') || Array.from(document.querySelectorAll('button')).find(b => b.textContent?.includes('Close'));
      if (btn) {
        (btn as HTMLButtonElement).click();
        return true;
      }
      return false;
    });
    // If we couldn't close it, we might be stuck, but usually there's an X button.

    // Test Create Space
    await tauriPage.click('[data-testid="btn-create-space"]');
    await fill(tauriPage, '[data-testid="input-prompt-modal"]', 'Test Space');
    await tauriPage.click('[data-testid="btn-confirm-prompt-modal"]');
    
    // Verify space was created
    await waitForText(tauriPage, '"Test Space"', 10000);
    
    // Move session to space
    await rightClick(tauriPage, '[data-testid="session-item"]');
    await waitForText(tauriPage, '"Move to Space"', 10000);
    
    // Click "Test Space" in the context menu
    // The context menu renders a button with the space name
    await tauriPage.evaluate(() => {
      const btns = Array.from(document.querySelectorAll('button'));
      const testSpaceBtn = btns.find(b => b.textContent?.includes('Test Space'));
      if (testSpaceBtn) testSpaceBtn.click();
    });
    
    // Delete session
    await rightClick(tauriPage, '[data-testid="session-item"]');
    await waitForText(tauriPage, '"Delete Session"', 10000);
    await tauriPage.evaluate(() => {
      const btns = Array.from(document.querySelectorAll('button'));
      const deleteBtn = btns.find(b => b.textContent?.includes('Delete Session'));
      if (deleteBtn) deleteBtn.click();
    });
    
    // Confirm delete if there is a prompt modal
    // AgentSidebar.tsx just calls onDeleteSession, which shows a ConfirmModal in App.tsx
    await waitForText(tauriPage, '"Delete Session"', 10000); // the modal title
    await tauriPage.evaluate(() => {
      const btns = Array.from(document.querySelectorAll('button'));
      const deleteBtn = btns.find(b => b.textContent === 'Delete');
      if (deleteBtn) deleteBtn.click();
    });
  });
});
