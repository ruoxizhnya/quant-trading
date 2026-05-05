<template>
  <div class="pipeline-dashboard">
    <n-card title="AI 策略生成流水线" class="dashboard-card">
      <n-space vertical size="large">
        <!-- Input Section -->
        <n-card title="策略描述" size="small">
          <n-input
            v-model:value="strategyDescription"
            type="textarea"
            placeholder="请输入策略描述，例如：20日动量策略，在沪深300中选出最强10只股票，止损5%"
            :rows="3"
            :disabled="isProcessing"
          />
          <n-space justify="end" style="margin-top: 12px">
            <n-button
              type="primary"
              :loading="isProcessing"
              :disabled="!strategyDescription.trim()"
              @click="handleGenerate"
            >
              <template #icon>
                <n-icon><SparklesOutline /></n-icon>
              </template>
              生成策略
            </n-button>
            <n-button
              :disabled="isProcessing || !strategyDescription.trim()"
              @click="handleClear"
            >
              清空
            </n-button>
          </n-space>
        </n-card>

        <!-- Pipeline Progress -->
        <n-card v-if="currentJob" title="执行进度" size="small">
          <n-steps :current="currentStep" :status="stepStatus">
            <n-step title="意图解析" description="解析策略描述" />
            <n-step title="YAML生成" description="生成配置文件" />
            <n-step title="代码生成" description="生成策略代码" />
            <n-step title="编译验证" description="验证代码编译" />
            <n-step title="回测执行" description="运行回测" />
          </n-steps>

          <n-space vertical style="margin-top: 16px">
            <n-progress
              type="line"
              :percentage="progressPercentage"
              :indicator-placement="'inside'"
              :processing="isProcessing"
              :status="progressStatus"
            />
            <n-text depth="3">{{ statusMessage }}</n-text>
          </n-space>
        </n-card>

        <!-- Results Section -->
        <n-card v-if="currentResult" title="生成结果" size="small">
          <n-tabs type="line" animated>
            <!-- Intent Tab -->
            <n-tab-pane name="intent" tab="解析结果">
              <n-descriptions bordered :columns="2" size="small">
                <n-descriptions-item label="策略类型">
                  <n-tag :type="getStrategyTypeTag(currentResult.intent?.strategy_type)">
                    {{ currentResult.intent?.strategy_type || '未知' }}
                  </n-tag>
                </n-descriptions-item>
                <n-descriptions-item label="策略名称">
                  {{ currentResult.intent?.strategy_name || '-' }}
                </n-descriptions-item>
                <n-descriptions-item label="股票池" :span="2">
                  {{ currentResult.intent?.universe || 'csi300' }}
                </n-descriptions-item>
                <n-descriptions-item label="时间周期">
                  {{ currentResult.intent?.timeframe || '1d' }}
                </n-descriptions-item>
                <n-descriptions-item label="置信度">
                  <n-progress
                    type="line"
                    :percentage="Math.round((currentResult.intent?.confidence || 0) * 100)"
                    :show-indicator="false"
                    style="width: 100px"
                  />
                </n-descriptions-item>
              </n-descriptions>

              <n-divider />

              <n-h4>参数配置</n-h4>
              <n-data-table
                :columns="parameterColumns"
                :data="currentResult.intent?.parameters || []"
                :bordered="false"
                :single-line="false"
                size="small"
              />

              <n-divider />

              <n-h4>技术指标</n-h4>
              <n-space>
                <n-tag
                  v-for="indicator in currentResult.intent?.indicators || []"
                  :key="indicator"
                  type="info"
                >
                  {{ indicator }}
                </n-tag>
              </n-space>
            </n-tab-pane>

            <!-- YAML Config Tab -->
            <n-tab-pane name="yaml" tab="YAML配置">
              <n-code :code="currentResult.yaml_config || ''" language="yaml" show-line-numbers />
              <n-space justify="end" style="margin-top: 12px">
                <n-button size="small" @click="copyYAML">
                  <template #icon>
                    <n-icon><CopyOutline /></n-icon>
                  </template>
                  复制
                </n-button>
                <n-button size="small" type="primary" @click="downloadYAML">
                  <template #icon>
                    <n-icon><DownloadOutline /></n-icon>
                  </template>
                  下载
                </n-button>
              </n-space>
            </n-tab-pane>

            <!-- Generated Code Tab -->
            <n-tab-pane name="code" tab="生成代码">
              <n-code
                :code="currentResult.generated_code || ''"
                language="go"
                show-line-numbers
              />
              <n-space justify="end" style="margin-top: 12px">
                <n-button size="small" @click="copyCode">
                  <template #icon>
                    <n-icon><CopyOutline /></n-icon>
                  </template>
                  复制
                </n-button>
              </n-space>
            </n-tab-pane>

            <!-- Backtest Result Tab -->
            <n-tab-pane
              v-if="currentResult.backtest_result"
              name="backtest"
              tab="回测结果"
            >
              <BacktestResultCard :result="currentResult.backtest_result" />
            </n-tab-pane>

            <!-- Logs Tab -->
            <n-tab-pane name="logs" tab="执行日志">
              <n-log :log="logsText" :rows="15" />
            </n-tab-pane>
          </n-tabs>
        </n-card>

        <!-- Error Display -->
        <n-alert
          v-if="currentResult?.build_error"
          type="error"
          :title="'构建错误'"
          closable
        >
          <pre>{{ currentResult.build_error }}</pre>
        </n-alert>

        <n-alert
          v-if="currentResult?.backtest_error"
          type="warning"
          :title="'回测错误'"
          closable
        >
          <pre>{{ currentResult.backtest_error }}</pre>
        </n-alert>
      </n-space>
    </n-card>

    <!-- History Section -->
    <n-card title="历史记录" class="history-card" style="margin-top: 16px">
      <n-data-table
        :columns="historyColumns"
        :data="jobHistory"
        :bordered="false"
        :single-line="false"
        size="small"
        @update:page="handlePageChange"
      />
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import {
  NCard,
  NSpace,
  NInput,
  NButton,
  NIcon,
  NSteps,
  NStep,
  NProgress,
  NText,
  NTabs,
  NTabPane,
  NDescriptions,
  NDescriptionsItem,
  NTag,
  NDataTable,
  NDivider,
  NH4,
  NCode,
  NLog,
  NAlert,
  useMessage
} from 'naive-ui'
import {
  SparklesOutline,
  CopyOutline,
  DownloadOutline
} from '@vicons/ionicons5'
import { runPipeline } from '@/api/copilot'
import type { PipelineResult } from '@/types/pipeline'
import BacktestResultCard from './BacktestResultCard.vue'

const message = useMessage()

// State
const strategyDescription = ref('')
const isProcessing = ref(false)
const currentJob = ref<string | null>(null)
const currentResult = ref<PipelineResult | null>(null)
const jobHistory = ref<PipelineResult[]>([])
const currentStep = ref(0)
const statusMessage = ref('')

// Computed
const progressPercentage = computed(() => {
  if (!currentResult.value) return 0
  const stepMap: Record<string, number> = {
    'parse': 20,
    'generate': 40,
    'validate': 60,
    'compile': 80,
    'backtest': 100,
    'complete': 100,
    'failed': 0
  }
  return stepMap[currentResult.value.status] || 0
})

const progressStatus = computed(() => {
  if (!currentResult.value) return 'default'
  if (currentResult.value.status === 'failed') return 'error'
  if (currentResult.value.status === 'complete') return 'success'
  return 'default'
})

const stepStatus = computed(() => {
  if (!currentResult.value) return 'wait'
  if (currentResult.value.status === 'failed') return 'error'
  if (currentResult.value.status === 'complete') return 'finish'
  return 'process'
})

const logsText = computed(() => {
  return currentResult.value?.logs?.join('\n') || ''
})

// Table columns
const parameterColumns = [
  { title: '参数名', key: 'name' },
  { title: '类型', key: 'type' },
  { title: '值', key: 'value' },
  { title: '描述', key: 'description' }
]

const historyColumns = [
  { title: '策略名称', key: 'intent.strategy_name' },
  { title: '策略类型', key: 'intent.strategy_type' },
  { title: '状态', key: 'status' },
  { title: '耗时(ms)', key: 'duration_ms' },
  { title: '时间', key: 'started_at' }
]

// Methods
async function handleGenerate() {
  if (!strategyDescription.value.trim()) return

  isProcessing.value = true
  currentStep.value = 0
  statusMessage.value = '开始解析意图...'
  currentResult.value = null

  try {
    const result = await runPipeline(strategyDescription.value)
    currentResult.value = result
    currentJob.value = result.id

    // Update history
    jobHistory.value.unshift(result)

    // Show success/error message
    if (result.status === 'complete') {
      message.success('策略生成成功！')
    } else if (result.status === 'failed') {
      message.error(`策略生成失败: ${result.build_error || '未知错误'}`)
    }
  } catch (error) {
    message.error(`请求失败: ${error}`)
  } finally {
    isProcessing.value = false
  }
}

function handleClear() {
  strategyDescription.value = ''
  currentResult.value = null
  currentJob.value = null
}

function handlePageChange(page: number) {
  console.log('Page changed to:', page)
}

function getStrategyTypeTag(type: string | undefined): 'default' | 'error' | 'primary' | 'info' | 'success' | 'warning' {
  const tagMap: Record<string, 'default' | 'error' | 'primary' | 'info' | 'success' | 'warning'> = {
    'momentum': 'success',
    'mean_reversion': 'warning',
    'breakout': 'error',
    'multi_factor': 'info',
    'value': 'success',
    'quality': 'info',
    'custom': 'default'
  }
  return tagMap[type || ''] || 'default'
}

function copyYAML() {
  if (!currentResult.value?.yaml_config) return
  navigator.clipboard.writeText(currentResult.value.yaml_config)
  message.success('YAML已复制到剪贴板')
}

function downloadYAML() {
  if (!currentResult.value?.yaml_config) return
  const blob = new Blob([currentResult.value.yaml_config], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${currentResult.value.intent?.strategy_name || 'strategy'}.yaml`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  message.success('YAML已下载')
}

function copyCode() {
  if (!currentResult.value?.generated_code) return
  navigator.clipboard.writeText(currentResult.value.generated_code)
  message.success('代码已复制到剪贴板')
}

// Watch for status changes to update step
watch(
  () => currentResult.value?.status,
  (status) => {
    const stepMap: Record<string, number> = {
      'parse': 0,
      'generate': 1,
      'validate': 2,
      'compile': 3,
      'backtest': 4,
      'complete': 5,
      'failed': 5
    }
    currentStep.value = stepMap[status || ''] || 0

    const messageMap: Record<string, string> = {
      'parse': '正在解析策略意图...',
      'generate': '正在生成配置和代码...',
      'validate': '正在验证配置...',
      'compile': '正在编译验证...',
      'backtest': '正在运行回测...',
      'complete': '策略生成完成！',
      'failed': '策略生成失败'
    }
    statusMessage.value = messageMap[status || ''] || ''
  }
)
</script>

<style scoped>
.pipeline-dashboard {
  padding: 16px;
  max-width: 1200px;
  margin: 0 auto;
}

.dashboard-card {
  margin-bottom: 16px;
}

.history-card {
  margin-top: 16px;
}

pre {
  white-space: pre-wrap;
  word-wrap: break-word;
  font-size: 12px;
  max-height: 300px;
  overflow-y: auto;
}
</style>
