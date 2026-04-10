<template>
  <div class="bt-page">
    <BacktestForm
      :strategy="form.strategy"
      :stock-pool="form.stockPool"
      :start-date="form.startDate"
      :end-date="form.endDate"
      :initial-capital="form.initialCapital"
      :commission-rate="form.commissionRate"
      :slippage-rate="form.slippageRate"
      :loading="isRunning"
      :strategies="strategiesCache"
      @update:strategy="form.strategy = $event"
      @update:stock-pool="form.stockPool = $event"
      @update:start-date="form.startDate = $event"
      @update:end-date="form.endDate = $event"
      @update:initial-capital="form.initialCapital = $event"
      @update:commission-rate="form.commissionRate = $event"
      @update:slippage-rate="form.slippageRate = $event"
      @submit="runBacktest"
    />

    <div class="mode-toggle">
      <n-button-group size="tiny">
        <n-button :type="asyncMode ? 'default' : 'primary'" size="tiny" @click="asyncMode = false">同步</n-button>
        <n-button :type="asyncMode ? 'primary' : 'default'" size="tiny" @click="asyncMode = true">异步</n-button>
      </n-button-group>
    </div>

    <BacktestProgress
      v-if="asyncMode && asyncState.status !== 'idle'"
      :visible="true"
      :status="asyncState.status"
      :progress="asyncState.progress"
      :error="asyncState.error"
      :cancellable="asyncState.status === 'running' || asyncState.status === 'pending'"
      @cancel="asyncBacktest.cancel()"
    />

    <div v-if="result" class="results-section">
      <MetricsCards :metrics="resultMetrics" />
      <EquityChart
        :portfolio-values="result.portfolio_values || []"
        :trades="safeTrades"
        :show-trades="showTrades"
        :stock-prices="stockPrices"
        @toggle-trades="showTrades = !showTrades"
      />
      <TradeTable v-if="showTrades && safeTrades.length > 0" :trades="safeTrades" />
      <DetailMetrics :metrics="safeMetrics" />
    </div>

    <n-empty v-else-if="!isRunning && !fromQuickRun && !loadError && asyncState.status === 'idle'" description="配置参数后运行回测" class="empty-state"></n-empty>

    <n-alert v-if="loadError" type="warning" title="报告加载失败" closable @close="loadError = ''">
      {{ loadError }}
      <template #icon>
        <n-button size="small" @click="loadError = ''; fromQuickRun = false">确定</n-button>
      </template>
    </n-alert>

    <BacktestHistory
      :history="backtestStore.history"
      @clear="backtestStore.clearHistory()"
      @view-report="loadReport"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, shallowRef, triggerRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty, NAlert, NButton, NButtonGroup, useMessage } from 'naive-ui'
import { useBacktestStore } from '@/stores/backtest'
import { runBacktest as apiRunBacktest, getBacktestReport, getOHLCV, type OHLCVAPIResponse, type OHLCVDataPoint } from '@/api/backtest'
import { getStrategies } from '@/api/strategy'
import { fmtPercent, fmtNumber } from '@/utils/format'
import type { BacktestResult, PortfolioPoint, Trade } from '@/types/api'
import BacktestForm from '@/components/backtest/BacktestForm.vue'
import MetricsCards from '@/components/backtest/MetricsCards.vue'
import EquityChart from '@/components/backtest/EquityChart.vue'
import TradeTable from '@/components/backtest/TradeTable.vue'
import DetailMetrics from '@/components/backtest/DetailMetrics.vue'
import BacktestHistory from '@/components/backtest/BacktestHistory.vue'
import BacktestProgress from '@/components/backtest/BacktestProgress.vue'
import { useAsyncBacktest } from '@/composables/useAsyncBacktest'

const route = useRoute()
const message = useMessage()
const backtestStore = useBacktestStore()

const loading = ref(false)
const btRunning = ref(false)
const loadError = ref('')
const showTrades = ref(true)
const result = shallowRef<BacktestResult | null>(null)
const asyncMode = ref(true)
interface StockPricePoint {
  date: string
  price: number
}

const stockPrices = ref<StockPricePoint[]>([])

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
const fromQuickRun = ref(!!route.query.id)

const asyncBacktest = useAsyncBacktest()
const asyncState = computed(() => asyncBacktest.state.value)

const isRunning = computed(() => btRunning.value || ['pending', 'running'].includes(asyncState.value.status))

const safeTrades = computed<Trade[]>(() => result.value?.trades || [])
const safeMetrics = computed(() => result.value?.metrics || null)

const resultMetrics = computed(() => {
  if (!result.value) return []
  const r = result.value
  return [
    { label: '总收益率', value: fmtPercent(r.total_return), cls: (r.total_return ?? 0) >= 0 ? 'positive' : 'negative' },
    { label: '年化收益', value: fmtPercent(r.annual_return), cls: (r.annual_return ?? 0) >= 0 ? 'positive' : 'negative' },
    { label: '夏普比率', value: fmtNumber(r.sharpe_ratio, 2), cls: '' },
    { label: '最大回撤', value: fmtPercent(r.max_drawdown), cls: 'negative' },
    { label: 'Calmar 比率', value: fmtNumber(r.calmar_ratio, 2), cls: '' },
  ]
})

watch(() => asyncState.value.result, (newResult) => {
  if (newResult) {
    result.value = newResult
    triggerRef(result)
    backtestStore.addToHistory(newResult)
    fromQuickRun.value = false
    fetchStockPrices()
  }
}, { immediate: true })

// Fetch OHLCV data for price chart when result changes
async function fetchStockPrices() {
  const r = result.value
  if (!r?.stock_pool?.length || !r.start_date || !r.end_date) {
    stockPrices.value = []
    return
  }
  const symbol = r.stock_pool[0]

  console.log('[fetchStockPrices] Fetching price data for', symbol, r.start_date, 'to', r.end_date)

  // Try OHLCV API first
  try {
    const res: OHLCVAPIResponse = await getOHLCV(symbol, r.start_date, r.end_date)
    console.log('[fetchStockPrices] OHLCV API response:', res)

    // Handle different response formats from backend
    let ohlcvData: OHLCVDataPoint[] = []
    if (res.ohlcv && Array.isArray(res.ohlcv)) {
      ohlcvData = res.ohlcv
    } else if (res.data && Array.isArray(res.data)) {
      ohlcvData = res.data
    }

    if (ohlcvData.length > 0) {
      stockPrices.value = ohlcvData
        .map((d: OHLCVDataPoint) => ({
          date: d.trade_date || d.date || '',
          price: Number(d.close) || 0,
        }))
        .filter((p: StockPricePoint) => p.date && p.price > 0)
      console.log('[fetchStockPrices] Loaded', stockPrices.value.length, 'price points from OHLCV API')
      return
    } else {
      console.warn('[fetchStockPrices] OHLCV API returned empty data')
    }
  } catch (e: unknown) {
    console.warn('[fetchStockPrices] OHLCV API failed:', e instanceof Error ? e.message : String(e))
  }

  // Fallback: Extract price points from trades
  if (r.trades?.length) {
    console.log('[fetchStockPrices] Falling back to trade extraction, trades count:', r.trades.length)
    const priceMap = new Map<string, number>()
    // Use trade execution prices as data points
    r.trades.forEach((t: Trade) => {
      if (t.timestamp && t.price != null) {
        const dateKey = t.timestamp.split('T')[0]
        if (dateKey && !priceMap.has(dateKey)) {
          priceMap.set(dateKey, t.price)
        }
      }
    })
    if (priceMap.size > 0) {
      stockPrices.value = Array.from(priceMap.entries())
        .map(([date, price]) => ({ date, price }))
        .sort((a: StockPricePoint, b: StockPricePoint) => a.date.localeCompare(b.date))
      console.log('[fetchStockPrices] Extracted', stockPrices.value.length, 'price points from trades')
      return
    }
  }

  console.warn('[fetchStockPrices] No price data available')
  stockPrices.value = []
}

onMounted(async () => {
  backtestStore.loadHistory()
  backtestStore.loadHistoryFromDB()
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
  if (isRunning.value) return
  if (!form.stockPool) { message.warning('请输入股票代码'); return }
  loadError.value = ''
  showTrades.value = false
  result.value = null
  triggerRef(result)

  if (asyncMode.value) {
    runAsyncBacktest()
  } else {
    await runSyncBacktest()
  }
}

async function runSyncBacktest() {
  btRunning.value = true
  loading.value = true
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
    fetchStockPrices()
  } catch (e: any) {
    message.error('回测失败: ' + (e.message || e))
  } finally {
    loading.value = false
    btRunning.value = false
  }
}

function runAsyncBacktest() {
  loading.value = true
  asyncBacktest.submit({
    strategy_id: form.strategy,
    universe: form.stockPool,
    start_date: form.startDate,
    end_date: form.endDate,
  })
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

    // Sync trades to store for history display
    backtestStore.addToHistory(res)

    fetchStockPrices()
  } catch (e: any) {
    loadError.value = '加载失败: ' + (e.message || '未知错误')
  } finally {
    loading.value = false
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
.results-section { display: flex; flex-direction: column; gap: 16px; }
.empty-state { padding: 60px 0; }
.mode-toggle { display: flex; justify-content: flex-end; }
</style>
