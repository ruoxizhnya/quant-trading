import api from './client'
import type { CopilotRequest, CopilotResponse, ScreenResult } from '@/types/api'

export function generateStrategy(req: CopilotRequest): Promise<CopilotResponse> {
  return api.post<CopilotResponse>('/api/copilot/generate', req, { timeout: 120000 })
}

export function saveStrategy(data: any) {
  return api.post('/api/copilot/save', data)
}

export function getCopilotStats() {
  return api.get('/api/copilot/stats')
}

export function doScreen(params: Record<string, number>): Promise<ScreenResult> {
  return api.post<ScreenResult>('/screen', params)
}
