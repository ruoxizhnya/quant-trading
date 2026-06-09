import api from './client'
import type { BacktestRequest, BacktestResult, BacktestJob } from '@/types/api'

/**
 * CR-04 (ODR-012): The Go backend `POST /api/backtest` (handlers_backtest.go)
 * dispatches by sniffing the request body — it tries
 *   1) `backtest.CreateJobRequest` (strategy_id + universe)  → async job
 *   2) `backtest.BacktestRequest`  (strategy + stock_pool)   → sync run
 * To make the client-side contract explicit, both flavors below carry a
 * `mode` discriminator. The backend currently ignores it, but the field
 * documents intent and lets us split into `/api/backtest` and
 * `/api/backtest/jobs` in a future PR without breaking clients.
 */
export type BacktestMode = 'sync' | 'async'

export interface CreateJobRequest {
  mode?: BacktestMode
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
  return api.post<BacktestResult>('/api/backtest', { ...req, mode: 'sync' }, { timeout: 300000 })
}

export function createBacktestJob(req: CreateJobRequest): Promise<JobResponse> {
  return api.post<JobResponse>('/api/backtest', { ...req, mode: 'async' })
}

export function getBacktestReport(id: string): Promise<BacktestResult> {
  return api.get<BacktestResult>(`/api/backtest/${id}/report`)
}

export function listBacktestJobs(limit = 20): Promise<{ jobs: BacktestJob[]; total: number }> {
  return api.get<{ jobs: BacktestJob[]; total: number }>(`/api/backtest?limit=${limit}`)
}

export function getBacktestJob(id: string): Promise<BacktestJob> {
  return api.get<BacktestJob>(`/api/backtest/${id}`)
}

export function getOHLCV(symbol: string, start: string, end: string): Promise<OHLCVAPIResponse> {
  return api.get(`/api/ohlcv/${symbol}?start_date=${start}&end_date=${end}`)
}

export interface OHLCVAPIResponse {
  ohlcv?: OHLCVDataPoint[] | null
  data?: OHLCVDataPoint[] | null
}

export interface OHLCVDataPoint {
  trade_date?: string
  date?: string
  close?: number | string
  open?: number | string
  high?: number | string
  low?: number | string
  volume?: number
}
