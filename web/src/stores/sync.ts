import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type {
  DataSourceStatus,
  DataSourceHealth,
  SyncStatus,
  SyncJob,
  DataImportRequest,
} from '@/types/sync'
import {
  getDataSourceStatus,
  getDataSourceHealth,
  switchDataSource,
  getSyncStatus,
  startDataImport,
} from '@/api/sync'

export const useSyncStore = defineStore('sync', () => {
  // State
  const dataSourceStatus = ref<DataSourceStatus | null>(null)
  const dataSourceHealth = ref<DataSourceHealth | null>(null)
  const syncStatus = ref<SyncStatus | null>(null)
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  const sseConnected = ref(false)

  // SSE state
  let eventSource: EventSource | null = null

  // Getters
  const isDataSourceEnabled = computed(() => dataSourceStatus.value?.enabled ?? false)
  const primaryDataSource = computed(() => dataSourceStatus.value?.primary ?? 'unknown')
  const isSyncRunning = computed(() => syncStatus.value?.is_running ?? false)
  const queueLength = computed(() => syncStatus.value?.queue_length ?? 0)
  const currentJob = computed(() => syncStatus.value?.current_job ?? null)
  const progressPercent = computed(() => {
    const job = syncStatus.value?.current_job
    if (!job || job.total === 0) return 0
    return Math.round((job.progress / job.total) * 100)
  })

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
    isLoading.value = true
    error.value = null
    try {
      dataSourceHealth.value = await getDataSourceHealth()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch data source health'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  async function switchSource(name: string, type: 'http' | 'inmemory', url?: string) {
    isLoading.value = true
    error.value = null
    try {
      const response = await switchDataSource({ name, type, url })
      await fetchDataSourceStatus()
      return response
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to switch data source'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  async function fetchSyncStatus() {
    isLoading.value = true
    error.value = null
    try {
      syncStatus.value = await getSyncStatus()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to fetch sync status'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  async function importData(request: DataImportRequest) {
    isLoading.value = true
    error.value = null
    try {
      const response = await startDataImport(request)
      await fetchSyncStatus()
      return response
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to start data import'
      throw err
    } finally {
      isLoading.value = false
    }
  }

  function clearError() {
    error.value = null
  }

  // SSE Actions
  function connectSSE(baseURL: string = '') {
    if (eventSource?.readyState === EventSource.OPEN) return

    const url = baseURL
      ? `${baseURL}/api/sync/stream`
      : '/api/sync/stream'

    eventSource = new EventSource(url)

    eventSource.onopen = () => {
      sseConnected.value = true
    }

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as SyncStatus
        syncStatus.value = data
      } catch (e) {
        console.warn('Failed to parse SSE message:', e)
      }
    }

    eventSource.onerror = () => {
      sseConnected.value = false
      // Auto-reconnect will be handled by EventSource automatically
    }
  }

  function disconnectSSE() {
    if (eventSource) {
      eventSource.close()
      eventSource = null
      sseConnected.value = false
    }
  }

  function updateSyncStatusFromJob(job: SyncJob) {
    if (!syncStatus.value) {
      syncStatus.value = {
        is_running: job.status === 'running',
        current_job: job,
        queue_length: 0,
      }
    } else {
      syncStatus.value.current_job = job
      syncStatus.value.is_running = job.status === 'running'
    }
  }

  return {
    // State
    dataSourceStatus,
    dataSourceHealth,
    syncStatus,
    isLoading,
    error,
    sseConnected,
    // Getters
    isDataSourceEnabled,
    primaryDataSource,
    isSyncRunning,
    queueLength,
    currentJob,
    progressPercent,
    // Actions
    fetchDataSourceStatus,
    fetchDataSourceHealth,
    switchSource,
    fetchSyncStatus,
    importData,
    clearError,
    // SSE Actions
    connectSSE,
    disconnectSSE,
    updateSyncStatusFromJob,
  }
})
