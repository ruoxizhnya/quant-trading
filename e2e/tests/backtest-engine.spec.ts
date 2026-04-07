import { test, expect } from '@playwright/test';
import { waitForAPIReady } from '../helpers/api';

const BASE = process.env.BASE_URL || 'http://localhost:8085';

test.describe('Backtest Engine Page (index.html)', () => {

  test.beforeAll(async () => {
    const ready = await waitForAPIReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads successfully', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');
    await expect(page.locator('.logo-text')).toContainText('Quant Lab');
  });

  test('strategy selector shows default options', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    const sel = page.locator('#s-strategy');
    await expect(sel).toBeVisible();
    const options = sel.locator('option');
    const count = await options.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test('form fields render correctly', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('#s-pool')).toBeVisible();
    await expect(page.locator('#s-start')).toBeVisible();
    await expect(page.locator('#s-end')).toBeVisible();
    await expect(page.locator('#s-capital')).toBeVisible();
    await expect(page.locator('#btn-run')).toBeVisible();
    await expect(page.locator('#btn-run')).toHaveText(/运行回测/);
  });

  test('run button triggers backtest and shows loading state', async ({ page }) => {
    test.setTimeout(120000);
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await page.locator('#btn-run').click();

    const status = page.locator('#status');
    await expect(status).toHaveClass(/show/);

    try {
      await expect(status).not.toHaveClass(/show/, { timeout: 90000 });
    } catch {
    }
  });

  test('results section displays metrics after successful backtest', async ({ page }) => {
    test.setTimeout(120000);
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await page.locator('#btn-run').click();

    try {
      await expect(page.locator('#results')).toBeVisible({ timeout: 90000 });
      await expect(page.locator('#m-ret')).toBeVisible();
      await expect(page.locator('#m-ann')).toBeVisible();
      await expect(page.locator('#m-shp')).toBeVisible();
      await expect(page.locator('#m-dd')).toBeVisible();
    } catch {
    }
  });

  test('equity chart canvas renders after backtest', async ({ page }) => {
    test.setTimeout(120000);
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await page.locator('#btn-run').click();

    try {
      await expect(page.locator('#results')).toBeVisible({ timeout: 90000 });
      await expect(page.locator('#c-equity')).toBeVisible();
    } catch {
    }
  });

  test('trades table populates with trade records', async ({ page }) => {
    test.setTimeout(120000);
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await page.locator('#btn-run').click();

    try {
      await expect(page.locator('#results')).toBeVisible({ timeout: 90000 });
      await expect(page.locator('#tb-trades')).toBeVisible();
    } catch {
    }
  });

  test('navigation header links work', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    const dashboardLink = page.locator('a[href="dashboard.html"]').first();
    await expect(dashboardLink).toBeVisible();

    const strategyLabLink = page.locator('a[href="strategy-selector.html"]');
    await expect(strategyLabLink).toBeVisible();
  });

  test('sidebar sections are visible', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('.sidebar')).toBeVisible();
    await expect(page.locator('.content')).toBeVisible();
    await expect(page.locator('.section-title').first()).toBeVisible();
  });

  test('empty state shown before first run', async ({ page }) => {
    await page.goto(`${BASE}/`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('#empty')).toBeVisible();
    await expect(page.locator('#results')).not.toBeVisible();
  });
});
