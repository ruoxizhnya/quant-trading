import api from './client'

// P1-3 观察页的数据源。后端见 cmd/analysis/handlers_explore.go。
//
// 一次「探索」= 一轮研究：AI 连续试若干组参数，每试一次落一行实验日志。
// 这里的三个接口对应「启动 / 看进度 / 叫停」—— 叫停是一等公民，因为
// 一轮被叫停的探索和跑满的探索，其结论可信度完全不同。

export interface AttemptView {
  seq: number
  hypothesis: string
  params: Record<string, unknown>
  experiment_id: number
  sharpe: number
  ok: boolean
  error?: string
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
