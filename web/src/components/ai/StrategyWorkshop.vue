<template>
  <div class="strategy-workshop">
    <n-card title="策略工坊" class="workshop-card">
      <n-space vertical size="large">
        <!-- Controls -->
        <n-space justify="space-between" align="center">
          <n-space>
            <n-button type="primary" @click="handleGenerate">
              <template #icon>
                <n-icon><SparklesOutline /></n-icon>
              </template>
              生成策略
            </n-button>
            <n-button @click="handleValidateAll">
              <template #icon>
                <n-icon><CheckmarkOutline /></n-icon>
              </template>
              验证全部
            </n-button>
          </n-space>
          <n-space>
            <n-select
              v-model:value="filterType"
              placeholder="类型筛选"
              clearable
              :options="typeOptions"
              style="width: 120px"
            />
            <n-input
              v-model:value="searchQuery"
              placeholder="搜索策略..."
              clearable
              style="width: 200px"
            >
              <template #prefix>
                <n-icon><SearchOutline /></n-icon>
              </template>
            </n-input>
          </n-space>
        </n-space>

        <!-- Stats -->
        <n-grid :cols="4" :x-gap="16">
          <n-gi>
            <n-statistic label="总策略数" :value="stats.total" />
          </n-gi>
          <n-gi>
            <n-statistic label="可编译" :value="stats.compilable">
              <template #suffix>
                <n-tag type="success" size="tiny">{{ stats.compilableRate }}</n-tag>
              </template>
            </n-statistic>
          </n-gi>
          <n-gi>
            <n-statistic label="平均Sharpe" :value="stats.avgSharpe" />
          </n-gi>
          <n-gi>
            <n-statistic label="平均回撤" :value="stats.avgDrawdown">
              <template #suffix>
                <n-tag :type="parseFloat(stats.avgDrawdown) < 0.15 ? 'success' : 'warning'" size="tiny">
                  {{ parseFloat(stats.avgDrawdown) < 0.15 ? '可控' : '偏高' }}
                </n-tag>
              </template>
            </n-statistic>
          </n-gi>
        </n-grid>

        <!-- Strategy Grid -->
        <n-spin :show="loading">
          <n-empty v-if="filteredStrategies.length === 0" description="暂无策略" />
          <n-grid v-else :cols="2" :x-gap="16" :y-gap="16">
            <n-gi v-for="strategy in filteredStrategies" :key="strategy.id">
              <StrategyCard
                :strategy="strategy"
                @view="handleView"
                @validate="handleValidate"
                @delete="handleDelete"
              />
            </n-gi>
          </n-grid>
        </n-spin>
      </n-space>
    </n-card>

    <!-- Generate Modal -->
    <n-modal v-model:show="showGenerateModal" title="生成新策略" preset="card" style="width: 600px">
      <n-space vertical>
        <n-input
          v-model:value="generateDescription"
          type="textarea"
          placeholder="输入策略描述，例如：基于20日均线和成交量突破的动量策略"
          :rows="4"
        />
        <n-space justify="end">
          <n-button @click="showGenerateModal = false">取消</n-button>
          <n-button type="primary" :loading="generating" @click="confirmGenerate">
            生成
          </n-button>
        </n-space>
      </n-space>
    </n-modal>

    <!-- Detail Modal -->
    <n-modal v-model:show="showDetailModal" title="策略详情" preset="card" style="width: 800px">
      <n-space vertical v-if="selectedStrategy">
        <n-descriptions :column="2">
          <n-descriptions-item label="名称">{{ selectedStrategy.name }}</n-descriptions-item>
          <n-descriptions-item label="类型">
            <n-tag>{{ selectedStrategy.type }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="描述" :span="2">{{ selectedStrategy.description }}</n-descriptions-item>
          <n-descriptions-item label="收益率">{{ (selectedStrategy.totalReturn * 100)?.toFixed(1) }}%</n-descriptions-item>
          <n-descriptions-item label="Sharpe">{{ selectedStrategy.sharpe?.toFixed(2) }}</n-descriptions-item>
          <n-descriptions-item label="最大回撤">{{ (selectedStrategy.maxDrawdown * 100)?.toFixed(1) }}%</n-descriptions-item>
          <n-descriptions-item label="胜率">{{ (selectedStrategy.winRate * 100)?.toFixed(1) }}%</n-descriptions-item>
        </n-descriptions>
        <n-divider />
        <n-text strong>策略代码</n-text>
        <n-code :code="selectedStrategy.code" language="go" show-line-numbers />
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  SparklesOutline,
  CheckmarkOutline,
  SearchOutline,
} from '@vicons/ionicons5'
import StrategyCard from './StrategyCard.vue'

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

const loading = ref(false)
const strategies = ref<Strategy[]>([])
const filterType = ref('')
const searchQuery = ref('')
const showGenerateModal = ref(false)
const showDetailModal = ref(false)
const selectedStrategy = ref<Strategy | null>(null)
const generateDescription = ref('')
const generating = ref(false)

const typeOptions = [
  { label: '动量', value: 'momentum' },
  { label: '均值回归', value: 'mean_reversion' },
  { label: '多因子', value: 'multi_factor' },
  { label: '突破', value: 'breakout' },
]

const filteredStrategies = computed(() => {
  let result = strategies.value

  if (filterType.value) {
    result = result.filter(s => s.type === filterType.value)
  }

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(s =>
      s.name.toLowerCase().includes(query) ||
      s.description.toLowerCase().includes(query)
    )
  }

  return result
})

const stats = computed(() => {
  const total = strategies.value.length
  const compilable = strategies.value.filter(s => s.status === 'compilable').length
  const avgSharpe = total > 0
    ? strategies.value.reduce((sum, s) => sum + (s.sharpe || 0), 0) / total
    : 0
  const avgDrawdown = total > 0
    ? strategies.value.reduce((sum, s) => sum + (s.maxDrawdown || 0), 0) / total
    : 0

  return {
    total,
    compilable,
    avgSharpe: avgSharpe.toFixed(2),
    avgDrawdown: avgDrawdown.toFixed(3),
    compilableRate: total > 0 ? `${((compilable / total) * 100).toFixed(1)}%` : '0%',
  }
})

function parseFloat(val: string): number {
  return parseFloat(val) || 0
}

function handleGenerate() {
  showGenerateModal.value = true
}

async function confirmGenerate() {
  if (!generateDescription.value.trim()) return

  generating.value = true
  try {
    const newStrategy: Strategy = {
      id: `strategy_${Date.now()}`,
      name: `Generated Strategy ${strategies.value.length + 1}`,
      type: 'momentum',
      description: generateDescription.value,
      code: '// TODO: Generated code',
      totalReturn: 0,
      sharpe: 0,
      maxDrawdown: 0,
      winRate: 0,
      fitness: 0,
      status: 'pending',
    }
    strategies.value.unshift(newStrategy)
    showGenerateModal.value = false
    generateDescription.value = ''
  } finally {
    generating.value = false
  }
}

function handleValidateAll() {
  // TODO: Implement batch validation
}

function handleView(strategy: Strategy) {
  selectedStrategy.value = strategy
  showDetailModal.value = true
}

function handleValidate(strategy: Strategy) {
  // TODO: Implement validation
  console.log('Validate:', strategy.id)
}

function handleDelete(strategy: Strategy) {
  const index = strategies.value.findIndex(s => s.id === strategy.id)
  if (index > -1) {
    strategies.value.splice(index, 1)
  }
}

onMounted(() => {
  strategies.value = [
    {
      id: 'strategy_1',
      name: '双均线动量',
      type: 'momentum',
      description: '基于20日和60日均线的交叉策略',
      code: 'func (s *Strategy) GenerateSignals(bars []OHLCV) []Signal { ... }',
      totalReturn: 0.25,
      sharpe: 1.35,
      maxDrawdown: 0.12,
      winRate: 0.58,
      fitness: 8.2,
      status: 'compilable',
    },
    {
      id: 'strategy_2',
      name: 'RSI均值回归',
      type: 'mean_reversion',
      description: 'RSI超卖买入，超买卖出',
      code: 'func (s *Strategy) GenerateSignals(bars []OHLCV) []Signal { ... }',
      totalReturn: 0.18,
      sharpe: 1.1,
      maxDrawdown: 0.08,
      winRate: 0.62,
      fitness: 7.5,
      status: 'compilable',
    },
  ]
})
</script>

<style scoped>
.strategy-workshop {
  padding: 16px;
}

.workshop-card {
  min-height: 600px;
}
</style>
