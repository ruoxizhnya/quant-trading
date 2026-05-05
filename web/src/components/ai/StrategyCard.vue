<template>
  <n-card class="strategy-card" size="small" :class="statusClass">
    <template #header>
      <n-space align="center" justify="space-between">
        <n-space align="center">
          <n-tag :type="typeType" size="small">{{ strategy.type }}</n-tag>
          <span class="strategy-name">{{ strategy.name }}</span>
        </n-space>
        <n-tag :type="statusType" size="small">{{ strategy.status }}</n-tag>
      </n-space>
    </template>

    <n-space vertical size="small">
      <!-- Description -->
      <n-text depth="3" class="description">{{ strategy.description }}</n-text>

      <!-- Metrics Grid -->
      <n-grid :cols="4" :x-gap="8" class="metrics-grid">
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">收益率</div>
            <div class="metric-value" :class="getReturnClass(strategy.totalReturn)">
              {{ (strategy.totalReturn * 100).toFixed(1) }}%
            </div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">Sharpe</div>
            <div class="metric-value" :class="getSharpeClass(strategy.sharpe)">
              {{ strategy.sharpe?.toFixed(2) }}
            </div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">最大回撤</div>
            <div class="metric-value">{{ (strategy.maxDrawdown * 100).toFixed(1) }}%</div>
          </div>
        </n-gi>
        <n-gi>
          <div class="metric-item">
            <div class="metric-label">胜率</div>
            <div class="metric-value">{{ (strategy.winRate * 100).toFixed(1) }}%</div>
          </div>
        </n-gi>
      </n-grid>

      <!-- Code Preview -->
      <n-code :code="strategy.code" language="go" :show-line-numbers="false" class="code-preview" />

      <!-- Actions -->
      <n-space justify="end" size="small">
        <n-button text size="tiny" @click="$emit('view', strategy)">
          <template #icon><n-icon><EyeOutline /></n-icon></template>
          查看
        </n-button>
        <n-button text size="tiny" @click="$emit('validate', strategy)">
          <template #icon><n-icon><CheckmarkOutline /></n-icon></template>
          验证
        </n-button>
        <n-button text size="tiny" type="error" @click="$emit('delete', strategy)">
          <template #icon><n-icon><TrashOutline /></n-icon></template>
          删除
        </n-button>
      </n-space>
    </n-space>
  </n-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { EyeOutline, TrashOutline, CheckmarkOutline } from '@vicons/ionicons5'

interface Strategy {
  id: string
  name: string
  type: string
  description: string
  code: string
  totalReturn: number
  sharpe: number
  maxDrawdown: number
  winRate: number
  fitness: number
  status: string
}

const props = defineProps<{
  strategy: Strategy
}>()

defineEmits<{
  view: [strategy: Strategy]
  validate: [strategy: Strategy]
  delete: [strategy: Strategy]
}>()

const statusClass = computed(() => {
  return {
    'strategy-compilable': props.strategy.status === 'compilable',
    'strategy-pending': props.strategy.status === 'pending',
    'strategy-error': props.strategy.status === 'error',
  }
})

const typeType = computed(() => {
  const types: Record<string, string> = {
    momentum: 'success',
    mean_reversion: 'warning',
    multi_factor: 'info',
    breakout: 'error',
  }
  return types[props.strategy.type] || 'default'
})

const statusType = computed(() => {
  const types: Record<string, string> = {
    compilable: 'success',
    pending: 'warning',
    error: 'error',
  }
  return types[props.strategy.status] || 'default'
})

function getReturnClass(val: number): string {
  if (val >= 0.2) return 'excellent'
  if (val >= 0.1) return 'good'
  if (val >= 0) return 'fair'
  return 'poor'
}

function getSharpeClass(val: number): string {
  if (val >= 1.5) return 'excellent'
  if (val >= 1.0) return 'good'
  if (val >= 0.5) return 'fair'
  return 'poor'
}
</script>

<style scoped>
.strategy-card {
  transition: all 0.2s ease;
  border-left: 3px solid transparent;
}

.strategy-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.strategy-compilable {
  border-left-color: #18a058;
}

.strategy-pending {
  border-left-color: #f0a020;
}

.strategy-error {
  border-left-color: #d03050;
}

.strategy-name {
  font-weight: 600;
  font-size: 14px;
}

.description {
  font-size: 12px;
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
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

.code-preview {
  max-height: 80px;
  overflow: hidden;
  font-size: 11px;
}

.code-preview :deep(pre) {
  margin: 0;
  padding: 8px;
}
</style>
