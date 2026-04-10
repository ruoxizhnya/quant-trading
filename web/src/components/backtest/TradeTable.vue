<template>
  <n-card v-if="trades.length > 0" title="交易记录" class="trades-card">
    <n-data-table
      :columns="columns"
      :data="trades"
      :pagination="{ pageSize: 10 }"
      size="small"
      striped
      bordered
    ></n-data-table>
  </n-card>
</template>

<script setup lang="ts">
import { h } from 'vue'
import { NCard, NDataTable, NTag } from 'naive-ui'
import { fmtPercent } from '@/utils/format'
import type { Trade } from '@/types/api'

defineProps<{ trades: Trade[] }>()

function directionTag(direction: string) {
  if (direction === 'long') return { type: 'success' as const, text: '多' }
  if (direction === 'short') return { type: 'error' as const, text: '空' }
  return { type: 'info' as const, text: '平' }
}

const columns = [
  { title: '方向', key: 'direction', width: 70,
    render(row: Trade) {
      const t = directionTag(row.direction)
      return h(NTag, { type: t.type, size: 'small', round: true, bordered: false }, () => t.text)
    },
  },
  { title: '股票', key: 'symbol', width: 110 },
  { title: '入场日期', key: 'entry_date', width: 110 },
  { title: '入场价', key: 'entry_price', width: 85, render: (r: Trade) => r.entry_price?.toFixed(2) },
  { title: '出场日期', key: 'exit_date', width: 110 },
  { title: '出场价', key: 'exit_price', width: 85, render: (r: Trade) => r.exit_price?.toFixed(2) },
  { title: '数量', key: 'quantity', width: 65 },
  { title: 'PnL', key: 'pnl', width: 90,
    render: (r: Trade) => h('span', { class: (r.pnl ?? 0) >= 0 ? 'pnl-pos' : 'pnl-neg' }, () => fmtPercent(r.pnl ?? 0)),
  },
]
</script>

<style scoped>
.trades-card .n-data-table { --n-font-size: 13px; }
.pnl-pos { color: var(--q-success); }
.pnl-neg { color: var(--q-danger); }
</style>
