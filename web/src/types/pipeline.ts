import type { BacktestResult } from './api'

export interface PipelineParameter {
  name: string
  type: string
  value: number | string | boolean
  description: string
  min?: number
  max?: number
}

export interface PipelineRiskConstraints {
  max_positions?: number
  max_drawdown?: number
  stop_loss?: number
  take_profit?: number
  position_sizing?: string
}

export interface PipelineIntent {
  raw_text: string
  strategy_type: string
  strategy_name: string
  description: string
  parameters: PipelineParameter[]
  indicators: string[]
  timeframe: string
  universe: string
  risk_constraints?: PipelineRiskConstraints
  confidence: number
}

export interface PipelineResult {
  id: string
  status: 'parse' | 'generate' | 'validate' | 'compile' | 'backtest' | 'complete' | 'failed'
  intent?: PipelineIntent
  yaml_config?: string
  generated_code?: string
  build_error?: string
  backtest_result?: BacktestResult
  backtest_error?: string
  started_at: string
  completed_at?: string
  duration_ms: number
  logs?: string[]
}
