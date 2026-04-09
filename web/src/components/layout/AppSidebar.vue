<template>
  <aside class="app-sidebar" :class="{ collapsed }">
    <div class="sidebar-brand">
      <n-icon size="22" color="#58a6ff"><TrendingUpOutline /></n-icon>
      <span v-if="!collapsed" class="brand-text">Quant Lab</span>
    </div>

    <nav class="sidebar-nav">
      <router-link
        v-for="item in navItems"
        :key="item.path"
        :to="item.path"
        class="nav-item"
        :class="{ active: isActive(item.path) }"
      >
        <n-icon size="20"><component :is="item.icon" /></n-icon>
        <span v-if="!collapsed" class="nav-label">{{ item.label }}</span>
      </router-link>
    </nav>

    <div class="sidebar-footer">
      <div v-if="!collapsed" class="sys-status">
        <n-space vertical :size="4" size="small">
          <div class="sys-row">
            <span class="sys-dot online"></span>
            <span class="sys-text">Redis</span>
          </div>
          <div class="sys-row">
            <span class="sys-dot online"></span>
            <span class="sys-text">Postgres</span>
          </div>
        </n-space>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import {
  HomeOutline,
  AnalyticsOutline,
  SearchOutline,
  ChatbubbleEllipsesOutline,
  BeakerOutline,
  TrendingUpOutline,
} from '@vicons/ionicons5'
import { NSpace, NIcon } from 'naive-ui'

defineProps<{ collapsed: boolean }>()

const route = useRoute()

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}

const navItems = [
  { path: '/', label: '控制台', icon: HomeOutline },
  { path: '/backtest', label: '回测引擎', icon: AnalyticsOutline },
  { path: '/screener', label: '选股器', icon: SearchOutline },
  { path: '/copilot', label: '策略 Copilot', icon: ChatbubbleEllipsesOutline },
  { path: '/strategy-lab', label: '策略实验室', icon: BeakerOutline },
]
</script>

<style scoped>
.app-sidebar {
  width: var(--q-sidebar-width);
  background: var(--q-surface);
  border-right: 1px solid var(--q-border);
  display: flex;
  flex-direction: column;
  transition: width 0.2s ease;
  flex-shrink: 0;
  overflow: hidden;
}

.app-sidebar.collapsed { width: 60px; }

.sidebar-brand {
  height: var(--q-header-height);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  border-bottom: 1px solid var(--q-border);
}

.brand-text { font-size: 16px; font-weight: 700; color: var(--q-text); white-space: nowrap; }

.sidebar-nav {
  flex: 1;
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border-radius: var(--q-radius-sm);
  color: var(--q-text2);
  text-decoration: none;
  transition: all var(--q-transition);
  white-space: nowrap;
}

.nav-item:hover { background: var(--q-surface2); color: var(--q-text); }
.nav-item.active {
  background: rgba(88,166,255,0.12);
  color: var(--q-primary);
  box-shadow: inset 0 0 0 1px rgba(88,166,255,0.2);
}
.nav-item .nav-label { font-size: 13px; }

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--q-border);
}

.sys-row { display: flex; align-items: center; gap: 6px; font-size: 11px; }
.sys-dot { width: 6px; height: 6px; border-radius: 50%; }
.sys-dot.online { background: var(--q-success); }
.sys-dot.offline { background: var(--q-danger); }
.sys-text { color: var(--q-text3); }
</style>
