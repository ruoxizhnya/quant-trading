import api from './client'
import type { Strategy } from '@/types/api'

export function getStrategies(): Promise<{ strategies: Strategy[] }> {
  return api.get('/api/strategies')
}
