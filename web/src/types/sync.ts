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

export interface DataSourceSwitchRequest {
  name: string
  type: 'http' | 'inmemory'
  url?: string
  token?: string
}

export interface DataSourceSwitchResponse {
  message: string
  name: string
  type: string
}

export interface SyncJob {
  id: string
  type: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  progress: number
  total: number
  message?: string
  created_at: string
  started_at?: string
  completed_at?: string
}

export interface SyncStatus {
  is_running: boolean
  current_job?: SyncJob
  queue_length: number
  last_sync?: string
}

export interface ImportTask {
  id: string
  symbol: string
  start_date: string
  end_date: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  records_imported: number
  error?: string
}

export interface DataImportRequest {
  symbols: string[]
  start_date: string
  end_date: string
  data_type: 'ohlcv' | 'fundamental' | 'all'
}

export interface DataImportResponse {
  job_id: string
  message: string
  tasks: ImportTask[]
}
