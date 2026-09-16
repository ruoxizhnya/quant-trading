export interface DataSourceStatus {
  enabled: boolean
  primary?: string
  stopped?: boolean
  mode?: string
  source?: string
}

export interface DataSourceHealth {
  status: string
  primary?: string
  mode?: string
  error?: string
}

// Job status — mirrors pkg/sync/types/types.go JobStatus.
export type SyncJobStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'retrying'

// Mirrors pkg/sync/types/types.go Job JSON tags (single source of truth:
// the L0 sync-job API at POST/GET /api/sync/jobs, ODR-062).
export interface SyncJob {
  id: string
  job_type: string
  status: SyncJobStatus
  params?: unknown
  progress_percent: number
  total_items: number
  processed_items: number
  failed_items: number
  error_message?: string
  result?: unknown
  created_at: string
  started_at?: string
  completed_at?: string
  retry_count: number
  max_retries: number
  scheduled_at?: string
  worker_id?: string
}

// POST /api/sync/jobs request body.
export interface CreateSyncJobRequest {
  type: string
  params?: Record<string, unknown>
}

export interface CreateSyncJobResponse {
  message: string
  job_id: string
  status: SyncJobStatus
}

// GET /api/sync/jobs response.
export interface SyncJobListResponse {
  jobs: SyncJob[]
  count: number
}

// UI-level form model for the DataSync import form — mapped onto
// CreateSyncJobRequest by the sync store (not an API contract).
export interface DataImportRequest {
  symbols: string[]
  start_date: string
  end_date: string
  data_type: 'ohlcv' | 'fundamental' | 'all'
}
