// usePaperTradingData.ts — S7-P2-9
//
// Extracted from PaperTrading.vue (661→~470 lines). Owns the paper-
// trading data lifecycle: fetch status/portfolio/positions/orders,
// 5s auto-refresh polling, and the derived account metrics
// (positionsValue, dailyPnl, cumulativePnl, todayOrders).
//
// The composable pattern keeps PaperTrading.vue focused on template
// composition + table column definitions, while the data fetching and
// polling mechanics — which are identical across any future paper-
// trading sub-view — live here and can be unit-tested in isolation.

import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import {
  getPaperTradingStatus,
  getOrders,
  getPositions,
  getPortfolio,
} from '@/api/paper-trading'
import type {
  Position,
  Order,
  PaperTradingStatus,
  Portfolio,
} from '@/api/paper-trading'

// A-share convention: red = up/profit, green = down/loss.
export const pnlColorUp = '#e03131' // 红
export const pnlColorDown = '#2f9e44' // 绿

// 5s poll interval — matches the spec from the original PaperTrading.vue.
const POLL_INTERVAL_MS = 5000

export function usePaperTradingData() {
  // ── State ────────────────────────────────────────────────────────
  const status = ref<PaperTradingStatus | null>(null)
  const loading = ref(false)
  const portfolio = ref<Portfolio | null>(null)
  const positions = ref<Position[]>([])
  const orders = ref<Order[]>([])

  // Auto-refresh toggle (default on).
  const autoRefresh = ref(true)
  let pollInterval: ReturnType<typeof setInterval> | null = null

  // ── Fetch ────────────────────────────────────────────────────────
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

  // ── Computed account metrics ─────────────────────────────────────
  // 累计盈亏 = 总资产 - 初始资金 (realised+unrealised P&L since start).
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
  // "当日订单" spec.
  const todayOrders = computed(() => {
    const today = new Date()
    const yyyy = today.getFullYear()
    const mm = String(today.getMonth() + 1).padStart(2, '0')
    const dd = String(today.getDate()).padStart(2, '0')
    const todayPrefix = `${yyyy}-${mm}-${dd}`
    return orders.value
      .filter((o) => (o.timestamp || '').startsWith(todayPrefix))
      .slice()
      .sort((a, b) => (a.timestamp < b.timestamp ? 1 : a.timestamp > b.timestamp ? -1 : 0))
  })

  // ── Polling ──────────────────────────────────────────────────────
  // autoRefresh toggles the 5s poll on/off. We tear the interval down
  // when the switch is flipped off (and on unmount) so the dashboard
  // doesn't keep hammering the API in the background.
  function startPolling() {
    if (pollInterval) return
    pollInterval = setInterval(fetchData, POLL_INTERVAL_MS)
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

  return {
    // State
    status,
    loading,
    portfolio,
    positions,
    orders,
    autoRefresh,
    // Actions
    fetchData,
    // Computed metrics
    positionsValue,
    dailyPnl,
    cumulativePnl,
    todayOrders,
  }
}
