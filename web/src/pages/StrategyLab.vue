<template>
  <div class="strategy-lab-page">
    <div class="lab-header">
      <h1>策略实验室</h1>
      <n-space align="center" :size="12">
        <n-input v-model:value="searchQuery" placeholder="搜索策略..." clearable style="width:200px" />
        <n-select v-model:value="filterType" :options="typeOptions" style="width:130px" placeholder="全部类型" clearable />
        <n-select v-model:value="filterPeriod" :options="periodOptions" style="width:120px" placeholder="全部周期" clearable />
        <n-button type="primary" @click="showCreateModal = true">➕ 新建策略</n-button>
      </n-space>
    </div>

    <div class="strategy-grid">
      <n-card v-for="s in filteredStrategies" :key="s.id" class="strategy-card" hoverable>
        <div class="card-header">
          <span class="card-name">{{ s.name }}</span>
          <n-tag :type="s.category === 'classic' ? 'info' : s.category === 'ml' ? 'warning' : 'default'" size="small" round :bordered="false">
            {{ categoryLabel(s.category) }}
          </n-tag>
        </div>
        <p class="card-desc">{{ s.description }}</p>
        <div class="card-actions">
          <n-button size="small" type="primary" @click="runBacktest(s)">📈 回测</n-button>
          <n-button size="small" quaternary type="error" @click="deleteStrategy(s)">🗑️ 删除</n-button>
        </div>
      </n-card>
    </div>

    <n-empty v-if="filteredStrategies.length === 0 && !loading" description="暂无策略" class="empty-state" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NCard, NButton, NInput, NSelect, NSpace, NTag, NEmpty, useMessage, useDialog } from 'naive-ui'
import { getStrategies } from '@/api/strategy'
import type { Strategy } from '@/types/api'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const strategies = ref<Strategy[]>([])
const searchQuery = ref('')
const filterType = ref<string | null>(null)
const filterPeriod = ref<string | null>(null)
const showCreateModal = ref(false)

const typeOptions = [
  { label: '传统策略', value: 'classic' },
  { label: '机器学习', value: 'ml' },
  { label: '自定义策略', value: 'custom' },
]

const periodOptions = [
  { label: '日线', value: '1d' },
  { label: '小时线', value: '1h' },
  { label: '分钟线', value: '15m' },
  { label: 'Tick', value: 'tick' },
]

const filteredStrategies = computed(() => {
  let list = strategies.value
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(s => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q))
  }
  if (filterType.value) list = list.filter(s => s.category === filterType.value)
  return list
})

function categoryLabel(cat: string): string {
  const map: Record<string, string> = { classic: '传统策略', ml: '机器学习', custom: '自定义' }
  return map[cat] || cat
}

onMounted(async () => {
  loading.value = true
  try {
    const res = await getStrategies()
    strategies.value = res.strategies || []
  } catch (e: unknown) {
    message.error('加载失败: ' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
})

async function runBacktest(strategy: Strategy) {
  router.push({
    name: 'backtest',
    query: { strategy: strategy.name || strategy.id, stock: '600000.SH', start: '2024-01-01', end: '2024-12-31' },
  })
}

function deleteStrategy(s: Strategy) {
  dialog.warning({
    title: '确认删除',
    content: `确定要删除策略 "${s.name}" 吗？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => {
      strategies.value = strategies.value.filter(x => x.id !== s.id)
      message.success(`已删除 ${s.name}`)
    },
  })
}
</script>

<style scoped>
.strategy-lab-page { max-width: 1200px; margin: 0 auto; }

.lab-header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 24px; flex-wrap: wrap; gap: 12px;
}
.lab-header h1 { font-size: 22px; font-weight: 700; color: var(--q-text); }

.strategy-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 16px;
}

.strategy-card { cursor: default; transition: transform var(--q-transition); }
.strategy-card:hover { transform: translateY(-2px); }

.card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.card-name { font-size: 15px; font-weight: 600; color: var(--q-text); }
.card-desc { font-size: 13px; color: var(--q-text2); line-height: 1.5; margin-bottom: 12px; min-height: 40px; }
.card-actions { display: flex; justify-content: flex-end; gap: 8px; }

.empty-state { padding: 80px 0; }
</style>
