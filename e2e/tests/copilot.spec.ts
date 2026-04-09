import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('Copilot Page (Vue SPA)', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('page loads with chat interface', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('.copilot-page')).toBeVisible();
    await expect(page.locator('.n-card').first()).toContainText('策略 Copilot');
  });

  test('chat container and input area render', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    await expect(page.locator('.messages')).toBeVisible();
    await expect(page.locator('.input-area')).toBeVisible();

    const sendBtn = page.locator('.n-button--primary-type');
    await expect(sendBtn).toBeVisible();
    await expect(sendBtn).toContainText(/发送/);
  });

  test('welcome message displays with suggestions', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    const emptyChat = page.locator('.empty-chat');
    await expect(emptyChat).toBeVisible();

    await expect(emptyChat).toContainText('策略');
    const listItems = emptyChat.locator('li');
    const count = await listItems.count();
    expect(count).toBeGreaterThanOrEqual(2);
  });

  test('send button is disabled when input is empty', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    const sendBtn = page.locator('.n-button--primary-type');
    const isDisabled = await sendBtn.isDisabled();
    expect(isDisabled).toBe(true);
  });

  test('input placeholder text is present', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    const textarea = page.locator('.n-input textarea, .n-input__input-el');
    await expect(textarea.first()).toHaveAttribute('placeholder', /描述.*策略/);
  });

  test('typing enables send button', async ({ page }) => {
    await page.goto('/copilot');
    await page.waitForLoadState('domcontentloaded');

    const textarea = page.locator('.n-input textarea, .n-input__input-el');
    await textarea.fill('测试策略');

    const sendBtn = page.locator('.n-button--primary-type');
    const isDisabled = await sendBtn.isDisabled();
    expect(isDisabled).toBe(false);
  });

  test('navigation from dashboard works', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');

    await page.locator('.nav-tile').filter({ hasText: '策略 Copilot' }).click();
    await page.waitForLoadState('domcontentloaded');

    await expect(page).toHaveURL(/\/copilot/);
    await expect(page.locator('.copilot-page')).toBeVisible();
  });
});
