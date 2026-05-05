<script setup lang="ts">
import { ref, computed } from 'vue'
import { useSyncStore } from '@/stores/sync'
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
} from 'naive-ui'
import { SwapHorizontalOutline } from '@vicons/ionicons5'

const store = useSyncStore()

const sourceName = ref('')
const sourceType = ref<'http' | 'inmemory'>('http')
const sourceUrl = ref('')

const typeOptions = [
  { label: 'HTTP 数据源', value: 'http' },
  { label: '内存数据源', value: 'inmemory' },
]

const showUrl = computed(() => sourceType.value === 'http')

async function handleSwitch() {
  if (!sourceName.value) return
  if (sourceType.value === 'http' && !sourceUrl.value) return

  try {
    await store.switchSource(
      sourceName.value,
      sourceType.value,
      sourceType.value === 'http' ? sourceUrl.value : undefined,
    )
    // Reset form
    sourceName.value = ''
    sourceUrl.value = ''
  } catch {
    // Error is handled by store
  }
}
</script>

<template>
  <NCard title="切换数据源" embedded>
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
          :model="{ sourceName, sourceType, sourceUrl }"
          label-placement="left"
          label-width="auto"
        >
          <NFormItem label="数据源名称" required>
            <NInput
              v-model:value="sourceName"
              placeholder="输入数据源名称"
              clearable
            />
          </NFormItem>

          <NFormItem label="数据源类型" required>
            <NSelect
              v-model:value="sourceType"
              :options="typeOptions"
              placeholder="选择数据源类型"
            />
          </NFormItem>

          <NFormItem
            v-if="showUrl"
            label="URL 地址"
            required
          >
            <NInput
              v-model:value="sourceUrl"
              placeholder="http://localhost:8081"
              clearable
            />
          </NFormItem>

          <NFormItem>
            <NButton
              type="primary"
              @click="handleSwitch"
              :disabled="!sourceName || (showUrl && !sourceUrl)"
              :loading="store.isLoading"
            >
              <template #icon>
                <SwapHorizontalOutline />
              </template>
              切换数据源
            </NButton>
          </NFormItem>
        </NForm>
      </NSpace>
    </NSpin>
  </NCard>
</template>
