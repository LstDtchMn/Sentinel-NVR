import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Import Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/import');
  });

  test('heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Import Cameras' })).toBeVisible();
  });

  test('Blue Iris radio is checked by default', async ({ page }) => {
    const blueIrisRadio = page.getByRole('radio', { name: /blue iris/i });
    await expect(blueIrisRadio).toBeChecked();
  });

  test('Frigate radio is present and unchecked', async ({ page }) => {
    const frigateRadio = page.getByRole('radio', { name: /frigate/i });
    await expect(frigateRadio).toBeVisible();
    await expect(frigateRadio).not.toBeChecked();
  });

  test('file input area shows .reg file text', async ({ page }) => {
    await expect(page.getByText('Choose .reg file...')).toBeVisible();
  });

  test('preview button is visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Preview' })).toBeVisible();
  });

  test('clicking Frigate radio changes file input text', async ({ page }) => {
    await page.getByRole('radio', { name: /frigate/i }).click();
    await expect(page.getByText('Choose .reg file...')).not.toBeVisible();
  });
});
