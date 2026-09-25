// Login.test.ts —— 钉住「探测没问到」时这一页到底渲染什么。
//
// 这是本次修复**用户可见的那一半**：429/断网时不能摆出一个按下去也不会成功的
// 凭据表单，要说「连不上」并给重试。stores/auth 那边已经单测了姿势分类，
// 这里管的是分类之后的渲染分支 —— 两者缺一个，缺陷都能悄悄回来。
//
// 另外两条分支（创建首个管理员 / 登录）也各钉一条：它们与「连不上」共用
// 同一个 `isBootstrap` 计算属性，改坏优先级会互相影响。

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

// vi.mock 会被提升到 import 之前，所以共享状态必须用 vi.hoisted 起。
const holder = vi.hoisted(() => ({ current: null as Record<string, unknown> | null }))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: vi.fn().mockResolvedValue(undefined) }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => holder.current,
}))

import Login from './Login.vue'

type Store = {
  isUnavailable: boolean
  isAuthenticated: boolean
  isOpenAccess: boolean
  needsFirstAdmin: boolean
  bootstrapRequired: boolean
  loading: boolean
  error: string | null
  probe: ReturnType<typeof vi.fn>
  login: ReturnType<typeof vi.fn>
  bootstrap: ReturnType<typeof vi.fn>
}

function makeStore(overrides: Partial<Store> = {}): Store {
  return {
    isUnavailable: false,
    isAuthenticated: false,
    isOpenAccess: false,
    needsFirstAdmin: false,
    bootstrapRequired: false,
    loading: false,
    error: null,
    probe: vi.fn().mockResolvedValue(undefined),
    login: vi.fn(),
    bootstrap: vi.fn(),
    ...overrides,
  }
}

async function render(store: Store) {
  holder.current = store as unknown as Record<string, unknown>
  const wrapper = mount(Login)
  await flushPromises()
  return wrapper
}

describe('Login.vue', () => {
  beforeEach(() => {
    holder.current = null
  })

  it('探测没问到（unavailable）⇒ 不摆凭据表单，只给「连不上」与重试', async () => {
    const wrapper = await render(makeStore({ isUnavailable: true, error: 'rate limit exceeded' }))

    expect(wrapper.find('[data-testid="login-title"]').text()).toContain('连不上')
    // 关键断言：账号密码框**一个都不许在** —— 我们连「要不要凭据」都还不知道。
    expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="login-password"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="login-submit"]').exists()).toBe(false)
    // 替代品是一个可用的动作。
    expect(wrapper.find('[data-testid="login-unavailable"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="login-retry"]').exists()).toBe(true)
  })

  it('重试按钮会真的再探一次', async () => {
    const store = makeStore({ isUnavailable: true })
    const wrapper = await render(store)
    store.probe.mockClear()

    await wrapper.find('[data-testid="login-retry"]').trigger('click')
    await flushPromises()

    expect(store.probe).toHaveBeenCalledTimes(1)
  })

  it('unavailable 优先于「创建首个管理员」：不知道就别猜是首次运行', async () => {
    // bootstrapRequired 可能是上一个成功探测留下的陈旧值 —— 不能因此
    // 在连不上的时候摆出创建表单。
    const wrapper = await render(
      makeStore({ isUnavailable: true, needsFirstAdmin: true, bootstrapRequired: true }),
    )

    expect(wrapper.find('[data-testid="login-title"]').text()).toContain('连不上')
    expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(false)
  })

  it('可答复且窗口开着 ⇒ 创建首个管理员表单', async () => {
    const wrapper = await render(makeStore({ needsFirstAdmin: true, bootstrapRequired: true }))

    expect(wrapper.find('[data-testid="login-title"]').text()).toContain('创建首个管理员')
    expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="login-confirm"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="login-retry"]').exists()).toBe(false)
  })

  it('可答复且窗口关着 ⇒ 登录表单', async () => {
    const wrapper = await render(makeStore({ bootstrapRequired: false }))

    expect(wrapper.find('[data-testid="login-title"]').text()).toBe('登录')
    expect(wrapper.find('[data-testid="login-username"]').exists()).toBe(true)
    // 登录表单没有「确认密码」——那个只在创建时才有。
    expect(wrapper.find('[data-testid="login-confirm"]').exists()).toBe(false)
  })
})
