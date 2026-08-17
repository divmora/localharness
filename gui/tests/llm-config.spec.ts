import { test, expect } from './fixtures';

test.describe('LLM Configuration CRUD', () => {
  test('should allow creating, editing, and deleting an LLM endpoint', async ({ page, tauriPage }) => {
    if (page) await page.waitForLoadState('networkidle');

    // 1. Open Customizations Page
    await tauriPage.click('text=Customizations');
    await tauriPage.waitForSelector('text="LLM Configuration"', 10000);

    // 2. Add Endpoint
    // We mock the prompt because playwright does not auto-handle window.prompt unless we set up a handler.
    await tauriPage.evaluate(() => {
      window.prompt = () => "test-endpoint";
    });
    
    await tauriPage.click('text="Add Endpoint"');
    
    // 3. Edit Endpoint fields
    await tauriPage.waitForSelector('text="New Endpoint: test-endpoint"', 5000);
    
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
    // The new endpoint should not be default initially since we didn't mock deleting the divmora one
    // Actually, let's just make it default
    await tauriPage.click('text="Make Default"');
    await tauriPage.waitForSelector('span:has-text("Default")', 5000);

    // 5. Delete Endpoint
    // We need to mock window.confirm to always return true
    await tauriPage.evaluate(() => {
      window.confirm = () => true;
    });

    await tauriPage.click('text="Delete"');
    
    // Ensure it was deleted
    await expect(tauriPage.locator('text="test-endpoint"')).toHaveCount(0);
  });
});
