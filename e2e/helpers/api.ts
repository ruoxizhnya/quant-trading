import { request, APIRequestContext, expect } from '@playwright/test';

const BASE = process.env.BASE_URL || 'http://localhost:8085';
const DATA = process.env.DATA_SERVICE_URL || 'http://localhost:8081';

export async function apiRequest(): Promise<APIRequestContext> {
  return await request.newContext({
    baseURL: BASE,
    extraHTTPHeaders: { 'Content-Type': 'application/json' },
  });
}

export async function dataApiRequest(): Promise<APIRequestContext> {
  return await request.newContext({
    baseURL: DATA,
    extraHTTPHeaders: { 'Content-Type': 'application/json' },
  });
}

export const API = {
  async health(ctx: APIRequestContext) {
    return ctx.get('/health');
  },

  async stocksCount(ctx: APIRequestContext) {
    return ctx.get('/stocks/count');
  },

  async marketIndex(ctx: APIRequestContext) {
    return ctx.get('/market/index');
  },

  async strategies(ctx: APIRequestContext) {
    return ctx.get('/api/strategies');
  },

  async runBacktest(ctx: APIRequestContext, payload: any) {
    return ctx.post('/backtest', { data: payload });
  },

  async getBacktestReport(ctx: APIRequestContext, id: string) {
    return ctx.get(`/backtest/${id}/report`);
  },

  async copilotGenerate(ctx: APIRequestContext, prompt: string) {
    return ctx.post('/api/copilot/generate', { data: { prompt } });
  },

  async screen(ctx: APIRequestContext, filters: any) {
    return ctx.post('/screen', { data: { filters } });
  },

  async ohlcv(ctx: APIRequestContext, symbol: string, start: string, end: string) {
    return ctx.get(`/ohlcv/${symbol}?start_date=${start}&end_date=${end}`);
  },
};

export async function waitForAPIReady(timeoutMs = 60000): Promise<boolean> {
  const ctx = await apiRequest();
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await ctx.get('/health', { timeout: 5000 });
      if (res.ok()) {
        const body = await res.json();
        if (body.status === 'healthy') {
          await ctx.dispose();
          return true;
        }
      }
    } catch {}
    await new Promise(r => setTimeout(r, 2000));
  }
  await ctx.dispose();
  return false;
}
