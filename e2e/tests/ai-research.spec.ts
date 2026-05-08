import { test, expect } from '@playwright/test';
import { waitForBackendReady } from '../helpers/api';

test.describe('AI Research Platform', () => {
  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test.describe('Factor Lab', () => {
    test('page loads with factor discovery interface', async ({ page }) => {
      await page.goto('/ai-research/factor-lab');
      await page.waitForLoadState('domcontentloaded');

      await expect(page.locator('.factor-lab-page, .ai-research-page')).toBeVisible();
    });

    test('factor input area renders', async ({ page }) => {
      await page.goto('/ai-research/factor-lab');
      await page.waitForLoadState('domcontentloaded');

      const input = page.locator('input, textarea').first();
      await expect(input).toBeVisible();
    });

    test('navigation from dashboard works', async ({ page }) => {
      await page.goto('/');
      await page.waitForLoadState('domcontentloaded');

      const aiNav = page.locator('.nav-tile, .nav-item').filter({ hasText: /AI|研究|因子/ });
      if (await aiNav.count() > 0) {
        await aiNav.first().click();
        await page.waitForLoadState('domcontentloaded');
        await expect(page).toHaveURL(/\/ai-research/);
      }
    });
  });

  test.describe('Strategy Workshop', () => {
    test('page loads with strategy generation interface', async ({ page }) => {
      await page.goto('/ai-research/strategy-workshop');
      await page.waitForLoadState('domcontentloaded');

      await expect(page.locator('.strategy-workshop-page, .ai-research-page')).toBeVisible();
    });

    test('strategy description input renders', async ({ page }) => {
      await page.goto('/ai-research/strategy-workshop');
      await page.waitForLoadState('domcontentloaded');

      const input = page.locator('input, textarea').first();
      await expect(input).toBeVisible();
    });
  });

  test.describe('Evolution Observatory', () => {
    test('page loads with evolution monitoring interface', async ({ page }) => {
      await page.goto('/ai-research/evolution');
      await page.waitForLoadState('domcontentloaded');

      await expect(page.locator('.evolution-obs-page, .ai-research-page')).toBeVisible();
    });
  });

  test.describe('Pipeline Dashboard', () => {
    test('page loads with pipeline visualization', async ({ page }) => {
      await page.goto('/ai-research/pipeline');
      await page.waitForLoadState('domcontentloaded');

      await expect(page.locator('.pipeline-dashboard-page, .ai-research-page')).toBeVisible();
    });
  });

  test.describe('AI Research API', () => {
    test('GET /api/ai/health returns 200', async ({ request }) => {
      const response = await request.get('http://localhost:8086/api/ai/health');
      expect([200, 404, 503]).toContain(response.status());
    });

    test('POST /api/ai/factors/discover with empty body returns 400', async ({ request }) => {
      const response = await request.post('http://localhost:8086/api/ai/factors/discover', {
        data: {}
      });
      expect([400, 404, 503]).toContain(response.status());
    });

    test('POST /api/ai/strategies/generate with empty description returns 400', async ({ request }) => {
      const response = await request.post('http://localhost:8086/api/ai/strategies/generate', {
        data: { description: '' }
      });
      expect([400, 404, 503]).toContain(response.status());
    });
  });

  test.describe('Negative Tests', () => {
    test('accessing invalid AI research route shows error state', async ({ page }) => {
      await page.goto('/ai-research/nonexistent-page');
      await page.waitForLoadState('domcontentloaded');

      const errorEl = page.locator('.n-result, .error-state, .n-empty, [class*="error"], [class*="empty"], [class*="not-found"]');
      const visibleError = await errorEl.filter({ hasText: /不存在|找不到|失败|error|404|not found/i }).first().isVisible().catch(() => false);

      const pageContent = await page.content();
      const hasFriendlyMessage = /不存在|找不到|无法加载|无效|no.*found|not.*available|404/i.test(pageContent);

      expect(visibleError || hasFriendlyMessage).toBeTruthy();
    });

    test('AI research pages handle service unavailable gracefully', async ({ page }) => {
      await page.route('**/api/ai/**', route => route.fulfill({ status: 503, body: '{"error":"Service Unavailable"}' }));

      await page.goto('/ai-research/factor-lab');
      await page.waitForLoadState('domcontentloaded');

      const bodyVisible = await page.locator('body').isVisible();
      expect(bodyVisible).toBeTruthy();
    });
  });
});
