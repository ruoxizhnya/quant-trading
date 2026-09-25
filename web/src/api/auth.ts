// 鉴权域的 API —— 后端是 `cmd/analysis/handlers_auth.go`。
//
// 三个端点，其中两个是**公开**的（见 handlers_auth.go 里各自的注释）：
//
//   GET  /api/auth/status     鉴权开没开、首个管理员窗口还开着吗
//   POST /api/auth/bootstrap  一次性创建首个管理员（窗口关了就永远 403）
//   POST /api/auth/login      username + password -> access/refresh
//
// `/api/auth/status` 必须在**没有任何凭据**的情况下可调用：SPA 用它决定
// 「显示登录页」还是「显示创建首个管理员」，而这个决定发生在登录之前。
// 后端把它列在 pkg/auth 的 publicPaths 白名单里；那张名单和路由表是否一致
// 由 cmd/analysis/auth_public_paths_test.go 机器校验。

import api from './client'

export interface AuthStatus {
  auth_enabled: boolean
  bootstrap_required: boolean
}

export interface TokenResponse {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
  username?: string
  role?: string
}

export interface MeResponse {
  user_id: number
  username: string
  role: string
}

/** 公开。返回该实例的鉴权姿势；失败由调用方按「fail closed」处理。 */
export async function getAuthStatus(): Promise<AuthStatus> {
  return api.get<AuthStatus>('/api/auth/status')
}

/** 公开（仅当 users 表为空时可用一次）。成功后直接返回 token，无需再登录。 */
export async function bootstrapFirstAdmin(
  username: string,
  password: string,
): Promise<TokenResponse> {
  return api.post<TokenResponse>('/api/auth/bootstrap', { username, password })
}

export async function login(username: string, password: string): Promise<TokenResponse> {
  return api.post<TokenResponse>('/api/auth/login', { username, password })
}

/** 需要凭据。用来在页面刷新后确认手上的 token 还有效。 */
export async function getMe(): Promise<MeResponse> {
  return api.get<MeResponse>('/api/auth/me')
}

/** 用 refresh token 换一对新 token。由 client.ts 在 401 时驱动，UI 不直接调。 */
export async function refreshTokens(refreshToken: string): Promise<TokenResponse> {
  return api.post<TokenResponse>('/api/auth/refresh', { refresh_token: refreshToken })
}
