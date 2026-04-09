import api from './client'
import type { BacktestRequest, BacktestResult } from '@/types/api'

export function runBacktest(req: BacktestRequest): Promise<BacktestResult> {
  return api.post<BacktestResult>('/backtest', req, { timeout: 300000 })
}

export function getBacktestReport(id: string): Promise<BacktestResult> {
  return api.get<BacktestResult>(`/backtest/${id}/report`)
}

export function getOHLCV(symbol: string, start: string, end: string) {
  return api.get(`/ohlcv/${symbol}?start_date=${start}&end_date=${end}`)
}
