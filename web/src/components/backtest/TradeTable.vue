<template>
  <n-card v-if="pairedTrades.length > 0" title="交易记录" class="trades-card">
    <n-data-table
      :columns="columns"
      :data="pairedTrades"
      :pagination="{ pageSize: 10 }"
      size="small"
      striped
      bordered
    ></n-data-table>
  </n-card>
</template>

<script setup lang="ts">
import { computed, h } from 'vue'
import { NCard, NDataTable, NTag } from 'naive-ui'
import { fmtPercent, fmtNumber } from '@/utils/format'
import type { Trade } from '@/types/api'

const props = defineProps<{ trades: Trade[] }>()

interface PairedTrade {
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

function directionTag(direction: string) {
  if (direction === 'long') return { type: 'success' as const, text: '多' }
  if (direction === 'short') return { type: 'error' as const, text: '空' }
  return { type: 'info' as const, text: '平' }
}

function extractDate(ts: string | undefined): string {
  if (!ts) return '-'
  return ts.split('T')[0]
}

/**
 * Pair raw trades into round-trip trades.
 * Backend returns event-based trades (one row per buy/sell).
 * We need to pair buy (long/short) with sell (close) for display.
 */
const pairedTrades = computed((): PairedTrade[] => {
  const raw = props.trades
  if (!raw?.length) return []

  const result: PairedTrade[] = []
  // Map: symbol -> stack of open positions (FIFO)
  const openPositions: Record<string, Array<{ id: string; date: string; price: number; quantity: number; commission: number; direction: 'long' | 'short' }>> = {}

  for (const t of raw) {
    const sym = t.symbol
    const qty = Math.abs(t.quantity || 0)
    const price = t.price ?? 0
    const date = extractDate(t.timestamp)
    const commission = (t.commission || 0) + (t.transfer_fee || 0) + (t.stamp_tax || 0)

    if (t.direction === 'long' || t.direction === 'short') {
      // Opening trade
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
      // Closing trade - pair with earliest open position (FIFO)
      let remainingQty = qty
      while (remainingQty > 0.0001 && openPositions[sym]?.length) {
        const open = openPositions[sym][0]
        const matchedQty = Math.min(remainingQty, open.quantity)

        const pnl = (price - open.price) * matchedQty * (open.direction === 'long' ? 1 : -1)
          - open.commission * (matchedQty / open.quantity)
          - commission * (matchedQty / qty)
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

      // If no matching open position, treat as standalone close (should not happen normally)
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

  // Add remaining open positions (not yet closed)
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

  // Sort by entry date descending
  return result.sort((a, b) => b.entry_date.localeCompare(a.entry_date))
})

const columns = [
  { title: '方向', key: 'direction', width: 70,
    render(row: PairedTrade) {
      const t = directionTag(row.direction)
      return h(NTag, { type: t.type, size: 'small', round: true, bordered: false }, () => t.text)
    },
  },
  { title: '股票', key: 'symbol', width: 110 },
  { title: '入场日期', key: 'entry_date', width: 110 },
  { title: '入场价', key: 'entry_price', width: 85, render: (r: PairedTrade) => fmtNumber(r.entry_price, 2) },
  { title: '出场日期', key: 'exit_date', width: 110, render: (r: PairedTrade) => r.exit_date ?? '-' },
  { title: '出场价', key: 'exit_price', width: 85, render: (r: PairedTrade) => fmtNumber(r.exit_price, 2) },
  { title: '数量', key: 'quantity', width: 65 },
  { title: 'PnL', key: 'pnl', width: 90,
    render: (r: PairedTrade) => h('span', { class: r.pnl >= 0 ? 'pnl-pos' : 'pnl-neg' }, () => fmtPercent(r.pnl_pct)),
  },
]
</script>

<style scoped>
.trades-card .n-data-table { --n-font-size: 13px; }
.pnl-pos { color: var(--q-success); }
.pnl-neg { color: var(--q-danger); }
</style>
