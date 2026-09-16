import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSyncStore } from './sync'
import type { SyncJob } from '@/types/sync'

vi.mock('@/api/sync', () => ({
  getDataSourceStatus: vi.fn(),
  getDataSourceHealth: vi.fn(),
  createSyncJob: vi.fn(),
  listSyncJobs: vi.fn(),
  getSyncJob: vi.fn(),
  cancelSyncJob: vi.fn(),
  retrySyncJob: vi.fn(),
  syncJobProgressStreamPath: vi.fn((id: string) => `/api/sync/jobs/${id}/progress`),
}))

import {
  createSyncJob,
  getDataSourceHealth,
  getDataSourceStatus,
  listSyncJobs,
} from '@/api/sync'

const mockedGetDataSourceStatus = vi.mocked(getDataSourceStatus)
const mockedGetDataSourceHealth = vi.mocked(getDataSourceHealth)
const mockedCreateSyncJob = vi.mocked(createSyncJob)
const mockedListSyncJobs = vi.mocked(listSyncJobs)

function makeJob(overrides: Partial<SyncJob> = {}): SyncJob {
  return {
    id: 'job-1',
    job_type: 'stocks',
    status: 'pending',
    progress_percent: 0,
    total_items: 0,
    processed_items: 0,
    failed_items: 0,
    created_at: '2026-09-16T00:00:00Z',
    retry_count: 0,
    max_retries: 3,
    ...overrides,
  }
}

describe('sync store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('has a clean initial state', () => {
    const store = useSyncStore()
    expect(store.jobs).toEqual([])
    expect(store.activeJob).toBeNull()
    expect(store.isSyncRunning).toBe(false)
    expect(store.progressPercent).toBe(0)
    expect(store.isDataSourceEnabled).toBe(false)
    expect(store.primaryDataSource).toBe('unknown')
  })

  it('fetches data source status', async () => {
    const store = useSyncStore()
    mockedGetDataSourceStatus.mockResolvedValue({
      enabled: true,
      primary: 'tushare',
    })
    await store.fetchDataSourceStatus()
    expect(store.isDataSourceEnabled).toBe(true)
    expect(store.primaryDataSource).toBe('tushare')
  })

  it('surfaces data source status errors', async () => {
    const store = useSyncStore()
    mockedGetDataSourceStatus.mockRejectedValue(new Error('boom'))
    await expect(store.fetchDataSourceStatus()).rejects.toThrow('boom')
    expect(store.error).toBe('boom')
  })

  it('fetches data source health', async () => {
    const store = useSyncStore()
    mockedGetDataSourceHealth.mockResolvedValue({ status: 'ok' })
    await store.fetchDataSourceHealth()
    expect(store.dataSourceHealth).toEqual({ status: 'ok' })
  })

  it('fetches sync jobs', async () => {
    const store = useSyncStore()
    mockedListSyncJobs.mockResolvedValue({ jobs: [makeJob()], count: 1 })
    await store.fetchSyncJobs()
    expect(store.jobs).toHaveLength(1)
    expect(store.jobs[0].id).toBe('job-1')
  })

  it('applies job updates as upsert', () => {
    const store = useSyncStore()
    store.applyJobUpdate(makeJob({ id: 'a', status: 'running' }))
    expect(store.jobs).toHaveLength(1)
    store.applyJobUpdate(makeJob({ id: 'a', status: 'completed', progress_percent: 100 }))
    expect(store.jobs).toHaveLength(1)
    expect(store.jobs[0].status).toBe('completed')
    store.applyJobUpdate(makeJob({ id: 'b' }))
    expect(store.jobs).toHaveLength(2)
  })

  it('prefers an active job over a terminal one', () => {
    const store = useSyncStore()
    store.applyJobUpdate(makeJob({ id: 'done', status: 'completed', progress_percent: 100 }))
    store.applyJobUpdate(makeJob({ id: 'active', status: 'running', progress_percent: 40 }))
    expect(store.activeJob?.id).toBe('active')
    expect(store.isSyncRunning).toBe(true)
    expect(store.progressPercent).toBe(40)
  })

  it('creates a job and refreshes the list', async () => {
    const store = useSyncStore()
    mockedCreateSyncJob.mockResolvedValue({
      message: 'stocks sync job created',
      job_id: 'job-9',
      status: 'pending',
    })
    mockedListSyncJobs.mockResolvedValue({ jobs: [makeJob({ id: 'job-9' })], count: 1 })
    const id = await store.createJob({ type: 'stocks' })
    expect(id).toBe('job-9')
    expect(mockedCreateSyncJob).toHaveBeenCalledWith({ type: 'stocks' })
    expect(mockedListSyncJobs).toHaveBeenCalled()
    expect(store.jobs[0].id).toBe('job-9')
  })

  it('maps the import form onto L0 job types', async () => {
    const store = useSyncStore()
    mockedCreateSyncJob.mockResolvedValue({
      message: 'ok',
      job_id: 'job-x',
      status: 'pending',
    })
    mockedListSyncJobs.mockResolvedValue({ jobs: [], count: 0 })

    await store.importData({
      symbols: ['600519'],
      start_date: '2024-01-01',
      end_date: '',
      data_type: 'ohlcv',
    })
    expect(mockedCreateSyncJob).toHaveBeenCalledWith({
      type: 'ohlcv',
      params: { symbols: ['600519'], start_date: '2024-01-01' },
    })

    mockedCreateSyncJob.mockClear()
    await store.importData({
      symbols: ['600519'],
      start_date: '',
      end_date: '',
      data_type: 'fundamental',
    })
    expect(mockedCreateSyncJob).toHaveBeenCalledWith({
      type: 'fundamentals',
      params: { symbols: ['600519'] },
    })

    mockedCreateSyncJob.mockClear()
    await store.importData({
      symbols: [],
      start_date: '',
      end_date: '',
      data_type: 'all',
    })
    expect(mockedCreateSyncJob).toHaveBeenCalledTimes(2)
    expect(mockedCreateSyncJob.mock.calls[0][0].type).toBe('ohlcv')
    expect(mockedCreateSyncJob.mock.calls[1][0].type).toBe('fundamentals')
  })

  it('clears errors', async () => {
    const store = useSyncStore()
    mockedGetDataSourceStatus.mockRejectedValue(new Error('boom'))
    await expect(store.fetchDataSourceStatus()).rejects.toThrow()
    expect(store.error).toBe('boom')
    store.clearError()
    expect(store.error).toBeNull()
  })
})
