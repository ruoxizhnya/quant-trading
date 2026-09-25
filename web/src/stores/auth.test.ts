import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { clearTokens, getAccessToken, getRefreshToken, setTokens } from '@/api/authToken'
import { ApiError } from '@/api/client'

vi.mock('@/api/auth', () => ({
  getAuthStatus: vi.fn(),
  getMe: vi.fn(),
  login: vi.fn(),
  bootstrapFirstAdmin: vi.fn(),
  refreshTokens: vi.fn(),
}))

import { bootstrapFirstAdmin, getAuthStatus, getMe, login } from '@/api/auth'

const mockedStatus = vi.mocked(getAuthStatus)
const mockedMe = vi.mocked(getMe)
const mockedLogin = vi.mocked(login)
const mockedBootstrap = vi.mocked(bootstrapFirstAdmin)

function tokens(overrides: Partial<{ access_token: string; refresh_token: string; username: string; role: string }> = {}) {
  return {
    access_token: 'access-1',
    refresh_token: 'refresh-1',
    token_type: 'Bearer',
    expires_in: 900,
    username: 'alice',
    role: 'admin',
    ...overrides,
  }
}

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    clearTokens()
    vi.clearAllMocks()
  })

  it('初始态是 unknown，且按 fail closed 当成"需要凭据"', () => {
    const store = useAuthStore()
    expect(store.posture).toBe('unknown')
    expect(store.authEnabled).toBe(true)
    expect(store.isOpenAccess).toBe(false)
    expect(store.isAuthenticated).toBe(false)
  })

  describe('probe', () => {
    it('后端说没开鉴权 ⇒ open-access，且不请求 /me', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: false, bootstrap_required: false })
      const store = useAuthStore()

      await store.probe()

      expect(store.isOpenAccess).toBe(true)
      expect(store.authEnabled).toBe(false)
      expect(store.needsFirstAdmin).toBe(false)
      expect(mockedMe).not.toHaveBeenCalled()
    })

    it('开了鉴权但没有凭据 ⇒ anonymous，窗口信息来自 /status', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: true })
      const store = useAuthStore()

      await store.probe()

      expect(store.posture).toBe('anonymous')
      expect(store.bootstrapRequired).toBe(true)
      expect(store.needsFirstAdmin).toBe(true)
      expect(mockedMe).not.toHaveBeenCalled()
    })

    it('有 token 且 /me 认账 ⇒ authenticated，用户名与角色就位', async () => {
      setTokens('access-1', 'refresh-1')
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedMe.mockResolvedValue({ user_id: 1, username: 'alice', role: 'admin' })
      const store = useAuthStore()

      await store.probe()

      expect(store.isAuthenticated).toBe(true)
      expect(store.username).toBe('alice')
      expect(store.role).toBe('admin')
      expect(store.needsFirstAdmin).toBe(false)
      expect(getAccessToken()).toBe('access-1')
    })

    it('/me 回 401 ⇒ 丢凭据、回 anonymous', async () => {
      setTokens('stale', 'refresh-1')
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedMe.mockRejectedValue(new ApiError(401, '未授权，请重新登录'))
      const store = useAuthStore()

      await store.probe()

      expect(store.posture).toBe('anonymous')
      expect(getAccessToken()).toBeNull()
      expect(getRefreshToken()).toBeNull()
    })

    it('/me 因网络失败（非 401）⇒ 保留凭据，且**不写成未登录**', async () => {
      setTokens('good-but-unconfirmed', 'refresh-1')
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedMe.mockRejectedValue(new ApiError(0, '网络连接失败'))
      const store = useAuthStore()

      await store.probe()

      // 「没问到」≠「没登录」：写 anonymous 会凭空造出「你已登出」这个事实。
      expect(store.posture).toBe('unavailable')
      // 断网不该把用户的会话抹掉：下次刷新页面网络恢复后能直接接回来
      expect(getAccessToken()).toBe('good-but-unconfirmed')
      expect(store.error).toBe('网络连接失败')
    })

    it('探测失败 ⇒ unavailable（fail closed），绝不能猜成 open-access', async () => {
      mockedStatus.mockRejectedValue(new ApiError(500, '服务器内部错误'))
      const store = useAuthStore()

      await store.probe()

      expect(store.posture).toBe('unavailable')
      // fail closed：仍然当成「需要凭据」，不许把控制台闪出来
      expect(store.isOpenAccess).toBe(false)
      expect(store.isUnavailable).toBe(true)
      expect(store.authEnabled).toBe(true)
      expect(store.error).toBeTruthy()
    })

    it('429（被限流）也走 unavailable —— 限流不是「会话失效」', async () => {
      // 2026-09-25 的真实缺陷：/api/auth/status 被限流回 429，前端 fail closed
      // 当成未登录，整个 SPA 被弹到登录页（全量 playwright 红了 127 条）。
      mockedStatus.mockRejectedValue(new ApiError(429, 'rate limit exceeded'))
      const store = useAuthStore()

      await store.probe()

      expect(store.posture).toBe('unavailable')
      expect(store.isAuthenticated).toBe(false)
    })

    it('unavailable 是**可再探**状态：再 await 一次 probe 会真的重发请求', async () => {
      // 后端恢复后不该需要用户手动刷新页面 —— 守卫每次跳转都会 await probe，
      // 所以这里必须真的再发一次，而不是被 `posture !== unknown` 挡掉。
      mockedStatus.mockRejectedValueOnce(new ApiError(500, '服务器内部错误'))
      mockedStatus.mockResolvedValueOnce({ auth_enabled: false, bootstrap_required: false })
      const store = useAuthStore()

      await store.probe()
      expect(store.posture).toBe('unavailable')

      await store.probe()
      expect(mockedStatus).toHaveBeenCalledTimes(2)
      expect(store.posture).toBe('open-access')
    })

    it('已有答复的状态（anonymous / open-access）不会被无谓地再探', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: false, bootstrap_required: false })
      const store = useAuthStore()

      await store.probe()
      await store.probe()
      await store.probe()

      expect(mockedStatus).toHaveBeenCalledTimes(1)
    })

    it('幂等：多个跳转 await 它，HTTP 只发一次', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      const store = useAuthStore()

      await Promise.all([store.probe(), store.probe(), store.probe()])
      await store.probe()

      expect(mockedStatus).toHaveBeenCalledTimes(1)
    })
  })

  describe('login / bootstrap', () => {
    it('登录成功 ⇒ 落 token、转 authenticated', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedLogin.mockResolvedValue(tokens())
      const store = useAuthStore()
      await store.probe()

      await store.login('alice', 'pw-123456')

      expect(store.isAuthenticated).toBe(true)
      expect(store.username).toBe('alice')
      expect(getAccessToken()).toBe('access-1')
      expect(getRefreshToken()).toBe('refresh-1')
      expect(store.error).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('登录失败 ⇒ 记下错误、仍是 anonymous、不留 token', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedLogin.mockRejectedValue(new ApiError(401, 'invalid username or password'))
      const store = useAuthStore()
      await store.probe()

      await expect(store.login('alice', 'wrong')).rejects.toBeTruthy()

      expect(store.posture).toBe('anonymous')
      expect(store.error).toBe('invalid username or password')
      expect(getAccessToken()).toBeNull()
      expect(store.loading).toBe(false)
    })

    it('创建首个管理员成功 ⇒ 直接进入已登录（后端会一并签发 token）', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: true })
      mockedBootstrap.mockResolvedValue(tokens())
      const store = useAuthStore()
      await store.probe()
      expect(store.needsFirstAdmin).toBe(true)

      await store.bootstrap('root', 'pw-123456')

      expect(store.isAuthenticated).toBe(true)
      expect(store.bootstrapRequired).toBe(false)
      expect(store.needsFirstAdmin).toBe(false)
    })

    it('bootstrap 被 403 拒绝（窗口已关）⇒ 翻转成登录表单 + 专门文案', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: true })
      mockedBootstrap.mockRejectedValue(new ApiError(403, 'bootstrap window is closed'))
      const store = useAuthStore()
      await store.probe()

      await expect(store.bootstrap('root', 'pw-123456')).rejects.toBeTruthy()

      // 关键：不能在「窗口已关」之后还继续显示创建表单 —— 那个表单永远不会成功
      expect(store.bootstrapRequired).toBe(false)
      expect(store.needsFirstAdmin).toBe(false)
      expect(store.error).toContain('已经创建过了')
      expect(store.posture).toBe('anonymous')
    })
  })

  describe('logout', () => {
    it('清掉凭据并回到 anonymous', async () => {
      setTokens('access-1', 'refresh-1')
      mockedStatus.mockResolvedValue({ auth_enabled: true, bootstrap_required: false })
      mockedMe.mockResolvedValue({ user_id: 1, username: 'alice', role: 'viewer' })
      const store = useAuthStore()
      await store.probe()
      expect(store.isAuthenticated).toBe(true)

      store.logout()

      expect(store.posture).toBe('anonymous')
      expect(store.username).toBeNull()
      expect(store.role).toBeNull()
      expect(getAccessToken()).toBeNull()
      expect(getRefreshToken()).toBeNull()
    })

    it('markSessionExpired 与 logout 等效，并给出原因', () => {
      setTokens('access-1', 'refresh-1')
      const store = useAuthStore()

      store.markSessionExpired()

      expect(store.posture).toBe('anonymous')
      expect(getAccessToken()).toBeNull()
      expect(store.error).toContain('过期')
    })

    it('open-access 下 logout 不会把用户变成"需要登录"', async () => {
      mockedStatus.mockResolvedValue({ auth_enabled: false, bootstrap_required: false })
      const store = useAuthStore()
      await store.probe()

      store.logout()

      // logout 会置 anonymous，但下一次 probe 之前守卫已经不再拦人（见
      // router 的 isOpenAccess 分支）—— 这里只钉住「不残留凭据」。
      expect(getAccessToken()).toBeNull()
    })
  })
})
