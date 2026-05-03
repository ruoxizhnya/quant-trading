import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('Strategy Lab Page (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads with strategy lab UI', async ({ page }) => {
    await page.goto('/strategy-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });

    await expect(page.locator('.strategy-lab-page')).toBeVisible();
    await expect(page.locator('.lab-header h1')).toContainText('策略实验室');
  });

  test('header shows title and action buttons', async ({ page }) => {
    await page.goto('/strategy-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });

    await expect(page.locator('.lab-header h1')).toBeVisible();

    const createBtn = page.locator('.n-button--primary-type').first();
    await expect(createBtn).toBeVisible();
    await expect(createBtn).toContainText(/新建策略/);
  });

  test('search and filter controls render', async ({ page }) => {
    await page.goto('/strategy-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });

    await expect(page.getByPlaceholder('搜索策略')).toBeVisible();
  });

  test('strategy cards render with data', async ({ page }) => {
    await page.goto('/strategy-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });
    await page.waitForSelector('.strategy-card, .n-empty', { timeout: 10000 });

    const cards = page.locator('.strategy-card');
    const count = await cards.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('empty state or cards display correctly', async ({ page }) => {
    await page.goto('/strategy-lab');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });
    await page.waitForSelector('.strategy-card, .n-empty', { timeout: 10000 });

    const cards = page.locator('.strategy-card');
    const cardCount = await cards.count();

    if (cardCount === 0) {
      const emptyState = page.locator('.n-empty');
      await expect(emptyState).toBeVisible();
    } else {
      await expect(cards.first()).toBeVisible();
    }
  });

  test('navigation from dashboard works', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-tile', { timeout: 15000 });

    await page.locator('.nav-tile').filter({ hasText: '因子分析' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.strategy-lab-page', { timeout: 15000 });

    await expect(page).toHaveURL(/\/strategy-lab/);
    await expect(page.locator('.strategy-lab-page')).toBeVisible();
  });
});
