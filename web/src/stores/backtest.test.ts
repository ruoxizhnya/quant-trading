import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBacktestStore } from '@/stores/backtest'
import type { BacktestResult } from '@/types/api'

function makeResult(overrides: Partial<BacktestResult> = {}): BacktestResult {
  return {
    id: 'test-id-1',
    strategy: 'momentum',
    stock_pool: ['600000.SH'],
    start_date: '2024-01-01',
    end_date: '2024-06-30',
    total_return: 0.15,
    annual_return: 0.30,
    sharpe_ratio: 1.5,
    max_drawdown: -0.08,
    win_rate: 0.6,
    total_trades: 10,
    portfolio_values: [],
    trades: [
      {
        id: 'trade-1',
        symbol: '600000.SH',
        direction: 'long',
        price: 10.5,
        quantity: 1000,
        commission: 3.15,
        timestamp: '2024-01-15T00:00:00Z',
        pending_qty: 0,
        filled_qty: 1000,
      },
      {
        id: 'trade-2',
        symbol: '600000.SH',
        direction: 'close',
        price: 12.0,
        quantity: 1000,
        commission: 3.60,
        timestamp: '2024-06-28T00:00:00Z',
        pending_qty: 0,
        filled_qty: 1000,
      },
    ],
    ...overrides,
  } as BacktestResult
}

describe('useBacktestStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('initializes with empty history', () => {
    const store = useBacktestStore()
    expect(store.history).toHaveLength(0)
  })

  it('adds result to history', () => {
    const store = useBacktestStore()
    const result = makeResult()
    store.addToHistory(result)
    expect(store.history).toHaveLength(1)
    expect(store.history[0].id).toBe('test-id-1')
  })

  it('stores trades via historyWithTrades computed', () => {
    const store = useBacktestStore()
    const result = makeResult()
    store.addToHistory(result)
    const item = store.history.find(h => h.id === 'test-id-1')
    expect(item).toBeDefined()
    expect(item!.trades).toHaveLength(2)
  })

  it('extracts buy and close trades correctly', () => {
    const store = useBacktestStore()
    const result = makeResult()
    store.addToHistory(result)
    const item = store.history.find(h => h.id === 'test-id-1')
    expect(item!.trades[0].direction).toBe('long')
    expect(item!.trades[1].direction).toBe('close')
  })

  it('maps trade fields correctly', () => {
    const store = useBacktestStore()
    const result = makeResult()
    store.addToHistory(result)
    const item = store.history.find(h => h.id === 'test-id-1')
    expect(item!.trades[0].symbol).toBe('600000.SH')
    expect(item!.trades[0].price).toBe(10.5)
    expect(item!.trades[0].quantity).toBe(1000)
  })

  it('returns empty trades for non-existent id', () => {
    const store = useBacktestStore()
    store.addToHistory(makeResult())
    const item = store.history.find(h => h.id === 'nonexistent')
    expect(item).toBeUndefined()
  })

  it('clears history', () => {
    const store = useBacktestStore()
    store.addToHistory(makeResult())
    store.clearHistory()
    expect(store.history).toHaveLength(0)
  })

  it('handles result without trades', () => {
    const store = useBacktestStore()
    const result = makeResult({ trades: undefined })
    store.addToHistory(result)
    expect(store.history).toHaveLength(1)
    const item = store.history.find(h => h.id === 'test-id-1')
    expect(item!.trades).toHaveLength(0)
  })

  it('handles multiple results', () => {
    const store = useBacktestStore()
    store.addToHistory(makeResult({ id: 'r1' }))
    store.addToHistory(makeResult({ id: 'r2', strategy: 'mean_reversion' }))
    expect(store.history).toHaveLength(2)
    const r1 = store.history.find(h => h.id === 'r1')
    const r2 = store.history.find(h => h.id === 'r2')
    expect(r1!.trades).toHaveLength(2)
    expect(r2!.trades).toHaveLength(2)
  })

  it('preserves history item fields', () => {
    const store = useBacktestStore()
    store.addToHistory(makeResult())
    const item = store.history[0]
    expect(item.total_return).toBe(0.15)
    expect(item.sharpe_ratio).toBe(1.5)
    expect(item.max_drawdown).toBe(-0.08)
    expect(item.strategy).toBe('momentum')
    expect(item.stock_pool).toEqual(['600000.SH'])
  })

  it('deduplicates history items by id', () => {
    const store = useBacktestStore()
    const result = makeResult()
    store.addToHistory(result)
    store.addToHistory(result)
    expect(store.history).toHaveLength(1)
  })
})
