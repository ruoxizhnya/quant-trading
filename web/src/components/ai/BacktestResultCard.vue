<template>
  <div class="backtest-result-card">
    <n-descriptions bordered :columns="2" size="small">
      <n-descriptions-item label="总收益率">
        {{ formatPercent(result?.total_return) }}
      </n-descriptions-item>
      <n-descriptions-item label="年化收益率">
        {{ formatPercent(result?.annual_return) }}
      </n-descriptions-item>
      <n-descriptions-item label="夏普比率">
        {{ formatNumber(result?.sharpe_ratio) }}
      </n-descriptions-item>
      <n-descriptions-item label="最大回撤">
        {{ formatPercent(result?.max_drawdown) }}
      </n-descriptions-item>
      <n-descriptions-item label="索提诺比率">
        {{ formatNumber(result?.sortino_ratio) }}
      </n-descriptions-item>
      <n-descriptions-item label="卡尔玛比率">
        {{ formatNumber(result?.calmar_ratio) }}
      </n-descriptions-item>
    </n-descriptions>
  </div>
</template>

<script setup lang="ts">
import { NDescriptions, NDescriptionsItem } from 'naive-ui'
import type { BacktestResult } from '@/types/api'

interface Props {
  result?: BacktestResult
}

defineProps<Props>()

function formatPercent(value: number | undefined): string {
  if (value === undefined || value === null) return '-'
  return `${(value * 100).toFixed(2)}%`
}

function formatNumber(value: number | undefined): string {
  if (value === undefined || value === null) return '-'
  return value.toFixed(2)
}
</script>
