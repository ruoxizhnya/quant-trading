<template>
  <div class="genealogy-tree">
    <n-card title="策略谱系树" size="small">
      <div ref="treeContainer" class="tree-container" :style="{ height: `${height}px` }">
        <n-empty v-if="!hasData" description="暂无谱系数据" />
        <canvas v-else ref="treeCanvas" class="tree-canvas" />
      </div>
      <n-space v-if="hasData" justify="center" size="small">
        <n-tag size="tiny" type="success">● 当前代</n-tag>
        <n-tag size="tiny" type="info">● 父代</n-tag>
        <n-tag size="tiny" type="warning">● 祖父代</n-tag>
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted } from 'vue'

interface Strategy {
  id: string
  name: string
  fitness: number
  generation: number
  parentIDs: string[]
}

const props = defineProps<{
  strategies: Strategy[]
  height?: number
}>()

const treeContainer = ref<HTMLDivElement>()
const treeCanvas = ref<HTMLCanvasElement>()

const height = computed(() => props.height || 250)
const hasData = computed(() => props.strategies.length > 0)

function drawTree() {
  const canvas = treeCanvas.value
  const container = treeContainer.value
  if (!canvas || !container || !hasData.value) return

  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const width = container.clientWidth
  canvas.width = width
  canvas.height = height.value

  // Clear canvas
  ctx.clearRect(0, 0, width, height.value)

  // Build node map
  const nodeMap = new Map<string, Strategy>()
  props.strategies.forEach(s => nodeMap.set(s.id, s))

  // Group by generation
  const genMap = new Map<number, Strategy[]>()
  props.strategies.forEach(s => {
    const gen = s.generation
    if (!genMap.has(gen)) genMap.set(gen, [])
    genMap.get(gen)!.push(s)
  })

  const generations = Array.from(genMap.keys()).sort((a, b) => a - b)
  if (generations.length === 0) return

  const maxGen = Math.max(...generations)
  const minGen = Math.min(...generations)
  const genCount = maxGen - minGen + 1

  const nodeRadius = 20
  const levelHeight = height.value / (genCount + 1)

  // Calculate positions
  const positions = new Map<string, { x: number; y: number }>()

  generations.forEach((gen, genIndex) => {
    const strategies = genMap.get(gen)!
    const y = height.value - (genIndex + 1) * levelHeight
    const spacing = width / (strategies.length + 1)

    strategies.forEach((strategy, index) => {
      const x = spacing * (index + 1)
      positions.set(strategy.id, { x, y })
    })
  })

  // Draw connections
  ctx.strokeStyle = '#63e2b7'
  ctx.lineWidth = 1.5
  ctx.globalAlpha = 0.6

  props.strategies.forEach(strategy => {
    const pos = positions.get(strategy.id)
    if (!pos) return

    strategy.parentIDs.forEach(parentId => {
      const parentPos = positions.get(parentId)
      if (parentPos) {
        ctx.beginPath()
        ctx.moveTo(pos.x, pos.y)
        ctx.lineTo(parentPos.x, parentPos.y)
        ctx.stroke()
      }
    })
  })

  ctx.globalAlpha = 1.0

  // Draw nodes
  props.strategies.forEach(strategy => {
    const pos = positions.get(strategy.id)
    if (!pos) return

    const genDiff = maxGen - strategy.generation
    const color = genDiff === 0 ? '#63e2b7' : genDiff === 1 ? '#70c0e8' : '#f2c97d'

    // Circle
    ctx.beginPath()
    ctx.arc(pos.x, pos.y, nodeRadius, 0, Math.PI * 2)
    ctx.fillStyle = color
    ctx.fill()
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = 2
    ctx.stroke()

    // Text
    ctx.fillStyle = '#fff'
    ctx.font = '10px sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'

    // Truncate name
    let name = strategy.name
    if (name.length > 8) {
      name = name.substring(0, 6) + '...'
    }
    ctx.fillText(name, pos.x, pos.y - 4)

    // Fitness
    ctx.font = '9px sans-serif'
    ctx.fillText(strategy.fitness?.toFixed(3) || '0.000', pos.x, pos.y + 8)
  })
}

watch(() => props.strategies, async () => {
  await nextTick()
  drawTree()
}, { deep: true })

onMounted(() => {
  drawTree()
})
</script>

<style scoped>
.genealogy-tree {
  width: 100%;
}

.tree-container {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.tree-canvas {
  width: 100%;
  height: 100%;
}
</style>
