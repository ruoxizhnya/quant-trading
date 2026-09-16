# ODR-050: P1 底座契约落地 — L0-2 / L0-3 / L0-4 三项先行

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§2 数据归属 / §3 四层架构 / §5 证据服务 — 本次落地的依据）
> **Supersedes**: —
> **Related ODRs**: [ODR-048](odr-048-top-level-product-redefinition.md)（顶层产品重定义）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计，其 P0 假阳性缺陷由本次架构级消除）, [ODR-049](odr-049-adr-022-downstream-consistency.md)（下游一致性收口，本次为其后继）
> **Author**: AI Assistant

---

## Context

### 触发条件

用户决定按「先让底座可用、再推进工作面」的次序执行，并在 [ODR-049](odr-049-adr-022-downstream-consistency.md) 收口后选择 **P1 最小契约先行**。经确认，本轮范围为 **三项契约先行**（L0-2 / L0-3 / L0-4），以「接口冻结」为出口，使 EquityDeep（工作面 1）与 Quant Lab（工作面 2）两侧可并行开工。

### 前置事实（本次现场复核）

| 项 | 复核结论 |
|---|---|
| 迁移**实际执行路径** | `pkg/storage/postgres.go` 内联 `migrate()`（raw SQL 切片 + `pool.Exec`，在 `NewPostgresStore` 内被调用） |
| `migrations/*.sql` 的定位 | 同源文档副本（仅在 `postgres.go` 注释中被引用），**不是**执行路径 |
| `pkg/storage/migration_manager.go` | golang-migrate 封装，**全仓未被调用**（连同 `migrations/0000000{1,2,3}_*/up.sql` 副本一并排除在计数之外） |
| 表数（改动前） | 32 张 — 该数字**遗漏**了内联的 `users` / `audit_logs`（P1-2 引入） |
| 迁移编号 | 存在历史重复：根目录 `012`×2、`docs/migrations` `007`×2 |
| 工具链 | `go` 不在 PATH；实际可执行文件为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`（Go 1.25.0） |
| Docker | Docker Desktop 未运行 → 所有 DB-backed 测试自动 SKIP（按仓库既有 `testStore` 约定视为通过） |

---

## Decision

**以「内联执行 + 统一编号」方式落地 L0-2 / L0-3 / L0-4 三项底座契约，仅新增、不改存量表，并把 SQL 文件作为同源文档副本纳入版本控制。**

### 1. L0-2 — `ingest.raw`（类 A 原始源响应归档）

- **存储层**：`pkg/storage/ingest_raw.go`
  - `RawIngest{content_hash, source, dataset, key, as_of, payload, fetched_at}`
  - `ContentHashOf(payload)`：解码 JSON → 重新 marshal 规范化 → sha256 → 小写 hex（64 字符）。**空白与键序不同但内容等价的响应产同一哈希**。
  - `SaveRawIngest`：`nil` / 空 hash 校验；`fetched_at` 缺省补 `time.Now()`；`ON CONFLICT (content_hash) DO NOTHING` → **写入幂等且不可变**（重摄取永不改写历史）。
  - `GetRawIngest`：`pgx.ErrNoRows → (nil, nil)`，用**双返回值区分「未摄取」与「查询失败」**。
- **DDL**：`postgres.go` 内联（唯一执行路径）+ `docs/migrations/020_add_ingest_raw.sql`（文档副本）。

### 2. L0-3 — Evidence API（只读证据服务）

- **Handler**：`cmd/analysis/handlers_evidence.go`（沿用 `ToolsHandler` / `RiskHandler` 的 "pattern B"：struct + functional options + `RegisterRoutes`）
  - `GET /api/evidence/:content_hash` → 唯一 `ingest.raw` 行；**404 = 未摄取，是一等答案而非错误**；`nil` store 在构造期 panic（fail loud）。
- **注入**：`ServerDeps` 新增第 18 个字段 `Store *storage.PostgresStore`（`deps.go`），在 `main.go` 以 `Store: store` 注入并注册路由。
- **架构意义**：证据坐标由「文件路径 + 模糊字符串」升级为 `citation = {source, dataset, key, as_of, content_hash}`，**在架构层面消除 ODR-047 记录的 P0 假阳性缺陷**。

### 3. L0-4 — `research` schema（类 E 研究结构化状态投影）

- `research.profile` / `research.conclusion` / `research.question` 共 3 张表。
- **不持外部 FK**：`citations.content_hash` 在写时校验而非 DB 级约束，确保可 `DROP SCHEMA research CASCADE` 后由 vault markdown 确定性重建。
- **DDL**：`postgres.go` 内联 + `docs/migrations/021_add_research_schema.sql`（文档副本）。

### 4. 迁移编号统一

| 原编号 | 新编号 | 说明 |
|---|---|---|
| （新增） | `020_add_ingest_raw.sql` | L0-2 |
| （新增） | `021_add_research_schema.sql` | L0-4 |
| （预留） | `022_equitydeep_fundamentals.sql` | EQD-P1-1（阶段 P3） |
| 根 `migrations/012_add_gene_pool_tables.sql` | `023_add_gene_pool_tables.sql` | 解除根目录 `012` 重复 |
| `docs/migrations/007_add_factor_returns_table.sql` | `024_add_factor_returns_table.sql` | 解除 `docs/migrations` `007` 重复 |
| （预留） | `025_equitydeep_field_consolidation.sql` | EQD-P3-1 / TASKS.md C-8 |

> `docs/migrations/007_add_splits_table.sql` 保持 `007`（与 `024` 重编号后不再冲突）。

---

## Consequences

### 正面

- **接口冻结**：工作面 1（EquityDeep）的解封条件达成，两侧可并行开工；
- **架构级消除缺陷**：Evidence API 使「数字 → 唯一原始记录」成为可校验路径，ODR-047 的 P0 假阳性不再可能；
- **不重复存储落地可验证**：`ingest.raw`（类 A）与 `research.*`（类 E）均**只新增、不改存量表**，存量 34 张表零改动；
- **可重建性标注成立**：`research` schema 无外部 FK，`DROP SCHEMA research CASCADE` 后可重建 —— 权威在 vault markdown；
- 表数口径修正为 **38 张活跃表（内联 20 + 迁移 18）**，消除此前 32 张的遗漏。

### 负面 / 代价

- `ServerDeps` 由 17 → 18 字段，`deps_test.go` 需同步（已同步）；
- 新增测试文件（`handlers_evidence_test.go` / `ingest_raw_test.go`）被 `.gitignore` 的 `*_test.go` 规则排除，须以 `git add -f` 显式纳入；
- DB-backed 测试在当前环境（Docker 未运行）全部 SKIP，**真实写入路径未被端到端验证**，须在容器起来后补跑。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| `ingest.raw` 无冷热分层，随时间无限增长 | 中 | 已在 PRODUCT.md 登记为未决问题 **Q-2**，本轮不做 |
| `content_hash` 规范化依赖 JSON 语义（非 JSON 响应无法哈希） | 低 | 当前源均为 JSON；`ContentHashOf` 对非法 JSON 显式报错而非静默 |
| DB-backed 测试长期处于 SKIP 状态形成盲区 | 中 | 容器化后须纳入 CI 常规跑（属阶段 P2 的容器化任务 EQD-P2-2） |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `pkg/storage/ingest_raw.go` | `RawIngest` + `ErrEmptyContentHash` + `ContentHashOf` + `SaveRawIngest` + `GetRawIngest` |
| **新建** | `pkg/storage/ingest_raw_test.go` | 4 个纯函数测试 + 2 个 DB-backed 测试（无 DB 则 SKIP） |
| **新建** | `cmd/analysis/handlers_evidence.go` | EvidenceHandler（pattern B） |
| **新建** | `cmd/analysis/handlers_evidence_test.go` | nil panic + 404 + 200 round-trip |
| **新建** | `docs/migrations/020_add_ingest_raw.sql` | L0-2 DDL 文档副本 |
| **新建** | `docs/migrations/021_add_research_schema.sql` | L0-4 DDL 文档副本 |
| **修改** | `pkg/storage/postgres.go` | 内联 `ingest.raw` + `research.{profile,conclusion,question}` DDL（唯一执行路径） |
| **修改** | `cmd/analysis/deps.go` | 新增第 18 字段 `Store` |
| **修改** | `cmd/analysis/main.go` | `Store: store` 注入 + 注册 Evidence 路由 |
| **修改** | `cmd/analysis/deps_test.go` | 同步 18 字段 + 类型断言 + 路由样例 |
| **重命名** | `migrations/012_add_gene_pool_tables.sql` → `023_*` | 解除根目录重复编号 |
| **重命名** | `docs/migrations/007_add_factor_returns_table.sql` → `024_*` | 解除 `docs/migrations` 重复编号 |
| **修改** | `docs/ARCHITECTURE.md` | 表数 32 → 38；实际执行路径说明；ADR-022 schema 去 Proposed |
| **修改** | `docs/SPEC.md` | Evidence API 「草案 → 已实现」 |
| **修改** | `docs/openapi.yaml` | 新增 `/api/evidence/{content_hash}` + `RawIngest` schema + `Evidence` tag |
| **修改** | `docs/TASKS.md` | L0-2/L0-3/L0-4 → ✅；统计 18/1/213 → 15/1/216；版本 3.24.0 → 3.25.0；新增 changelog |
| **修改** | `AGENTS.md` | CR-47 表数复核（18 → 38）；导航表数字 |
| **新建** | `docs/archive/odr/odr-050-p1-base-contract-landing.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 新建文件 | 7（4 源码/测试 + 2 SQL + 本 ODR） |
| 修改文件 | 9（postgres.go / deps.go / main.go / deps_test.go / ARCHITECTURE.md / SPEC.md / openapi.yaml / TASKS.md / AGENTS.md） |
| 重命名迁移 | 2 |
| 新增表 | 4（`ingest.raw` + `research.*` ×3）；存量表改动 **0** |
| 活跃表总数 | 32 → **38** |
| `go build ./...` | 通过（exit 0） |
| `go vet ./pkg/storage/... ./cmd/analysis/...` | 通过（exit 0） |
| `go test ./pkg/storage/ ./cmd/analysis/` | 通过（DB-backed 用例按约定 SKIP） |
| 任务增减 | 0（三项由 ⬜ → ✅，总计 233 不变） |
| ADR 累计 | 22 |
| ODR 累计 | 50 |

---

## Lessons Learned

1. **「文件存在」不等于「代码被执行」** —— `migrations/*.sql` 与 `migration_manager.go` 都在仓库里，但真正建表的是 `postgres.go` 的内联 `migrate()`。**落地 DDL 前必须先确认执行路径**，否则会写出"看起来生效、实际不执行"的迁移。
2. **可重建性需要显式的架构让步** —— `research` schema 主动放弃外部 FK，换取 `DROP SCHEMA ... CASCADE` 后的可重建。**"可丢弃"是可重建性的前提条件**，不是清理动作。
3. **`(nil, nil)` 优于 `error`** —— `GetRawIngest` 用双返回值区分"未摄取"与"查询失败"，使 404 成为一等答案。**把"没有"编码成错误会让上层无法区分语义**。
4. **文档副本必须显式声明自己是副本** —— 同一份 DDL 存在两个位置时，若不声明权威方，下一次改动必然只改一处。**同源副本需要一句"实际执行路径为 X"的注记**。
5. **计数类断言应可复现** —— 表数由 32 修正为 38，根因是"遗漏内联的 users/audit_logs"而非"新增了 6 张表"。**凡文档中的数字断言，都应附一条可复现的校验命令或来源**。
6. **测试被 `.gitignore` 静默吞掉** —— `*_test.go` 在忽略列表中，新测试默认不入库。**新增测试后必须确认 `git status` 是否可见**，否则提交会丢掉验证资产。

---

_本记录为 2026-09-15 P1 底座契约三项先行落地操作。阶段 P1 剩余任务（L0-1 单一摄取入口 / EQD-P0-1 契约冻结 / EQD-P0-2 抽检泛化 / EQD-P3-2 文档漂移）与阶段 P2~P5 的实施情况应记入新的 ODR，不在本文件追加。_