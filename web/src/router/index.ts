import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
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
        path: 'strategy-lab',
        name: 'strategy-lab',
        component: () => import('@/pages/StrategyLab.vue'),
        meta: { title: '策略实验室' },
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

router.beforeEach((to) => {
  document.title = `${to.meta.title || 'Quant Lab'} — Quant Lab`
})

export default router
