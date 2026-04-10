<template>
  <n-card v-if="visible" class="progress-card">
    <div class="progress-header">
      <span class="progress-title">{{ title }}</span>
      <n-button v-if="cancellable && status === 'running'" quaternary size="tiny" type="error" @click="$emit('cancel')">
        取消
      </n-button>
    </div>
    <n-progress
      type="line"
      :status="progress >= 100 ? 'success' : status === 'failed' || status === 'cancelled' ? 'error' : 'default'"
      :percentage="progress"
      :show-indicator="true"
      indicator-placement="inside"
      :processing="status === 'running' || status === 'pending'"
      :class="{ 'progress-error': status === 'failed' || status === 'cancelled' }"
    />
    <div class="progress-status">
      <n-tag :type="statusType" size="small" round :bordered="false">
        {{ statusLabel }}
      </n-tag>
      <span v-if="error" class="progress-error">{{ error }}</span>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NProgress, NButton, NTag } from 'naive-ui'
import type { JobStatus } from '@/composables/useAsyncBacktest'

const props = defineProps<{
  visible: boolean
  status: JobStatus
  progress: number
  error: string | null
  cancellable?: boolean
}>()

defineEmits<{
  cancel: []
}>()

const title = computed(() => {
  switch (props.status) {
    case 'pending': return '回测任务已提交...'
    case 'running': return '正在执行回测...'
    case 'completed': return '回测完成'
    case 'failed': return '回测失败'
    case 'cancelled': return '已取消'
    default: return ''
  }
})

const statusLabel = computed(() => {
  const map: Record<string, string> = {
    pending: '等待中',
    running: '执行中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消',
  }
  return map[props.status] || props.status
})

const statusType = computed(() => {
  const map: Record<string, 'info' | 'success' | 'error' | 'warning'> = {
    pending: 'info',
    running: 'info',
    completed: 'success',
    failed: 'error',
    cancelled: 'warning',
  }
  return map[props.status] || 'default'
})
</script>

<style scoped>
.progress-card { margin-top: 4px; }
.progress-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.progress-title { font-size: 13px; font-weight: 600; color: var(--q-text); }
.progress-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}
.progress-error { font-size: 12px; color: var(--q-danger); }
</style>
