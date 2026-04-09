<template>
  <div class="bt-page">
    <!-- Form Section -->
    <n-card title="回测引擎">
      <div class="form-grid">
        <div class="form-item">
          <label class="form-label">策略</label>
          <n-select v-model:value="form.strategy" :options="strategyOptions" size="small" />
        </div>
        <div class="form-item form-item-wide">
          <label class="form-label">股票池</label>
          <n-input v-model:value="form.stockPool" placeholder="600000.SH,600036.SH" size="small" />
        </div>
        <div class="form-item">
          <label class="form-label">开始日期</label>
          <n-date-picker v-model:formatted-value="form.startDate" type="date" value-format="yyyy-MM-dd" size="small" />
        </div>
        <div class="form-item">
          <label class="form-label">结束日期</label>
          <n-date-picker v-model:formatted-value="form.endDate" type="date" value-format="yyyy-MM-dd" size="small" />
        </div>
        <div class="form-item">
          <label class="form-label">初始资金</label>
          <n-input-number v-model:value="form.initialCapital" :min="10000" :step="100000" size="small" />
        </div>
        <div class="form-item">
          <label class="form-label">手续费率</label>
          <n-input-number v-model:value="form.commissionRate" :step="0.0001" :precision="4" size="small" />
        </div>
        <div class="form-item">
          <label class="form-label">滑点率</label>
          <n-input-number v-model:value="form.slippageRate" :step="0.0001" :precision="4" size="small" />
        </div>
        <div class="form-item form-item-btn">
          <n-button type="primary" :loading="loading" block @click="runBacktest">运行回测</n-button>
        </div>
      </div>
    </n-card>

    <!-- Results Section -->
    <div v-if="result" class="results-section">
      <!-- Metrics Cards -->
      <div class="metrics-grid">
        <div v-for="m in resultMetrics" :key="m.label" class="metric-box">
          <div class="metric-label">{{ m.label }}</div>
          <div class="metric-val" :class="m.cls">{{ m.value }}</div>
        </div>
      </div>

      <!-- Equity Curve with Trade Markers -->
      <n-card v-if="chartData.length > 0" class="chart-card">
        <template #header>
          <div class="chart-header">
            <span>净值曲线</span>
            <n-space :size="8">
              <n-tag v-if="safeTrades.length > 0" :type="showTrades ? 'info' : 'default'" size="small" round :bordered="false" @click="showTrades = !showTrades">
                {{ showTrades ? '📋 隐藏交易' : '📋 显示交易' }}
              </n-tag>
              <n-tag v-if="tradeMarkers.length > 0" type="success" size="small" round :bordered="false">
                {{ tradeBuyCount }} 买 / {{ tradeSellCount }} 卖
              </n-tag>
            </n-space>
          </div>
        </template>
        <canvas ref="eqCanvasRef" height="300"></canvas>
      </n-card>

      <!-- Trades Table (toggleable) -->
      <n-card v-if="showTrades && safeTrades.length > 0" title="交易记录" class="trades-card">
        <n-data-table
          :columns="tradeColumns"
          :data="safeTrades"
          :pagination="{ pageSize: 10 }"
          size="small"
          striped
          bordered
        ></n-data-table>
      </n-card>

      <!-- Detail Metrics -->
      <n-card v-if="safeMetrics && Object.keys(safeMetrics).length > 0" title="详细指标" class="detail-metrics">
        <div class="metrics-detail-grid">
          <div v-for="(v, k) in safeMetrics" :key="k" class="metric-box-sm">
            <span class="sm-label">{{ k }}</span>
            <span class="sm-val">{{ formatMetric(v) }}</span>
          </div>
        </div>
      </n-card>
    </div>

    <n-empty v-else-if="!loading && !fromQuickRun && !loadError" description="配置参数后运行回测" class="empty-state"></n-empty>

    <n-alert v-if="loadError" type="warning" title="报告加载失败" closable @close="loadError = ''">
      {{ loadError }}
      <template #action>
        <n-button size="small" @click="loadError = ''; fromQuickRun = false">确定</n-button>
      </template>
    </n-alert>

    <!-- History Section -->
    <n-card title="回测历史" class="history-section">
      <template #extra>
        <n-space align="center" :size="8">
          <n-tag round :bordered="false" size="small">{{ backtestStore.history.length }} 条记录</n-tag>
          <n-button quaternary size="tiny" @click="backtestStore.clearHistory()">清除</n-button>
        </n-space>
      </template>
      <n-list v-if="validHistory.length > 0" bordered>
        <n-list-item v-for="(item, i) in validHistory" :key="item.id || i">
          <template #prefix>
            <n-tag :type="(item.total_return ?? 0) >= 0 ? 'success' : 'error'" size="small" round :bordered="false">
              {{ (item.total_return ?? 0) >= 0 ? '多' : '空' }}
            </n-tag>
          </template>
          <n-thing :title="itemTitle(item)" :description="itemDesc(item)"></n-thing>
          <template #suffix>
            <n-button quaternary size="tiny" type="primary" @click="loadReport(item.id)">查看报告</n-button>
          </template>
        </n-list-item>
      </n-list>
      <n-empty v-else description="暂无回测历史"></n-empty>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, shallowRef, triggerRef, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NCard, NForm, NFormItem, NSelect, NInput, NInputNumber,
  NDatePicker, NButton, NGrid, NGi, NEmpty, NAlert,
  NDataTable, NList, NListItem, NThing, NTag, NSpace, useMessage,
} from 'naive-ui'
import { Chart, registerables } from 'chart.js'
import { useBacktestStore } from '@/stores/backtest'
import { runBacktest as apiRunBacktest, getBacktestReport } from '@/api/backtest'
import { getStrategies } from '@/api/strategy'
import { fmtPercent } from '@/utils/format'
import type { BacktestResult, PortfolioPoint, Trade } from '@/types/api'

Chart.register(...registerables)

const MAX_CHART_POINTS = 120

const route = useRoute()
const router = useRouter()
const message = useMessage()
const backtestStore = useBacktestStore()

const validHistory = computed(() =>
  (backtestStore.history || []).filter((item: any) => item && item.id)
)

const loading = ref(false)
const btRunning = ref(false)
const loadError = ref('')
const showTrades = ref(false)
const result = shallowRef<BacktestResult | null>(null)
const eqCanvasRef = ref<HTMLCanvasElement>()
let eqChart: Chart | null = null

const form = reactive({
  strategy: (route.query.strategy as string) || 'momentum',
  stockPool: (route.query.stock as string) || '600000.SH',
  startDate: (route.query.start as string) || '2024-01-01',
  endDate: (route.query.end as string) || '2024-06-30',
  initialCapital: 1000000,
  commissionRate: 0.0003,
  slippageRate: 0.0001,
})

const strategiesCache = ref<string[]>([])
const strategyOptions = computed(() =>
  strategiesCache.value.map(s => ({ label: s, value: s }))
)

function directionTag(direction: string) {
  if (direction === 'long') return { type: 'success' as const, text: '多' }
  if (direction === 'short') return { type: 'error' as const, text: '空' }
  return { type: 'info' as const, text: '平' }
}

const tradeColumns = [
  { title: '方向', key: 'direction', width: 70,
    render(row: any) {
      const t = directionTag(row.direction)
      return h(NTag, { type: t.type, size: 'small', round: true, bordered: false }, () => t.text)
    },
  },
  { title: '股票', key: 'symbol', width: 110 },
  { title: '入场日期', key: 'entry_date', width: 110 },
  { title: '入场价', key: 'entry_price', width: 85, render: (r: any) => r.entry_price?.toFixed(2) },
  { title: '出场日期', key: 'exit_date', width: 110 },
  { title: '出场价', key: 'exit_price', width: 85, render: (r: any) => r.exit_price?.toFixed(2) },
  { title: '数量', key: 'quantity', width: 65 },
  { title: 'PnL', key: 'pnl', width: 90,
    render: (r: any) => h('span', { class: r.pnl >= 0 ? 'pnl-pos' : 'pnl-neg' }, () => fmtPercent(r.pnl)),
  },
]

const chartData = ref<{ date: string; value: number }[]>([])

const safeTrades = computed<Trade[]>(() => result.value?.trades || [])
const safeMetrics = computed(() => result.value?.metrics || null)

const tradeMarkers = computed(() => {
  if (!result.value?.portfolio_values || !safeTrades.value.length) return []
  const pvMap = new Map<string, number>()
  result.value.portfolio_values.forEach((p: PortfolioPoint) => {
    const d = (p.date || '').split('T')[0]
    if (d) pvMap.set(d, p.total_value || 0)
  })
  return safeTrades.value.map(t => ({
    date: (t.entry_date || '').split('T')[0],
    value: pvMap.get((t.entry_date || '').split('T')[0]) || 0,
    direction: t.direction,
    symbol: t.symbol,
    price: t.entry_price,
  })).filter(m => m.date && m.value > 0)
})

const tradeBuyCount = computed(() => tradeMarkers.value.filter(t => t.direction === 'long').length)
const tradeSellCount = computed(() => tradeMarkers.value.filter(t => t.direction !== 'long').length)

const resultMetrics = computed(() => {
  if (!result.value) return []
  const r = result.value
  return [
    { label: '总收益率', value: fmtPercent(r.total_return), cls: (r.total_return ?? 0) >= 0 ? 'positive' : 'negative' },
    { label: '年化收益', value: fmtPercent(r.annual_return), cls: (r.annual_return ?? 0) >= 0 ? 'positive' : 'negative' },
    { label: '夏普比率', value: formatNum(r.sharpe_ratio, 2), cls: '' },
    { label: '最大回撤', value: fmtPercent(r.max_drawdown), cls: 'negative' },
    { label: 'Calmar 比率', value: formatNum(r.calmar_ratio, 2), cls: '' },
  ]
})

const fromQuickRun = ref(!!route.query.id)

function formatNum(v: any, digits: number): string {
  if (v == null || isNaN(Number(v))) return '-'
  return Number(v).toFixed(digits)
}

function formatMetric(v: any): string {
  if (v == null || isNaN(Number(v))) return String(v ?? '-')
  return Math.abs(Number(v)) > 1 ? Number(v).toFixed(4) : (Number(v) * 100).toFixed(2) + '%'
}

function itemTitle(item: BacktestResult): string {
  return `${Array.isArray(item.stock_pool) ? item.stock_pool.join(',') : (item.stock_pool || '')} · ${item.strategy || ''}`
}

function itemDesc(item: BacktestResult): string {
  const ret = fmtPercent(item.total_return)
  const sharpe = (item.sharpe_ratio != null && !isNaN(item.sharpe_ratio)) ? item.sharpe_ratio.toFixed(2) : '-'
  const dd = (item.max_drawdown != null && !isNaN(item.max_drawdown)) ? (item.max_drawdown * 100).toFixed(2) + '%' : '-'
  return `收益: ${ret} | 夏普: ${sharpe} | 最大回撤: ${dd}`
}

function sampleData(data: { date: string; value: number }[], maxPoints: number): { date: string; value: number }[] {
  if (data.length <= maxPoints) return data
  const step = Math.ceil(data.length / maxPoints)
  const sampled: { date: string; value: number }[] = []
  for (let i = 0; i < data.length; i += step) {
    sampled.push(data[i])
  }
  const last = data[data.length - 1]
  if (!sampled.length || sampled[sampled.length - 1].date !== last.date) {
    sampled.push(last)
  }
  return sampled
}

onMounted(async () => {
  backtestStore.loadHistory()
  try {
    const res = await getStrategies()
    strategiesCache.value = (res.strategies || []).map((s: any) => s.name || s.id || s.description)
  } catch {}
  if (route.query.id) {
    await nextTick()
    await loadReport(route.query.id as string)
  }
})

async function runBacktest() {
  if (btRunning.value) return
  if (!form.stockPool) { message.warning('请输入股票代码'); return }
  btRunning.value = true
  loading.value = true
  loadError.value = ''
  showTrades.value = false
  result.value = null
  triggerRef(result)
  try {
    const res = await apiRunBacktest({
      strategy: form.strategy,
      stock_pool: form.stockPool.split(',').map(s => s.trim()),
      start_date: form.startDate,
      end_date: form.endDate,
      initial_capital: form.initialCapital,
      commission_rate: form.commissionRate,
      slippage_rate: form.slippageRate,
    })
    result.value = res
    triggerRef(result)
    backtestStore.addToHistory(res)
    fromQuickRun.value = false
    await nextTick()
    renderChart()
  } catch (e: any) {
    message.error('回测失败: ' + (e.message || e))
  } finally {
    loading.value = false
    btRunning.value = false
  }
}

async function loadReport(id: string) {
  loading.value = true
  loadError.value = ''
  try {
    const res = await getBacktestReport(id)
    result.value = res
    triggerRef(result)
    fromQuickRun.value = false
    showTrades.value = false
    await nextTick()
    renderChart()
  } catch (e: any) {
    const status = e?.status || e?.response?.status
    if (status === 404) {
      loadError.value = `回测报告已过期（服务重启后内存数据丢失）。请重新运行回测。`
    } else {
      loadError.value = '加载失败: ' + (e.message || '未知错误')
    }
  } finally {
    loading.value = false
  }
}

async function renderChart() {
  if (!result.value?.portfolio_values?.length) return
  try {
    if (eqChart) { eqChart.destroy(); eqChart = null }

    const pv = result.value.portfolio_values
    if (!pv || !Array.isArray(pv) || pv.length === 0) return

    const rawData = pv.map(p => ({
      date: (p.date || '').split('T')[0],
      value: Number(p.total_value) || 0,
    })).filter(d => d.date && d.value > 0)

    if (rawData.length === 0) return

    const data = sampleData(rawData, MAX_CHART_POINTS)
    chartData.value = data

    await nextTick()

    if (!eqCanvasRef.value) {
      console.warn('Canvas element not found after nextTick')
      return
    }

    const ctx = eqCanvasRef.value.getContext('2d')
    if (!ctx) return

    const datasets: any[] = [{
      type: 'line' as const,
      label: '净值',
      data: data.map(d => d.value),
      borderColor: '#58a6ff',
      backgroundColor: 'rgba(88,166,255,0.08)',
      fill: true,
      tension: 0.3,
      pointRadius: 0,
      borderWidth: 2,
      order: 2,
    }]

    if (tradeMarkers.value.length > 0) {
      const buyData: { x: number; y: number }[] = []
      const sellData: { x: number; y: number }[] = []
      const dateIndex = new Map<string, number>()
      data.forEach((d, i) => dateIndex.set(d.date, i))

      tradeMarkers.value.forEach(m => {
        const idx = dateIndex.get(m.date)
        if (idx != null) {
          const pt = { x: idx, y: m.value }
          if (m.direction === 'long') buyData.push(pt)
          else sellData.push(pt)
        }
      })

      if (buyData.length > 0) {
        datasets.push({
          type: 'scatter' as const,
          label: '买入',
          data: buyData,
          backgroundColor: '#3fb950',
          borderColor: '#3fb950',
          pointRadius: 5,
          pointHoverRadius: 7,
          pointStyle: 'triangle',
          order: 1,
        })
      }
      if (sellData.length > 0) {
        datasets.push({
          type: 'scatter' as const,
          label: '卖出',
          data: sellData,
          backgroundColor: '#f85149',
          borderColor: '#f85149',
          pointRadius: 5,
          pointHoverRadius: 7,
          pointStyle: 'crossRot',
          order: 1,
        })
      }
    }

    eqChart = new Chart(ctx, {
      type: 'line',
      data: { labels: data.map(d => d.date), datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: { duration: 500 },
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: true, position: 'top', labels: { color: '#8b949e', boxWidth: 12, padding: 12, font: { size: 11 } } },
          tooltip: {
            callbacks: {
              label(ctx: any) {
                if (ctx.dataset.type === 'scatter') {
                  const marker = tradeMarkers.value[ctx.dataIndex]
                  return `${marker.direction === 'long' ? '买入' : '卖出'} ${marker.symbol} @ ${marker.price?.toFixed(2)}`
                }
                return `${ctx.dataset.label}: ¥${(ctx.parsed.y / 1000).toFixed(0)}K`
              }
            }
          },
        },
        scales: {
          x: { grid: { color: '#21262d' }, ticks: { color: '#484f58', maxTicksLimit: 10, font: { size: 11 } } },
          y: { grid: { color: '#21262d' }, ticks: { color: '#484f58', font: { size: 11 } } },
        },
      },
    })
  } catch (e) {
    console.warn('Chart render error:', e)
  }
}
</script>

<style scoped>
.bt-page {
  max-width: 1400px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* ── Form Grid (Responsive) ──────────────────── */
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px 16px;
  align-items: end;
}

.form-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.form-item-wide {
  grid-column: span 2;
  min-width: 200px;
}

.form-item-btn {
  grid-column: span 1;
}

.form-label {
  font-size: 11px;
  color: var(--q-text3);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  white-space: nowrap;
}

/* ── Metrics Grid (Responsive) ──────────────── */
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 12px;
}

.metric-box {
  background: var(--q-surface2);
  border-radius: var(--q-radius-sm);
  padding: 16px 14px;
  transition: transform var(--q-transition);
}

.metric-box:hover {
  transform: translateY(-1px);
}

.metric-label {
  font-size: 11px;
  color: var(--q-text3);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.metric-val {
  font-size: 20px;
  font-weight: 700;
  color: var(--q-primary);
  margin-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.metric-val.positive { color: var(--q-success); }
.metric-val.negative { color: var(--q-danger); }

/* ── Chart Card ─────────────────────────────── */
.chart-card {
  min-height: 340px;
}

.chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

/* ── Trades Table ───────────────────────────── */
.trades-card .n-data-table { --n-font-size: 13px; }

/* ── Detail Metrics ─────────────────────────── */
.detail-metrics { margin-top: 4px; }

.metrics-detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 4px 24px;
}

.metric-box-sm {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 7px 0;
  border-bottom: 1px solid var(--q-border-light);
}
.metric-box-sm:last-child { border-bottom: none; }

.sm-label { color: var(--q-text3); font-size: 12px; flex-shrink: 0; }
.sm-val { color: var(--q-text); font-weight: 600; font-size: 12px; margin-left: 12px; word-break: break-all; }

/* ── Empty State ────────────────────────────── */
.empty-state { padding: 60px 0; }

/* ── History Section ────────────────────────── */
.history-section { margin-top: 8px; }

.pnl-pos { color: var(--q-success); }
.pnl-neg { color: var(--q-danger); }

/* ── Responsive Breakpoints ─────────────────── */
@media (max-width: 1200px) {
  .metrics-grid { grid-template-columns: repeat(3, 1fr); }
  .form-item-wide { grid-column: span 1; }
}

@media (max-width: 900px) {
  .metrics-grid { grid-template-columns: repeat(2, 1fr); }
  .form-grid { grid-template-columns: repeat(2, 1fr); }
  .metric-val { font-size: 17px; }
}

@media (max-width: 640px) {
  .form-grid { grid-template-columns: 1fr; }
  .metrics-grid { grid-template-columns: repeat(2, 1fr); gap: 8px; }
  .metric-box { padding: 12px 10px; }
  .metric-val { font-size: 16px; }
  .metrics-detail-grid { grid-template-columns: 1fr; }
  .chart-header { flex-direction: column; align-items: flex-start; gap: 6px; }
}
</style>
