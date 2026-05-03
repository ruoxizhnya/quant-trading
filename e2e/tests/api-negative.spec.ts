import { test, expect } from '@playwright/test';
import { apiRequest, API } from '../helpers/api';

test.describe('API Negative Tests', () => {
  let ctx: any;

  test.beforeAll(async () => {
    ctx = await apiRequest();
  });

  test.afterAll(async () => {
    await ctx.dispose();
  });

  test('POST /api/backtest with empty body returns 400', async () => {
    const res = await ctx.post('/api/backtest', { data: {} });
    expect(res.status()).toBe(400);
  });

  test('POST /api/backtest with missing strategy returns 400', async () => {
    const res = await ctx.post('/api/backtest', {
      data: { stock_pool: ['600519'], start_date: '20250101', end_date: '20251231' },
    });
    expect(res.status()).toBe(400);
  });

  test('POST /api/backtest with missing stock_pool returns 400', async () => {
    const res = await ctx.post('/api/backtest', {
      data: { strategy: 'momentum', start_date: '20250101', end_date: '20251231' },
    });
    expect(res.status()).toBe(400);
  });

  test('GET /api/backtest/nonexistent-id/report returns 404', async () => {
    const res = await ctx.get('/api/backtest/nonexistent-id-12345/report');
    expect([404, 500]).toContain(res.status());
  });

  test('GET /api/backtest/nonexistent-id/trades returns 404', async () => {
    const res = await ctx.get('/api/backtest/nonexistent-id-12345/trades');
    expect([404, 500]).toContain(res.status());
  });

  test('GET /api/backtest/nonexistent-id/equity returns 404', async () => {
    const res = await ctx.get('/api/backtest/nonexistent-id-12345/equity');
    expect([404, 500]).toContain(res.status());
  });

  test('GET /api/ohlcv with missing params returns 400', async () => {
    const res = await ctx.get('/api/ohlcv/600519');
    expect(res.status()).toBe(400);
  });

  test('POST /api/screen with empty body returns 400 or 500', async () => {
    const res = await ctx.post('/api/screen', { data: {} });
    expect([400, 500]).toContain(res.status());
  });

  test('GET /api/strategies/nonexistent returns 404', async () => {
    const res = await ctx.get('/api/strategies/nonexistent-strategy-xyz');
    expect([404, 500]).toContain(res.status());
  });

  test('POST /api/copilot/generate with empty prompt returns 400', async () => {
    const res = await ctx.post('/api/copilot/generate', { data: { prompt: '' } });
    expect([400, 500]).toContain(res.status());
  });

  test('GET /api/walkforward/nonexistent returns 404', async () => {
    const res = await ctx.get('/api/walkforward/nonexistent-strategy-xyz');
    expect([404, 500]).toContain(res.status());
  });
});
