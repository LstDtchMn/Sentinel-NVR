import { test, expect } from '@playwright/test';

const BASE = 'http://localhost:5173';

test.describe('Authentication', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`${BASE}/login`);
  });

  test('successful login redirects to /live', async ({ page }) => {
    await page.getByLabel(/username/i).fill('admin');
    await page.locator('input[type="password"]').fill('admin');
    await page.getByRole('button', { name: /sign in/i }).click();

    await expect(page).toHaveURL(/\/live/);
    await expect(page.getByRole('heading', { name: /live view/i })).toBeVisible();
  });

  test('wrong credentials shows error', async ({ page }) => {
    await page.getByLabel(/username/i).fill('wronguser');
    await page.locator('input[type="password"]').fill('wrongpass');
    await page.getByRole('button', { name: /sign in/i }).click();

    await expect(page.getByText(/invalid username or password/i)).toBeVisible();
  });

  test('logout redirects to /login and blocks /live', async ({ page }) => {
    // Login first
    await page.getByLabel(/username/i).fill('admin');
    await page.locator('input[type="password"]').fill('admin');
    await page.getByRole('button', { name: /sign in/i }).click();
    await expect(page).toHaveURL(/\/live/);

    // Logout — button has title="Sign out" and/or aria-label
    await page.getByRole('button', { name: /sign out/i }).click();
    await expect(page).toHaveURL(/\/login/);

    // Verify /live redirects back to /login
    await page.goto(`${BASE}/live`);
    await expect(page).toHaveURL(/\/login/);
  });

  test('protected route /settings redirects to /login', async ({ browser }) => {
    // Use a fresh context with explicitly empty storageState to test unauthenticated access
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const page = await context.newPage();
    await page.goto(`${BASE}/settings`);
    await expect(page).toHaveURL(/\/login/, { timeout: 10000 });
    await context.close();
  });

  test('password field has type="password"', async ({ page }) => {
    // type=password inputs are not findable via getByRole('textbox') — use locator
    const passwordInput = page.locator('input[type="password"]');
    await expect(passwordInput).toHaveAttribute('type', 'password');
  });

  test('enter key submits login form', async ({ page }) => {
    await page.getByLabel(/username/i).fill('admin');
    const passwordField = page.locator('input[type="password"]');
    await passwordField.fill('admin');
    await passwordField.press('Enter');

    await expect(page).toHaveURL(/\/live/);
  });
});
