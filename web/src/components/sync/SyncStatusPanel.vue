<template>
  <NCard title="同步状态" class="mb-4">
    <template #header-extra>
      <NSpace>
        <NTag v-if="store.sseConnected" type="success" size="small">实时连接</NTag>
        <NButton size="small" @click="refresh">刷新</NButton>
        <NButton v-if="store.watchedJobId" size="small" quaternary @click="store.unwatchJob">
          断开实时
        </NButton>
      </NSpace>
    </template>

    <NAlert v-if="store.error" type="error" class="mb-3" closable @close="store.clearError">
      {{ store.error }}
    </NAlert>

    <NEmpty v-if="!store.activeJob" description="暂无同步任务">
      <template #extra>
        <NButton size="small" @click="refresh">加载任务列表</NButton>
      </template>
    </NEmpty>

    <template v-else>
      <div class="mb-4">
        <div class="mb-2 flex items-center gap-2">
          <NTag :type="statusType" size="small">{{ statusText }}</NTag>
          <NTag size="small" :bordered="false">{{ store.activeJob.job_type }}</NTag>
          <span class="text-xs text-gray-500">{{ store.activeJob.id }}</span>
        </div>
        <NProgress
          type="line"
          :percentage="store.activeJob.progress_percent"
          :status="store.isSyncRunning ? 'default' : store.activeJob.status === 'failed' ? 'error' : 'success'"
          indicator-placement="inside"
          processing
        />
        <p class="mt-2 text-xs text-gray-500">
          {{ store.activeJob.processed_items }}/{{ store.activeJob.total_items }} 项
          <template v-if="store.activeJob.failed_items > 0">
            · <span class="text-red-500">失败 {{ store.activeJob.failed_items }} 项</span>
          </template>
        </p>
        <p v-if="store.activeJob.error_message" class="mt-1 text-xs text-red-500">
          {{ store.activeJob.error_message }}
        </p>
      </div>

      <NDescriptions v-if="store.jobs.length > 1" :column="1" size="small" label-placement="left" bordered>
        <NDescriptionsItem label="近期任务">
          <NSpace size="small">
            <NTag
              v-for="job in recentJobs"
              :key="job.id"
              size="small"
              :type="jobTagType(job.status)"
              :title="job.error_message || job.id"
            >
              {{ job.job_type }} · {{ jobStatusText(job.status) }}
            </NTag>
          </NSpace>
        </NDescriptionsItem>
      </NDescriptions>
    </template>
  </NCard>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NProgress,
  NSpace,
  NTag,
} from 'naive-ui'
import { useSyncStore } from '@/stores/sync'
import type { SyncJobStatus } from '@/types/sync'

const store = useSyncStore()

const recentJobs = computed(() => store.jobs.slice(0, 5))

const statusText = computed(() => {
  const job = store.activeJob
  if (!job) return '空闲'
  return jobStatusText(job.status)
})

const statusType = computed(() => {
  const job = store.activeJob
  if (!job) return 'default' as const
  return jobTagType(job.status)
})

function jobStatusText(status: SyncJobStatus): string {
  switch (status) {
    case 'pending':
      return '排队中'
    case 'running':
      return '同步中'
    case 'retrying':
      return '重试中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    case 'cancelled':
      return '已取消'
  }
}

function jobTagType(status: SyncJobStatus): 'default' | 'success' | 'warning' | 'error' {
  switch (status) {
    case 'pending':
    case 'running':
    case 'retrying':
      return 'warning'
    case 'completed':
      return 'success'
    case 'failed':
    case 'cancelled':
      return 'error'
  }
}

async function refresh() {
  try {
    await store.fetchSyncJobs()
    if (store.activeJob && isJobActiveStatus(store.activeJob.status)) {
      store.watchJob(store.activeJob.id)
    }
  } catch {
    // error state is rendered by the panel itself
  }
}

function isJobActiveStatus(status: SyncJobStatus): boolean {
  return status === 'pending' || status === 'running' || status === 'retrying'
}

onMounted(refresh)
</script>
