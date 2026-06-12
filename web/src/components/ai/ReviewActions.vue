<template>
  <div class="review-actions">
    <!-- 已审查状态: 显示只读记录 -->
    <n-alert
      v-if="existingReview"
      :type="reviewAlertType"
      :title="reviewAlertTitle"
      :show-icon="true"
    >
      <template #header>
        {{ reviewAlertTitle }}
      </template>
      <n-space vertical size="small">
        <n-text v-if="existingReview.comment" depth="2">
          备注: {{ existingReview.comment }}
        </n-text>
        <n-text depth="3" style="font-size: 12px">
          审查时间: {{ formatReviewTime(existingReview.reviewed_at) }}
        </n-text>
      </n-space>
    </n-alert>

    <!-- 三按钮: 审查主操作 -->
    <n-space v-else>
      <n-button
        type="primary"
        :loading="submitting"
        :disabled="submitting"
        @click="onApprove"
      >
        <template #icon>
          <n-icon><CheckmarkCircleOutline /></n-icon>
        </template>
        通过
      </n-button>
      <n-button
        type="warning"
        :loading="submitting"
        :disabled="submitting"
        @click="onReject"
      >
        <template #icon>
          <n-icon><CloseCircleOutline /></n-icon>
        </template>
        拒绝
      </n-button>
      <n-button
        :loading="submitting"
        :disabled="submitting || !yamlConfig"
        @click="showEditModal = true"
      >
        <template #icon>
          <n-icon><CreateOutline /></n-icon>
        </template>
        编辑 YAML
      </n-button>
    </n-space>

    <!-- 评论行: 与三按钮并列, 共享 submitting 状态 -->
    <n-input
      v-if="!existingReview"
      v-model:value="comment"
      type="text"
      placeholder="审查备注 (可选)"
      :disabled="submitting"
      style="margin-top: 8px"
    />

    <!-- Edit 弹窗 -->
    <n-modal
      v-model:show="showEditModal"
      preset="card"
      title="编辑 YAML 配置"
      style="max-width: 720px"
      :mask-closable="!submitting"
      :close-on-esc="!submitting"
    >
      <n-space vertical>
        <n-text depth="3">
          提交后, 编辑后的 YAML 将替代 AI 输出, 推送到 L5 上线.
        </n-text>
        <n-input
          v-model:value="editedYaml"
          type="textarea"
          placeholder="在此编辑 YAML"
          :rows="14"
          :disabled="submitting"
          style="font-family: monospace"
        />
        <n-input
          v-model:value="comment"
          type="text"
          placeholder="编辑原因 (可选)"
          :disabled="submitting"
        />
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button :disabled="submitting" @click="showEditModal = false">
            取消
          </n-button>
          <n-button
            type="primary"
            :loading="submitting"
            :disabled="submitting || !editedYaml.trim()"
            @click="onEditSubmit"
          >
            提交编辑
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
// ReviewActions.vue — P1-13 (ODR-017)
//
// L5 人工审查 UI: 三按钮 (Approve / Reject / Edit) + 备注输入 + 状态展示.
// 父组件传入 jobId + 当前 yaml, 监听 reviewed 事件拿到更新后的 PipelineResult.
import { ref, computed, watch } from 'vue'
import {
  NButton,
  NIcon,
  NSpace,
  NInput,
  NModal,
  NAlert,
  NText,
  useMessage
} from 'naive-ui'
import {
  CheckmarkCircleOutline,
  CloseCircleOutline,
  CreateOutline
} from '@vicons/ionicons5'
import { submitPipelineReview } from '@/api/copilot'
import type {
  PipelineResult,
  PipelineReview,
  PipelineReviewPayload
} from '@/types/pipeline'

interface Props {
  jobId: string
  yamlConfig?: string
  review?: PipelineReview
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'reviewed', updated: PipelineResult): void
}>()

const message = useMessage()

const submitting = ref(false)
const comment = ref('')
const showEditModal = ref(false)
const editedYaml = ref(props.yamlConfig || '')

// 父组件传入的 review 状态: 已审查则只读, 未审查则显示按钮
const existingReview = computed(() => props.review)

// 当父组件传入新的 yamlConfig 时, 同步到编辑器 (但仅在 modal 未打开时)
watch(
  () => props.yamlConfig,
  (newYaml) => {
    if (!showEditModal.value) {
      editedYaml.value = newYaml || ''
    }
  }
)

const reviewAlertType = computed<'success' | 'error' | 'info'>(() => {
  if (!props.review) return 'info'
  if (props.review.decision === 'approve') return 'success'
  if (props.review.decision === 'reject') return 'error'
  return 'info'
})

const reviewAlertTitle = computed(() => {
  if (!props.review) return '已审查'
  switch (props.review.decision) {
    case 'approve': return '已通过'
    case 'reject':  return '已拒绝'
    case 'edit':    return '已编辑'
    default:        return '已审查'
  }
})

function formatReviewTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString('zh-CN', { hour12: false })
  } catch {
    return iso
  }
}

function buildPayload(decision: PipelineReviewPayload['decision'], extra: Partial<PipelineReviewPayload> = {}): PipelineReviewPayload {
  const payload: PipelineReviewPayload = { decision, ...extra }
  if (comment.value.trim()) {
    payload.comment = comment.value.trim()
  }
  return payload
}

async function submit(payload: PipelineReviewPayload) {
  if (submitting.value) return
  submitting.value = true
  try {
    const updated = await submitPipelineReview(props.jobId, payload)
    message.success(
      payload.decision === 'approve' ? '已通过'
        : payload.decision === 'reject' ? '已拒绝'
        : '编辑已提交'
    )
    // 清空本地输入, 但不重置 review 状态 (由父组件用 updated 替换)
    comment.value = ''
    showEditModal.value = false
    emit('reviewed', updated)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : '审查提交失败'
    message.error(msg)
  } finally {
    submitting.value = false
  }
}

function onApprove() {
  submit(buildPayload('approve'))
}

function onReject() {
  submit(buildPayload('reject'))
}

function onEditSubmit() {
  const yaml = editedYaml.value.trim()
  if (!yaml) {
    message.warning('编辑后的 YAML 不能为空')
    return
  }
  submit(buildPayload('edit', { edited_yaml: yaml }))
}
</script>

<style scoped>
.review-actions {
  width: 100%;
}
</style>
