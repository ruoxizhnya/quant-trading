import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api } from '@/api/client'
import {
  clearTokens,
  getAccessToken,
  getRefreshToken,
  setTokens,
  setUnauthorizedHandler,
} from '@/api/authToken'

// 这一层把「凭据怎么进入 HTTP」的契约钉死。三条最要紧的性质：
//
//  1. **没有 token 时一个头都不加**。open-access（没配 JWT_SECRET）是本地与
//     CI 的默认形态，e2e/tests/rbac-open-access.spec.ts 断言此时不得出现任何
//     401/403。任何"顺手把 Authorization 加上、值可能是空串"的写法都会破坏
//     它 —— 那正是本文件第一条测试存在的理由。
//  2. 显式传入的 Authorization **压过**自动注入。execution.ts 的
//     emergency-flatten 带的是交易用的 emergency token（不是 JWT），被覆盖
//     等于要命。
//  3. 401 → 刷新 → 重放只发生一次，且并发 401 只换一次令牌。refresh 端点是
//     轮换语义：并发换两次会让后到的那次带着已作废的 token，用户被随机登出。

type Handler = (url: string, init: RequestInit) => Response

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'Err',
    json: () => Promise.resolve(body),
  } as Response
}

function headerOf(init: RequestInit): Record<string, string> {
  return (init.headers || {}) as Record<string, string>
}

/** 按 URL 分派的 fetch 假体。返回 mock 以便断言调用次数与顺序。 */
function installFetch(handler: Handler) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
    Promise.resolve(handler(String(input), init || {})),
  )
  vi.stubGlobal('fetch', mock)
  return mock
}

describe('Authorization 注入', () => {
  let seen: Array<{ url: string; init: RequestInit }>

  beforeEach(() => {
    clearTokens()
    seen = []
    installFetch((url, init) => {
      seen.push({ url, init })
      return jsonResponse({ ok: 1 })
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearTokens()
  })

  it('open-access：没有 token 时不含 Authorization 头', async () => {
    await api.get('/api/strategies')
    expect(headerOf(seen[0].init).Authorization).toBeUndefined()
  })

  it('反证腿：同一个请求在有 token 时必须带上 Bearer', async () => {
    setTokens('tok-1', 'refresh-1')
    await api.get('/api/strategies')
    expect(headerOf(seen[0].init).Authorization).toBe('Bearer tok-1')
  })

  it('Content-Type 仍然存在（注入没有挤掉它）', async () => {
    setTokens('tok-1', 'refresh-1')
    await api.post('/api/x', { a: 1 })
    expect(headerOf(seen[0].init)['Content-Type']).toBe('application/json')
  })

  it('显式传入的 Authorization 压过自动注入（emergency token 不被 JWT 覆盖）', async () => {
    setTokens('tok-1', 'refresh-1')
    await api.post('/api/execution/emergency-flatten', { reason: 'x' }, {
      headers: { Authorization: 'Bearer EMERGENCY-TOKEN' },
    })
    expect(headerOf(seen[0].init).Authorization).toBe('Bearer EMERGENCY-TOKEN')
  })

  it('download() 是第二个 fetch 出口，同样必须注入', async () => {
    setTokens('tok-1', 'refresh-1')
    const mock = installFetch((_url, _init) => ({
      ok: true,
      status: 200,
      statusText: 'OK',
      headers: { get: () => null },
      blob: () => Promise.resolve(new Blob(['x'])),
    }) as unknown as Response)

    await api.download('/api/backtest/1/report.html')
    const init = mock.mock.calls[0][1] as RequestInit
    expect(headerOf(init).Authorization).toBe('Bearer tok-1')
  })
})

describe('401 -> 刷新 -> 重放', () => {
  beforeEach(() => {
    clearTokens()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    clearTokens()
    setUnauthorizedHandler(null)
  })

  it('access token 过期时自动换令牌并重放，调用方只看到成功', async () => {
    setTokens('stale-access', 'refresh-1')
    const calls: string[] = []
    const mock = installFetch((url, init) => {
      const auth = headerOf(init).Authorization
      calls.push(`${url}#${auth ?? 'none'}`)
      if (url.endsWith('/api/auth/refresh')) {
        expect(JSON.parse(String(init.body))).toEqual({ refresh_token: 'refresh-1' })
        return jsonResponse({ access_token: 'fresh-access', refresh_token: 'refresh-2' })
      }
      return auth === 'Bearer fresh-access'
        ? jsonResponse({ ok: 'replayed' })
        : jsonResponse({ error: 'token expired' }, 401)
    })

    const res = await api.get<{ ok: string }>('/api/strategies')
    expect(res.ok).toBe('replayed')
    expect(mock).toHaveBeenCalledTimes(3)
    // 重放带着**新**令牌，而不是旧的那个
    expect(calls[2]).toBe('/api/strategies#Bearer fresh-access')
    expect(getAccessToken()).toBe('fresh-access')
    expect(getRefreshToken()).toBe('refresh-2')
  })

  it('刷新失败时清空凭据并通知装配层，调用方拿到 401', async () => {
    setTokens('stale-access', 'refresh-1')
    const notified = vi.fn()
    setUnauthorizedHandler(notified)
    installFetch((url) =>
      url.endsWith('/api/auth/refresh')
        ? jsonResponse({ error: 'invalid refresh token' }, 401)
        : jsonResponse({ error: 'token expired' }, 401),
    )

    await expect(api.get('/api/strategies')).rejects.toBeInstanceOf(ApiError)
    expect(getAccessToken()).toBeNull()
    expect(getRefreshToken()).toBeNull()
    expect(notified).toHaveBeenCalledTimes(1)
  })

  it('重放又 401 时不再刷新（不死循环），只报错一次', async () => {
    setTokens('stale-access', 'refresh-1')
    let refreshCount = 0
    const mock = installFetch((url) => {
      if (url.endsWith('/api/auth/refresh')) {
        refreshCount++
        return jsonResponse({ access_token: 'fresh-access', refresh_token: 'refresh-2' })
      }
      return jsonResponse({ error: 'nope' }, 401)
    })

    await expect(api.get('/api/strategies')).rejects.toMatchObject({ status: 401 })
    expect(refreshCount).toBe(1)
    expect(mock).toHaveBeenCalledTimes(3)
  })

  it('本来就没有 token 的 401 不触发会话失效（否则登录页输错密码会被"登出"）', async () => {
    const notified = vi.fn()
    setUnauthorizedHandler(notified)
    const mock = installFetch(() => jsonResponse({ error: 'invalid username or password' }, 401))

    await expect(api.post('/api/auth/login', { username: 'a', password: 'b' })).rejects.toMatchObject(
      { status: 401 },
    )
    expect(notified).not.toHaveBeenCalled()
    // 也没去调 refresh
    expect(mock).toHaveBeenCalledTimes(1)
  })

  it('网络失败（非 2xx 以外的异常）不当作会话失效', async () => {
    setTokens('stale-access', 'refresh-1')
    const mock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: 'expired' }, 401))
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
    vi.stubGlobal('fetch', mock)

    await expect(api.get('/api/strategies')).rejects.toMatchObject({ status: 401 })
    // 断网不该把用户的凭据删掉
    expect(getAccessToken()).toBe('stale-access')
  })

  it('并发 401 只换一次令牌（refresh 是轮换语义）', async () => {
    setTokens('stale-access', 'refresh-1')
    let refreshCount = 0
    installFetch((url, init) => {
      if (url.endsWith('/api/auth/refresh')) {
        refreshCount++
        return jsonResponse({ access_token: 'fresh-access', refresh_token: 'refresh-2' })
      }
      return headerOf(init).Authorization === 'Bearer fresh-access'
        ? jsonResponse({ ok: 1 })
        : jsonResponse({ error: 'expired' }, 401)
    })

    const results = await Promise.all([
      api.get('/api/a'),
      api.get('/api/b'),
      api.get('/api/c'),
    ])
    expect(results).toHaveLength(3)
    expect(refreshCount).toBe(1)
  })
})
