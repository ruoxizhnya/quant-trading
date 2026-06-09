<template>
  <div class="backtest-result-card">
    <n-descriptions bordered :columns="2" size="small">
      <n-descriptions-item label="总收益率">
        {{ fmtPercent(result?.total_return) }}
      </n-descriptions-item>
      <n-descriptions-item label="年化收益率">
        {{ fmtPercent(result?.annual_return) }}
      </n-descriptions-item>
      <n-descriptions-item label="夏普比率">
        {{ fmtNumber(result?.sharpe_ratio) }}
      </n-descriptions-item>
      <n-descriptions-item label="最大回撤">
        {{ fmtPercent(result?.max_drawdown) }}
      </n-descriptions-item>
      <n-descriptions-item label="索提诺比率">
        {{ fmtNumber(result?.sortino_ratio) }}
      </n-descriptions-item>
      <n-descriptions-item label="卡尔玛比率">
        {{ fmtNumber(result?.calmar_ratio) }}
      </n-descriptions-item>
    </n-descriptions>
  </div>
</template>

<script setup lang="ts">
import { NDescriptions, NDescriptionsItem } from 'naive-ui'
import type { BacktestResult } from '@/types/api'
import { fmtPercent, fmtNumber } from '@/utils/format'

interface Props {
  result?: BacktestResult
}

defineProps<Props>()

// CR-05 (ODR-012): Local formatPercent/formatNumber replaced by
// the shared utils/format.ts. fmtPercent includes a `+` sign for
// positive values; fmtNumber uses zh-CN locale formatting. These
// differences are intentional (the utilities are the single source
// of truth per AGENTS.md "Never Do" rule).
</script>
