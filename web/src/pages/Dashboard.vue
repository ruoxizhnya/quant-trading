<template>
  <div class="dashboard-page">
    <div class="greeting-section">
      <div>
        <h1 class="greeting">{{ greeting }} 👋</h1>
        <p class="date-str">{{ todayStr }}</p>
      </div>
      <n-space :size="12">
        <n-select v-model:value="selectedIndex" :options="indexOptions" size="small" style="width:120px" />
        <n-button size="small" @click="refreshMarket">刷新</n-button>
      </n-space>
    </div>

    <n-grid :cols="4" :x-gap="16" :y-gap="16" class="metrics-grid">
      <n-gi v-for="m in metrics" :key="m.label">
        <div class="metric-card">
          <n-icon :size="28" :color="m.iconColor"><component :is="m.icon" /></n-icon>
          <div class="metric-value" :class="m.valueClass">{{ m.value }}</div>
          <div class="metric-label">{{ m.label }}</div>
          <div v-if="m.sub" class="metric-sub">{{ m.sub }}</div>
        </div>
      </n-gi>
    </n-grid>

    <n-card title="快速回测" class="quick-bt-card">
      <template #header-extra><n-icon size="18"><FlashOutline /></n-icon></template>
      <n-form inline :label-width="60">
        <n-form-item label="策略">
          <n-select v-model:value="quickForm.strategy" :options="strategyOptions" style="width:160px" size="small" />
        </n-form-item>
        <n-form-item label="股票">
          <n-input v-model:value="quickForm.stock" placeholder="600000.SH" style="width:140px" size="small" />
        </n-form-item>
        <n-form-item label="开始">
          <n-date-picker v-model:formatted-value="quickForm.start" type="date" value-format="yyyy-MM-dd" style="width:140px" size="small" />
        </n-form-item>
        <n-form-item label="结束">
          <n-date-picker v-model:formatted-value="quickForm.end" type="date" value-format="yyyy-MM-dd" style="width:140px" size="small" />
        </n-form-item>
        <n-button type="primary" :loading="quickLoading" @click="runQuick" size="small">
          <template #icon><PlayOutline /></template>
          运行回测
        </n-button>
      </n-form>
    </n-card>

    <n-grid :cols="4" :x-gap="16" :y-gap="16" class="nav-tiles">
      <n-gi v-for="tile in navTiles" :key="tile.path">
        <router-link :to="tile.path" class="nav-tile">
          <n-icon :size="32" :color="tile.iconColor"><component :is="tile.icon" /></n-icon>
          <div class="nav-tile-title">{{ tile.title }}</div>
          <div class="nav-tile-desc">{{ tile.desc }}</div>
          <span class="nav-tile-arrow">→</span>
        </router-link>
      </n-gi>
    </n-grid>

    <n-card title="控制台日志" class="history-card">
      <template #header-extra><n-icon size="18"><ListOutline /></n-icon></template>
      <template #extra>
        <n-space align="center" :size="8">
          <n-tag round :bordered="false" size="small">{{ backtestStore.history.length }}</n-tag>
          <n-button quaternary size="tiny" @click="clearHistory">清除</n-button>
        </n-space>
      </template>
      <n-list v-if="validHistory.length" bordered>
        <n-list-item v-for="(item, i) in validHistory" :key="item.id || i">
          <template #prefix>
            <n-tag :type="(item.total_return ?? 0) >= 0 ? 'success' : 'error'" size="small" round :bordered="false">
              {{ (item.total_return ?? 0) >= 0 ? '多' : '空' }}
            </n-tag>
          </template>
          <n-thing :title="`${Array.isArray(item.stock_pool) ? item.stock_pool[0] : (item.stock_pool || '')} · ${item.strategy || ''}`" :description="historyDesc(item)" />
          <template #suffix>
            <n-time :time="new Date(item.started_at || Date.now())" format="MM-dd HH:mm" type="relative" />
          </template>
        </n-list-item>
      </n-list>
      <n-empty v-else description="暂无回测记录" />
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, markRaw } from 'vue'
import { useRouter } from 'vue-router'
import {
  NGrid, NGi, NCard, NForm, NFormItem, NSelect, NInput,
  NDatePicker, NButton, NSpace, NTag, NList, NListItem, NThing,
  NTime, NEmpty, NIcon, useMessage,
} from 'naive-ui'
import {
  FlashOutline, PlayOutline, ListOutline,
  ServerOutline, StatsChartOutline, RocketOutline, TimeOutline,
  AnalyticsOutline, SearchOutline, ChatbubbleEllipsesOutline, BeakerOutline,
} from '@vicons/ionicons5'
import { useBacktestStore } from '@/stores/backtest'
import { getMarketIndex, getStockCount } from '@/api/market'
import { getStrategies } from '@/api/strategy'
import { runBacktest } from '@/api/backtest'
import { fmtPercent } from '@/utils/format'

const router = useRouter()
const message = useMessage()
const backtestStore = useBacktestStore()

const validHistory = computed(() =>
  (backtestStore.history || []).filter((item: any) => item && item.id).slice(0, 10)
)

const selectedIndex = ref('sh300')
const quickLoading = ref(false)
const quickRunning = ref(false)
const quickForm = ref({ strategy: 'momentum', stock: '600000.SH', start: '2024-01-01', end: '2024-12-31' })

const metrics = ref([
  { icon: markRaw(ServerOutline), iconColor: '#58a6ff', value: '-', label: '数据库股票数', sub: '', valueClass: '' },
  { icon: markRaw(StatsChartOutline), iconColor: '#3fb950', value: '-', label: '累计回测次数', sub: '', valueClass: '' },
  { icon: markRaw(RocketOutline), iconColor: '#f0883e', value: '-', label: '可用策略', sub: '', valueClass: '' },
  { icon: markRaw(TimeOutline), iconColor: '#8b949e', value: '-', label: '最后数据同步', sub: '', valueClass: '' },
])

const indexOptions = [
  { label: '沪深300', value: 'sh300' },
  { label: '上证指数', value: 'sse' },
  { label: '创业板', value: 'cyb' },
]

const strategyOptions = [
  { label: '动量策略', value: 'momentum' },
  { label: '均值回归', value: 'mean_reversion' },
  { label: '质量因子', value: 'quality' },
  { label: '价值因子', value: 'value' },
]

const navTiles = [
  { path: '/backtest', icon: markRaw(AnalyticsOutline), iconColor: '#58a6ff', title: '回测引擎', desc: '运行策略回测，查看净值曲线与交易记录' },
  { path: '/screener', icon: markRaw(SearchOutline), iconColor: '#3fb950', title: '选股器', desc: '按财务指标筛选股票，构建投资组合' },
  { path: '/copilot', icon: markRaw(ChatbubbleEllipsesOutline), iconColor: '#a371f7', title: '策略 Copilot', desc: '用自然语言生成量化交易策略代码' },
  { path: '/strategy-lab', icon: markRaw(BeakerOutline), iconColor: '#f0883e', title: '因子分析', desc: 'Alpha 因子研究与多因子模型' },
]

const now = new Date()
const hour = now.getHours()
let greeting = '晚上好'
if (hour >= 5 && hour < 12) greeting = '早上好'
else if (hour >= 12 && hour < 14) greeting = '中午好'
else if (hour >= 14 && hour < 18) greeting = '下午好'

const todayStr = now.toLocaleDateString('zh-CN', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })

onMounted(() => {
  backtestStore.loadHistory()
  loadMetrics()
  loadStrategies()
})

async function loadMetrics() {
  try {
    const [stockRes, stratRes] = await Promise.all([
      getStockCount().catch(() => null),
      getStrategies().catch(() => null),
    ])
    if (stockRes) {
      metrics.value[0].value = (stockRes.count || 0).toLocaleString()
      if (stockRes.latest_date) metrics.value[0].sub = `最新: ${stockRes.latest_date}`
    }
    if (stratRes) {
      const list = stratRes.strategies || []
      metrics.value[2].value = String(list.length)
      const names = list.map(s => s.description || s.name).slice(0, 3).join(', ')
      if (names) metrics.value[2].sub = names
    }
    metrics.value[1].value = String(backtestStore.history.length)
    const last = backtestStore.history[0]
    if (last) metrics.value[3].sub = new Date(last.started_at || '').toLocaleDateString('zh-CN')
  } catch {}
}

async function loadStrategies() {
  try {
    const res = await getStrategies()
    // already loaded in loadMetrics, this is a fallback
  } catch {}
}

function refreshMarket() { loadMetrics() }

async function runQuick() {
  if (quickRunning.value) return
  if (!quickForm.value.stock || !quickForm.value.start || !quickForm.value.end) {
    message.warning('请填写完整参数')
    return
  }
  quickRunning.value = true
  quickLoading.value = true
  try {
    const result = await runBacktest({
      strategy: quickForm.value.strategy,
      stock_pool: [quickForm.value.stock],
      start_date: quickForm.value.start,
      end_date: quickForm.value.end,
      initial_capital: 1000000,
      commission_rate: 0.0003,
      slippage_rate: 0.0001,
    })
    if (result && result.id) {
      backtestStore.addToHistory(result)
      router.push({ name: 'backtest', query: { id: result.id } })
    } else {
      message.warning('回测完成但未返回有效结果')
    }
  } catch (e: any) {
    message.error('回测失败: ' + (e.message || e))
  } finally {
    quickLoading.value = false
    quickRunning.value = false
  }
}

function clearHistory() { backtestStore.clearHistory() }

function historyDesc(item: any): string {
  const ret = fmtPercent(item.total_return)
  const sharpe = (item.sharpe_ratio != null && !isNaN(item.sharpe_ratio)) ? item.sharpe_ratio.toFixed(2) : '-'
  const dd = (item.max_drawdown != null && !isNaN(item.max_drawdown)) ? (item.max_drawdown * 100).toFixed(2) + '%' : '-'
  return `收益: ${ret} | 夏普: ${sharpe} | 最大回撤: ${dd}`
}
</script>

<style scoped>
.dashboard-page { max-width: 1400px; margin: 0 auto; display: flex; flex-direction: column; gap: 20px; }

.greeting-section { display: flex; justify-content: space-between; align-items: flex-start; }
.greeting { font-size: 24px; font-weight: 700; color: var(--q-text); }
.date-str { color: var(--q-text3); font-size: 13px; margin-top: 4px; }

.metric-card {
  background: var(--q-surface);
  border: 1px solid var(--q-border);
  border-radius: var(--q-radius);
  padding: 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  transition: border-color var(--q-transition), transform var(--q-transition);
}
.metric-card:hover { border-color: var(--q-text3); transform: translateY(-2px); }
.metric-value { font-size: 28px; font-weight: 700; color: var(--q-primary); margin-top: 6px; }
.metric-label { font-size: 12px; color: var(--q-text3); margin-top: 4px; }
.metric-sub { font-size: 11px; color: var(--q-text3); margin-top: 2px; }

.quick-bt-card { margin-top: 4px; }

.nav-tile {
  display: block;
  background: var(--q-surface);
  border: 1px solid var(--q-border);
  border-radius: var(--q-radius);
  padding: 20px;
  text-decoration: none;
  transition: all var(--q-transition);
  position: relative;
}
.nav-tile:hover { border-color: var(--q-primary); transform: translateY(-2px); box-shadow: var(--q-shadow-lg); }
.nav-tile-title { font-size: 15px; font-weight: 600; color: var(--q-text); margin-top: 8px; }
.nav-tile-desc { font-size: 12px; color: var(--q-text3); margin-top: 4px; line-height: 1.5; }
.nav-tile-arrow { position: absolute; right: 16px; top: 50%; transform: translateY(-50%); color: var(--q-text3); font-size: 18px; }

.history-card .n-list { max-height: 400px; overflow-y: auto; }
</style>
