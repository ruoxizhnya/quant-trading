import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { BacktestResult } from '@/types/api'

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
    final_capital: result.final_capital,
  }
}

export const useBacktestStore = defineStore('backtest', () => {
  const history = ref<BacktestResult[]>([])
  const currentResult = ref<BacktestResult | null>(null)
  const loading = ref(false)

  function addToHistory(result: BacktestResult) {
    if (!result || !result.id) return

    const light = stripHeavyData(result)
    history.value.unshift(light as any)

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

  function clearHistory() {
    history.value = []
    try { localStorage.removeItem(STORAGE_KEY) } catch {}
  }

  return { history, currentResult, loading, addToHistory, loadHistory, clearHistory }
})
