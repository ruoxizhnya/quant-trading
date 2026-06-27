import api from './client'
import type { Strategy } from '@/types/api'

export function getStrategies(): Promise<{ strategies: Strategy[] }> {
  return api.get('/api/strategies')
}

// StrategyBuilder (P3): persist a visually-assembled multi-factor strategy.
// The factors array mirrors what the canvas produces — each entry pins a
// factor definition to a weight (0-100) and a direction. stock_count and
// rebalance_freq drive the selection + rebalance logic on the backend.
export interface StrategyFactorEntry {
  id: string
  weight: number
  direction: 'long' | 'short'
}

export interface SaveStrategyRequest {
  name: string
  factors: StrategyFactorEntry[]
  stock_count: number
  rebalance_freq: 'daily' | 'weekly' | 'monthly'
}

export interface SaveStrategyResponse {
  id: string
  name: string
}

export function saveStrategy(req: SaveStrategyRequest): Promise<SaveStrategyResponse> {
  return api.post<SaveStrategyResponse>('/api/strategies', req)
}
