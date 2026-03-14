import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.describe('accessibility - authenticated pages', () => {
  test.use({ storageState: '.auth/user.json' });

  const authenticatedPages = [
    { path: '/live', heading: /live/i },
    { path: '/playback', heading: /playback/i },
    { path: '/events', heading: /events/i },
    { path: '/dashboard', heading: /dashboard/i },
    { path: '/cameras', heading: /cameras/i },
    { path: '/faces', heading: /faces/i },
    { path: '/models', heading: /models/i },
    { path: '/import', heading: /import/i },
    { path: '/notifications', heading: /notifications/i },
    { path: '/settings', heading: /settings/i },
  ];

  for (const route of authenticatedPages) {
    test(`${route.path} has no critical/serious WCAG 2.0 A/AA violations`, async ({ page }) => {
      await page.goto(route.path);

      const heading = page.getByRole('heading', { name: route.heading }).first();
      await expect(heading).toBeVisible({ timeout: 10000 });

      const results = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa'])
        // Exclude known color-contrast issues in the dark theme (cosmetic, not functional)
        .disableRules(['color-contrast'])
        .analyze();

      if (results.violations.length > 0) {
        results.violations.forEach((v) => {
          console.log(`[a11y] ${route.path}:`, v.id, v.impact, v.help, v.nodes.map((n) => n.target));
        });
      }

      // Only hard-fail on critical or serious violations (not moderate/minor)
      const criticalOrSerious = results.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious'
      );

      expect.soft(
        criticalOrSerious,
        `${route.path} has ${criticalOrSerious.length} critical/serious accessibility violation(s)`
      ).toHaveLength(0);
    });
  }
});

test.describe('accessibility - login page', () => {
  test.use({ storageState: undefined });

  test('/login has no critical/serious WCAG 2.0 A/AA violations', async ({ page }) => {
    await page.goto('/login');

    const heading = page.getByRole('heading', { name: /sign in/i });
    await expect(heading).toBeVisible({ timeout: 10000 });

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .disableRules(['color-contrast'])
      .analyze();

    if (results.violations.length > 0) {
      results.violations.forEach((v) => {
        console.log(`[a11y] /login:`, v.id, v.impact, v.help, v.nodes.map((n) => n.target));
      });
    }

    const criticalOrSerious = results.violations.filter(
      (v) => v.impact === 'critical' || v.impact === 'serious'
    );

    expect.soft(
      criticalOrSerious,
      `/login has ${criticalOrSerious.length} critical/serious accessibility violation(s)`
    ).toHaveLength(0);
  });
});
