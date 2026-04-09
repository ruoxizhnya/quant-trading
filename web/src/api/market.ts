import api from './client'
import type { MarketData, StockCount, HealthStatus } from '@/types/api'

export function getMarketIndex(): Promise<MarketData> {
  return api.get<MarketData>('/market/index')
}

export function getStockCount(): Promise<StockCount> {
  return api.get<StockCount>('/stocks/count')
}

export function getHealth(): Promise<HealthStatus> {
  return api.get<HealthStatus>('/health')
}
