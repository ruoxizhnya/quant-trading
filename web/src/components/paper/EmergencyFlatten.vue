<template>
  <n-card
    :bordered="false"
    class="emergency-card"
    :class="{ 'is-armed': armed }"
  >
    <div class="emergency-header">
      <div class="title-block">
        <n-icon size="20" color="#f56c6c">
          <WarningOutline />
        </n-icon>
        <span class="title">紧急平仓 (Kill Switch)</span>
        <n-tag :type="armed ? 'error' : 'default'" size="small">
          {{ armed ? '已上膛 — 输入 token 解锁' : '安全' }}
        </n-tag>
      </div>
    </div>

    <p class="description">
      一键以市价清空所有持仓。T+1 限制将被绕过, 审计 trail
      记录本次操作的 reason + T+1 bypass 状态。
      <strong>不可撤销</strong>。
    </p>

    <n-collapse-transition :show="!armed">
      <n-button
        type="error"
        block
        ghost
        :loading="loading"
        @click="arm"
      >
        <template #icon>
          <n-icon><WarningOutline /></n-icon>
        </template>
        紧急平仓 (Arm)
      </n-button>
    </n-collapse-transition>

    <n-collapse-transition :show="armed">
      <n-space vertical>
        <n-form-item label="原因 (必填, 审计记录)">
          <n-input
            v-model:value="reason"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="如: 系统检测到异常行情, 立即清仓止损"
          />
        </n-form-item>
        <n-form-item label="Token (从服务器配置读取)">
          <n-input
            v-model:value="token"
            type="password"
            placeholder="从 trading.emergency_token 配置"
            show-password-on="click"
          />
        </n-form-item>
        <n-space>
          <n-button @click="disarm" :disabled="loading">取消</n-button>
          <n-button
            type="error"
            :loading="loading"
            :disabled="!canSubmit"
            @click="confirm"
          >
            <template #icon>
              <n-icon><WarningOutline /></n-icon>
            </template>
            确认紧急平仓
          </n-button>
        </n-space>
      </n-space>
    </n-collapse-transition>

    <n-divider v-if="lastResult" />

    <n-card
      v-if="lastResult"
      :title="`最近一次结果: ${lastResult.sold.length} 笔成交, ${lastResult.skipped.length} 笔跳过`"
      size="small"
      class="result-card"
    >
      <n-descriptions :column="2" size="small" bordered>
        <n-descriptions-item label="净成交总额">
          ¥{{ formatNumber(lastResult.sold_total) }}
        </n-descriptions-item>
        <n-descriptions-item label="延迟">
          {{ lastResult.latency_ms }}ms
        </n-descriptions-item>
        <n-descriptions-item label="原因">
          {{ lastResult.reason }}
        </n-descriptions-item>
        <n-descriptions-item label="开始时间">
          {{ formatTime(lastResult.started_at) }}
        </n-descriptions-item>
      </n-descriptions>

      <n-data-table
        v-if="lastResult.sold.length"
        :columns="soldColumns"
        :data="lastResult.sold"
        :pagination="false"
        size="small"
        :row-key="(r: EmergencyFlattenOrder) => r.order_id"
        class="sold-table"
      />

      <n-alert
        v-if="lastResult.skipped.length"
        type="warning"
        title="以下持仓未能成交, 需要手动处理"
        class="skipped-alert"
      >
        <ul>
          <li v-for="(s, i) in lastResult.skipped" :key="i">
            <code>{{ s.symbol }}</code>: {{ s.quantity }} 股 — {{ s.reason }}
          </li>
        </ul>
      </n-alert>
    </n-card>
  </n-card>
</template>

<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { markRaw } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import {
  NCard,
  NButton,
  NIcon,
  NSpace,
  NFormItem,
  NInput,
  NTag,
  NDivider,
  NDescriptions,
  NDescriptionsItem,
  NDataTable,
  NAlert,
  NCollapseTransition,
  useMessage,
} from 'naive-ui'
import { WarningOutline } from '@vicons/ionicons5'
import {
  emergencyFlatten,
  type EmergencyFlattenOrder,
  type EmergencyFlattenResult,
} from '@/api/paper-trading'

const message = useMessage()
const armed = ref(false)
const reason = ref('')
const token = ref('')
const loading = ref(false)
const lastResult = ref<EmergencyFlattenResult | null>(null)

const canSubmit = computed(() => reason.value.trim() !== '' && token.value !== '')

function arm() {
  armed.value = true
  reason.value = ''
}

function disarm() {
  armed.value = false
  reason.value = ''
  token.value = ''
}

async function confirm() {
  if (!canSubmit.value) return

  const ok = window.confirm(
    `[!] 紧急平仓确认\n\n` +
      `原因: ${reason.value}\n` +
      `持仓将全部以市价清空, T+1 限制被绕过, 操作不可撤销。\n\n` +
      `确定继续?`,
  )
  if (!ok) return

  loading.value = true
  try {
    lastResult.value = await emergencyFlatten(token.value, reason.value.trim())
    message.success(
      `紧急平仓完成: ${lastResult.value.sold.length} 笔成交, ` +
        `净收 ¥${formatNumber(lastResult.value.sold_total)}`,
    )
    // Auto-disarm after success; the operator can re-arm if they
    // need to fire another flatten in a different scenario.
    armed.value = false
    reason.value = ''
  } catch (e: unknown) {
    const detail = e instanceof Error ? e.message : String(e)
    message.error(`紧急平仓失败: ${detail}`)
  } finally {
    loading.value = false
  }
}

function formatNumber(n: number): string {
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatTime(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  return d.toLocaleString('zh-CN', { hour12: false })
}

const soldColumns: DataTableColumns<EmergencyFlattenOrder> = [
  {
    title: '标的',
    key: 'symbol',
    width: 110,
    render: (row) => h('code', null, row.symbol),
  },
  {
    title: '数量',
    key: 'quantity',
    width: 90,
    render: (row) => row.quantity.toFixed(0),
  },
  {
    title: '成交价',
    key: 'fill_price',
    width: 100,
    render: (row) => `¥${row.fill_price.toFixed(4)}`,
  },
  {
    title: '净收',
    key: 'net_proceeds',
    width: 110,
    render: (row) => `¥${formatNumber(row.net_proceeds)}`,
  },
  {
    title: 'T+1 绕过',
    key: 'bypassed_t1',
    width: 100,
    render: (row) =>
      h(
        NTag,
        {
          type: row.bypassed_t1 ? 'warning' : 'success',
          size: 'small',
          bordered: false,
        },
        { default: () => (row.bypassed_t1 ? '是' : '否') },
      ),
  },
]
</script>

<style scoped>
.emergency-card {
  border: 1px solid var(--q-border, #2a2a2a);
  border-left: 3px solid #f56c6c;
}

.emergency-card.is-armed {
  border-left-color: #f56c6c;
  background: rgba(245, 108, 108, 0.04);
}

.emergency-header {
  display: flex;
  align-items: center;
  margin-bottom: 8px;
}

.title-block {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title {
  font-size: 15px;
  font-weight: 600;
  color: var(--q-text, inherit);
}

.description {
  font-size: 12px;
  color: var(--q-text-muted, #888);
  line-height: 1.5;
  margin: 8px 0 12px 0;
}

.description strong {
  color: #f56c6c;
}

.result-card {
  margin-top: 12px;
}

.sold-table {
  margin-top: 8px;
}

.skipped-alert {
  margin-top: 8px;
}

.skipped-alert ul {
  margin: 4px 0 0 0;
  padding-left: 18px;
}
</style>
