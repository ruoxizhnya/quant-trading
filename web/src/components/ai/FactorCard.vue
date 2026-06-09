<template>
  <n-card class="factor-card" size="small" :class="statusClass">
    <template #header>
      <n-space align="center" justify="space-between">
        <n-space align="center">
          <n-tag :type="categoryType" size="small">{{ factor.category }}</n-tag>
          <span class="factor-name">{{ factor.name }}</span>
        </n-space>
        <n-tag :type="statusType" size="small">{{ factor.status }}</n-tag>
      </n-space>
    </template>

    <n-space vertical size="small">
      <!-- Formula -->
      <div class="formula-section">
        <n-text code class="formula">{{ factor.formula }}</n-text>
      </div>

      <!-- Metrics Grid -->
      <n-grid :cols="4" :x-gap="8" class="metrics-grid">
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">IC</div>
            <div class="metric-value" :class="getMetricClass(factor.ic, 0.03)">
              {{ fmtMetric(factor.ic) }}
            </div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">IR</div>
            <div class="metric-value" :class="getMetricClass(factor.ir, 0.3)">
              {{ fmtMetric(factor.ir) }}
            </div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">换手率</div>
            <div class="metric-value">{{ fmtPercent(factor.turnover) }}</div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">Fitness</div>
            <div class="metric-value fitness">{{ fmtMetric(factor.fitness) }}</div>
          </div>
        </n-gi>
      </n-grid>

      <!-- Description -->
      <n-text depth="3" class="description">{{ factor.description }}</n-text>

      <!-- Actions -->
      <n-space justify="end" size="small">
        <n-button text size="tiny" @click="$emit('view', factor)">
          <template #icon><n-icon><EyeOutline /></n-icon></template>
          查看
        </n-button>
        <n-button text size="tiny" @click="$emit('mutate', factor)">
          <template #icon><n-icon><GitBranchOutline /></n-icon></template>
          变异
        </n-button>
        <n-button text size="tiny" type="error" @click="$emit('delete', factor)">
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          删除
        </n-button>
      </n-space>
    </n-space>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { EyeOutline, TrashOutline, GitBranchOutline } from '@vicons/ionicons5'
import { fmtPercent, fmtMetric } from '@/utils/format'

interface Factor {
  id: string
  name: string
  category: string
  formula: string
  description: string
  ic: number
  ir: number
  turnover: number
  sharpe: number
  fitness: number
  status: string
}

const props = defineProps<{
  factor: Factor
}>()

defineEmits<{
  view: [factor: Factor]
  mutate: [factor: Factor]
  delete: [factor: Factor]
}>()

const statusClass = computed(() => {
  return {
    'factor-validated': props.factor.status === 'validated',
    'factor-rejected': props.factor.status === 'rejected',
    'factor-pending': props.factor.status === 'pending',
  }
})

const categoryType = computed(() => {
  const types: Record<string, string> = {
    momentum: 'success',
    value: 'warning',
    quality: 'info',
    volatility: 'error',
    liquidity: 'default',
  }
  return types[props.factor.category] || 'default'
})

const statusType = computed(() => {
  const types: Record<string, string> = {
    validated: 'success',
    rejected: 'error',
    pending: 'warning',
  }
  return types[props.factor.status] || 'default'
})

function getMetricClass(value: number, threshold: number): string {
  if (value >= threshold * 2) return 'excellent'
  if (value >= threshold) return 'good'
  if (value >= threshold * 0.5) return 'fair'
  return 'poor'
}

// CR-06 (ODR-012): Local formatPercent/formatMetric replaced by the shared
// fmtPercent/fmtMetric in @/utils/format. fmtPercent now prefixes '+' for
// positive values; fmtMetric auto-switches between fixed and percentage
// representation based on magnitude. These are the single source of truth
// per AGENTS.md "Never Do" rule.
</script>

<style scoped>
.factor-card {
  transition: all 0.2s ease;
  border-left: 3px solid transparent;
}

.factor-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.factor-validated {
  border-left-color: #18a058;
}

.factor-rejected {
  border-left-color: #d03050;
}

.factor-pending {
  border-left-color: #f0a020;
}

.factor-name {
  font-weight: 600;
  font-size: 14px;
}

.formula-section {
  background: #f5f5f5;
  padding: 8px;
  border-radius: 4px;
  margin: 4px 0;
}

.formula {
  font-size: 12px;
  word-break: break-all;
}

.metrics-grid {
  margin: 8px 0;
}

.metric-item {
  text-align: center;
  padding: 4px;
  background: #fafafa;
  border-radius: 4px;
}

.metric-label {
  font-size: 11px;
  color: #666;
  margin-bottom: 2px;
}

.metric-value {
  font-size: 13px;
  font-weight: 600;
}

.metric-value.excellent {
  color: #18a058;
}

.metric-value.good {
  color: #2080f0;
}

.metric-value.fair {
  color: #f0a020;
}

.metric-value.poor {
  color: #d03050;
}

.metric-value.fitness {
  color: #8a2be2;
}

.description {
  font-size: 12px;
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
