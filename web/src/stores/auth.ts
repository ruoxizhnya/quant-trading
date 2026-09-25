import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { MeResponse, TokenResponse } from '@/api/auth'
import {
  bootstrapFirstAdmin,
  getAuthStatus,
  getMe,
  login as loginRequest,
} from '@/api/auth'
import {
  clearTokens,
  getAccessToken,
  setTokens,
} from '@/api/authToken'
import { ApiError } from '@/api/client'

/**
 * 这个实例的鉴权姿势。四态，且 `unknown` 是**必须**存在的初始态：
 * 「还不知道」和「知道且没登录」要分开 —— 混成一个就会在探测完成前
 * 把已经登录的用户踢到登录页（刷新页面的那一瞬间）。
 */
export type AuthPosture =
  /** 还没探测过后端 */
  | 'unknown'
  /** 后端没配 JWT_SECRET：open-access，全站不需要凭据（本地/CI 默认形态） */
  | 'open-access'
  /** 后端开了鉴权，但我们没有可用凭据 */
  | 'anonymous'
  /**
   * 探测**拿不到答复**（429 / 5xx / 断网）。它和 `anonymous` 是两件事，别合并：
   *   - `anonymous` = 后端**说了**「要凭据」，而我们没有；
   *   - `unavailable` = 后端**没说话**。
   * 把后者当前者，就是**凭空造出「你已登出」这个事实** —— 一次限流或一次网络
   * 抖动会把已经登录的用户弹到登录页（2026-09-25 的真实缺陷：`/api/auth/status`
   * 被限流回 429，全量 playwright 因此红了 127 条）。
   * 处置仍然是 **fail closed**（照样拦住受保护页面），但界面必须说「连不上」，
   * 不能摆出一个按下去也不会成功的凭据表单。
   */
  | 'unavailable'
  /** 有凭据且后端确认有效 */
  | 'authenticated'

export const useAuthStore = defineStore('auth', () => {
  // ── State ──────────────────────────────────────────────────────────
  const posture = ref<AuthPosture>('unknown')
  const bootstrapRequired = ref(false)
  const username = ref<string | null>(null)
  const role = ref<string | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  let probeInFlight: Promise<void> | null = null

  // ── Getters ────────────────────────────────────────────────────────
  const isOpenAccess = computed(() => posture.value === 'open-access')
  const isAuthenticated = computed(() => posture.value === 'authenticated')
  /**
   * authEnabled 表达「这个部署需不需要凭据」。unknown / unavailable 时返回 true
   * （fail closed）：探测没回来、或者根本没问到，都把界面当成需要凭据 ——
   * 宁可多显示一瞬登录页，也不要先把控制台闪出来再把人踢走。
   */
  const authEnabled = computed(() => posture.value !== 'open-access')
  /** 探测拿不到答复：界面该说「连不上」，而不是摆一个用不了的凭据表单。 */
  const isUnavailable = computed(() => posture.value === 'unavailable')
  /** 首个管理员窗口：只在这个状态下登录页显示「创建管理员」而不是「登录」。 */
  const needsFirstAdmin = computed(() => bootstrapRequired.value && posture.value !== 'authenticated')

  // ── Actions ────────────────────────────────────────────────────────

  /**
   * probe 问一次后端「你是什么姿势」，顺带确认手上 token 还有效。
   *
   * 幂等且并发安全：路由守卫在每个跳转上都会 await 它，但真正发出的
   * HTTP 只有第一批（最多两次：/status 与 /me）。
   *
   * 探测失败**仍然 fail closed**（绝不猜「大概是 open-access 吧」—— 猜错的
   * 后果是把受保护的界面在没登录的情况下渲染出来），但结论要分清：
   *   - 后端说 auth 关着 ⇒ open-access，全站放行；
   *   - 后端不可达 / 429 / 5xx ⇒ `unavailable` —— 这是「没问到」，不是「没登录」。
   *
   * `unavailable` 属于**可再探**状态：守卫每次跳转都会 await 它，所以后端一
   * 恢复，下一次跳转就把会话接回来，用户不必手动刷新。
   */
  async function probe(): Promise<void> {
    // unknown（还没问过）与 unavailable（没问到）都要真的发请求；其余状态
    // 已经是有答复的结论，重复问只是白花一个请求。
    if (posture.value !== 'unknown' && posture.value !== 'unavailable') return
    if (probeInFlight) return probeInFlight

    probeInFlight = (async () => {
      try {
        const status = await getAuthStatus()
        if (!status.auth_enabled) {
          posture.value = 'open-access'
          bootstrapRequired.value = false
          return
        }
        bootstrapRequired.value = status.bootstrap_required

        if (!getAccessToken()) {
          posture.value = 'anonymous'
          return
        }
        // 有 token：让服务端说它还认不认。不本地解 JWT —— 解出来的
        // exp/role 是未经验证的声明，用它渲染界面等于把过期判断交给客户端时钟。
        await adoptExistingToken()
      } catch (err) {
        // 「没问到」不是「没登录」。见 AuthPosture 的注释。
        posture.value = 'unavailable'
        error.value = err instanceof Error ? err.message : '无法连接后端服务'
      }
    })()

    try {
      await probeInFlight
    } finally {
      probeInFlight = null
    }
  }

  async function adoptExistingToken(): Promise<void> {
    try {
      const me: MeResponse = await getMe()
      username.value = me.username
      role.value = me.role
      posture.value = 'authenticated'
      bootstrapRequired.value = false
      error.value = null
    } catch (err) {
      const rejected = err instanceof ApiError && err.status === 401
      // 只有服务端明确说「这个 token 不认」才丢凭据。断网 / 5xx 时**保留**：
      // 下次刷新页面（probe 重跑）网络若已恢复，/me 会把会话直接接回来，
      // 用户不必因为一次网络抖动重新登录。
      if (rejected) clearTokens()
      // 同上：拿不到答复就是拿不到答复，别写成「你已登出」。
      posture.value = rejected ? 'anonymous' : 'unavailable'
      if (!rejected) {
        error.value = err instanceof Error ? err.message : '无法确认登录状态'
      }
    }
  }

  async function login(user: string, password: string): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const tokens = await loginRequest(user, password)
      applyTokens(tokens)
    } catch (err) {
      error.value = err instanceof Error ? err.message : '登录失败'
      throw err
    } finally {
      loading.value = false
    }
  }

  /**
   * bootstrap 创建首个管理员并直接登录。
   *
   * 后端只在 `users` 表为空时接受（返回 403 之后窗口永久关闭），所以这里
   * 的失败信息要能区分「被别人抢先了」和「参数不对」—— 403 单独说清楚，
   * 否则用户会一直重试一个不可能成功的表单。
   */
  async function bootstrap(user: string, password: string): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const tokens = await bootstrapFirstAdmin(user, password)
      applyTokens(tokens)
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        bootstrapRequired.value = false
        error.value = '首个管理员已经创建过了，请直接登录'
      } else {
        error.value = err instanceof Error ? err.message : '创建管理员失败'
      }
      throw err
    } finally {
      loading.value = false
    }
  }

  /**
   * logout 只是**本地**丢弃凭据。
   *
   * 后端没有会话表也没有吊销端点：access / refresh 都是无状态 JWT，服务端
   * 只验签名与 exp。所以「登出」不可能让已经泄漏出去的 token 失效 —— 这是
   * 当前设计的已知边界，不是遗漏。要真正吊销得先加 jti 黑名单或短 TTL +
   * 会话表，属于另一个任务。
   */
  function logout(): void {
    clearTokens()
    username.value = null
    role.value = null
    posture.value = 'anonymous'
    error.value = null
  }

  /** 会话失效（client.ts 在 401 且刷不回来时回调）。与 logout 同义，分开命名只为可读性。 */
  function markSessionExpired(): void {
    logout()
    error.value = '登录已过期，请重新登录'
  }

  function applyTokens(tokens: TokenResponse): void {
    setTokens(tokens.access_token, tokens.refresh_token)
    username.value = tokens.username ?? null
    role.value = tokens.role ?? null
    posture.value = 'authenticated'
    bootstrapRequired.value = false
    error.value = null
  }

  function clearError(): void {
    error.value = null
  }

  return {
    // State
    posture,
    bootstrapRequired,
    username,
    role,
    loading,
    error,
    // Getters
    isOpenAccess,
    isAuthenticated,
    isUnavailable,
    authEnabled,
    needsFirstAdmin,
    // Actions
    probe,
    login,
    bootstrap,
    logout,
    markSessionExpired,
    clearError,
  }
})
