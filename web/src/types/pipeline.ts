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

// P1-13 (ODR-017) — L5 人工审查 payload / result.
//
// Decision taxonomy mirrors the SPEC §5 (Review) endpoint contract:
//   - approve:  push the generated YAML to live (L4 → L5 promotion)
//   - reject:   archive the job as rejected, do not promote
//   - edit:     submit a user-edited YAML that supersedes the AI output
//
// `comment` is free-form and is stored verbatim for auditability
// (P1-2 audit_logs will surface these once the auth refactor lands).
export type ReviewDecision = 'approve' | 'reject' | 'edit'

export interface PipelineReviewPayload {
  decision: ReviewDecision
  comment?: string
  // Required when decision === 'edit'. Server validates non-empty.
  edited_yaml?: string
}

export interface PipelineReview {
  decision: ReviewDecision
  comment?: string
  reviewed_at: string
  reviewer?: string
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
  // P1-13 (ODR-017): review metadata. Absent until a human acts on the job.
  review?: PipelineReview
}
