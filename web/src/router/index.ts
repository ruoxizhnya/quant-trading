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
        path: 'paper-trading',
        name: 'paper-trading',
        component: () => import('@/pages/PaperTrading.vue'),
        meta: { title: '模拟交易' },
      },
      {
        path: 'alerts',
        name: 'alerts',
        component: () => import('@/pages/Alerts.vue'),
        meta: { title: '风险告警' },
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
