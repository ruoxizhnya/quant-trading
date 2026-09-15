<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  NCard,
  NSpace,
  NInput,
  NButton,
  NAlert,
  NDivider,
  NDescriptions,
  NDescriptionsItem,
  NCode,
  NText,
} from 'naive-ui'
import { SearchOutline } from '@vicons/ionicons5'
import { ApiError } from '@/api/client'
import { getEvidence } from '@/api/evidence'
import type { RawIngest } from '@/types/evidence'

// ADR-022 §5: the L0 evidence lookup. One content_hash in, exactly one
// archived source response out (or an explicit "not ingested").
const contentHash = ref('')
const record = ref<RawIngest | null>(null)
const notIngested = ref(false)
const errorMessage = ref<string | null>(null)
const loading = ref(false)

const canSubmit = computed(() => contentHash.value.trim().length > 0)
const payloadText = computed(() =>
  record.value ? JSON.stringify(record.value.payload, null, 2) : '',
)

async function handleLookup() {
  const hash = contentHash.value.trim()
  if (!hash) return

  loading.value = true
  record.value = null
  notIngested.value = false
  errorMessage.value = null
  try {
    record.value = await getEvidence(hash)
  } catch (err) {
    // 404 is the "not ingested" answer, not a failure.
    if (err instanceof ApiError && err.isNotFound) {
      notIngested.value = true
    } else {
      errorMessage.value = err instanceof Error ? err.message : '证据查询失败'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <NCard title="证据查询" embedded>
    <NSpace vertical>
      <NText depth="3">
        输入 64 位 content_hash，回溯该数字在 ingest.raw 中的唯一原始记录。
      </NText>

      <NSpace align="center">
        <NInput
          v-model:value="contentHash"
          placeholder="content_hash（sha256 小写 hex）"
          clearable
          style="width: 520px"
        />
        <NButton
          type="primary"
          :loading="loading"
          :disabled="!canSubmit"
          @click="handleLookup"
        >
          <template #icon>
            <SearchOutline />
          </template>
          查询
        </NButton>
      </NSpace>

      <NAlert
        v-if="errorMessage"
        type="error"
        closable
        @close="errorMessage = null"
      >
        {{ errorMessage }}
      </NAlert>

      <NAlert v-if="notIngested" type="warning">
        未摄取：该 content_hash 不在 ingest.raw 中。
      </NAlert>

      <template v-if="record">
        <NDivider />
        <NDescriptions label-placement="left" :column="1" size="small" bordered>
          <NDescriptionsItem label="content_hash">
            {{ record.content_hash }}
          </NDescriptionsItem>
          <NDescriptionsItem label="来源">
            {{ record.source }}
          </NDescriptionsItem>
          <NDescriptionsItem label="数据集">
            {{ record.dataset }}
          </NDescriptionsItem>
          <NDescriptionsItem label="键">
            {{ record.key }}
          </NDescriptionsItem>
          <NDescriptionsItem label="数据时点">
            {{ record.as_of || '—' }}
          </NDescriptionsItem>
          <NDescriptionsItem label="归档时间">
            {{ record.fetched_at }}
          </NDescriptionsItem>
        </NDescriptions>

        <NCode :code="payloadText" language="json" word-wrap />
      </template>
    </NSpace>
  </NCard>
</template>