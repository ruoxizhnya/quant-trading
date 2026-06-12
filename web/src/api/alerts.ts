import api from './client'

// ── Alert type definitions ──────────────────────────────────────────
// Mirror the Go struct in pkg/alert/manager.go. JSON tag
// conventions follow Go's encoding/json snake_case output.

export type AlertSeverity = 'info' | 'warning' | 'critical'

export interface Alert {
  id: string
  rule: string
  severity: AlertSeverity
  message: string
  timestamp: string
  symbol?: string
  sector?: string
  value?: number
  threshold?: number
  attributes?: Record<string, unknown>
}

export interface AlertHistoryResponse {
  alerts: Alert[]
  count: number
  limit: number
}

export interface AlertStatsResponse {
  enabled: boolean
  channel_count: number
  recorder_len: number
  recorder_evicted: number
  history_len: number
  history_limit: number
  by_rule: Record<string, number>
  by_severity: Record<string, number>
}

export interface ForceCheckResponse {
  dispatched: number
}

// ── Endpoint helpers ────────────────────────────────────────────────

export async function getAlertHistory(params?: {
  limit?: number
  severity?: AlertSeverity
}): Promise<AlertHistoryResponse> {
  const search = new URLSearchParams()
  if (params?.limit) search.set('limit', String(params.limit))
  if (params?.severity) search.set('severity', params.severity)
  const qs = search.toString()
  return api.get<AlertHistoryResponse>(`/alerts/history${qs ? '?' + qs : ''}`)
}

export async function forceCheckAlerts(): Promise<ForceCheckResponse> {
  return api.post<ForceCheckResponse>('/alerts/force-check', {})
}

export async function getAlertStats(): Promise<AlertStatsResponse> {
  return api.get<AlertStatsResponse>('/alerts/stats')
}
