<template>
  <div class="dashboard-page">
    <MarketMetrics
      v-model:selected-date="selectedDate"
      v-model:selected-index="selectedIndex"
      :loading="marketLoading"
      :metrics="marketMetrics"
      @refresh="fetchMarketData"
    />

    <QuickBacktest
      v-model:strategy="quickForm.strategy"
      v-model:stock="quickForm.stock"
      v-model:start-date="quickForm.startDate"
      v-model:end-date="quickForm.endDate"
      :running="quickRunning"
      :strategies="strategiesCache"
      :quick-result="quickResult"
      @run="runQuickBacktest"
      @view-report="(id: string) => router.push({ path: '/backtest', query: { id } })"
    />

    <NavTiles />

    <ConsoleHistory
      :history="backtestStore.history"
      @clear="backtestStore.clearHistory()"
      @view-report="(id: string) => router.push({ path: '/backtest', query: { id } })"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useBacktestStore } from '@/stores/backtest'
import { getMarketIndex } from '@/api/market'
import { getStrategies } from '@/api/strategy'
import { runBacktest as apiRunBacktest } from '@/api/backtest'
import type { Strategy, MarketIndex } from '@/types/api'
import MarketMetrics from '@/components/dashboard/MarketMetrics.vue'
import QuickBacktest from '@/components/dashboard/QuickBacktest.vue'
import NavTiles from '@/components/dashboard/NavTiles.vue'
import ConsoleHistory from '@/components/dashboard/ConsoleHistory.vue'

const router = useRouter()
const message = useMessage()
const backtestStore = useBacktestStore()

const selectedDate = ref(new Date().toISOString().split('T')[0])
const selectedIndex = ref('000001.SH')
const marketLoading = ref(false)
const marketMetrics = ref<MarketIndex | Record<string, unknown>>({})
const strategiesCache = ref<string[]>([])
const quickRunning = ref(false)
const quickResult = ref<QuickResult | null>(null)

interface QuickResult {
  id: string
  total_return?: number
  sharpe_ratio?: number
}

const quickForm = reactive({
  strategy: 'momentum',
  stock: '600000.SH',
  startDate: '2024-01-01',
  endDate: '2024-03-31',
})

onMounted(async () => {
  backtestStore.loadHistory()
  backtestStore.loadHistoryFromDB()
  try {
    const res = await getStrategies()
    strategiesCache.value = (res.strategies || []).map((s: Strategy) => (s.name || s.id || s.description))
  } catch {}
  fetchMarketData()
})

async function fetchMarketData() {
  marketLoading.value = true
  try {
    const res = await getMarketIndex(selectedIndex.value, selectedDate.value)
    if (Array.isArray(res.indices) && res.indices.length > 0) {
      const latest = res.indices[res.indices.length - 1]
      marketMetrics.value = latest
    }
  } catch (e) {
    console.warn('市场数据获取失败:', e)
  } finally {
    marketLoading.value = false
  }
}

async function runQuickBacktest() {
  if (!quickForm.stock) return
  quickRunning.value = true
  quickResult.value = null
  try {
    const res = await apiRunBacktest({
      strategy: quickForm.strategy,
      stock_pool: [quickForm.stock],
      start_date: quickForm.startDate,
      end_date: quickForm.endDate,
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.0001,
    })
    if (!res || !res.id) {
      throw new Error('回测返回结果无效：缺少ID')
    }
    quickResult.value = { id: res.id, total_return: res.total_return, sharpe_ratio: res.sharpe_ratio }
    backtestStore.addToHistory(res)
  } catch (e: unknown) {
    message.error('快速回测失败: ' + (e instanceof Error ? e.message : String(e)))
  } finally {
    quickRunning.value = false
  }
}
</script>

<style scoped>
.dashboard-page {
  max-width: 1200px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>
