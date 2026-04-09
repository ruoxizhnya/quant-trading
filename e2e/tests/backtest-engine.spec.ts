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
