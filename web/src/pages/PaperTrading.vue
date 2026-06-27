<template>
  <div class="paper-trading-page">
    <n-page-header title="模拟交易" subtitle="Real-time Paper Trading 实盘模拟仪表盘">
      <template #extra>
        <n-space align="center">
          <n-tag v-if="status?.running" type="success" size="small">运行中</n-tag>
          <n-tag v-else type="default" size="small">已停止</n-tag>
          <n-space align="center" :size="6">
            <n-text depth="3" style="font-size: 12px">自动刷新</n-text>
            <n-switch v-model:value="autoRefresh" size="small" />
          </n-space>
          <n-button v-if="!status?.running" type="primary" size="small" @click="showStartModal = true">
            启动
          </n-button>
          <n-button v-else type="error" size="small" @click="handleStop">
            停止
          </n-button>
          <n-button size="small" @click="fetchData" :loading="loading">刷新</n-button>
        </n-space>
      </template>
    </n-page-header>

    <!-- Top: account overview -->
    <n-card title="账户概览" :bordered="false" class="overview-card">
      <n-grid :cols="5" :x-gap="12" :y-gap="12" responsive="screen" item-responsive>
        <n-gi span="5 m:2 l:1">
          <n-statistic label="总资产" :value="portfolio?.total_value || 0" :precision="2">
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi span="5 m:2 l:1">
          <n-statistic label="可用资金" :value="portfolio?.cash || 0" :precision="2">
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi span="5 m:2 l:1">
          <n-statistic label="持仓市值" :value="positionsValue" :precision="2">
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi span="5 m:2 l:1">
          <n-statistic
            label="当日盈亏"
            :value="dailyPnl"
            :precision="2"
            :value-style="{ color: dailyPnl >= 0 ? pnlColorUp : pnlColorDown }"
          >
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi span="5 m:1 l:1">
          <n-statistic
            label="累计盈亏"
            :value="cumulativePnl"
            :precision="2"
            :value-style="{ color: cumulativePnl >= 0 ? pnlColorUp : pnlColorDown }"
          >
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
      </n-grid>
    </n-card>

    <!-- Middle: positions (left) + today's orders (right) -->
    <n-grid :cols="24" :x-gap="16" :y-gap="16" responsive="screen" item-responsive>
      <n-gi span="24 l:14">
        <n-card title="持仓列表" size="small" class="panel-card">
          <template #header-extra>
            <n-tag size="small" :bordered="false">{{ positions.length }} 只</n-tag>
          </template>
          <n-data-table
            :columns="positionColumns"
            :data="positions"
            :loading="loading"
            :pagination="{ pageSize: 8 }"
            :row-key="(r: Position) => r.symbol"
          />
        </n-card>
      </n-gi>
      <n-gi span="24 l:10">
        <n-card title="当日订单" size="small" class="panel-card">
          <template #header-extra>
            <n-tag size="small" :bordered="false">{{ todayOrders.length }} 笔</n-tag>
          </template>
          <n-data-table
            :columns="orderColumns"
            :data="todayOrders"
            :loading="loading"
            :pagination="{ pageSize: 8 }"
            :row-key="(r: Order) => r.id"
          />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Bottom: quick trade panel -->
    <n-card title="快速交易" size="small" class="trade-card">
      <n-form :model="orderForm" :rules="orderRules" ref="orderFormRef" label-placement="left" :label-width="80">
        <n-grid :cols="24" :x-gap="12" :y-gap="12" responsive="screen" item-responsive>
          <n-gi span="24 m:12 l:6">
            <n-form-item label="股票代码" path="symbol">
              <n-input v-model:value="orderForm.symbol" placeholder="如: 000001.SZ" @blur="refreshSuitability" />
            </n-form-item>
          </n-gi>
          <n-gi span="24 m:12 l:6">
            <n-form-item label="买卖" path="direction">
              <n-select v-model:value="orderForm.direction" :options="directionOptions" />
            </n-form-item>
          </n-gi>
          <n-gi span="24 m:12 l:6">
            <n-form-item label="价格" path="limit_price">
              <n-input-number
                v-model:value="orderForm.limit_price"
                :min="0"
                :step="0.01"
                placeholder="限价 (留空为市价)"
                style="width: 100%"
              />
            </n-form-item>
          </n-gi>
          <n-gi span="24 m:12 l:6">
            <n-form-item label="数量" path="quantity">
              <n-input-number v-model:value="orderForm.quantity" :min="1" :step="100" placeholder="100" style="width: 100%" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <!-- P2-4 (ODR-028): investor-suitability precheck banner.
             Renders the verdict of POST /api/compliance/check for the
             current symbol. When rejected, submit is disabled. -->
        <n-alert
          v-if="suitabilityState.visible"
          :type="suitabilityState.allowed ? 'success' : 'error'"
          :title="suitabilityState.title"
          :show-icon="true"
          class="suitability-alert"
          style="margin-bottom: 12px"
        >
          <template v-if="suitabilityState.allowed">
            当前账户已通过 <strong>{{ suitabilityState.boardName }}</strong> 适当性检查，可正常下单。
          </template>
          <template v-else>
            <p style="margin: 0 0 4px 0">
              当前账户不符合 <strong>{{ suitabilityState.boardName }}</strong> 准入要求，下单按钮已禁用。
            </p>
            <ul style="margin: 4px 0 0 20px; padding: 0">
              <li v-for="(reason, idx) in suitabilityState.reasons" :key="idx">{{ reason }}</li>
            </ul>
          </template>
        </n-alert>

        <n-space justify="end">
          <n-button @click="resetOrderForm">重置</n-button>
          <n-button
            type="primary"
            :loading="submitting"
            :disabled="suitabilityState.checked && !suitabilityState.allowed"
            @click="handleSubmitOrder"
          >
            提交订单
          </n-button>
        </n-space>
      </n-form>
    </n-card>

    <!-- Start Modal -->
    <n-modal v-model:show="showStartModal" title="启动模拟交易" preset="card" style="width: 500px">
      <n-form :model="startForm" ref="startFormRef">
        <n-form-item label="股票代码" path="symbols">
          <n-select
            v-model:value="startForm.symbols"
            multiple
            :options="stockOptions"
            placeholder="选择要交易的股票"
          />
        </n-form-item>
        <n-form-item label="初始资金" path="initial_capital">
          <n-input-number v-model:value="startForm.initial_capital" :min="100000" :step="100000" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showStartModal = false">取消</n-button>
          <n-button type="primary" @click="handleStart" :loading="starting">启动</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { h, ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import { NTag, useMessage } from 'naive-ui'
import type { DataTableColumns, FormRules, FormInst } from 'naive-ui'
import {
  getPaperTradingStatus,
  startPaperTrading,
  stopPaperTrading,
  submitOrder,
  getOrders,
  getPositions,
  getPortfolio,
} from '@/api/paper-trading'
import type { Position, Order, PaperTradingStatus, Portfolio } from '@/api/paper-trading'
import { checkSuitability } from '@/api/compliance'
import type { CheckResponse } from '@/api/compliance'
import { fmtNumber } from '@/utils/format'

const message = useMessage()

// A-share convention: red = up/profit, green = down/loss. We expose
// them as constants so the NStatistic value-style + table cell class
// stay in sync. The existing BacktestCompare page uses the inverse
// (Western) convention via --q-success/--q-danger; here we follow the
// domestic market colour norm since this is a trading dashboard.
const pnlColorUp = '#e03131'   // 红
const pnlColorDown = '#2f9e44' // 绿

// Status
const status = ref<PaperTradingStatus | null>(null)
const loading = ref(false)
const submitting = ref(false)
const starting = ref(false)

// Data
const portfolio = ref<Portfolio | null>(null)
const positions = ref<Position[]>([])
const orders = ref<Order[]>([])

// Auto-refresh toggle (default on, 5s interval per spec)
const autoRefresh = ref(true)
let pollInterval: ReturnType<typeof setInterval> | null = null

// Local stock-name lookup. The Position/Order types may carry an
// optional `name` from the backend; when absent we fall back to this
// map so the 持仓列表/订单 columns aren't full of dashes.
const STOCK_NAMES: Record<string, string> = {
  '000001.SZ': '平安银行',
  '600000.SH': '浦发银行',
  '600519.SH': '贵州茅台',
  '300750.SZ': '宁德时代',
  '002594.SZ': '比亚迪',
  '601318.SH': '中国平安',
  '000858.SZ': '五粮液',
  '600036.SH': '招商银行',
}

function stockName(symbol: string): string {
  return STOCK_NAMES[symbol] || symbol
}

// Methods
async function fetchData() {
  loading.value = true
  try {
    const [statusRes, portfolioRes, positionsRes, ordersRes] = await Promise.all([
      getPaperTradingStatus(),
      getPortfolio(),
      getPositions(),
      getOrders(),
    ])
    status.value = statusRes
    portfolio.value = portfolioRes
    positions.value = positionsRes
    orders.value = ordersRes
  } catch (error) {
    console.error('Failed to fetch paper trading data:', error)
  } finally {
    loading.value = false
  }
}

// ── Computed account metrics ──────────────────────────────────────
// 累计盈亏 = 总资产 - 初始资金 (the realised+unrealised P&L since start).
// 当日盈亏 is approximated by the sum of floating P&L across positions;
// the backend has no "yesterday's close" snapshot, so unrealised P&L
// is the closest available proxy for an intraday dashboard.
const positionsValue = computed(() =>
  positions.value.reduce((sum, pos) => sum + pos.market_value, 0),
)
const dailyPnl = computed(() =>
  positions.value.reduce((sum, pos) => sum + pos.unrealized_pnl, 0),
)
const cumulativePnl = computed(() =>
  (portfolio.value?.total_value || 0) - (status.value?.initial_capital || 0),
)

// Today's orders, newest first. The API returns all orders; we filter
// to the current calendar day so the right-hand panel matches the
// "当日订单" spec. Sorting is enforced in the column sorter too, but
// pre-sorting keeps the default view correct before the user clicks.
const todayOrders = computed(() => {
  const today = new Date()
  const yyyy = today.getFullYear()
  const mm = String(today.getMonth() + 1).padStart(2, '0')
  const dd = String(today.getDate()).padStart(2, '0')
  const todayPrefix = `${yyyy}-${mm}-${dd}`
  return orders.value
    .filter(o => (o.timestamp || '').startsWith(todayPrefix))
    .slice()
    .sort((a, b) => (a.timestamp < b.timestamp ? 1 : a.timestamp > b.timestamp ? -1 : 0))
})

// ── Position table ────────────────────────────────────────────────
// 可卖量: A-share T+1 means shares bought today cannot be sold today.
// The Position type has no per-lot lot-date, so we approximate by
// showing the full quantity — paper trading in this codebase does not
// enforce T+1 strictly (see pkg/live MockTrader). pnl_pct is derived
// from cost basis vs current price.
function pnlPct(p: Position): number {
  const cost = p.avg_cost * p.quantity
  if (!cost) return 0
  return p.unrealized_pnl / cost
}

const positionColumns: DataTableColumns<Position> = [
  { title: '股票代码', key: 'symbol', sorter: 'default', width: 110 },
  {
    title: '名称',
    key: 'name',
    width: 90,
    render: (row) => row.symbol ? stockName(row.symbol) : '—',
  },
  { title: '持仓量', key: 'quantity', sorter: (a, b) => a.quantity - b.quantity, width: 90 },
  {
    title: '可卖量',
    key: 'sellable',
    width: 90,
    render: (row) => String(row.quantity),
  },
  {
    title: '成本价',
    key: 'avg_cost',
    sorter: (a, b) => a.avg_cost - b.avg_cost,
    width: 90,
    render: (row) => fmtNumber(row.avg_cost, 2),
  },
  {
    title: '现价',
    key: 'current_price',
    sorter: (a, b) => a.current_price - b.current_price,
    width: 90,
    render: (row) => fmtNumber(row.current_price, 2),
  },
  {
    title: '市值',
    key: 'market_value',
    sorter: (a, b) => a.market_value - b.market_value,
    width: 110,
    render: (row) => '¥' + fmtNumber(row.market_value, 2),
  },
  {
    title: '盈亏',
    key: 'unrealized_pnl',
    sorter: (a, b) => a.unrealized_pnl - b.unrealized_pnl,
    width: 110,
    render: (row) =>
      h('span', { style: { color: row.unrealized_pnl >= 0 ? pnlColorUp : pnlColorDown, fontWeight: 600 } },
        (row.unrealized_pnl >= 0 ? '+' : '') + fmtNumber(row.unrealized_pnl, 2)),
  },
  {
    title: '盈亏%',
    key: 'pnl_pct',
    sorter: (a, b) => pnlPct(a) - pnlPct(b),
    width: 100,
    render: (row) => {
      const pct = pnlPct(row)
      return h('span', { style: { color: pct >= 0 ? pnlColorUp : pnlColorDown, fontWeight: 600 } },
        (pct >= 0 ? '+' : '') + (pct * 100).toFixed(2) + '%')
    },
  },
]

// ── Order table ───────────────────────────────────────────────────
const orderColumns: DataTableColumns<Order> = [
  {
    title: '时间',
    key: 'timestamp',
    sorter: (a, b) => (a.timestamp < b.timestamp ? -1 : a.timestamp > b.timestamp ? 1 : 0),
    defaultSortOrder: 'descend',
    width: 100,
    render: (row) => {
      const t = row.timestamp
      // Show HH:MM:SS when the backend sent a full ISO timestamp;
      // otherwise fall back to the raw string.
      return t && t.includes('T') ? t.slice(t.indexOf('T') + 1, t.indexOf('T') + 9) : (t || '—')
    },
  },
  { title: '股票代码', key: 'symbol', width: 110 },
  {
    title: '方向',
    key: 'direction',
    width: 80,
    render: (row) =>
      h(NTag, {
        size: 'small',
        bordered: false,
        type: row.direction === 'long' ? 'error' : 'success',
      }, { default: () => row.direction === 'long' ? '买入' : '卖出' }),
  },
  {
    title: '价格',
    key: 'price',
    width: 90,
    render: (row) => (row.price != null ? fmtNumber(row.price, 2) : '—'),
  },
  { title: '数量', key: 'quantity', width: 80 },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) =>
      h(NTag, {
        size: 'small',
        bordered: false,
        type: row.status === 'filled' ? 'success' : row.status === 'rejected' ? 'error' : 'warning',
      }, { default: () => row.status }),
  },
]

// ── Quick trade form ──────────────────────────────────────────────
const showStartModal = ref(false)
const orderFormRef = ref<FormInst | null>(null)
const startFormRef = ref<FormInst | null>(null)

const orderForm = reactive({
  symbol: '',
  direction: 'long',
  quantity: 100,
  limit_price: undefined as number | undefined,
})

const startForm = reactive({
  symbols: [] as string[],
  initial_capital: 1000000,
})

const directionOptions = [
  { label: '买入', value: 'long' },
  { label: '卖出', value: 'short' },
]

const stockOptions = [
  { label: '平安银行 (000001.SZ)', value: '000001.SZ' },
  { label: '浦发银行 (600000.SH)', value: '600000.SH' },
  { label: '贵州茅台 (600519.SH)', value: '600519.SH' },
  { label: '宁德时代 (300750.SZ)', value: '300750.SZ' },
  { label: '比亚迪 (002594.SZ)', value: '002594.SZ' },
]

const orderRules: FormRules = {
  symbol: [{ required: true, message: '请输入股票代码', trigger: 'blur' }],
  direction: [{ required: true, message: '请选择方向', trigger: 'change' }],
  quantity: [{ required: true, type: 'number', min: 1, message: '数量必须大于0', trigger: 'blur' }],
}

function resetOrderForm() {
  orderForm.symbol = ''
  orderForm.direction = 'long'
  orderForm.quantity = 100
  orderForm.limit_price = undefined
  resetSuitability()
}

async function handleStart() {
  starting.value = true
  try {
    await startPaperTrading(startForm.symbols, startForm.initial_capital)
    message.success('模拟交易已启动')
    showStartModal.value = false
    await fetchData()
  } catch (error) {
    message.error(extractErrorMessage(error, '启动失败'))
  } finally {
    starting.value = false
  }
}

async function handleStop() {
  try {
    await stopPaperTrading()
    message.success('模拟交易已停止')
    await fetchData()
  } catch (error) {
    message.error(extractErrorMessage(error, '停止失败'))
  }
}

async function handleSubmitOrder() {
  try {
    await orderFormRef.value?.validate()
  } catch {
    return
  }

  // P2-4 (ODR-028): defensive precheck on submit so a rejected symbol
  // can never reach /api/execution/orders through the UI even if the
  // operator skipped the blur handler.
  if (orderForm.symbol) {
    const ok = await ensureSuitability(orderForm.symbol)
    if (!ok) {
      message.warning('当前账户不符合该板块适当性要求，下单已拦截')
      return
    }
  }

  submitting.value = true
  try {
    await submitOrder({
      symbol: orderForm.symbol,
      direction: orderForm.direction,
      quantity: orderForm.quantity,
      order_type: orderForm.limit_price != null ? 'limit' : 'market',
      limit_price: orderForm.limit_price,
    })
    message.success('订单已提交')
    resetOrderForm()
    await fetchData()
  } catch (error) {
    message.error(extractErrorMessage(error, '提交失败'))
  } finally {
    submitting.value = false
  }
}

// ============================================================
// P2-4 (ODR-028): investor-suitability precheck logic.
// ============================================================

interface SuitabilityState {
  visible: boolean
  checked: boolean
  allowed: boolean
  title: string
  boardName: string
  reasons: string[]
}

const initialSuitability = (): SuitabilityState => ({
  visible: false,
  checked: false,
  allowed: false,
  title: '',
  boardName: '',
  reasons: [],
})

const suitabilityState = reactive<SuitabilityState>(initialSuitability())

function resetSuitability() {
  Object.assign(suitabilityState, initialSuitability())
}

function applySuitabilityResult(result: CheckResponse) {
  if (result.allowed) {
    suitabilityState.allowed = true
    suitabilityState.title = `适当性检查通过 (${result.board_name || result.board})`
    suitabilityState.boardName = result.board_name || result.board
    suitabilityState.reasons = []
  } else {
    suitabilityState.allowed = false
    suitabilityState.title = `适当性检查未通过 (${result.board_name || result.board})`
    suitabilityState.boardName = result.board_name || result.board
    suitabilityState.reasons = result.reasons || []
  }
  suitabilityState.checked = true
  suitabilityState.visible = true
}

async function refreshSuitability() {
  const symbol = orderForm.symbol.trim()
  if (!symbol) {
    resetSuitability()
    return
  }
  try {
    const result = await checkSuitability({ symbol })
    applySuitabilityResult(result)
  } catch {
    resetSuitability()
  }
}

async function ensureSuitability(symbol: string): Promise<boolean> {
  try {
    const result = await checkSuitability({ symbol })
    applySuitabilityResult(result)
    return result.allowed
  } catch (error) {
    message.error(extractErrorMessage(error, '适当性预检失败'))
    return false
  }
}

function extractErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object') {
    const e = error as { response?: { data?: { error?: string } }; message?: string }
    return e.response?.data?.error || e.message || fallback
  }
  return fallback
}

// ── Polling ───────────────────────────────────────────────────────
// autoRefresh toggles the 5s poll on/off. We tear the interval down
// when the switch is flipped off (and on unmount) so the dashboard
// doesn't keep hammering the API in the background.
function startPolling() {
  if (pollInterval) return
  pollInterval = setInterval(fetchData, 5000)
}

function stopPolling() {
  if (pollInterval) {
    clearInterval(pollInterval)
    pollInterval = null
  }
}

watch(autoRefresh, (on) => {
  if (on) startPolling()
  else stopPolling()
})

onMounted(() => {
  fetchData()
  if (autoRefresh.value) startPolling()
})

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.paper-trading-page {
  padding: 16px;
  max-width: 1400px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.overview-card,
.panel-card,
.trade-card {
  margin-top: 0;
}

.suitability-alert :deep(.n-alert__content) {
  font-size: 13px;
  line-height: 1.6;
}
</style>
