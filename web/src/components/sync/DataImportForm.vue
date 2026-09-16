<template>
  <NCard title="数据导入" class="mb-4">
    <NAlert v-if="successMessage" type="success" class="mb-3" closable @close="successMessage = ''">
      {{ successMessage }}
    </NAlert>
    <NAlert v-if="store.error" type="error" class="mb-3" closable @close="store.clearError">
      {{ store.error }}
    </NAlert>

    <NForm label-placement="top">
      <NFormItem label="数据类型">
        <NSelect v-model:value="form.data_type" :options="dataTypeOptions" />
      </NFormItem>

      <NFormItem label="股票代码（逗号分隔，留空则跳过本次导入）">
        <NInput
          v-model:value="symbolsText"
          type="textarea"
          :rows="2"
          placeholder="600519,000001"
        />
      </NFormItem>

      <NFormItem label="起始日期">
        <NInput v-model:value="form.start_date" placeholder="2024-01-01" />
      </NFormItem>

      <NFormItem label="结束日期">
        <NInput v-model:value="form.end_date" placeholder="2024-12-31" />
      </NFormItem>

      <NSpace>
        <NButton type="primary" :loading="store.isLoading" :disabled="!isValid" @click="handleImport">
          开始导入
        </NButton>
        <NButton quaternary @click="resetForm">重置</NButton>
      </NSpace>
    </NForm>
  </NCard>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSelect, NSpace } from 'naive-ui'
import { useSyncStore } from '@/stores/sync'
import type { DataImportRequest } from '@/types/sync'

const store = useSyncStore()

const dataTypeOptions = [
  { label: '行情（OHLCV）', value: 'ohlcv' },
  { label: '基本面', value: 'fundamental' },
  { label: '全部', value: 'all' },
]

const form = reactive<DataImportRequest>({
  symbols: [],
  start_date: '',
  end_date: '',
  data_type: 'ohlcv',
})

const symbolsText = ref('')
const successMessage = ref('')

const isValid = computed(() => symbolsText.value.trim() !== '')

async function handleImport() {
  successMessage.value = ''
  store.clearError()
  try {
    const ids = await store.importData({
      ...form,
      symbols: symbolsText.value
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s !== ''),
    })
    successMessage.value = `已创建同步任务：${ids.join('、')}，可在上方同步状态中查看进度`
  } catch {
    // error state is rendered by the panel itself
  }
}

function resetForm() {
  form.data_type = 'ohlcv'
  form.start_date = ''
  form.end_date = ''
  symbolsText.value = ''
  successMessage.value = ''
  store.clearError()
}
</script>
