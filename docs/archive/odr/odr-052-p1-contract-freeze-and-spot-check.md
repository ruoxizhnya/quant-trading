# ODR-052: P1 底座契约收尾 — EQD-P0-1 契约冻结 + EQD-P0-2 抽检泛化

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§2 数据归属 / §3 四层架构「契约优先」/ §4 取数严格单一入口 — 本次落地的依据）
> **Supersedes**: —
> **Related ODRs**: [ODR-051](odr-051-l0-1-single-ingest-entry.md)（本记录承接其末行明确列出的 EQD-P0-1 / EQD-P0-2）, [ODR-050](odr-050-p1-base-contract-landing.md)（P1 三项先行）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计 — C-2 / C-7 来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-051](odr-051-l0-1-single-ingest-entry.md) 落地 L0-1 后，阶段 P1「底座契约」的 L0-1 ~ L0-4 四项全部冻结，仅剩 **EQD-P0-1（契约冻结）** 与 **EQD-P0-2（抽检泛化）** 两项。这两项是 P1 出口判据的另一半：

- L0-1~L0-4 解决的是「**数据面存在**」——表在、写入口在、证据 API 在。
- EQD-P0-1 / EQD-P0-2 解决的是「**契约可裁决 + 质量可度量**」——否则 RESEARCH §3.2/§3.3 的 schema 只存在于文档里，M1 的「10 票 × 20 数字、错误率 < 2%」硬门槛没有任何执行器。

### 前置事实（本次现场复核，逐条实证）

| 项 | 复核结论 |
|---|---|
| `contracts/` 目录 | **不存在**（RESEARCH §3.1 架构图已列出该目录及三个文件名，但从未落盘） |
| `evals/` 目录 | **不存在** |
| 全仓 Python 代码 | **0 个**（`Glob **/*.py` → No file found）；`tools/` 仅有 `warmcache_bench.go` |
| Python 工具链 | `python` → 3.12.10；PyYAML **6.0.3 已安装**（`yaml` 可 import） |
| RESEARCH §3.1 列出的契约文件 | `contracts/field_dictionary.yaml`、`contracts/snapshot.schema.json`、`contracts/profile.schema.json`（**3 个**） |
| RESEARCH §3.2 的表 DDL | `fundamentals_detail` 逐字段行存 + `ann_date` PIT + `snapshot_uri` 溯源 —— **未在 §3.1 图中作为文件列出** |
| `fundamentals_detail` 三处同源 | `pkg/storage/postgres.go` 内联 `migrate()`（**实际执行路径**）/ `docs/migrations/022_equitydeep_fundamentals.sql`（预留）/ `contracts/*.schema.sql`（本次新增） |
| 现存基本面表口径 | `market.fundamentals` 为**宽表**（PE/PB/PS/ROE/ROA/GrossMargin/NetMargin/Revenue/NetProfit/TotalAssets/TotalLiab），与契约 C1 的「逐字段行存 + PIT」**口径不同** → 不复用 |
| 抽检的数据来源约束 | ADR-022 §4 规定 L0 是取数**唯一入口**，抽检脚本若直连 tushare/akshare 即构成绕过 L0 的第二条取数路径 |
| `.gitignore` 影响面 | 第 29 行 `*_test.go` 仅忽略 Go 测试文件；本次新增的 `.py`/`.yaml`/`.json`/`.sql` **不受影响**，可直接 `git add` |

---

## Decision

**把 RESEARCH §3.2/§3.3 的 schema 从文档提升为版本控制内的可裁决契约（`contracts/`），并把 EquityDeep M1 的抽检硬门槛泛化为零依赖、不直连数据源的通用执行器（`evals/data_quality/`）。**

### 1. 契约冻结（EQD-P0-1 / C-2）

- **落盘位置**：新建 `contracts/`，共 **4 个文件**。
- **为什么是 4 个而非 §3.1 图列的 3 个**：C-2 的语义是「冻结**契约**」，不是「冻结三个文件名」。契约 C1 由两半构成 —— `field_dictionary.yaml` 定**列语义**（field_code ↔ raw_names ↔ unit），表 DDL 定**行结构与约束**（主键、PIT 列、溯源列、索引）。只冻结一半会让「三处同源、以契约副本为准」的裁决规则失去对象，故把 §3.2 的 DDL 一并作为 `fundamentals_detail.schema.sql` 冻结。
- **冻结语义（写入各文件头部，构成变更协议）**：
  - `field_code` **不可重命名、不可删除**（消费者与历史数据行按其定位）。
  - `raw_names` 是**白名单**；白名单外的原始字段名一律**丢弃并记 warn**，**禁止启发式翻译**。
  - 变更走兼容性矩阵：新增 `field_code` 属 **minor**，既有字段语义/单位变更属 **major**。
  - 每个文件携带 `version` + `status: frozen` + `frozen_at`。
- **裁决顺序**：表 DDL 三处同源时，**以 `contracts/` 为准**（该规则写入 `fundamentals_detail.schema.sql` 头部注释）。
- **单位采用显式换算表**：`unit_scale`（`CNY=1` / `CNY_thousand=1e3` / `CNY_10k=1e4` / `CNY_million=1e6` / `CNY_100m=1e8`），避免"元/万元/亿元"靠字段名猜测。
- **抽检默认集内置于契约**：`spot_check_defaults.tickers`（10 票）—— 抽检范围与字段字典同源，避免脚本硬编码标的。
- **`profile.schema.json` 的 citation 采用 ADR-022 §5 形式**（`content_hash` + `pointer`），并在 description 中显式声明 v1.0 的 `snapshots/<file>.json#/data/<字段>` 文件路径形式**已被取代** —— 防止旧形制回流。

### 2. 抽检泛化（EQD-P0-2 / C-7 / 桥 B3）

- **落盘位置**：新建 `evals/data_quality/spot_check.py`（本仓**第一个 Python 工件**）。
- **不直连数据源（关键设计决策）**：脚本消费的是「**读数转储**」（JSONL），字段与契约 C1 表行同构（`ts_code / end_date / ann_date / field_code / raw_field_name / value / unit / source`）。理由：ADR-022 §4 规定 L0 是取数唯一入口；门禁一旦自取数，就绕过了它要保护的那条通道，并成为第二个数据模型。该决策已写入模块 docstring，避免后续被"顺手改成直连"。
- **三项检查**：
  1. **白名单校验** —— `raw_field_name` 必须 ∈ 该 `field_code` 的 `raw_names`；否则记 `violation` 并**排除出比对**（不猜测、不计入错误率）。
  2. **显式单位换算** —— 经 `unit_scale` 显式换算到 `base_unit`；单位不在换算表内记 `violation`。
  3. **双源比对** —— 被检源 vs 参照源同 `(标的, 报告期, 字段)` 单元格，相对差 > `tolerance`（默认 1%）记 `mismatch`；参照源无该单元格记 `missing`。
- **判定与退出码**：`错误率 = mismatch / comparable`，`< 2%` → **PASS**(0)；否则 **FAIL**(1)；`comparable == 0` → **INCONCLUSIVE**(3)；用法/IO 错误 → **ERROR**(4)。
  - **`comparable == 0` 必须是 INCONCLUSIVE 而非 PASS** —— 空集上的 `rate < threshold` 恒真，是质量门禁最常见的假绿。
- **自检**：`--self-test` 用合成转储同时构造 **PASS 与 FAIL 两个场景**（3 处不一致 → PASS / 8 处 → FAIL），并断言白名单违约数、缺失数、可比数、单元格总数与判定。只测 PASS 分支的自检无法证明门禁会失败。
- **边界校验**：`--tickers` 强制 6 位数字校验。起因见 Lessons Learned #6 —— 未加引号的逗号列表会被 shell 解析并静默改变抽检范围。

---

## Consequences

### 正面

- **P1 出口判据达成**：契约进入版本控制且带冻结语义与裁决规则；质量门禁可在**零外部依赖**下独立运行（不依赖 EquityDeep M1 通过、不依赖 Docker、不依赖外部数据源）。
- **零 Go 侧改动、零 DDL、零存量表改动** —— 仅新增文件，活跃表数仍为 38。
- **M1 硬门槛首次拥有执行器**：EquityDeep M1 的「10 票 × 20 数字、错误率 < 2%」不再是文字承诺，而是可运行、可判定的脚本。
- **契约与脚本同源**：抽检脚本直接读 `contracts/field_dictionary.yaml`，字段白名单与默认抽检集只有一处定义。

### 负面 / 代价

- **表 DDL 出现第三份副本**（内联 `migrate()` / `docs/migrations/022_*.sql` / `contracts/*.schema.sql`）→ 漂移风险上升。本次以「三处同源声明 + `contracts/` 为准」的 header 契约缓解，但**未引入自动一致性校验**。
- **转储生产者缺位**：脚本已定义输入契约，但"谁产出转储"仍未定义 —— 真实数据路径**未被端到端验证**。
- **冻结先于治理**：`contracts/` 的 owner 与变更流程（RESEARCH Q-2）仍未决，却已声明 `status: frozen`。冻结的**执行**目前依赖本次写入的 header 协议，无强制机制。
- 脚本为 Python，而仓库主语言为 Go → 工具链分叉（Python 3.12 + PyYAML 是新增前置依赖）。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 三处 DDL 副本漂移，实际执行路径与契约副本不一致 | 中 | header 明确裁决顺序为 `contracts/`；后续可加一致性校验（属工程化，非契约变更） |
| 无真实转储生产者 → 门禁无法在真实数据上跑通，M1 仍不可判定 | **高** | 属阶段 P2（工作面 1 跑通）范围；EQD-P2-2 / P2-2 落地后即可产出转储 |
| `status: frozen` 无强制机制，变更可能绕过 header 协议 | 中 | 登记为 RESEARCH **Q-2**（owner 与变更流程），本轮不做 |
| 抽检脚本直连数据源的诱因仍在（"少一步转储"） | 中 | 已在 docstring 显式记录「有意不直连」，并说明理由来自 ADR-022 §4 |
| Python 工具链分叉（Go 仓库引入 Python 前置） | 低 | 零第三方依赖之外的仅 PyYAML；`.py` 不在 CI 门禁内（Q-4 未决） |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `contracts/field_dictionary.yaml` | 契约 C1-a：`version: 1` + `status: frozen` + `unit_scale`（5 值）+ 10 票默认抽检集 + **20 个 field_code**（含 `raw_names` 白名单 / `unit` / `statement`） |
| **新建** | `contracts/fundamentals_detail.schema.sql` | 契约 C1-b：表 DDL + `idx_fund_detail_lookup`；三处同源声明与裁决顺序 |
| **新建** | `contracts/snapshot.schema.json` | JSON Schema (2020-12)：`ingest.raw` payload 快照结构；`ann_date` 必填（缺则引入 look-ahead bias） |
| **新建** | `contracts/profile.schema.json` | JSON Schema (2020-12)：`_profile.json` 档案镜像；citation 采用 ADR-022 §5 的 `content_hash` + `pointer` |
| **新建** | `evals/data_quality/spot_check.py` | 抽检泛化脚本：三项检查 + 四种退出码 + `--self-test` + 6 位标的校验 |
| **修改** | `docs/TASKS.md` | EQD-P0-1 / EQD-P0-2：⬜ → ✅；统计 14/1/217 → 12/1/219（Sprint 8 内 12/1/4 → 10/1/6）；版本 3.26.0 → 3.27.0；新增 changelog |
| **修改** | `docs/ADR.md` | ODR 索引 51 → 52；ODR 累计 51 → 52、Implementation 28 → 29；版本 3.8.0 → 3.9.0 |
| **新建** | `docs/archive/odr/odr-052-p1-contract-freeze-and-spot-check.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 新建文件 | 6（4 契约 + 1 脚本 + 本 ODR） |
| 修改文件 | 2（TASKS.md / ADR.md） |
| 新增表 | 0；存量表改动 **0**（活跃表数仍 38） |
| 新增端点 | 0 |
| 契约文件 | 4（`field_dictionary.yaml` / `fundamentals_detail.schema.sql` / `snapshot.schema.json` / `profile.schema.json`） |
| 字段字典 | 20 个 `field_code`；`unit_scale` 5 值；默认抽检集 10 票 |
| `python -m py_compile` | 通过（exit 0） |
| `spot_check.py --self-test` | **SELF-TEST OK**（exit 0）—— 白名单校验 / 显式单位换算 / 双源比对 / 缺失与违约隔离 / PASS 与 FAIL 判定 |
| 契约可解析性 | `field_dictionary.yaml`（20 字段 / 10 票 / 5 单位）+ 2 个 JSON Schema 全部解析通过 |
| 真实转储端到端 | 合成 2 票 × 2 字段转储 → 单元格 4 / 可比 3 / 一致 2 / 不一致 1 / 缺失 1 → FAIL(exit 1)，与预期一致 |
| `go build ./...` | 未跑（本次零 Go 改动） |
| 任务增减 | 0（EQD-P0-1 / EQD-P0-2 由 ⬜ → ✅，总计 233 不变） |
| ADR 累计 | 22 |
| ODR 累计 | 52 |

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| `EQD-P3-2` 文档漂移 8 项收口校验 | 阶段 P1 第三项，仍为 🔵 进行中；DR-1~DR-6 + DR-8 已于审计中完成，DR-7 由阶段 P3 的 EQD-P3-1 承接 |
| `EQD-P0-3` EquityDeep 回查脚本 P0 假阳性缺陷修复 | 属阶段 **P2**（TASKS.md 阶段映射明确），非 P1 尾巴 |
| 真实「读数转储」生产者 | 依赖工作面 1 跑通（阶段 P2）；本轮只定义输入契约 |
| `contracts/` owner 与变更流程 | 登记为 RESEARCH **Q-2**，本轮冻结先于治理 |
| 三处 DDL 副本的自动一致性校验 | 工程化增强，非契约变更 |
| EquityDeep CI 纳入抽检脚本 | 登记为 RESEARCH **Q-4**，未决 |
| `source.Registry` 完整重接 | 沿用 ODR-051 结论，属多 Sprint 工程 |

---

## Lessons Learned

1. **契约冻结要冻结「可执行的那份」，并写明裁决顺序** —— 契约 C1 的 DDL 存在三处同源（内联 `migrate()` / `docs/migrations/` / `contracts/`）。若只冻结字段字典而不冻结 DDL，"以契约为准"就失去了对象。**冻结必须覆盖分歧可能发生的每一层，并显式声明谁优先**。
2. **白名单的价值在于「拒绝猜测」** —— 白名单外的原始字段名一律**丢弃 + 记 warn**，而不是"尽力翻译"。翻译错误会以"数据正确"的形态进入研究结论，比缺数据危险得多。
3. **质量门禁不要直连数据源** —— 抽检脚本若自己取数，就绕过了它要保护的那条通道（ADR-022 §4 的 L0 唯一入口），并成为第二个数据模型。**门禁的输入应当是被检对象的输出，而非它自己的旁路**。
4. **无有效比对必须是 INCONCLUSIVE，不能是 PASS** —— `comparable == 0` 时 `rate < threshold` 恒真。缺失数据被静默判为"通过"，是质量门禁最典型的假绿。
5. **自检要断言「检查确实会失败」** —— 只验证 PASS 分支的自检无法证明门禁有效。本次自检同时构造 PASS（3 处不一致）与 FAIL（8 处不一致）两个场景，并断言各状态计数，才算证明门禁会拦截。
6. **边界输入必须做格式校验 —— 静默的范围改变比报错危险** —— 实测发现：PowerShell 中未加引号的 `--tickers 600519,000858` 会被解析为数组、`000858` 按数字取值 → 传给脚本的是 `600519,858`。后果**不是报错**，而是抽检集静默从 2 票缩为"2 票中只有 1 票有数据"，仍输出一份看似合理的 FAIL 报告。**当参数错误会改变统计口径而非触发失败时，必须在入口做格式断言**（本次补 6 位数字校验）。

---

_本记录为 2026-09-15 P1 底座契约 EQD-P0-1 / EQD-P0-2 落地操作。至此阶段 P1 仅余 EQD-P3-2（文档漂移收口校验，🔵）。阶段 P2 起的实施情况（含 EQD-P0-3 回查脚本修复、转储生产者、contracts/ 治理）应记入新的 ODR，不在本文件追加。_