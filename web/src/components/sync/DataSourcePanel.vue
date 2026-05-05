<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useSyncStore } from '@/stores/sync'
import {
  NCard,
  NSpace,
  NButton,
  NTag,
  NSpin,
  NAlert,
  NDescriptions,
  NDescriptionsItem,
  NDivider,
} from 'naive-ui'
import {
  ServerOutline,
  PulseOutline,
  SwapHorizontalOutline,
  RefreshOutline,
} from '@vicons/ionicons5'

const store = useSyncStore()

const statusType = computed(() => {
  if (!store.dataSourceStatus) return 'default'
  return store.dataSourceStatus.enabled ? 'success' : 'warning'
})

const statusText = computed(() => {
  if (!store.dataSourceStatus) return '未知'
  return store.dataSourceStatus.enabled ? '已启用' : '未启用'
})

const healthStatus = computed(() => {
  if (!store.dataSourceHealth) return 'unknown'
  return store.dataSourceHealth.status
})

const healthType = computed(() => {
  switch (healthStatus.value) {
    case 'healthy':
      return 'success'
    case 'unhealthy':
      return 'error'
    default:
      return 'default'
  }
})

onMounted(() => {
  store.fetchDataSourceStatus()
  store.fetchDataSourceHealth()
})

async function handleRefresh() {
  await store.fetchDataSourceStatus()
  await store.fetchDataSourceHealth()
}
</script>

<template>
  <NCard title="数据源管理" embedded>
    <template #header-extra>
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
              <ServerOutline />
            </template>
            {{ statusText }}
          </NTag>
          <NTag :type="healthType" size="large">
            <template #icon>
              <PulseOutline />
            </template>
            {{ healthStatus === 'healthy' ? '健康' : healthStatus === 'unhealthy' ? '异常' : '未知' }}
          </NTag>
        </NSpace>

        <NDivider />

        <NDescriptions
          v-if="store.dataSourceStatus"
          label-placement="left"
          :column="1"
          size="small"
          bordered
        >
          <NDescriptionsItem label="主数据源">
            {{ store.primaryDataSource }}
          </NDescriptionsItem>
          <NDescriptionsItem label="模式">
            {{ store.dataSourceStatus.mode || 'direct' }}
          </NDescriptionsItem>
          <NDescriptionsItem
            v-if="store.dataSourceStatus.stopped !== undefined"
            label="状态"
          >
            {{ store.dataSourceStatus.stopped ? '已停止' : '运行中' }}
          </NDescriptionsItem>
        </NDescriptions>

        <NDescriptions
          v-if="store.dataSourceHealth?.error"
          label-placement="left"
          :column="1"
          size="small"
        >
          <NDescriptionsItem label="错误信息">
            <span style="color: #d03050">{{ store.dataSourceHealth.error }}</span>
          </NDescriptionsItem>
        </NDescriptions>
      </NSpace>
    </NSpin>
  </NCard>
</template>
