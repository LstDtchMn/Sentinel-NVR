import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Faces Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/faces');
  });

  test('heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Enrolled Faces' })).toBeVisible();
  });

  test('counter shows 0 enrolled', async ({ page }) => {
    await expect(page.getByText('0 enrolled')).toBeVisible();
  });

  test('enroll section heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Enroll from Photo' })).toBeVisible();
  });

  test('name textbox with placeholder is present', async ({ page }) => {
    const nameInput = page.getByPlaceholder('e.g. Alice Smith');
    await expect(nameInput).toBeVisible();
  });

  test('choose file button is present', async ({ page }) => {
    await expect(page.locator('input[type="file"]')).toBeAttached();
  });

  test('enroll face button is present and disabled', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Enroll Face' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Enroll Face' })).toBeDisabled();
  });

  test('raw embedding API section expands on click', async ({ page }) => {
    const toggleButton = page.getByRole('button', { name: /raw embedding api/i });
    await expect(toggleButton).toBeVisible();
    await toggleButton.click();
    await expect(page.getByText(/embedding/i).last()).toBeVisible();
  });

  test('empty state shows no faces enrolled', async ({ page }) => {
    await expect(page.getByText('No faces enrolled')).toBeVisible();
  });
});
