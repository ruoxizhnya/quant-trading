<template>
  <n-card v-if="chartData.length > 0" class="chart-card">
    <template #header>
      <div class="chart-header">
        <span>净值曲线</span>
        <n-space :size="8">
          <n-tag v-if="tradeMarkers.length > 0" :type="showTrades ? 'info' : 'default'" size="small" round :bordered="false" @click="$emit('toggle-trades')">
            {{ showTrades ? '📋 隐藏交易' : '📋 显示交易' }}
          </n-tag>
          <n-tag v-if="tradeMarkers.length > 0" type="success" size="small" round :bordered="false">
            {{ buyCount }} 买 / {{ sellCount }} 卖
          </n-tag>
        </n-space>
      </div>
    </template>
    <canvas ref="canvasRef" height="300"></canvas>
  </n-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { NCard, NSpace, NTag } from 'naive-ui'
import { useBacktestChart, type TradeMarker } from '@/composables/useBacktestChart'
import type { PortfolioPoint, Trade } from '@/types/api'

const props = defineProps<{
  portfolioValues: PortfolioPoint[]
  trades: Trade[]
  showTrades: boolean
}>()

const emit = defineEmits<{
  'toggle-trades': []
}>()

const canvasRef = ref<HTMLCanvasElement>()
const { chartData, renderChart, buildTradeMarkers } = useBacktestChart(canvasRef)

const tradeMarkers = ref<TradeMarker[]>([])
const buyCount = computed(() => tradeMarkers.value.filter(t => t.direction === 'long').length)
const sellCount = computed(() => tradeMarkers.value.filter(t => t.direction !== 'long').length)

watch(
  () => [props.portfolioValues, props.trades] as const,
  async () => {
    tradeMarkers.value = buildTradeMarkers(props.portfolioValues, props.trades)
    await nextTick()
    renderChart(props.portfolioValues, props.trades)
  },
  { immediate: true },
)
</script>

<style scoped>
.chart-card { min-height: 340px; }
.chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
</style>
