import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

const BASE = 'http://localhost:5173';

const NAV_ITEMS = [
  { name: 'Live View', path: '/live', heading: /live view/i },
  { name: 'Playback', path: '/playback', heading: /playback/i },
  { name: 'Events', path: '/events', heading: /events/i },
  { name: 'Dashboard', path: '/dashboard', heading: /dashboard/i },
  { name: 'Cameras', path: '/cameras', heading: /cameras/i },
  { name: 'Faces', path: '/faces', heading: /faces/i },
  { name: 'Models', path: '/models', heading: /models/i },
  { name: 'Import', path: '/import', heading: /import/i },
  { name: 'Notifications', path: '/notifications', heading: /Notification Settings/i },
  { name: 'Settings', path: '/settings', heading: /settings/i },
];

test.describe('Sidebar Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`${BASE}/live`);
    await expect(page).toHaveURL(/\/live/);
  });

  for (const item of NAV_ITEMS) {
    test(`navigates to ${item.path}`, async ({ page }) => {
      await page.getByRole('link', { name: item.name, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(item.path));
      await expect(page.getByRole('heading', { name: item.heading })).toBeVisible();
    });
  }

  test('active link has sentinel-500 class', async ({ page }) => {
    for (const item of NAV_ITEMS.slice(0, 3)) {
      await page.getByRole('link', { name: item.name, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(item.path));

      const link = page.getByRole('link', { name: item.name, exact: true });
      // Active link uses "text-sentinel-500" and/or "bg-sentinel-500/10"
      await expect(link).toHaveClass(/sentinel-500/);
    }
  });

  test('only one active link at a time', async ({ page }) => {
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await expect(page).toHaveURL(/\/settings/);

    const nav = page.locator('nav');
    const activeLinks = nav.locator('[class*="sentinel-500"]');
    await expect(activeLinks).toHaveCount(1);
  });

  test('unknown route /nonexistent redirects to /live', async ({ page }) => {
    await page.goto(`${BASE}/nonexistent`);
    await expect(page).toHaveURL(/\/live/);
  });

  test('root / redirects to /live', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await expect(page).toHaveURL(/\/live/);
  });

  test('sidebar footer shows version string', async ({ page }) => {
    await expect(page.getByText(/Sentinel NVR v/i)).toBeVisible();
  });
});
