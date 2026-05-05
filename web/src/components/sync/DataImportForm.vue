<script setup lang="ts">
import { ref } from 'vue'
import { useSyncStore } from '@/stores/sync'
import type { DataImportRequest } from '@/types/sync'
import {
  NCard,
  NSpace,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NAlert,
  NSpin,
  NDatePicker,
} from 'naive-ui'
import { CloudUploadOutline } from '@vicons/ionicons5'

const store = useSyncStore()

const symbols = ref('')
const dateRange = ref<[number, number] | null>(null)
const dataType = ref<'ohlcv' | 'fundamental' | 'all'>('ohlcv')

const dataTypeOptions = [
  { label: 'OHLCV 行情数据', value: 'ohlcv' },
  { label: '基本面数据', value: 'fundamental' },
  { label: '全部数据', value: 'all' },
]

async function handleImport() {
  if (!symbols.value || !dateRange.value) return

  const symbolList = symbols.value.split(',').map(s => s.trim()).filter(Boolean)
  if (symbolList.length === 0) return

  const startDate = new Date(dateRange.value[0]).toISOString().split('T')[0]
  const endDate = new Date(dateRange.value[1]).toISOString().split('T')[0]

  const request: DataImportRequest = {
    symbols: symbolList,
    start_date: startDate,
    end_date: endDate,
    data_type: dataType.value,
  }

  try {
    await store.importData(request)
    // Reset form
    symbols.value = ''
    dateRange.value = null
  } catch {
    // Error is handled by store
  }
}
</script>

<template>
  <NCard title="数据导入" embedded>
    <NSpin :show="store.isLoading">
      <NSpace vertical>
        <NAlert
          v-if="store.error"
          type="error"
          closable
          @close="store.clearError"
        >
          {{ store.error }}
        </NAlert>

        <NForm
          label-placement="left"
          label-width="auto"
        >
          <NFormItem label="股票代码" required>
            <NInput
              v-model:value="symbols"
              placeholder="输入股票代码，用逗号分隔 (如: AAPL,GOOGL,MSFT)"
              type="textarea"
              :rows="2"
              clearable
            />
          </NFormItem>

          <NFormItem label="日期范围" required>
            <NDatePicker
              v-model:value="dateRange"
              type="daterange"
              clearable
              placeholder="选择数据日期范围"
            />
          </NFormItem>

          <NFormItem label="数据类型" required>
            <NSelect
              v-model:value="dataType"
              :options="dataTypeOptions"
              placeholder="选择数据类型"
            />
          </NFormItem>

          <NFormItem>
            <NButton
              type="primary"
              @click="handleImport"
              :disabled="!symbols || !dateRange"
              :loading="store.isLoading"
            >
              <template #icon>
                <CloudUploadOutline />
              </template>
              开始导入
            </NButton>
          </NFormItem>
        </NForm>
      </NSpace>
    </NSpin>
  </NCard>
</template>
