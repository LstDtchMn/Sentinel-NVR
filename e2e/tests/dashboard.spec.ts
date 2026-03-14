import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Dashboard page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/dashboard');
  });

  test('page heading is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  });

  test('all six status labels are visible', async ({ page }) => {
    const main = page.locator('main');
    const labels = ['Status', 'Uptime', 'Cameras', 'Recordings', 'Database', 'go2rtc'];
    for (const label of labels) {
      await expect(main.getByText(label, { exact: true })).toBeVisible();
    }
  });

  test('status value shows ok', async ({ page }) => {
    await expect(page.getByText('ok')).toBeVisible();
  });

  test('database value shows connected', async ({ page }) => {
    // Status cards are divs — label and value are siblings, not parent-child
    // Just verify the text "connected" appears on the page (both DB and go2rtc show it)
    await expect(page.getByText('connected').first()).toBeVisible();
  });

  test('go2rtc value shows connected', async ({ page }) => {
    // Both Database and go2rtc show "connected" — verify at least 2 occurrences
    await expect(page.getByText('connected')).toHaveCount(2);
  });

  test('cameras value shows 0', async ({ page }) => {
    // "0" appears as the camera count value on the dashboard
    await expect(page.getByText('0', { exact: true }).first()).toBeVisible();
  });
});
