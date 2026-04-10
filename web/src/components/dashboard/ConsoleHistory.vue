<template>
  <n-card title="控制台历史">
    <template #extra>
      <n-button quaternary size="tiny" @click="$emit('clear')">清除</n-button>
    </template>
    <n-list v-if="validHistory.length > 0" bordered>
      <n-list-item v-for="(item, i) in validHistory" :key="item.id || i">
        <template #prefix>
          <n-tag :type="(item.total_return ?? 0) >= 0 ? 'success' : 'error'" size="small" round :bordered="false">
            {{ (item.total_return ?? 0) >= 0 ? '多' : '空' }}
          </n-tag>
        </template>
        <n-thing :title="itemTitle(item)" :description="itemDesc(item)"></n-thing>
        <template #suffix>
          <n-button quaternary size="tiny" type="primary" @click="$emit('view-report', item.id)">查看</n-button>
        </template>
      </n-list-item>
    </n-list>
    <n-empty v-else description="暂无历史记录"></n-empty>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NList, NListItem, NThing, NTag, NButton, NEmpty } from 'naive-ui'
import { fmtPercent } from '@/utils/format'
import type { BacktestResult } from '@/types/api'

const props = defineProps<{ history: BacktestResult[] }>()

const emit = defineEmits<{
  clear: []
  'view-report': [id: string]
}>()

const validHistory = computed(() =>
  (props.history || []).filter((item: any) => item && item.id)
)

function itemTitle(item: BacktestResult): string {
  return `${Array.isArray(item.stock_pool) ? item.stock_pool.join(',') : (item.stock_pool || '')} · ${item.strategy || ''}`
}

function itemDesc(item: BacktestResult): string {
  const ret = fmtPercent(item.total_return)
  const sharpe = (item.sharpe_ratio != null && !isNaN(item.sharpe_ratio)) ? item.sharpe_ratio.toFixed(2) : '-'
  return `收益: ${ret} | 夏普: ${sharpe}`
}
</script>
