import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Notification Settings Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/notifications');
    await expect(page.getByRole('heading', { name: 'Notification Settings' })).toBeVisible();
  });

  test('displays page heading and description', async ({ page }) => {
    const description = page.getByText(/Register device tokens and configure/i);
    await expect(description).toBeVisible();
  });

  test('displays Device Tokens section with empty state', async ({ page }) => {
    const main = page.locator('main');
    await expect(main.getByText('Device Tokens').first()).toBeVisible();
    await expect(main.getByText('No tokens registered yet.')).toBeVisible();
  });

  test('displays register token form with correct defaults', async ({ page }) => {
    const typeCombobox = page.getByRole('combobox').first();
    await expect(typeCombobox).toHaveValue('webhook');

    await expect(page.getByRole('button', { name: /Register Token/i })).toBeVisible();
  });

  test('displays Alert Preferences section', async ({ page }) => {
    await expect(page.getByText('Alert Preferences')).toBeVisible();
  });

  test('Enabled checkbox is checked by default', async ({ page }) => {
    const enabledCheckbox = page.getByRole('checkbox', { name: /enabled/i });
    await expect(enabledCheckbox).toBeVisible();
    await expect(enabledCheckbox).toBeChecked();
  });

  test('Critical alert checkbox is visible', async ({ page }) => {
    const criticalCheckbox = page.getByRole('checkbox', { name: /critical alert/i });
    await expect(criticalCheckbox).toBeVisible();
  });

  test('Save Preference button is visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Save Preference' })).toBeVisible();
  });
});
