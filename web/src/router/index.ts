import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { authGuard } from './authGuard'

const routes: RouteRecordRaw[] = [
  {
    // 登录页在 AppLayout **之外**：没有侧边栏/顶栏可点，也不该有 —— 那时
    // 用户还没有任何可导航的身份。
    path: '/login',
    name: 'login',
    component: () => import('@/pages/Login.vue'),
    // public 由下面的守卫读：这一条是唯一允许「未登录也能停在上面」的路由，
    // 少了它就会守卫把自己重定向到自己的死循环。
    meta: { title: '登录', public: true },
  },
  {
    path: '/',
    component: () => import('@/components/layout/AppLayout.vue'),
    children: [
      {
        path: '',
        name: 'dashboard',
        component: () => import('@/pages/Dashboard.vue'),
        meta: { title: '控制台' },
      },
      {
        path: 'backtest',
        name: 'backtest',
        component: () => import('@/pages/BacktestEngine.vue'),
        meta: { title: '回测引擎' },
      },
      {
        // P2-2 (ODR-027): multi-strategy comparison page. Must be
        // registered BEFORE any `/backtest/:id` style catch-all if one
        // is added later. For now it sits next to the engine page
        // because both are static paths.
        path: 'backtest/compare',
        name: 'backtest-compare',
        component: () => import('@/pages/BacktestCompare.vue'),
        meta: { title: '多策略对比' },
      },
      {
        path: 'screener',
        name: 'screener',
        component: () => import('@/pages/Screener.vue'),
        meta: { title: '选股器' },
      },
      {
        path: 'copilot',
        name: 'copilot',
        component: () => import('@/pages/Copilot.vue'),
        meta: { title: '策略 Copilot' },
      },
      {
        // P1-3: 探索观察台 —— 看见 AI 正在试什么、试得怎么样，并随时叫停。
        path: 'explore',
        name: 'explore',
        component: () => import('@/pages/Explore.vue'),
        meta: { title: '探索观察台' },
      },
      {
        path: 'strategy-lab',
        name: 'strategy-lab',
        component: () => import('@/pages/StrategyLab.vue'),
        meta: { title: '策略实验室' },
      },
      {
        // P3: visual drag-drop multi-factor strategy builder.
        path: 'strategy-builder',
        name: 'strategy-builder',
        component: () => import('@/pages/StrategyBuilder.vue'),
        meta: { title: '策略编辑器' },
      },
      {
        path: 'data-sync',
        name: 'data-sync',
        component: () => import('@/pages/DataSync.vue'),
        meta: { title: '数据同步' },
      },
      {
        path: 'alerts',
        name: 'alerts',
        component: () => import('@/pages/Alerts.vue'),
        meta: { title: '风险告警' },
      },
      {
        // ADR-022 §5: L0 evidence lookup — content_hash -> ingest.raw row.
        path: 'evidence',
        name: 'evidence',
        component: () => import('@/pages/Evidence.vue'),
        meta: { title: '证据查询' },
      },
      {
        // TASKS.md P5-3: factor_cache row -> its evidence coordinates, with
        // one-click hand-off to /evidence?content_hash=...
        path: 'factors',
        name: 'factors',
        component: () => import('@/pages/FactorCitation.vue'),
        meta: { title: '因子证据' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/pages/NotFound.vue'),
    meta: { title: '页面不存在' },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 守卫本体在 ./authGuard.ts（单独成文件是为了能被单测直接调用）。
// useAuthStore() 在守卫里是安全的：main.ts 先 app.use(createPinia()) 再
// app.use(router)，首次导航在 mount 时才发生。
router.beforeEach(async (to) => {
  document.title = `${to.meta.title || 'Quant Lab'} — Quant Lab`
  return authGuard(to)
})

export default router
