import { test, expect } from '@playwright/test';

test.use({ storageState: '.auth/user.json' });

test.describe('Settings Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/settings');
  });

  test('displays Settings heading', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  });

  test('Server section: Log Level combobox with info selected', async ({ page }) => {
    await expect(page.getByText('Log Level')).toBeVisible();
    // Log Level combobox may not have an accessible name — select by label proximity
    const logLevelCombobox = page.getByRole('combobox', { name: /log level/i })
      .or(page.getByRole('combobox').first());
    await expect(logLevelCombobox).toHaveValue('info');
  });

  test('Storage section: hot and cold storage paths', async ({ page }) => {
    await expect(page.getByText(/Hot Storage Path/)).toBeVisible();
    await expect(page.getByText('/media/hot', { exact: true }).first()).toBeVisible();
    await expect(page.getByText(/Cold Storage Path/)).toBeVisible();
    await expect(page.getByText('/media/cold', { exact: true }).first()).toBeVisible();
  });

  test('Retention inputs have correct default values', async ({ page }) => {
    await expect(page.getByText('Hot Retention (days)')).toBeVisible();
    // Spinbuttons may not have accessible names matching the label text — use nth index
    const spinbuttons = page.getByRole('spinbutton');
    const hotRetention = spinbuttons.nth(0);
    await expect(hotRetention).toHaveValue('3');

    await expect(page.getByText('Cold Retention (days)')).toBeVisible();
    const coldRetention = spinbuttons.nth(1);
    await expect(coldRetention).toHaveValue('30');

    await expect(page.getByText('Segment Duration (min)')).toBeVisible();
    const segmentDuration = spinbuttons.nth(2);
    await expect(segmentDuration).toHaveValue('10');
  });

  test('Storage Usage section is visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Storage Usage' })).toBeVisible();
    await expect(page.getByText(/Hot storage/)).toBeVisible();
    await expect(page.getByText(/Cold storage/)).toBeVisible();
  });

  test('Event Retention Rules section', async ({ page }) => {
    await expect(page.getByRole('heading', { name: /Event Retention Rules/i })).toBeVisible();
    await expect(page.getByText('Camera', { exact: true }).first()).toBeVisible();
    await expect(page.getByRole('combobox').first()).toBeVisible();
    await expect(page.getByText('Event type', { exact: true })).toBeVisible();
    await expect(page.getByText('Keep (days)', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add Rule' })).toBeVisible();
  });

  test('Detection section displays backend and enabled status', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Detection' })).toBeVisible();
    // Backend and Enabled values are combined in paragraphs: "Backend: remote", "Enabled: No"
    await expect(page.getByText(/Backend:\s*remote/)).toBeVisible();
    await expect(page.getByText(/Enabled:\s*No/)).toBeVisible();
  });

  test('Remote Access section', async ({ page }) => {
    await expect(page.getByText('Remote Access')).toBeVisible();
    const nvrUrlTextbox = page.getByRole('textbox', { name: /nvr url/i });
    await expect(nvrUrlTextbox).toBeVisible();
    await expect(page.getByText(/HTTP/)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Generate QR Code' })).toBeVisible();
  });

  test('Save Settings button appears after making a change', async ({ page }) => {
    // Save button only shows when there are unsaved changes (sticky bar)
    const hotRetention = page.getByRole('spinbutton').nth(0);
    await hotRetention.clear();
    await hotRetention.fill('7');
    await expect(page.getByRole('button', { name: 'Save Settings' })).toBeVisible();
  });

  test('can edit hot retention value', async ({ page }) => {
    const hotRetention = page.getByRole('spinbutton').nth(0);
    await hotRetention.clear();
    await hotRetention.fill('7');
    await expect(hotRetention).toHaveValue('7');
  });
});
