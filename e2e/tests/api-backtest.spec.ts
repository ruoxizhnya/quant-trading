import { test, expect } from '@playwright/test';
import { apiRequest, API, waitForBackendReady } from '../helpers/api';

const QUICK_BACKTEST = {
  strategy: 'momentum',
  stock_pool: ['600000.SH'],
  start_date: '2024-01-02',
  end_date: '2024-01-31',
  initial_capital: 1000000,
  commission_rate: 0.0003,
  slippage_rate: 0.0001,
};

async function runQuickBacktest(ctx: any) {
  const res = await API.runBacktest(ctx, QUICK_BACKTEST);
  expect(res.status()).toBe(200);
  const body = await res.json();
  return body;
}

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
    expect(body).not.toBeNull();
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
    expect(body).not.toBeNull();
    await ctx.dispose();
  });
});

test.describe('Backend API — Backtest Result Persistence', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('POST /backtest persists result to DB — report retrievable via GET /backtest/:id/report', async () => {
    const ctx = await apiRequest();

    const result = await runQuickBacktest(ctx);
    expect(result.id).toBeTruthy();
    expect(result.status).toBe('completed');
    expect(result.strategy).toBe('momentum');

    const reportRes = await API.getBacktestReport(ctx, result.id);
    expect(reportRes.status()).toBe(200);
    const report = await reportRes.json();
    expect(report.total_return).toBeDefined();
    expect(report.sharpe_ratio).toBeDefined();
    expect(Array.isArray(report.portfolio_values)).toBeTruthy();
    expect(report.portfolio_values.length).toBeGreaterThan(0);
    expect(Array.isArray(report.trades)).toBeTruthy();

    await ctx.dispose();
  });

  test('GET /backtest?limit=5 lists recent jobs including completed ones', async () => {
    const ctx = await apiRequest();

    await runQuickBacktest(ctx);

    const listRes = await API.listBacktestJobs(ctx, 5);
    expect(listRes.status()).toBe(200);
    const listBody = await listRes.json();
    expect(listBody.jobs).toBeDefined();
    expect(Array.isArray(listBody.jobs)).toBeTruthy();
    expect(listBody.jobs.length).toBeGreaterThanOrEqual(1);

    const completedJob = listBody.jobs.find((j: any) => j.status === 'completed');
    expect(completedJob).toBeDefined();
    expect(completedJob.id).toBeTruthy();
    expect(completedJob.strategy_id).toBeTruthy();

    await ctx.dispose();
  });

  test('GET /backtest/:id/trades returns trades array from DB', async () => {
    const ctx = await apiRequest();

    const result = await runQuickBacktest(ctx);

    const tradesRes = await API.getBacktestTrades(ctx, result.id);
    expect(tradesRes.status()).toBe(200);
    const tradesBody = await tradesRes.json();
    expect(tradesBody.total).toBeGreaterThanOrEqual(0);
    expect(Array.isArray(tradesBody.trades)).toBeTruthy();

    if (tradesBody.trades.length > 0) {
      const trade = tradesBody.trades[0];
      expect(trade.symbol).toBeTruthy();
      expect(trade.direction).toBeTruthy();
      expect(trade.quantity).toBeDefined();
    }

    await ctx.dispose();
  });

  test('GET /backtest/:id/equity returns portfolio values from DB', async () => {
    const ctx = await apiRequest();

    const result = await runQuickBacktest(ctx);

    const equityRes = await API.getBacktestEquity(ctx, result.id);
    expect(equityRes.status()).toBe(200);
    const equityBody = await equityRes.json();
    expect(equityBody.total_points).toBeGreaterThan(0);
    expect(Array.isArray(equityBody.equity_curve)).toBeTruthy();

    if (equityBody.equity_curve.length > 0) {
      const point = equityBody.equity_curve[0];
      expect(point.date).toBeTruthy();
      expect(point.total_value).toBeDefined();
    }

    await ctx.dispose();
  });

  test('persisted report contains all required metrics fields', async () => {
    const ctx = await apiRequest();

    const result = await runQuickBacktest(ctx);
    const reportRes = await API.getBacktestReport(ctx, result.id);
    const report = await reportRes.json();

    expect(typeof report.total_return).toBe('number');
    expect(typeof report.annual_return).toBe('number');
    expect(typeof report.sharpe_ratio).toBe('number');
    expect(typeof report.max_drawdown).toBe('number');
    expect(typeof report.calmar_ratio).toBe('number');
    expect(typeof report.win_rate).toBe('number');
    expect(typeof report.total_trades).toBe('number');

    if (report.stock_pool !== undefined) {
      expect(report.stock_pool).toContain('600000.SH');
    }
    if (report.initial_capital !== undefined) {
      expect(report.initial_capital).toBe(QUICK_BACKTEST.initial_capital);
    }

    await ctx.dispose();
  });
});

test.describe('Backend API — Backtest Result Comparison', () => {

  test.beforeAll(async () => {
    const ready = await waitForBackendReady(60000);
    expect(ready).toBe(true);
  });

  test('same parameters produce consistent results (idempotency)', async () => {
    const ctx = await apiRequest();

    const payload = {
      strategy: 'momentum',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-02',
      end_date: '2024-03-31',
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.001,
    };

    const res1 = await API.runBacktest(ctx, payload);
    expect(res1.status()).toBe(200);
    const body1 = await res1.json();

    const res2 = await API.runBacktest(ctx, payload);
    expect(res2.status()).toBe(200);
    const body2 = await res2.json();

    expect(body1.total_return).toBeDefined();
    expect(body2.total_return).toBeDefined();
    expect(body1.total_return).toEqual(body2.total_return);
    expect(body1.sharpe_ratio).toEqual(body2.sharpe_ratio);
    expect(body1.total_trades).toEqual(body2.total_trades);
    expect(body1.max_drawdown).toEqual(body2.max_drawdown);

    await ctx.dispose();
  });

  test('different strategies produce different results', async () => {
    const ctx = await apiRequest();

    const basePayload = (strategy: string) => ({
      strategy,
      stock_pool: ['600000.SH'],
      start_date: '2024-01-02',
      end_date: '2024-06-30',
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.001,
    });

    const r1 = await API.runBacktest(ctx, basePayload('momentum'));
    expect(r1.status()).toBe(200);
    const b1 = await r1.json();

    const r2 = await API.runBacktest(ctx, basePayload('value'));
    const b2Status = r2.status();
    if ([200, 201, 202].includes(b2Status)) {
      const b2 = await r2.json();
      const returnsDiffer = Math.abs((b1.total_return || 0) - (b2.total_return || 0)) > 0.001
        || Math.abs((b1.sharpe_ratio || 0) - (b2.sharpe_ratio || 0)) > 0.001;
      expect(returnsDiffer || b1.total_trades !== b2.total_trades).toBeTruthy();
    }

    await ctx.dispose();
  });

  test('different date ranges produce different trade counts', async () => {
    const ctx = await apiRequest();

    const shortRange = {
      strategy: 'momentum',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-02',
      end_date: '2024-02-29',
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.001,
    };

    const longRange = {
      ...shortRange,
      end_date: '2024-06-30',
    };

    const rShort = await API.runBacktest(ctx, shortRange);
    expect(rShort.status()).toBe(200);
    const bShort = await rShort.json();

    const rLong = await API.runBacktest(ctx, longRange);
    expect(rLong.status()).toBe(200);
    const bLong = await rLong.json();

    expect(bLong.total_trades).toBeGreaterThanOrEqual(bShort.total_trades);

    await ctx.dispose();
  });

  test('higher commission rate reduces net return', async () => {
    const ctx = await apiRequest();

    const lowCommission = {
      strategy: 'momentum',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-02',
      end_date: '2024-06-30',
      initial_capital: 1000000,
      commission_rate: 0.0001,
      slippage_rate: 0.0001,
    };

    const highCommission = {
      ...lowCommission,
      commission_rate: 0.005,
    };

    const rLow = await API.runBacktest(ctx, lowCommission);
    expect(rLow.status()).toBe(200);
    const bLow = await rLow.json();

    const rHigh = await API.runBacktest(ctx, highCommission);
    expect(rHigh.status()).toBe(200);
    const bHigh = await rHigh.json();

    if ((bLow.total_trades || 0) > 5) {
      expect(bLow.total_return).toBeGreaterThanOrEqual(bHigh.total_return);
    }

    await ctx.dispose();
  });

  test('larger initial capital produces proportionally larger absolute PnL', async () => {
    const ctx = await apiRequest();

    const smallCapital = {
      strategy: 'momentum',
      stock_pool: ['600000.SH'],
      start_date: '2024-01-02',
      end_date: '2024-06-30',
      initial_capital: 500000,
      commission_rate: 0.0003,
      slippage_rate: 0.001,
    };

    const largeCapital = {
      ...smallCapital,
      initial_capital: 2000000,
    };

    const rSmall = await API.runBacktest(ctx, smallCapital);
    expect(rSmall.status()).toBe(200);
    const bSmall = await rSmall.json();

    const rLarge = await API.runBacktest(ctx, largeCapital);
    expect(rLarge.status()).toBe(200);
    const bLarge = await rLarge.json();

    expect(bSmall.total_return).toEqual(bLarge.total_return);
    expect(bSmall.sharpe_ratio).toEqual(bLarge.sharpe_ratio);

    await ctx.dispose();
  });
});
