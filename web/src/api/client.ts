import { API_TIMEOUT, API_RETRY_DELAY, API_MAX_RETRIES } from '@/constants/api'
import {
  clearTokens,
  getAccessToken,
  getRefreshToken,
  notifyUnauthorized,
  setTokens,
} from './authToken'

export class ApiError extends Error {
  // CR-50 (ODR-012): set explicitly at throw-sites that originate from
  // an AbortController (timeout / manual signal / pagehide). The
  // previous `message.includes('abort')` substring check silently
  // returned `false` for the actual Chinese message ('请求已取消'),
  // which made `isTimeout` permanently false for cancelled requests
  // — breaking `createCancellableRequest`'s re-throw decision on
  // line 142 and any composable that branched on `e.isTimeout`.
  isAbort: boolean
  constructor(public status: number, message: string, public body?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.isAbort = false
  }

  get isClientError() { return this.status >= 400 && this.status < 500 }
  get isServerError() { return this.status >= 500 }
  get isNotFound() { return this.status === 404 }
  get isTimeout() { return this.isAbort }
  get isNetworkError() { return this.status === 0 && !this.isAbort }
}

interface RequestOptions extends RequestInit {
  timeout?: number
  retry?: number
  signal?: AbortSignal
  /**
   * 内部使用：在「刷新 token 后重放」的那一次请求上置位，防止 401 -> 刷新 ->
   * 重放 -> 又 401 -> 又刷新 的死循环。调用方不需要传。
   */
  skipAuthRefresh?: boolean
}

/**
 * 一次 401 善后的三种结局。见 ApiClient.refreshSession 的注释：把
 * 「服务端拒绝了会话」和「我们没问到」分开，是这套逻辑里唯一容易搞错的地方。
 */
type RefreshOutcome = 'refreshed' | 'rejected' | 'unavailable'

/**
 * authHeader 给出本次请求要带的鉴权头。
 *
 * 没有 token 时返回空对象 —— 这正是 open-access（未配 JWT_SECRET）形态下
 * 必须保持的行为：一个头都不加，请求与加鉴权之前**逐字节相同**。
 * 本地 compose 默认就是这个形态，`e2e/tests/rbac-open-access.spec.ts`
 * 断言此时不得出现任何 401/403。
 */
function authHeader(): Record<string, string> {
  const token = getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const STATUS_MESSAGES: Record<number, string> = {
  400: '请求参数有误',
  401: '未授权，请重新登录',
  403: '无权限访问该资源',
  404: '请求的资源不存在',
  409: '资源冲突，请刷新后重试',
  422: '数据验证失败',
  429: '请求过于频繁，请稍后再试',
  500: '服务器内部错误',
  502: '网关服务不可用',
  503: '服务暂时不可用，请稍后重试',
  504: '服务响应超时',
}

function getStatusMessage(status: number, fallback: string): string {
  return STATUS_MESSAGES[status] || fallback
}

// CR-25 (ODR-012): one pagehide listener at module scope aborts every
// in-flight controller. See request() below for the add/delete lifecycle.
const inFlightControllers = new Set<AbortController>()
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => {
    inFlightControllers.forEach(c => {
      try { c.abort() } catch { /* ignore */ }
    })
    inFlightControllers.clear()
  })
}

class ApiClient {
  private baseURL: string

  /**
   * refreshInFlight 让「并发的多个 401」只换来一次刷新。
   *
   * 仪表盘一进页面就会并发打好几个接口，token 过期时它们会同时拿到 401。
   * 没有这一层就会并发发出 N 个 /api/auth/refresh，而 refresh 端点是**轮换**
   * 语义（返回新的 refresh token），后到的请求带着已被换掉的旧 refresh token
   * 去换 —— 服务端无法区分它和重放攻击，只能拒绝，于是用户被"随机"登出。
   */
  private refreshInFlight: Promise<RefreshOutcome> | null = null

  constructor(baseURL: string = '') {
    this.baseURL = baseURL || import.meta.env.VITE_API_BASE || ''
  }

  /**
   * refreshSession 用 refresh token 换一对新 token。
   *
   * 返回三态而不是布尔 —— 因为「换不了」有两种完全不同的成因，善后动作相反：
   *
   *   - `rejected`：服务端明确拒绝（401/403，或压根没有 refresh token）。
   *     会话是真的死了 ⇒ 清凭据 + 提示重新登录。
   *   - `unavailable`：网络层失败，或刷新端点自己 5xx。**我们根本没问出口**，
   *     这不构成"会话失效"的证据 ⇒ 保留凭据，把原来的 401 照实报出去。
   *     断网不该把用户的会话抹掉；网络恢复后下一次请求会自己换回来。
   *
   * 用一个布尔把两者合并，就是这份代码最早的 bug：一次网络抖动会被读成
   * "会话过期"，用户被登出（`client.auth.test.ts` 里那条测试抓到的）。
   *
   * 用裸 fetch 而不是 this.request：走自己会再次经过 401 处理链，形成递归。
   */
  private refreshSession(): Promise<RefreshOutcome> {
    if (this.refreshInFlight) return this.refreshInFlight

    const refreshToken = getRefreshToken()
    if (!refreshToken) return Promise.resolve('rejected')

    this.refreshInFlight = (async (): Promise<RefreshOutcome> => {
      try {
        const res = await fetch(`${this.baseURL}/api/auth/refresh`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: refreshToken }),
        })
        if (!res.ok) {
          // 401/403 = 令牌被拒；5xx = 服务端自己有问题，两者不能混为一谈。
          if (res.status === 401 || res.status === 403) {
            clearTokens()
            return 'rejected'
          }
          return 'unavailable'
        }
        const body = (await res.json()) as { access_token?: string; refresh_token?: string }
        if (!body?.access_token) {
          clearTokens()
          return 'rejected'
        }
        // 服务端会轮换 refresh token；万一它没给（老版本），沿用旧的。
        setTokens(body.access_token, body.refresh_token || refreshToken)
        return 'refreshed'
      } catch {
        return 'unavailable'
      }
    })()

    return this.refreshInFlight.finally(() => {
      this.refreshInFlight = null
    }) as Promise<RefreshOutcome>
  }

  /**
   * settleUnauthorized 是 request() 与 download() 共用的 401 善后：
   * 能刷新就刷新（让调用方重放），刷不动才判会话失效。
   *
   * 只有请求发出时**确实带着 token** 才通知装配层 —— 否则登录页输错密码
   * 也会触发"会话过期"跳转（`/api/auth/login` 失败同样是 401）。
   */
  private async settleUnauthorized(
    hadToken: boolean,
    skipAuthRefresh: boolean,
  ): Promise<RefreshOutcome> {
    if (skipAuthRefresh) {
      // 已经刷新过一次了还 401：令牌刚拿到就被拒，只能判会话失效。
      this.handleUnauthorized(hadToken)
      return 'rejected'
    }
    const outcome = await this.refreshSession()
    if (outcome === 'rejected') this.handleUnauthorized(hadToken)
    // 'unavailable' 时什么都不做：凭据留着，把这次 401 原样报给调用方。
    return outcome
  }

  private handleUnauthorized(hadToken: boolean): void {
    if (!hadToken) return
    clearTokens()
    notifyUnauthorized()
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { timeout = API_TIMEOUT, retry = 0, signal, skipAuthRefresh = false, ...init } = options

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), timeout)

    if (signal) {
      signal.addEventListener('abort', () => controller.abort(), { once: true })
    }

    // 发出时带没带 token，是后面判断「这是会话失效还是我本来就没登录」的依据。
    const hadToken = getAccessToken() !== null

    // CR-25 (ODR-012): pagehide is a one-shot browser event, so we install
    // a SINGLE module-scoped listener that aborts every in-flight controller
    // registered through `inFlightControllers`. The previous implementation
    // added a fresh `addEventListener('pagehide', ...)` on every request —
    // 100 API calls = 100 lingering listeners because SPA route changes do
    // not fire pagehide.
    inFlightControllers.add(controller)
    try {
      const url = path.startsWith('http') ? path : `${this.baseURL}${path}`
      const res = await fetch(url, {
        ...init,
        signal: controller.signal,
        headers: {
          'Content-Type': 'application/json',
          // 顺序即优先级：显式传入的 init.headers 永远压过自动注入的
          // Authorization。`api/execution.ts` 的 emergency-flatten 就靠这一点
          // —— 它带的是交易用的 emergency token，不是 JWT，不能被覆盖。
          ...authHeader(),
          ...init.headers,
        },
      })

      clearTimeout(timer)

      if (!res.ok) {
        // 401：先试一次换令牌再重放原请求。用户看不到这次失败，页面也不会
        // 闪回登录页。换不动的两种情形由 settleUnauthorized 分派。
        if (res.status === 401) {
          const outcome = await this.settleUnauthorized(hadToken, skipAuthRefresh)
          if (outcome === 'refreshed') {
            return this.request<T>(path, { ...options, skipAuthRefresh: true })
          }
        }

        let errMsg: string
      let errBody: Record<string, unknown> | undefined
      try {
        errBody = await res.json()
        errMsg = (errBody?.error as string) || (errBody?.message as string) || res.statusText
      } catch {
        errMsg = res.statusText
      }
      const message = getStatusMessage(res.status, errMsg)
      throw new ApiError(res.status, message, errBody)
      }

      return await res.json()
    } catch (e: unknown) {
      clearTimeout(timer)
      if (e instanceof DOMException || (e instanceof Error && e.name === 'AbortError')) {
        // CR-50 (ODR-012): mark the error as abort-derived so `isTimeout`
        // returns true and downstream code (e.g. createCancellableRequest
        // on line 142, useAsyncBacktest, etc.) can branch correctly.
        const abortErr = new ApiError(0, '请求已取消')
        abortErr.isAbort = true
        throw abortErr
      }
      if (e instanceof ApiError) throw e
      if (retry > 0) {
        // CR-46 (ODR-012): schedule is now derived from API_MAX_RETRIES
        // instead of a magic "4". Delays are linear in *remaining*
        // retries so the first retry is the most aggressive and the
        // last is the most patient — mirrors user behaviour on flaky
        // networks ("retry quickly, then back off").
        //   remainingRetries=3 (default) -> 1s
        //   remainingRetries=2          -> 2s
        //   remainingRetries=1          -> 3s
        const remainingRetries = retry
        const delayMs = API_RETRY_DELAY * (API_MAX_RETRIES + 1 - remainingRetries)
        await new Promise(r => setTimeout(r, delayMs))
        return this.request<T>(path, { ...options, retry: retry - 1 })
      }
      throw new ApiError(0, e instanceof Error ? e.message : '网络连接失败')
    } finally {
      inFlightControllers.delete(controller)
    }
  }

  get<T>(path: string, options?: RequestOptions) {
    return this.request<T>(path, { ...options, method: 'GET' })
  }

  post<T>(path: string, body?: unknown, options?: RequestOptions) {
    return this.request<T>(path, {
      ...options,
      method: 'POST',
      body: body ? JSON.stringify(body) : undefined,
    })
  }

  delete<T>(path: string, options?: RequestOptions) {
    return this.request<T>(path, { ...options, method: 'DELETE' })
  }

  /**
   * P2-1 (ODR-027): Raw blob download — bypasses JSON parsing so the caller
   * can save binary/text content (HTML report, future PDF, CSV, ...).
   * Resolves with the blob and the response's Content-Disposition filename
   * (extracted from the header when present, otherwise empty).
   */
  async download(path: string, options: RequestOptions = {}): Promise<{ blob: Blob; filename: string }> {
    const { timeout = API_TIMEOUT, signal, skipAuthRefresh = false, ...init } = options

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), timeout)
    if (signal) {
      signal.addEventListener('abort', () => controller.abort(), { once: true })
    }
    const hadToken = getAccessToken() !== null
    inFlightControllers.add(controller)
    try {
      const url = path.startsWith('http') ? path : `${this.baseURL}${path}`
      const res = await fetch(url, {
        ...init,
        signal: controller.signal,
        // 和 request() 一样注入鉴权头：这里是全站第二个、也是最后一个
        // 直连 fetch 的出口（P2-1 的报告/CSV 下载）。只改 request() 会让
        // 下载在开鉴权的部署里全 401。
        headers: { ...authHeader(), ...init.headers },
      })
      clearTimeout(timer)
      if (!res.ok) {
        // download 是第二个 fetch 出口，401 的善后走和 request() 完全相同的
        // 那条路（刷新 -> 重放 / 判定会话失效 / 断网保留凭据），免得两处
        // 各写一份、日后只改一处。
        if (res.status === 401) {
          const outcome = await this.settleUnauthorized(hadToken, skipAuthRefresh)
          if (outcome === 'refreshed') {
            return this.download(path, { ...options, skipAuthRefresh: true })
          }
        }
        let errMsg = res.statusText
        try {
          const body = (await res.json()) as Record<string, unknown>
          errMsg = (body?.error as string) || (body?.message as string) || errMsg
        } catch { /* not JSON, keep statusText */ }
        throw new ApiError(res.status, getStatusMessage(res.status, errMsg))
      }
      const blob = await res.blob()
      const filename = extractFilename(res.headers.get('Content-Disposition'))
      return { blob, filename }
    } catch (e: unknown) {
      clearTimeout(timer)
      if (e instanceof DOMException || (e instanceof Error && e.name === 'AbortError')) {
        const abortErr = new ApiError(0, '请求已取消')
        abortErr.isAbort = true
        throw abortErr
      }
      throw e
    } finally {
      inFlightControllers.delete(controller)
    }
  }

  createCancellableRequest<T>(path: string, method: string = 'GET', body?: unknown) {
    const controller = new AbortController()
    const promise = this.request<T>(path, {
      method,
      signal: controller.signal,
      body: body ? JSON.stringify(body) : undefined,
    }).catch(e => {
      if (e instanceof ApiError && e.isTimeout) throw e
      throw e
    })
    return { promise, abort: () => controller.abort() }
  }
}

export const api = new ApiClient()
export default api

// P2-1 (ODR-027): parse `filename="x"` / `filename=x` / `filename*=UTF-8''x`
// out of a Content-Disposition header. Empty string when absent.
function extractFilename(header: string | null): string {
  if (!header) return ''
  // RFC 5987 extended form first
  const ext = header.match(/filename\*=UTF-8''([^;]+)/i)
  if (ext) {
    try {
      return decodeURIComponent(ext[1].replace(/^"|"$/g, ''))
    } catch { /* fall through */ }
  }
  const basic = header.match(/filename\s*=\s*"?([^";]+)"?/i)
  return basic ? basic[1].trim() : ''
}
