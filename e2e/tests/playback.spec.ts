import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Playback page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/playback');
  });

  test('page heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Playback' })).toBeVisible();
  });

  test('camera selector combobox is present', async ({ page }) => {
    // The combobox has no accessible name — use first() or the Select camera... option text
    const combobox = page.getByRole('combobox').first();
    await expect(combobox).toBeVisible();
    await expect(combobox).toContainText('Select camera');
  });

  test('date picker button is present', async ({ page }) => {
    // Date picker shows "Mar 13, 2026" format (locale string), not yyyy-mm-dd
    await expect(page.getByRole('button', { name: /[A-Z][a-z]{2} \d+, \d{4}/ })).toBeVisible();
  });

  test('timeline zoom buttons are visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: '24h' })).toBeVisible();
    await expect(page.getByRole('button', { name: '6h' })).toBeVisible();
    await expect(page.getByRole('button', { name: '2h' })).toBeVisible();
    await expect(page.getByRole('button', { name: '1h' })).toBeVisible();
  });

  test('empty state text is visible', async ({ page }) => {
    await expect(page.getByText('Select a camera and date to start playback')).toBeVisible();
  });

  test('hour markers are visible in the timeline', async ({ page }) => {
    await expect(page.getByText('00:00')).toBeVisible();
    await expect(page.getByText('12:00')).toBeVisible();
  });
});
