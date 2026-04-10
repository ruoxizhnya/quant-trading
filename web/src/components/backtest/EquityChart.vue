<template>
  <n-card v-if="portfolioValues && portfolioValues.length > 0" class="chart-card">
    <template #header>
      <div class="chart-header">
        <span>净值 & 股价曲线</span>
        <n-space :size="8">
          <n-tag v-if="tradeMarkers.length > 0" :type="showTrades ? 'info' : 'default'" size="small" round :bordered="false" @click="$emit('toggle-trades')">
            {{ showTrades ? '📋 隐藏交易' : '📋 显示交易' }}
          </n-tag>
          <n-tag v-if="tradeMarkers.length > 0" type="success" size="small" round :bordered="false">
            {{ buyCount }} 买 / {{ sellCount }} 卖
          </n-tag>
        </n-space>
      </div>
    </template>

    <!-- Legend toggle -->
    <div v-if="stockPrices?.length" class="legend-toggle">
      <n-checkbox v-model:checked="showEquity" @update:checked="renderChart">净值</n-checkbox>
      <n-checkbox v-model:checked="showPrice" @update:checked="renderChart">股价</n-checkbox>
    </div>

    <div ref="chartContainer" class="chart-container">
      <canvas ref="canvasEl" height="350"></canvas>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { NCard, NSpace, NTag, NCheckbox } from 'naive-ui'
import { Chart, registerables, type TooltipItem } from 'chart.js'
import type { PortfolioPoint, Trade } from '@/types/api'

Chart.register(...registerables)

const MAX_CHART_POINTS = 150

interface PricePoint {
  date: string
  price: number
}

interface TradeMarker {
  date: string
  value: number
  direction: string
  symbol: string
  price: number | undefined
}

const props = defineProps<{
  portfolioValues: PortfolioPoint[]
  trades: Trade[]
  showTrades: boolean
  stockPrices?: PricePoint[] // New prop for stock price data
}>()

const emit = defineEmits<{
  'toggle-trades': []
}>()

const chartContainer = ref<HTMLDivElement | null>(null)
const canvasEl = ref<HTMLCanvasElement | null>(null)
const showEquity = ref(true)
const showPrice = ref(true)

let chartInstance: Chart | null = null
const tradeMarkers = ref<TradeMarker[]>([])
const buyCount = computed(() => tradeMarkers.value.filter(t => t.direction === 'long').length)
const sellCount = computed(() => tradeMarkers.value.filter(t => t.direction !== 'long').length)

function sampleData(raw: { date: string; value: number }[]) {
  if (raw.length <= MAX_CHART_POINTS) return raw
  const step = Math.ceil(raw.length / MAX_CHART_POINTS)
  const sampled: { date: string; value: number }[] = []
  for (let i = 0; i < raw.length; i += step) {
    sampled.push(raw[i])
  }
  const last = raw[raw.length - 1]
  if (!sampled.length || sampled[sampled.length - 1].date !== last.date) {
    sampled.push(last)
  }
  return sampled
}

function buildTradeMarkers(portfolioValues: PortfolioPoint[], trades: Trade[]): TradeMarker[] {
  if (!portfolioValues?.length || !trades.length) return []
  const pvMap = new Map<string, number>()
  portfolioValues.forEach((p) => {
    const d = (p.date || '').split('T')[0]
    if (d) pvMap.set(d, p.total_value || 0)
  })

  return trades.map(t => {
    // 根据交易方向选择正确的日期和价格字段
    let tradeDate = ''
    let tradePrice: number | undefined = undefined

    if (t.direction === 'close') {
      // 卖出交易：使用 timestamp 或 exit_date
      tradeDate = (t.timestamp || t.exit_date || '').split('T')[0]
      tradePrice = t.price ?? t.exit_price  // 后端的price字段存储执行价格
    } else {
      // 买入交易：使用 timestamp 或 entry_date
      tradeDate = (t.timestamp || t.entry_date || '').split('T')[0]
      tradePrice = t.price ?? t.entry_price
    }

    return {
      date: tradeDate,
      value: pvMap.get(tradeDate) || 0,
      direction: t.direction,
      symbol: t.symbol,
      price: tradePrice,
    }
  }).filter(m => m.date && m.value > 0)
}

async function renderChart() {
  if (!props.portfolioValues?.length) return
  if (!canvasEl.value) return

  try {
    destroyChart()

    const rawData = props.portfolioValues.map(p => ({
      date: (p.date || '').split('T')[0],
      value: Number(p.total_value) || 0,
    })).filter(d => d.date && d.value > 0)

    if (rawData.length === 0) return

    const data = sampleData(rawData)

    const ctx = canvasEl.value.getContext('2d')
    if (!ctx) return

    const markers = buildTradeMarkers(props.portfolioValues, props.trades)
  tradeMarkers.value = markers

  console.log('[EquityChart] Total trades:', props.trades.length)
  console.log('[EquityChart] Markers after filter:', markers.length)
  console.log('[EquityChart] Buy markers:', markers.filter(m => m.direction === 'long').length)
  console.log('[EquityChart] Sell markers:', markers.filter(m => m.direction === 'close').length)
  if (props.stockPrices?.length) {
    console.log('[EquityChart] Stock prices loaded:', props.stockPrices.length, 'points')
  } else {
    console.warn('[EquityChart] No stock prices data')
  }

    const datasets: any[] = []

    // Build date index for marker alignment
    const dateIndex = new Map<string, number>()
    data.forEach((d, i) => dateIndex.set(d.date, i))

    // 1. Equity line (left Y axis)
    if (showEquity.value) {
      datasets.push({
        type: 'line',
        label: '净值 (¥)',
        data: data.map(d => d.value),
        borderColor: '#58a6ff',
        backgroundColor: 'rgba(88,166,255,0.06)',
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        borderWidth: 2,
        yAxisID: 'yEquity',
        order: 2,
      })

      // Buy/sell markers on equity line
      if (markers.length > 0 && props.showTrades) {
        const buyData: { x: number; y: number }[] = []
        const sellData: { x: number; y: number }[] = []

        markers.forEach(m => {
          const idx = dateIndex.get(m.date)
          if (idx != null) {
            const pt = { x: idx, y: m.value }
            if (m.direction === 'long') buyData.push(pt)
            else sellData.push(pt)
          }
        })

        if (buyData.length > 0) {
          datasets.push({
            type: 'scatter',
            label: '买入',
            data: buyData,
            backgroundColor: '#3fb950',
            borderColor: '#3fb950',
            pointRadius: 6,
            pointHoverRadius: 8,
            pointStyle: 'triangle',
            yAxisID: 'yEquity',
            order: 1,
          })
        }
        if (sellData.length > 0) {
          datasets.push({
            type: 'scatter',
            label: '卖出',
            data: sellData,
            backgroundColor: '#f85149',
            borderColor: '#f85149',
            pointRadius: 6,
            pointHoverRadius: 8,
            pointStyle: 'crossRot',
            yAxisID: 'yEquity',
            order: 1,
          })
        }
      }
    }

    // 2. Stock price line (right Y axis)
    if (props.stockPrices?.length && showPrice.value) {
      // Align price data with equity dates
      const priceMap = new Map<string, number>()
      props.stockPrices.forEach(p => {
        const d = (p.date || '').split('T')[0]
        if (d) priceMap.set(d, p.price)
      })

      const alignedPrices = data.map(d => ({ x: d.date, y: priceMap.get(d.date) ?? null }))
      const validPrices = alignedPrices.filter((_, i) => alignedPrices[i].y != null)

      // Buy/sell markers on price line
      let buyPriceData: { x: number; y: number }[] = []
      let sellPriceData: { x: number; y: number }[] = []

      if (markers.length > 0 && props.showTrades) {
        markers.forEach(m => {
          const idx = dateIndex.get(m.date)
          const price = m.price ?? priceMap.get(m.date)
          if (idx != null && price != null) {
            const pt = { x: idx, y: price }
            if (m.direction === 'long') buyPriceData.push(pt)
            else sellPriceData.push(pt)
          }
        })
      }

      datasets.push({
        type: 'line',
        label: '股价 (¥)',
        data: alignedPrices.map(d => d.y),
        borderColor: '#f0883e',
        backgroundColor: 'rgba(240,136,62,0.06)',
        borderDash: [5, 3],
        tension: 0.3,
        pointRadius: 0,
        borderWidth: 1.5,
        yAxisID: 'yPrice',
        spanGaps: true,
        order: 2,
      })

      if (buyPriceData.length > 0) {
        datasets.push({
          type: 'scatter',
          label: '买入价',
          data: buyPriceData,
          backgroundColor: '#238636',
          borderColor: '#238636',
          pointRadius: 7,
          pointHoverRadius: 9,
          pointStyle: 'triangle',
          yAxisID: 'yPrice',
          order: 1,
        })
      }
      if (sellPriceData.length > 0) {
        datasets.push({
          type: 'scatter',
          label: '卖出价',
          data: sellPriceData,
          backgroundColor: '#da3633',
          borderColor: '#da3633',
          pointRadius: 7,
          pointHoverRadius: 9,
          pointStyle: 'crossRot',
          yAxisID: 'yPrice',
          order: 1,
        })
      }
    }

    chartInstance = new Chart(ctx, {
      type: 'line',
      data: { labels: data.map(d => d.date), datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: { duration: 500 },
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: {
            display: true,
            position: 'top',
            labels: { color: '#8b949e', boxWidth: 12, padding: 12, font: { size: 11 }, usePointStyle: true },
          },
          tooltip: {
            callbacks: {
              label(ctx: TooltipItem<'line' | 'scatter'>) {
                if (ctx.dataset.type === 'scatter') {
                  const marker = markers[ctx.dataIndex]
                  if (!marker) return ''
                  const action = marker.direction === 'long' ? '买入' : '卖出'
                  const priceStr = marker.price ? ` @ ¥${marker.price.toFixed(2)}` : ''
                  return `${action} ${marker.symbol}${priceStr}`
                }
                const val = ctx.parsed.y ?? 0
                const prefix = ctx.dataset.yAxisID === 'yPrice' ? '股价' : ctx.dataset.label
                return `${prefix}: ¥${(val / 1000).toFixed(1)}K`
              },
            },
          },
        },
        scales: {
          x: {
            grid: { color: '#21262d' },
            ticks: { color: '#484f58', maxTicksLimit: 15, font: { size: 11 } },
          },
          yEquity: {
            type: 'linear',
            display: showEquity.value,
            position: 'left',
            grid: { color: '#21262d' },
            ticks: { color: '#58a6ff', font: { size: 11 }, callback(v) { return `¥${(Number(v)/1000).toFixed(0)}K` } },
            title: { display: true, text: '净值', color: '#58a6ff', font: { size: 11 } },
          },
          yPrice: {
            type: 'linear',
            display: showPrice.value && !!props.stockPrices?.length,
            position: 'right',
            grid: { drawOnChartArea: false },
            ticks: { color: '#f0883e', font: { size: 11 }, callback(v) { return `¥${Number(v).toFixed(1)}` } },
            title: { display: true, text: '股价', color: '#f0883e', font: { size: 11 } },
          },
        },
      },
    })
  } catch (e) {
    console.warn('[DualChart] Render error:', e)
  }
}

function destroyChart() {
  if (chartInstance) {
    chartInstance.destroy()
    chartInstance = null
  }
}

watch(
  () => [props.portfolioValues, props.trades, props.stockPrices] as const,
  async () => {
    await nextTick()
    renderChart()
  },
  { immediate: true, deep: true },
)

onBeforeUnmount(() => {
  destroyChart()
})
</script>

<style scoped>
.chart-card { min-height: 400px; }
.chart-container {
  position: relative;
  height: 350px;
}
.chart-container canvas {
  width: 100% !important;
  height: 100% !important;
}
.chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
.legend-toggle {
  display: flex;
  gap: 16px;
  padding: 4px 0 8px;
  font-size: 13px;
}
</style>
