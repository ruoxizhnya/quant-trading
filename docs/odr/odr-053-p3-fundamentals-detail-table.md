# ODR-053: P3 计算面起步 — EQD-P1-1 `fundamentals_detail` 表落地 + 三副本一致性校验

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§2 数据归属「按数据性质分区」/ §3 四层架构「契约优先」/ §4 取数严格单一入口 — 本次落地的依据）
> **Supersedes**: —
> **Related ODRs**: [ODR-052](odr-052-p1-contract-freeze-and-spot-check.md)（契约冻结；本记录补齐其自认的「未引入自动一致性校验」缺口）, [ODR-050](odr-050-p1-base-contract-landing.md)（迁移编号统一与活跃表数口径）, [ODR-047](odr-047-equitydeep-integration-audit.md)（契约 C1 来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

阶段 P1「底座契约」出口后（ODR-052），按 ADR-022 执行路线应进入阶段 P2「工作面 1 跑通」。**本机勘察否定了 P2 两项的可执行性**：

| 阶段 P2 任务 | 目标 | 本机可执行性 |
|---|---|---|
| `EQD-P0-3` EquityDeep 回查脚本 P0 假阳性修复 | EquityDeep 仓（回查脚本） | ❌ **该仓不在本机**（`Workspace/` 下仅 `claudeer` / `quant-trading` / 两个职业病项目 / `gdpr-preferenc`） |
| `EQD-P2-2` vault 只读挂载 → worker 容器 | `docker-compose.override.yml` | ❌ 该文件**不存在**且被 [.gitignore](../../.gitignore) 第 78 行忽略；且需 EquityDeep 镜像 |

据此本轮转向**本机零外部依赖、且在关键路径上**的 `EQD-P1-1`（阶段 P3 计算面第一项）：契约 C1 已在 ODR-052 冻结，DDL 三处同源但**唯一实际执行路径尚未落地** —— 契约存在而表不存在。

### 前置事实（本次现场复核，逐条实证）

| 项 | 复核结论 |
|---|---|
| `fundamentals_detail` 三处同源 | ① `pkg/storage/postgres.go` 内联 `migrate()`（**唯一实际执行路径**）② `docs/migrations/022_equitydeep_fundamentals.sql`（编号已预留，文件此前不存在）③ `contracts/fundamentals_detail.schema.sql`（ODR-052 冻结） |
| 裁决规则 | 三处漂移时**以 `contracts/` 为准**（写入各副本 header） |
| ODR-052 自认缺口 | 「表 DDL 出现第三份副本 → 漂移风险上升……但**未引入自动一致性校验**」 |
| 迁移编号空间 | `020`（L0-2）/ `021`（L0-4）已落地；`022` 为 EQD-P1-1 预留；`025` 为 EQD-P3-1 预留 |
| 活跃表数口径 | 38 张（内联 20 + 迁移 18）；本次 +1 → **39**（内联 21） |
| 与存量表的关系 | `market.fundamentals` 为**宽表**（PE/PB/ROE…），与契约 C1 的「逐字段行存 + PIT」口径不同 → **不复用、不改动** |
| `.gitignore` 约束 | 第 29 行 `*_test.go` 被忽略 → **新建 Go 测试文件会被静默 untracked**，测试必须追加到已跟踪文件 |
| 行尾约定（实测） | Go 文件 CRLF（`postgres.go` 412 / `postgres_test.go` 631，无孤立 LF）；SQL 文件 LF-only（`022_*.sql` 33 / `contracts/*.schema.sql` 41）；仓内**无 `.gitattributes`** |
| `gofmt` 可用性 | 对上述 CRLF 文件会把**整个文件**报为需重写（gofmt 归一 CRLF→LF）→ **不可用作格式校验**；改用行尾计数验证一致性 |
| DB 可用性 | 本机 Docker/Postgres 未运行 → DB-backed 测试按 `testStore(t)` 约定自动 SKIP |

---

## Decision

**把契约 C1 从「已冻结」推进到「已生效」：在内联 `migrate()` 落地 `fundamentals_detail`，同步补出文档副本，并为三副本增加自动一致性校验 —— 使 ODR-052 声明的「以契约副本为准」首次拥有执行器。**

### 1. 表落地（EQD-P1-1 / 契约 C1 / RESEARCH §3.2）

- **落盘**：`pkg/storage/postgres.go` 内联 `migrate()` 追加 Migration 022（建表 + 索引两条语句）+ 新建 `docs/migrations/022_equitydeep_fundamentals.sql` 文档副本。
- **冻结语义（5 条，逐条落入 DDL）**：
  1. **主键 `(ts_code, end_date, ann_date, field_code, fetched_at)`** —— 含 `fetched_at` 以支持 **restatement**：同一报告期被先后公告/修正的读数并存，不互相覆盖。
  2. **`ann_date NOT NULL`** —— PIT（point-in-time）硬要求。因子计算必须能约束 `ann_date <= D`；缺此列即引入 look-ahead bias。
  3. **`raw_field_name` 保留** —— 与 `field_code` 并存，形成「规范字段码 ← 原始字段名」溯源链。
  4. **`snapshot_uri NOT NULL`** —— 反查唯一原始记录（`ingest.raw.content_hash` 或 vault 相对路径），与 Evidence API（L0-3）内容坐标对齐。
  5. **`source` 前缀 `equitydeep:`** —— 与存量宽表物理隔离，可精确圈定本表写入方。
- **`value NUMERIC(24,4)` 是唯一可空列** —— 「缺失读数」必须与「读数为 0」可区分；这是财务数据的常见陷阱（0 与 NULL 语义完全不同）。
- **仅新增**：一条 `CREATE TABLE IF NOT EXISTS` + 一条 `CREATE INDEX IF NOT EXISTS`，**不改存量表**（`market.fundamentals` 维持原样）。
- **索引 `idx_fund_detail_lookup (ts_code, field_code, end_date DESC)`** —— 服务因子计算的主查询形态：「取某标的某字段的最近一期读数」。

### 2. 三副本自动一致性校验（补齐 ODR-052 缺口）

- **落盘位置**：**追加**到已跟踪的 `pkg/storage/postgres_test.go`（因 `.gitignore` 第 29 行忽略 `*_test.go`，新建文件会被静默 untracked）。
- **三个测试**：
  1. `TestFundamentalsDetailSchema_ThreeCopiesAgree` —— 从三处分别提取建表 DDL 与索引 DDL，剥离注释并归一空白后**逐字比对**；不一致时报出「哪两份漂移 + 以契约副本为准并同时修正三处」。建表与索引**分别断言**（合并提取会把 Go 原始字符串字面量的边界混入比较）。
  2. `TestFundamentalsDetailSchema_FrozenSemantics` —— 在**契约副本**上钉住冻结语义：`ann_date` / `fetched_at` / `snapshot_uri` 为 `NOT NULL`、主键 5 列、`value` 可空（断言其带尾逗号，即未被追加约束）、索引列为 `(ts_code, field_code, end_date DESC)`。
  3. `TestFundamentalsDetailSchema_AppliedByMigrate` —— 查 `information_schema.columns` 验证**活库**中 `ann_date` 确为 `NOT NULL`（无 Docker 时按 `testStore` 约定 SKIP，属预期行为）。
- **比较口径**：只比 DDL 正文，**不比注释** —— 三处 header 各自承担不同说明职责（契约副本写裁决规则、文档副本写可重建性），强制注释一致会阻止必要说明。

---

## Consequences

### 正面

- **契约 C1 首次「已生效」**：`fundamentals_detail` 从文档进入实际执行路径，`go build ./...` exit 0，表结构由 `migrate()` 保证。
- **ODR-052 缺口闭合**：三副本漂移从「靠 header 协议自觉」升级为**测试可拦截**；这是 ODR-052 明确自认的遗留项。
- **零存量表改动、零端点新增**：仅 +1 张新表 +1 个索引；`market.fundamentals` 宽表不动，避免既有消费者回归。
- **PIT 对齐能力就位**：阶段 P3 的 EQD-P1-2（5 个纵向因子）与桥 B1 的 `ann_date <= D` 约束，此前无表可依，现已具备。

### 负面 / 代价

- **活跃表数口径变更**（38 → 39），需连带更新 ARCHITECTURE.md 与 TASKS.md 的历史统计 —— 表数口径已成为需要持续维护的文档资产（已第三次修正，见 ODR-050）。
- **测试寄居**：为规避 `.gitignore` 第 29 行，测试只能追加到 `postgres_test.go`（已增至 631 行），该文件正逐步承载与存储层无关的契约校验职责。
- **校验是「同源三副本」的局部解，非通用机制**：本轮只覆盖 `fundamentals_detail` 一处；其余 DDL（`ingest.raw` / `research.*`）若出现第二副本仍需重复实现。
- **`docs/migrations/022_*.sql` 仍是文档副本**：`migrate()` 才是执行路径，SQL 文件不参与任何运行时——「文件存在即已迁移」的错觉风险仍在。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 三副本未来再次漂移（本轮校验仅覆盖建表与索引正文，注释/顺序差异不拦） | 低 | 已由 `ThreeCopiesAgree` 拦截结构漂移；注释差异是刻意放行 |
| 表已建但**无写入方** —— 摄取命令属 EQD-P1-2，本轮未做 | 中 | `EQD-P1-2`（阶段 P3 第二项）；本轮只保证「表结构可用」，不宣称数据链路可用 |
| 与存量宽表 `market.fundamentals` 字段语义重叠未消解 | 中 | 已登记 `EQD-P3-1`（`025_*.sql`，DR-7），本轮不动 |
| 校验测试寄居 `postgres_test.go`，若未来该文件被拆分/重命名则失效 | 低 | 属工程化；拆文件时须同步迁移测试块 |
| 无 DB 环境下 `AppliedByMigrate` 长期 SKIP，活库验证被静默跳过 | 低 | 与其他存储层测试同一约定；CI 有 Docker 时会执行 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `docs/migrations/022_equitydeep_fundamentals.sql` | 文档副本（LF，33 行）：定位 / 可重建性 / 三处同源与裁决顺序 / 5 条冻结语义 + 表与索引 DDL |
| **修改** | `pkg/storage/postgres.go` | 内联 `migrate()` 追加 Migration 022（**实际执行路径**） |
| **修改** | `pkg/storage/postgres_test.go` | 追加三副本一致性测试块（3 个测试 + 5 个 helper：`readRepoFile` / `extractParenGroup` / `extractFundamentalsDetailTable` / `extractFundamentalsDetailIndex` / `normalizeDDL`） |
| **修改** | `docs/TASKS.md` | EQD-P1-1：⬜ → ✅；统计 12/1/219 → 11/1/220（Sprint 8 内 10/1/6 → 9/1/7）；版本 3.27.0 → 3.28.0 + changelog；迁移编号注「`022` 已落地」 |
| **修改** | `docs/ARCHITECTURE.md` | 活跃表数 38 → **39**（内联 20 → 21）；`docs/migrations/` 副本数 11 → 12；新增 ADR-022 计算面追加说明 |
| **修改** | `docs/ADR.md` | ODR 索引 52 → 53；ODR 累计 52 → 53、Implementation 29 → 30；版本 3.9.0 → 3.10.0 |
| **修改** | `AGENTS.md` | 表数口径 38 → 39（文档导航表 + CR-47 复核注）；§1 当前版本「P1 底座契约待启动」→ 已完成 / P3 起步；资料缺口表 `fundamentals_detail` 注「表已落地、摄取链路未通」 |
| **新建** | `docs/odr/odr-053-p3-fundamentals-detail-table.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 新建文件 | 2（文档副本 SQL + 本 ODR） |
| 修改文件 | 6（`postgres.go` / `postgres_test.go` / `TASKS.md` / `ARCHITECTURE.md` / `ADR.md` / `AGENTS.md`） |
| 新增表 | **1**（`fundamentals_detail`）；活跃表数 38 → **39**（内联 21 + 迁移 18） |
| 新增索引 | 1（`idx_fund_detail_lookup`） |
| 存量表改动 | **0** |
| 新增端点 | 0 |
| 新增测试 | 3（`ThreeCopiesAgree` / `FrozenSemantics` / `AppliedByMigrate`） |
| 测试结果 | `ThreeCopiesAgree` **PASS**；`FrozenSemantics` **PASS**；`AppliedByMigrate` **SKIP**（本机无 Postgres，按约定） |
| `go build ./...` | **exit 0** |
| `go test ./pkg/storage/...` | **ok**（无回归） |
| 行尾一致性 | Go 文件 CRLF（412 / 631，孤立 LF = 0）；SQL 文件 LF-only（33 / 41） |
| `gofmt -l` | **未作为校验依据**（CRLF 触发全文件误报，属仓内既有现象，非本次引入） |
| 任务增减 | 0（EQD-P1-1 由 ⬜ → ✅，总计 233 不变） |
| ADR 累计 | 22 |
| ODR 累计 | 53 |

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| `EQD-P1-2` EquityDeep 摄取命令 + 5 个纵向基本面因子（桥 B1） | 阶段 P3 第二项；本表已就位但**摄取链路未通** |
| `EQD-P3-1` `fundamentals` / `stock_fundamentals` 字段重叠收口（`025_*.sql`） | 阶段 P3 第三项（ODR-047 DR-7 承接） |
| `EQD-P3-2` 文档漂移 8 项一致性复核 | 阶段 P1 余项（🔵）；DR-1~DR-6 + DR-8 已由审计完成，DR-7 由 EQD-P3-1 承接 |
| `EQD-P0-3` 回查脚本 P0 假阳性修复 / `EQD-P2-2` vault 只读挂载 | 阶段 P2，**本机不可执行**（EquityDeep 仓不在本机 / 目标文件被 gitignore） |
| 真实「读数转储」生产者 | 依赖工作面 1 跑通（阶段 P2）；与 ODR-052 结论一致 |
| 通用化「多副本 DDL 校验」基础设施 | 本轮只覆盖 `fundamentals_detail` 一处；未抽象为跨表机制 |
| `contracts/` owner 与变更流程 | 仍为 RESEARCH **Q-2**，未决 |

---

## Lessons Learned

1. **「已冻结」不等于「已生效」——契约冻结必须追到唯一执行路径** —— 契约 C1 在 ODR-052 已是 `status: frozen`，但 `fundamentals_detail` 在 `migrate()` 里并不存在。**只冻结副本而不落地执行路径，等于契约在纸面上生效**。冻结与落地必须成对交付。
2. **「以 X 为准」是一句空话，除非有校验器** —— ODR-052 把裁决顺序写入三处 header 后即声明风险已「缓解」，但同时承认「未引入自动一致性校验」。**治理声明若不附带可执行的检查，其强度等于注释**。本轮把裁决规则翻译成 `ThreeCopiesAgree`，才真正闭合。
3. **提取器锚点必须在目标文件中唯一** —— 首跑 FAIL 的根因是提取索引时用了 `"CREATE INDEX IF NOT EXISTS "` 作起始标记：它在 `postgres.go` 中**首次出现于 Migration 020**，于是把 `ingest.raw` 到 `fundamentals_detail` 的整段都吞进了比较。**校验代码自身的定位逻辑也需要校验**；锚点应含唯一标识（索引名），而非依赖语句前缀。
4. **不要把多个 DDL 对象合并提取** —— 首次实现把「建表 + 索引」当一整段提取并延伸到索引收尾，结果把 Go 原始字符串字面量的边界（`` )`, ` ``）也纳入比较，而 SQL 文件对应位置是 `);` + 空行。**比较对象必须与语义对象一一对应**（建表 vs 索引分别断言）。
5. **`.gitignore` 会改变「测试放哪」这个决策** —— 第 29 行 `*_test.go` 使新建测试文件静默 untracked（`git status` 干净、测试却存在）；`ripgrep` 遵循 ignore 规则也因此找不到它们（须用 `Glob`）。**在引入「新建文件」的方案前，必须先确认该路径未被忽略**。
6. **`value` 必须可空 —— 缺失读数 ≠ 读数为 0** —— 财务数据中「未披露」与「披露为 0」语义相反（前者是信息缺失，后者是事实）。把 `value` 设为 `NOT NULL` 会强迫用 0 表示缺失，使后续因子计算无法区分，且这个错误**只在遇到真实缺失数据时才暴露**。冻结语义时已显式保留该可空性，并在测试中断言（尾逗号断言：若被追加约束则失败）。
7. **CRLF 仓库中 `gofmt -l` 不可作为校验手段** —— 实测 `gofmt` 会把 CRLF 归一为 LF，于是把整个文件（412 / 631 行）报为需重写。仓内既有文件同样如此（非本次引入），**误报会淹没真实格式问题**。改用行尾计数（CRLF 数 + 孤立 LF 数）确认「新写入内容未引入混合行尾」。

---

_本记录为 2026-09-15 阶段 P3 计算面 EQD-P1-1 落地操作。阶段 P2（EQD-P0-3 / EQD-P2-2 / P2-1 / P2-2）因需 EquityDeep 仓或 gitignore 例外，仍待具备条件后实施；阶段 P3 余项（EQD-P1-2 / EQD-P3-1）与 P4/P5 的实施情况应记入新的 ODR，不在本文件追加。_