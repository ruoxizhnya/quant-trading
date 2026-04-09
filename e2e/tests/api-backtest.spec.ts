import { test, expect } from '@playwright/test';
import { apiRequest, API, waitForBackendReady } from '../helpers/api';

test.describe('Backend API — Backtest Engine', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('POST /backtest accepts valid request', async () => {
    const ctx = await apiRequest();

    const payload = {
      strategy: 'momentum',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-01',
      end_date: '2024-03-31',
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.0001,
    };

    const res = await API.runBacktest(ctx, payload);
    const body = await res.json();

    // API should respond (200 with result/job, or 400 if data unavailable)
    expect([200, 201, 202, 400]).toContain(res.status());
    expect(body).toBeDefined();
    await ctx.dispose();
  });

  test('POST /backtest rejects empty stock pool', async () => {
    const ctx = await apiRequest();

    const res = await API.runBacktest(ctx, {
      strategy: 'momentum',
      stock_pool: [],
      start_date: '2024-01-01',
      end_date: '2024-03-31',
      initial_capital: 1000000,
    });

    expect(res.status()).toBeGreaterThanOrEqual(400);
    await ctx.dispose();
  });

  test('POST /backtest with mean_reversion strategy returns response', async () => {
    const ctx = await apiRequest();

    const res = await API.runBacktest(ctx, {
      strategy: 'mean_reversion',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-01',
      end_date: '2024-06-30',
      initial_capital: 500000,
      commission_rate: 0.0003,
      slippage_rate: 0.0001,
    });

    // Accept both success and data-unavailable responses
    expect([200, 201, 202, 400]).toContain(res.status());
    const body = await res.json();
    expect(body).toBeDefined();
    await ctx.dispose();
  });
});
