# ODR-057: 阶段 P4 — EQD-P2-1 第 19 个 MCP 工具 `research.profile`（C-5 / 桥 B2）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§2 五分区（类 E `research.*` 可 DROP 重建）/ §5 citation 5 元组 / §6 飞轮四步闭环 — 本次落地的是飞轮第 1 步「读回研究档案」的入口）
> **Supersedes**: —
> **Related ODRs**: [ODR-050](odr-050-p1-base-contract-landing.md)（L0-4 `research` schema DDL — 本记录首次为它接上读取方）, [ODR-052](odr-052-p1-contract-freeze-and-spot-check.md)（契约 C2 `_profile.json` 冻结 — 本记录的 vault 回退路径按其校验）, [ODR-046](odr-046-hermes-agent-integration-decision.md)（MCP 工具桥范式 — 本记录是第 19 个工具的加入）, [ODR-056](odr-056-fundamentals-table-consolidation.md)（阶段 P3 收口，本记录为其后续阶段的起点）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计 — C-5 缺口来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

阶段 P3 于 [ODR-056](odr-056-fundamentals-table-consolidation.md) 3/3 关闭后进入阶段 P4「飞轮打通」。P4 两项任务中，`P4-1`（飞轮闭环端到端）需要 EquityDeep 仓配合，而 **`EQD-P2-1` 是唯一纯 Quant Lab 侧、无外部依赖的一项**，因此先行。

C-5 在 [RESEARCH.md](../RESEARCH.md) §3.5 早已给出接口草案（端点、请求/响应形态、实现要点），但**只有规格没有实现**：`pkg/tools/builtin/` 停在 18 个工具；`research` schema 自 [ODR-050](odr-050-p1-base-contract-landing.md) 落地以来**只有 DDL、零 Go 读取代码** —— 三张表有结构、有索引，没有任何一行被读过。

出口判据：**Hermes 能经 `GET/POST /api/tools/research.profile` 读到某标的的研究档案（结论 + 疑点），每个论断都带 citation 锚定；无档案时得到明确的 not-found 而非空对象。**

### 前置事实（本次现场复核）

| 项 | 复核结论 |
|---|---|
| `research` schema 表列权威 | `pkg/storage/postgres.go` L264-302 内联 `migrate()`，与 `docs/migrations/021_add_research_schema.sql` 逐字一致 |
| `research.*` 读取代码（改动前） | **0 个** —— 三表只有写入侧 DDL，无任何 `SELECT` |
| 契约 C2 | `contracts/profile.schema.json` 已冻结（[ODR-052](odr-052-p1-contract-freeze-and-spot-check.md)）：`schema_version const 1`、`ticker ^[0-9]{6}$`、`additionalProperties: false`、citations 每项 required `content_hash ^[0-9a-f]{64}$` + `pointer ^/` |
| 契约 C2 冻结语义 | ①`schema_version` 恒 1；②**过期检测为消费方必做**（`source_mtime > generated_at` 或超龄 → 降级并标注 stale）；③输出不得含未经 citation 锚定的论断（不做 LLM 二次加工） |
| `profile` 表有无 `generated_at` 列 | **无** —— 只有 `source_mtime` / `last_researched` / `updated_at`；契约 C2 的 `generated_at` 在投影侧无同名列 |
| vault 镜像位置 | `{EQUITYDEEP_VAULT_PATH}/{ticker}/_profile.json` |
| 配置可达性 | `cmd/analysis/setup.go` 已 `AutomaticEnv()` + `SetEnvKeyReplacer(".", "_")` ⇒ `EQUITYDEEP_VAULT_PATH` 可经 `v.GetString("equitydeep.vault_path")` 读到（**零配置面改动**） |
| 工具接口范式 | `Tool = ToolCore + SchemaProvider + Executable`（ISP 三拆）；`Registry.Register` 同名替换；dot namespace 工具名 |
| 既有工具数与分组 | 18 个 / 9 组（`buildToolsRegistry()`，7 参签名） |
| 依赖方向 | `pkg/storage` **不** import `pkg/tools` / `pkg/risk` / `pkg/gene_pool` ⇒ 存储侧不能反向引用工具侧接口 |
| 窄接口注入先例 | `gene_pool_tools.go` / `market_regime_tool.go`：包内定义窄接口 + 具体类型隐式满足 + `var _ X = (*T)(nil)` 编译期断言 |
| 错误分类范式 | `respondToolError` 已映射 `ErrToolNotRegistered`→404 / `ErrInvalidArgs`→400；**无 not-found 语义** |
| gin 路径参数行为 | `/:name` **只按 `/` 切分**（不按 `.`），含点号的工具名可被捕获 —— 本记录以实测固化（见 §4） |
| 工具数断言散落位置 | ARCHITECTURE.md / AGENTS.md / SPEC.md / VISION.md / hermes（config + YAML 清单 + 验收测试）/ `pkg/ai/agents/doc.go` |
| 历史决策记录中的「18」 | [ODR-046](odr-046-hermes-agent-integration-decision.md)（含 `18 (across 9 groups)` 指标行）/ `adr-015-ai-agent-architecture.md` L24 —— **本次不回写**（见 §5） |
| 工具链 / Docker | `go` 为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`；Docker Desktop 未运行 → DB-backed 测试按既有约定 SKIP |
| 本仓 git 写对象 | 默认 createObject 模式下 `commit` / `add` 报 `Permission denied`，须 `git -c core.createObject=link` |

### 档案来源裁决

| 候选 | 说明 | 结果 |
|---|---|---|
| **PG 投影优先 + vault 回退** | 先读 `research.*` 投影；投影无该标的数据时回退读 vault `_profile.json` | ✅ **采纳** |
| 仅 vault（`_profile.json` 为唯一来源） | 实现最简，C-5 可立即端到端验证 | ✗ 与 ADR-022 §4 方向相反（`_profile.json` 将**降级为导出格式**、不再作权威存储），`EQD-P2-1`（EquityDeep 接 PG）落地后整个来源解析要重写 |
| 仅 PG | 完全对齐最终形态，无临时分支 | ✗ `research` schema 当前**无写入方**（写入方是阶段 P2 的 `EQD-P2-1`，在 EquityDeep 仓），工具将恒返回 404，C-5 无法验证 |

裁决理由：`TASKS.md` 原文写「读 `research` schema 投影 / `_profile.json` 导出镜像」，本身就要求**两者都要读**；PG 优先使 P2-1 落地后只需**删掉回退分支**（而非重写来源解析），是中转态代价最小的方向。

---

## Decision

**新增第 19 个 MCP 工具 `research.profile`：storage 侧补上 `research.*` 的第一个读取方，工具侧以「PG 投影优先 → vault 镜像回退 → not-found」解析来源，输出逐字对齐契约 C2（citations 原样透传、不做 LLM 二次加工）。**

### 1. 读取层 `pkg/storage/research.go`（新建）

`ResearchProfile` / `ResearchConclusion` / `ResearchQuestion` 三个结构体（JSON tag 对齐契约 C2 命名），加：

- `GetResearchProfile(ctx, ticker)`：主表 `research.profile` 单行 + 两张子表列表。三处语义是**有意区分**的：
  - `ticker == ""` → `ErrEmptyResearchTicker`（参数错误，非「无档案」）；
  - 主表无行 → **`(nil, nil)`** —— 「这个标的没有档案」由调用方映射为 404，「挂库失败」才是 error，两者不能混；
  - 子表无行 → **空切片而非 error** —— 有档案但没写结论是合法状态。
- 子表查询 `ORDER BY id`（输出确定性）+ `out := make([]T, 0)`（`nil` 切片会序列化成 `null`，契约 C2 要求数组）。
- `citations JSONB` 以 `json.RawMessage` 承接后**原样透传**：中间层不解析、不重建、不补全。

### 2. 工具本体 `pkg/tools/builtin/research_tool.go`（新建）

| 机制 | 实现 |
|---|---|
| 依赖注入 | **窄接口 `ResearchProfileClient` 定义在 builtin 包内**（`GetResearchProfile(ctx, ticker)`），由 `*storage.PostgresStore` 隐式满足 + `var _ ResearchProfileClient = (*storage.PostgresStore)(nil)` 编译期断言 —— 避免 `pkg/storage` 反向依赖 `pkg/tools`（沿用 `gene_pool_tools.go` / `market_regime_tool.go` 先例）；构造器 nil client 直接 panic |
| 来源解析 | `GetResearchProfile` → 有行 ⇒ `fromProjection`（`Source:"postgres"`）；无行 ⇒ `loadMirror` → 有文件 ⇒ `fromMirror`（`Source:"vault"`）；两者皆无 ⇒ `ErrNotFound`（映射 404） |
| ticker 归一化 | `600519` / `600519.SH\|SZ\|BJ` / `sh600519` → 裸 6 位码；后缀集**闭集**，未知后缀（如 `.HK`）→ `ErrInvalidArgs`（静默丢后缀会给港股请求返回 A 股档案） |
| `sections` | 缺省/空 ⇒ 两段都返回；未知段名 → `ErrInvalidArgs` |
| `schema_version` | `!= 1` → **拒绝服务**（报错）而非尽力解析 —— 契约 C2 冻结语义 |
| `stale` 判定 | 三条触发：① `generated_at` 零值；② `source_mtime > generated_at`（markdown 在镜像生成后被编辑）；③ 超 `DefaultProfileMaxAge`（90 天 = 季度研究节奏）。命中即置 `stale=true` + `stale_reason` 可读原因 |
| vault 镜像校验 | 文件内 `ticker` 归一化后**必须等于目录 ticker**，否则「wrong directory」报错（防复制粘贴串档）；文件存在但解码失败 → 报错（**不静默当不存在**，否则掩盖同步故障）；无 `vault_path` 配置或文件不存在 → `(nil, nil)`（未研究是正常态） |
| citations 保真 | `normalizeCitations` 只把 `null` / 空映射为 `[]`，其余**逐字节透传**；工具不重写、不补全、不生成新论断 |

`Parameters` = `ticker`（required）+ `sections`；`OutputSchema` 12 字段（含 `source` / `stale` / `stale_reason`，让 Hermes 能区分「档案来自哪一侧」与「是否可信」）。

### 3. HTTP 层

- `pkg/tools/errors.go` 新增第 5 个哨兵 `ErrNotFound`。
- `cmd/analysis/handlers_tools.go` 的 `respondToolError` 新增映射：`ErrNotFound` → **404 `NOT_FOUND`**。
- 「有档案」与「没档案」由此在 HTTP 语义上分离：前者 200 + 内容，后者 404 —— **不是 200 + 空对象**（空对象会让 Hermes 把「没研究过」误读成「研究过但没有结论」）。

### 4. 注册与点号工具名的路由验证

- `buildToolsRegistry` 签名 7 参 → **8 参**（新增 `researchProfile builtin.ResearchProfileClient`），调用方 `cmd/analysis/main.go` 传入 `store`。
- 新增 **Group 10（Research archive）**，注册 `research.profile`，vault 路径取 `v.GetString("equitydeep.vault_path")`。
- `research.profile` 是全仓**第一个经 HTTP 层实测的含点工具名**。gin 的 `/:name` 只按 `/` 切分，点号不会截断参数 —— 本记录以 `TestIntegration_DottedToolName_RoutesThroughGin` **实测固化**该行为（4 个子测：execute 到达工具并完成归一化、discovery 返回点号工具 schema、no_profile→404、malformed ticker→400），而非假设。

### 5. 文档同步口径（重要）

工具计数断言分两类处理：

| 类型 | 处理 | 位置 |
|---|---|---|
| **描述性现状文档**（声称「当前有 N 个工具」） | **同步为 19** | `ARCHITECTURE.md`（§Tools Registry 工具清单 + 前端弃用注 + 尾注）/ `AGENTS.md`（目录树 + ODR-046 注 + 状态区）/ `SPEC.md`（内置工具总述 / MCP 工具镜像 / 注册测试）/ `VISION.md` / `docs/hermes/config/hermes.yaml` / `pkg/ai/agents/doc.go` |
| **历史决策记录**（某次决策时点的快照与指标） | **保留原文，不回写** | [ODR-046](odr-046-hermes-agent-integration-decision.md)（`18 (across 9 groups)`）/ `adr-015-ai-agent-architecture.md`（`MCP bridge (18 tools)`） |

理由：ODR/ADR 是**带日期的决策快照**，其指标行的含义是「决策当时确有 18 个」，改写成 19 会抹掉「工具是后来增长的」这一事实，并使后续审计无法解释历史数字。**新增工具用新的 ODR 记录，而不是改写旧 ODR** —— 本记录即该事实的权威出处。

---

## Consequences

### 正面

- **飞轮第 1 步有了入口** —— 此前 Hermes 只能看见行情、因子与基因池，看不到「某家公司的哪份档案、哪条疑点」；`research.profile` 是飞轮从「读回研究档案」启动的那一环。
- **`research` schema 从「有表无链」变为「有表有链」** —— 与 [ODR-055](odr-055-eqd-p1-2-vertical-factors.md) 中 `fundamentals_detail` 的情形同类：**表落地只完成契约的一半**。
- **零新架构** —— 只是 `pkg/tools/builtin/` 的第 19 个工具，自动出现在 `GET /api/tools` 供 Hermes 发现（延续 [ODR-046](odr-046-hermes-agent-integration-decision.md) 的 MCP 桥模式）；零新端点、零 DDL、零迁移。
- **依赖方向未被污染** —— 窄接口定义在 builtin 包内，`pkg/storage` 仍不依赖 `pkg/tools`。
- **「不猜」延续到档案读取** —— 未知交易所后缀拒绝、`schema_version` 不匹配拒绝、镜像串档拒绝、无档案 404；没有一处「尽力猜一下」的分支。
- **临时双源有明确拆除路径** —— `EQD-P2-1`（EquityDeep 接 PG）落地后只需删除 vault 回退分支，来源解析与输出整形不必重写。

### 负面 / 代价

- **vault 回退是临时双源** —— 在 `EQD-P2-1` 落地前，工具可能读到**用户可手改的 markdown 导出**而非 PG 投影；`source` 字段把这一事实显式暴露给调用方，但**双源本身就是待偿的债**。
- **`generated_at` 用 `updated_at` 代理** —— `research.profile` 表无 `generated_at` 列（契约 C2 有），投影路径以 `updated_at` 充当。语义不完全等同：`updated_at` 是「行最后一次被写」，`generated_at` 是「镜像生成时刻」。
- **投影路径的 DB 查询未端到端验证** —— Docker 未运行，`GetResearchProfile` 的真实 SQL 路径只经 `&PostgresStore{}` 空 store 的参数校验与 fixture 测试覆盖（DB-backed 用例 SKIP）。
- **含点工具名依赖框架行为** —— `/:name` 只按 `/` 切分是 gin 的实现细节而非契约；已用集成测试固化，但换框架/换路由写法会静默失效。
- **工具数断言散落 8 处文档** —— 本次逐一同步；`ARCHITECTURE.md` §Tools Registry 已写明「新增工具需同步更新此列表」，仍靠人工维护。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 回退源（vault）与投影源在同一标的上**同时存在且内容不一致** | 中 | 优先级写死为「投影优先」并输出 `source` 字段；`EQD-P2-1` 落地后删除回退分支 |
| `updated_at` 代理 `generated_at` 导致 stale 判定偏松/偏紧 | 低 | 90 天阈值对季度研究节奏足够宽松；`stale_reason` 输出可读原因供人工判断 |
| 契约 C2 未来升版（`schema_version` 2）时工具直接报错 | 低 | 这是**有意行为**：宁可拒绝服务也不按 v1 结构解析 v2 数据；升版须先改本工具 |
| 含点工具名在其他消费者（非 gin 路由）上被截断 | 低 | 已实测固化 gin 行为并留集成测试；`Name()` 本身的 dot namespace 与 `/api/tools/{name}` 同构 |
| 工具数断言漏改 | 中 | 本次全仓 grep 定位 8 处并逐项判定「同步 / 不回写（并说明理由）」 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `pkg/storage/research.go` | `ResearchProfile` / `ResearchConclusion` / `ResearchQuestion` + `GetResearchProfile`（`ErrEmptyResearchTicker`；无行 → `(nil, nil)`；子表空 → 空切片；citations `json.RawMessage` 透传） |
| **新建** | `pkg/tools/builtin/research_tool.go` | `ResearchProfileTool` + 窄接口 `ResearchProfileClient` + `normalizeTicker` / `parseSections` / `checkSchemaVersion` / `profileStaleness` / `loadMirror` / `normalizeCitations`；`Parameters`（ticker + sections）+ `OutputSchema`（12 字段） |
| **新建** | `pkg/tools/builtin/research_tool_test.go` | 18 个 `TestXxx`（15 个子测组）：参数校验 / 归一化 / 投影优先 / vault 回退 / no_profile / store 错误 / sections / 空档案 / schema_version 拒绝 / staleness / 串档 / 畸形镜像 / citations 保真 |
| **新建** | `pkg/storage/research_test.go` | 5 个 `TestXxx`：ticker 必填 / 往返（排序 + JSONB 保真 + superseded/closed 不过滤）/ 未知 ticker `(nil, nil)` / 无子行空切片 / citations 为空 |
| **修改** | `pkg/tools/errors.go` | 新增哨兵 `ErrNotFound` |
| **修改** | `cmd/analysis/handlers_tools.go` | `respondToolError` 新增 `ErrNotFound` → 404 `NOT_FOUND` |
| **修改** | `cmd/analysis/setup.go` | `buildToolsRegistry` 7 参 → 8 参；新增 Group 10 注册 `research.profile`；工具数注释与启动日志 18 → 19 |
| **修改** | `cmd/analysis/main.go` | 调用方传入 `store` |
| **修改** | `cmd/analysis/setup_test.go` | `RegistersAll18Tools` → `RegistersAll19Tools`；`assert.Len(19)`；`expectedTools` 增 `research.profile`；新增 `stubResearchProfile` |
| **修改** | `cmd/analysis/handlers_tools_integration_test.go` | 新增 `TestIntegration_DottedToolName_RoutesThroughGin`（点号工具名经 gin 路由 + 404/400 映射，4 子测） |
| **修改** | `docs/hermes/tools-quant-backtest.yaml` | 工具清单 17 → 19（头部注释 + Phase 注释）+ 新增 Group 10 `research.profile` 条目 |
| **修改** | `docs/hermes/e2e-acceptance-test.md` | 5 处 18 → 19（验收条件/基建表/期望/注释/python assert） |
| **修改** | `docs/RESEARCH.md` | §3.5 标注已落地（实现要点 ↔ 实际实现的差异）；§3.7 C-5 行与「落地状态」注同步 |
| **修改** | `docs/ARCHITECTURE.md` | §Tools Registry 工具清单 18 → 19（新增 `research.profile`）；前端弃用注与尾注同步 |
| **修改** | `AGENTS.md` | 目录树 `18 内置工具` → 19；ODR-046 注 `18 个工具` → 19；状态区 `18 个 MCP 工具` → 19；`docs/odr/` 范围 `ODR-001~056` → `~057` |
| **修改** | `docs/SPEC.md` | 内置工具总述（16 → 19，修正旧漂移）/ MCP 工具镜像 18 → 19 / 注册测试 18 → 19 |
| **修改** | `docs/VISION.md` | 2 处 18 → 19（「18 个工具」「18 MCP tools」） |
| **修改** | `docs/hermes/config/hermes.yaml` | MCP Tool Bridge 注释 18 → 19 |
| **修改** | `pkg/ai/agents/doc.go` | 包注释 `MCP bridge (18 tools)` → 19 |
| **修改** | `docs/TASKS.md` | `EQD-P2-1` → ✅；阶段 P4 进展注；版本 3.31.0 → 3.32.0；统计与变更日志 |
| **修改** | `docs/ADR.md` | ODR 索引新增 ODR-057；尾注 ODR 56 → 57、Implementation 32 → 33；index 3.12.0 → 3.13.0 |
| **新建** | `docs/odr/odr-057-eqd-p2-1-research-profile-tool.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 提交 | 1 个 atomic commit（代码 + 测试 + 文档 + 本记录同批，hash 见 `git log`） |
| 新建文件 | 4（2 源码 + 2 测试）+ 本 ODR |
| 修改文件 | 13（源码/测试 6 + 文档 7） |
| 工具数 | 18 → **19**（分组 9 → 10） |
| 新增端点 | 0（复用 `GET/POST /api/tools/{name}`） |
| 新增表 / 迁移 | 0 / 0（**首次消费** ODR-050 落地的 `research` schema） |
| 新增哨兵错误 | 1（`ErrNotFound` → 404 `NOT_FOUND`） |
| 新增测试 | 23 个 `TestXxx`（`research_tool_test.go` 18 + `research_test.go` 5）+ 1 个集成测试（4 子测） |
| `go build ./...` | 通过（exit 0） |
| `go test ./pkg/tools/... ./pkg/storage/... ./cmd/analysis/...` | 通过（DB-backed 用例按约定 SKIP） |
| `go vet` / `gofmt` | 通过（`gofmt -l` 对 CRLF 文件报错，经 CRLF→LF 副本判定为行尾误报） |
| 任务增减 | 0（EQD-P2-1 由 ⬜ → ✅，总计 233 不变） |
| 阶段 P4 进度 | **1/2**（EQD-P2-1 ✅；余 P4-1 飞轮闭环端到端） |
| 文档回写 | 描述性现状 8 处同步；历史决策记录 2 处保留原文（口径见 §5） |
| ADR 累计 | 22 |
| ODR 累计 | 57 |

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| 投影路径（`GetResearchProfile` 真实 SQL）端到端验证 | 需 Docker + PG 且 `research.*` 当前无写入方（写入方是阶段 P2 的 `EQD-P2-1`，在 EquityDeep 仓）；本次以 fixture + 空 store 参数校验覆盖 |
| 删除 vault 回退分支 | 需先有 PG 写入方（`EQD-P2-1`），否则工具会恒 404 —— 回退分支是**中转态的必要部分** |
| 端点鉴权 | 与 analysis 服务其余 `/api/tools/*` 同级（当前内网），须统一处理 |
| `_profile.json` 的 `contracts/profile.schema.json` 全量校验（jsonschema） | 本工具只校验 `schema_version` 与 `ticker` 一致性；契约校验器属 C-2/抽检范畴 |
| C-6 vault 只读挂载（`EQD-P2-2`） | 独立任务（阶段 P2，需容器化）；本工具只读路径，无挂载假定 |
| P4-1 飞轮闭环端到端 | 需 EquityDeep 侧配合，独立任务 |
| 历史决策记录（ODR-046 / ADR-015）中的「18」 | 按 §5 口径保留原文，不重写历史 |

---

## Lessons Learned

1. **表存在 ≠ 有链，DDL 落地 ≠ 契约可用** —— `research` schema 自 [ODR-050](odr-050-p1-base-contract-landing.md) 起有结构、有索引、有契约，但**零读取方**，对任何消费者等价于不存在。[ODR-055](odr-055-eqd-p1-2-vertical-factors.md) 已在 `fundamentals_detail` 上踩过同一坑（写入方与读取方都是 0）；**任务拆分必须显式区分「表落地 / 写链落地 / 读链落地」三段**，否则会出现「表已 ✅、功能不可用」的假完成。
2. **窄接口定义在消费方一侧，才能不污染依赖方向** —— `pkg/storage` 不依赖 `pkg/tools`，若接口定义在工具侧就会形成反向依赖。**把接口放在使用它的包里、让实现方隐式满足**（再补一行编译期断言），是这条边界唯一不靠纪律维持的写法。
3. **「无数据」与「查失败」必须在类型层面分开** —— 主表无行返回 `(nil, nil)`、DB 故障返回 error、子表无行返回空切片。三者若统一为 `nil`，调用方只能靠猜；到了 HTTP 层就变成「没研究过」与「空档案」不可区分。
4. **契约的必填字段可能在本侧存储里没有对应列** —— 契约 C2 要求 `generated_at`，而 `research.profile` 只有 `updated_at`。**用近似列代理可以接受，但必须在 ODR 里写清语义差异**，否则下次审计会把它当成 bug 或当成等价物。
5. **临时双源要有明确的拆除路径，而不是「以后再说」** —— 「PG 优先 + vault 回退」之所以优于「仅 vault」，是因为 P2-1 落地后**删一个分支**即可收敛；反过来如果先只做 vault 并把 vault 当权威，后面就得重写来源解析。**选中转态时，评价标准是「到终态的迁移成本」，不是「当下的实现量」**。
6. **框架行为要实测固化，不要凭直觉假设** —— 「gin 的 `/:name` 能否捕获含点号的工具名」是一个会静默 404 的假设。`research.profile` 是全仓第一个含点工具名，**用集成测试把它钉住**，比在代码注释里写「应该可以」便宜得多。
7. **历史快照不该被回写成现状** —— ODR/ADR 里的「18 个工具」是**决策时点的指标**。全仓搜索会命中它们，但正确动作是**在新 ODR 里记录增长事实并说明不回写理由**，而不是把旧记录改成 19 —— 否则「工具数量何时增长、由谁引入」这条线索就从文档里消失了。

---

_本记录为 2026-09-15 阶段 P4 EQD-P2-1 落地操作，标志阶段 P4 首项（飞轮第 1 步：读回研究档案）打通。阶段 P4 剩余 P4-1 与阶段 P2/P5 的实施情况应记入新的 ODR，不在本文件追加。_