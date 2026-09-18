<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDivider,
  NEmpty,
  NH1,
  NInput,
  NInputNumber,
  NList,
  NListItem,
  NScrollbar,
  NSpace,
  NStatistic,
  NTag,
  NText,
} from 'naive-ui'
import {
  getExploreStatus,
  startExplore,
  stopExplore,
  dimensionLabel,
  type AttemptView,
  type Challenge,
  type ExploreStatus,
} from '@/api/explore'

// P2-9 裁决的展示口径。
//
// 概率不是「通过率」：综合概率取已评估维度的**最小值**，所以一眼看去最该
// 关注的永远是 Weakest 那一维，而不是那个最好看的数字。
type SeverityType = 'error' | 'warning' | 'default' | 'success'

function probType(p: number): SeverityType {
  if (p < 0.2) return 'error'
  if (p < 0.5) return 'warning'
  return 'success'
}

function challengeType(severity: string): SeverityType {
  if (severity === 'blocking') return 'error'
  if (severity === 'warning') return 'warning'
  return 'default'
}

// blocking 排最前 —— 它是「这条足以否掉结论」的那一级，不能被 note 埋掉。
const severityRank: Record<string, number> = { blocking: 0, warning: 1, note: 2 }

// P1-3 观察页：正在试什么 / 结果流 / 当前最优。
//
// 三栏不是排版偏好，是「人在环」的最低配置：看不见正在试什么就没法判断
// 方向对不对，看不见结果流就不知道试了多少次才撞出来，看不见当前最优
// 就没法决定该继续还是该叫停。

const description = ref('做一个动量策略')
const maxTries = ref(20)
const runID = ref('')
const status = ref<ExploreStatus | null>(null)
const busy = ref(false)
const error = ref('')

let timer: number | undefined

const attempts = computed<AttemptView[]>(() => status.value?.attempts ?? [])
const latest = computed<AttemptView | null>(() =>
  attempts.value.length ? attempts.value[attempts.value.length - 1] : null,
)
const succeeded = computed(() => attempts.value.filter((a) => a.ok))
const failedCount = computed(() => attempts.value.length - succeeded.value.length)
const running = computed(() => status.value?.running ?? false)
const best = computed<AttemptView | null>(() => {
  const seq = status.value?.best_seq
  if (seq == null) return null
  return attempts.value.find((a) => a.seq === seq) ?? null
})

// 裁决详情看哪一次：默认跟最新，点结果流里任一行可切过去。
const selectedSeq = ref<number | null>(null)
const selected = computed<AttemptView | null>(() => {
  if (selectedSeq.value !== null) {
    const hit = attempts.value.find((a) => a.seq === selectedSeq.value)
    if (hit) return hit
  }
  return latest.value
})
const verdict = computed(() => selected.value?.verdict ?? null)
const sortedChallenges = computed<Challenge[]>(() =>
  [...(verdict.value?.challenges ?? [])].sort(
    (a, b) => (severityRank[a.severity] ?? 9) - (severityRank[b.severity] ?? 9),
  ),
)

function stopTimer() {
  if (timer !== undefined) {
    clearInterval(timer)
    timer = undefined
  }
}

// poll 返回新的状态而不只是写进 status —— 靠副作用推断的话，TS 会认为
// status.value 还是 null，读它的字段就成了 never。
function poll(): Promise<ExploreStatus> {
  return getExploreStatus(runID.value).then((s) => {
    status.value = s
    if (!s.running) stopTimer()
    return s
  })
}

async function run() {
  error.value = ''
  busy.value = true
  status.value = null
  selectedSeq.value = null
  try {
    const res = await startExplore({ description: description.value, max_tries: maxTries.value })
    runID.value = res.run_id
    const s = await poll()
    if (s.running) {
      stopTimer()
      timer = window.setInterval(() => {
        poll().catch((e: unknown) => {
          error.value = e instanceof Error ? e.message : String(e)
          stopTimer()
        })
      }, 1000)
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function stop() {
  if (!runID.value) return
  try {
    await stopExplore(runID.value)
    await poll()
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function fmtParams(p: Record<string, unknown>): string {
  return Object.entries(p)
    .map(([k, v]) => `${k}=${v}`)
    .join('  ')
}

onUnmounted(stopTimer)
</script>

<template>
  <div class="explore-page">
    <NH1>探索观察台</NH1>

    <NCard size="small" class="controls">
      <NSpace align="center" :wrap="true">
        <NInput v-model:value="description" placeholder="想试的方向，例如：做一个动量策略" style="width: 320px" />
        <NText depth="3">最多试</NText>
        <NInputNumber v-model:value="maxTries" :min="1" :max="500" style="width: 120px" />
        <NText depth="3">次</NText>
        <NButton type="primary" :loading="busy" :disabled="running" @click="run">开始探索</NButton>
        <NButton :disabled="!running" @click="stop">叫停</NButton>
        <NTag v-if="running" type="info">进行中 · 已试 {{ attempts.length }}</NTag>
        <NTag v-else-if="status && status.stopped === 'cancelled'" type="warning">已叫停</NTag>
        <NTag v-else-if="status" type="success">已完成</NTag>
      </NSpace>
    </NCard>

    <NAlert v-if="error" type="error" :title="error" style="margin-bottom: 16px" />

    <div class="grid">
      <!-- 左：正在试什么 -->
      <NCard title="正在试什么" size="small">
        <NEmpty v-if="!latest" description="还没有开始" />
        <template v-else>
          <NText strong>第 {{ latest.seq + 1 }} 次</NText>
          <NDivider style="margin: 8px 0" />
          <NText depth="2">{{ latest.hypothesis }}</NText>
          <NDivider style="margin: 8px 0" />
          <NText depth="3" style="font-size: 12px">{{ fmtParams(latest.params) }}</NText>
        </template>
      </NCard>

      <!-- 中：结果流 -->
      <NCard title="结果流" size="small">
        <NEmpty v-if="!attempts.length" description="结果会按尝试顺序出现" />
        <NScrollbar v-else style="max-height: 460px">
          <NList size="small" :bordered="false" clickable hoverable>
            <NListItem v-for="a in attempts" :key="a.seq" @click="selectedSeq = a.seq">
              <NSpace align="center" :wrap="false" justify="space-between">
                <NSpace align="center" :size="8">
                  <NText depth="3" style="width: 44px">#{{ a.seq }}</NText>
                  <NTag :type="a.ok ? 'success' : 'error'" size="small">
                    {{ a.ok ? a.sharpe.toFixed(2) : '失败' }}
                  </NTag>
                  <!-- 裁决概率：Sharpe 是「跑出来多少」，概率是「这个数有多少可信」 -->
                  <NTag
                    v-if="a.verdict"
                    :type="probType(a.verdict.probability)"
                    size="small"
                    :bordered="false"
                  >
                    P {{ a.verdict.probability.toFixed(2) }}
                  </NTag>
                  <NText depth="2" style="font-size: 12px">{{ a.hypothesis }}</NText>
                </NSpace>
                <NText depth="3" style="font-size: 12px">{{ fmtParams(a.params) }}</NText>
              </NSpace>
            </NListItem>
          </NList>
        </NScrollbar>
      </NCard>

      <!-- 右：当前最优 -->
      <NCard title="当前最优" size="small">
        <NEmpty v-if="!best" description="还没有成功的一次" />
        <template v-else>
          <NStatistic label="Sharpe" :value="best.sharpe" :precision="3" />
          <NDivider style="margin: 12px 0" />
          <NText depth="3" style="font-size: 12px">第 {{ best.seq }} 次 · {{ fmtParams(best.params) }}</NText>
          <NDivider style="margin: 12px 0" />
          <NSpace :vertical="true" :size="4">
            <NText depth="3" style="font-size: 12px">总共试了 {{ attempts.length }} 次</NText>
            <NText depth="3" style="font-size: 12px">失败 {{ failedCount }} 次</NText>
          </NSpace>
          <NDivider style="margin: 12px 0" />
          <NText depth="3" style="font-size: 12px">
            试 {{ attempts.length }} 次里撞出这个数 —— 次数越多，同样的结果越可疑。
          </NText>
          <template v-if="best.verdict">
            <NDivider style="margin: 12px 0" />
            <NStatistic label="裁决概率" :value="best.verdict.probability" :precision="3" />
          </template>
        </template>
      </NCard>
    </div>

    <!-- 下：验证器裁决 -->
    <NCard title="验证器裁决" size="small" class="verdict-card">
      <template #header-extra>
        <NText depth="3" style="font-size: 12px">
          第 {{ selected?.seq ?? 0 }} 次 · 点结果流里任一行可切换
        </NText>
      </template>

      <NEmpty v-if="!verdict" description="这一条没有裁决 —— 失败的尝试没有可被证伪的东西" />

      <template v-else>
        <NSpace align="center" :size="24">
          <NStatistic label="综合概率" :value="verdict.probability" :precision="3" />
          <NStatistic label="质疑条数" :value="verdict.challenges.length" />
          <NSpace :vertical="true" :size="4">
            <NText depth="3" style="font-size: 12px">
              综合概率取各维最小值 —— 结论受限于最弱的那一环
            </NText>
            <NSpace :size="8">
              <NText depth="3" style="font-size: 12px">最弱维：</NText>
              <NTag
                v-if="verdict.weakest"
                :type="probType(verdict.dimensions[verdict.weakest] ?? 0)"
                size="small"
              >
                {{ dimensionLabel(verdict.weakest) }}
              </NTag>
              <NTag v-if="verdict.blocking > 0" type="error" size="small">
                {{ verdict.blocking }} 条 blocking
              </NTag>
            </NSpace>
          </NSpace>
        </NSpace>

        <NDivider style="margin: 12px 0" />

        <NSpace :size="8" :wrap="true">
          <NTag
            v-for="[dim, p] in Object.entries(verdict.dimensions)"
            :key="dim"
            :type="probType(p)"
            size="small"
          >
            {{ dimensionLabel(dim) }} {{ p.toFixed(2) }}
          </NTag>
          <!-- 「没查」必须显示成没查，不能省掉 —— 省掉就成了「没问题」 -->
          <NTag
            v-for="dim in verdict.unassessed ?? []"
            :key="dim"
            size="small"
            :bordered="false"
            type="default"
          >
            {{ dimensionLabel(dim) }} 未评估
          </NTag>
        </NSpace>

        <!-- 因果维：机制是人要读的，但旁边必须写着它赌中了几条 ——
             只讲机制不下注的故事，等于没讲。 -->
        <template v-if="verdict.causal">
          <NDivider style="margin: 12px 0" />
          <NSpace :vertical="true" :size="4">
            <NSpace :size="8">
              <NText depth="3" style="font-size: 12px">机制（模型给的）</NText>
              <NTag :type="probType(verdict.causal.probability)" size="small">
                可证伪预测 {{ verdict.causal.testable }} 条 · 应验
                {{ verdict.causal.passed }} · 被证伪 {{ verdict.causal.failed }}
              </NTag>
            </NSpace>
            <NText depth="2" style="font-size: 13px">{{ verdict.causal.mechanism }}</NText>
          </NSpace>
        </template>

        <NDivider style="margin: 12px 0" />

        <NAlert
          v-for="(c, i) in sortedChallenges"
          :key="i"
          :type="challengeType(c.severity)"
          :title="dimensionLabel(c.dimension)"
          style="margin-bottom: 8px"
        >
          {{ c.message }}
        </NAlert>
      </template>
    </NCard>
  </div>
</template>

<style scoped>
.explore-page {
  padding: 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.controls {
  margin-bottom: 16px;
}

.grid {
  display: grid;
  grid-template-columns: 280px 1fr 280px;
  gap: 16px;
  align-items: start;
}

.verdict-card {
  margin-top: 16px;
}

@media (max-width: 1100px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
