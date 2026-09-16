<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NCard,
  NSpace,
  NInput,
  NButton,
  NAlert,
  NDivider,
  NDescriptions,
  NDescriptionsItem,
  NDataTable,
  NTag,
  NText,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import { SearchOutline, OpenOutline } from '@vicons/ionicons5'
import { ApiError } from '@/api/client'
import { getFactorCitation } from '@/api/factor'
import type { CitationTuple, FactorCacheEntry } from '@/types/factor'

// TASKS.md P5-3 / ADR-022 §5: read a factor_cache row and show the evidence
// coordinate behind it, so any factor number can be walked back to the exact
// archived source response. The row is the A→B link; the tuple is the address.
const router = useRouter()

const factorName = ref('')
const symbol = ref('')
const date = ref('')

const entry = ref<FactorCacheEntry | null>(null)
const notFound = ref(false)
const errorMessage = ref<string | null>(null)
const loading = ref(false)

const canSubmit = computed(
  () =>
    factorName.value.trim() !== '' &&
    symbol.value.trim() !== '' &&
    /^\d{8}$/.test(date.value.trim()),
)

const tuples = computed<CitationTuple[]>(() => entry.value?.citation ?? [])

/** A tuple with no four-tuple fields means the response was never archived —
 *  the stored hash is still truthful, so it is shown as-is (never padded). */
function isArchived(tuple: CitationTuple): boolean {
  return Boolean(tuple.source || tuple.dataset || tuple.key || tuple.as_of)
}

async function handleLoad() {
  if (!canSubmit.value) return

  loading.value = true
  entry.value = null
  notFound.value = false
  errorMessage.value = null
  try {
    entry.value = await getFactorCitation(
      factorName.value.trim(),
      symbol.value.trim(),
      date.value.trim(),
    )
  } catch (err) {
    // 404 is the "no such factor_cache row" answer, not a failure.
    if (err instanceof ApiError && err.isNotFound) {
      notFound.value = true
    } else {
      errorMessage.value = err instanceof Error ? err.message : '因子读取失败'
    }
  } finally {
    loading.value = false
  }
}

/** One click walks the coordinate back to its ingested source response: the
 *  evidence page receives the hash and resolves it without further input. */
function traceEvidence(tuple: CitationTuple) {
  router.push({ name: 'evidence', query: { content_hash: tuple.content_hash } })
}

const columns: DataTableColumns<CitationTuple> = [
  { title: 'source', key: 'source', render: (row) => row.source || '—' },
  { title: 'dataset', key: 'dataset', render: (row) => row.dataset || '—' },
  { title: 'key', key: 'key', render: (row) => row.key || '—' },
  { title: 'as_of', key: 'as_of', render: (row) => row.as_of || '—' },
  {
    title: 'content_hash',
    key: 'content_hash',
    render: (row) =>
      h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => row.content_hash }),
  },
  {
    title: '状态',
    key: 'archived',
    render: (row) =>
      isArchived(row)
        ? h(NTag, { size: 'small', type: 'success' }, { default: () => '已归档' })
        : h(NTag, { size: 'small', type: 'warning' }, { default: () => '未归档' }),
  },
  {
    title: '操作',
    key: 'actions',
    render: (row) =>
      h(
        NButton,
        { size: 'small', onClick: () => traceEvidence(row) },
        { icon: () => h(OpenOutline), default: () => '回溯证据' },
      ),
  },
]
</script>

<template>
  <NCard title="因子证据坐标" embedded>
    <NSpace vertical>
      <NText depth="3">
        输入因子、标的与交易日，查看该 factor_cache 行的证据坐标（source / dataset / key /
        as_of / content_hash），并可一键回溯原始归档记录。
      </NText>

      <NSpace align="center">
        <NInput v-model:value="factorName" placeholder="因子（如 momentum）" clearable style="width: 240px" />
        <NInput v-model:value="symbol" placeholder="标的（如 000001.SZ）" clearable style="width: 200px" />
        <NInput v-model:value="date" placeholder="交易日 YYYYMMDD" clearable style="width: 160px" />
        <NButton type="primary" :loading="loading" :disabled="!canSubmit" @click="handleLoad">
          <template #icon>
            <SearchOutline />
          </template>
          查询
        </NButton>
      </NSpace>

      <NAlert v-if="errorMessage" type="error" closable @close="errorMessage = null">
        {{ errorMessage }}
      </NAlert>

      <NAlert v-if="notFound" type="warning">
        未命中：该 标的 / 交易日 / 因子 组合没有 factor_cache 行。
      </NAlert>

      <template v-if="entry">
        <NDivider />
        <NDescriptions label-placement="left" :column="3" size="small" bordered>
          <NDescriptionsItem label="标的">{{ entry.symbol }}</NDescriptionsItem>
          <NDescriptionsItem label="交易日">{{ entry.trade_date }}</NDescriptionsItem>
          <NDescriptionsItem label="因子">{{ entry.factor_name }}</NDescriptionsItem>
          <NDescriptionsItem label="原始值">{{ entry.raw_value }}</NDescriptionsItem>
          <NDescriptionsItem label="z-score">{{ entry.z_score }}</NDescriptionsItem>
          <NDescriptionsItem label="百分位">{{ entry.percentile }}</NDescriptionsItem>
        </NDescriptions>

        <NAlert v-if="tuples.length === 0" type="info">
          该因子行暂无证据坐标（citation 为空，尚未建立 A→B 链路）。
        </NAlert>
        <NDataTable
          v-else
          :columns="columns"
          :data="tuples"
          :row-key="(row: CitationTuple) => row.content_hash"
          :bordered="false"
          size="small"
        />
      </template>
    </NSpace>
  </NCard>
</template>