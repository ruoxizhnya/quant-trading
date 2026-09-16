# ODR-062: 阶段 P5 收尾评估 — 旁路取数残留全量勘察（切片 B + 切片 D 合并审计）

> **Status**: Completed（方案甲已裁决并实施落地，2026-09-16）
> **Date**: 2026-09-16
> **Category**: Audit
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§3 单向依赖原则）
> **Supersedes**: —
> **Related ODRs**: [ODR-060](odr-060-p5-1-frontend-evidence-api.md)（本记录承接其「未做项」第 2 条 = 切片 B、第 4 条 = 切片 D）, [ODR-061](odr-061-p5-1-slice-c-citation-evaluation.md)（切片 C 已完成）, [ODR-059](odr-059-p5-1-retire-datasource-switch.md)（切片 2，其「未做项」第 4 条首次点名残留）, [ODR-058](odr-058-p5-1-retire-direct-providers.md)（切片 1）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-059](odr-059-p5-1-retire-datasource-switch.md) 把 `P5-1` 的关闭条件写死在其「未做项」第 4 条；[ODR-060](odr-060-p5-1-frontend-evidence-api.md)（切片 3，Vue SPA 对接 Evidence API）与 [ODR-061](odr-061-p5-1-slice-c-citation-evaluation.md)（切片 C，citation 元组）分别关闭了「Vue SPA」「Research Engine」两个半句。`P5-1` 至今未关闭的**唯一剩余维度**是「**去除旁路取数**」的落地残留，即 ODR-060「未做项」中的两条：

> 第 2 条（切片 B）— 「SPA 数据管理面对齐 L0 …… `web/src/api/sync.ts` 的 `/api/sync/status` / `/api/sync/import` / `/api/sync/stream` 在 `cmd/analysis` 与 `cmd/data` 均无注册 …… 修正属**跨前后端契约变更**，须独立评估。」
> 第 4 条（切片 D）— 「分层/旁路残留清理 —— `web/vite.config.ts` 的 `/market` → `8081`（L3 → L0 直连，绕过 analysis 代理）与 `cmd/analysis/handlers_proxy.go` 无 `/api` 前缀的遗留镜像路由。」

本记录即该独立评估。**本记录只做勘察与裁决点整理，不含实施。**

### 判据：什么算「旁路」，什么不算

[ADR-022 §3](../adr/adr-022-unified-research-platform.md) 的原则原文：

> 「L0 唯一数据面 + 单一写者 + **单向依赖（L3→L2→L1→L0）** + 可重建性标注 + 契约优先」

据此建立判定表：

| 调用方向 | 判定 | 依据 |
|---|---|---|
| L3（Vue SPA / 浏览器）→ L0（data :8081）**直连** | ❌ **旁路** | 跳过了 L2 编排面 / L1 计算面（analysis :8085 是 compose 注明的 API Gateway），违反单向依赖 |
| L3 → analysis(:8085) → 代理转发 → data(:8081) | ✅ 合法 facade | L3→L2→L0，方向正确 |
| Go 内部 L1/L2 服务（strategy / marketdata / tools / backtest）→ data(:8081) API | ✅ **合法 L0 消费** | 正是单向依赖的 L1→L0 边；「单一数据面」要求的是都走 L0 API，而非都走 analysis |
| 同一服务内部的裸路径 rewrite（analysis bare → analysis `/api`） | ⚠️ 非「旁路」，属**入口形状整洁问题** | 不跨层；但同一资源两个 URL 形状会造成契约漂移 |
| 任何绕过 L0 API 直接抓外部源 / 直连 PG 的行为 | ❌ 旁路 | 切片 1（ODR-058）/ 切片 2（ODR-059）已退役 |

### 现场取证

| # | 取证 | 方法 | 结论 |
|---|---|---|---|
| **a** | **SPA 的 4 个 sync 端点全仓无注册** | 读 `web/src/api/sync.ts`（3 端点）+ `web/src/stores/sync.ts:116`（SSE `/api/sync/stream`）；全仓 Grep `sync/status\|sync/import\|sync/stream` 于 `*.go` → **0 命中** | 前端数据管理面的核心链路（状态 / 导入 / 进度流）在两个后端上**均不存在**，调用必 404 |
| **b** | **data 侧真实契约是 `/api/sync/jobs*` 家族** | 读 `cmd/data/main.go:129-152`（`r.Group("/api/sync")`：jobs CRUD + `:id/progress` SSE + workers + schedules 全家族）；job 类型常量 `pkg/sync/types/types.go:36-43`（`stocks / ohlcv / ohlcv_all / fundamentals / calendar / dividends / splits`） | L0 已有完整异步任务契约；前端的 `/api/sync/status\|import\|stream` 是**对着一版从未实现的 SPEC 写的**（`docs/SPEC.md:1321-1334` 记载的是 jobs 家族，status/import 从未存在） |
| **c** | **e2e data-sync 三套件按 gateway 契约编写，当前必 404** | 读 `e2e/helpers/api.ts:3-11`（`apiRequest` baseURL = `BACKEND_URL` = analysis :8085）+ `e2e/tests/data-sync*.spec.ts` 全部经 `apiRequest` 请求 `/api/sync/jobs*`；而 analysis 无该路由（取证 a） | SPEC.md / AGENTS.md:79 / ARCHITECTURE.md:149 均按「analysis 持有 `/api/sync/jobs`」记载；实现却只在 data :8081。**文档、e2e、实现三方脱节**。另：`data-sync.spec.ts:24` 提交 `type: 'stock_list'`，而真实 job 类型是 `"stocks"`（取证 b）——即使路由通了，创建请求也会失败 |
| **d** | **analysis 死代理路由：3 条零消费者** | 逐条追踪 `cmd/analysis/handlers_proxy.go` 的 11 条路由：`/api/sync/calendar`(:83)、裸 `/sync/calendar`(:108)、`/api/v1/trading/calendar`(:113) —— `web/src`、`e2e`、`cmd/analysis/static`、Go 内部消费面 **全部 0 命中**（Go 侧 `pkg/marketdata/http_provider.go:139` 直连 data 的同名路由，不经 analysis） | 3 条代理路由是纯死代码 |
| **e** | **analysis 裸镜像路由：4 条有活消费者** | `cmd/analysis/main.go:243-284` 服务 legacy 静态页；`static/index.html:192`（`API='http://localhost:8085'` + `:363` 裸 `/ohlcv/`）、`static/screen.html:146,175`（裸 POST `/screen`）、`static/dashboard.html:318`（`:356` 裸 `/market/index`、`:389,415,420` 裸 `/stocks/count`） | 4 条裸镜像路由（`/ohlcv/:symbol`、`/screen`、`/stocks/count`、`/market/index`）被 **analysis 自带 legacy 页面**消费；且 compose（base + services）**均无 SPA 部署**——legacy 静态页是当前唯一的服务端 UI，不能视为死代码 |
| **f** | **e2e 不经 vite、不用裸路径** | 读 `e2e/playwright.config.ts:4-6`（baseURL=5173 但 API 测试走 `helpers/api.ts` 直连 :8085/:8081）；`e2e/tests/api-health.spec.ts:23,39` 的裸路径仅出现在**测试标题**，实际请求经 helpers 走 `/api/stocks/count`、`/api/market/index` | vite 的非 `/api` 代理在测试面也无消费者 |
| **g** | **vite 死代理 3 条** | 读 `web/vite.config.ts:14-31`（`/market`→8081、`/stocks`→8085、`/ohlcv`→8085）；`web/src` 全目录 Grep 裸路径 `/(market|ohlcv|stocks)[/"']` → 除 `api/*.ts` 的 `/api` 前缀命中外 **0 命中**（取证 f 排除 e2e） | 3 条 dev proxy 全部无消费者；其中 `/market`→8081 是**唯一一处 L3→L0 直连形态**（虽已无实际流量），`/stocks`、`/ohlcv`→8085 是同一服务的多余入口 |
| **h** | **SPA sync 面的活端点只有 2 个** | 读 `cmd/analysis/handlers_datasource.go:19-60`（`/api/datasource/status` + `/health` 本地实现，ODR-059 已把 switch 门退役并留注释） | `DataSourcePanel.vue` 正常工作；死的是 `SyncStatusPanel.vue`（fetchSyncStatus + SSE）与 `DataImportForm.vue`（importData）这两块。另 `api/sync.ts` 的 `getImportProgress` 在 store/组件层 **0 调用**（连前端内部都是死代码）；旁证：`e2e/tests/copilot-e2e.spec.ts:289` 已把 `/api/sync/stream` 不可达作为 skip 理由写死 |

## 残留清单（按维度）

### 维度 1 — SPA 数据管理面死端点（= 切片 B 正主）

| 前端调用 | 后端现实 | 消费组件 |
|---|---|---|
| `api/sync.ts` GET `/api/sync/status` | 无 | `SyncStatusPanel.vue` |
| `api/sync.ts` POST `/api/sync/import` | 无 | `DataImportForm.vue` |
| `api/sync.ts` GET `/api/sync/import/:jobId` | 无（前端内部也无调用方） | — |
| `stores/sync.ts` SSE `/api/sync/stream` | 无 | `SyncStatusPanel.vue`（connectSSE） |

### 维度 2 — analysis 代理路由残留（handlers_proxy.go）

| 路由 | 消费者 | 判定 |
|---|---|---|
| `/api/ohlcv/:symbol`、`/api/screen`、`/api/stocks/count`、`/api/market/index` | SPA（market.ts / backtest.ts）+ e2e helpers | ✅ 合法 facade，不动 |
| 裸 `/ohlcv/:symbol`、`/screen`、`/stocks/count`、`/market/index`（rewrite 到上表） | analysis legacy 静态页（取证 e） | ⚠️ 非旁路；入口形状问题，**与 legacy 页面共存亡** |
| `/api/sync/calendar`、裸 `/sync/calendar`、`/api/v1/trading/calendar` | 无（取证 d） | ❌ 死代码 |

### 维度 3 — vite dev proxy 残留

| 代理 | 消费者 | 判定 |
|---|---|---|
| `/api` → 8085 | 全部 SPA API | ✅ 不动 |
| `/market` → 8081 | 无（取证 g） | ❌ 死配置，且是 L3→L0 直连形态 |
| `/stocks` → 8085、`/ohlcv` → 8085 | 无 | ❌ 死配置 |

### 合法面（明确不动）

Go 内部 L1/L2 直连 L0 API：`pkg/strategy/utils.go:112`（`/screen`）、`pkg/marketdata/http_provider.go`（`/ohlcv`、`/api/v1/trading/calendar`、`/api/v1/ohlcv/bulk`）、`pkg/tools/builtin/datafetch.go`（`/ohlcv`）、backtest 缓存预热（`/api/v1/cache/warm`）。全仓 Grep 确认 `pkg/`、`cmd/` 在 data 目录之外**无任何 tushare 直连残留**（仅类型定义与注释）。analysis 本地 `/api/datasource/*` 为只读观测，不动。

## 切片候选与裁决点

| 候选 | 内容 | 风险面 | 说明 |
|---|---|---|---|
| **S-A. SPA 数据管理面对齐 L0**（前端） | 重写 `api/sync.ts` + `stores/sync.ts` + `types/sync.ts`：导入 → `POST /api/sync/jobs`（type 映射到 7 个真实 job 类型）；状态 → `GET /api/sync/jobs?...` 派生；SSE `/api/sync/stream` → `/api/sync/jobs/:id/progress`（按 job 订阅）或降级为轮询；删 `getImportProgress`；UI 三组件语义适配 | 中 | 纯前端契约变更，后端零改动；「全局队列状态」语义（is_running / queue_length）在 jobs 列表契约下需重新表达（无对应全局端点，`GET /api/sync/workers` 可给 worker 侧视角） |
| **S-B. analysis 补 `/api/sync/*` 网关代理**（后端） | `handlers_proxy.go` 增前缀代理 `/api/sync/*` → data `/api/sync/*`；**SSE 须流式透传**（现有 `proxyRequest` 是整体 JSON decode-and-replay，不适用；需 `httputil.ReverseProxy` + `FlushInterval<0` 或手写 flush 转发） | 中高 | 收益：SPEC/AGENTS/ARCHITECTURE 的 gateway 契约成真；e2e data-sync 三套件复活（但仍需修 `stock_list`→`stocks` 的类型漂移，属测试修正）；与 S-A 互补——没有 S-B，SPA 无法经 facade 用 jobs 家族 |
| **S-C. analysis 死代理路由退役** | 删 `/api/sync/calendar`、裸 `/sync/calendar`、`/api/v1/trading/calendar` 三条零消费路由 | 低 | 唯一保留意见：`/sync/calendar` 可能被运维手工 curl（无法从代码证明）；data 侧原路由不受影响 |
| **S-D. 裸镜像路由处置** | 选项 1：**保留**（legacy 页面仍活着，且 compose 无 SPA 部署——取证 e；非旁路，仅形状问题）；选项 2：改 4 个 static HTML 为 `/api` 前缀后删镜像（工作量集中且 legacy 页面本身是否退役是更大的独立议题） | 低 / 中 | 推荐**选项 1**：把「legacy 静态页退役」另立议题，本切片不夹带 |
| **S-E. vite 死代理清理** | 删 `/market`、`/stocks`、`/ohlcv` 三条，保留 `/api` | 零 | 纯删无消费者的 dev 配置；同时移除仓内最后一处 L3→L0 直连形态 |

### 组合方案（供裁决）

| 方案 | 内容 | 效果 |
|---|---|---|
| **甲（全量收口）** | S-A + S-B + S-C + S-E（S-D 选项 1） | `P5-1` 的「去除旁路取数」维度可判定**关闭**：L3 直连形态清零、死契约清零、SPA 数据管理面真实可用、gateway 契约与文档对齐 |
| **乙（最小收口）** | S-C + S-E + S-A（S-B 推迟） | 死代码清零 + 前端改直连……**不成立**：S-A 无 S-B 时前端到 jobs 家族无通路，只能退化为「删除死端点、DataSync 页同步面板下线」——即把切片 B 改判为「退役」而非「对齐」 |
| **丙（仅清死代码）** | S-C + S-E；SPA sync 死端点仅删除不对齐（DataSync 页保留 DataSourcePanel，同步面板摘除） | 旁路形态清零，但「SPA 数据管理面」能力为空，`P5-1` 该半句以「判定为无此需求」收口 |

---

## Decision

**用户裁决（2026-09-16）：方案甲（全量收口）** = S-A + S-B + S-C + S-E；**S-D 保留现状**（选项 1 —— 裸镜像路由与 legacy 静态页共存亡，legacy 页面退役另立议题，本切片不夹带）。

### 实施边界（含实施中发现并补齐的两项契约缺口）

| 项 | 内容 | 结果 |
|---|---|---|
| **S-A** 前端对齐 jobs 契约 | 5 文件重写：`types/sync.ts`（`SyncJob` 逐字对应 sync_jobs JSON + `CreateSyncJobRequest` / `CreateSyncJobResponse` 等，删 `SyncStatus` / `ImportTask` / 旧 `DataImportResponse`）；`api/sync.ts`（`createSyncJob` / `listSyncJobs` / `getSyncJob` / `cancelSyncJob` / `retrySyncJob` + SSE 路径 helper，删 3 死端点）；`stores/sync.ts`（jobs 列表态 + `activeJob` computed + `applyJobUpdate` upsert + `watchJob` EventSource 按 job 订阅 `/api/sync/jobs/:id/progress`，监听 `progress`/`complete` 事件）；`SyncStatusPanel.vue`（activeJob 进度视图：NProgress + job_type + processed/total + failed + SSE 连接态）；`DataImportForm.vue`（创建成功反馈 + snake_case 绑定） | ✅ |
| **S-B** analysis 网关代理 | `handlers_proxy.go` 增 `router.Any("/api/sync/*path", ...)` 前缀代理 → data `/api/sync/*`（`httputil.NewSingleHostReverseProxy` + `FlushInterval = -1` SSE 流式透传——现有 `proxyRequest` 的 JSON decode-and-replay 不适用于流式）。**顺带修复 viper 接线**：原读全局 viper（`loadConfig` 用局部 `viper.New()`，全局从未填充），`data_service.url` 恒空、硬编码 docker hostname 静默生效 —— 改签名接收 `deps.Viper` | ✅ |
| **S-C** 死代理路由退役 | 删 `/api/sync/calendar`、裸 `/sync/calendar`、`/api/v1/trading/calendar` 三条零消费路由（取证 d；data 直连 :8081 原路由不受影响） | ✅ |
| **S-E** vite 死代理清理 | `web/vite.config.ts` 删 `/market`→8081（**仓内最后一处 L3→L0 直连形态**）、`/stocks`→8085、`/ohlcv`→8085，保留 `/api`→8085 | ✅ |
| **契约缺口 ×1**（实施发现） | data 侧 `POST /api/sync/jobs` **不存在**（SPEC.md:1323 / e2e 三套件均按其编写，实现只有 legacy per-type 端点）→ 新增**类型化创建门** `createJobHandler`：按 7 种 job type switch 逐类型 unmarshal params + 缺省值 + 门上校验 400 fail-fast（未知类型列出支持列表 / missing symbols / 非法日期）；`jsonHasKey` 区分 key 缺失 vs 显式空数组（唯一宽容：`symbols` 显式 `[]` 接受）；`validDateRange` 双格式（YYYYMMDD / YYYY-MM-DD）兼容 | ✅ |
| **语义修正 ×1**（实施发现） | `createScheduleHandler` / `updateScheduleHandler` 原把一切错误当 500 → 新增哨兵 `sync.ErrInvalidCron`（`fmt.Errorf("%w: ...")` 包装、消息不变、可 `errors.Is`）→ 无效 cron 返回 **400** | ✅ |
| **S-D** 裸镜像路由 | 保留 4 条（`/ohlcv/:symbol`、`/screen`、`/stocks/count`、`/market/index`）—— 有活消费者（legacy 静态页）且非旁路（取证 e） | 裁决保留 |

---

## Metrics / 验证

### 静态检查（全绿）

- `go build ./...` EXIT=0
- `go test -count=1 ./cmd/analysis/ ./cmd/data/ ./pkg/storage/` 全 ok（`pkg/sync` 除外：`worker_test.go` 引用的 `newMockJobStore` mock 定义文件**从未入库**——存量问题非本切片引入，疑因 gitignore 误伤 `*_test.go` → 登记为 TASKS.md **P1-18**，全仓 `go test ./...` 门禁被其阻断）
- 前端：`vue-tsc --noEmit` 0 错 / `vitest run` 13 files 158 tests 全过 / `vite build` 成功
- e2e 三套件静态对齐真实契约：`stock_list`→`stocks` ×6、`finalBody.progress`→`progress_percent`、SSE 断言改按 job 订阅 `/api/sync/jobs/:id/progress`；**运行时全套件需 tushare token，待跑**

### 运行时取证（网关面 12 项全过）

环境：TimescaleDB `pg16` :15432 + Redis :16379 一次性容器 + data(:8081) / analysis(:8085) 本地二进制；`JWT_SECRET` 未设（open-access）；migrations 012/013 **手工应用**（内联 `migrate()` 不含、golang-migrate `MigrationManager` 无调用方——存量已知脱节）。

| # | 验收项 | 结果 |
|---|---|---|
| 1 | `POST /api/sync/jobs`（经 :8085 网关）type=stocks | **202** + job_id |
| 2 | `GET /api/sync/jobs?limit=5` | **200** + 列表 |
| 3 | `GET /api/sync/jobs/{id}` | **200** |
| 4 | SSE `GET /api/sync/jobs/{id}/progress` | **event: progress 流式到达**（`FlushInterval=-1` 生效） |
| 5 | `POST /api/sync/jobs/{id}/cancel` | **200** |
| 6 | `POST /api/sync/jobs/{id}/retry`（已取消 job） | **400**（语义拒绝） |
| 7 | `POST /api/sync/jobs` type=unknown | **400**（响应列出支持类型） |
| 8 | `POST /api/sync/jobs` type=ohlcv 缺 `symbols` key | **400** |
| 9 | `POST /api/sync/jobs` 非法日期 | **400** |
| 10 | 已删路由 `/api/sync/calendar` 等 3 条（网关面） | **404**（data 直连 :8081 原路由仍 200） |
| 11 | facade `/api/market/index`、`/api/stocks/count` | **200**（S-C/S-E 未伤合法面） |
| 12 | `POST /api/sync/schedules` 合法 cron **201** / 无效 cron **400**（`ErrInvalidCron` 语义生效）+ `GET /schedules` + `GET /workers` | **全过** |

非验收点备注：测试 stocks job 因 worker 池在 sync_jobs 建表前处于 error-backoff 未被 dequeue，保持 cancelled（一次性环境时序，非契约问题）。

---

## Lessons Learned

1. **viper 局部实例陷阱**：`loadConfig` 用局部 `viper.New()` 填充配置时，全局 viper 永不填充 —— 任何读全局 viper 的代码**恒得空值**，而 docker 内硬编码 hostname（`http://data-service:8081`）恰好等价于真实地址，把静默失效掩盖了数月。教训：配置一律经注入实例传递，禁止混用全局与局部 viper；排障时先查配置链路再查网络。
2. **mock `*_test.go` 不入库 = 假绿**：`worker_test.go` 引用的 `newMockJobStore` 从未 commit（疑因 gitignore 误伤 `*_test.go`），本地「全绿」只是本地恰有未跟踪文件 —— CI / 他人 clone 必编译失败。教训：新包提交时用 `git status --ignored` 核对测试辅助文件；全仓门禁要跑在干净 checkout 上。
3. **SSE 透传**：`httputil.ReverseProxy` 转发 `text/event-stream` 必须 `FlushInterval = -1`（逐事件立即 flush），且不可经 JSON decode-and-replay 型中间件 —— 中间件整体缓冲会吞掉流式语义。
4. **JSON 校验需区分「key 缺失」与「显式空值」**：`json.Unmarshal` 到 struct 无法区分二者；对 `json.RawMessage` 先 `jsonHasKey` 再 unmarshal，才能既 fail-fast 拒绝缺 key，又不误伤显式空数组（语义上交由运行期处理）。
