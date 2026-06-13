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
      <n-space class="result-actions" align="center" justify="space-between">
        <n-space align="center">
          <n-tag :type="result.id ? 'success' : 'default'" size="small">
            {{ result.id ? `ID: ${result.id}` : '未持久化' }}
          </n-tag>
          <n-text depth="3" style="font-size: 12px">
            <n-checkbox v-model:checked="compareSelected" :disabled="!result.id">
              加入对比
            </n-checkbox>
          </n-text>
        </n-space>
        <n-space>
          <n-button
            size="small"
            ghost
            :disabled="!result.id || exporting"
            :loading="exporting"
            @click="onExportHtml"
          >
            <n-icon><DownloadOutline /></n-icon>
            导出 HTML
          </n-button>
          <n-button
            size="small"
            type="primary"
            ghost
            :disabled="compareIds.length < 2"
            @click="goCompare"
          >
            <n-icon><GitCompareOutline /></n-icon>
            对比所选 ({{ compareIds.length }})
          </n-button>
        </n-space>
      </n-space>
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
import { useRoute, useRouter } from 'vue-router'
import { NEmpty, NAlert, NButton, NButtonGroup, NCheckbox, NSpace, NTag, NText, NIcon, useMessage } from 'naive-ui'
import { DownloadOutline, GitCompareOutline } from '@vicons/ionicons5'
import { useBacktestStore } from '@/stores/backtest'
import { runBacktest as apiRunBacktest, getBacktestReport, getOHLCV, exportHtml, type OHLCVAPIResponse, type OHLCVDataPoint } from '@/api/backtest'
import { getStrategies } from '@/api/strategy'
import { fmtPercent, fmtNumber } from '@/utils/format'
import { loadCompareIds, saveCompareIds } from '@/constants/backtest'
import type { BacktestResult, PortfolioPoint, Trade, Strategy } from '@/types/api'
import BacktestForm from '@/components/backtest/BacktestForm.vue'
import MetricsCards from '@/components/backtest/MetricsCards.vue'
import EquityChart from '@/components/backtest/EquityChart.vue'
import TradeTable from '@/components/backtest/TradeTable.vue'
import DetailMetrics from '@/components/backtest/DetailMetrics.vue'
import BacktestHistory from '@/components/backtest/BacktestHistory.vue'
import BacktestProgress from '@/components/backtest/BacktestProgress.vue'
import { useAsyncBacktest } from '@/composables/useAsyncBacktest'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const backtestStore = useBacktestStore()

const loading = ref(false)
const btRunning = ref(false)
const loadError = ref('')
const showTrades = ref(true)
const result = shallowRef<BacktestResult | null>(null)
const asyncMode = ref(true)
const exporting = ref(false)

// P2-2 (ODR-027): multi-select backtest IDs that participate in the
// "compare" page. Persisted in localStorage via constants/backtest.ts
// so the selection survives route changes / page reloads.
const compareIds = ref<string[]>(loadCompareIds())
const compareSelected = computed({
  get: () => !!(result.value?.id && compareIds.value.includes(result.value.id)),
  set: (on: boolean) => {
    const id = result.value?.id
    if (!id) return
    if (on) {
      if (!compareIds.value.includes(id)) compareIds.value = [...compareIds.value, id]
    } else {
      compareIds.value = compareIds.value.filter(x => x !== id)
    }
    saveCompareIds(compareIds.value)
  },
})
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

// CR-45 (ODR-012): strategiesCache is intentionally typed `string[]`
// because BacktestForm.vue consumes labels, not full Strategy objects
// (it only needs a `<n-select>` option list). The previous code stored
// `s.name || s.id || s.description` directly in a string[] — if ALL
// three were undefined the result was `undefined` in a `string[]`,
// which TypeScript couldn't catch (the type widens to `string |
// undefined` only under strictNullChecks + noUncheckedIndexedAccess).
// Normalise to '' here so the array is strictly `string[]`, and
// drop empties so the dropdown doesn't show a blank option.
const strategiesCache = ref<string[]>([])

function strategyLabel(s: Strategy): string {
  return s.name ?? s.id ?? s.description ?? ''
}
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

  try {
    const res: OHLCVAPIResponse = await getOHLCV(symbol, r.start_date, r.end_date)

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
      return
    } else {
      console.warn('[fetchStockPrices] OHLCV API returned empty data')
    }
  } catch (e: unknown) {
    console.warn('[fetchStockPrices] OHLCV API failed:', e instanceof Error ? e.message : String(e))
  }

  // Fallback: Extract price points from trades
  if (r.trades?.length) {
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
    strategiesCache.value = (res.strategies || [])
      .map(strategyLabel)
      .filter((label): label is string => label !== '')
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
    if (!res || !res.id) {
      throw new Error('回测返回结果无效：缺少ID')
    }
    result.value = res
    triggerRef(result)
    backtestStore.addToHistory(res)
    fromQuickRun.value = false
    fetchStockPrices()
  } catch (e: unknown) {
    message.error('回测失败: ' + (e instanceof Error ? e.message : String(e)))
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
    initial_capital: form.initialCapital,
    commission_rate: form.commissionRate,
    slippage_rate: form.slippageRate,
  })
}

async function loadReport(id: string) {
  loading.value = true
  loadError.value = ''
  try {
    const res = await getBacktestReport(id)
    if (!res) {
      throw new Error('报告数据为空')
    }
    if (!res.id) {
      res.id = id
    }
    result.value = res
    triggerRef(result)
    fromQuickRun.value = false
    showTrades.value = false

    backtestStore.addToHistory(res)

    fetchStockPrices()
  } catch (e: unknown) {
    loadError.value = '加载失败: ' + (e instanceof Error ? e.message : '未知错误')
  } finally {
    loading.value = false
  }
}

// P2-1 (ODR-027): trigger an HTML export for the current backtest.
// The browser save dialog appears via a synthetic `<a download>` click;
// the Blob is revoked once the user has had a chance to start the
// download (revoke-after-click is the safe pattern in Chromium).
async function onExportHtml() {
  const id = result.value?.id
  if (!id) {
    message.warning('当前结果尚未持久化，无法导出')
    return
  }
  exporting.value = true
  try {
    const { blob, filename } = await exportHtml(id)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename || `backtest-${id}.html`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    message.success(`已下载: ${a.download}`)
  } catch (e: unknown) {
    message.error('导出失败: ' + (e instanceof Error ? e.message : String(e)))
  } finally {
    exporting.value = false
  }
}

// P2-2 (ODR-027): jump to the multi-strategy comparison page with the
// current selection. We pass IDs through the query string so a deep
// link (e.g. shared URL) can recreate the same view.
function goCompare() {
  if (compareIds.value.length < 2) {
    message.warning('请至少选择 2 个回测进行对比')
    return
  }
  router.push({ path: '/backtest/compare', query: { ids: compareIds.value.join(',') } })
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
