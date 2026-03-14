import { test as setup, expect } from '@playwright/test';

setup('authenticate', async ({ page }) => {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Username' }).fill('admin');
  await page.getByRole('textbox', { name: 'Password' }).fill('admin');
  await page.getByRole('button', { name: 'Sign in' }).click();

  // Wait for redirect to /live after successful login
  await page.waitForURL('**/live');
  await expect(page.getByRole('heading', { name: 'Live View' })).toBeVisible();

  // Save auth state for reuse
  await page.context().storageState({ path: '.auth/user.json' });
});
