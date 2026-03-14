import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Models Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/models');
  });

  test('heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'AI Models' })).toBeVisible();
  });

  test('displays 4 model cards with correct names', async ({ page }) => {
    // Model names also appear in descriptions — use heading-level or first match
    const main = page.locator('main');
    const modelNames = ['General Security', 'Package Delivery', 'Face Recognition', 'Audio Classification'];
    for (const name of modelNames) {
      await expect(main.getByText(name, { exact: true }).first()).toBeVisible();
    }
  });

  test('each card has Curated badge', async ({ page }) => {
    // Wait for all 4 model cards to render before counting badges
    await expect(page.getByText('General Security')).toBeVisible();
    const badges = page.getByText('Curated', { exact: true });
    await expect(badges).toHaveCount(4);
  });

  test('each card has a Download button', async ({ page }) => {
    // Wait for cards to render before counting
    await expect(page.getByText('General Security')).toBeVisible();
    const downloadButtons = page.getByRole('button', { name: 'Download' });
    await expect(downloadButtons).toHaveCount(4);
  });

  test('upload section is visible', async ({ page }) => {
    await expect(page.getByText('Upload Custom Model')).toBeVisible();
  });

  test('file input is present', async ({ page }) => {
    await expect(page.locator('input[type="file"]')).toBeAttached();
  });

  test('upload button is visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Upload' })).toBeVisible();
  });
});
