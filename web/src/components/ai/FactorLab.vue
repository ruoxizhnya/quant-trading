<template>
  <div class="factor-lab">
    <n-card title="因子实验室" class="lab-card">
      <n-space vertical size="large">
        <!-- Header Controls -->
        <n-space justify="space-between" align="center">
          <n-space>
            <n-button type="primary" @click="handleGenerate">
              <template #icon>
                <n-icon><SparklesOutline /></n-icon>
              </template>
              生成因子
            </n-button>
            <n-button @click="handleMutateAll">
              <template #icon>
                <n-icon><GitBranchOutline /></n-icon>
              </template>
              批量变异
            </n-button>
            <n-button @click="handleValidateAll">
              <template #icon>
                <n-icon><CheckmarkOutline /></n-icon>
              </template>
              批量验证
            </n-button>
          </n-space>
          <n-space>
            <n-select
              v-model:value="filterCategory"
              placeholder="分类筛选"
              clearable
              :options="categoryOptions"
              style="width: 120px"
            />
            <n-select
              v-model:value="filterStatus"
              placeholder="状态筛选"
              clearable
              :options="statusOptions"
              style="width: 120px"
            />
            <n-input
              v-model:value="searchQuery"
              placeholder="搜索因子..."
              clearable
              style="width: 200px"
            >
              <template #prefix>
                <n-icon><SearchOutline /></n-icon>
              </template>
            </n-input>
          </n-space>
        </n-space>

        <!-- Stats Overview -->
        <n-grid :cols="5" :x-gap="16">
          <n-gi>
            <n-statistic label="总因子数" :value="stats.total" />
          </n-gi>
          <n-gi>
            <n-statistic label="已验证" :value="stats.validated">
              <template #suffix>
                <n-tag type="success" size="tiny">{{ stats.validatedRate }}</n-tag>
              </template>
            </n-statistic>
          </n-gi>
          <n-gi>
            <n-statistic label="待验证" :value="stats.pending" />
          </n-gi>
          <n-gi>
            <n-statistic label="已拒绝" :value="stats.rejected" />
          </n-gi>
          <n-gi>
            <n-statistic label="平均IC" :value="stats.avgIC">
              <template #suffix>
                <n-tag :type="stats.avgICRaw >= 0.03 ? 'success' : 'warning'" size="tiny">
                  {{ stats.avgICRaw >= 0.03 ? '优秀' : '一般' }}
                </n-tag>
              </template>
            </n-statistic>
          </n-gi>
        </n-grid>

        <!-- Factor Grid -->
        <n-spin :show="loading">
          <n-empty v-if="filteredFactors.length === 0" description="暂无因子" />
          <n-grid v-else :cols="2" :x-gap="16" :y-gap="16">
            <n-gi v-for="factor in filteredFactors" :key="factor.id">
              <FactorCard
                :factor="factor"
                @view="handleView"
                @mutate="handleMutate"
                @delete="handleDelete"
              />
            </n-gi>
          </n-grid>
        </n-spin>

        <!-- Pagination -->
        <n-space justify="center" v-if="filteredFactors.length > 0">
          <n-pagination
            v-model:page="currentPage"
            :page-size="pageSize"
            :item-count="filteredFactors.length"
            show-size-picker
            :page-sizes="[10, 20, 50]"
          />
        </n-space>
      </n-space>
    </n-card>

    <!-- Generate Modal -->
    <n-modal v-model:show="showGenerateModal" title="生成新因子" preset="card" style="width: 600px">
      <n-space vertical>
        <n-input
          v-model:value="generateTopic"
          type="textarea"
          placeholder="输入研究主题，例如：20日价格动量因子"
          :rows="3"
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
    <n-modal v-model:show="showDetailModal" title="因子详情" preset="card" style="width: 700px">
      <n-space vertical v-if="selectedFactor">
        <n-descriptions :column="2">
          <n-descriptions-item label="名称">{{ selectedFactor.name }}</n-descriptions-item>
          <n-descriptions-item label="分类">
            <n-tag>{{ selectedFactor.category }}</n-tag>
          </n-descriptions-item>
          <n-descriptions-item label="公式" :span="2">
            <n-text code>{{ selectedFactor.formula }}</n-text>
          </n-descriptions-item>
          <n-descriptions-item label="IC">{{ selectedFactor.ic?.toFixed(4) }}</n-descriptions-item>
          <n-descriptions-item label="IR">{{ selectedFactor.ir?.toFixed(4) }}</n-descriptions-item>
          <n-descriptions-item label="换手率">{{ (selectedFactor.turnover * 100)?.toFixed(1) }}%</n-descriptions-item>
          <n-descriptions-item label="Sharpe">{{ selectedFactor.sharpe?.toFixed(4) }}</n-descriptions-item>
          <n-descriptions-item label="Fitness">{{ selectedFactor.fitness?.toFixed(4) }}</n-descriptions-item>
          <n-descriptions-item label="状态">
            <n-tag :type="selectedFactor.status === 'validated' ? 'success' : 'warning'">
              {{ selectedFactor.status }}
            </n-tag>
          </n-descriptions-item>
        </n-descriptions>
        <n-text>{{ selectedFactor.description }}</n-text>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  SparklesOutline,
  GitBranchOutline,
  CheckmarkOutline,
  SearchOutline,
} from '@vicons/ionicons5'
import FactorCard from './FactorCard.vue'

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

const loading = ref(false)
const factors = ref<Factor[]>([])
const filterCategory = ref('')
const filterStatus = ref('')
const searchQuery = ref('')
const currentPage = ref(1)
const pageSize = ref(20)
const showGenerateModal = ref(false)
const showDetailModal = ref(false)
const selectedFactor = ref<Factor | null>(null)
const generateTopic = ref('')
const generating = ref(false)

const categoryOptions = [
  { label: '动量', value: 'momentum' },
  { label: '价值', value: 'value' },
  { label: '质量', value: 'quality' },
  { label: '波动率', value: 'volatility' },
  { label: '流动性', value: 'liquidity' },
]

const statusOptions = [
  { label: '已验证', value: 'validated' },
  { label: '待验证', value: 'pending' },
  { label: '已拒绝', value: 'rejected' },
]

const filteredFactors = computed(() => {
  let result = factors.value

  if (filterCategory.value) {
    result = result.filter(f => f.category === filterCategory.value)
  }

  if (filterStatus.value) {
    result = result.filter(f => f.status === filterStatus.value)
  }

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    result = result.filter(f =>
      f.name.toLowerCase().includes(query) ||
      f.formula.toLowerCase().includes(query) ||
      f.description.toLowerCase().includes(query)
    )
  }

  return result
})

const stats = computed(() => {
  const total = factors.value.length
  const validated = factors.value.filter(f => f.status === 'validated').length
  const pending = factors.value.filter(f => f.status === 'pending').length
  const rejected = factors.value.filter(f => f.status === 'rejected').length
  const avgIC = total > 0
    ? factors.value.reduce((sum, f) => sum + (f.ic || 0), 0) / total
    : 0

  return {
    total,
    validated,
    pending,
    rejected,
    avgIC: avgIC.toFixed(3),
    avgICRaw: avgIC,
    validatedRate: total > 0 ? `${((validated / total) * 100).toFixed(1)}%` : '0%',
  }
})

function handleGenerate() {
  showGenerateModal.value = true
}

async function confirmGenerate() {
  if (!generateTopic.value.trim()) return

  generating.value = true
  try {
    // TODO: Call API to generate factor
    const newFactor: Factor = {
      id: `factor_${Date.now()}`,
      name: `Generated Factor ${factors.value.length + 1}`,
      category: 'momentum',
      formula: 'ts_mean(close, 20)',
      description: generateTopic.value,
      ic: 0,
      ir: 0,
      turnover: 0,
      sharpe: 0,
      fitness: 0,
      status: 'pending',
    }
    factors.value.unshift(newFactor)
    showGenerateModal.value = false
    generateTopic.value = ''
  } finally {
    generating.value = false
  }
}

function handleMutateAll() {
  // TODO: Implement batch mutation
}

function handleValidateAll() {
  // TODO: Implement batch validation
}

function handleView(factor: Factor) {
  selectedFactor.value = factor
  showDetailModal.value = true
}

function handleMutate(factor: Factor) {
  // TODO: Implement mutation
  console.log('Mutate:', factor.id)
}

function handleDelete(factor: Factor) {
  const index = factors.value.findIndex(f => f.id === factor.id)
  if (index > -1) {
    factors.value.splice(index, 1)
  }
}

// Load sample data on mount
onMounted(() => {
  factors.value = [
    {
      id: 'factor_1',
      name: '20日动量',
      category: 'momentum',
      formula: 'ts_pct_change(close, 20)',
      description: '20日价格动量因子',
      ic: 0.045,
      ir: 0.65,
      turnover: 0.32,
      sharpe: 1.2,
      fitness: 8.5,
      status: 'validated',
    },
    {
      id: 'factor_2',
      name: '60日波动率',
      category: 'volatility',
      formula: 'ts_std(close, 60)',
      description: '60日价格波动率因子',
      ic: 0.032,
      ir: 0.45,
      turnover: 0.28,
      sharpe: 0.9,
      fitness: 6.2,
      status: 'validated',
    },
    {
      id: 'factor_3',
      name: 'PE比率',
      category: 'value',
      formula: 'cs_rank(pe)',
      description: '市盈率排名因子',
      ic: 0.028,
      ir: 0.38,
      turnover: 0.15,
      sharpe: 0.7,
      fitness: 5.1,
      status: 'pending',
    },
  ]
})
</script>

<style scoped>
.factor-lab {
  padding: 16px;
}

.lab-card {
  min-height: 600px;
}
</style>
