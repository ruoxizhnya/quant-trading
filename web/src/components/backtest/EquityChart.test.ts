import { describe, it, expect } from 'vitest'
import { buildTradeMarkers, type TradeMarker } from '@/utils/tradeMarkers'
import type { PortfolioPoint, Trade } from '@/types/api'

function makeTrade(overrides: Partial<Trade> = {}): Trade {
  return {
    id: 't-1',
    symbol: '600000.SH',
    direction: 'long',
    price: 10.5,
    quantity: 1000,
    commission: 3.15,
    timestamp: '2024-01-15T00:00:00Z',
    pending_qty: 0,
    filled_qty: 1000,
    ...overrides,
  } as Trade
}

function makePV(date: string, value: number): PortfolioPoint {
  return { date, total_value: value, cash: 0, positions: 0 }
}

describe('buildTradeMarkers', () => {
  const portfolioValues: PortfolioPoint[] = [
    makePV('2024-01-10', 1000000),
    makePV('2024-01-15', 1010000),
    makePV('2024-02-20', 1050000),
    makePV('2024-06-28', 1150000),
  ]

  it('returns empty array for empty portfolio values', () => {
    const result = buildTradeMarkers([], [makeTrade()])
    expect(result).toHaveLength(0)
  })

  it('returns empty array for empty trades', () => {
    const result = buildTradeMarkers(portfolioValues, [])
    expect(result).toHaveLength(0)
  })

  it('creates marker for buy trade using timestamp', () => {
    const trade = makeTrade({ direction: 'long', timestamp: '2024-01-15T00:00:00Z', price: 10.5 })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(1)
    expect(result[0].date).toBe('2024-01-15')
    expect(result[0].direction).toBe('long')
    expect(result[0].price).toBe(10.5)
    expect(result[0].value).toBe(1010000)
  })

  it('creates marker for sell (close) trade using timestamp', () => {
    const trade = makeTrade({ direction: 'close', timestamp: '2024-06-28T00:00:00Z', price: 12.0 })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(1)
    expect(result[0].date).toBe('2024-06-28')
    expect(result[0].direction).toBe('close')
    expect(result[0].price).toBe(12.0)
  })

  it('filters out markers with empty date', () => {
    const trade = makeTrade({ direction: 'long', timestamp: '', price: 10.5 })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(0)
  })

  it('filters out markers with zero portfolio value', () => {
    const trade = makeTrade({ direction: 'long', timestamp: '2024-03-01T00:00:00Z', price: 10.5 })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(0)
  })

  it('handles both buy and sell trades together', () => {
    const trades = [
      makeTrade({ id: 't-1', direction: 'long', timestamp: '2024-01-15T00:00:00Z', price: 10.5 }),
      makeTrade({ id: 't-2', direction: 'close', timestamp: '2024-06-28T00:00:00Z', price: 12.0 }),
    ]
    const result = buildTradeMarkers(portfolioValues, trades)
    expect(result).toHaveLength(2)
    expect(result[0].direction).toBe('long')
    expect(result[1].direction).toBe('close')
  })

  it('uses exit_date fallback for close trade when timestamp is empty', () => {
    const trade = makeTrade({
      direction: 'close',
      timestamp: '',
      exit_date: '2024-06-28',
      price: 12.0,
    })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(1)
    expect(result[0].date).toBe('2024-06-28')
  })

  it('uses entry_date fallback for long trade when timestamp is empty', () => {
    const trade = makeTrade({
      direction: 'long',
      timestamp: '',
      entry_date: '2024-01-15',
      price: 10.5,
    })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(1)
    expect(result[0].date).toBe('2024-01-15')
  })

  it('handles short direction correctly', () => {
    const trade = makeTrade({ direction: 'short', timestamp: '2024-02-20T00:00:00Z', price: 11.0 })
    const result = buildTradeMarkers(portfolioValues, [trade])
    expect(result).toHaveLength(1)
    expect(result[0].direction).toBe('short')
  })

  it('handles multiple symbols', () => {
    const trades = [
      makeTrade({ id: 't-1', symbol: '600000.SH', direction: 'long', timestamp: '2024-01-15T00:00:00Z', price: 10.5 }),
      makeTrade({ id: 't-2', symbol: '600519.SH', direction: 'long', timestamp: '2024-01-15T00:00:00Z', price: 50.0 }),
    ]
    const result = buildTradeMarkers(portfolioValues, trades)
    expect(result).toHaveLength(2)
    expect(result[0].symbol).toBe('600000.SH')
    expect(result[1].symbol).toBe('600519.SH')
  })
})
