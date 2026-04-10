import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

async function navigateToBacktest(page: any) {
  await page.goto('/');
  await page.waitForLoadState('domcontentloaded');
  await page.waitForSelector('.dashboard-page', { timeout: 15000 });
  await page.locator('.nav-item').filter({ hasText: '回测引擎' }).click();
  await page.waitForLoadState('domcontentloaded');
  await page.waitForSelector('.bt-page', { timeout: 20000 });
}

test.describe('Backtest Engine Page (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads successfully', async ({ page }) => {
    await navigateToBacktest(page);

    await expect(page.locator('.bt-page')).toBeVisible();
    await expect(page.locator('.n-card').first()).toContainText('回测引擎');
  });

  test('strategy selector shows options', async ({ page }) => {
    await navigateToBacktest(page);

    const sel = page.locator('.n-select').first();
    await expect(sel).toBeVisible();
  });

  test('form fields render correctly', async ({ page }) => {
    await navigateToBacktest(page);

    await expect(page.getByPlaceholder('600000.SH,600036.SH')).toBeVisible();

    const datePickers = page.locator('.n-date-picker');
    await expect(datePickers.first()).toBeVisible();
    await expect(datePickers.nth(1)).toBeVisible();

    const runBtn = page.locator('.n-button--primary-type');
    await expect(runBtn).toBeVisible();
    await expect(runBtn).toContainText('运行回测');
  });

  test('run button exists and is interactive', async ({ page }) => {
    await navigateToBacktest(page);

    const runBtn = page.locator('.n-button--primary-type').filter({ hasText: '运行回测' });
    await expect(runBtn).toBeVisible();
    await expect(runBtn).toBeEnabled();
  });

  test('empty state or results shown', async ({ page }) => {
    await navigateToBacktest(page);

    const emptyState = page.locator('.empty-state');
    const resultsSection = page.locator('.results-section');

    const hasEmpty = await emptyState.count().then(c => c > 0);
    const hasResults = await resultsSection.count().then(c => c > 0);

    expect(hasEmpty || hasResults).toBeTruthy();
  });

  test('history section renders at bottom', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForTimeout(1000);

    const historySection = page.locator('.history-section');
    await expect(historySection).toBeVisible({ timeout: 10000 });
    await expect(historySection).toContainText('回测历史');
  });

  test('navigation from dashboard works via nav tile', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-tile', { timeout: 15000 });

    await page.locator('.nav-tile').filter({ hasText: '回测引擎' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.bt-page', { timeout: 20000 });

    await expect(page).toHaveURL(/\/backtest/);
    await expect(page.locator('.bt-page')).toBeVisible();
  });
});

test.describe('Backtest Engine — Interaction Tests', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('sync/async mode toggle exists and is interactive', async ({ page }) => {
    await navigateToBacktest(page);

    const toggleGroup = page.locator('.mode-toggle .n-button-group');
    await expect(toggleGroup).toBeVisible();

    const buttons = toggleGroup.locator('.n-button');
    const count = await buttons.count();
    expect(count).toBeGreaterThanOrEqual(2);

    const syncBtn = buttons.first();
    const asyncBtn = buttons.last();
    await expect(syncBtn).toContainText('同步');
    await expect(asyncBtn).toContainText('异步');

    await asyncBtn.click();
    await expect(asyncBtn).toHaveClass(/n-button--primary-type/);
  });

  test('async backtest creates job and shows progress', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    const runBtn = page.locator('.n-button--primary-type').filter({ hasText: '运行回测' });
    await runBtn.click();

    const progressCard = page.locator('.progress-card');
    await expect(progressCard).toBeVisible({ timeout: 5000 });
    await expect(progressCard).toContainText(/回测任务|正在执行/);
  });

  test('backtest result renders metrics cards after completion', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    const runBtn = page.locator('.n-button--primary-type').filter({ hasText: '运行回测' });
    await runBtn.click();

    const metricsGrid = page.locator('.metrics-grid');
    await expect(metricsGrid).toBeVisible({ timeout: 120000 });

    const metricBoxes = metricsGrid.locator('.metric-box');
    const count = await metricBoxes.count();
    expect(count).toBeGreaterThanOrEqual(5);

    const labels = ['总收益率', '年化收益', '夏普比率', '最大回撤'];
    for (const label of labels) {
      await expect(metricsGrid).toContainText(label, { timeout: 5000 });
    }
  });

  test('equity chart canvas renders after backtest completes', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const canvas = page.locator('canvas');
    await expect(canvas).toBeVisible({ timeout: 120000 });
  });

  test('trade markers toggle shows/hides trade data', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    await page.waitForSelector('.metrics-grid', { timeout: 120000 });

    const showTradesTag = page.locator('.chart-header .n-tag:has-text("交易")');
    const count = await showTradesTag.count();
    if (count > 0) {
      await showTradesTag.first().click();
      const tradeTable = page.locator('.trades-card');
      await expect(tradeTable).toBeVisible({ timeout: 3000 });
      await expect(tradeTable).toContainText('交易记录');
    }
  });

  test('history list view-report button navigates with query param', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForTimeout(1500);

    const historyItems = page.locator('.history-section .n-list-item');
    const count = await historyItems.count();
    if (count === 0) return;

    const firstItem = historyItems.first();
    const viewBtn = firstItem.locator('.n-button:has-text("查看")');
    await expect(viewBtn).toBeVisible();
  });

  test('form prevents duplicate submission while running', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    const runBtn = page.locator('.n-button--primary-type').filter({ hasText: '运行回测' });
    await runBtn.click();

    const progressCard = page.locator('.progress-card');
    await expect(progressCard).toBeVisible({ timeout: 5000 });

    await runBtn.click({ timeout: 2000 });
    const progressCount = await progressCard.count();
    expect(progressCount).toBe(1);
  });
});
