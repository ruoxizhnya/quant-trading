<template>
  <div class="fitness-chart">
    <n-card title="适应度进化曲线" size="small">
      <div ref="chartContainer" class="chart-container" :style="{ height: `${height}px` }">
        <n-empty v-if="!hasData" description="暂无进化数据" />
        <canvas v-else ref="chartCanvas" class="chart-canvas" />
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'

interface GenerationStat {
  generation: number
  bestFitness: number
  avgFitness: number
  worstFitness: number
  diversity?: number
}

const props = defineProps<{
  generations: GenerationStat[]
  height?: number
}>()

const chartContainer = ref<HTMLDivElement>()
const chartCanvas = ref<HTMLCanvasElement>()

const height = computed(() => props.height || 300)
const hasData = computed(() => props.generations.length > 0)

// CR-23 (ODR-012): Math.max(...arr) and Math.min(...arr) spread each element
// as a function argument. For >~65k elements (or fewer on V8 with strict mode
// in older runtimes), this triggers "Maximum call stack size exceeded". A
// 200-generation evolution run produces 3*200 = 600 fitness values via
// flatMap — safe today, but a 1000-generation scenario would crash. Use
// reduce-based safe extremes.
function safeMax(values: number[], fallback: number): number {
  let best = fallback
  for (let i = 0; i < values.length; i++) {
    if (values[i] > best) best = values[i]
  }
  return best
}

function safeMin(values: number[], fallback: number): number {
  let best = fallback
  for (let i = 0; i < values.length; i++) {
    if (values[i] < best) best = values[i]
  }
  return best
}

function drawChart() {
  const canvas = chartCanvas.value
  const container = chartContainer.value
  if (!canvas || !container || !hasData.value) return

  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const width = container.clientWidth
  canvas.width = width
  canvas.height = height.value

  // Clear canvas
  ctx.clearRect(0, 0, width, height.value)

  const padding = { top: 30, right: 30, bottom: 40, left: 50 }
  const chartWidth = width - padding.left - padding.right
  const chartHeight = height.value - padding.top - padding.bottom

  // Calculate scales
  const gens = props.generations
  const genValues = gens.map(g => g.generation)
  const maxGen = safeMax(genValues, 0)
  const minGen = safeMin(genValues, 0)

  const allFitness = gens.flatMap(g => [g.bestFitness, g.avgFitness, g.worstFitness])
  const maxFitness = safeMax(allFitness, 0.1)
  const minFitness = safeMin(allFitness, 0)
  const fitnessRange = maxFitness - minFitness || 1

  // Helper functions
  const xScale = (gen: number) => {
    if (maxGen === minGen) return padding.left + chartWidth / 2
    return padding.left + ((gen - minGen) / (maxGen - minGen)) * chartWidth
  }

  const yScale = (fitness: number) => {
    return padding.top + chartHeight - ((fitness - minFitness) / fitnessRange) * chartHeight
  }

  // Draw grid
  ctx.strokeStyle = '#333'
  ctx.lineWidth = 0.5
  ctx.globalAlpha = 0.3

  // Horizontal grid lines
  for (let i = 0; i <= 5; i++) {
    const y = padding.top + (chartHeight / 5) * i
    ctx.beginPath()
    ctx.moveTo(padding.left, y)
    ctx.lineTo(width - padding.right, y)
    ctx.stroke()

    // Y-axis labels
    const fitness = maxFitness - (fitnessRange / 5) * i
    ctx.fillStyle = '#999'
    ctx.font = '11px sans-serif'
    ctx.textAlign = 'right'
    ctx.textBaseline = 'middle'
    ctx.fillText(fitness.toFixed(3), padding.left - 8, y)
  }

  // Vertical grid lines
  const xSteps = Math.min(maxGen - minGen + 1, 10)
  for (let i = 0; i <= xSteps; i++) {
    const gen = minGen + ((maxGen - minGen) / xSteps) * i
    const x = xScale(Math.round(gen))
    ctx.beginPath()
    ctx.moveTo(x, padding.top)
    ctx.lineTo(x, height.value - padding.bottom)
    ctx.stroke()

    // X-axis labels
    ctx.fillStyle = '#999'
    ctx.font = '11px sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    ctx.fillText(Math.round(gen).toString(), x, height.value - padding.bottom + 8)
  }

  ctx.globalAlpha = 1.0

  // Draw axes
  ctx.strokeStyle = '#666'
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(padding.left, padding.top)
  ctx.lineTo(padding.left, height.value - padding.bottom)
  ctx.lineTo(width - padding.right, height.value - padding.bottom)
  ctx.stroke()

  // Draw lines
  const drawLine = (data: number[], color: string, label: string) => {
    if (data.length < 2) return

    ctx.strokeStyle = color
    ctx.lineWidth = 2
    ctx.beginPath()

    gens.forEach((gen, i) => {
      const x = xScale(gen.generation)
      const y = yScale(data[i])
      if (i === 0) {
        ctx.moveTo(x, y)
      } else {
        ctx.lineTo(x, y)
      }
    })
    ctx.stroke()

    // Draw points
    ctx.fillStyle = color
    gens.forEach((gen, i) => {
      const x = xScale(gen.generation)
      const y = yScale(data[i])
      ctx.beginPath()
      ctx.arc(x, y, 3, 0, Math.PI * 2)
      ctx.fill()
    })
  }

  drawLine(gens.map(g => g.bestFitness), '#63e2b7', 'Best')
  drawLine(gens.map(g => g.avgFitness), '#70c0e8', 'Average')
  drawLine(gens.map(g => g.worstFitness), '#f2c97d', 'Worst')

  // Legend
  const legendItems = [
    { color: '#63e2b7', label: '最佳' },
    { color: '#70c0e8', label: '平均' },
    { color: '#f2c97d', label: '最差' },
  ]

  let legendX = width - padding.right - 150
  const legendY = padding.top - 20

  legendItems.forEach(item => {
    ctx.fillStyle = item.color
    ctx.fillRect(legendX, legendY, 12, 8)
    ctx.fillStyle = '#ccc'
    ctx.font = '11px sans-serif'
    ctx.textAlign = 'left'
    ctx.textBaseline = 'middle'
    ctx.fillText(item.label, legendX + 16, legendY + 4)
    legendX += 50
  })

  // Axis labels
  ctx.fillStyle = '#999'
  ctx.font = '12px sans-serif'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'top'
  ctx.fillText('代数', width / 2, height.value - 15)

  ctx.save()
  ctx.translate(15, height.value / 2)
  ctx.rotate(-Math.PI / 2)
  ctx.textAlign = 'center'
  ctx.textBaseline = 'bottom'
  ctx.fillText('适应度', 0, 0)
  ctx.restore()
}

watch(() => props.generations, async () => {
  await nextTick()
  drawChart()
}, { deep: true })

// CR-27 (ODR-012): register the resize listener in onMounted and remove it in
// onUnmounted. Previously the listener was added on mount but never removed,
// so a navigation away from the Evolution page would leave a dangling
// `drawChart` closure holding refs to the canvas — and re-running it on every
// browser resize of unrelated pages.
onMounted(() => {
  drawChart()
  window.addEventListener('resize', drawChart)
})

onUnmounted(() => {
  window.removeEventListener('resize', drawChart)
})
</script>

<style scoped>
.fitness-chart {
  width: 100%;
}

.chart-container {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.chart-canvas {
  width: 100%;
  height: 100%;
}
</style>
