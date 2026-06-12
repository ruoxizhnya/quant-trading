<template>
  <div class="alerts-page">
    <n-page-header title="风险告警" subtitle="AlertManager 实时监控 + 历史回溯">
      <template #extra>
        <n-space>
          <n-tag v-if="stats?.enabled" type="success">告警已启用</n-tag>
          <n-tag v-else type="warning">告警已停用</n-tag>
          <n-button
            type="primary"
            :loading="forceChecking"
            @click="handleForceCheck"
            title="立即跑一次评估，无需等待下一 tick"
          >
            <template #icon>
              <n-icon><RefreshOutline /></n-icon>
            </template>
            强制评估
          </n-button>
        </n-space>
      </template>
    </n-page-header>

    <!-- Stats overview -->
    <n-grid :cols="4" :x-gap="12" :y-gap="12" class="stats-row">
      <n-gi>
        <n-card embedded>
          <n-statistic label="Channel 数量" :value="stats?.channel_count ?? 0" />
        </n-card>
      </n-gi>
      <n-gi>
        <n-card embedded>
          <n-statistic
            label="History (本次进程)"
            :value="stats?.history_len ?? 0"
            :precision="0"
          />
          <div class="card-sub">
            Capacity {{ stats?.history_limit ?? 0 }} · 驱逐 {{ stats?.recorder_evicted ?? 0 }}
          </div>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card embedded>
          <n-statistic
            label="Critical 告警"
            :value="criticalCount"
            :precision="0"
          />
        </n-card>
      </n-gi>
      <n-gi>
        <n-card embedded>
          <n-statistic
            label="Warning 告警"
            :value="warningCount"
            :precision="0"
          />
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Severity filter & limit -->
    <n-card class="filter-card">
      <n-space>
        <n-radio-group v-model:value="severityFilter" @update:value="refetch">
          <n-radio-button value="">全部</n-radio-button>
          <n-radio-button value="info">Info</n-radio-button>
          <n-radio-button value="warning">Warning</n-radio-button>
          <n-radio-button value="critical">Critical</n-radio-button>
        </n-radio-group>
        <n-input-number
          v-model:value="limit"
          :min="1"
          :max="500"
          :step="10"
          placeholder="显示条数"
          @update:value="refetch"
        />
        <n-button @click="refetch" :loading="loading" title="刷新">
          <template #icon>
            <n-icon><RefreshOutline /></n-icon>
          </template>
          刷新
        </n-button>
      </n-space>
    </n-card>

    <!-- Alert list -->
    <n-card title="告警历史" class="history-card">
      <n-empty v-if="!loading && alerts.length === 0" description="暂无告警" />
      <n-data-table
        v-else
        :columns="columns"
        :data="alerts"
        :loading="loading"
        :pagination="{ pageSize: 20 }"
        :row-key="rowKey"
        size="small"
      />
    </n-card>

    <!-- Rule breakdown -->
    <n-card v-if="Object.keys(ruleBreakdown).length > 0" title="按规则分布" class="breakdown-card">
      <div class="rule-grid">
        <div v-for="(count, rule) in ruleBreakdown" :key="rule" class="rule-pill">
          <span class="rule-name">{{ ruleLabel(rule) }}</span>
          <span class="rule-count">{{ count }}</span>
        </div>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref } from 'vue'
import { markRaw } from 'vue'
import type { DataTableColumns } from 'naive-ui'
import {
  NCard,
  NPageHeader,
  NSpace,
  NTag,
  NButton,
  NIcon,
  NGrid,
  NGi,
  NStatistic,
  NRadioGroup,
  NRadioButton,
  NInputNumber,
  NDataTable,
  NEmpty,
  useMessage,
} from 'naive-ui'
import { RefreshOutline } from '@vicons/ionicons5'
import {
  getAlertHistory,
  forceCheckAlerts,
  getAlertStats,
  type Alert,
  type AlertSeverity,
  type AlertStatsResponse,
} from '@/api/alerts'

const message = useMessage()
const alerts = ref<Alert[]>([])
const stats = ref<AlertStatsResponse | null>(null)
const loading = ref(false)
const forceChecking = ref(false)
const severityFilter = ref<'' | AlertSeverity>('')
const limit = ref(50)
let pollHandle: number | undefined

// ── Helpers ────────────────────────────────────────────────────────

const SEVERITY_TYPE: Record<AlertSeverity, 'info' | 'warning' | 'error'> = {
  info: 'info',
  warning: 'warning',
  critical: 'error',
}

const SEVERITY_LABEL: Record<AlertSeverity, string> = {
  info: '信息',
  warning: '警告',
  critical: '严重',
}

const RULE_LABELS: Record<string, string> = {
  position_concentration: '单标的集中度',
  sector_concentration: '单行业集中度',
  drawdown: '回撤告警',
  daily_loss_limit: '日内亏损',
  order_failure_rate: '订单失败率',
  risk_metric_breach: '风险指标越界',
}

function ruleLabel(rule: string): string {
  return RULE_LABELS[rule] || rule
}

function rowKey(row: Alert): string {
  return row.id
}

function formatTime(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  return d.toLocaleString('zh-CN', { hour12: false })
}

const criticalCount = computed(
  () => stats.value?.by_severity?.critical ?? 0,
)
const warningCount = computed(
  () => stats.value?.by_severity?.warning ?? 0,
)
const ruleBreakdown = computed(() => stats.value?.by_rule ?? {})

// ── Table columns ──────────────────────────────────────────────────

const columns: DataTableColumns<Alert> = [
  {
    title: '时间',
    key: 'timestamp',
    width: 170,
    render: (row) => formatTime(row.timestamp),
  },
  {
    title: '等级',
    key: 'severity',
    width: 80,
    render: (row) =>
      h(
        NTag,
        { type: SEVERITY_TYPE[row.severity], size: 'small', bordered: false },
        { default: () => SEVERITY_LABEL[row.severity] },
      ),
  },
  {
    title: '规则',
    key: 'rule',
    width: 160,
    render: (row) => ruleLabel(row.rule),
  },
  {
    title: '标的',
    key: 'symbol',
    width: 120,
    render: (row) => (row.symbol ? h('code', null, row.symbol) : '—'),
  },
  {
    title: '行业',
    key: 'sector',
    width: 100,
    render: (row) => row.sector || '—',
  },
  {
    title: '数值',
    key: 'value',
    width: 100,
    render: (row) =>
      row.value !== undefined && row.value !== null
        ? Number(row.value).toFixed(4)
        : '—',
  },
  {
    title: '阈值',
    key: 'threshold',
    width: 100,
    render: (row) =>
      row.threshold !== undefined && row.threshold !== null
        ? Number(row.threshold).toFixed(4)
        : '—',
  },
  {
    title: '说明',
    key: 'message',
    ellipsis: { tooltip: true },
  },
]

// ── Actions ────────────────────────────────────────────────────────

async function refetch() {
  loading.value = true
  try {
    const params: { limit: number; severity?: AlertSeverity } = { limit: limit.value }
    if (severityFilter.value) params.severity = severityFilter.value
    const [history, s] = await Promise.all([
      getAlertHistory(params),
      getAlertStats(),
    ])
    alerts.value = history.alerts
    stats.value = s
  } catch (e) {
    message.error(`加载告警失败: ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    loading.value = false
  }
}

async function handleForceCheck() {
  forceChecking.value = true
  try {
    const res = await forceCheckAlerts()
    message.success(`已强制评估，触发了 ${res.dispatched} 条告警`)
    await refetch()
  } catch (e) {
    message.error(`强制评估失败: ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    forceChecking.value = false
  }
}

// ── Lifecycle ──────────────────────────────────────────────────────

onMounted(() => {
  refetch()
  // 30s auto-refresh so the page shows new alerts without manual reload.
  // Cheap because the endpoint is in-memory; no DB hit.
  pollHandle = window.setInterval(refetch, 30_000)
})

onUnmounted(() => {
  if (pollHandle !== undefined) {
    window.clearInterval(pollHandle)
    pollHandle = undefined
  }
})
</script>

<style scoped>
.alerts-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px;
}

.stats-row {
  margin-top: 8px;
}

.card-sub {
  font-size: 12px;
  color: var(--q-text-muted, #888);
  margin-top: 4px;
}

.filter-card {
  padding: 12px;
}

.history-card,
.breakdown-card {
  /* allow table to overflow horizontally on narrow viewports */
  overflow-x: auto;
}

.rule-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}

.rule-pill {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--q-surface-muted, rgba(255, 255, 255, 0.04));
  border: 1px solid var(--q-border, #2a2a2a);
  border-radius: 6px;
}

.rule-name {
  font-size: 13px;
  color: var(--q-text, inherit);
}

.rule-count {
  font-size: 18px;
  font-weight: 600;
  color: var(--q-primary, #58a6ff);
  font-variant-numeric: tabular-nums;
}
</style>
