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
        <!--
          身份区只在**已登录**时存在。

          条件必须是 isAuthenticated 而不是 authEnabled：open-access（没配
          JWT_SECRET）的部署下这里必须一个节点都不多渲染 —— 那是本地默认形态，
          也是 playwright 视觉回归与其余 UI 用例跑的那个形态。
        -->
        <template v-if="auth.isAuthenticated">
          <n-tag size="small" round :bordered="false" type="info" data-testid="header-user">
            {{ auth.username }} · {{ roleLabel }}
          </n-tag>
          <n-tooltip trigger="hover">
            <template #trigger>
              <n-button quaternary circle size="small" data-testid="header-logout" @click="handleLogout">
                <template #icon><LogOutOutline :size="17" /></template>
              </n-button>
            </template>
            退出登录
          </n-tooltip>
        </template>
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
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NTag, NTooltip, NSpace, useMessage } from 'naive-ui'
import {
  LogOutOutline, MenuOutline, MoonOutline,
  TrendingUpOutline, TimeOutline,
} from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'

const emit = defineEmits(['toggle-sidebar'])
const message = useMessage()
const auth = useAuthStore()
const router = useRouter()
const apiOnline = ref(true)
const currentTime = ref('')
let timer: ReturnType<typeof setInterval>

const ROLE_LABELS: Record<string, string> = {
  viewer: '只读',
  trader: '交易员',
  admin: '管理员',
}
const roleLabel = computed(() => (auth.role ? ROLE_LABELS[auth.role] || auth.role : ''))

function toggleSidebar() { emit('toggle-sidebar') }
function toggleTheme() { message.info('主题切换功能开发中') }

function handleLogout() {
  // 纯本地动作：后端没有会话表可撤销（见 stores/auth.logout 的注释）。
  auth.logout()
  void router.replace({ name: 'login' })
}

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
