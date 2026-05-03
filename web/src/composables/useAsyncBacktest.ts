import { ref, type Ref } from 'vue'
import {
  createBacktestJob,
  getBacktestJob,
  getBacktestReport,
  type JobResponse,
} from '@/api/backtest'
import type { BacktestResult } from '@/types/api'

export type JobStatus = 'idle' | 'submitting' | 'pending' | 'running' | 'completed' | 'failed' | 'cancelled' | 'timeout'

interface AsyncBacktestState {
  status: JobStatus
  jobId: string | null
  progress: number
  error: string | null
  result: BacktestResult | null
}

const POLL_INTERVAL_MS = 2000
const TIMEOUT_MS = 5 * 60 * 1000
const MAX_POLL_ATTEMPTS = 150

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
  let pollAttempts = 0
  let startTime = 0

  async function pollJob(jobId: string) {
    if (aborted) return

    pollAttempts++

    if (startTime && Date.now() - startTime > TIMEOUT_MS) {
      console.warn('[AsyncBacktest] Polling timeout reached', { jobId, elapsed: Date.now() - startTime })
      stopPolling()
      state.value.status = 'timeout'
      state.value.error = '回测超时（5分钟），请检查后端服务状态'
      state.value.progress = 0
      return
    }

    if (pollAttempts > MAX_POLL_ATTEMPTS) {
      console.warn('[AsyncBacktest] Max poll attempts reached', { jobId, attempts: pollAttempts })
      stopPolling()
      state.value.status = 'timeout'
      state.value.error = '轮询次数超限，请刷新页面重试'
      return
    }

    try {
      const job = await getBacktestJob(jobId)

      if (!job || !job.id) {
        console.warn('[AsyncBacktest] Job not found or invalid response', { jobId, job })
        if (pollAttempts < 3) {
          return
        }
        stopPolling()
        state.value.status = 'failed'
        state.value.error = '任务未找到，可能已被删除'
        return
      }

      state.value.jobId = job.id
      state.value.status = job.status as JobStatus

      switch (job.status) {
        case 'completed':
          if (!state.value.result && job.result) {
            try {
              const report = await getBacktestReport(jobId)
              state.value.result = report
            } catch (e) {
              console.warn('[AsyncBacktest] Failed to get report, using job result', e)
              state.value.result = job.result
            }
          } else if (job.result && !state.value.result) {
            state.value.result = job.result
          }
          state.value.progress = 100
          stopPolling()
          break

        case 'failed':
          stopPolling()
          state.value.status = 'failed'
          state.value.error = job.error || '回测执行失败，请查看后端日志'
          state.value.progress = 0
          console.error('[AsyncBacktest] Job failed', { jobId, error: job.error })
          break

        case 'running':
          if (startTime > 0) {
            const elapsed = Date.now() - startTime
            const estimatedProgress = Math.min(90, Math.floor((elapsed / 120000) * 85) + 5)
            state.value.progress = Math.max(state.value.progress, estimatedProgress)
          } else {
            state.value.progress = Math.min(state.value.progress + 2, 20)
          }

          if (startTime && Date.now() - startTime > 180000) {
            console.warn('[AsyncBacktest] Job running for >3min', {
              jobId,
              elapsed: Date.now() - startTime,
              progress: state.value.progress
            })
          }
          break

        case 'pending':
          if (state.value.progress < 10) {
            state.value.progress = 5 + Math.min(5, Math.floor(pollAttempts / 2))
          }

          if (startTime && Date.now() - startTime > 30000) {
            console.warn('[AsyncBacktest] Job still pending after 30s', {
              jobId,
              elapsed: Date.now() - startTime,
              attempts: pollAttempts
            })
          }
          break

        default:
          console.warn('[AsyncBacktest] Unknown job status', { status: job.status, jobId })
      }
    } catch (e: unknown) {
      const errorMessage = e instanceof Error ? e.message : String(e)
      const errorStatus = (e as { status?: number })?.status

      if (!aborted && errorStatus !== 404) {
        console.error('[AsyncBacktest] Poll error', {
          jobId,
          attempt: pollAttempts,
          error: errorMessage,
          status: errorStatus
        })

        if (pollAttempts % 10 === 0) {
          state.value.error = `网络请求失败 (${errorStatus || 'unknown'})，正在重试...`
        }
      }
    }
  }

  async function submit(req: {
    strategy_id: string
    universe: string
    start_date: string
    end_date: string
    initial_capital?: number
    commission_rate?: number
    slippage_rate?: number
  }) {
    reset()

    state.value.status = 'submitting'
    state.value.progress = 1

    try {
      const res: JobResponse = await createBacktestJob(req)

      state.value.jobId = res.job_id
      state.value.status = res.status as JobStatus
      state.value.progress = 5

      startTime = Date.now()
      pollAttempts = 0

      await pollJob(res.job_id)

      if (!aborted && state.value.status !== 'completed' && state.value.status !== 'failed') {
        pollTimer = setInterval(() => {
          if (!aborted) {
            pollJob(res.job_id).catch((e) => {
              console.error('[AsyncBacktest] Interval poll error', e)
            })
          }
        }, POLL_INTERVAL_MS)
      }
    } catch (e: unknown) {
      console.error('[AsyncBacktest] Submit error', e)
      state.value.status = 'failed'
      state.value.error = e instanceof Error ? e.message : '提交回测任务失败'
      state.value.progress = 0
    }
  }

  function cancel() {
    aborted = true
    stopPolling()
    if (state.value.status === 'pending' || state.value.status === 'running' || state.value.status === 'submitting') {
      state.value.status = 'cancelled'
      state.value.error = '用户取消'
    }
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function reset() {
    cancel()
    state.value = {
      status: 'idle',
      jobId: null,
      progress: 0,
      error: null,
      result: null,
    }
    aborted = false
    pollAttempts = 0
    startTime = 0
  }

  return { state, submit, cancel, reset }
}
