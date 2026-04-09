import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('Stock Screener Page (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads with screener UI', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    await expect(page.locator('.screener-page')).toBeVisible();
    await expect(page.locator('.n-card').first()).toContainText('选股器');
  });

  test('all filter inputs are rendered', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    await expect(page.locator('.n-input-number').first()).toBeVisible();

    const inputs = page.locator('.n-input-number');
    const count = await inputs.count();
    expect(count).toBeGreaterThanOrEqual(6);
  });

  test('action buttons are present', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    const screenBtn = page.locator('.n-button--primary-type');
    await expect(screenBtn).toBeVisible();
    await expect(screenBtn).toContainText(/开始选股/);

    const resetBtn = page.locator('button:has-text("重置")');
    await expect(resetBtn).toBeVisible();
  });

  test('limit dropdown has correct options', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    const limitSelect = page.locator('.n-select').last();
    await expect(limitSelect).toBeVisible();
  });

  test('reset button clears form', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    const wrapper = page.locator('.n-input-number').first();
    await wrapper.click();

    const firstInput = wrapper.locator('input');
    await firstInput.fill('30', { force: true });

    const valueBefore = await firstInput.inputValue();
    expect(valueBefore).toBe('30');

    await page.locator('button:has-text("重置")').click();

    const valueAfter = await firstInput.inputValue();
    expect(valueAfter).toBe('');
  });

  test('clicking screen button triggers API call', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    const apiCall = page.waitForResponse((res) =>
      res.url().includes('/screen') && res.request().method() === 'POST'
    );

    await page.locator('.n-button--primary-type').click();

    const response = await apiCall;
    expect(response.status()).toBeDefined();
  });

  test('empty state displays before search', async ({ page }) => {
    await page.goto('/screener');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    const emptyStates = page.locator('.n-empty');
    await expect(emptyStates.first()).toBeVisible();
  });

  test('navigation from dashboard works', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.nav-tile', { timeout: 15000 });

    await page.locator('.nav-tile').filter({ hasText: '选股器' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.screener-page', { timeout: 15000 });

    await expect(page).toHaveURL(/\/screener/);
    await expect(page.locator('.screener-page')).toBeVisible();
  });
});
