import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('Dashboard Page (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads successfully with correct title', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });

    await expect(page).toHaveTitle(/控制台.*Quant Lab/);

    const greeting = page.locator('.greeting');
    await expect(greeting).toBeVisible();
  });

  test('sidebar navigation renders with all items', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });
    await page.waitForTimeout(1000);

    const navItems = page.locator('.nav-item');
    await expect(navItems.first()).toBeVisible({ timeout: 10000 });
    const count = await navItems.count();
    expect(count).toBe(5);

    await expect(navItems.filter({ hasText: '控制台' })).toBeVisible();
    await expect(navItems.filter({ hasText: '回测引擎' })).toBeVisible();
    await expect(navItems.filter({ hasText: '选股器' })).toBeVisible();
    await expect(navItems.filter({ hasText: '策略 Copilot' })).toBeVisible();
    await expect(navItems.filter({ hasText: '策略实验室' })).toBeVisible();
  });

  test('header shows logo, API status and clock', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.app-header', { timeout: 15000 });

    await expect(page.locator('.logo')).toContainText('Quant Lab');

    const apiTag = page.locator('.app-header .n-tag').first();
    await expect(apiTag).toContainText(/API/);

    const timeTag = page.locator('.app-header .n-tag').nth(1);
    await expect(timeTag).toBeVisible();
    const timeText = await timeTag.textContent();
    expect(timeText?.trim().length).toBeGreaterThan(0);
  });

  test('metrics cards render with labels', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.metric-card', { timeout: 15000 });

    const cards = page.locator('.metric-card');
    const count = await cards.count();
    expect(count).toBe(4);

    await expect(page.locator('.metric-label').filter({ hasText: '股票数' })).toBeVisible();
    await expect(page.locator('.metric-label').filter({ hasText: '回测' })).toBeVisible();
    await expect(page.locator('.metric-label').filter({ hasText: '策略' })).toBeVisible();
  });

  test('quick backtest form renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.quick-bt-card', { timeout: 15000 });

    const card = page.locator('.quick-bt-card');
    await expect(card).toBeVisible();
    await expect(card).toContainText('快速回测');

    await expect(page.locator('.n-select').first()).toBeVisible();
    await expect(page.getByPlaceholder('600000.SH')).toBeVisible();

    const runBtn = page.locator('.n-button--primary-type');
    await expect(runBtn.first()).toBeVisible();
    await expect(runBtn).toContainText(/运行回测/);
  });

  test('navigation tiles are present and link to correct routes', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-tile', { timeout: 15000 });
    await page.waitForTimeout(500);

    const tiles = page.locator('.nav-tile');
    const count = await tiles.count();
    expect(count).toBe(4);

    await expect(tiles.filter({ hasText: '回测引擎' })).toBeVisible();
    await expect(tiles.filter({ hasText: '选股器' })).toBeVisible();
    await expect(tiles.filter({ hasText: '策略 Copilot' })).toBeVisible();
    await expect(tiles.filter({ hasText: '因子分析' })).toBeVisible();
  });

  test('greeting text shows time-appropriate message', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.greeting', { timeout: 15000 });

    const greeting = page.locator('.greeting');
    await expect(greeting).toBeVisible();
    const text = await greeting.textContent();
    expect(text).toMatch(/(早上好|中午好|下午好|晚上好)/);
  });

  test('history card section renders', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.history-card', { timeout: 15000 });

    await expect(page.locator('.history-card')).toBeVisible();
    await expect(page.locator('.history-card')).toContainText('控制台日志');
  });

  test('no naive-ui provider errors in console', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.greeting', { timeout: 15000 });
    await page.waitForTimeout(3000);

    const providerErrors = errors.filter(e =>
      e.includes('n-message-provider') ||
      e.includes('n-dialog-provider') ||
      e.includes('n-notification-provider') ||
      e.includes('n-config-provider')
    );
    expect(providerErrors.length).toBe(0);
  });
});
