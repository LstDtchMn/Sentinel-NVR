import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Live View page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/live');
  });

  test('page heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Live View' })).toBeVisible();
  });

  test('empty state shows no cameras message', async ({ page }) => {
    await expect(page.getByText('No cameras to display')).toBeVisible();
  });

  test('empty state shows helper text', async ({ page }) => {
    await expect(page.getByText('Add cameras on the Cameras page to see live video here')).toBeVisible();
  });
});
