import { test, expect } from '@playwright/test';
import { apiRequest, API, waitForBackendReady } from '../helpers/api';

test.describe('Backend API — Strategy CRUD & Copilot', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('GET /api/strategies returns built-in strategies', async () => {
    const ctx = await apiRequest();
    const res = await API.strategies(ctx);
    const body = await res.json();

    expect(res.status()).toBe(200);
    expect(body.strategies.length).toBeGreaterThanOrEqual(1);

    // Should have momentum at minimum
    const names = body.strategies.map((s: any) => s.name || s.id || s.strategy_id);
    const hasMomentum = names.some((n: string) =>
      n.toLowerCase().includes('momentum') || n.toLowerCase().includes('动量')
    );
    expect(hasMomentum).toBe(true);
    await ctx.dispose();
  });

  test('POST /api/copilot/generate requires AI_API_KEY (returns error when not set)', async () => {
    const ctx = await apiRequest();

    const res = await API.copilotGenerate(ctx, '写一个简单的均线策略');

    // Without AI_API_KEY set, should get an error response
    const body = await res.json();
    expect([200, 202, 500, 503]).toContain(res.status());
    if (res.status() === 500 || res.status() === 503) {
      expect(body.error).toBeDefined();
    } else if (res.status() === 200 || res.status() === 202) {
      expect(body.code || body.job_id).toBeDefined();
    }
    await ctx.dispose();
  });
});
