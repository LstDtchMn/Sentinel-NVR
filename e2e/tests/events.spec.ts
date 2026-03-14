import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Events page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/events');
  });

  test('page heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible();
  });

  test('live indicator is visible', async ({ page }) => {
    // Use exact match within the heading area to avoid matching "Live View" in sidebar
    await expect(page.locator('span', { hasText: /^Live$/ }).or(page.getByText('Live', { exact: true })).first()).toBeVisible();
  });

  test('total count is visible', async ({ page }) => {
    await expect(page.getByText(/\d+ total/)).toBeVisible();
  });

  test('camera filter combobox is visible', async ({ page }) => {
    // Combobox has no accessible name — use first() and check its text
    const cameraFilter = page.getByRole('combobox').first();
    await expect(cameraFilter).toBeVisible();
    await expect(cameraFilter).toContainText('All cameras');
  });

  test('type filter combobox is visible with correct options', async ({ page }) => {
    // Combobox has no accessible name — use nth(1) for the second combobox
    const typeFilter = page.getByRole('combobox').nth(1);
    await expect(typeFilter).toBeVisible();

    const options = [
      'All types',
      'Detection',
      'Face match',
      'Audio detection',
      'Camera connected',
      'Camera disconnected',
      'Recording started',
      'Recording stopped',
    ];

    for (const option of options) {
      await expect(typeFilter.getByRole('option', { name: option, exact: true })).toBeAttached();
    }
  });

  test('date filter input is present', async ({ page }) => {
    await expect(page.locator('input[type="date"]')).toBeVisible();
  });

  test('event cards render', async ({ page }) => {
    // Event cards are plain divs — verify delete buttons exist as a proxy for cards
    await expect(page.getByRole('button', { name: 'Delete event' }).first()).toBeVisible();
  });

  test('delete buttons are present on cards', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Delete event' }).first()).toBeVisible();
  });
});
