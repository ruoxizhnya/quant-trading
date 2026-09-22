<template>
  <div class="nav-tiles">
    <router-link v-for="tile in tiles" :key="tile.to" :to="tile.to" class="nav-tile" :style="{ '--accent': tile.color }">
      <component :is="tile.icon" class="tile-icon" />
      <div class="tile-text">
        <div class="tile-title">{{ tile.title }}</div>
        <div class="tile-desc">{{ tile.desc }}</div>
      </div>
      <n-icon class="tile-arrow"><ChevronForward /></n-icon>
    </router-link>
  </div>
</template>

<script setup lang="ts">
import { h } from 'vue'
import { RouterLink } from 'vue-router'
import { NIcon } from 'naive-ui'
import {
  AnalyticsOutline,
  FlashOutline,
  SearchOutline,
  ChatbubblesOutline,
  SettingsOutline,
} from '@vicons/ionicons5'
import { ChevronForward } from '@vicons/ionicons5'

interface NavTile {
  to: string
  icon: ReturnType<typeof h>
  title: string
  desc: string
  color: string
}

const tiles: NavTile[] = [
  { to: '/backtest', icon: h(NIcon, null, () => h(AnalyticsOutline)), title: '回测引擎', desc: '单股票/组合回测分析', color: '#58a6ff' },
  { to: '/screener', icon: h(NIcon, null, () => h(SearchOutline)), title: '因子选股', desc: '多因子筛选与排名', color: '#a371f7' },
  { to: '/strategy-lab', icon: h(NIcon, null, () => h(SettingsOutline)), title: '策略实验室', desc: '策略管理与参数优化', color: '#f78166' },
  { to: '/copilot', icon: h(NIcon, null, () => h(ChatbubblesOutline)), title: 'AI Copilot', desc: '智能策略生成助手', color: '#3fb950' },
  { to: '/dashboard?tab=batch', icon: h(NIcon, null, () => h(FlashOutline)), title: '批量回测', desc: '多策略多标的对比', color: '#f85149' },
]
</script>

<style scoped>
.nav-tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }

.nav-tile {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  border-radius: var(--q-radius-md);
  border: 1px solid var(--q-border);
  cursor: pointer;
  transition: all 0.2s ease;
  text-decoration: none;
  color: inherit;
}

.nav-tile:hover {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent);
  transform: translateY(-1px);
}

.tile-icon {
  font-size: 24px;
  color: var(--accent);
  flex-shrink: 0;
}

.tile-text { flex: 1; min-width: 0; }
.tile-title { font-size: 13px; font-weight: 700; color: var(--q-text); }
.tile-desc { font-size: 11px; color: var(--q-text3); margin-top: 2px; }

.tile-arrow { font-size: 16px; color: var(--q-text3); flex-shrink: 0; }

@media (max-width: 640px) {
  .nav-tiles { grid-template-columns: 1fr; }
}
</style>
