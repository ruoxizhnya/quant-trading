# ODR-051: P1 底座契约落地 — L0-1 单一摄取入口（归档接入真实路径 + 写入口 API）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§2 数据归属 / §3 四层架构 L0 单一写者 / §4 取数 / §5 证据服务 — 本次落地的依据）
> **Supersedes**: —
> **Related ODRs**: [ODR-050](odr-050-p1-base-contract-landing.md)（P1 三项先行，本记录承接其列明的剩余项 L0-1）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计）, [ODR-048](odr-048-top-level-product-redefinition.md)（顶层产品重定义）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-050](odr-050-p1-base-contract-landing.md) 冻结了 L0-2 / L0-3 / L0-4 三项底座契约，并把 L0-1 列为 P1 剩余项。L0-1 的出口判据是 **「任意外部组件可经 Evidence API 拿到唯一原始记录」**——在 L0-1 落地前，`ingest.raw` 有表、有读写函数、有只读证据 API，**但没有任何写入方**，Evidence API 对一切 hash 只能返回 404。

### 前置事实（本次现场复核，逐条实证）

| 项 | 复核结论 |
|---|---|
| 声明的「单一摄取入口」 | `pkg/data/source/` 的 `Registry` + `ETLPipeline` —— **不在真实数据路径上** |
| `Registry` 的消费方 | 仅 `cmd/data/registry_init.go` 构建、`registry_handlers.go` 的 `/api/datasource/registry/{status,health,chains}` 诊断端点消费 |
| `ETLPipeline.Process` 的调用方 | **全仓仅有测试调用** |
| 真实取数路径 | `cmd/data/sync_handlers.go` 各 executor **直连** `data.TushareClient.Fetch*` |
| `TushareAdapter.SupportedTypes()` | 仅 2 类型，而真实使用中的数据集有 7+ —— 完整重接 Registry 属多 Sprint 工程 |
| tushare 响应的唯一咽喉点 | `TushareClient.call()`（`pkg/data/tushare.go`）—— `resp.Body`（原始 HTTP body）在此可见且在**任何调用方规范化之前** |
| `ingest.raw` 写入方（改动前） | **0 个** |
| `TushareStore` mock 影响面 | `createTestTushareClient`（`tushare_test_helpers.go`）构造 client 时传 `&storage.PostgresStore{}`（空结构体）→ **接口加方法无需改 mock** |
| 工具链 / Docker | `go` 可执行文件为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`；Docker Desktop 未运行 → DB-backed 测试按既有约定 SKIP |

---

## Decision

**把归档挂到真实取数路径的咽喉点，并开一道窄写入口供外部生产者上报，使 `ingest.raw` 首次拥有真实写入方。本轮明确不做 `source.Registry` 完整重接。**

### 1. 归档接入真实路径（A 面，进程内）

- **注入点**：`TushareClient.call()` —— 仅归档 `Code == 0` 的成功响应。错误信封不是任何人可以引用的数据。
- **归档对象**：原始 HTTP body（`resp.Body`），**不是**规范化后的行。理由：规范化数据（`market.*`）可从原始响应重算，反之不可。
- **实现**：`pkg/data/tushare_raw.go`
  - `archiveRaw(ctx, apiName, params, body)`：`ContentHashOf` → `RawIngest{source: "tushare", dataset: "tushare."+apiName, key, payload}` → `SaveRawIngest`。
  - `rawIngestKey(apiName, params)`：参数按名排序渲染为确定性 key（如 `stk_factor_pro:end_date=20240930&ts_code=600519.SH`），空值丢弃 —— 同一请求重放必须产生同一 key，否则 `ingest.raw` 不再是稳定的引用坐标。
  - `as_of` **故意留 NULL**：单个 tushare 响应没有单一观察日（一次请求可跨数千交易日），`content_hash` 已唯一标识该响应。
- **best-effort**：归档失败只记 `warn`，不影响 fetch 结果。fetch 结果本身仍然有效，且 `ingest.raw` 追加式，下次 fetch 可再归档。

### 2. 写入口 API（B 面，HTTP）

- **端点**：`POST /api/ingest/raw` —— 平台写入 `ingest.raw` 的唯一 HTTP 门（`cmd/data/handlers_ingest.go`）。
- **为什么必须有**：工作面 1 的 akshare 侧取的是 data 服务尚未拥有的数据源。没有这道门，akshare 数据永远进不了 L0，EquityDeep 就不得不自留一份副本 —— 正是 ADR-022 禁止的重复存储。
- **门很窄**：逐字归档 payload 并返回 `content_hash`；**不做**规范化、解释或领域内容校验。规范化是独立的、可重放的一步，它读回这些行。
- **校验顺序**（任何一步失败都在写入之前返回 400）：required 字段 → `ContentHashOf` → 可选 `content_hash` 一致性校验 → `as_of` 解析。
- **幂等**：`content_hash` 为主键 + `ON CONFLICT DO NOTHING`，重复上报是 no-op，永不改写原始归属。
- **`as_of` 格式**：接受 `YYYY-MM-DD`（Python `datetime.date` 的输出）或 RFC3339；空串 → NULL。
- **body 上限** 32 MiB：`ingest.raw` 存逐字源响应，上限需宽但必须有界。

### 3. 契约收紧

`TushareStore` 接口新增 `SaveRawIngest(ctx, *storage.RawIngest) error`（`pkg/data/tushare.go` L36）—— 归档契约在**编译期**强制，而非运行时才发现。

---

## Consequences

### 正面

- **P1 出口判据达成**：`ingest.raw` 首次拥有真实写入方；任意外部组件上报后即可经 `GET /api/evidence/{content_hash}` 取回唯一原始记录，ODR-047 的 P0 假阳性缺陷在**运行时**而非仅在架构上不可复现。
- **不重复存储落地可验证**：工作面 1 可以只存 `content_hash` + 叙事，数据本体留在 L0。
- **零存量表改动**：仅新增代码与路由，无 DDL 变更，活跃表数仍为 38。
- **两个生产者共用一条归档契约**：data 服务进程内归档与外部 HTTP 上报最终走同一个 `SaveRawIngest`，`ingest.raw` 保持单一写者语义。

### 负面 / 代价

- `pkg/data` 的 `TushareStore` 接口面扩大 1 个方法（当前唯一实现为 `*storage.PostgresStore`，无 mock 需要同步）。
- 归档是**同步**写入，处在 fetch 热路径上。当前以 best-effort + 独立 `ingest.raw` 写入控制影响面；异步化未做（见风险表）。
- 新测试文件（`pkg/data/tushare_raw_test.go` / `cmd/data/handlers_ingest_test.go`）被 `.gitignore` 的 `*_test.go` 规则排除，须 `git add -f` 显式纳入（与 ODR-050 同一坑）。
- DB-backed 用例在当前环境（Docker 未运行）SKIP，`POST /api/ingest/raw` 的**真实写入路径未被端到端验证**，须在容器起来后补跑。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 归档在 fetch 热路径上同步写库，可能拖慢取数 | 中 | 归档失败不影响 fetch；若压测显示瓶颈，改为异步队列（属性能优化，非契约变更） |
| 完整 `source.Registry` 仍未接真实路径 —— 声明的「单一入口」与实际的「咽喉点归档 + 窄写入口」之间存在落差，未来重接时归档点可能迁移 | 中 | 归档契约（`SaveRawIngest` + `RawIngest`）已稳定，重接只换**调用者**不换**契约** |
| `ingest.raw` 无冷热分层，随时间无限增长 | 中 | 已登记为未决问题 **Q-2**，沿用 ODR-050 结论，本轮不做 |
| 写入口无鉴权，任何可达 data 服务者均可写入 | 中 | 与 data 服务其余端点同级（当前均为内网无鉴权）；若对外开放须纳入统一鉴权，登记为后续项 |
| tushare/akshare 双源同一数字可能产生两条证据行，引用坐标不唯一 | 中 | 已登记为未决问题 **Q-6**（双源对账），本轮不做 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `pkg/data/tushare_raw.go` | `archiveRaw` + `rawIngestKey` + `tushareSourceName` |
| **新建** | `pkg/data/tushare_raw_test.go` | key 确定性 / 空值丢弃 / 逐字归档 / 等价内容同哈希 / 不可用输入静默跳过 |
| **新建** | `cmd/data/handlers_ingest.go` | `ingestRawHandler` + `ingestRawRequest` + `parseIngestAsOf` + 32 MiB 上限 |
| **新建** | `cmd/data/handlers_ingest_test.go` | 7 个 400 拒绝用例 + `parseIngestAsOf` 四种输入 + DB-backed 201 往返与幂等 |
| **修改** | `pkg/data/tushare.go` | `TushareStore` 接口新增 `SaveRawIngest`；`call()` 在 `Code==0` 后调用 `archiveRaw` |
| **修改** | `cmd/data/main.go` | 注册 `POST /api/ingest/raw` |
| **修改** | `cmd/data/setup_test.go` | 结构不变量守护清单纳入 `handlers_ingest.go` |
| **修改** | `docs/openapi.yaml` | 新增 `POST /api/ingest/raw` + `RawIngestWrite` schema |
| **修改** | `docs/TASKS.md` | L0-1 ⬜ → ✅；统计 15/1/216 → 14/1/217；版本 3.25.0 → 3.26.0；新增 changelog |
| **修改** | `docs/ADR.md` | ODR 索引 50 → 51 |
| **新建** | `docs/odr/odr-051-l0-1-single-ingest-entry.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 新建文件 | 5（2 源码 + 2 测试 + 本 ODR） |
| 修改文件 | 6（tushare.go / main.go / setup_test.go / openapi.yaml / TASKS.md / ADR.md） |
| 新增表 | 0；存量表改动 **0**（活跃表数仍 38） |
| 新增端点 | 1（`POST /api/ingest/raw`） |
| `go build ./...` | 通过（exit 0） |
| `go test ./pkg/data/ ./pkg/storage/ ./cmd/data/` | 通过（DB-backed 用例按约定 SKIP） |
| `go test ./...` | 仍有 5 个**既有**包失败（见下），与本次改动无关 |
| 任务增减 | 0（L0-1 由 ⬜ → ✅，总计 233 不变） |
| ADR 累计 | 22 |
| ODR 累计 | 51 |

> **既有失败（非本次引入，未修复）**：`internal/sandbox/runner`（7 例，需进程派生，被当前沙箱阻断）、`pkg/sync`（构建失败 —— `worker_test.go` 引用未定义的 `newMockJobStore`）、`pkg/backtest/state`、`pkg/live`、`pkg/strategy` 各 1 例。基线既有，本轮不动。

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| `source.Registry` 完整重接真实路径 | 需为 stocks/calendar/dividends/splits/index_constituents 补 data type + adapter，属多 Sprint 工程 |
| akshare adapter 落地 | 属工作面 1（EquityDeep）侧实现，本轮只提供写入口 |
| 归档异步化、`ingest.raw` 冷热分层 | 性能优化，非契约；后者为 PRODUCT.md Q-2 |
| 写入口鉴权 | 与 data 服务其余端点同为内网无鉴权，须统一处理 |

---

## Lessons Learned

1. **「声明的入口」不等于「真实的入口」** —— `source.Registry` / `ETLPipeline` 看起来就是摄取层，但 `Registry` 只服务诊断端点、`Process` 只被测试调用。**在往一个"入口"上挂东西之前，必须先证明它在调用链上**（本次靠"谁调用它"的反向检索证实），否则会写出"接了但没人走"的归档。
2. **归档要挂在咽喉点，不要挂在抽象层** —— `TushareClient.call()` 是全部 tushare 响应的唯一出网口，挂这里以最小代价覆盖全部 7+ 数据集；挂 Registry 需要先补齐 5 个缺失的 data type。
3. **证据要归档"不可重算的那一层"** —— 归原始 body 而非规范化行：规范化可重算，原始响应不可重算。**归档层级选错，可重建性宣称就是假的**。
4. **窄门的价值在于拒绝做多余的事** —— 写入口只做"归档 + 返回 hash"，不做规范化与领域校验。**门一旦开始解释数据，它就成为第二个数据模型**。
5. **契约放在编译期** —— 把 `SaveRawIngest` 加进 `TushareStore` 接口，让"归档能力"成为 client 的构造前提；否则归档会退化为可选路径，随时被静默跳过。
6. **测试文件的可见性要单独确认** —— `*_test.go` 在 `.gitignore` 中，连 ripgrep 也据此跳过；定位测试须用 Glob，新增测试须 `git add -f`。

---

_本记录为 2026-09-15 P1 底座契约 L0-1 落地操作。至此 L0-1 ~ L0-4 四项全部冻结，阶段 P1 剩余任务（EQD-P0-1 契约冻结 / EQD-P0-2 抽检泛化 / EQD-P3-2 文档漂移）与阶段 P2~P5 的实施情况应记入新的 ODR，不在本文件追加。_