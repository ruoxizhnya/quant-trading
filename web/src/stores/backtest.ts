import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { BacktestResult, BacktestJob, Trade, HistoryEntry, TradeDisplay } from '@/types/api'
import { listBacktestJobs } from '@/api/backtest'

const MAX_HISTORY = 20
const STORAGE_KEY = 'qbh'

type HistoryItem = HistoryEntry

function safeSerialize(obj: HistoryItem[]): string {
  try {
    return JSON.stringify(obj)
  } catch {
    return '[]'
  }
}

function safeParse(raw: string | null): HistoryItem[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function toHistoryItem(result: BacktestResult): HistoryItem {
  return {
    id: result.id,
    strategy: result.strategy,
    stock_pool: result.stock_pool,
    start_date: result.start_date,
    end_date: result.end_date,
    total_return: result.total_return,
    sharpe_ratio: result.sharpe_ratio,
    max_drawdown: result.max_drawdown,
    created_at: result.created_at || new Date().toISOString(),
  }
}

function jobToHistoryItem(job: BacktestJob): HistoryItem {
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
    created_at: job.created_at,
  }
}

export const useBacktestStore = defineStore('backtest', () => {
  const history = ref<HistoryItem[]>([])
  const currentResult = ref<BacktestResult | null>(null)
  const loading = ref(false)

  // In-memory trade cache keyed by result ID (not persisted to localStorage)
  const tradesMap = ref<Map<string, TradeDisplay[]>>(new Map())

  function addToHistory(result: BacktestResult) {
    if (!result || !result.id) return

    const item = toHistoryItem(result)
    const existingIdx = history.value.findIndex((h) => h.id === result.id)
    if (existingIdx >= 0) {
      history.value[existingIdx] = item
    } else {
      history.value.unshift(item)
    }

    // Store trades in memory (map backend field names to display format)
    if (result.trades?.length) {
      tradesMap.value.set(result.id, result.trades.map((t: Trade): TradeDisplay => ({
        id: t.id,
        symbol: t.symbol,
        direction: t.direction,
        timestamp: t.timestamp || '',
        price: t.price ?? null,
        quantity: t.quantity ?? null,
        commission: t.commission ?? null,
        // Display aliases
        entry_date: t.timestamp || t.entry_date || '',
        entry_price: t.price ?? t.entry_price ?? null,
        exit_date: t.exit_date ?? null,
        exit_price: t.exit_price ?? null,
        pnl: t.pnl ?? 0,
        pnl_pct: t.pnl_pct ?? 0,
      })))
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

      for (const job of dbJobs) {
        const item = jobToHistoryItem(job)

        // Always update or add the history item
        const existingIdx = history.value.findIndex((h) => h.id === job.id)
        if (existingIdx >= 0) {
          history.value[existingIdx] = item
        } else {
          history.value.push(item)
        }

        // Always load trades from job result if available (ensures fresh data)
        if (job.result?.trades?.length) {
          tradesMap.value.set(job.id, job.result.trades.map((t: Trade): TradeDisplay => ({
            id: t.id,
            symbol: t.symbol,
            direction: t.direction,
            timestamp: t.timestamp || '',
            price: t.price ?? null,
            quantity: t.quantity ?? null,
            commission: t.commission ?? null,
            entry_date: t.timestamp || t.entry_date || '',
            entry_price: t.price ?? t.entry_price ?? null,
            exit_date: t.exit_date ?? null,
            exit_price: t.exit_price ?? null,
            pnl: t.pnl ?? 0,
            pnl_pct: t.pnl_pct ?? 0,
          })))
        }
      }

      history.value.sort((a, b) => {
        const ta = a.start_date ? new Date(a.start_date).getTime() : 0
        const tb = b.start_date ? new Date(b.start_date).getTime() : 0
        return tb - ta
      })

      if (history.value.length > MAX_HISTORY) {
        history.value = history.value.slice(0, MAX_HISTORY)
      }
    } catch {}
  }

  function clearHistory() {
    history.value = []
    tradesMap.value.clear()
    try { localStorage.removeItem(STORAGE_KEY) } catch {}
  }

  // Computed history with trades attached (for UI display)
  const historyWithTrades = computed(() => {
    return history.value.map(item => ({
      ...item,
      trades: tradesMap.value.get(item.id) || [],
    }))
  })

  return { history: historyWithTrades, currentResult, loading, addToHistory, loadHistory, loadHistoryFromDB, clearHistory }
})
