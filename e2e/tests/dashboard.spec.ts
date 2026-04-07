import { test, expect, Page } from '@playwright/test';
import { waitForAPIReady } from '../helpers/api';

const BASE = process.env.BASE_URL || 'http://localhost:8085';

test.describe('Dashboard Page', () => {

  test.beforeAll(async () => {
    const ready = await waitForAPIReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads successfully with correct title', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('.logo-text')).toContainText('Quant Lab');
    await expect(page.locator('#greeting')).toBeVisible({ timeout: 5000 });
  });

  test('header shows clock and service status indicators', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // Clock should be visible
    const clock = page.locator('#clock');
    await expect(clock).toBeVisible();
    const clockText = await clock.textContent();
    expect(clockText?.trim().length).toBeGreaterThan(0);

    // Status dots should exist
    await expect(page.locator('#apiDot')).toBeVisible();
    await expect(page.locator('#dataDot')).toBeVisible();
  });

  test('stats cards render with labels', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for stats to load (they fetch asynchronously)
    await page.waitForTimeout(3000);

    // Check stat cards exist
    await expect(page.locator('#stat-stocks')).toBeVisible();
    await expect(page.locator('#stat-bt-count')).toBeVisible();
    await expect(page.locator('#stat-strategies')).toBeVisible();
    await expect(page.locator('#stat-sync')).toBeVisible();

    // Labels should be present
    await expect(page.locator('.stat-label').filter({ hasText: '股票数' })).toBeVisible();
    await expect(page.locator('.stat-label').filter({ hasText: '回测' })).toBeVisible();
    await expect(page.locator('.stat-label').filter({ hasText: '策略' })).toBeVisible();
  });

  test('quick run form renders correctly', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // Quick form elements
    await expect(page.locator('#qf-strategy')).toBeVisible();
    await expect(page.locator('#qf-stock')).toBeVisible();
    await expect(page.locator('#qf-start')).toBeVisible();
    await expect(page.locator('#qf-end')).toBeVisible();

    // Run button
    const runBtn = page.locator('.qf-btn');
    await expect(runBtn).toBeVisible();
    await expect(runBtn).toHaveText(/运行回测/);
  });

  test('navigation tiles are present and link correctly', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // Nav tiles should include 回测引擎, 选股器, 策略 Copilot
    const tiles = page.locator('.nav-tile');
    const count = await tiles.count();
    expect(count).toBeGreaterThanOrEqual(3);

    await expect(tiles.filter({ hasText: '回测引擎' })).toBeVisible();
    await expect(tiles.filter({ hasText: '选股器' })).toBeVisible();
    await expect(tiles.filter({ hasText: 'Copilot' })).toBeVisible();
  });

  test('system status section renders', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for async data to load
    await page.waitForTimeout(3000);

    // System status rows
    await expect(page.locator('#sys-api')).toBeVisible();
    await expect(page.locator('#sys-db')).toBeVisible();
    await expect(page.locator('#sys-engine')).toBeVisible();
  });

  test('market overview pills display', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    await page.waitForTimeout(3000);

    // Market pills should exist
    await expect(page.locator('#m-sh300')).toBeVisible();
    await expect(page.locator('#m-sse')).toBeVisible();
    await expect(page.locator('#m-cyb')).toBeVisible();
  });

  test('header navigation tiles have correct hrefs', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    // 回测引擎 tile links to index.html
    const backtestTile = page.locator('.nav-tile').filter({ hasText: '回测引擎' });
    await expect(backtestTile).toBeVisible();

    // 选股器 tile exists
    const screenerTile = page.locator('.nav-tile').filter({ hasText: '选股器' });
    await expect(screenerTile).toBeVisible();

    // Copilot tile exists
    const copilotTile = page.locator('.nav-tile').filter({ hasText: 'Copilot' });
    await expect(copilotTile).toBeVisible();
  });

  test('greeting text changes based on time of day', async ({ page }) => {
    await page.goto(`${BASE}/dashboard.html`);
    await page.waitForLoadState('domcontentloaded');

    const greeting = page.locator('#greeting');
    await expect(greeting).toBeVisible();
    const text = await greeting.textContent();
    expect(text).toMatch(/(早上好|中午好|下午好|晚上好)/);
  });
});
