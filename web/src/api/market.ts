import api from './client'
import type { MarketData, ScreenResult, StockCount, HealthStatus } from '@/types/api'

export function getMarketIndex(symbol?: string, date?: string): Promise<MarketData> {
  const params = new URLSearchParams()
  if (symbol) params.set('symbol', symbol)
  if (date) params.set('date', date)
  const qs = params.toString()
  return api.get<MarketData>(`/api/market/index${qs ? '?' + qs : ''}`)
}

export function getStockCount(): Promise<StockCount> {
  return api.get<StockCount>('/api/stocks/count')
}

export function getHealth(): Promise<HealthStatus> {
  return api.get<HealthStatus>('/health')
}

export function screenStocks(params: Record<string, number>): Promise<ScreenResult> {
  return api.post<ScreenResult>('/api/screen', params)
}
