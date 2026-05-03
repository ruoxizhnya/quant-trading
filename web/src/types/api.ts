export interface BacktestRequest {
  strategy: string
  stock_pool: string[]
  start_date: string
  end_date: string
  initial_capital?: number
  commission_rate?: number
  slippage_rate?: number
}

export interface PortfolioPoint {
  date: string
  total_value: number
  cash: number
  positions: number
}

// Trade as returned by backend engine (raw format from domain.Trade)
export interface Trade {
  id: string
  symbol: string
  direction: 'long' | 'short' | 'close'
  quantity: number
  price: number              // execution price (entry for long/short, exit for close)
  commission: number
  transfer_fee: number
  stamp_tax: number
  timestamp: string          // ISO date when trade executed
  pending_qty: number
  filled_qty: number

  // Computed/display fields (not from backend, derived in UI)
  entry_date?: string        // = timestamp for long/short trades
  exit_date?: string | null  // = timestamp for close trades
  entry_price?: number       // = price for long/short
  exit_price?: number | null // = price for close
  pnl?: number               // computed from trade pair
  pnl_pct?: number           // computed from trade pair
}

export interface BacktestResult {
  id: string
  status: string
  strategy?: string
  stock_pool?: string[]
  start_date?: string
  end_date?: string
  total_return: number
  annual_return: number
  sharpe_ratio: number
  sortino_ratio: number
  max_drawdown: number
  max_drawdown_date: string
  calmar_ratio: number
  started_at: string
  completed_at: string | null
  created_at?: string
  portfolio_values: PortfolioPoint[]
  trades: Trade[]
  metrics?: Record<string, number>
  initial_capital?: number
}

export interface BacktestJob {
  id: string
  strategy_id: string
  params: Record<string, any>
  universe: string
  start_date: string
  end_date: string
  status: string
  result?: BacktestResult
  error?: string
  created_at: string
  started_at?: string
  completed_at?: string
}

export interface Strategy {
  id: string
  name: string
  description: string
  category: string
  parameters?: StrategyParam[]
}

export interface StrategyParam {
  name: string
  type: 'number' | 'string' | 'boolean' | 'select'
  default: number | string | boolean | null
  min?: number
  max?: number
  description: string
  options?: { label: string; value: number | string | boolean }[] // For select type
}

export interface MarketIndex {
  code: string
  name: string
  open: number
  high: number
  low: number
  close: number
  volume: number
  change: number
  pct: number
}

export interface MarketData {
  indices: MarketIndex[]
  updated_at: string
}

export interface StockCount {
  count: number
  latest_date: string
}

export interface HealthStatus {
  status: string
  version: string
  uptime_seconds: number
}

export interface ScreenResult {
  symbols: string[]
  total: number
  factors: Record<string, number>[]
}

export interface CopilotRequest {
  prompt: string
  context?: string
}

export interface CopilotResponse {
  code: string
  language: string
  explanation: string
  strategy_name: string
}

export interface HistoryEntry {
  id: string
  strategy?: string
  stock_pool?: string[]
  start_date?: string
  end_date?: string
  total_return: number
  sharpe_ratio?: number
  max_drawdown?: number
  created_at?: string
  trades?: TradeDisplay[]
}

export interface TradeDisplay {
  id: string
  symbol: string
  direction: string
  timestamp: string
  price: number | null
  quantity: number | null
  commission?: number
  pnl?: number
  pnl_pct?: number
  entry_date?: string
  exit_date?: string | null
  entry_price?: number | null
  exit_price?: number | null
}
