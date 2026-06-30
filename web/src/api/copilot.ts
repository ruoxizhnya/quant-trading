import api from './client'
import type { CopilotRequest, CopilotResponse } from '@/types/api'

// S7-P2-7: removed pipeline-related functions (runPipeline,
// getPipelineJob, getPipelineJobs, submitPipelineReview) and the
// saveStrategy / getCopilotStats stubs. None of these had any live
// consumers after the AI Research dead-code deletion — only the now-
// deleted components/ai/* used them. Kept only generateStrategy, which
// is wired into the Copilot page (router:/copilot → Copilot.vue).
// types/pipeline.ts was also deleted (no remaining consumers).

export function generateStrategy(req: CopilotRequest): Promise<CopilotResponse> {
  return api.post<CopilotResponse>('/api/copilot/generate', req, { timeout: 120000 })
}
