<template>
  <n-card title="回测历史" class="history-section">
    <template #header-extra>
      <n-space align="center" :size="8">
        <n-tag round :bordered="false" size="small">{{ history.length }} 条记录</n-tag>
        <n-button quaternary size="tiny" @click="$emit('clear')">清除</n-button>
      </n-space>
    </template>

    <n-collapse v-if="validHistory.length > 0" accordion>
      <n-collapse-item v-for="(item, i) in validHistory" :key="item.id || i" :name="item.id">
        <template #header>
          <div class="history-header">
            <div class="history-main">
              <span class="history-title">{{ itemTitle(item) }}</span>
              <span class="history-desc">{{ itemDesc(item) }}</span>
            </div>
            <n-space :size="4" align="center">
              <n-tag 
                :type="(item.total_return ?? 0) >= 0 ? 'success' : 'error'" 
                size="small" 
                round 
                :bordered="false"
              >
                {{ fmtPercent(item.total_return) }}
              </n-tag>
              <n-button size="tiny" type="primary" quaternary @click.stop="$emit('view-report', item.id)">
                查看报告
              </n-button>
            </n-space>
          </div>
        </template>

        <!-- Trade Sub-list -->
        <div v-if="itemTrades[item.id]?.length > 0" class="trade-sublist">
          <n-table :single-line="true" size="small" :bordered="false" striped>
            <thead>
              <tr>
                <th>方向</th>
                <th>股票</th>
                <th>交易时间</th>
                <th>成交价</th>
                <th>数量</th>
                <th>手续费</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(trade, ti) in itemTrades[item.id]!" :key="ti" :class="trade.direction === 'long' ? 'trade-long' : trade.direction === 'short' ? 'trade-short' : ''">
                <td>
                  <n-tag
                    size="small" round :bordered="false"
                    :type="trade.direction === 'long' ? 'success' : trade.direction === 'short' ? 'error' : 'warning'"
                  >
                    {{ directionLabel(trade.direction) }}
                  </n-tag>
                </td>
                <td><code>{{ trade.symbol }}</code></td>
                <td>{{ formatDate(trade.timestamp ?? trade.entry_date) }}</td>
                <td>{{ formatPrice(trade.price ?? trade.entry_price ?? null) }}</td>
                <td>{{ (trade.quantity ?? 0).toLocaleString() }}</td>
                <td>
                  <span v-if="trade.commission != null" class="cost-info">
                    {{ trade.commission.toFixed(2) }}
                  </span>
                  <span v-else>-</span>
                </td>
              </tr>
            </tbody>
          </n-table>
        </div>
        <n-empty v-else description="无交易记录" size="small"></n-empty>
      </n-collapse-item>
    </n-collapse>

    <n-empty v-else description="暂无回测历史"></n-empty>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NCollapse, NCollapseItem, NTable, NTag, NSpace, NButton, NEmpty } from 'naive-ui'
import { fmtPercent } from '@/utils/format'

// Use a minimal interface that matches what the store actually provides
interface HistoryEntry {
  id: string
  strategy?: string
  stock_pool?: string[]
  start_date?: string
  end_date?: string
  total_return: number
  sharpe_ratio: number
  max_drawdown: number
  created_at?: string
  trades?: TradeInfo[]
}

interface TradeInfo {
  id: string
  symbol: string
  direction: string
  timestamp: string       // backend field name (required)
  price: number | null    // backend field name (execution price, required)
  quantity: number | null
  commission?: number
  pnl?: number
  pnl_pct?: number

  // Display aliases (computed from above)
  entry_date?: string
  exit_date?: string | null
  entry_price?: number | null
  exit_price?: number | null
}

const props = defineProps<{
  history: HistoryEntry[]
}>()

const emit = defineEmits<{
  clear: []
  'view-report': [id: string]
}>()

const validHistory = computed(() =>
  (props.history || []).filter((item: HistoryEntry) => item && item.id)
)

// Build trade lookup map by result ID
const itemTrades = computed<Record<string, TradeInfo[]>>(() => {
  const map: Record<string, TradeInfo[]> = {}
  for (const item of props.history || []) {
    if (item && item.id && item.trades?.length) {
      map[item.id] = item.trades
      console.log(`[BacktestHistory] Item ${item.id}: ${item.trades.length} trades, symbols:`, [...new Set(item.trades.map(t => t.symbol))])
    }
  }
  return map
})

function itemTitle(item: HistoryEntry): string {
  const stocks = Array.isArray(item.stock_pool) ? item.stock_pool.join(', ') : (item.stock_pool || '')
  return `${stocks} · ${item.strategy || ''}`
}

function itemDesc(item: HistoryEntry): string {
  const ret = fmtPercent(item.total_return)
  const sharpe = (item.sharpe_ratio != null && !isNaN(item.sharpe_ratio)) ? item.sharpe_ratio.toFixed(2) : '-'
  const dd = (item.max_drawdown != null && !isNaN(item.max_drawdown)) ? (item.max_drawdown * 100).toFixed(2) + '%' : '-'
  let desc = `夏普: ${sharpe} | 最大回撤: ${dd}`
  if (item.created_at) {
    try {
      const d = new Date(item.created_at)
      if (!isNaN(d.getTime())) {
        desc += ` | ${d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}`
      }
    } catch {}
  }
  return desc
}

function directionLabel(dir: string): string {
  switch (dir) {
    case 'long': return '买入'
    case 'short': return '做空'
    case 'close': return '平仓'
    default: return dir
  }
}

function formatDate(d: string | null): string {
  if (!d) return '-'
  try {
    const date = new Date(d)
    return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
  } catch {
    return d.slice(0, 10)
  }
}

function formatPrice(p: number | null): string {
  if (p == null) return '-'
  return p.toFixed(2)
}
</script>

<style scoped>
.history-section { margin-top: 8px; }

.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  gap: 12px;
}

.history-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.history-title {
  font-weight: 500;
  font-size: 13px;
}

.history-desc {
  font-size: 12px;
  color: #8b949e;
}

.trade-sublist {
  padding: 4px 0 0 16px;
}

.trade-long td { background: rgba(63, 185, 80, 0.04); }
.trade-short td { background: rgba(248, 81, 73, 0.04); }

.pnl-pos { color: #3fb950; font-weight: 500; }
.pnl-neg { color: #f85149; font-weight: 500; }
.pnl-pct { color: #8b949e; font-size: 11px; margin-left: 4px; }

:deep(.n-collapse-item__header-main) {
  width: 100%;
}
</style>
