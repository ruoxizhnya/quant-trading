<template>
  <n-card title="市场概览">
    <div class="market-header">
      <span class="greeting">{{ greeting }}</span>
      <n-space align="center" :size="12">
        <n-date-picker :formatted-value="selectedDate" type="date" value-format="yyyy-MM-dd" size="small" @update:formatted-value="$emit('update:selectedDate', $event)" />
        <n-select :value="selectedIndex" :options="indexOptions" size="small" style="width: 120px" @update:value="$emit('update:selectedIndex', $event)" />
        <n-button quaternary size="tiny" @click="$emit('refresh')">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
      </n-space>
    </div>

    <n-spin :show="loading">
      <div class="metrics-row">
        <div class="metric-item">
          <span class="metric-label">收盘价</span>
          <span class="metric-val">{{ metrics.close?.toFixed(2) || '-' }}</span>
        </div>
        <div class="metric-item">
          <span class="metric-label">涨跌幅</span>
          <span class="metric-val" :class="(metrics.change_pct ?? 0) >= 0 ? 'up' : 'down'">
            {{ (metrics.change_pct ?? 0) > 0 ? '+' : '' }}{{ (metrics.change_pct ?? 0).toFixed(2) }}%
          </span>
        </div>
        <div class="metric-item">
          <span class="metric-label">成交量</span>
          <span class="metric-val">{{ formatVolume(metrics.volume) }}</span>
        </div>
        <div class="metric-item">
          <span class="metric-label">成交额</span>
          <span class="metric-val">{{ formatAmount(metrics.amount) }}亿</span>
        </div>
      </div>
    </n-spin>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NSpace, NDatePicker, NSelect, NButton, NSpin, NIcon } from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'

const props = defineProps<{
  selectedDate: string
  selectedIndex: string
  loading: boolean
  metrics: Record<string, any>
}>()

const emit = defineEmits<{
  'update:selectedDate': [value: string]
  'update:selectedIndex': [value: string]
  refresh: []
}>()

const indexOptions = [
  { label: '上证指数', value: '000001.SH' },
  { label: '深证成指', value: '399001.SZ' },
  { label: '创业板指', value: '399006.SZ' },
]

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 11) return '早上好 👋'
  if (h < 14) return '中午好 ☀️'
  return '下午好 🌤'
})

function formatVolume(v: any): string {
  if (!v) return '-'
  const num = Number(v)
  if (num >= 1e8) return (num / 1e8).toFixed(1) + '亿'
  return (num / 1e4).toFixed(0) + '万'
}

function formatAmount(a: any): string {
  if (!a) return '-'
  return (Number(a) / 1e8).toFixed(1)
}
</script>

<style scoped>
.market-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.greeting { font-size: 18px; font-weight: 700; color: var(--q-text); }
.metrics-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.metric-item {
  background: var(--q-surface2);
  border-radius: var(--q-radius-sm);
  padding: 14px 10px;
  text-align: center;
}
.metric-label { font-size: 11px; color: var(--q-text3); display: block; margin-bottom: 4px; }
.metric-val { font-size: 20px; font-weight: 700; color: var(--q-text); }
.metric-val.up { color: var(--q-success); }
.metric-val.down { color: var(--q-danger); }

@media (max-width: 768px) {
  .market-header { flex-direction: column; gap: 8px; align-items: flex-start; }
  .metrics-row { grid-template-columns: repeat(2, 1fr); }
}
</style>
