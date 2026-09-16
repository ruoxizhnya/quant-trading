import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type {
  CreateSyncJobRequest,
  DataImportRequest,
  DataSourceHealth,
  DataSourceStatus,
  SyncJob,
  SyncJobStatus,
} from '@/types/sync'
import {
  cancelSyncJob,
  createSyncJob,
  getDataSourceHealth,
  getDataSourceStatus,
  listSyncJobs,
  retrySyncJob,
  syncJobProgressStreamPath,
} from '@/api/sync'

function isJobActive(job: SyncJob): boolean {
  return job.status === 'pending' || job.status === 'running' || job.status === 'retrying'
}

export const useSyncStore = defineStore('sync', () => {
  // State
  const dataSourceStatus = ref<DataSourceStatus | null>(null)
  const dataSourceHealth = ref<DataSourceHealth | null>(null)
  const jobs = ref<SyncJob[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  const sseConnected = ref(false)
  const watchedJobId = ref<string | null>(null)

  let eventSource: EventSource | null = null

  // Getters
  const isDataSourceEnabled = computed(() => dataSourceStatus.value?.enabled ?? false)
  const primaryDataSource = computed(() => dataSourceStatus.value?.primary ?? 'unknown')

  // Most recent active job; falls back to the most recent job overall so a
  // just-finished run stays visible.
  const activeJob = computed<SyncJob | null>(() => {
    if (jobs.value.length === 0) return null
    return jobs.value.find(isJobActive) ?? jobs.value[0]
  })

  const isSyncRunning = computed(() => activeJob.value !== null && isJobActive(activeJob.value))
  const queueLength = computed(() => jobs.value.filter((j) => j.status === 'pending').length)
  const progressPercent = computed(() => activeJob.value?.progress_percent ?? 0)

  // Actions
  async function fetchDataSourceStatus() {
    isLoading.value = true
    error.value = null
    try {
      dataSourceStatus.value = await getDataSourceStatus()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch data source status'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  async function fetchDataSourceHealth() {
    try {
      dataSourceHealth.value = await getDataSourceHealth()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch data source health'
      throw err
    }
  }

  async function fetchSyncJobs() {
    isLoading.value = true
    error.value = null
    try {
      const resp = await listSyncJobs()
      jobs.value = resp.jobs
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch sync jobs'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  // Upsert one job — the merge point for SSE progress events and polling.
  // Exposed as an action so it is testable without an EventSource.
  function applyJobUpdate(job: SyncJob) {
    const idx = jobs.value.findIndex((j) => j.id === job.id)
    if (idx === -1) {
      jobs.value = [job, ...jobs.value]
    } else {
      jobs.value = jobs.value.map((j) => (j.id === job.id ? job : j))
    }
  }

  async function createJob(request: CreateSyncJobRequest): Promise<string> {
    isLoading.value = true
    error.value = null
    try {
      const resp = await createSyncJob(request)
      await fetchSyncJobs()
      watchJob(resp.job_id)
      return resp.job_id
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to create sync job'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  // Map the import form model onto L0 job types. 'all' fans out into one
  // OHLCV job plus one fundamentals job; the door accepts both ISO
  // YYYY-MM-DD (what the form yields) and YYYYMMDD.
  async function importData(request: DataImportRequest): Promise<string[]> {
    const params: Record<string, unknown> = { symbols: request.symbols }
    if (request.start_date) params.start_date = request.start_date
    if (request.end_date) params.end_date = request.end_date
    const jobTypes =
      request.data_type === 'all'
        ? ['ohlcv', 'fundamentals']
        : [request.data_type === 'fundamental' ? 'fundamentals' : 'ohlcv']
    const ids: string[] = []
    for (const type of jobTypes) {
      ids.push(await createJob({ type, params }))
    }
    return ids
  }

  async function cancelJob(jobId: string) {
    error.value = null
    try {
      await cancelSyncJob(jobId)
      await fetchSyncJobs()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to cancel sync job'
      throw err
    }
  }

  async function retryJob(jobId: string) {
    error.value = null
    try {
      await retrySyncJob(jobId)
      await fetchSyncJobs()
      watchJob(jobId)
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to retry sync job'
      throw err
    }
  }

  // SSE — per-job progress stream (GET /api/sync/jobs/:id/progress). The old
  // global /api/sync/stream never existed on any backend (ODR-062 取证 a).
  function watchJob(jobId: string) {
    unwatchJob()
    if (typeof EventSource === 'undefined') return // non-browser test env
    eventSource = new EventSource(syncJobProgressStreamPath(jobId))
    watchedJobId.value = jobId
    eventSource.onopen = () => {
      sseConnected.value = true
    }
    eventSource.addEventListener('progress', (event) => {
      try {
        applyJobUpdate(JSON.parse((event as MessageEvent).data) as SyncJob)
      } catch (err) {
        console.warn('Failed to parse SSE progress message:', err)
      }
    })
    eventSource.addEventListener('complete', (event) => {
      try {
        applyJobUpdate(JSON.parse((event as MessageEvent).data) as SyncJob)
      } catch {
        // ignore malformed terminal events
      }
      unwatchJob()
    })
    eventSource.onerror = () => {
      sseConnected.value = false
      // EventSource auto-reconnects; watchedJobId stays so the UI shows
      // which stream is being retried.
    }
  }

  function unwatchJob() {
    if (eventSource) {
      try {
        eventSource.close()
      } catch {
        // ignore close errors
      }
      eventSource = null
    }
    watchedJobId.value = null
    sseConnected.value = false
  }

  function clearError() {
    error.value = null
  }

  return {
    // State
    dataSourceStatus,
    dataSourceHealth,
    jobs,
    isLoading,
    error,
    sseConnected,
    watchedJobId,
    // Getters
    isDataSourceEnabled,
    primaryDataSource,
    activeJob,
    isSyncRunning,
    queueLength,
    progressPercent,
    // Actions
    fetchDataSourceStatus,
    fetchDataSourceHealth,
    fetchSyncJobs,
    applyJobUpdate,
    createJob,
    importData,
    cancelJob,
    retryJob,
    watchJob,
    unwatchJob,
    clearError,
  }
})
