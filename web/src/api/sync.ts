import api from './client'
import type {
  DataSourceStatus,
  DataSourceHealth,
  DataSourceSwitchRequest,
  DataSourceSwitchResponse,
  SyncStatus,
  DataImportRequest,
  DataImportResponse,
} from '@/types/sync'

export async function getDataSourceStatus(): Promise<DataSourceStatus> {
  return api.get<DataSourceStatus>('/api/datasource/status')
}

export async function getDataSourceHealth(): Promise<DataSourceHealth> {
  return api.get<DataSourceHealth>('/api/datasource/health')
}

export async function switchDataSource(
  request: DataSourceSwitchRequest,
): Promise<DataSourceSwitchResponse> {
  return api.post<DataSourceSwitchResponse>('/api/datasource/switch', request)
}

export async function getSyncStatus(): Promise<SyncStatus> {
  return api.get<SyncStatus>('/api/sync/status')
}

export async function startDataImport(
  request: DataImportRequest,
): Promise<DataImportResponse> {
  return api.post<DataImportResponse>('/api/sync/import', request)
}

export async function getImportProgress(jobId: string): Promise<SyncStatus> {
  return api.get<SyncStatus>(`/api/sync/import/${jobId}`)
}
