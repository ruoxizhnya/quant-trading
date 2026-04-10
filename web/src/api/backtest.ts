import api from './client'
import type { BacktestRequest, BacktestResult, BacktestJob } from '@/types/api'

export interface CreateJobRequest {
  strategy_id: string
  universe: string
  start_date: string
  end_date: string
  initial_capital?: number
  commission_rate?: number
  slippage_rate?: number
}

export interface JobResponse {
  job_id: string
  status: string
}

export function runBacktest(req: BacktestRequest): Promise<BacktestResult> {
  return api.post<BacktestResult>('/backtest', req, { timeout: 300000 })
}

export function createBacktestJob(req: CreateJobRequest): Promise<JobResponse> {
  return api.post<JobResponse>('/backtest', req)
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
