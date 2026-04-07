import { test, expect } from '@playwright/test';
import { waitForAPIReady } from '../helpers/api';

const BASE = process.env.BASE_URL || 'http://localhost:8085';

test.describe('Copilot Page', () => {

  test.beforeAll(async () => {
    const ready = await waitForAPIReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads with chat interface', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    // Header
    await expect(page.locator('.logo-text')).toContainText('Copilot');
    await expect(page.locator('.header-badge')).toContainText('AI');

    // Chat area
    await expect(page.locator('#chatArea')).toBeVisible();

    // Input area
    await expect(page.locator('#msgInput')).toBeVisible();
    await expect(page.locator('#sendBtn')).toBeVisible();
  });

  test('welcome message displays with suggestion chips', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    // Welcome message should contain greeting text
    await expect(page.locator('.msg.ai')).toBeVisible();
    await expect(page.locator('.msg-text').filter({ hasText: 'Copilot' })).toBeVisible();

    // Suggestion chips should be present
    const suggestions = page.locator('.sugg');
    const count = await suggestions.count();
    expect(count).toBeGreaterThanOrEqual(3);

    // Known suggestions
    await expect(suggestions.filter({ hasText: 'RSI' })).toBeVisible();
    await expect(suggestions.filter({ hasText: 'MACD' })).toBeVisible();
    await expect(suggestions.filter({ hasText: '布林带' })).toBeVisible();
  });

  test('clicking suggestion chip fills input', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    // Click first suggestion chip
    const chip = page.locator('.sugg').first();
    await chip.click();

    // Input should now have text
    const input = page.locator('#msgInput');
    const value = await input.inputValue();
    expect(value.length).toBeGreaterThan(0);
  });

  test('send button is enabled by default', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('#sendBtn')).toBeEnabled();
  });

  test('input placeholder text is present', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    const input = page.locator('#msgInput');
    await expect(input).toHaveAttribute('placeholder', /描述.*策略/);
  });

  test('header back link points to dashboard', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    const backLink = page.locator('.header-back');
    await expect(backLink).toBeVisible();
    await expect(backLink).toHaveAttribute('href', 'dashboard.html');
  });

  test('sending a message shows user message bubble', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    // Type and send
    await page.locator('#msgInput').fill('测试消息');
    await page.locator('#sendBtn').click();

    // User message should appear
    const userMsg = page.locator('.msg.user');
    await expect(userMsg).toBeVisible();

    // Thinking indicator or AI response should follow
    await page.waitForTimeout(3000);

    // After sending, input should be cleared
    const inputValue = await page.locator('#msgInput').inputValue();
    expect(inputValue).toBe('');
  });

  test('AI response includes code block or error message', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    // Send a prompt
    await page.locator('#msgInput').fill('写一个双均线策略');
    await page.locator('#sendBtn').click();

    // Wait for response (AI call takes time)
    await page.waitForTimeout(15000);

    // Either code block appears OR error message
    const codeBlock = page.locator('.code-block');
    const errorMsg = page.locator('.msg-text').filter({ hasText: /失败|错误|error/i });

    const hasCode = await codeBlock.count() > 0;
    const hasError = await errorMsg.count() > 0;

    // At least one of these should be present after waiting
    expect(hasCode || hasError).toBe(true);
  });

  test('hint text below input is displayed', async ({ page }) => {
    await page.goto(`${BASE}/copilot.html`);
    await page.waitForLoadState('domcontentloaded');

    const hint = page.locator('.input-hint');
    await expect(hint).toBeVisible();
    const text = await hint.textContent();
    expect(text).toContain('Enter');
  });
});
