import api from './client'
import type { BacktestRequest, BacktestResult, BacktestJob } from '@/types/api'

export function runBacktest(req: BacktestRequest): Promise<BacktestResult> {
  return api.post<BacktestResult>('/backtest', req, { timeout: 300000 })
}

export function getBacktestReport(id: string): Promise<BacktestResult> {
  return api.get<BacktestResult>(`/backtest/${id}/report`)
}

export function listBacktestJobs(limit = 20): Promise<{ jobs: BacktestJob[]; total: number }> {
  return api.get<{ jobs: BacktestJob[]; total: number }>(`/backtest?limit=${limit}`)
}

export function getBacktestJob(id: string): Promise<BacktestJob> {
  return api.get<BacktestJob>(`/backtest/${id}`)
}

export function getOHLCV(symbol: string, start: string, end: string) {
  return api.get(`/ohlcv/${symbol}?start_date=${start}&end_date=${end}`)
}
