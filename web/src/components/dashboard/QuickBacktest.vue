<template>
  <n-card title="快速回测">
    <n-form inline label-placement="left" label-width="auto" size="small">
      <n-form-item label="策略">
        <n-select :value="strategy" :options="strategyOptions" size="small" style="width: 140px" @update:value="$emit('update:strategy', $event)" />
      </n-form-item>
      <n-form-item label="标的">
        <n-input :value="stock" placeholder="600000.SH" size="small" style="width: 130px" @update:value="$emit('update:stock', $event)" />
      </n-form-item>
      <n-form-item label="起止">
        <n-date-picker :formatted-value="startDate" type="date" value-format="yyyy-MM-dd" size="small" style="width: 120px" @update:formatted-value="$emit('update:startDate', $event)" />
        <span style="margin: 0 4px; line-height: 28px;">~</span>
        <n-date-picker :formatted-value="endDate" type="date" value-format="yyyy-MM-dd" size="small" style="width: 120px" @update:formatted-value="$emit('update:endDate', $event)" />
      </n-form-item>
      <n-form-item>
        <n-button type="primary" size="small" :loading="running" @click="$emit('run')">运行</n-button>
      </n-form-item>
    </n-form>
    <div v-if="quickResult" class="quick-result">
      <n-tag round :bordered="false" :type="(quickResult.total_return ?? 0) >= 0 ? 'success' : 'error'" size="small">
        收益 {{ fmtPercent(quickResult.total_return) }}
      </n-tag>
      <n-tag round :bordered="false" size="small">
        夏普 {{ quickResult.sharpe_ratio?.toFixed(2) || '-' }}
      </n-tag>
      <n-button quaternary size="tiny" type="primary" @click="$emit('view-report', quickResult.id)">查看报告</n-button>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NForm, NFormItem, NSelect, NInput, NDatePicker, NButton, NTag } from 'naive-ui'
import { fmtPercent } from '@/utils/format'

const props = defineProps<{
  strategy: string
  stock: string
  startDate: string
  endDate: string
  running: boolean
  strategies: string[]
  quickResult: any
}>()

const emit = defineEmits<{
  'update:strategy': [value: string]
  'update:stock': [value: string]
  'update:startDate': [value: string]
  'update:endDate': [value: string]
  run: []
  'view-report': [id: string]
}>()

const strategyOptions = computed(() =>
  props.strategies.map(s => ({ label: s, value: s }))
)
</script>

<style scoped>
.quick-result {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--q-border-light);
}
</style>
