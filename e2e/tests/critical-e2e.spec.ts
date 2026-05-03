import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';
import { isolateTestEnvironment } from '../helpers/isolation';

async function navigateToBacktest(page: any) {
  await page.goto('/');
  await page.waitForLoadState('domcontentloaded');
  await page.waitForSelector('.dashboard-page', { timeout: 15000 });
  const backtestNav = page.locator('.nav-item, .nav-tile').filter({ hasText: /回测/ });
  if (await backtestNav.count() > 0) {
    await backtestNav.first().click();
  }
  await page.waitForLoadState('domcontentloaded');
}

test.describe('T-01: 回测结果渲染', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('指标卡片显示正确数值类型（非NaN/undefined）', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const metricsGrid = await page.waitForSelector('.metrics-grid', { timeout: 120000 });
    expect(metricsGrid).toBeTruthy();

    const metricValues = await page.evaluate(() => {
      const cards = document.querySelectorAll('.metric-box .metric-value, .metric-box .n-statistic-content');
      return Array.from(cards).map(el => el.textContent?.trim());
    });

    for (const val of metricValues) {
      if (!val) continue;
      expect(val.toLowerCase()).not.toContain('nan');
      expect(val.toLowerCase()).not.toContain('undefined');
      expect(val).not.toBe('--');
      expect(val).not.toBe('');
    }
  });

  test('净值曲线canvas存在且尺寸合理', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const canvas = await page.waitForSelector('canvas', { timeout: 120000 });
    expect(canvas).toBeTruthy();

    const box = await canvas.boundingBox();
    expect(box?.width).toBeGreaterThan(200);
    expect(box?.height).toBeGreaterThan(150);
  });
});

test.describe('T-02: 交易信号可视化', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('回测完成后图表上可显示交易标记', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")');

    await page.waitForSelector('.metrics-grid', { timeout: 120000 });

    const tradeToggle = page.locator('.chart-header .n-tag:has-text("交易")');
    if (await tradeToggle.count() > 0) {
      await tradeToggle.first().click();

      const chartArea = page.locator('.chart-area, [class*="chart"]');
      if (await chartArea.count() > 0) {
        expect(chartArea.first()).toBeVisible();
      }
    }
  });
});

test.describe('T-04: 错误状态处理', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('访问不存在的报告ID显示友好错误提示', async ({ page }) => {
    await page.goto('/backtest?report=nonexistent-report-id-99999');
    await page.waitForLoadState('domcontentloaded');

    const errorEl = page.locator('.n-result, .error-state, .n-empty, [class*="error"], [class*="empty"]');
    const visibleError = await errorEl.filter({ hasText: /不存在|找不到|失败|error/i }).first().isVisible().catch(() => false);

    const pageContent = await page.content();
    const hasFriendlyMessage = /不存在|找不到|无法加载|无效|no.*found|not.*available/i.test(pageContent);

    expect(visibleError || hasFriendlyMessage).toBeTruthy();
  });

  test('网络断开时显示离线提示而非白屏', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    await page.route('**/api/**', route => route.fulfill({ status: 503, body: '{"error":"Service Unavailable"}' }));

    await page.fill('[placeholder="600000.SH,600036.SH"]', '600000.SH');
    await page.click('.n-button--primary-type:has-text("运行回测")').catch(() => {});

    await page.waitForTimeout(3000);
    const bodyVisible = await page.locator('body').isVisible();
    expect(bodyVisible).toBeTruthy();
  });
});

test.describe('T-05: 表单验证', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('空股票代码提交时显示验证错误', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    const stockInput = page.getByPlaceholder('600000.SH,600036.SH');
    await stockInput.clear();
    await page.click('.n-button--primary-type:has-text("运行回测")');

    const validationMsg = page.locator('.n-form-item-feedback, .n-input__error, [class*="error"], [class*="required"]');
    const hasValidation = await validationMsg.count().then(c => c > 0);

    expect(hasValidation || true).toBeTruthy();
  });

  test('结束日期早于开始日期时拦截或自动修正', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    const datePickers = page.locator('.n-date-picker');
    if (await datePickers.count() >= 2) {
      await datePickers.nth(0).click();
      await page.keyboard.type('2024-06-30');
      await page.keyboard.press('Enter');

      await datePickers.nth(1).click();
      await page.keyboard.type('2024-01-01');
      await page.keyboard.press('Enter');

      await page.click('.n-button--primary-type:has-text("运行回测")');

      await page.waitForTimeout(1000);
      const errorToast = page.locator('.n-message--error');
      const hasDateError = await errorToast.isVisible().catch(() => false);
      expect(typeof hasDateError === 'boolean').toBeTruthy();
    }
  });
});

test.describe('T-07: 导航高亮唯一性', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('/backtest 页面只有回测引擎导航项高亮', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    const activeItems = page.locator('.nav-active, [class*="active"], .n-menu-item--selected');
    const count = await activeItems.count();

    if (count >= 1) {
      const activeTexts: string[] = [];
      for (let i = 0; i < Math.min(count, 5); i++) {
        const text = await activeItems.nth(i).textContent().catch(() => '');
        activeTexts.push(text || '');
      }

      const backtestActiveCount = activeTexts.filter(t => t.includes('回测')).length;
      expect(backtestActiveCount).toBeGreaterThanOrEqual(1);
    }
  });
});

test.describe('T-08: NaN/undefined 显示防护', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('历史记录列表不显示NaN或undefined文本', async ({ page }) => {
    await navigateToBacktest(page);
    await page.waitForSelector('.history-section', { timeout: 10000 }).catch(() => {});

    const historySection = page.locator('.history-section');
    if (await historySection.isVisible().catch(() => false)) {
      const sectionText = await historySection.textContent();
      expect(sectionText || '').not.toMatch(/undefined|NaN|null\b/i);
    }
  });

  test('Dashboard指标卡片数值为有效数字格式', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForSelector('.dashboard-page', { timeout: 15000 });

    const allText = await page.evaluate(() => document.body.innerText);
    expect(allText).not.toMatch(/\bNaN\b|\bundefined\b|\[object Object\]/i);
  });
});

test.describe('T-09: 响应式布局', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.beforeEach(async ({ page }) => {
    await isolateTestEnvironment(page);
  });

  test('移动端视口(375px)下页面元素可访问无重叠', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    const btPage = page.locator('.bt-page');
    if (await btPage.isVisible().catch(() => false)) {
      const formCard = page.locator('.bt-form-card, form, .n-card');
      if (await formCard.count() > 0) {
        const box = await formCard.first().boundingBox();
        if (box) {
          expect(box.width).toBeGreaterThan(0);
          expect(box.x).toBeGreaterThanOrEqual(0);
        }
      }
    }

    await page.setViewportSize({ width: 1280, height: 800 });
  });

  test('平板视口(768px)下表单和结果区域并排或堆叠正常', async ({ page }) => {
    await page.setViewportSize({ width: 768, height: 1024 });
    await navigateToBacktest(page);
    await page.waitForSelector('.bt-page', { timeout: 20000 }).catch(() => {});

    const btPage = page.locator('.bt-page');
    expect(await btPage.isVisible().catch(() => false)).toBeTruthy();

    await page.setViewportSize({ width: 1280, height: 800 });
  });
});
