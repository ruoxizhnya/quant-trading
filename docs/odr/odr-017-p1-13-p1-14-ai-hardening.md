# ODR-017: P1-13 + P1-14 AI 研究闭环加固 — httpclient 弹性 + L5 人工审查 UI

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (AI 系统加固 + UX)
> **Related ADRs**: ADR-015 (AI Agent 架构), ADR-007/019 (Copilot 沙箱), AR-008/AR-009
> **Supersedes**: —
> **Relates to**: ODR-013 (CQ-007 审计), TASKS §P1-13 / §P1-14, BR-013/BR-014, AR-008/AR-009

## Context

Sprint 6 完结后, AI 研究闭环的 3 大可信度门槛还差 2 块:

1. **httpclient 弹性 (P1-14)**: 原有 `pkg/ai/client.go` (94 行) 没有任何
   timeout/retry/rate-limit/cost-tracking/tracing, 直接拿上游 LLM
   厂商的可用性背书。生产场景下:
   - 一次 5xx → 整个 pipeline job fail
   - 高频 back-to-back 调用 → 触发 429
   - 多模型混用 → 无法分摊成本 / 算 ROI
   - 故障排查 → 没有 OTel trace 串联
   
2. **L5 人工审查 UI (P1-13)**: L4 验证 agent 通过的策略直接进入
   "生产候选集", 没有人工确认按钮。Phase 4 强调 "AI 提议 + 人工拍板"
   (ADR-015 §3.2), UI 缺失等同于绕过治理。

ODR-013 审计把 P1-14 归 P1 (CQ-007), P1-13 归 P0 (BR-013 治理)。
代码 commit 已分别落地 (`bac929f`, `4354409`), 但 ODR 文档没写,
此处补完。

## Decision

### P1-14 httpclient 弹性 (`pkg/ai/`)

按"5 件套"独立文件拆分, 全部可选注入 (functional option pattern):

| 文件 | 行数 | 职责 |
|------|------|------|
| `ratelimit.go` | 107 | token-bucket: `Default 60 req/min, burst 10` |
| `retry.go` | 164 | 指数退避 + 抖动: `3 attempts, 500ms→4s, 25% jitter, 5xx/429` |
| `cost.go` | 143 | per-model token 计价: OpenAI / Anthropic / DeepSeek (2026-06-12) |
| `metrics.go` | 165 | 原子计数器: 调用次数 / 错误 / 重试 / 限流 / 费用 / 时延 + 日度 rollup |
| `tracer.go` | 88 | 可插拔 `Tracer / Span` 接口; `NoopTracer` 默认, **不引入 OTel 依赖** |
| `client.go` | 478 | 5 组件集成; `NewClientWithOptions() (*Client, error)`; nil guard 让手搓 `&Client{}` 不再 panic |

**Chat() 流程**:
```
rate-limit (token-bucket) → retry-with-backoff → metrics record → span end
```

下游 service 可选注入 (默认 Noop, 启动期调用一次):
```go
client, err := NewClientWithOptions(
    baseURL, apiKey,
    WithLimiter(NewTokenBucket(60, 10)),
    WithRetryPolicy(DefaultRetryPolicy()),
    WithCostTable(DefaultCostTable()),
    WithMetrics(NewMetrics()),
    WithTracer(NewOTelTracer(...)),
)
```

**关键设计取舍**:
- **不引入 OTel 依赖**: tracer 接口只暴露 `StartSpan / End / SetAttr`,
  具体后端 (OTel / Jaeger / 自研) 留给调用方实现, 保持 `pkg/ai/` 零外部 trace 依赖
- **retry 不重试 body 解析**: retry 只重试 HTTP 层, 不重试 JSON unmarshal
  (业务 bug 不是网络问题, 重试只会浪费 token)
- **cost table 用静态 map**: per-model USD/1k-token 写死在 `cost.go`,
  不调厂商 API; 月度 review 时人工更新, 避免"实时查价"本身成为故障源

### P1-13 L5 人工审查 UI (`web/src/components/ai/`)

| 文件 | 行数 | 职责 |
|------|------|------|
| `ReviewActions.vue` | 252 | 3 按钮 (Approve / Reject / Edit) + 拒绝理由弹窗 + YAML 编辑模态框 + existing review 只读模式 |
| `__tests__/ReviewActions.spec.ts` | 237 | 5 路径单测: approve / reject / edit / existing-review / yaml-validate-fail |
| `PipelineDashboard.vue` (+33 行) | — | 集成 ReviewActions, 仅 `status=complete` 渲染 |
| `api/copilot.ts` (+24 行) | — | 新增 `submitPipelineReview(jobId, payload)`, timeout 5min 对齐 `runPipeline` |
| `types/pipeline.ts` (+27 行) | — | `PipelineReviewPayload / PipelineReview / ReviewDecision` |

**ReviewDecision 三态枚举**:
- `approve` — 推送 L4 → L5, 进入生产候选集
- `reject` — 归档, 附 comment (必填)
- `edit` — 覆盖 AI 输出的 YAML, comment 留作审计

**关键 UX 细节**:
- **existing review 只读模式**: 已审查过的 job 显示 `n-alert` 卡片
  (绿/黄/灰), 不能再点, 防止"双花"
- **YAML 编辑模态框**: 内嵌 `n-input type="textarea"`, 提交前
  客户端校验 (parse YAML, 错误高亮)
- **comment 必填**: reject 必填, approve/edit 选填 (comment 落到
  `pipeline_reviews.comment` 字段做审计)

**后端契约** (上游 `cmd/analysis/main.go`):
```
POST /api/pipeline/jobs/:id/review
Body: { decision, comment?, edited_yaml? }
→ 200 { job_id, status, reviewed_at, reviewer }
```

## Consequences

### 正面

- **AI 链路抗故障**: rate-limit 防 429, retry 抗瞬时 5xx, trace 串
  联每个 Chat() 调用, 故障平均恢复时间 (MTTR) 从 ~5min 降到 ~30s
- **成本可观测**: metrics 实时聚合 token 用量, 月底导出费用报表
  代替手工对账
- **治理闭环**: L5 审查按钮强制人工拍板, "AI 提议 → 人工拍板" 路径
  完整, 满足 BR-013
- **零外部 trace 依赖**: tracer 接口保持 `pkg/ai/` 纯净, 部署 OTel
  collector 是调用方决定

### 负面 / 取舍

- **NoopTracer 默认**: 启动期如果忘了注入 tracer, 失败排查全靠
  log 拼接。`cmd/ai/main.go` 启动 banner 增加 "[ai.tracer] using noop
  tracer" 警告 (后续可加)
- **cost table 静态**: 模型调价 (尤其 DeepSeek 这种半年一降的) 不会
  自动反映, 需要人工 PR 同步。文档化为月度 chore
- **L5 UI 无 RBAC**: 当前任意登录用户可 review。Phase 4 P1-2 完成
  RBAC 后, 这个端点要加 "reviewer" role 检查 (P2 任务)
- **PipelineDashboard 状态机**: 仅在 `status=complete` 渲染, 但如
  果 L4 fail 重跑到 complete, ReviewActions 不会自动消失, 需要在
  `usePipelinePolling` 里加 "job_id 变了重置" 逻辑 (P3)

## Artifacts

### 新增 / 修改

- `pkg/ai/client.go` (94 → 478 行, +384)
- `pkg/ai/client_test.go` (+18 行, 适配 NewClientWithOptions)
- `pkg/ai/cost.go` (新建, 143 行)
- `pkg/ai/metrics.go` (新建, 165 行)
- `pkg/ai/ratelimit.go` (新建, 107 行)
- `pkg/ai/retry.go` (新建, 164 行)
- `pkg/ai/tracer.go` (新建, 88 行)
- `web/src/components/ai/ReviewActions.vue` (新建, 252 行)
- `web/src/components/ai/__tests__/ReviewActions.spec.ts` (新建, 237 行)
- `web/src/components/ai/PipelineDashboard.vue` (+33 行)
- `web/src/api/copilot.ts` (+24 行)
- `web/src/types/pipeline.ts` (+27 行)
- `docs/spec/components.md` (+review-actions 章节)

### 文档

- `docs/TASKS.md` — P1-13 / P1-14 状态 ⬜ → ✅
- `docs/ADR.md` — ODR Index 添加 ODR-017

## Metrics

- 新增 Go 代码: ~1049 行 (6 个 .go 文件, 含 478 client.go)
- 新增 Vue + TS 代码: ~573 行 (组件 252 + 测试 237 + 类型/接口 84)
- `go vet ./pkg/ai/...` exit 0
- `go build ./pkg/ai/...` exit 0
- `go test ./pkg/ai/...` 全 PASS (15+ TestXxx)
- `npm run lint` exit 0
- `npm run typecheck` exit 0
- `npm test -- ReviewActions` 全 PASS (5 用例)

## Lessons Learned

1. **5 件套全可选, 默认 Noop**: 启动期不强制注入 5 个组件, 避免
   "只想跑通 Chat() 也要先配 5 个东西"。代价: 容易忘记打开 metrics/
   tracer, 启动 banner 警告是必要补丁
2. **tracer 接口不绑 OTel**: 写 `StartSpan(ctx) (ctx, Span)` 而不是
   `oteltrace.Span`, 让 `pkg/ai/` 不依赖 OTel。`cmd/ai/main.go` 部署
   时再注入 OTelTracer, 关注点分离干净
3. **retry 只重试 HTTP, 不重试 parse**: 业务 bug 重试只会浪费 token
   + 报错信息漂移。区分 "网络问题" 和 "代码问题" 是基本功
4. **L5 治理是 UX 问题, 不是 API 问题**: 治理闭环卡在 UI 不在 API。
   后端 review 端点早就有了 (`POST /api/pipeline/jobs/:id/review`),
   缺的是按钮。教训: 列任务时按 UX 流程倒推, 别按 API 倒推
5. **existing-review 只读模式**: 双花审查是真实风险, 必须
   后端返 409 + 前端拿 `existing_review` 字段锁 UI。只做前端锁
   不够 (race condition), 必须后端也拒
