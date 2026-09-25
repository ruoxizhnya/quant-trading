import type { RouteLocationNormalized } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

/**
 * 全站唯一的「要不要凭据」判断点。
 *
 * 单独成文件（而不是内联在 router/index.ts 里）是为了能被单测直接调用：
 * 这个分支判错的后果是整站不可用 —— open-access 误判成需要登录会让默认
 * 部署（本地/CI）和 playwright 全站用例一起跳登录页。
 *
 * 三个分支的顺序是有理由的，别重排：
 *
 *  1. `isOpenAccess` 先走。这个部署根本没装门（未配 JWT_SECRET，本地 compose
 *     的默认形态），一个跳转都不该拦 —— 它保证"默认形态下前端行为与加鉴权
 *     之前逐字节相同"，也是 e2e/tests/rbac-open-access.spec.ts 的前提。
 *  2. `meta.public`（目前只有 /login）。少了它，守卫会把自己重定向到自己，
 *     形成死循环。
 *  3. 其余：没有凭据就去登录页，并带上原目标以便登录后跳回。
 *
 * 每次跳转都 `await probe()`，但 probe 本身幂等（见 stores/auth）—— 每个
 * 页面加载只真的发一次 HTTP。放在守卫里而不是启动流程里，是因为跳转发生在
 * 挂载那一刻：探测若挂在别处就会和首次渲染赛跑，输的那个会先把受保护的页面
 * 渲染出来再被踢走（可见闪烁），或者更糟，先渲染、再也不踢。
 */
export async function authGuard(to: RouteLocationNormalized) {
  const auth = useAuthStore()
  await auth.probe()

  if (auth.isOpenAccess) return true
  if (to.meta.public) return true
  if (!auth.isAuthenticated) {
    return to.fullPath === '/' || to.fullPath === ''
      ? { name: 'login' }
      : { name: 'login', query: { redirect: to.fullPath } }
  }
  return true
}
