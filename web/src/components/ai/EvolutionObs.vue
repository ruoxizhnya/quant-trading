<template>
  <div class="evolution-obs">
    <n-card title="进化观测站" class="obs-card">
      <n-space vertical size="large">
        <!-- Controls -->
        <n-space justify="space-between" align="center">
          <n-space>
            <n-button type="primary" @click="handleStartEvolution" :loading="evolving">
              <template #icon>
                <n-icon><PlayOutline /></n-icon>
              </template>
              开始进化
            </n-button>
            <n-button @click="handlePauseEvolution" :disabled="!evolving">
              <template #icon>
                <n-icon><PauseOutline /></n-icon>
              </template>
              暂停
            </n-button>
            <n-button @click="handleReset">
              <template #icon>
                <n-icon><RefreshOutline /></n-icon>
              </template>
              重置
            </n-button>
          </n-space>
          <n-space>
            <n-statistic label="当前代数" :value="currentGen" />
            <n-statistic label="种群规模" :value="populationSize" />
          </n-space>
        </n-space>

        <!-- Evolution Config -->
        <n-collapse>
          <n-collapse-item title="进化参数">
            <n-grid :cols="3" :x-gap="16">
              <n-gi>
                <n-form-item label="种群大小">
                  <n-input-number v-model:value="config.populationSize" :min="10" :max="200" />
                </n-form-item>
              </n-gi>
              <n-gi>
                <n-form-item label="最大代数">
                  <n-input-number v-model:value="config.maxGenerations" :min="10" :max="1000" />
                </n-form-item>
              </n-gi>
              <n-gi>
                <n-form-item label="精英数量">
                  <n-input-number v-model:value="config.eliteCount" :min="1" :max="20" />
                </n-form-item>
              </n-gi>
              <n-gi>
                <n-form-item label="交叉率">
                  <n-slider v-model:value="config.crossoverRate" :min="0" :max="1" :step="0.05" />
                </n-form-item>
              </n-gi>
              <n-gi>
                <n-form-item label="变异率">
                  <n-slider v-model:value="config.mutationRate" :min="0" :max="1" :step="0.05" />
                </n-form-item>
              </n-gi>
              <n-gi>
                <n-form-item label="收敛阈值">
                  <n-slider v-model:value="config.convergenceThresh" :min="0" :max="0.01" :step="0.001" />
                </n-form-item>
              </n-gi>
            </n-grid>
          </n-collapse-item>
        </n-collapse>

        <!-- Fitness Chart -->
        <FitnessChart
          :generations="generationStats"
          :height="300"
        />

        <!-- Population Stats -->
        <n-grid :cols="4" :x-gap="16">
          <n-gi>
            <n-statistic label="最佳适应度" :value="bestFitness">
              <template #suffix>
                <n-tag :type="parseFloat(bestFitness) > 0 ? 'success' : 'warning'" size="tiny">
                  {{ parseFloat(bestFitness) > 0 ? '优秀' : '一般' }}
                </n-tag>
              </template>
            </n-statistic>
          </n-gi>
          <n-gi>
            <n-statistic label="平均适应度" :value="avgFitness" />
          </n-gi>
          <n-gi>
            <n-statistic label="种群多样性" :value="diversity">
              <template #suffix>
                <n-tag :type="parseFloat(diversity) > 0.1 ? 'success' : 'warning'" size="tiny">
                  {{ parseFloat(diversity) > 0.1 ? '健康' : '偏低' }}
                </n-tag>
              </template>
            </n-statistic>
          </n-gi>
          <n-gi>
            <n-statistic label="终止条件" :value="terminationReason" />
          </n-gi>
        </n-grid>

        <!-- Top Strategies -->
        <n-card title="顶级策略" size="small">
          <n-spin :show="loading">
            <n-empty v-if="topStrategies.length === 0" description="暂无策略" />
            <n-list v-else>
              <n-list-item v-for="(strategy, index) in topStrategies" :key="strategy.id">
                <n-thing>
                  <template #header>
                    <n-space align="center">
                      <n-tag type="success">#{{ index + 1 }}</n-tag>
                      <span>{{ strategy.name }}</span>
                      <n-tag size="small">{{ strategy.strategyType }}</n-tag>
                    </n-space>
                  </template>
                  <template #description>
                    <n-space>
                      <n-tag size="tiny">Fitness: {{ strategy.fitness?.toFixed(4) }}</n-tag>
                      <n-tag size="tiny">Sharpe: {{ strategy.sharpe?.toFixed(4) }}</n-tag>
                      <n-tag size="tiny">Return: {{ strategy.totalReturn?.toFixed(4) }}</n-tag>
                      <n-tag size="tiny">Gen: {{ strategy.generation }}</n-tag>
                    </n-space>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
          </n-spin>
        </n-card>

        <!-- Genealogy Tree -->
        <GenealogyTree
          :strategies="topStrategies"
          :height="250"
        />
      </n-space>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  PlayOutline,
  PauseOutline,
  RefreshOutline,
} from '@vicons/ionicons5'
import FitnessChart from './FitnessChart.vue'
import GenealogyTree from './GenealogyTree.vue'

interface Strategy {
  id: string
  name: string
  strategyType: string
  fitness: number
  sharpe: number
  totalReturn: number
  generation: number
  parentIDs: string[]
}

interface GenerationStat {
  generation: number
  bestFitness: number
  avgFitness: number
  worstFitness: number
  diversity: number
}

interface EvolveConfig {
  populationSize: number
  maxGenerations: number
  eliteCount: number
  crossoverRate: number
  mutationRate: number
  convergenceThresh: number
}

const loading = ref(false)
const evolving = ref(false)
const currentGen = ref(0)
const populationSize = ref(50)
const terminationReason = ref('未开始')
const generationStats = ref<GenerationStat[]>([])
const topStrategies = ref<Strategy[]>([])

const config = ref<EvolveConfig>({
  populationSize: 50,
  maxGenerations: 100,
  eliteCount: 5,
  crossoverRate: 0.8,
  mutationRate: 0.15,
  convergenceThresh: 0.001,
})

const bestFitness = computed(() => {
  if (generationStats.value.length === 0) return '0.0000'
  return generationStats.value[generationStats.value.length - 1].bestFitness.toFixed(4)
})

const avgFitness = computed(() => {
  if (generationStats.value.length === 0) return '0.0000'
  return generationStats.value[generationStats.value.length - 1].avgFitness.toFixed(4)
})

const diversity = computed(() => {
  if (generationStats.value.length === 0) return '0.0000'
  return generationStats.value[generationStats.value.length - 1].diversity.toFixed(4)
})

function handleStartEvolution() {
  evolving.value = true
  terminationReason.value = '运行中'
  // TODO: Call API to start evolution
  simulateEvolution()
}

function handlePauseEvolution() {
  evolving.value = false
  terminationReason.value = '已暂停'
}

function handleReset() {
  evolving.value = false
  currentGen.value = 0
  generationStats.value = []
  topStrategies.value = []
  terminationReason.value = '已重置'
}

function simulateEvolution() {
  // Simulation for demo purposes
  let gen = currentGen.value
  const interval = setInterval(() => {
    if (!evolving.value || gen >= config.value.maxGenerations) {
      clearInterval(interval)
      evolving.value = false
      if (gen >= config.value.maxGenerations) {
        terminationReason.value = '达到最大代数'
      }
      return
    }

    gen++
    currentGen.value = gen

    const best = 0.5 + Math.random() * 0.3 + gen * 0.005
    const avg = best - Math.random() * 0.1
    const worst = avg - Math.random() * 0.2
    const div = Math.max(0.01, 0.2 - gen * 0.002)

    generationStats.value.push({
      generation: gen,
      bestFitness: best,
      avgFitness: avg,
      worstFitness: worst,
      diversity: div,
    })

    // Update top strategies
    if (gen % 10 === 0 || gen === 1) {
      updateTopStrategies(gen)
    }
  }, 100)
}

function updateTopStrategies(gen: number) {
  const strategies: Strategy[] = []
  for (let i = 0; i < 5; i++) {
    strategies.push({
      id: `strategy_${gen}_${i}`,
      name: `Strategy ${gen}-${i}`,
      strategyType: ['momentum', 'mean_reversion', 'multi_factor'][Math.floor(Math.random() * 3)],
      fitness: 0.5 + Math.random() * 0.4,
      sharpe: 0.8 + Math.random() * 1.2,
      totalReturn: 0.1 + Math.random() * 0.3,
      generation: gen,
      parentIDs: gen > 1 ? [`strategy_${gen - 1}_${Math.floor(Math.random() * 5)}`] : [],
    })
  }
  topStrategies.value = strategies
}

onMounted(() => {
  // Load initial data
  loading.value = true
  setTimeout(() => {
    loading.value = false
  }, 500)
})
</script>

<style scoped>
.evolution-obs {
  padding: 16px;
}

.obs-card {
  max-width: 1200px;
  margin: 0 auto;
}
</style>
