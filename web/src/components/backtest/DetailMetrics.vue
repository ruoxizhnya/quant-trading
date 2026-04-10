<template>
  <n-card v-if="metrics && Object.keys(metrics).length > 0" title="详细指标" class="detail-metrics">
    <div class="metrics-detail-grid">
      <div v-for="(v, k) in metrics" :key="k" class="metric-box-sm">
        <span class="sm-label">{{ k }}</span>
        <span class="sm-val">{{ formatMetric(v) }}</span>
      </div>
    </div>
  </n-card>
</template>

<script setup lang="ts">
import { fmtMetric } from '@/utils/format'

const props = defineProps<{ metrics: Record<string, number> | null }>()

// Expose to template
const formatMetric = fmtMetric
</script>

<style scoped>
.detail-metrics { margin-top: 4px; }
.metrics-detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 4px 24px;
}
.metric-box-sm {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 7px 0;
  border-bottom: 1px solid var(--q-border-light);
}
.metric-box-sm:last-child { border-bottom: none; }
.sm-label { color: var(--q-text3); font-size: 12px; flex-shrink: 0; }
.sm-val { color: var(--q-text); font-weight: 600; font-size: 12px; margin-left: 12px; word-break: all; }

@media (max-width: 640px) {
  .metrics-detail-grid { grid-template-columns: 1fr; }
}
</style>
