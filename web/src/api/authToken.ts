// 鉴权状态里「HTTP 层需要看见」的那一面。
//
// 为什么这是一个普通模块，而不是 Pinia store：
//
//  1. `api/client.ts` 要在每个请求上带 Authorization，就得读当前 token。
//     它若 import `stores/auth`，就出现
//     `stores/auth → api/auth → api/client → stores/auth` 的环。
//  2. 更要命的是，store 只能在 `app.use(createPinia())` 之后调用。而
//     `api/client.test.ts` 等单测只 import `api/client`，从不装 Pinia ——
//     一旦 client 在请求路径上碰 store，这些测试会**全部**报
//     "getActivePinia() was called but there was no active Pinia"。
//
// 所以这里只有「值 + 通知」两件事，写入者唯一：`stores/auth`（以及
// `api/client` 在拿到 401 时清空）。读的人只有 `api/client`。
//
// 存哪儿：localStorage。access / refresh 都是无状态 JWT，服务端没有会话表
// 可失效，所以「登出」= 本地丢弃（见 stores/auth.logout 的注释）。持久化让
// 刷新页面不必重新登录；代价是 XSS 能读到 token —— 对本地/局域网工具可以
// 接受，将来要放到公网应先换成 httpOnly cookie + CSRF 方案。
//
// 注意 e2e/helpers/isolation.ts 每个用例都会 `localStorage.clear()`，
// 所以 token 不会在用例之间泄漏。

const ACCESS_KEY = 'quant.auth.access_token'
const REFRESH_KEY = 'quant.auth.refresh_token'

function store(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    // 隐私模式 / 沙箱里访问 localStorage 会抛
    return null
  }
}

export function getAccessToken(): string | null {
  return store()?.getItem(ACCESS_KEY) || null
}

export function getRefreshToken(): string | null {
  return store()?.getItem(REFRESH_KEY) || null
}

export function setTokens(accessToken: string, refreshToken: string): void {
  const s = store()
  if (!s) return
  s.setItem(ACCESS_KEY, accessToken)
  if (refreshToken) s.setItem(REFRESH_KEY, refreshToken)
}

export function clearTokens(): void {
  const s = store()
  if (!s) return
  s.removeItem(ACCESS_KEY)
  s.removeItem(REFRESH_KEY)
}

// ── 401 之后要做什么，由组合根（main.ts）决定 ──────────────────────────
//
// client.ts 只负责「发现拿到 401 且刷不回 token」，不负责跳转 —— 它连
// router 都不该认识。注册一个回调，把「怎么呈现」留给装配层。

let unauthorizedHandler: (() => void) | null = null

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

export function notifyUnauthorized(): void {
  try {
    unauthorizedHandler?.()
  } catch {
    // 处理器的失败不得影响请求本身的报错路径
  }
}
