import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { RouteLocationNormalized } from 'vue-router'
import { authGuard } from './authGuard'
import { clearTokens, getAccessToken, setTokens } from '@/api/authToken'
import { ApiError } from '@/api/client'

vi.mock('@/api/auth', () => ({
  getAuthStatus: vi.fn(),
  getMe: vi.fn(),
  login: vi.fn(),
  bootstrapFirstAdmin: vi.fn(),
  refreshTokens: vi.fn(),
}))

import { getAuthStatus, getMe } from '@/api/auth'

const mockedStatus = vi.mocked(getAuthStatus)
const mockedMe = vi.mocked(getMe)

// 只需要 meta 与 fullPath 两处 —— 守卫就是为了不依赖整个 RouteLocation 才
// 单独成文件的。
function route(fullPath: string, meta: Record<string, unknown> = {}): RouteLocationNormalized {
  return { fullPath, meta } as unknown as RouteLocationNormalized
}

describe('authGuard', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    clearTokens()
    vi.clearAllMocks()
  })

  it('open-access：受保护页面照样放行（默认部署形态，一个跳转都不拦）', async () => {
    mockedStatus.mockResolvedValue({ auth_enabled: false, bootstrap_required: false })

    const result = await authGuard(route('/backtest'))

    expect(result).toBe(true)
    expect(mockedMe).not.toHaveBeenCalled()
  })

  it('开了鉴权且没有凭据：受保护页面重定向到 /login 并带上原目标', async () => {
    mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })

    const result = await authGuard(route('/backtest/compare'))

    expect(result).toEqual({ name: 'login', query: { redirect: '/backtest/compare' } })
  })

  it('根路径不带 redirect 参数（回到首页不需要"原目标"）', async () => {
    mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })

    expect(await authGuard(route('/'))).toEqual({ name: 'login' })
  })

  it('登录页是 public，未登录也放行 —— 否则守卫会把自己重定向到自己', async () => {
    mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })

    const result = await authGuard(route('/login', { public: true }))

    expect(result).toBe(true)
  })

  it('有有效凭据：放行', async () => {
    setTokens('access-1', 'refresh-1')
    mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
    mockedMe.mockResolvedValue({ user_id: 1, username: 'alice', role: 'viewer' })

    expect(await authGuard(route('/screener'))).toBe(true)
  })

  it('探测失败（后端不可达）时按未登录处理，而不是放行', async () => {
    mockedStatus.mockRejectedValue(new Error('boom'))

    const result = await authGuard(route('/screener'))

    expect(result).toEqual({ name: 'login', query: { redirect: '/screener' } })
  })

  it('429 被限流时仍然拦截（fail closed），但**不抹掉用户的凭据**', async () => {
    // 这条钉的是「限流 ≠ 会话失效」：拦住是对的，丢 token 是错的 ——
    // 丢掉之后后端一恢复用户就得重新登录，而 429 只是让他等一会儿。
    setTokens('still-good', 'refresh-1')
    mockedStatus.mockRejectedValue(new ApiError(429, 'rate limit exceeded'))

    const result = await authGuard(route('/screener'))

    expect(result).toEqual({ name: 'login', query: { redirect: '/screener' } })
    expect(getAccessToken()).toBe('still-good')
  })

  it('后端恢复后自动放行：unavailable 会被下一次跳转再探一次', async () => {
    mockedStatus.mockRejectedValueOnce(new Error('ECONNREFUSED'))
    mockedStatus.mockResolvedValueOnce({ auth_enabled: false, bootstrap_required: false })

    // 第一次：后端没起来，拦住
    expect(await authGuard(route('/screener'))).toEqual({
      name: 'login',
      query: { redirect: '/screener' },
    })
    // 第二次：后端回来了，不需要用户手动刷新页面
    expect(await authGuard(route('/screener'))).toBe(true)
    expect(mockedStatus).toHaveBeenCalledTimes(2)
  })

  it('每次跳转都 await probe，但同一个页面加载只探一次', async () => {
    mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })

    await authGuard(route('/a'))
    await authGuard(route('/b'))
    await authGuard(route('/c'))

    expect(mockedStatus).toHaveBeenCalledTimes(1)
  })
})
