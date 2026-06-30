// usePaperTradingData.test.ts — S7-P2-9
//
// Tests the data lifecycle composable extracted from PaperTrading.vue.
// Because the composable registers onMounted / onUnmounted hooks, we
// mount a lightweight host component that invokes it from setup() and
// exposes the returned refs via a capture closure.
//
// Coverage:
//   1. Initial fetch fires on mount and populates status/portfolio/
//      positions/orders.
//   2. Computed metrics (positionsValue, dailyPnl, cumulativePnl,
//      todayOrders) derive correctly from the fetched data.
//   3. autoRefresh toggle starts/stops the 5s polling interval.
//   4. fetchData sets loading true during the request and false after.
//   5. Polling invokes fetchData on each tick.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { usePaperTradingData } from '@/composables/usePaperTradingData'
import type {
  Position,
  Order,
  PaperTradingStatus,
  Portfolio,
} from '@/api/paper-trading'

vi.mock('@/api/paper-trading', () => ({
  getPaperTradingStatus: vi.fn(),
  getPortfolio: vi.fn(),
  getPositions: vi.fn(),
  getOrders: vi.fn(),
}))

import {
  getPaperTradingStatus,
  getPortfolio,
  getPositions,
  getOrders,
} from '@/api/paper-trading'

const mockStatus = getPaperTradingStatus as unknown as ReturnType<typeof vi.fn>
const mockPortfolio = getPortfolio as unknown as ReturnType<typeof vi.fn>
const mockPositions = getPositions as unknown as ReturnType<typeof vi.fn>
const mockOrders = getOrders as unknown as ReturnType<typeof vi.fn>

const statusRes: PaperTradingStatus = { running: true, initial_capital: 1000000 }
const portfolioRes: Portfolio = { cash: 500000, positions: [], total_value: 1100000 }
const positionsRes: Position[] = [
  {
    symbol: '600000.SH',
    quantity: 1000,
    avg_cost: 10.0,
    current_price: 11.0,
    market_value: 11000,
    unrealized_pnl: 1000,
  },
  {
    symbol: '000001.SZ',
    quantity: 500,
    avg_cost: 12.0,
    current_price: 11.0,
    market_value: 5500,
    unrealized_pnl: -500,
  },
]

function makeOrder(overrides: Partial<Order> = {}): Order {
  return {
    id: 'ord-1',
    symbol: '600000.SH',
    direction: 'long',
    quantity: 1000,
    status: 'filled',
    timestamp: new Date().toISOString(),
    ...overrides,
  } as Order
}

beforeEach(() => {
  vi.useFakeTimers()
  mockStatus.mockReset()
  mockPortfolio.mockReset()
  mockPositions.mockReset()
  mockOrders.mockReset()
  // Default happy-path mocks so mount() doesn't throw if a test forgets
  // to set up its own. Each test overrides as needed.
  mockStatus.mockResolvedValue(statusRes)
  mockPortfolio.mockResolvedValue(portfolioRes)
  mockPositions.mockResolvedValue(positionsRes)
  mockOrders.mockResolvedValue([])
})

afterEach(() => {
  vi.useRealTimers()
})

// Mount a host component that captures the composable's return value
// into `captured` so assertions can inspect reactive state directly.
function mountWithData(autoRefreshInitial = true) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const captured: any = {}
  const TestHost = defineComponent({
    name: 'UsePaperTradingDataHost',
    setup() {
      const api = usePaperTradingData()
      // Force autoRefresh to the desired initial state for tests that
      // need polling disabled from the start.
      if (!autoRefreshInitial) {
        api.autoRefresh.value = false
      }
      Object.assign(captured, api)
      // Capture fetchData spy AFTER the composable returns it.
      return () => h('div')
    },
  })
  const wrapper = mount(TestHost)
  return { wrapper, captured }
}

describe('usePaperTradingData — initial fetch', () => {
  it('fires fetchData on mount and populates status/portfolio/positions/orders', async () => {
    const { captured } = mountWithData(false) // disable polling
    await flushPromises()

    expect(mockStatus).toHaveBeenCalledTimes(1)
    expect(mockPortfolio).toHaveBeenCalledTimes(1)
    expect(mockPositions).toHaveBeenCalledTimes(1)
    expect(mockOrders).toHaveBeenCalledTimes(1)

    expect(captured.status.value).toEqual(statusRes)
    expect(captured.portfolio.value).toEqual(portfolioRes)
    expect(captured.positions.value).toEqual(positionsRes)
    expect(captured.orders.value).toEqual([])
    expect(captured.loading.value).toBe(false)
  })

  it('sets loading true during the request and false after', async () => {
    // Hold the promise so we can observe loading=true mid-flight.
    let releaseStatus: (v: PaperTradingStatus) => void = () => {}
    mockStatus.mockReturnValueOnce(
      new Promise<PaperTradingStatus>((resolve) => {
        releaseStatus = resolve
      }),
    )
    mockPortfolio.mockResolvedValue(portfolioRes)
    mockPositions.mockResolvedValue(positionsRes)
    mockOrders.mockResolvedValue([])

    const { captured } = mountWithData(false)
    await flushPromises()

    expect(captured.loading.value).toBe(true)

    releaseStatus(statusRes)
    await flushPromises()

    expect(captured.loading.value).toBe(false)
  })
})

describe('usePaperTradingData — computed metrics', () => {
  it('derives positionsValue and dailyPnl from the positions array', async () => {
    const { captured } = mountWithData(false)
    await flushPromises()

    // 11000 + 5500 = 16500
    expect(captured.positionsValue.value).toBe(16500)
    // 1000 + (-500) = 500
    expect(captured.dailyPnl.value).toBe(500)
  })

  it('derives cumulativePnl from total_value minus initial_capital', async () => {
    const { captured } = mountWithData(false)
    await flushPromises()

    // 1100000 - 1000000 = 100000
    expect(captured.cumulativePnl.value).toBe(100000)
  })

  it('filters todayOrders to the current calendar day and sorts newest-first', async () => {
    const now = new Date()
    const todayPrefix = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    const todayOrders: Order[] = [
      makeOrder({ id: 'old', timestamp: '2020-01-01T09:30:00Z' }),
      makeOrder({ id: 't1', timestamp: `${todayPrefix}T09:30:00Z` }),
      makeOrder({ id: 't2', timestamp: `${todayPrefix}T14:00:00Z` }),
    ]
    mockOrders.mockResolvedValue(todayOrders)

    const { captured } = mountWithData(false)
    await flushPromises()

    const result = captured.todayOrders.value
    expect(result).toHaveLength(2)
    // Newest first — t2 (14:00) before t1 (09:30).
    expect(result[0].id).toBe('t2')
    expect(result[1].id).toBe('t1')
  })

  it('returns an empty todayOrders list when no orders match today', async () => {
    mockOrders.mockResolvedValue([
      makeOrder({ id: 'old', timestamp: '2020-01-01T09:30:00Z' }),
    ])

    const { captured } = mountWithData(false)
    await flushPromises()

    expect(captured.todayOrders.value).toHaveLength(0)
  })
})

describe('usePaperTradingData — polling', () => {
  it('starts the 5s poll on mount when autoRefresh is true', async () => {
    const { captured } = mountWithData(true)
    await flushPromises()

    // Initial mount fetch = 1 call.
    expect(mockStatus).toHaveBeenCalledTimes(1)

    // Advance 5s → first poll tick fires fetchData again.
    vi.advanceTimersByTime(5000)
    await flushPromises()
    expect(mockStatus).toHaveBeenCalledTimes(2)

    // Advance another 5s → second poll tick.
    vi.advanceTimersByTime(5000)
    await flushPromises()
    expect(mockStatus).toHaveBeenCalledTimes(3)

    expect(captured.autoRefresh.value).toBe(true)
  })

  it('stops polling when autoRefresh is toggled off', async () => {
    const { captured } = mountWithData(true)
    await flushPromises()

    expect(mockStatus).toHaveBeenCalledTimes(1)

    // Toggle off.
    captured.autoRefresh.value = false
    await nextTick()

    vi.advanceTimersByTime(15000) // 3 ticks worth — should fire 0 calls
    await flushPromises()

    expect(mockStatus).toHaveBeenCalledTimes(1)
  })

  it('resumes polling when autoRefresh is toggled back on', async () => {
    const { captured } = mountWithData(true)
    await flushPromises()

    captured.autoRefresh.value = false
    await nextTick()

    vi.advanceTimersByTime(10000)
    await flushPromises()
    expect(mockStatus).toHaveBeenCalledTimes(1)

    captured.autoRefresh.value = true
    await nextTick()

    vi.advanceTimersByTime(5000)
    await flushPromises()
    expect(mockStatus).toHaveBeenCalledTimes(2)
  })

  it('tears down the interval on unmount so polling stops', async () => {
    const { wrapper } = mountWithData(true)
    await flushPromises()

    expect(mockStatus).toHaveBeenCalledTimes(1)

    wrapper.unmount()

    vi.advanceTimersByTime(15000)
    await flushPromises()

    // No additional calls after unmount.
    expect(mockStatus).toHaveBeenCalledTimes(1)
  })
})
