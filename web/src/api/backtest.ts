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

/**
 * P2-1 (ODR-027): Download a self-contained HTML report for a completed
 * backtest. The backend (handlers_backtest.go) returns the file with a
 * `Content-Disposition: attachment; filename="backtest-<id>-<date>.html"`
 * header, which `client.ts:download()` parses for the caller.
 *
 * The browser is responsible for triggering the save dialog — we resolve
 * with a Blob + suggested filename so the UI layer can do the actual
 * `<a download>` dance.
 */
export interface ExportHtmlOptions {
  theme?: 'light' | 'dark'
  footer?: string
  includeEquity?: boolean
  includeTrades?: boolean
}

export async function exportHtml(
  id: string,
  opts: ExportHtmlOptions = {},
): Promise<{ blob: Blob; filename: string }> {
  const params = new URLSearchParams()
  if (opts.theme) params.set('theme', opts.theme)
  if (opts.footer) params.set('footer', opts.footer)
  // API defaults both to "include"; the query opt-out is `=0`.
  if (opts.includeEquity === false) params.set('equity', '0')
  if (opts.includeTrades === false) params.set('trades', '0')
  const qs = params.toString()
  const path = `/api/backtest/${id}/export/html${qs ? `?${qs}` : ''}`
  return api.download(path)
}

/**
 * P2-2 (ODR-027): Multi-strategy comparison. The backend endpoint is
 * registered in handlers_backtest.go:api.GET("/compare") and accepts
 * 2-8 backtest IDs. Resolves with the merged report (or a
 * partial-resolution payload with a `Missing` list when at least one
 * ID is bad). Throws on 4xx (min/max count violations).
 */
export interface CompareEntry {
  id: string
  strategy: string
  start_date?: string
  end_date?: string
  total_return: number
  annual_return: number
  sharpe_ratio: number
  sortino_ratio: number
  max_drawdown: number
  calmar_ratio: number
  win_rate: number
  total_trades: number
  win_trades: number
  lose_trades: number
  avg_holding_days: number
  initial_capital: number
  universe?: string
  has_equity_data: boolean
}

export interface CompareMissing {
  id: string
  reason: string
}

export interface CompareBest {
  total_return_id?: string
  sharpe_ratio_id?: string
  sortino_ratio_id?: string
  max_drawdown_id?: string
  calmar_ratio_id?: string
  win_rate_id?: string
  annual_return_id?: string
}

export interface CompareReport {
  generated_at: string
  requested: number
  resolved: number
  entries: CompareEntry[]
  missing: CompareMissing[]
  best: CompareBest
}

export function compareBacktests(ids: string[]): Promise<CompareReport> {
  if (ids.length < 2) {
    return Promise.reject(new Error('比较至少需要 2 个回测 ID'))
  }
  if (ids.length > 8) {
    return Promise.reject(new Error('比较最多支持 8 个回测 ID'))
  }
  return api.get<CompareReport>(`/api/backtest/compare?ids=${ids.join(',')}`)
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
