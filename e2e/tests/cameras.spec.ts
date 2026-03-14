import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Cameras Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/cameras');
  });

  test('page heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Cameras' })).toBeVisible();
  });

  test('Add Camera button is visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Add Camera' })).toBeVisible();
  });

  test('empty state shows no cameras message', async ({ page }) => {
    await expect(page.getByText('No cameras configured')).toBeVisible();
  });

  test('empty state shows add first camera button', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Add your first camera' })).toBeVisible();
  });

  test('clicking Add Camera opens form with fields', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    await expect(page.getByRole('heading', { name: 'Add Camera' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: /name/i })).toBeVisible();
    await expect(page.getByRole('textbox', { name: /main stream url/i })).toBeVisible();
    await expect(page.getByRole('textbox', { name: /sub stream url/i })).toBeVisible();
  });

  test('form has correct default toggle states', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    const enabledSwitch = page.getByRole('switch', { name: /enabled/i });
    const recordSwitch = page.getByRole('switch', { name: /record/i });
    const detectSwitch = page.getByRole('switch', { name: /detect/i });
    await expect(enabledSwitch).toBeChecked();
    await expect(recordSwitch).toBeChecked();
    await expect(detectSwitch).not.toBeChecked();
  });

  test('helper text about supported protocols is visible', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    await expect(page.getByText('rtsp://')).toBeVisible();
  });

  test('submit button is disabled when required fields are empty', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    const submitButton = page.getByRole('button', { name: 'Add Camera' }).last();
    await expect(submitButton).toBeDisabled();
  });

  test('shows error for invalid URL', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    await page.getByRole('textbox', { name: /name/i }).fill('Test');
    await page.getByRole('textbox', { name: /main stream url/i }).fill('bad-url');
    await page.getByRole('button', { name: 'Add Camera' }).last().click();
    await expect(page.getByText(/unsupported protocol/i)).toBeVisible();
  });

  test('cancel button closes form', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    await expect(page.getByRole('heading', { name: 'Add Camera' })).toBeVisible();
    await page.getByRole('button', { name: /cancel/i }).click();
    await expect(page.getByRole('heading', { name: 'Add Camera' })).not.toBeVisible();
  });

  test('close (X) button closes form', async ({ page }) => {
    await page.getByRole('button', { name: 'Add Camera' }).click();
    await expect(page.getByRole('heading', { name: 'Add Camera' })).toBeVisible();
    // Close button label is "Close form"
    await page.getByRole('button', { name: /close form/i }).click();
    await expect(page.getByRole('heading', { name: 'Add Camera' })).not.toBeVisible();
  });
});
