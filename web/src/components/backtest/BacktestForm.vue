<template>
  <n-card title="回测引擎">
    <div class="form-grid">
      <div class="form-item">
        <label class="form-label">策略</label>
        <n-select :value="strategy" :options="strategyOptions" size="small" @update:value="$emit('update:strategy', $event)" />
      </div>
      <div class="form-item form-item-wide">
        <label class="form-label">股票池</label>
        <n-input :value="stockPool" placeholder="600000.SH,600036.SH" size="small" @update:value="$emit('update:stockPool', $event)" />
      </div>
      <div class="form-item">
        <label class="form-label">开始日期</label>
        <n-date-picker :formatted-value="startDate" type="date" value-format="yyyy-MM-dd" size="small" @update:formatted-value="$emit('update:startDate', $event)" />
      </div>
      <div class="form-item">
        <label class="form-label">结束日期</label>
        <n-date-picker :formatted-value="endDate" type="date" value-format="yyyy-MM-dd" size="small" @update:formatted-value="$emit('update:endDate', $event)" />
      </div>
      <div class="form-item">
        <label class="form-label">初始资金</label>
        <n-input-number :value="initialCapital" :min="10000" :step="100000" size="small" @update:value="(val: number | null) => $emit('update:initialCapital', val ?? 1000000)" />
      </div>
      <div class="form-item">
        <label class="form-label">手续费率</label>
        <n-input-number :value="commissionRate" :step="0.0001" :precision="4" size="small" @update:value="(val: number | null) => $emit('update:commissionRate', val ?? 0.0003)" />
      </div>
      <div class="form-item">
        <label class="form-label">滑点率</label>
        <n-input-number :value="slippageRate" :step="0.0001" :precision="4" size="small" @update:value="(val: number | null) => $emit('update:slippageRate', val ?? 0.0001)" />
      </div>
      <div class="form-item form-item-btn">
        <n-button type="primary" :loading="loading" block @click="$emit('submit')">运行回测</n-button>
      </div>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NSelect, NInput, NInputNumber, NDatePicker, NButton } from 'naive-ui'

const props = defineProps<{
  strategy: string
  stockPool: string
  startDate: string
  endDate: string
  initialCapital: number
  commissionRate: number
  slippageRate: number
  loading: boolean
  strategies: string[]
}>()

const emit = defineEmits<{
  'update:strategy': [value: string]
  'update:stockPool': [value: string]
  'update:startDate': [value: string]
  'update:endDate': [value: string]
  'update:initialCapital': [value: number]
  'update:commissionRate': [value: number]
  'update:slippageRate': [value: number]
  submit: []
}>()

const strategyOptions = computed(() =>
  props.strategies.map(s => ({ label: s, value: s }))
)
</script>

<style scoped>
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px 16px;
  align-items: end;
}
.form-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.form-item-wide { grid-column: span 2; min-width: 200px; }
.form-item-btn { grid-column: span 1; }
.form-label {
  font-size: 11px;
  color: var(--q-text3);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  white-space: nowrap;
}
@media (max-width: 900px) {
  .form-grid { grid-template-columns: repeat(2, 1fr); }
  .form-item-wide { grid-column: span 1; }
}
@media (max-width: 640px) {
  .form-grid { grid-template-columns: 1fr; }
}
</style>
