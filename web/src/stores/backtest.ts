import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { BacktestResult, BacktestJob } from '@/types/api'
import { listBacktestJobs } from '@/api/backtest'

const MAX_HISTORY = 20
const STORAGE_KEY = 'qbh'

function safeSerialize(obj: any): string {
  try {
    return JSON.stringify(obj)
  } catch {
    return '[]'
  }
}

function safeParse(raw: string | null): any[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function stripHeavyData(result: BacktestResult): any {
  return {
    id: result.id,
    strategy: result.strategy,
    stock_pool: result.stock_pool,
    start_date: result.start_date,
    end_date: result.end_date,
    total_return: result.total_return,
    sharpe_ratio: result.sharpe_ratio,
    max_drawdown: result.max_drawdown,
    win_rate: result.win_rate,
    total_trades: result.total_trades,
    initial_capital: result.initial_capital,
  }
}

function jobToLightResult(job: BacktestJob): any {
  const r = job.result
  return {
    id: job.id,
    strategy: job.strategy_id,
    stock_pool: job.universe ? job.universe.split(',') : [],
    start_date: job.start_date,
    end_date: job.end_date,
    total_return: r?.total_return ?? 0,
    sharpe_ratio: r?.sharpe_ratio ?? 0,
    max_drawdown: r?.max_drawdown ?? 0,
    win_rate: r?.win_rate ?? 0,
    total_trades: r?.total_trades ?? 0,
    status: job.status,
    created_at: job.created_at,
  }
}

export const useBacktestStore = defineStore('backtest', () => {
  const history = ref<BacktestResult[]>([])
  const currentResult = ref<BacktestResult | null>(null)
  const loading = ref(false)

  function addToHistory(result: BacktestResult) {
    if (!result || !result.id) return

    const light = stripHeavyData(result)
    const existingIdx = history.value.findIndex((h: any) => h.id === result.id)
    if (existingIdx >= 0) {
      history.value[existingIdx] = light as any
    } else {
      history.value.unshift(light as any)
    }

    if (history.value.length > MAX_HISTORY) {
      history.value = history.value.slice(0, MAX_HISTORY)
    }

    try {
      const serialized = safeSerialize(history.value)
      if (serialized.length < 5 * 1024 * 1024) {
        localStorage.setItem(STORAGE_KEY, serialized)
      } else {
        localStorage.setItem(STORAGE_KEY, safeSerialize(history.value.slice(0, 5)))
      }
    } catch {}
  }

  function loadHistory() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      history.value = safeParse(raw)
    } catch {
      history.value = []
    }
  }

  async function loadHistoryFromDB() {
    try {
      const res = await listBacktestJobs(MAX_HISTORY)
      const dbJobs = (res.jobs || [])
        .filter((j: BacktestJob) => j.status === 'completed')
        .map(jobToLightResult)

      const existingIds = new Set(history.value.map((h: any) => h.id))
      for (const dbItem of dbJobs) {
        if (!existingIds.has(dbItem.id)) {
          history.value.push(dbItem as any)
        }
      }

      history.value.sort((a: any, b: any) => {
        const ta = a.created_at ? new Date(a.created_at).getTime() : 0
        const tb = b.created_at ? new Date(b.created_at).getTime() : 0
        return tb - ta
      })

      if (history.value.length > MAX_HISTORY) {
        history.value = history.value.slice(0, MAX_HISTORY)
      }
    } catch {}
  }

  function clearHistory() {
    history.value = []
    try { localStorage.removeItem(STORAGE_KEY) } catch {}
  }

  return { history, currentResult, loading, addToHistory, loadHistory, loadHistoryFromDB, clearHistory }
})
