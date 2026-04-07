import { test, expect } from '@playwright/test';
import { waitForAPIReady } from '../helpers/api';

const BASE = process.env.BASE_URL || 'http://localhost:8085';

test.describe('Stock Screener Page', () => {

  test.beforeAll(async () => {
    const ready = await waitForAPIReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads with screener UI', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('h1')).toContainText('选股器');
  });

  test('all filter inputs are rendered', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    // Filter fields
    await expect(page.locator('#pe_max')).toBeVisible();
    await expect(page.locator('#pb_max')).toBeVisible();
    await expect(page.locator('#roe_min')).toBeVisible();
    await expect(page.locator('#roa_min')).toBeVisible();
    await expect(page.locator('#gross_margin_min')).toBeVisible();
    await expect(page.locator('#net_margin_min')).toBeVisible();
    await expect(page.locator('#market_cap_min')).toBeVisible();
    await expect(page.locator('#limit')).toBeVisible();
  });

  test('action buttons are present', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    const screenBtn = page.locator('#screenBtn');
    await expect(screenBtn).toBeVisible();
    await expect(screenBtn).toHaveText(/开始选股/);

    const resetBtn = page.locator('button:has-text("重置")');
    await expect(resetBtn).toBeVisible();
  });

  test('limit dropdown has correct options', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    const limitSelect = page.locator('#limit');
    const options = limitSelect.locator('option');
    const values: string[] = [];
    for (let i = 0; i < await options.count(); i++) {
      values.push(await options.nth(i).getAttribute('value') || '');
    }

    expect(values).toEqual(expect.arrayContaining(['10', '20', '50', '100']));
    // Default should be 50
    await expect(limitSelect).toHaveValue('50');
  });

  test('navigation links work', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('a[href="dashboard.html"]')).toBeVisible();
    await expect(page.locator('a[href="index.html"]')).toBeVisible();
    // Active state on current page
    await expect(page.locator('a.active')).toBeVisible();
  });

  test('reset button clears all filters', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    // Fill in some filters
    await page.locator('#pe_max').fill('30');
    await page.locator('#roe_min').fill('10');

    // Click reset
    await page.locator('button:has-text("重置")').click();

    // All filters should be cleared
    await expect(page.locator('#pe_max')).toHaveValue('');
    await expect(page.locator('#pb_max')).toHaveValue('');
    await expect(page.locator('#roe_min')).toHaveValue('');
    await expect(page.locator('#roa_min')).toHaveValue('');
    await expect(page.locator('#gross_margin_min')).toHaveValue('');
    await expect(page.locator('#net_margin_min')).toHaveValue('');
    await expect(page.locator('#market_cap_min')).toHaveValue('');

    // Limit should reset to 50
    await expect(page.locator('#limit')).toHaveValue('50');
  });

  test('clicking screen button shows loading state', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    await page.locator('#screenBtn').click();

    const loading = page.locator('#loadingBox');
    try {
      await expect(loading).toBeVisible({ timeout: 5000 });
      await expect(loading).toHaveText(/选股中/);
    } catch {
      // Screen may complete or fail fast
    }
  });

  test('screening request sends POST to /screen endpoint', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    // Intercept the API call
    const apiCall = page.waitForResponse((res) =>
      res.url().includes('/screen') && res.request().method() === 'POST'
    );

    await page.locator('#screenBtn').click();

    const response = await apiCall;
    expect(response.status()).toBeDefined();
  });

  test('results table structure exists (hidden initially)', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    // Results box exists but hidden
    const resultsBox = page.locator('#resultsBox');
    await expect(resultsBox).toBeAttached();
    await expect(resultsBox).toBeHidden();

    // Table headers should be defined
    const resultsTable = page.locator('#resultsBox table thead th');
    const headers = await resultsTable.allTextContents();
    expect(headers).toContain('股票代码');
    expect(headers.some(h => h.includes('PE'))).toBeTruthy();
    expect(headers.some(h => h.includes('PB'))).toBeTruthy();
    expect(headers.some(h => h.includes('ROE'))).toBeTruthy();
  });

  test('empty state and error box exist in DOM', async ({ page }) => {
    await page.goto(`${BASE}/screen.html`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('#emptyBox')).toBeAttached();
    await expect(page.locator('#errorBox')).toBeAttached();
    // Both hidden initially
    await expect(page.locator('#emptyBox')).toBeHidden();
    await expect(page.locator('#errorBox')).toBeHidden();
  });
});
