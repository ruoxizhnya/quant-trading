# ODR-059: 阶段 P5 切片 2 — 退役运行时数据源切换门（`POST /api/datasource/switch` 去留存废评估）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§1 外部数据源不得由本服务直连取数 / 单一摄取入口）
> **Supersedes**: —
> **Related ODRs**: [ODR-058](odr-058-p5-1-retire-direct-providers.md)（P5-1 切片 1 — 本记录承接其 §6 / 「未做项」中显式排除的 **P-B**）, [ODR-051](odr-051-l0-1-single-ingest-entry.md)（L0-1 单一摄取入口）, [ODR-020](../adr/adr-020-engine-decomposition.md)（`SetDataAdapter` / `SwitchDataSource` 桥接的来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-058](odr-058-p5-1-retire-direct-providers.md) 把阶段 P5 的 `P5-1`（验收断言「两个工作面共享 L0-L2，**无平行数据路径**」，`docs/PRODUCT.md:301`）切成片执行，其旁路勘察列出 4 条路径，其中 **P-B**（`cmd/analysis` 运行时数据源切换门 `POST /api/datasource/switch`）因属**用户可见行为变更**而被显式排除：

> `docs/odr/odr-058-*.md:201` — 「**`POST /api/datasource/switch`（P-B）** —— 用户可见行为变更，须单独评估（见 §6）。」

本记录即该「单独评估」的取证与裁决。

### 现场取证（三条事实）

| # | 取证 | 方法 | 结论 |
|---|---|---|---|
| **a** | **生产接线下一按即崩** | 按 `cmd/analysis/setup.go:269` 的真实接线 `marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)`（`bus = nil`）构造 adapter，调用 `SetPrimary` → **复现 panic**: `runtime error: invalid memory address or nil pointer dereference` | `DataAdapter.SetPrimary`（`pkg/marketdata/adapter.go:49`）**无条件** `a.bus.Publish(...)`（`:63`），而生产接线传的正是 `nil` EventBus ⇒ 该「已交付能力」在默认生产配置下**不可用**（panic 被 `gin.Recovery()` 吞成 HTTP 500） |
| **b** | **`http` 分支收任意 URL** | 读 `handlers_datasource.go` 原 `switch` handler + `marketdata.NewHTTPProvider(req.URL, logger)` | 请求体 `{"type":"http","url":"<任意>"}` 可把引擎读源指向任意外部服务 —— 与 ADR-022 §1 / `PRODUCT.md:271`「单一摄取入口不可绕过」**直接冲突**；且该门在 `authSvc.Enabled()` 默认关闭时**无鉴权**（仅 100/min 限流） |
| **c** | **驱动它的配置是死的** | 全仓 Grep `datasource.*` 配置读取点 | **0 个读取点**；`config/analysis-service.yaml` 的 `datasource:` 块与其对应的 `DataSourceConfig` / `AdapterFactory`（`pkg/marketdata/config.go`）**全仓 0 生产调用者**（ODR-058 已核验工厂 0 调用）⇒ 切换门所依赖的「多源可切换」模型**从未被生产接线使用** |

取证 (a) 是被删测试前的**临时探针**实测（探针文件已在取得实证后删除，未提交）。

### 去留存废候选与裁决

| 候选 | 说明 | 结果 |
|---|---|---|
| **退役 switch，保留 status / health** | 删 `POST /api/datasource/switch`（后端 handler + 前端切换表单 + store/api/types + `openapi.yaml`），保留只读 `GET /status` 与 `GET /health`；顺带清死配置 `datasource:` 块与死工厂 `DataSourceConfig` / `AdapterFactory` | ✅ **采纳**（用户裁决） |
| 保留并修好 switch | 补 `nil` EventBus 防护 + 加鉴权 + URL 白名单 |  与 ADR-022 §1 冲突：切换门的**语义**（运行时把读源指向任意外部服务）本身就是「平行数据路径」，加固只能降低风险、不能消除架构冲突 |
| 保留 handler、仅加鉴权/白名单 | 最小改动 | ✗ 门仍暴露「任意 URL」形状，且 (a) 的 panic 与 (c) 的死配置未解决 |
| 整体删除 `DataAdapter` 抽象 | 彻底收敛 | ✗ 超出本评估范围：`DataAdapter` 的 `resolveProvider`（primary 健康检查 + fallback 降级）仍是生产读路径的一部分 |

---

## Decision

**退役 `cmd/analysis` 的运行时数据源切换门 `POST /api/datasource/switch`，保留只读观测端点 `GET /api/datasource/status` 与 `GET /api/datasource/health`；读源由启动期配置 `data_service.url` 固定。同步清理其前端链路、OpenAPI 描述、死配置与死工厂。**

### 1. 后端 handler 退役

`cmd/analysis/handlers_datasource.go`：删除 `POST /switch` handler 与其 `logger` 参数，保留 `GET /status`（`enabled` / `primary` / `stopped`）与 `GET /health`（`adapter.CheckConnectivity`）原逻辑不变；函数签名收敛为 `registerDatasourceRoutes(router *gin.Engine, engine *backtest.Engine)`（`cmd/analysis/main.go:325` 同步）。

### 2. 前端切换链路退役

| 文件 | 变更 |
|---|---|
| `web/src/components/sync/DataSourceSwitch.vue` | **删除**（用户可见切换表单） |
| `web/src/pages/DataSync.vue` | 去 import 与其 `NGridItem`（页面余 `DataSourcePanel` / `SyncStatusPanel` / `DataImportForm`） |
| `web/src/stores/sync.ts` | 去 `switchDataSource` import、`switchSource()` action 与其导出 |
| `web/src/api/sync.ts` | 去 `switchDataSource()` 与其类型 import |
| `web/src/types/sync.ts` | 去 `DataSourceSwitchRequest` / `DataSourceSwitchResponse` |
| `web/src/stores/sync.test.ts` | 去 switch mock 与 `'should switch data source'` 用例 |

### 3. 死配置与死工厂清理

| 文件 | 变更 | 依据 |
|---|---|---|
| `config/analysis-service.yaml` | 删整段 `datasource:`（`primary` / `fallback` / `cache` / `sources`）及其上方 ADR-022 注释 | 全仓 0 读取点（取证 c）；注释中「工厂对这两个 type 显式拒绝」随工厂删除而失效 |
| `pkg/marketdata/config.go` | **整文件删除**（`DataSourceConfig` / `SourceConfig` / `FactoryDeps` / `AdapterFactory` 及其 `BuildPrimary` / `BuildFallback` / `BuildAdapter` / `BuildProvider` / `wrapCache` / `buildProvider` / `DefaultDataSourceConfig`） | 全仓 0 生产调用者（ODR-058 §前置事实已核验；本切片复核仍为 0） |

### 4. 明确保留项（本切片不动）

| 项 | 状态 | 说明 |
|---|---|---|
| `GET /api/datasource/status` / `GET /health` | ✅ 保留 | 只读观测，不构成取数路径；是运维判断「当前读源是谁、是否连通」的唯一入口 |
| `Engine.SwitchDataSource`（`pkg/backtest/engine.go:366`） |  不动 | 删掉 HTTP 门后其**生产调用者归 0**，但仍被 `pkg/backtest/engine_accessors_test.go` 引用，且属 engine 层进程内 API（无外部触发面）。去留属独立议题（见「未做项」） |
| `DataAdapter.SetPrimary`（`pkg/marketdata/adapter.go:49`） |  不动 | 同上；其 nil-bus panic（取证 a）在切换门退役后**已不可从生产到达**，但缺陷本身未修（见「未做项」） |
| `pkg/marketdata/cached_provider.go` | ⛔ 不动 | `NewCachedProvider` / `CacheConfig` 原被已删 `config.go` 的 `wrapCache` 引用；删工厂后生产调用者归 0，但其为包级公开 API（缓存装饰器），去留属独立议题 |
| `pkg/marketdata/{adapter,provider,eventbus}.go` 等 | ⛔ 不动 | 仍在生产读路径上（`NewHTTPProvider` / `NewPostgresProvider` / `NewDataAdapter`） |

---

## Consequences

### 正面

| 收益 | 说明 |
|---|---|
| **平行数据路径再减少 1 条** | P5-1 验收断言「无平行数据路径」的最后一条**用户可见**路径被物理删除；读源不再可在运行时被指向任意外部服务 |
| **消除一个不可用的「已交付能力」** | 取证 (a) 证明该门在生产接线下一按即崩（nil-bus panic → 500）。删除即消除「文档/前端声称可用、实际必崩」的语义漂移 |
| **架构约束可执行化** | 「读源只能来自 L0 数据面」从「不该用切换门」的约定，变为**接口不存在**——不需要额外纪律来遵守 |
| **配置面清零** | `datasource:` 块 + `DataSourceConfig` / `AdapterFactory` 整族删除，`config/analysis-service.yaml` 不再声明一个无人读取的「多源可切换」模型 |
| **前端包体与测试收敛** | 删 1 个组件 + 1 个 store action + 1 个 API 函数 + 2 个类型 + 1 个用例 |

### 负面 / 代价

| 代价 | 说明 | 缓解 |
|---|---|---|
| 失去运行时切换读源的能力 | 若 `data_service.url` 指向的 data-service 不可用，运维无法经 HTTP 热切到备用读源，须改配置 + 重启 | 这是 ADR-022 §1 的**有意代价**；且该能力在生产接线下**本就不可用**（取证 a），实际损失为零 |
| 前端「数据同步中心」少一张卡片 | 页面从 4 卡变 3 卡 | 该卡片的按钮在生产下必然 500，删除消除误导 |
| 破坏性变更（API 面） | 任何外部调用方仍 `POST /api/datasource/switch` 将得 404 | 该门仅由本仓 Vue SPA 消费（已同批删除）；`docs/SPEC.md` / `openapi.yaml` / `ARCHITECTURE.md` 已同步 |

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 误删仍被引用的符号 | 低 | `go build ./...` EXIT=0；`pkg/marketdata` / `pkg/backtest` / `cmd/analysis` 测试全绿；前端 `vue-tsc --noEmit` + ESLint（0 error）+ vitest 全绿 |
| 遗留「无调用者」符号（`SetPrimary` / `SwitchDataSource` / `NewCachedProvider`） | 低 | 已在 §4 逐条登记为「保留项」并说明理由；其去留作为独立议题留待评估，不在本切片夹带 |
| nil-bus panic 缺陷仍在代码中 | 低 | 已不可从生产到达（唯一触发点 = 已删 HTTP 门 + 已删死工厂 `BuildAdapter`）；若不删 `SetPrimary` 则应单独立项修（见「未做项」） |

---

## Artifacts

### 代码

| 文件 | 变更 |
|---|---|
| `cmd/analysis/handlers_datasource.go` | 删 `POST /switch` handler 与 `logger` 参数；函数签名收敛；新增退役理由注释（指向 ODR-059 / ADR-022 §1） |
| `cmd/analysis/main.go` | `registerDatasourceRoutes(router, deps.Engine, deps.Logger)` → `registerDatasourceRoutes(router, deps.Engine)` |
| `pkg/marketdata/config.go` | **删除**（死工厂整族） |

### 配置

| 文件 | 变更 |
|---|---|
| `config/analysis-service.yaml` | 删整段 `datasource:` 及其上方 ADR-022 注释 |

### 前端

| 文件 | 变更 |
|---|---|
| `web/src/components/sync/DataSourceSwitch.vue` | **删除** |
| `web/src/pages/DataSync.vue` | 去 import + 去卡片 |
| `web/src/stores/sync.ts` | 去 `switchSource()` + import + 导出 |
| `web/src/api/sync.ts` | 去 `switchDataSource()` + 类型 import |
| `web/src/types/sync.ts` | 去 `DataSourceSwitchRequest` / `DataSourceSwitchResponse` |
| `web/src/stores/sync.test.ts` | 去 switch mock 与用例 |

### 文档

| 文件 | 变更 |
|---|---|
| `docs/odr/odr-059-p5-1-retire-datasource-switch.md` | **新建**（本记录） |
| `docs/openapi.yaml` | 删 `/api/datasource/switch`（`operationId: switchDatasource`）整段 |
| `docs/SPEC.md` | 两处 API 清单删 switch 行 + 退役说明 |
| `docs/ARCHITECTURE.md` | 端点清单删 switch 行 + 退役注 |
| `AGENTS.md` | 请求链路图删 `POST /api/datasource/switch ──► ProviderManager` 行 |
| `docs/TASKS.md` | 头部版本 3.33.0 → 3.34.0；`P5-1` 行补切片 2；阶段 P5 进展注改为「切片 2 完成」；统计表 Sprint 8 行 + 变更日志 |
| `docs/ADR.md` | ODR 索引新增 ODR-059；尾注 ODR 58 → 59；index 3.14.0 → 3.15.0 |

---

## Metrics / 验证

| 项 | 结果 |
|---|---|
| `go build ./...` | **EXIT=0** |
| `go test -count=1 ./pkg/marketdata/ ./pkg/backtest/ ./cmd/analysis/` | **全部 `ok`（EXIT=0）** |
| 前端 `npm run typecheck`（`vue-tsc --noEmit`） | **EXIT=0** |
| 前端 `npx vitest run` | **11 files / 153 tests 全通过**（含 `sync.test.ts` 14 项） |
| 前端 `npm run lint` | **0 error**（642 warning，均为既有 `vue/max-attributes-per-line` 类风格告警） |
| **验收点**：仓内 `POST /api/datasource/switch` 引用 = 0 | ✅ 全仓 Grep `DataSourceSwitch\|switchSource\|switchDataSource\|datasource/switch\|switchDatasource` → 仅剩**本记录与历史/归档文档**的叙述性引用（`docs/archive/**` 按历史口径保留）+ 新 handler 注释中的退役说明；**无任何活代码引用** |
| 死配置读取点 | ✅ `datasource:` 块删除后全仓配置读取点仍为 0（原本即 0） |
| 无关失败（不属本切片） | `pkg/backtest/state` 的 `TestDiskStateStore_ConcurrentSaveLoad` 因用例生成含 `>`/`<`/`?` 的临时文件名在 Windows 上失败 —— **既有平台缺陷**（ODR-058 已记录），本切片未触及该包 |

---

## 未做项（明确排除）

1. **`Engine.SwitchDataSource` / `DataAdapter.SetPrimary` / `NewCachedProvider` 的去留** —— 删门后其在生产侧归 0 调用者，但仍被单元测试引用；是否连带删除（或改为非导出）须独立评估，避免与「退役 HTTP 门」这一**用户可见变更**混在同一切片。
2. **`DataAdapter.SetPrimary` 的 nil-bus panic 修复** —— 取证 (a) 的缺陷本身未修（已不可从生产到达）。若保留该函数，应单独立项补 `if a.bus != nil` 防护或改为启动期注入非 nil bus。
3. **`/api/datasource/status` 的 `stopped` 字段语义** —— 该字段来源 `adapter.Stopped()`，与切换门同属 DataAdapter 机制；本切片只退役写入口，未复核该字段在多源模型删除后是否仍有意义。
4. **`P5-1` 是否关闭** —— 本切片只完成其「**去除旁路取数**」维度；`P5-1` 描述中的「Vue SPA / Research Engine 存量能力**对接 L0 单一数据面 + Evidence API**」尚未开始，故 `P5-1` 整体保持未关闭。
5. **`docs/odr/odr-058-*.md` 原文回写** —— 按本仓既有口径（ODR-057 裁决），**历史决策记录保留原文不回写**；ODR-058 §6 的「P-B 不动」在其时点成立，本记录以 `Related ODRs` 承接。
6. **`docs/archive/research-2026-Q2/CODE_REVIEW_REPORT.md`** —— 其中「`/api/datasource/switch` 等危险操作完全开放」属**归档历史快照**，按历史口径保留原样（其结论正由本记录落实）。

---

## Lessons Learned

1. **「用户可见能力」不等于「真实可用的能力」** —— 该门有前端表单、有 store action、有 OpenAPI 描述、有 handler，形态完整；但按生产接线（`NewDataAdapter(nil, ...)`）走到 `SetPrimary` 必然 panic。**评估存废时必须按真实接线复现一次**，否则会把「文档/UI 声称的能力」误当成「存量语义」而给出过高的兼容性顾虑。
2. **测试用非 nil 依赖注入会掩盖生产缺陷** —— `adapter_test.go` 的 3 个 `SetPrimary` 用例与 `engine_accessors_test.go` 的 2 个 `SwitchDataSource` 用例都传 `NewEventBus(2)`，恰好绕开了生产唯一会崩的 `nil` bus 路径。**构造器参数在生产侧为 nil 时，单元测试应显式覆盖该 nil 路径**，否则「测试全绿」与「生产必崩」可以同时成立。
3. **架构冲突无法靠加固消除** —— 切换门的语义就是「把读源指向任意外部服务」，加鉴权 / 加白名单仍保留这个形状。当一个能力与 L0 原则**语义冲突**（而非仅安全性不足）时，正确动作是退役而非加固。
4. **退役要连着「驱动它的配置」一起清** —— `datasource:` 块 + `DataSourceConfig` / `AdapterFactory` 是该门所依赖的「多源可切换」模型；只删 HTTP handler 会留下一族无人读取却看似一等公民的配置与工厂，下一轮勘察仍会把它当成活路径。

---

_Last updated by: AI Assistant — 2026-09-15 (P5-1 切片 2: 退役 P-B 运行时数据源切换门)_