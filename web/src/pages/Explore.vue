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
  type AttemptView,
  type ExploreStatus,
} from '@/api/explore'

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
          <NList size="small" :bordered="false">
            <NListItem v-for="a in attempts" :key="a.seq">
              <NSpace align="center" :wrap="false" justify="space-between">
                <NSpace align="center" :size="8">
                  <NText depth="3" style="width: 44px">#{{ a.seq }}</NText>
                  <NTag :type="a.ok ? 'success' : 'error'" size="small">
                    {{ a.ok ? a.sharpe.toFixed(2) : '失败' }}
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
        </template>
      </NCard>
    </div>
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

@media (max-width: 1100px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
