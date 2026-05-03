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
  return api.post<BacktestResult>('/api/backtest', req, { timeout: 300000 })
}

export function createBacktestJob(req: CreateJobRequest): Promise<JobResponse> {
  return api.post<JobResponse>('/api/backtest', req)
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
