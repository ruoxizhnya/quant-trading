// P2-4 (ODR-028): Investor-suitability (适当性管理) API client.
//
// The frontend calls these endpoints BEFORE submitting an order to
// /api/execution/orders. The backend is the source of truth for
// per-board thresholds (100k ChiNext, 500k STAR, 1M BSE — see
// pkg/compliance/appropriateness.go), so the UI does NOT duplicate
// the rule logic; it only renders the verdict and the explanation
// reasons.
//
//   POST /api/compliance/check        — verdict for a single symbol
//   GET  /api/compliance/requirements  — current per-board thresholds
//   GET  /api/compliance/boards        — boards that require a check
//
// The handler returns HTTP 200 for "allowed" or "implicit allow"
// (boards outside the suitability scope) and HTTP 422 for explicit
// rejections with a structured CheckResponse body. The client
// surfaces the rejection reasons verbatim — translation to UI
// language happens in the consumer (OrderForm, etc.).
import api from './client'

// RiskLevel mirrors pkg/compliance.RiskLevel. The numbers are 0..5
// (Unset, C1, C2, C3, C4, C5). We re-declare the union here so the
// frontend can do `as const` checks without a runtime import.
export type RiskLevel = 0 | 1 | 2 | 3 | 4 | 5

// SuitabilityProfile mirrors the backend's `compliance.SuitabilityProfile`.
// All fields are optional; missing fields fall back to the handler's
// `defaultProfile` (configured server-side from
// `trading.default_user_profile.*`).
export interface SuitabilityProfile {
  user_id?: string
  asset_daily_avg_cny?: number
  first_trade_at?: string // RFC3339
  risk_level?: RiskLevel
  boards_enabled?: string[]
  risk_test_expired_at?: string // RFC3339
}

// CheckRequest is the body of POST /api/compliance/check. Symbol is
// the only required field — the profile is optional and falls back
// to the server-side default.
export interface CheckRequest {
  symbol: string
  // Optional profile overrides. Production callers (paper trading
  // page, signal review) usually omit these; dev / test callers
  // supply them to exercise specific rejection paths.
  user_id?: string
  asset_daily_avg_cny?: number
  first_trade_at?: string
  risk_level?: RiskLevel
  boards_enabled?: string[]
  risk_test_expired_at?: string
}

// BoardRequirement mirrors pkg/compliance.BoardRequirement. DisplayName
// and Description are server-supplied Chinese text; the frontend
// renders them verbatim in the "为什么我不能下单" disclosure.
export interface BoardRequirement {
  Board: string
  AssetThresholdCNY: number
  ExperienceMonths: number
  RiskLevel: RiskLevel
  DisplayName: string
  Description: string
}

// CheckResponse is the body returned by POST /api/compliance/check.
// The same shape is returned on both HTTP 200 and 422; the only
// difference is the HTTP status code itself.
export interface CheckResponse {
  allowed: boolean
  board: string
  board_name?: string
  reasons: string[]
  user_id: string
  profile_age_months: number
  asset_daily_avg_cny: number
  risk_level: string
  required?: BoardRequirement
  checked_at: string
}

// RequirementsResponse is the body of GET /api/compliance/requirements.
export interface RequirementsResponse {
  requirements: BoardRequirement[]
  generated_at: string
}

// BoardsResponse is the body of GET /api/compliance/boards.
export interface BoardsResponse {
  boards: string[]
  count: number
}

/**
 * Check whether the configured user profile is allowed to trade
 * `symbol`. Returns the structured verdict; HTTP 422 from the server
 * is surfaced as a normal resolved response (the client does NOT
 * throw on 4xx — the consumer branches on `result.allowed`).
 *
 * Throws only on transport / 5xx failures.
 */
export async function checkSuitability(req: CheckRequest): Promise<CheckResponse> {
  return api.post<CheckResponse>('/compliance/check', req)
}

/**
 * List all per-board thresholds. Used by the onboarding page to
 * render the "板块开通条件" table without hard-coding the numbers
 * in the SPA bundle.
 */
export async function getRequirements(): Promise<RequirementsResponse> {
  return api.get<RequirementsResponse>('/compliance/requirements')
}

/**
 * List the boards that require a suitability precheck. Used to
 * decide whether the order form should run the precheck at all —
 * main-board orders are unconditional, so the form can skip the
 * round-trip and submit directly.
 */
export async function getBoards(): Promise<BoardsResponse> {
  return api.get<BoardsResponse>('/compliance/boards')
}
