import api from './client'

// P1-3 观察页的数据源。后端见 cmd/analysis/handlers_explore.go。
//
// 一次「探索」= 一轮研究：AI 连续试若干组参数，每试一次落一行实验日志。
// 这里的三个接口对应「启动 / 看进度 / 叫停」—— 叫停是一等公民，因为
// 一轮被叫停的探索和跑满的探索，其结论可信度完全不同。

// P2-9 验证器链的裁决。后端见 pkg/validation/aggregate.go。
//
// 关键语义（别在前端改掉）：它不是通过/不通过，而是**概率估计 + 质疑清单**。
// 决策权在人手里 —— 所以这里要把「没评估的维度」也显示出来，
// 「没查」不等于「没问题」。
export interface Challenge {
  dimension: string
  severity: 'blocking' | 'warning' | 'note'
  message: string
}

/** 因果维（六维里唯一需要模型的一维）：机制 + 可证伪预测的验证结果 */
export interface CausalResult {
  mechanism?: string
  /** 可证伪的预测条数；只有这些才算数，讲得出但验不了的不算 */
  testable: number
  passed: number
  failed: number
  probability: number
}

export interface Verdict {
  /** 各维概率；缺的那一维不在 map 里（= 未评估，不是 0 分） */
  dimensions: Record<string, number>
  unassessed?: string[]
  /** 综合概率 = 已评估维度的最小值：结论受限于最弱的那一环 */
  probability: number
  weakest?: string
  geometric_mean: number
  blocking: number
  challenges: Challenge[]
  /** 只对最终候选做（一次模型调用不便宜），其余尝试为 undefined */
  causal?: CausalResult | null
}

export const DIMENSION_LABELS: Record<string, string> = {
  statistical: '统计',
  economic: '经济',
  robustness: '稳健',
  bias: '偏差',
  redundancy: '冗余',
  causal: '因果',
}

export function dimensionLabel(d: string): string {
  return DIMENSION_LABELS[d] ?? d
}

export interface AttemptView {
  seq: number
  hypothesis: string
  params: Record<string, unknown>
  experiment_id: number
  sharpe: number
  ok: boolean
  error?: string
  /** 失败的尝试没有裁决 —— 没有回测结果就没有可被证伪的东西 */
  verdict?: Verdict | null
}

export interface ExploreStatus {
  run_id: string
  running: boolean
  stopped?: string
  attempts: AttemptView[]
  best_seq?: number | null
}

export interface StartExploreRequest {
  description: string
  max_tries?: number
  lookback_min?: number
  lookback_max?: number
}

export interface StartExploreResponse {
  run_id: string
  max_tries: number
}

export function startExplore(req: StartExploreRequest): Promise<StartExploreResponse> {
  return api.post<StartExploreResponse>('/api/explore/runs', req)
}

export function getExploreStatus(runID: string): Promise<ExploreStatus> {
  return api.get<ExploreStatus>(`/api/explore/runs/${encodeURIComponent(runID)}`)
}

export function stopExplore(runID: string): Promise<{ run_id: string; stopping: boolean }> {
  return api.post<{ run_id: string; stopping: boolean }>(
    `/api/explore/runs/${encodeURIComponent(runID)}/stop`,
    {},
  )
}
