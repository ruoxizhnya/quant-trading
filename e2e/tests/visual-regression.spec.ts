import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';
import { isolateTestEnvironment } from '../helpers/isolation';

/**
 * P2-26: Visual regression tests — screenshot comparison for key pages.
 *
 * Uses Playwright's toHaveScreenshot() for pixel-diff comparison.
 * First run generates baseline screenshots; subsequent runs compare.
 * Update baselines with: npx playwright test --update-snapshots
 */

test.describe('Visual Regression — Key Pages', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  // ─── Dashboard ────────────────────────────────────────────

  test('dashboard renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('dashboard.png');
  });

  test('dashboard sidebar visible', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.waitForSelector('.nav-item', { timeout: 10000 });
    await page.waitForTimeout(300);
    const sidebar = page.locator('.n-layout-sider, .sidebar, nav').first();
    await expect(sidebar).toHaveScreenshot('dashboard-sidebar.png');
  });

  test('dashboard metric cards render', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    const cards = page.locator('.metric-card, .quick-bt-card, .nav-tile');
    if (await cards.count() > 0) {
      await expect(cards.first()).toHaveScreenshot('dashboard-metric-card.png');
    }
  });

  // ─── Backtest Engine ──────────────────────────────────────

  test('backtest page renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.locator('.nav-item').filter({ hasText: '回测引擎' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.bt-page', { timeout: 20000 });
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('backtest.png');
  });

  test('backtest form visible', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.locator('.nav-item').filter({ hasText: '回测引擎' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.bt-page', { timeout: 20000 });
    const form = page.locator('.n-card').first();
    await expect(form).toHaveScreenshot('backtest-form.png');
  });

  // ─── AI Research ──────────────────────────────────────────

  test('ai research factor lab renders', async ({ page }) => {
    await page.goto('/ai-research/factor-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.factor-lab-page, .ai-research-page', { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('ai-factor-lab.png');
  });

  test('ai research strategy workshop renders', async ({ page }) => {
    await page.goto('/ai-research/strategy-workshop');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-workshop-page, .ai-research-page', { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('ai-strategy-workshop.png');
  });

  test('ai research evolution renders', async ({ page }) => {
    await page.goto('/ai-research/evolution');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.ai-research-page', { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('ai-evolution.png');
  });

  // ─── Stock Screener ───────────────────────────────────────

  test('stock screener renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.locator('.nav-item').filter({ hasText: '选股器' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('stock-screener.png');
  });

  // ─── Data Sync ────────────────────────────────────────────

  test('data sync page renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.locator('.nav-item').filter({ hasText: '数据同步' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('data-sync.png');
  });

  // ─── Responsive Layout ────────────────────────────────────

  test('dashboard responsive on mobile viewport', async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 375, height: 667 } });
    const page = await context.newPage();
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('dashboard-mobile.png');
    await context.close();
  });

  test('backtest responsive on tablet viewport', async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 768, height: 1024 } });
    const page = await context.newPage();
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.locator('.nav-item').filter({ hasText: '回测引擎' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});
    await page.waitForTimeout(500);
    await expect(page).toHaveScreenshot('backtest-tablet.png');
    await context.close();
  });
});
