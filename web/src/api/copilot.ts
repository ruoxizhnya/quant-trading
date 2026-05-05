import api from './client'
import type { CopilotRequest, CopilotResponse } from '@/types/api'
import type { PipelineResult } from '@/types/pipeline'

export function generateStrategy(req: CopilotRequest): Promise<CopilotResponse> {
  return api.post<CopilotResponse>('/api/copilot/generate', req, { timeout: 120000 })
}

export function saveStrategy(data: Record<string, unknown>) {
  return api.post('/api/copilot/save', data)
}

export function getCopilotStats() {
  return api.get('/api/copilot/stats')
}

// Pipeline API
export function runPipeline(description: string): Promise<PipelineResult> {
  return api.post<PipelineResult>('/api/pipeline/run', { description }, { timeout: 300000 })
}

export function getPipelineJob(jobId: string): Promise<PipelineResult> {
  return api.get<PipelineResult>(`/api/pipeline/jobs/${jobId}`)
}

export function getPipelineJobs(): Promise<PipelineResult[]> {
  return api.get<PipelineResult[]>('/api/pipeline/jobs')
}
