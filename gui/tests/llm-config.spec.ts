import { test, expect } from './fixtures';

test.describe('LLM Configuration CRUD', () => {
  test('should allow creating, editing, and deleting an LLM endpoint', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');

    // 1. Open Customizations Page
    await tauriPage.click('text=Customizations');
    await tauriPage.waitForSelector('text="LLM Configuration"', 10000);

    // We must handle the confirm dialog before clicking Delete later
    page.on('dialog', dialog => dialog.accept());
    
    await tauriPage.click('text="Add Endpoint"');
    
    // 3. Edit Endpoint fields
    await tauriPage.waitForSelector('text="New Endpoint"', 5000);
    
    await tauriPage.fill('input[placeholder="my-endpoint"]', 'test-endpoint');
    
    await tauriPage.fill('input[placeholder="https://litellm.divmora.cloud"]', 'http://localhost:4000');
    await tauriPage.fill('input[placeholder="litellm-key"]', 'sk-test-123');
    await tauriPage.fill('input[placeholder="workers-ai/@cf/zai-org/glm-5.2"]', 'test-model');
    
    // Save
    await tauriPage.click('text="Save Configuration"');
    
    // Wait for save to complete and list to render the new endpoint
    await tauriPage.waitForSelector('text="URL:"', 5000);
    await tauriPage.waitForSelector('text="http://localhost:4000"', 5000);
    await tauriPage.waitForSelector('text="test-model"', 5000);
    
    // 4. Set as Default
    await tauriPage.click('text="Make Default"');
    
    // Now test-endpoint is default. We cannot delete default endpoints.
    // So we must make divmora default again before deleting test-endpoint.
    await tauriPage.click('text="Make Default"');

    // 5. Delete Endpoint
    // The dialog handler registered earlier will automatically accept the confirm dialog.
    await tauriPage.click('text="Delete"');
    
    // Ensure it was deleted
    await expect(tauriPage.locator('text="test-endpoint"')).toHaveCount(0);
  });
});
