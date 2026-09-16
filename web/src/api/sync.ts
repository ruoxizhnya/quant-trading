import api from './client'
import type {
  CreateSyncJobRequest,
  CreateSyncJobResponse,
  DataSourceHealth,
  DataSourceStatus,
  SyncJob,
  SyncJobListResponse,
  SyncJobStatus,
} from '@/types/sync'

// Data source (read-only observation, implemented locally on analysis).
export async function getDataSourceStatus(): Promise<DataSourceStatus> {
  return api.get<DataSourceStatus>('/api/datasource/status')
}

export async function getDataSourceHealth(): Promise<DataSourceHealth> {
  return api.get<DataSourceHealth>('/api/datasource/health')
}

// Sync jobs — the L0 job API (/api/sync/jobs*) reached through the analysis
// gateway proxy (ODR-062 S-B). The old /api/sync/status|import|stream
// endpoints never existed on any backend (ODR-062 取证 a).
export async function createSyncJob(request: CreateSyncJobRequest): Promise<CreateSyncJobResponse> {
  return api.post<CreateSyncJobResponse>('/api/sync/jobs', request)
}

export async function listSyncJobs(status?: SyncJobStatus, limit?: number): Promise<SyncJobListResponse> {
  const params = new URLSearchParams()
  if (status) params.set('status', status)
  if (limit) params.set('limit', String(limit))
  const qs = params.toString()
  return api.get<SyncJobListResponse>(`/api/sync/jobs${qs ? '?' + qs : ''}`)
}

export async function getSyncJob(jobId: string): Promise<SyncJob> {
  return api.get<SyncJob>(`/api/sync/jobs/${encodeURIComponent(jobId)}`)
}

export async function cancelSyncJob(jobId: string): Promise<{ message: string; job_id: string }> {
  return api.post(`/api/sync/jobs/${encodeURIComponent(jobId)}/cancel`)
}

export async function retrySyncJob(jobId: string): Promise<{ message: string; job_id: string; status: SyncJobStatus }> {
  return api.post(`/api/sync/jobs/${encodeURIComponent(jobId)}/retry`)
}

// SSE progress stream for one job — EventSource is constructed by the sync
// store (needs the raw URL); this builder keeps the endpoint contract in the
// API layer.
export function syncJobProgressStreamPath(jobId: string): string {
  return `/api/sync/jobs/${encodeURIComponent(jobId)}/progress`
}
