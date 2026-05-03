import api from './client'
import type { CopilotRequest, CopilotResponse } from '@/types/api'

export function generateStrategy(req: CopilotRequest): Promise<CopilotResponse> {
  return api.post<CopilotResponse>('/api/copilot/generate', req, { timeout: 120000 })
}

export function saveStrategy(data: Record<string, unknown>) {
  return api.post('/api/copilot/save', data)
}

export function getCopilotStats() {
  return api.get('/api/copilot/stats')
}
