import api from './client'
import type { CopilotRequest, CopilotResponse } from '@/types/api'
import type { PipelineResult, PipelineReviewPayload } from '@/types/pipeline'

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

// P1-13 (ODR-017) — L5 人工审查 endpoint.
//
// Submits a human review decision (approve / reject / edit) for a
// completed pipeline job. The server returns the updated PipelineResult
// with the `review` field populated.
//
// Notes:
//   - For `decision: 'edit'`, `edited_yaml` must be non-empty.
//   - The 5-minute timeout mirrors runPipeline — review itself is
//     near-instant server-side, but the network budget covers slow
//     CI-style validation that may follow on the backend.
export function submitPipelineReview(
  jobId: string,
  payload: PipelineReviewPayload
): Promise<PipelineResult> {
  return api.post<PipelineResult>(
    `/api/pipeline/jobs/${jobId}/review`,
    payload,
    { timeout: 300000 }
  )
}
