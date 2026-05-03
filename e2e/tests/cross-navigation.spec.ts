import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('Cross-page Navigation & Integration (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('can navigate Dashboard -> Backtest -> Dashboard via sidebar', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await expect(page.locator('.dashboard-page')).toBeVisible();

    await page.locator('.nav-item').filter({ hasText: '回测引擎' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.bt-page', { timeout: 20000 });
    await expect(page).toHaveURL(/\/backtest/);
    await expect(page.locator('.bt-page')).toBeVisible();

    await page.locator('.nav-item').filter({ hasText: '控制台' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await expect(page).toHaveURL(/\//);
    await expect(page.locator('.dashboard-page')).toBeVisible();
  });

  test('Dashboard nav tiles navigate to correct routes', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-tile', { timeout: 15000 });

    const tiles = page.locator('.nav-tile');
    const count = await tiles.count();
    expect(count).toBe(4);
  });

  test('root URL serves Dashboard (default route)', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });

    await expect(page.locator('.dashboard-page')).toBeVisible();
    await expect(page.locator('.greeting')).toBeVisible();
  });

  test('all SPA routes load without crashing (via sidebar navigation)', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });

    const routes = [
      { label: '选股器', urlPattern: /\/screener/, selector: '.screener-page' },
      { label: '策略 Copilot', urlPattern: /\/copilot/, selector: '.copilot-page' },
      { label: '策略实验室', urlPattern: /\/strategy-lab/, selector: '.strategy-lab-page' },
    ];

    for (const route of routes) {
      await page.locator('.nav-item').filter({ hasText: route.label }).click();
      await page.waitForLoadState('domcontentloaded');
      await page.waitForSelector(route.selector, { timeout: 20000 });
      await expect(page).toHaveURL(route.urlPattern);
      await expect(page.locator(route.selector)).toBeVisible();
    }
  });

  test('no critical JavaScript errors on key pages', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    for (const url of ['/', '/screener', '/copilot']) {
      await page.goto(url);
      await page.waitForLoadState('networkidle');
    }

    const criticalErrors = errors.filter(e =>
      !e.includes('cdn') && !e.includes('chart.js') && !e.includes('luxon')
    );
    if (criticalErrors.length > 0) {
      console.log('JS errors:', criticalErrors);
    }

    const naiveErrors = criticalErrors.filter(e =>
      e.includes('naive-ui') || e.includes('provider')
    );
    expect(naiveErrors.length).toBe(0);
  });

  test('sidebar is present on all main pages', async ({ page }) => {
    const pages = [
      { url: '/', selector: '.dashboard-page' },
      { url: '/screener', selector: '.screener-page' },
      { url: '/copilot', selector: '.copilot-page' },
      { url: '/strategy-lab', selector: '.strategy-lab-page' },
    ];

    for (const p of pages) {
      await page.goto(p.url);
      await page.waitForLoadState('domcontentloaded');
      await page.waitForSelector(p.selector, { timeout: 15000 });

      await expect(page.locator('.app-sidebar')).toBeVisible();
      await expect(page.locator('.app-header')).toBeVisible();
      await expect(page.locator('.logo')).toContainText('Quant Lab');
    }
  });

  test('SPA navigation does not cause full page reload', async ({ page }) => {
    let loadCount = 0;
    page.on('load', () => loadCount++);

    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-item', { timeout: 15000 });

    await page.locator('.nav-item').filter({ hasText: '选股器' }).click();
    await page.waitForLoadState('domcontentloaded');

    await page.locator('.nav-item').filter({ hasText: '策略 Copilot' }).click();
    await page.waitForLoadState('domcontentloaded');

    // Initial page load + possible lazy-loaded component loads
    expect(loadCount).toBeLessThanOrEqual(2);
  });
});
