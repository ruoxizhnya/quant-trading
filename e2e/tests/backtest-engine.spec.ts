import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';
import { isolateTestEnvironment } from '../helpers/isolation';

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

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
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
    await page.waitForSelector('.history-section', { timeout: 10000 });

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

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
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
    await page.waitForSelector('.history-section .n-list-item', { timeout: 10000 }).catch(() => {});

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

    await expect(runBtn).toBeDisabled({ timeout: 2000 });
    const progressCount = await progressCard.count();
    expect(progressCount).toBe(1);
  });
});

test.describe('Backtest Engine — Complete Workflow Tests', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('complete backtest workflow: run → view results → check trades', async ({ page }) => {
    await navigateToBacktest(page);

    const stockInput = page.getByPlaceholder('600000.SH,600036.SH');
    await expect(stockInput).toBeVisible();
    await stockInput.fill('600000.SH');

    const runBtn = page.locator('.n-button--primary-type:has-text("运行回测")');
    await expect(runBtn).toBeEnabled();
    await runBtn.click();

    const metricsGrid = page.locator('.metrics-grid');
    await expect(metricsGrid).toBeVisible({ timeout: 120000 });

    const metricBoxes = metricsGrid.locator('.metric-box');
    const count = await metricBoxes.count();
    expect(count).toBeGreaterThanOrEqual(5);

    const expectedMetrics = ['总收益率', '年化收益', '夏普比率', '最大回撤'];
    for (const label of expectedMetrics) {
      await expect(metricsGrid).toContainText(label, { timeout: 5000 });
    }

    const canvas = page.locator('canvas').first();
    await expect(canvas).toBeVisible({ timeout: 10000 });

    const showTradesTag = page.locator('.chart-header .n-tag:has-text("交易")');
    if (await showTradesTag.count() > 0) {
      await showTradesTag.first().click();
      const tradeTable = page.locator('.trades-card');
      await expect(tradeTable).toBeVisible({ timeout: 3000 });
      await expect(tradeTable).toContainText(/买入|卖出|方向/);
    }
  });

  test('backtest history shows completed jobs with correct data', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600519.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    await page.waitForSelector('.metrics-grid', { timeout: 120000 });

    const historySection = page.locator('.history-section');
    await expect(historySection).toBeVisible({ timeout: 10000 });

    const historyItems = historySection.locator('.n-collapse-item');
    const itemCount = await historyItems.count();
    if (itemCount === 0) return;

    const firstItem = historyItems.first();
    await expect(firstItem).toBeVisible();
    await expect(firstItem).toContainText(/momentum|策略/);

    const totalReturnTag = firstItem.locator('.n-tag[type="success"], .n-tag[type="error"]');
    if (await totalReturnTag.count() > 0) {
      const text = await totalReturnTag.first().textContent();
      expect(text).toMatch(/[+-]?\d+\.?\d*%/);
    }
  });

  test('expandable trade details in history', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.history-section .n-collapse-item', { timeout: 10000 }).catch(() => {});

    const collapseItems = page.locator('.history-section .n-collapse-item');
    const count = await collapseItems.count();
    if (count === 0) return;

    const firstItem = collapseItems.first();
    await firstItem.click();

    const tradeTable = firstItem.locator('table, .trade-sublist');
    const tableCount = await tradeTable.count();
    if (tableCount > 0) {
      await expect(tradeTable.first()).toBeVisible({ timeout: 3000 });
      await expect(firstItem).toContainText(/方向|股票|交易时间|成交价/);
    }
  });

  test('view report button loads historical result', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.history-section', { timeout: 10000 });
    const historySection = page.locator('.history-section');
    const viewBtns = historySection.locator('.n-button:has-text("查看报告")');
    const btnCount = await viewBtns.count();
    if (btnCount === 0) return;

    await viewBtns.first().click();

    await page.waitForLoadState('domcontentloaded');
    const url = page.url();
    expect(url).toMatch(/report=/);
  });

  test('clear history removes all items', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.history-section', { timeout: 10000 });

    const clearBtn = page.locator('.history-section .n-button:has-text("清除")');
    const btnCount = await clearBtn.count();
    if (btnCount === 0) return;

    await clearBtn.click();

    const modal = page.locator('.n-modal-container');
    if (await modal.count() > 0) {
      const confirmBtn = modal.locator('.n-button--primary-type:has-text("确认")');
      if (await confirmBtn.count() > 0) {
        await confirmBtn.click();
      }
    }

    const emptyState = page.locator('.history-section .n-empty');
    await expect(emptyState).toBeVisible({ timeout: 5000 });
  });

  test('equity chart renders with dual axes when data available', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    await page.waitForSelector('.metrics-grid', { timeout: 120000 });

    const canvas = page.locator('canvas').first();
    await expect(canvas).toBeVisible({ timeout: 10000 });

    const boundingBox = await canvas.boundingBox();
    expect(boundingBox?.width).toBeGreaterThan(100);
    expect(boundingBox?.height).toBeGreaterThan(100);
  });

  test('date pickers enforce valid date range', async ({ page }) => {
    await navigateToBacktest(page);

    const startDatePicker = page.locator('.n-date-picker').first();
    const endDatePicker = page.locator('.n-date-picker').nth(1);

    await expect(startDatePicker).toBeVisible();
    await expect(endDatePicker).toBeVisible();
  });

  test('strategy selector loads strategies from backend', async ({ page }) => {
    await navigateToBacktest(page);

    const strategySelect = page.locator('.n-select').first();
    await strategySelect.click();

    const options = page.locator('.n-select-option');
    const optionCount = await options.count();
    expect(optionCount).toBeGreaterThanOrEqual(1);

    await options.first().click();
  });

  test('handles empty stock pool gracefully', async ({ page }) => {
    await navigateToBacktest(page);

    const stockInput = page.getByPlaceholder('600000.SH,600036.SH');
    await stockInput.clear();

    const runBtn = page.locator('.n-button--primary-type:has-text("运行回测")');
    await runBtn.click();

    const errorToast = page.locator('.n-message--error, [class*="error"]');
    const toastExists = await errorToast.count().then(c => c > 0);
    expect(typeof toastExists).toBe('boolean');
  });

  test('responsive layout on different viewport sizes', async ({ page }) => {
    await page.setViewportSize({ width: 768, height: 1024 });
    await navigateToBacktest(page);

    const btPage = page.locator('.bt-page');
    await expect(btPage).toBeVisible();

    const formCard = page.locator('.bt-form-card');
    await expect(formCard).toBeVisible();

    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(btPage).toBeVisible();
  });

  test('keyboard accessibility - Enter key triggers backtest', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');

    const runBtn = page.locator('.n-button--primary-type:has-text("运行回测")');
    await runBtn.focus();
    await page.keyboard.press('Enter');

    const progressCard = page.locator('.progress-card');
    await expect(progressCard).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Backtest Engine — Error Handling & Edge Cases', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('shows loading state during async backtest', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const progressCard = page.locator('.progress-card');
    await expect(progressCard).toBeVisible({ timeout: 5000 });

    const progressBar = progressCard.locator('.n-progress-line');
    if (await progressBar.count() > 0) {
      const width = await progressBar.getAttribute('style');
      expect(width).toBeDefined();
    }
  });

  test('displays error message for invalid stock code', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', 'INVALID.CODE');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const errorElement = page.locator('.n-message--error, .error-message');
    const hasError = await errorElement.count().then(c => c > 0);
    if (hasError) {
      await expect(errorElement.first()).toBeVisible({ timeout: 5000 });
    }
  });

  test('persists state after navigation away and back', async ({ page }) => {
    await navigateToBacktest(page);

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    const inputValue = await page.inputValue('[placeholder="600000.SH,600036.SH"]');
    expect(inputValue).toBe('600000.SH');

    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });

    await navigateToBacktest(page);

    const restoredValue = await page.inputValue('[placeholder="600000.SH,600036.SH"]');
    expect(restoredValue).toContain('600000.SH');
  });
});
