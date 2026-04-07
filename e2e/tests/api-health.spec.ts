import { test, expect } from '@playwright/test';
import { apiRequest, API, waitForAPIReady } from '../helpers/api';

test.describe('Backend API — Health & Connectivity', () => {

  test.beforeAll(async () => {
    const ready = await waitForAPIReady(60000);
    expect(ready).toBe(true);
  });

  test('GET /health returns healthy status', async () => {
    const ctx = await apiRequest();
    const res = await API.health(ctx);
    const body = await res.json();

    expect(res.status()).toBe(200);
    expect(body.status).toBe('healthy');
    expect(body.service).toBe('analysis-service');
    expect(body.timestamp).toBeDefined();
    await ctx.dispose();
  });

  test('GET /stocks/count returns stock count', async () => {
    const ctx = await apiRequest();
    const res = await API.stocksCount(ctx);

    // data-service may be unavailable in some setups
    if (res.status() === 200) {
      const body = await res.json();
      expect(body.count).toBeDefined();
      expect(typeof body.count).toBe('number');
    } else {
      // 502 is acceptable if data-service isn't running
      expect([502, 503]).toContain(res.status());
    }
    await ctx.dispose();
  });

  test('GET /market/index returns index data', async () => {
    const ctx = await apiRequest();
    const res = await API.marketIndex(ctx);

    if (res.status() === 200) {
      const body = await res.json();
      expect(body.indices).toBeDefined();
      expect(Array.isArray(body.indices)).toBe(true);
    } else {
      expect([502, 503]).toContain(res.status());
    }
    await ctx.dispose();
  });

  test('GET /api/strategies returns strategy list', async () => {
    const ctx = await apiRequest();
    const res = await API.strategies(ctx);
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(body.strategies).toBeDefined();
    expect(Array.isArray(body.strategies)).toBe(true);
    await ctx.dispose();
  });

  test('GET /nonexistent returns 404 or fallback', async ({ request }) => {
    const res = await request.get('/api/nonexistent-endpoint');
    expect(res.status()).toBeGreaterThanOrEqual(400);
  });
});
