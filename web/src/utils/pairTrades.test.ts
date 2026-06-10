import { describe, it, expect } from 'vitest'
import { pairTrades, type PairedTrade } from '@/utils/pairTrades'
import type { Trade } from '@/types/api'

// CR-28 (ODR-012): pairTrades was 90 lines of FIFO matching, partial
// close, and PnL logic with zero unit-test coverage. The previous
// "integration" was mounting the component and visually inspecting a
// table — which meant a regression in FIFO ordering or PnL math would
// reach production. These tests pin the contract: FIFO matching,
// partial closes, short positions, leftover opens, and standalone
// closes for unmatched positions.

function makeTrade(overrides: Partial<Trade> = {}): Trade {
  return {
    id: 't-1',
    symbol: '600000.SH',
    direction: 'long',
    price: 10,
    quantity: 100,
    commission: 0,
    transfer_fee: 0,
    stamp_tax: 0,
    timestamp: '2024-01-15T00:00:00Z',
    ...overrides,
  } as Trade
}

describe('pairTrades — empty and defensive inputs', () => {
  it('returns [] for undefined', () => {
    expect(pairTrades(undefined)).toEqual([])
  })
  it('returns [] for null', () => {
    expect(pairTrades(null)).toEqual([])
  })
  it('returns [] for empty array', () => {
    expect(pairTrades([])).toEqual([])
  })
})

describe('pairTrades — single long + close round-trip', () => {
  it('pairs a long open with a same-size close and computes PnL', () => {
    const result = pairTrades([
      makeTrade({ id: 'open-1', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'close-1', direction: 'close', price: 12, quantity: -100, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(1)
    const t = result[0]
    expect(t.direction).toBe('long')
    expect(t.entry_date).toBe('2024-01-02')
    expect(t.exit_date).toBe('2024-01-10')
    expect(t.entry_price).toBe(10)
    expect(t.exit_price).toBe(12)
    expect(t.quantity).toBe(100)
    // pnl = (12 - 10) * 100 = 200
    expect(t.pnl).toBeCloseTo(200, 6)
    // pnl_pct = 200 / (10 * 100) = 0.20 = 20%
    expect(t.pnl_pct).toBeCloseTo(0.20, 6)
  })

  it('inverts PnL for a short position (price drops after open)', () => {
    const result = pairTrades([
      makeTrade({ id: 'open-s', direction: 'short', price: 20, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'close-s', direction: 'close', price: 15, quantity: -100, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(1)
    expect(result[0].direction).toBe('short')
    // short pnl = (15 - 20) * 100 * -1 = 500
    expect(result[0].pnl).toBeCloseTo(500, 6)
    expect(result[0].pnl_pct).toBeCloseTo(0.25, 6)
  })

  it('inverts PnL for a short position with price rise (loser)', () => {
    const result = pairTrades([
      makeTrade({ id: 'open-s', direction: 'short', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'close-s', direction: 'close', price: 12, quantity: -100, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(1)
    // short pnl = (12 - 10) * 100 * -1 = -200
    expect(result[0].pnl).toBeCloseTo(-200, 6)
  })
})

describe('pairTrades — FIFO matching', () => {
  it('matches the earliest open first when two opens exist', () => {
    const result = pairTrades([
      makeTrade({ id: 'o1', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'o2', direction: 'long', price: 12, quantity: 100, timestamp: '2024-01-05T00:00:00Z' }),
      makeTrade({ id: 'c1', direction: 'close', price: 15, quantity: -100, timestamp: '2024-01-15T00:00:00Z' }),
    ])
    // c1 (qty 100) closes o1 first; o2 remains open.
    expect(result.filter(r => r.exit_date !== null)).toHaveLength(1)
    const closed = result.find(r => r.exit_date !== null)!
    expect(closed.entry_price).toBe(10) // FIFO must close o1 (price 10) first
    expect(closed.id).toBe('o1_c1')
    // o2 should still appear as an open position
    const opens = result.filter(r => r.exit_date === null)
    expect(opens).toHaveLength(1)
    expect(opens[0].id).toBe('o2')
    expect(opens[0].entry_price).toBe(12)
  })

  it('a single close of 100 against two opens of 50 each produces two paired trades', () => {
    const result = pairTrades([
      makeTrade({ id: 'a', direction: 'long', price: 10, quantity: 50, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'b', direction: 'long', price: 11, quantity: 50, timestamp: '2024-01-05T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: 13, quantity: -100, timestamp: '2024-01-15T00:00:00Z' }),
    ])
    expect(result.filter(r => r.exit_date !== null)).toHaveLength(2)
    // First close consumes a (50 @ 10), second consumes b (50 @ 11)
    const closed = result.filter(r => r.exit_date !== null).sort((x, y) => x.entry_price! - y.entry_price!)
    expect(closed[0].entry_price).toBe(10)
    expect(closed[0].quantity).toBe(50)
    expect(closed[1].entry_price).toBe(11)
    expect(closed[1].quantity).toBe(50)
  })
})

describe('pairTrades — partial close', () => {
  it('close of 50 against open of 100 leaves 50 in open position', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c1', direction: 'close', price: 12, quantity: -50, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    // One closed (50 shares), one still open (50 shares remaining)
    const closed = result.find(r => r.exit_date !== null)!
    expect(closed.quantity).toBe(50)
    expect(closed.pnl).toBeCloseTo(100, 6) // (12-10) * 50
    const open = result.find(r => r.exit_date === null)!
    expect(open.quantity).toBe(50)
    expect(open.exit_date).toBeNull()
  })

  it('two partial closes against the same open position consume it FIFO', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c1', direction: 'close', price: 11, quantity: -40, timestamp: '2024-01-10T00:00:00Z' }),
      makeTrade({ id: 'c2', direction: 'close', price: 12, quantity: -60, timestamp: '2024-01-15T00:00:00Z' }),
    ])
    expect(result.filter(r => r.exit_date !== null)).toHaveLength(2)
    expect(result.filter(r => r.exit_date === null)).toHaveLength(0)
    const first = result.find(r => r.exit_date === '2024-01-10')!
    expect(first.quantity).toBe(40)
    expect(first.pnl).toBeCloseTo(40, 6) // (11-10)*40
    const second = result.find(r => r.exit_date === '2024-01-15')!
    expect(second.quantity).toBe(60)
    expect(second.pnl).toBeCloseTo(120, 6) // (12-10)*60
  })
})

describe('pairTrades — unmatched closes', () => {
  it('a close with no open position becomes a standalone close row', () => {
    const result = pairTrades([
      makeTrade({ id: 'orphan', direction: 'close', price: 12, quantity: 50, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(1)
    expect(result[0].direction).toBe('close')
    expect(result[0].entry_date).toBe('-')
    expect(result[0].entry_price).toBeNull()
    expect(result[0].pnl).toBe(0)
    expect(result[0].quantity).toBe(50)
  })
})

describe('pairTrades — multi-symbol isolation', () => {
  it('open positions of one symbol do not match closes of another', () => {
    const result = pairTrades([
      makeTrade({ id: 'A-open', symbol: '600000.SH', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'B-open', symbol: '600001.SH', direction: 'long', price: 20, quantity: 200, timestamp: '2024-01-05T00:00:00Z' }),
      makeTrade({ id: 'B-close', symbol: '600001.SH', direction: 'close', price: 25, quantity: -200, timestamp: '2024-01-15T00:00:00Z' }),
    ])
    // B is fully closed; A is still open
    const a = result.find(r => r.symbol === '600000.SH')!
    expect(a.exit_date).toBeNull()
    expect(a.quantity).toBe(100)
    const b = result.find(r => r.symbol === '600001.SH' && r.exit_date !== null)!
    expect(b.entry_price).toBe(20)
    expect(b.exit_price).toBe(25)
    expect(b.pnl).toBeCloseTo(1000, 6)
  })
})

describe('pairTrades — fees', () => {
  it('subtracts pro-rated commission and stamp_tax from PnL', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, commission: 5, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: 12, quantity: -100, commission: 3, stamp_tax: 1, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(1)
    // gross pnl = (12-10)*100 = 200
    // opening commission pro-rated: 5 * (100/100) = 5
    // closing commission+tax: (3+0+1) * (100/100) = 4
    // net pnl = 200 - 5 - 4 = 191
    expect(result[0].pnl).toBeCloseTo(191, 6)
    expect(result[0].commission).toBeCloseTo(5 + 4, 6) // 9
  })

  it('partial close pro-rates the opening commission', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, commission: 10, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: 12, quantity: -50, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result).toHaveLength(2)
    const closed = result.find(r => r.exit_date !== null)!
    // opening commission pro-rated: 10 * (50/100) = 5
    // gross pnl = (12-10)*50 = 100
    // net pnl = 100 - 5 = 95
    expect(closed.pnl).toBeCloseTo(95, 6)
  })
})

describe('pairTrades — output ordering', () => {
  it('sorts paired trades by entry_date descending', () => {
    const result = pairTrades([
      makeTrade({ id: 'o1', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c1', direction: 'close', price: 11, quantity: -100, timestamp: '2024-01-05T00:00:00Z' }),
      makeTrade({ id: 'o2', direction: 'long', price: 20, quantity: 100, timestamp: '2024-02-15T00:00:00Z' }),
      makeTrade({ id: 'c2', direction: 'close', price: 22, quantity: -100, timestamp: '2024-02-20T00:00:00Z' }),
    ])
    expect(result).toHaveLength(2)
    expect(result[0].entry_date).toBe('2024-02-15')
    expect(result[1].entry_date).toBe('2024-01-02')
  })
})

describe('pairTrades — defensive numeric inputs', () => {
  it('treats missing price as 0', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: undefined as any, quantity: -100, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    expect(result[0].exit_price).toBe(0)
    // pnl = (0 - 10) * 100 = -1000
    expect(result[0].pnl).toBeCloseTo(-1000, 6)
  })

  it('treats missing quantity as 0 (close is a no-op)', () => {
    const result = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: 12, quantity: undefined as any, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    // qty=0 → remainingQty=0 → no paired trade emitted
    expect(result.filter(r => r.exit_date !== null)).toHaveLength(0)
    // The original open remains
    expect(result).toHaveLength(1)
    expect(result[0].exit_date).toBeNull()
  })

  it('emits a PairedTrade object that conforms to the public type', () => {
    const result: PairedTrade[] = pairTrades([
      makeTrade({ id: 'o', direction: 'long', price: 10, quantity: 100, timestamp: '2024-01-02T00:00:00Z' }),
      makeTrade({ id: 'c', direction: 'close', price: 12, quantity: -100, timestamp: '2024-01-10T00:00:00Z' }),
    ])
    // Compile-time guarantee; runtime shape check.
    const t = result[0]
    expect(t).toEqual(expect.objectContaining({
      id: expect.any(String),
      symbol: expect.any(String),
      direction: expect.stringMatching(/^(long|short|close)$/),
      entry_date: expect.any(String),
      entry_price: expect.any(Number),
      exit_date: expect.any(String),
      exit_price: expect.any(Number),
      quantity: expect.any(Number),
      pnl: expect.any(Number),
      pnl_pct: expect.any(Number),
      commission: expect.any(Number),
    }))
  })
})
