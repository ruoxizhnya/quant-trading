<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { useSyncStore } from '@/stores/sync'
import {
  NCard,
  NSpace,
  NButton,
  NTag,
  NSpin,
  NAlert,
  NProgress,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NTime,
} from 'naive-ui'
import {
  SyncOutline,
  RefreshOutline,
  ListOutline,
  RadioOutline,
  RadioButtonOffOutline,
} from '@vicons/ionicons5'

const store = useSyncStore()

let pollInterval: number | null = null

const statusType = computed(() => {
  if (!store.syncStatus) return 'default'
  return store.syncStatus.is_running ? 'warning' : 'success'
})

const statusText = computed(() => {
  if (!store.syncStatus) return '未知'
  return store.syncStatus.is_running ? '同步中' : '空闲'
})

const progressPercent = computed(() => {
  if (!store.syncStatus?.current_job) return 0
  const job = store.syncStatus.current_job
  if (job.total === 0) return 0
  return Math.round((job.progress / job.total) * 100)
})

onMounted(() => {
  store.fetchSyncStatus()
  // Connect SSE for real-time updates
  store.connectSSE()
  // Fallback polling every 5 seconds if SSE is not connected
  pollInterval = window.setInterval(() => {
    if (!store.sseConnected) {
      store.fetchSyncStatus()
    }
  }, 5000)
})

onUnmounted(() => {
  store.disconnectSSE()
  if (pollInterval) {
    clearInterval(pollInterval)
  }
})

async function handleRefresh() {
  await store.fetchSyncStatus()
}

function toggleSSE() {
  if (store.sseConnected) {
    store.disconnectSSE()
  } else {
    store.connectSSE()
  }
}
</script>

<template>
  <NCard title="同步状态" embedded>
    <template #header-extra>
      <NSpace align="center">
        <NTag
          :type="store.sseConnected ? 'success' : 'default'"
          size="small"
          round
        >
          <template #icon>
            <RadioOutline v-if="store.sseConnected" />
            <RadioButtonOffOutline v-else />
          </template>
          {{ store.sseConnected ? '实时' : '轮询' }}
        </NTag>
        <NButton
          quaternary
          circle
          size="small"
          @click="handleRefresh"
          :loading="store.isLoading"
        >
          <template #icon>
            <RefreshOutline />
          </template>
        </NButton>
      </NSpace>
    </template>

    <NSpin :show="store.isLoading">
      <NSpace vertical>
        <NAlert
          v-if="store.error"
          type="error"
          closable
          @close="store.clearError"
        >
          {{ store.error }}
        </NAlert>

        <NSpace align="center">
          <NTag :type="statusType" size="large">
            <template #icon>
              <SyncOutline />
            </template>
            {{ statusText }}
          </NTag>
          <NTag v-if="store.queueLength > 0" type="info" size="large">
            <template #icon>
              <ListOutline />
            </template>
            队列: {{ store.queueLength }}
          </NTag>
        </NSpace>

        <div v-if="store.syncStatus?.current_job">
          <NProgress
            type="line"
            :percentage="progressPercent"
            :indicator-placement="'inside'"
            :status="store.syncStatus.is_running ? 'warning' : 'success'"
          />
          <NDescriptions
            label-placement="left"
            :column="1"
            size="small"
            bordered
          >
            <NDescriptionsItem label="任务 ID">
              {{ store.syncStatus.current_job.id }}
            </NDescriptionsItem>
            <NDescriptionsItem label="类型">
              {{ store.syncStatus.current_job.type }}
            </NDescriptionsItem>
            <NDescriptionsItem label="进度">
              {{ store.syncStatus.current_job.progress }} / {{ store.syncStatus.current_job.total }}
            </NDescriptionsItem>
            <NDescriptionsItem
              v-if="store.syncStatus.current_job.message"
              label="消息"
            >
              {{ store.syncStatus.current_job.message }}
            </NDescriptionsItem>
            <NDescriptionsItem label="创建时间">
              <NTime :time="new Date(store.syncStatus.current_job.created_at)" />
            </NDescriptionsItem>
          </NDescriptions>
        </div>

        <NEmpty
          v-else-if="!store.syncStatus?.is_running"
          description="暂无同步任务"
        />
      </NSpace>
    </NSpin>
  </NCard>
</template>
