<template>
  <header class="app-header">
    <div class="header-left">
      <n-button quaternary @click="toggleSidebar" size="small">
        <template #icon><MenuOutline :size="20" /></template>
      </n-button>
      <span class="logo"><TrendingUpOutline :size="18" color="#58a6ff" /> Quant Lab</span>
    </div>
    <div class="header-right">
      <n-space align="center" :size="16">
        <n-tag :type="apiOnline ? 'success' : 'error'" size="small" round :bordered="false">
          {{ apiOnline ? 'API: 在线' : 'API: 离线' }}
        </n-tag>
        <n-tag type="default" size="small" round :bordered="false">
          <template #icon><TimeOutline :size="13" /></template>
          {{ currentTime }}
        </n-tag>
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button quaternary circle size="small" @click="toggleTheme">
              <template #icon><MoonOutline :size="17" /></template>
            </n-button>
          </template>
          切换主题
        </n-tooltip>
      </n-space>
    </div>
  </header>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { NButton, NTag, NTooltip, NSpace, useMessage } from 'naive-ui'
import {
  MenuOutline, MoonOutline,
  TrendingUpOutline, TimeOutline,
} from '@vicons/ionicons5'

const emit = defineEmits(['toggle-sidebar'])
const message = useMessage()
const apiOnline = ref(true)
const currentTime = ref('')
let timer: ReturnType<typeof setInterval>

function toggleSidebar() { emit('toggle-sidebar') }
function toggleTheme() { message.info('主题切换功能开发中') }

function updateClock() {
  const now = new Date()
  currentTime.value = now.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
}

onMounted(() => {
  updateClock()
  timer = setInterval(updateClock, 1000)
})

onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.app-header {
  height: var(--q-header-height);
  background: var(--q-surface);
  border-bottom: 1px solid var(--q-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  flex-shrink: 0;
}

.header-left { display: flex; align-items: center; gap: 12px; }

.logo {
  font-size: 16px;
  font-weight: 700;
  color: var(--q-text);
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-right { display: flex; align-items: center; }
</style>
