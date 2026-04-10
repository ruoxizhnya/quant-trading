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
      :loading="loading"
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

    <div v-if="result" class="results-section">
      <MetricsCards :metrics="resultMetrics" />

      <EquityChart
        :portfolio-values="result.portfolio_values || []"
        :trades="safeTrades"
        :show-trades="showTrades"
        @toggle-trades="showTrades = !showTrades"
      />

      <TradeTable v-if="showTrades && safeTrades.length > 0" :trades="safeTrades" />

      <DetailMetrics :metrics="safeMetrics" />
    </div>

    <n-empty v-else-if="!loading && !fromQuickRun && !loadError" description="配置参数后运行回测" class="empty-state"></n-empty>

    <n-alert v-if="loadError" type="warning" title="报告加载失败" closable @close="loadError = ''">
      {{ loadError }}
      <template #action>
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
import { ref, reactive, computed, onMounted, nextTick, shallowRef, triggerRef } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty, NAlert, NButton, useMessage } from 'naive-ui'
import { useBacktestStore } from '@/stores/backtest'
import { runBacktest as apiRunBacktest, getBacktestReport } from '@/api/backtest'
import { getStrategies } from '@/api/strategy'
import { fmtPercent } from '@/utils/format'
import type { BacktestResult, PortfolioPoint, Trade } from '@/types/api'
import BacktestForm from '@/components/backtest/BacktestForm.vue'
import MetricsCards from '@/components/backtest/MetricsCards.vue'
import EquityChart from '@/components/backtest/EquityChart.vue'
import TradeTable from '@/components/backtest/TradeTable.vue'
import DetailMetrics from '@/components/backtest/DetailMetrics.vue'
import BacktestHistory from '@/components/backtest/BacktestHistory.vue'

const route = useRoute()
const message = useMessage()
const backtestStore = useBacktestStore()

const loading = ref(false)
const btRunning = ref(false)
const loadError = ref('')
const showTrades = ref(false)
const result = shallowRef<BacktestResult | null>(null)

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

function formatNum(v: any, digits: number): string {
  if (v == null || isNaN(Number(v))) return '-'
  return Number(v).toFixed(digits)
}

const safeTrades = computed<Trade[]>(() => result.value?.trades || [])
const safeMetrics = computed(() => result.value?.metrics || null)

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
</style>
