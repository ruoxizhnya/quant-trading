<template>
  <div class="screener-page">
    <n-card title="🔍 选股器">
      <n-form :model="form" label-placement="left" :label-width="100">
        <n-grid :cols="4" :x-gap="16" :y-gap="16" responsive="screen">
          <n-grid-item>
            <n-form-item label="PE 上限">
              <n-input-number v-model:value="form.peMax" placeholder="30" :min="0" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="PB 上限">
              <n-input-number v-model:value="form.pbMax" placeholder="3" :min="0" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="ROE 下限 (%)">
              <n-input-number v-model:value="form.roeMin" placeholder="10" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="ROA 下限 (%)">
              <n-input-number v-model:value="form.roaMin" placeholder="5" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="毛利率 下限 (%)">
              <n-input-number v-model:value="form.grossMarginMin" placeholder="30" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="净利率 下限 (%)">
              <n-input-number v-model:value="form.netMarginMin" placeholder="10" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="市值 下限 (亿)">
              <n-input-number v-model:value="form.marketCapMin" placeholder="100" :min="0" style="width:100%" />
            </n-form-item>
          </n-grid-item>
          <n-grid-item>
            <n-form-item label="最多返回">
              <n-select v-model:value="form.limit" :options="limitOptions" style="width:100%" />
            </n-form-item>
          </n-grid-item>
        </n-grid>
        <n-space justify="end" style="margin-top: 16px;">
          <n-button type="primary" :loading="loading" @click="runScreen">🔍 开始选股</n-button>
          <n-button @click="resetForm">重置</n-button>
        </n-space>
      </n-form>
    </n-card>

    <n-card v-if="results.symbols?.length" title="选股结果" class="results-card">
      <template #header-extra>
        <n-tag type="success" round :bordered="false">找到 {{ results.total }} 只股票</n-tag>
      </template>
      <n-data-table
        :columns="columns"
        :data="tableData"
        :pagination="{ pageSize: 20 }"
        size="small"
        striped
        bordered
      />
    </n-card>

    <n-empty v-else-if="!loading && hasSearched" description="未找到符合条件的股票" class="empty-state" />

    <n-empty v-else description="设置筛选条件后开始选股" class="empty-state" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import {
  NCard, NForm, NFormItem, NInputNumber, NSelect, NButton,
  NDataTable, NEmpty, NTag, NSpace, NGrid, NGridItem, useMessage,
} from 'naive-ui'
import { screenStocks } from '@/api/market'

const message = useMessage()
const loading = ref(false)
const hasSearched = ref(false)

const form = reactive({
  peMax: null as number | null,
  pbMax: null as number | null,
  roeMin: null as number | null,
  roaMin: null as number | null,
  grossMarginMin: null as number | null,
  netMarginMin: null as number | null,
  marketCapMin: null as number | null,
  limit: 50,
})

const results = ref<{ symbols: string[]; total: number; factors?: Record<string, number>[] }>({ symbols: [], total: 0 })

const limitOptions = [
  { label: '10 只', value: 10 },
  { label: '20 只', value: 20 },
  { label: '50 只', value: 50 },
  { label: '100 只', value: 100 },
]

const columns = [
  { title: '#', key: 'index', width: 50, render: (_row: unknown, i: number) => i + 1 },
  { title: '股票代码', key: 'symbol', width: 120 },
  { title: 'PE (TTM)', key: 'pe_ttm', width: 90 },
  { title: 'PB', key: 'pb', width: 80 },
  { title: 'ROE (%)', key: 'roe', width: 90 },
  { title: 'ROA (%)', key: 'roa', width: 90 },
  { title: '毛利率 (%)', key: 'gross_margin', width: 100 },
  { title: '净利率 (%)', key: 'net_margin', width: 100 },
  { title: '市值(亿)', key: 'market_cap', width: 100 },
]

const tableData = computed(() =>
  results.value.symbols.map((sym, i) => ({
    index: i + 1,
    symbol: sym,
    ...(results.value.factors?.[i] || {}),
  }))
)

async function runScreen() {
  loading.value = true
  hasSearched.value = true
  try {
    const params: Record<string, number> = {}
    if (form.peMax != null) params.pe_max = form.peMax
    if (form.pbMax != null) params.pb_max = form.pbMax
    if (form.roeMin != null) params.roe_min = form.roeMin
    if (form.roaMin != null) params.roa_min = form.roaMin
    if (form.grossMarginMin != null) params.gross_margin_min = form.grossMarginMin
    if (form.netMarginMin != null) params.net_margin_min = form.netMarginMin
    if (form.marketCapMin != null) params.market_cap_min = form.marketCapMin
    params.limit = form.limit

    const res = await screenStocks(params)
    results.value = res
    message.success(`找到 ${res.total} 只股票`)
  } catch (e: unknown) {
    message.error('选股失败: ' + (e instanceof Error ? e.message : String(e)))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  Object.assign(form, { peMax: null, pbMax: null, roeMin: null, roaMin: null, grossMarginMin: null, netMarginMin: null, marketCapMin: null, limit: 50 })
  results.value = { symbols: [], total: 0 }
  hasSearched.value = false
}
</script>

<style scoped>
.screener-page { max-width: 1400px; margin: 0 auto; display: flex; flex-direction: column; gap: 20px; }
.empty-state { padding: 60px 0; }
.results-card { min-height: 300px; }
</style>
