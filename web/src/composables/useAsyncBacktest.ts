import { ref, onUnmounted } from 'vue'
import { createBacktestJob, getBacktestJob, getBacktestReport } from '@/api/backtest'
import type { BacktestResult, BacktestJob } from '@/types/api'

const POLL_INTERVAL_MS = 2000

export type JobStatus = 'idle' | 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface AsyncBacktestState {
  status: JobStatus
  jobId: string | null
  progress: number
  error: string | null
  result: BacktestResult | null
}

export function useAsyncBacktest() {
  const state = ref<AsyncBacktestState>({
    status: 'idle',
    jobId: null,
    progress: 0,
    error: null,
    result: null,
  })

  let pollTimer: ReturnType<typeof setInterval> | null = null
  let aborted = false

  function reset() {
    stopPolling()
    state.value = { status: 'idle', jobId: null, progress: 0, error: null, result: null }
    aborted = false
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  async function pollJob(jobId: string) {
    if (aborted) return
    try {
      const job = await getBacktestJob(jobId)
      state.value.status = job.status as JobStatus
      state.value.jobId = job.id

      if (job.status === 'completed' && !state.value.result) {
        const report = await getBacktestReport(jobId)
        state.value.result = report
        state.value.progress = 100
        stopPolling()
        return
      }

      if (job.status === 'failed') {
        state.value.error = job.error || '回测执行失败'
        state.value.progress = 100
        stopPolling()
        return
      }

      if (job.status === 'running') {
        const elapsed = Date.now() - new Date(job.created_at).getTime()
        state.value.progress = Math.min(90, Math.floor((elapsed / 120000) * 90))
      } else if (job.status === 'pending') {
        state.value.progress = 5
      }
    } catch (e: any) {
      if (!aborted && e?.status !== 404) {
        console.warn('Job poll error:', e)
      }
    }
  }

  async function submit(req: {
    strategy_id: string
    universe: string
    start_date: string
    end_date: string
  }) {
    reset()
    state.value.status = 'pending'
    state.value.progress = 5

    try {
      const res = await createBacktestJob({
        strategy_id: req.strategy_id,
        universe: req.universe,
        start_date: req.start_date,
        end_date: req.end_date,
      })
      state.value.jobId = res.job_id
      state.value.status = res.status as JobStatus

      pollTimer = setInterval(() => pollJob(res.job_id), POLL_INTERVAL_MS)
      pollJob(res.job_id)
    } catch (e: any) {
      state.value.status = 'failed'
      state.value.error = e?.message || '创建任务失败'
    }
  }

  function cancel() {
    aborted = true
    stopPolling()
    if (state.value.status === 'pending' || state.value.status === 'running') {
      state.value.status = 'cancelled'
    }
  }

  onUnmounted(() => {
    stopPolling()
  })

  return {
    state,
    submit,
    cancel,
    reset,
  }
}
