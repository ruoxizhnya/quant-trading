<template>
  <div class="paper-trading-page">
    <n-page-header title="模拟交易" subtitle="Paper Trading 实盘模拟">
      <template #extra>
        <n-space>
          <n-tag v-if="status?.running" type="success">运行中</n-tag>
          <n-tag v-else type="default">已停止</n-tag>
          <n-button v-if="!status?.running" type="primary" @click="showStartModal = true">
            启动模拟交易
          </n-button>
          <n-button v-else type="error" @click="handleStop">
            停止模拟交易
          </n-button>
        </n-space>
      </template>
    </n-page-header>

    <!-- Portfolio Summary -->
    <n-card title="组合概览" class="portfolio-card">
      <n-grid :cols="4" :x-gap="12" :y-gap="12">
        <n-gi>
          <n-statistic label="总资产" :value="portfolio?.total_value || 0" precision={2}>
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="可用现金" :value="portfolio?.cash || 0" precision={2}>
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="持仓市值" :value="positionsValue" precision={2}>
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
        <n-gi>
          <n-statistic label="初始资金" :value="status?.initial_capital || 1000000" precision={2}>
            <template #prefix>¥</template>
          </n-statistic>
        </n-gi>
      </n-grid>
    </n-card>

    <!-- Order Form -->
    <n-card title="下单" class="order-card">
      <n-form :model="orderForm" :rules="orderRules" ref="orderFormRef">
        <n-grid :cols="4" :x-gap="12">
          <n-gi>
            <n-form-item label="股票代码" path="symbol">
              <n-input v-model:value="orderForm.symbol" placeholder="如: 000001.SZ" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="方向" path="direction">
              <n-select v-model:value="orderForm.direction" :options="directionOptions" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="数量" path="quantity">
              <n-input-number v-model:value="orderForm.quantity" :min="1" placeholder="100" />
            </n-form-item>
          </n-gi>
          <n-gi>
            <n-form-item label="订单类型" path="order_type">
              <n-select v-model:value="orderForm.order_type" :options="orderTypeOptions" />
            </n-form-item>
          </n-gi>
        </n-grid>
        <n-form-item v-if="orderForm.order_type === 'limit'" label="限价" path="limit_price">
          <n-input-number v-model:value="orderForm.limit_price" :min="0" placeholder="10.5" />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" @click="handleSubmitOrder" :loading="submitting">
            提交订单
          </n-button>
        </n-form-item>
      </n-form>
    </n-card>

    <!-- Positions Table -->
    <n-card title="当前持仓" class="positions-card">
      <n-data-table
        :columns="positionColumns"
        :data="positions"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
      />
    </n-card>

    <!-- Orders Table -->
    <n-card title="订单记录" class="orders-card">
      <n-data-table
        :columns="orderColumns"
        :data="orders"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
      />
    </n-card>

    <!-- Trades Table -->
    <n-card title="成交记录" class="trades-card">
      <n-data-table
        :columns="tradeColumns"
        :data="trades"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
      />
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
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useMessage } from 'naive-ui'
import type { DataTableColumns, FormRules, FormInst } from 'naive-ui'
import {
  getPaperTradingStatus,
  startPaperTrading,
  stopPaperTrading,
  submitOrder,
  getOrders,
  getPositions,
  getPortfolio,
  getTrades,
} from '@/api/paper-trading'
import type { Position, Trade, Order, PaperTradingStatus, Portfolio } from '@/api/paper-trading'

const message = useMessage()

// Status
const status = ref<PaperTradingStatus | null>(null)
const loading = ref(false)
const submitting = ref(false)
const starting = ref(false)

// Data
const portfolio = ref<Portfolio | null>(null)
const positions = ref<Position[]>([])
const orders = ref<Order[]>([])
const trades = ref<Trade[]>([])

// Methods
async function fetchData() {
  loading.value = true
  try {
    const [statusRes, portfolioRes, positionsRes, ordersRes, tradesRes] = await Promise.all([
      getPaperTradingStatus(),
      getPortfolio(),
      getPositions(),
      getOrders(),
      getTrades(),
    ])
    status.value = statusRes
    portfolio.value = portfolioRes
    positions.value = positionsRes
    orders.value = ordersRes
    trades.value = tradesRes
  } catch (error) {
    console.error('Failed to fetch paper trading data:', error)
  } finally {
    loading.value = false
  }
}

// Forms
const showStartModal = ref(false)
const orderFormRef = ref<FormInst | null>(null)
const startFormRef = ref<FormInst | null>(null)

const orderForm = reactive({
  symbol: '',
  direction: 'long',
  quantity: 100,
  order_type: 'market',
  limit_price: undefined as number | undefined,
})

const startForm = reactive({
  symbols: [] as string[],
  initial_capital: 1000000,
})

// Options
const directionOptions = [
  { label: '买入', value: 'long' },
  { label: '卖出', value: 'short' },
]

const orderTypeOptions = [
  { label: '市价单', value: 'market' },
  { label: '限价单', value: 'limit' },
]

const stockOptions = [
  { label: '平安银行 (000001.SZ)', value: '000001.SZ' },
  { label: '浦发银行 (600000.SH)', value: '600000.SH' },
  { label: '贵州茅台 (600519.SH)', value: '600519.SH' },
  { label: '宁德时代 (300750.SZ)', value: '300750.SZ' },
  { label: '比亚迪 (002594.SZ)', value: '002594.SZ' },
]

// Computed
const positionsValue = computed(() => {
  return positions.value.reduce((sum, pos) => sum + pos.market_value, 0)
})

// Rules
const orderRules: FormRules = {
  symbol: [{ required: true, message: '请输入股票代码', trigger: 'blur' }],
  direction: [{ required: true, message: '请选择方向', trigger: 'change' }],
  quantity: [{ required: true, type: 'number', min: 1, message: '数量必须大于0', trigger: 'blur' }],
}

// Columns
const positionColumns: DataTableColumns<Position> = [
  { title: '股票代码', key: 'symbol' },
  { title: '数量', key: 'quantity' },
  { title: '成本价', key: 'avg_cost' },
  { title: '当前价', key: 'current_price' },
  { title: '市值', key: 'market_value' },
  { title: '浮动盈亏', key: 'unrealized_pnl' },
]

const orderColumns: DataTableColumns<Order> = [
  { title: '订单ID', key: 'id' },
  { title: '股票代码', key: 'symbol' },
  { title: '方向', key: 'direction' },
  { title: '数量', key: 'quantity' },
  { title: '状态', key: 'status' },
  { title: '时间', key: 'timestamp' },
]

const tradeColumns: DataTableColumns<Trade> = [
  { title: '交易ID', key: 'id' },
  { title: '股票代码', key: 'symbol' },
  { title: '方向', key: 'direction' },
  { title: '数量', key: 'quantity' },
  { title: '成交价', key: 'price' },
  { title: '佣金', key: 'commission' },
  { title: '时间', key: 'timestamp' },
]

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
    // Form validation errors are surfaced by n-form-item rules; no toast needed.
    return
  }
  submitting.value = true
  try {
    await submitOrder({
      symbol: orderForm.symbol,
      direction: orderForm.direction,
      quantity: orderForm.quantity,
      order_type: orderForm.order_type,
      limit_price: orderForm.limit_price,
    })
    message.success('订单已提交')
    orderForm.symbol = ''
    orderForm.quantity = 100
    orderForm.limit_price = undefined
    await fetchData()
  } catch (error) {
    message.error(extractErrorMessage(error, '提交失败'))
  } finally {
    submitting.value = false
  }
}

// CR-07 (ODR-012): `catch (error: any)` is replaced by typed `unknown` handling.
// The `any` escape hatch bypasses TypeScript's null/undefined safety net and
// silently propagates API client errors as `undefined` when axios structure
// changes. extractErrorMessage narrows the error and preserves the original
// server-side message when available.
function extractErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object') {
    const e = error as { response?: { data?: { error?: string } }; message?: string }
    return e.response?.data?.error || e.message || fallback
  }
  return fallback
}

// Polling
let pollInterval: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  fetchData()
  pollInterval = setInterval(fetchData, 5000)
})

onUnmounted(() => {
  if (pollInterval) clearInterval(pollInterval)
})
</script>

<style scoped>
.paper-trading-page {
  padding: 20px;
  max-width: 1400px;
  margin: 0 auto;
}

.portfolio-card,
.order-card,
.positions-card,
.orders-card,
.trades-card {
  margin-top: 16px;
}
</style>
