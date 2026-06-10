import type { Trade } from '@/types/api'

/**
 * CR-28 (ODR-012): pairTrades was inlined inside TradeTable.vue's
 * computed property — 90 lines of FIFO matching, PnL, and partial
 * close logic that was completely untested. Extracted here as a
 * pure function so the FIFO algorithm can be unit-tested without
 * mounting the component (Vue's reactivity layer would otherwise
 * add noise to the assertion path).
 *
 * The contract:
 *   - Event-based trades (one row per buy/sell) are paired into
 *     round-trip PairedTrade records.
 *   - FIFO matching: a 'close' trade consumes from the earliest
 *     open position of the same symbol.
 *   - 'long' and 'short' opens are kept in the same FIFO queue;
 *     a 'close' inverts the PnL sign for short positions.
 *   - Partial closes work: a close of 50 against an open of 100
 *     leaves 50 in the open position, consumed by the next close.
 *   - Open positions that are never closed appear in the output
 *     with exit_date/exit_price = null.
 *   - A 'close' with no matching open position is recorded as a
 *     standalone 'close' row (entry_date = '-').
 *   - The output is sorted by entry_date descending.
 */

export interface PairedTrade {
  id: string
  symbol: string
  direction: 'long' | 'short' | 'close'
  entry_date: string
  entry_price: number | null
  exit_date: string | null
  exit_price: number | null
  quantity: number
  pnl: number
  pnl_pct: number
  commission: number
}

function extractDate(ts: string | undefined): string {
  if (!ts) return '-'
  return ts.split('T')[0]
}

interface OpenPosition {
  id: string
  date: string
  price: number
  quantity: number
  commission: number
  direction: 'long' | 'short'
}

export function pairTrades(raw: Trade[] | undefined | null): PairedTrade[] {
  if (!raw?.length) return []

  const result: PairedTrade[] = []
  const openPositions: Record<string, OpenPosition[]> = {}

  for (const t of raw) {
    const sym = t.symbol
    const qty = Math.abs(t.quantity || 0)
    const price = t.price ?? 0
    const date = extractDate(t.timestamp)
    const commission =
      (t.commission || 0) + (t.transfer_fee || 0) + (t.stamp_tax || 0)

    if (t.direction === 'long' || t.direction === 'short') {
      if (!openPositions[sym]) openPositions[sym] = []
      openPositions[sym].push({
        id: t.id,
        date,
        price,
        quantity: qty,
        commission,
        direction: t.direction as 'long' | 'short',
      })
    } else if (t.direction === 'close') {
      let remainingQty = qty
      while (remainingQty > 0.0001 && openPositions[sym]?.length) {
        const open = openPositions[sym][0]
        const matchedQty = Math.min(remainingQty, open.quantity)

        const pnl =
          (price - open.price) * matchedQty * (open.direction === 'long' ? 1 : -1) -
          open.commission * (matchedQty / open.quantity) -
          commission * (matchedQty / qty)
        const pnlPct = open.price > 0 ? pnl / (open.price * matchedQty) : 0

        result.push({
          id: `${open.id}_${t.id}`,
          symbol: sym,
          direction: open.direction,
          entry_date: open.date,
          entry_price: open.price,
          exit_date: date,
          exit_price: price,
          quantity: Math.round(matchedQty),
          pnl,
          pnl_pct: pnlPct,
          commission: open.commission + commission,
        })

        remainingQty -= matchedQty
        open.quantity -= matchedQty
        if (open.quantity <= 0.0001) {
          openPositions[sym].shift()
        }
      }

      if (remainingQty > 0.0001) {
        result.push({
          id: t.id,
          symbol: sym,
          direction: 'close',
          entry_date: '-',
          entry_price: null,
          exit_date: date,
          exit_price: price,
          quantity: Math.round(remainingQty),
          pnl: 0,
          pnl_pct: 0,
          commission,
        })
      }
    }
  }

  for (const sym of Object.keys(openPositions)) {
    for (const open of openPositions[sym]) {
      result.push({
        id: open.id,
        symbol: sym,
        direction: open.direction,
        entry_date: open.date,
        entry_price: open.price,
        exit_date: null,
        exit_price: null,
        quantity: Math.round(open.quantity),
        pnl: 0,
        pnl_pct: 0,
        commission: open.commission,
      })
    }
  }

  return result.sort((a, b) => b.entry_date.localeCompare(a.entry_date))
}
