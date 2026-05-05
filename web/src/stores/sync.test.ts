import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSyncStore } from './sync'
import * as syncApi from '@/api/sync'
import type { DataSourceStatus, DataSourceHealth, SyncStatus, DataImportRequest } from '@/types/sync'

vi.mock('@/api/sync', () => ({
  getDataSourceStatus: vi.fn(),
  getDataSourceHealth: vi.fn(),
  switchDataSource: vi.fn(),
  getSyncStatus: vi.fn(),
  startDataImport: vi.fn(),
}))

describe('useSyncStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('should have correct initial state', () => {
    const store = useSyncStore()
    expect(store.dataSourceStatus).toBeNull()
    expect(store.dataSourceHealth).toBeNull()
    expect(store.syncStatus).toBeNull()
    expect(store.isLoading).toBe(false)
    expect(store.error).toBeNull()
    expect(store.sseConnected).toBe(false)
  })

  it('should compute isDataSourceEnabled correctly', () => {
    const store = useSyncStore()
    expect(store.isDataSourceEnabled).toBe(false)

    store.dataSourceStatus = { enabled: true, primary: 'test' }
    expect(store.isDataSourceEnabled).toBe(true)
  })

  it('should compute primaryDataSource correctly', () => {
    const store = useSyncStore()
    expect(store.primaryDataSource).toBe('unknown')

    store.dataSourceStatus = { enabled: true, primary: 'postgres' }
    expect(store.primaryDataSource).toBe('postgres')
  })

  it('should compute isSyncRunning correctly', () => {
    const store = useSyncStore()
    expect(store.isSyncRunning).toBe(false)

    store.syncStatus = { is_running: true, queue_length: 0 }
    expect(store.isSyncRunning).toBe(true)
  })

  it('should compute queueLength correctly', () => {
    const store = useSyncStore()
    expect(store.queueLength).toBe(0)

    store.syncStatus = { is_running: false, queue_length: 5 }
    expect(store.queueLength).toBe(5)
  })

  it('should compute progressPercent correctly', () => {
    const store = useSyncStore()
    expect(store.progressPercent).toBe(0)

    store.syncStatus = {
      is_running: true,
      queue_length: 0,
      current_job: {
        id: '1',
        type: 'import',
        status: 'running',
        progress: 50,
        total: 100,
        created_at: '2024-01-01T00:00:00Z',
      },
    }
    expect(store.progressPercent).toBe(50)
  })

  it('should fetch data source status', async () => {
    const mockStatus: DataSourceStatus = { enabled: true, primary: 'postgres' }
    vi.mocked(syncApi.getDataSourceStatus).mockResolvedValue(mockStatus)

    const store = useSyncStore()
    await store.fetchDataSourceStatus()

    expect(store.dataSourceStatus).toEqual(mockStatus)
    expect(store.isLoading).toBe(false)
    expect(store.error).toBeNull()
  })

  it('should handle fetch data source status error', async () => {
    vi.mocked(syncApi.getDataSourceStatus).mockRejectedValue(new Error('Network error'))

    const store = useSyncStore()
    await expect(store.fetchDataSourceStatus()).rejects.toThrow('Network error')

    expect(store.dataSourceStatus).toBeNull()
    expect(store.isLoading).toBe(false)
    expect(store.error).toBe('Network error')
  })

  it('should fetch data source health', async () => {
    const mockHealth: DataSourceHealth = { status: 'healthy', primary: 'postgres' }
    vi.mocked(syncApi.getDataSourceHealth).mockResolvedValue(mockHealth)

    const store = useSyncStore()
    await store.fetchDataSourceHealth()

    expect(store.dataSourceHealth).toEqual(mockHealth)
    expect(store.isLoading).toBe(false)
  })

  it('should switch data source', async () => {
    const mockResponse = { message: 'switched', name: 'test', type: 'http' }
    vi.mocked(syncApi.switchDataSource).mockResolvedValue(mockResponse)
    vi.mocked(syncApi.getDataSourceStatus).mockResolvedValue({ enabled: true, primary: 'test' })

    const store = useSyncStore()
    const result = await store.switchSource('test', 'http', 'http://localhost:8081')

    expect(result).toEqual(mockResponse)
    expect(syncApi.switchDataSource).toHaveBeenCalledWith({
      name: 'test',
      type: 'http',
      url: 'http://localhost:8081',
    })
  })

  it('should fetch sync status', async () => {
    const mockStatus: SyncStatus = { is_running: false, queue_length: 0 }
    vi.mocked(syncApi.getSyncStatus).mockResolvedValue(mockStatus)

    const store = useSyncStore()
    await store.fetchSyncStatus()

    expect(store.syncStatus).toEqual(mockStatus)
    expect(store.isLoading).toBe(false)
  })

  it('should import data', async () => {
    const mockResponse = { job_id: '123', message: 'started', tasks: [] }
    vi.mocked(syncApi.startDataImport).mockResolvedValue(mockResponse)
    vi.mocked(syncApi.getSyncStatus).mockResolvedValue({ is_running: true, queue_length: 1 })

    const store = useSyncStore()
    const request: DataImportRequest = {
      symbols: ['AAPL'],
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      data_type: 'ohlcv',
    }
    const result = await store.importData(request)

    expect(result).toEqual(mockResponse)
    expect(syncApi.startDataImport).toHaveBeenCalledWith(request)
  })

  it('should clear error', () => {
    const store = useSyncStore()
    store.error = 'Some error'
    store.clearError()
    expect(store.error).toBeNull()
  })

  it('should update sync status from job', () => {
    const store = useSyncStore()
    const job = {
      id: '1',
      type: 'import',
      status: 'running' as const,
      progress: 30,
      total: 100,
      created_at: '2024-01-01T00:00:00Z',
    }

    store.updateSyncStatusFromJob(job)

    expect(store.syncStatus).toEqual({
      is_running: true,
      current_job: job,
      queue_length: 0,
    })
  })

  it('should update existing sync status from job', () => {
    const store = useSyncStore()
    store.syncStatus = { is_running: false, queue_length: 2 }

    const job = {
      id: '1',
      type: 'import',
      status: 'completed' as const,
      progress: 100,
      total: 100,
      created_at: '2024-01-01T00:00:00Z',
    }

    store.updateSyncStatusFromJob(job)

    expect(store.syncStatus).toEqual({
      is_running: false,
      current_job: job,
      queue_length: 2,
    })
  })
})
