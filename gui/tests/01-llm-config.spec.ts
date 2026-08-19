import { test, expect, clickText, waitForText, fill } from './fixtures';

test.describe('LLM Configuration CRUD', () => {
  test('should allow creating, editing, and deleting an LLM endpoint', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');

    // 1. Open Customizations Page
    await tauriPage.click('[data-testid="btn-customizations"]');
    await waitForText(tauriPage, '"LLM Configuration"', 10000);

    // We must handle the confirm dialog before clicking Delete later
    if (page) page.on('dialog', dialog => dialog.accept());
    
    await clickText(tauriPage, '"Add Endpoint"');
    
    // 3. Edit Endpoint fields
    await waitForText(tauriPage, '"New Endpoint"', 5000);
    
    await fill(tauriPage, 'input[placeholder="my-endpoint"]', 'test-endpoint');
    
    await fill(tauriPage, 'input[placeholder="https://litellm.divmora.cloud"]', 'http://localhost:4000');
    await fill(tauriPage, 'input[placeholder="litellm-key"]', 'sk-test-123');
    await fill(tauriPage, 'input[placeholder="workers-ai/@cf/zai-org/glm-5.2"]', 'test-model');
    
    // Save
    await clickText(tauriPage, '"Save Configuration"');
    
    // Wait for save to complete and list to render the new endpoint
    await waitForText(tauriPage, '"URL:"', 5000);
    await waitForText(tauriPage, '"http://localhost:4000"', 5000);
    await waitForText(tauriPage, '"test-model"', 5000);
    
    // 4. Set as Default
    await clickText(tauriPage, '"Make Default"');
    
    // Now test-endpoint is default. We cannot delete default endpoints.
    // So we must make divmora default again before deleting test-endpoint.
    await clickText(tauriPage, '"Make Default"');

    // 5. Delete Endpoint
    // The dialog handler registered earlier will automatically accept the confirm dialog.
    await clickText(tauriPage, '"Delete"');
    
    // Ensure it was deleted
    const exists = await tauriPage.evaluate(() => Array.from(document.querySelectorAll('*')).some(e => e.textContent === 'test-endpoint'));
    expect(exists).toBe(false);
  });
});
