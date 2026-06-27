<template>
  <div class="builder-page">
    <n-card :bordered="false" class="header-card">
      <n-space justify="space-between" align="center">
        <div>
          <h2>可视化策略编辑器</h2>
          <p class="header-sub">
            从左侧因子库拖拽因子到画布，配置权重与方向，组合多因子选股策略。权重总和需等于 100%。
          </p>
        </div>
        <n-tag :type="weightValid ? 'success' : 'warning'" size="large">
          权重合计 {{ weightTotal.toFixed(0) }}%
        </n-tag>
      </n-space>
    </n-card>

    <n-grid :cols="24" :x-gap="16" :y-gap="16" responsive="screen" item-responsive>
      <!-- Left: factor library -->
      <n-gi span="24 l:6">
        <n-card title="因子库" size="small" class="panel-card library-card">
          <n-input
            v-model:value="search"
            placeholder="搜索因子…"
            clearable
            size="small"
            style="margin-bottom: 12px"
          />
          <div v-for="cat in categories" :key="cat.key" class="factor-group">
            <div class="group-title">{{ cat.label }}</div>
            <div
              v-for="f in filteredFactorsByCategory(cat.key)"
              :key="f.id"
              class="factor-card"
              draggable="true"
              @dragstart="onDragStart($event, f)"
              @dragend="onDragEnd"
            >
              <div class="factor-name">{{ f.name }}</div>
              <div class="factor-id">{{ f.id }}</div>
              <n-tag size="tiny" :bordered="false" type="info">{{ f.direction_hint }}</n-tag>
            </div>
            <n-empty
              v-if="filteredFactorsByCategory(cat.key).length === 0"
              description="无匹配因子"
              size="small"
              style="padding: 8px 0"
            />
          </div>
        </n-card>
      </n-gi>

      <!-- Middle: canvas -->
      <n-gi span="24 l:12">
        <n-card title="策略画布" size="small" class="panel-card canvas-card">
          <template #header-extra>
            <n-button size="tiny" quaternary :disabled="canvasFactors.length === 0" @click="clearCanvas">
              清空
            </n-button>
          </template>
          <div
            class="canvas-dropzone"
            :class="{ 'is-drag-over': dragOver }"
            @dragover.prevent="dragOver = true"
            @dragleave.prevent="dragOver = false"
            @drop.prevent="onDrop"
          >
            <n-empty
              v-if="canvasFactors.length === 0"
              description="将左侧因子卡片拖拽到此处"
              class="canvas-empty"
            />
            <div v-else class="canvas-list">
              <div
                v-for="(item, idx) in canvasFactors"
                :key="item.instanceId"
                class="canvas-item"
              >
                <div class="canvas-item-head">
                  <div class="canvas-item-name">
                    <n-tag :type="item.direction === 'long' ? 'success' : 'error'" size="tiny" :bordered="false">
                      {{ item.direction === 'long' ? '多' : '空' }}
                    </n-tag>
                    <span>{{ item.name }}</span>
                    <n-text depth="3" style="font-size: 11px">{{ item.id }}</n-text>
                  </div>
                  <n-space :size="4">
                    <n-button size="tiny" quaternary @click="toggleDirection(idx)">
                      切换方向
                    </n-button>
                    <n-button size="tiny" quaternary type="error" @click="removeFactor(idx)">
                      删除
                    </n-button>
                  </n-space>
                </div>
                <div class="canvas-item-weight">
                  <n-slider
                    :value="item.weight"
                    :min="0"
                    :max="100"
                    :step="1"
                    :tooltip="true"
                    @update:value="(v) => updateWeight(idx, v)"
                  />
                  <n-input-number
                    :value="item.weight"
                    :min="0"
                    :max="100"
                    :step="1"
                    size="small"
                    style="width: 90px; margin-left: 12px"
                    @update:value="(v) => updateWeight(idx, v ?? 0)"
                  >
                    <template #suffix>%</template>
                  </n-input-number>
                </div>
              </div>
            </div>
          </div>

          <div class="weight-summary" :class="{ valid: weightValid, invalid: !weightValid }">
            <span>权重总和</span>
            <span class="weight-value">{{ weightTotal.toFixed(0) }}%</span>
            <span v-if="!weightValid" class="weight-hint">（需等于 100%）</span>
          </div>
        </n-card>
      </n-gi>

      <!-- Right: config -->
      <n-gi span="24 l:6">
        <n-card title="策略配置" size="small" class="panel-card config-card">
          <n-form :model="config" label-placement="top" size="small">
            <n-form-item label="策略名称" path="name">
              <n-input v-model:value="config.name" placeholder="如：动量+价值混合" />
            </n-form-item>
            <n-form-item label="选股数量" path="stock_count">
              <n-select v-model:value="config.stock_count" :options="stockCountOptions" />
            </n-form-item>
            <n-form-item label="调仓频率" path="rebalance_freq">
              <n-select v-model:value="config.rebalance_freq" :options="rebalanceOptions" />
            </n-form-item>
          </n-form>

          <n-space vertical :size="8" style="margin-top: 8px">
            <n-button block @click="previewConfig" :disabled="canvasFactors.length === 0">
              预览配置
            </n-button>
            <n-button
              block
              type="primary"
              :loading="saving"
              :disabled="!canSave"
              @click="handleSave"
            >
              保存策略
            </n-button>
            <n-button
              block
              type="info"
              ghost
              :disabled="!canSave"
              @click="handleBacktest"
            >
              跳转回测
            </n-button>
          </n-space>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Preview modal -->
    <n-modal v-model:show="showPreview" title="策略 JSON 配置" preset="card" style="width: 640px">
      <n-code :code="previewJson" language="json" word-wrap />
      <template #footer>
        <n-space justify="end">
          <n-button size="small" @click="copyPreview">复制</n-button>
          <n-button size="small" type="primary" @click="showPreview = false">关闭</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  NCard, NSpace, NGrid, NGi, NButton, NInput, NInputNumber, NSelect,
  NSlider, NTag, NText, NEmpty, NModal, NCode, NForm, NFormItem, useMessage,
} from 'naive-ui'
import { saveStrategy } from '@/api/strategy'

const router = useRouter()
const message = useMessage()

// ── Factor library ────────────────────────────────────────────────
// The factor catalogue is the fixed palette the operator drags from.
// `direction_hint` is purely informational — the real direction is
// chosen per-instance on the canvas, so a factor can be used long or
// short regardless of its conventional signal direction.
interface FactorDef {
  id: string
  name: string
  category: 'momentum' | 'value' | 'quality' | 'volume'
  direction_hint: string
}

const FACTOR_LIBRARY: FactorDef[] = [
  // 动量因子
  { id: 'momentum_5d', name: '5日动量', category: 'momentum', direction_hint: '正向' },
  { id: 'momentum_20d', name: '20日动量', category: 'momentum', direction_hint: '正向' },
  { id: 'momentum_60d', name: '60日动量', category: 'momentum', direction_hint: '正向' },
  // 价值因子
  { id: 'pe_ratio', name: '市盈率', category: 'value', direction_hint: '反向' },
  { id: 'pb_ratio', name: '市净率', category: 'value', direction_hint: '反向' },
  { id: 'dividend_yield', name: '股息率', category: 'value', direction_hint: '正向' },
  // 质量因子
  { id: 'roe', name: '净资产收益率', category: 'quality', direction_hint: '正向' },
  { id: 'roa', name: '总资产收益率', category: 'quality', direction_hint: '正向' },
  { id: 'debt_ratio', name: '资产负债率', category: 'quality', direction_hint: '反向' },
  // 成交量因子
  { id: 'volume_ratio', name: '量比', category: 'volume', direction_hint: '正向' },
  { id: 'turnover_rate', name: '换手率', category: 'volume', direction_hint: '反向' },
]

const categories = [
  { key: 'momentum' as const, label: '动量因子' },
  { key: 'value' as const, label: '价值因子' },
  { key: 'quality' as const, label: '质量因子' },
  { key: 'volume' as const, label: '成交量因子' },
]

const search = ref('')
function filteredFactorsByCategory(cat: FactorDef['category']): FactorDef[] {
  const q = search.value.trim().toLowerCase()
  return FACTOR_LIBRARY.filter(f => {
    if (f.category !== cat) return false
    if (!q) return true
    return f.name.toLowerCase().includes(q) || f.id.toLowerCase().includes(q)
  })
}

// ── Canvas (drop target) ──────────────────────────────────────────
// Each dropped factor becomes a canvas entry with its own weight and
// direction. We allow the same factor definition to be dropped more
// than once (e.g. one long + one short instance) so `instanceId`
// disambiguates them — `id` alone would collide as a v-for key.
interface CanvasFactor {
  instanceId: string
  id: string
  name: string
  weight: number
  direction: 'long' | 'short'
}

const canvasFactors = ref<CanvasFactor[]>([])
const dragOver = ref(false)
let dragCounter = 0

function onDragStart(e: DragEvent, f: FactorDef) {
  // HTML5 DnD: stash the factor id in dataTransfer so onDrop can read
  // it back. Text/plain is the most broadly supported payload type.
  e.dataTransfer?.setData('text/plain', f.id)
  e.dataTransfer!.effectAllowed = 'copy'
}

function onDragEnd() {
  dragOver.value = false
}

function onDrop(e: DragEvent) {
  dragOver.value = false
  const factorId = e.dataTransfer?.getData('text/plain')
  if (!factorId) return
  const def = FACTOR_LIBRARY.find(f => f.id === factorId)
  if (!def) return
  addFactor(def)
}

function addFactor(def: FactorDef) {
  // Default weight: split the remaining gap evenly so the total stays
  // close to 100%. If the canvas is empty we start at 100%; otherwise
  // we give the new factor a share of the unused budget and shrink it
  // to a sensible floor of 5% so the slider isn't a no-op.
  const remaining = 100 - weightTotal.value
  const weight = canvasFactors.value.length === 0 ? 100 : Math.max(5, Math.round(remaining / (canvasFactors.value.length + 1) * 2))
  canvasFactors.value.push({
    instanceId: `${def.id}-${Date.now()}-${dragCounter++}`,
    id: def.id,
    name: def.name,
    weight,
    direction: 'long',
  })
  message.info(`已添加因子：${def.name}`)
}

function updateWeight(idx: number, v: number) {
  const clamped = Math.max(0, Math.min(100, Math.round(v)))
  canvasFactors.value[idx].weight = clamped
}

function toggleDirection(idx: number) {
  const item = canvasFactors.value[idx]
  item.direction = item.direction === 'long' ? 'short' : 'long'
}

function removeFactor(idx: number) {
  const removed = canvasFactors.value.splice(idx, 1)[0]
  message.info(`已移除因子：${removed.name}`)
}

function clearCanvas() {
  canvasFactors.value = []
}

const weightTotal = computed(() =>
  canvasFactors.value.reduce((s, f) => s + f.weight, 0),
)
const weightValid = computed(() => weightTotal.value === 100)

// ── Config panel ──────────────────────────────────────────────────
const config = reactive({
  name: '',
  stock_count: 10,
  rebalance_freq: 'weekly' as 'daily' | 'weekly' | 'monthly',
})

const stockCountOptions = [
  { label: '5 只', value: 5 },
  { label: '10 只', value: 10 },
  { label: '20 只', value: 20 },
  { label: '50 只', value: 50 },
  { label: '100 只', value: 100 },
]

const rebalanceOptions = [
  { label: '日调仓', value: 'daily' },
  { label: '周调仓', value: 'weekly' },
  { label: '月调仓', value: 'monthly' },
]

const canSave = computed(() =>
  weightValid.value && canvasFactors.value.length > 0 && config.name.trim().length > 0,
)

function buildConfig() {
  return {
    name: config.name.trim(),
    factors: canvasFactors.value.map(f => ({
      id: f.id,
      weight: f.weight,
      direction: f.direction,
    })),
    stock_count: config.stock_count,
    rebalance_freq: config.rebalance_freq,
  }
}

// ── Preview ───────────────────────────────────────────────────────
const showPreview = ref(false)
const previewJson = ref('')

function previewConfig() {
  previewJson.value = JSON.stringify(buildConfig(), null, 2)
  showPreview.value = true
}

async function copyPreview() {
  try {
    await navigator.clipboard.writeText(previewJson.value)
    message.success('已复制到剪贴板')
  } catch {
    message.error('复制失败，请手动选择文本')
  }
}

// ── Save / backtest ───────────────────────────────────────────────
const saving = ref(false)

async function handleSave() {
  if (!canSave.value) return
  saving.value = true
  try {
    const res = await saveStrategy(buildConfig())
    message.success(`策略已保存：${res.name} (${res.id})`)
  } catch (e: unknown) {
    message.error('保存失败: ' + (e instanceof Error ? e.message : String(e)))
  } finally {
    saving.value = false
  }
}

function handleBacktest() {
  if (!canSave.value) return
  // Hand the assembled strategy off to the backtest engine via query
  // params. The engine page reads `strategy` + `name`; a full config
  // round-trip through the backend would be cleaner but the backtest
  // page currently only consumes the strategy name.
  router.push({
    name: 'backtest',
    query: {
      strategy: config.name.trim(),
    },
  })
}
</script>

<style scoped>
.builder-page {
  max-width: 1400px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.header-card { padding: 4px 0; }
.header-card h2 { margin: 0 0 4px 0; }
.header-sub { margin: 0; font-size: 12px; color: var(--q-text2); }

.panel-card { height: 100%; }

/* Library */
.library-card { max-height: 70vh; overflow-y: auto; }
.factor-group { margin-bottom: 16px; }
.group-title {
  font-size: 12px; font-weight: 600; color: var(--q-text2);
  text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;
}
.factor-card {
  display: flex; flex-direction: column; gap: 2px;
  padding: 8px 10px; margin-bottom: 6px;
  background: var(--q-card-bg2, rgba(255,255,255,0.03));
  border: 1px solid var(--q-divider, rgba(255,255,255,0.09));
  border-radius: 6px; cursor: grab; transition: border-color 0.15s;
}
.factor-card:hover { border-color: var(--q-primary, #58a6ff); }
.factor-card:active { cursor: grabbing; }
.factor-name { font-size: 13px; font-weight: 600; }
.factor-id { font-size: 11px; color: var(--q-text3); font-family: monospace; }

/* Canvas */
.canvas-card { display: flex; flex-direction: column; }
.canvas-dropzone {
  min-height: 320px; padding: 12px;
  border: 2px dashed var(--q-divider, rgba(255,255,255,0.12));
  border-radius: 8px; transition: border-color 0.15s, background 0.15s;
}
.canvas-dropzone.is-drag-over {
  border-color: var(--q-primary, #58a6ff);
  background: rgba(88, 166, 255, 0.06);
}
.canvas-empty { padding: 60px 0; }
.canvas-list { display: flex; flex-direction: column; gap: 12px; }
.canvas-item {
  padding: 12px;
  background: var(--q-card-bg2, rgba(255,255,255,0.03));
  border: 1px solid var(--q-divider, rgba(255,255,255,0.09));
  border-radius: 6px;
}
.canvas-item-head {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 8px;
}
.canvas-item-name {
  display: flex; align-items: center; gap: 8px;
  font-size: 13px; font-weight: 600;
}
.canvas-item-weight {
  display: flex; align-items: center;
}

.weight-summary {
  display: flex; align-items: center; gap: 8px;
  margin-top: 12px; padding: 8px 12px;
  border-radius: 6px; font-size: 13px;
}
.weight-summary.valid { background: rgba(133, 232, 157, 0.08); color: var(--q-success, #85e89d); }
.weight-summary.invalid { background: rgba(255, 171, 64, 0.08); color: var(--q-warning, #ffab70); }
.weight-value { font-size: 16px; font-weight: 700; }
.weight-hint { font-size: 11px; }

/* Config */
.config-card { height: 100%; }
</style>
