// 执行（execution）域的 API —— 后端是 `cmd/analysis/handlers_execution.go`。
//
// 这个文件是 AUD-19（2026-09-22）从 `api/paper-trading.ts` 里搬出来的：
// 原先它和一批 `/api/paper/*` 调用挤在同一个文件里，但那批端点**从未挂载**
// （整条线已删除）。`emergencyFlatten` 是这里唯一真正活着的东西 ——
// 它打的是 `/api/execution/emergency-flatten`，由 ExecutionHandler 注册。
//
// 后端侧对应的能力还包括 `/api/execution/{orders,positions,account}`，
// 需要时在这里补。

import api from './client'

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
    '/api/execution/emergency-flatten',
    { reason, confirmation_token: token },
    {
      headers: { Authorization: `Bearer ${token}` },
    },
  )
}
