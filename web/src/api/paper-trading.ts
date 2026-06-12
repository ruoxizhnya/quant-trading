import api from './client'

export interface PaperTradingStatus {
  running: boolean
  initial_capital: number
}

export interface SubmitOrderRequest {
  symbol: string
  direction: string
  quantity: number
  order_type?: string
  limit_price?: number
}

export interface OrderResponse {
  order_id: string
  status: string
}

export interface Portfolio {
  cash: number
  positions: Position[]
  total_value: number
}

export interface Position {
  symbol: string
  quantity: number
  avg_cost: number
  current_price: number
  market_value: number
  unrealized_pnl: number
}

export interface Order {
  id: string
  symbol: string
  direction: string
  quantity: number
  status: string
  timestamp: string
}

export interface Trade {
  id: string
  symbol: string
  direction: string
  quantity: number
  price: number
  commission: number
  timestamp: string
}

export async function startPaperTrading(symbols: string[], initialCapital?: number) {
  return api.post('/paper/start', {
    symbols,
    initial_capital: initialCapital,
  })
}

export async function stopPaperTrading() {
  return api.post('/paper/stop')
}

export async function getPaperTradingStatus() {
  return api.get<PaperTradingStatus>('/paper/status')
}

export async function submitOrder(order: SubmitOrderRequest) {
  return api.post<OrderResponse>('/paper/orders', order)
}

export async function getOrders(): Promise<Order[]> {
  return api.get<Order[]>('/paper/orders')
}

export async function getOrder(orderId: string) {
  return api.get(`/paper/orders/${orderId}`)
}

export async function cancelOrder(orderId: string) {
  return api.delete(`/paper/orders/${orderId}`)
}

export async function getPositions() {
  return api.get<Position[]>('/paper/positions')
}

export async function getPortfolio() {
  return api.get<Portfolio>('/paper/portfolio')
}

export async function getTrades() {
  return api.get<Trade[]>('/paper/trades')
}

// ── Emergency Flatten (P2-3, ODR-026) ──────────────────────────────
// Kill-switch endpoint. The server-side `trading.emergency_token`
// must be configured (otherwise the server returns 503). The same
// token is required in both the Authorization Bearer header AND
// the body `confirmation_token` field — defence in depth.

export interface EmergencyFlattenOrder {
  symbol: string
  order_id: string
  quantity: number
  fill_price: number
  net_proceeds: number
  bypassed_t1: boolean
  submitted_at: string
}

export interface EmergencyFlattenSkip {
  symbol: string
  quantity: number
  reason: string
}

export interface EmergencyFlattenResult {
  sold: EmergencyFlattenOrder[]
  skipped: EmergencyFlattenSkip[]
  sold_total: number
  started_at: string
  completed_at: string
  reason: string
  latency_ms: number
}

export interface EmergencyFlattenRequest {
  reason: string
  confirmation_token: string
}

export async function emergencyFlatten(
  token: string,
  reason: string,
): Promise<EmergencyFlattenResult> {
  return api.post<EmergencyFlattenResult>(
    '/execution/emergency-flatten',
    { reason, confirmation_token: token },
    {
      headers: { Authorization: `Bearer ${token}` },
    },
  )
}
