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
