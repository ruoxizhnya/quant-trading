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

export interface Trade {
  id: string
  symbol: string
  direction: 'long' | 'short' | 'close'
  entry_date: string
  exit_date: string | null
  entry_price: number
  exit_price: number | null
  quantity: number
  pnl: number
  pnl_pct: number
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
