# ODR-047: EquityDeep 集成审计 — 项目定位复核 + 纵向研究层引入

> **Status**: Accepted
> **Date**: 2026-09-15
> **Category**: Audit
> **Related ADRs**: [adr-021](../adr/adr-021-equitydeep-research-layer.md) (本审计产出的架构决策), [adr-015](../adr/adr-015-ai-agent-architecture.md) (AI Agent 架构), [adr-016](../adr/adr-016-multi-source-data-architecture.md) (多数据源)
> **Related ODRs**: [odr-043](odr-043-comprehensive-audit-2026-06-29.md) (上次综合审计), [odr-046](odr-046-hermes-agent-integration-decision.md) (Hermes Agent 集成)
> **Supersedes**: None

## Context

触发条件：用户提出一份**新的独立设计** —— **EquityDeep**（`docs/design/equitydeep/EquityDeep_Product_Specification.md` + `EquityDeep_Technical_Specification.md`，原位于 `docs/design/` 根，本次审计后迁入子目录，见 DR-8），并委托对 Quant Lab 做一次审查，回答四个问题：

1. Quant Lab 到底在做什么（研究对象、产出、边界）；
2. EquityDeep 想要什么；
3. 两者结合是否可行 —— Quant Lab 的短板在哪，新想法是「改变」还是「补充」；
4. 产出可落地的 proposal（Product + Tech Implementation），并**融入现有 docs 体系**。

**审计方法**（遵 AGENTS.md §8 审计前置动作）：文档优先。通读 `VISION.md` / `SPEC.md` / `ARCHITECTURE.md` / `ROADMAP.md` / `ADR.md` / `TASKS.md` / `tasks-phase-2.md` / `docs/design/index.md` / `docs/hermes/*`，以及 ADR-015、ODR-043、ODR-045、ODR-046 变更历史；仅在被文档明确指向时抽查代码（`pkg/ai/`、`pkg/tools/builtin/`）以消解歧义。

**审计范围**：C1 项目定位复核；C2 新设计可行性；C3 既有架构短板；C4 文档一致性（漂移检测）。

## Decision

### D1 — 结论：「补充」而非「改变」

Quant Lab 与 EquityDeep 的研究对象**正交**，可线性叠加，因此**不推翻、不重写 Quant Lab**：

| 维度 | Quant Lab | EquityDeep |
|---|---|---|
| 研究单位 | N 只股票 × 1 因子（横截面） | 1 只股票 × N 个季度（纵向） |
| 时间尺度 | 日频 / 分钟频 | 季度频（财报期） |
| 产出 | 交易信号 / 策略排序 | 研究档案 / 可审读报告 |
| 消费者 | 回测引擎（机器） | 人（1 小时审读） |

Quant Lab 定位为 **A 股横截面量化研究平台** —— 研究对象是**策略与因子**，单只公司在其中被抽象为「一行 OHLCV 时间序列 + 三个标量基本面（`pe`/`pb`/`roe`）」。这一定位**正确且不需要改变**。

### D2 — 识别出三个真实缺口（本审计的核心发现）

| # | 缺口 | 证据 |
|---|---|---|
| **G1** | **基本面深度不足** | `stock_fundamentals` 仅 `pe`/`pb`/`roe` 三个标量；所谓 value / quality 因子本质是「三个数的分位数」，无三表明细、无现金流质量、无杜邦分解 |
| **G2** | **「事实正确性」门禁缺失** | `VISION.md` Principle 7「Evidence Over Intuition」是口号而非机制；现有 L1-L4 门禁只校验**代码正确性**（语法 / IC / 回测 / 走查），不校验**数字是否真有出处** |
| **G3** | **叙事解释层缺失 / L5 审查悬空** | 回测出 Sharpe 0.8 时系统能给出 IC 却给不出「这家公司怎么了」；且 L5 人工审查 UI 已随 ODR-045 删除，审查需求无处落地 |

三个缺口**恰好落在 EquityDeep 的射程内**，构成整合的正当性基础。

### D3 — 架构层面：建立「双时间尺度层」（落 [ADR-021](../adr/adr-021-equitydeep-research-layer.md)）

**关键判断：两侧的根本差异（技术栈 / 数据源 / 方法论 / UI 消费者）不应被统一。** EquityDeep 的「文件系统即数据库」「固定 7-stage pipeline」是其**护城河本身**，Go 重写会同时摧毁数据主权、人机共写记忆与成本可预算性，并把尚未通过的 M1 数据质量门槛推迟数周。

因此 ADR-021 采取**契约式整合**：两个独立代码库之间只允许通过**显式声明的数据契约**（JSON Schema + 字段字典）通信，并建立三条桥：

| 桥 | 方向 | 内容 |
|---|---|---|
| **B1** 深财务 → 因子 | EquityDeep → Quant Lab | 派生指标进 `factor_cache`，把 quality 因子从「ROE > 15%」升级为可解释的多维质量因子 |
| **B2** 档案 → 研究上下文 | EquityDeep → Quant Lab | 新增 MCP 工具 `research.profile`（第 19 个），Hermes 可查档案结论 / 疑点 / `needs_review` |
| **B3** 数据质量门禁共享 | 双向 | M1 抽检方法（10 票 × 20 数字，错误率 < 2%）泛化为通用数据源质量门禁 |

被否决的方案：B（全量并入 Go 重写）、C（完全独立）、D（建为第 5 个微服务）—— 否决理由见 ADR-021 §Alternatives。

### D4 — EquityDeep 侧发现：1 项护城河级缺陷 + 5 项加固建议

对上游规格的技术审查发现，其「逐数溯源」机制的**实现与承诺不符**：

| # | 严重度 | 发现 |
|---|---|---|
| **#1** | 🔴 **P0（护城河缺陷）** | 回查脚本（Technical Spec §3.1）做**全局子串匹配** —— `" ".join(all json).find(repr_num)`。任何数字只要在快照**任意位置**出现过即算通过 → **假阳性**。报告写「营收 1000 亿」，只要某不相关字段恰好是 1000 就蒙混过关，使 M2「溯源覆盖率 100%」指标形同虚设 |
| #2 |  P1 | 单位换算依赖 `variants()` 字符串穷举启发式，同时产生假阴性（漏）与假阳性（撞） |
| #3 |  P1 | restatement 场景下回查未做版本锚定，旧版本数据也能「通过」，抵消了留档的意义 |
| #4 | 🟡 P2 | 成本模型 ¥3.1/票 未计入重试 / 多轮重写 / PDF 解析，建议按 ¥5-8 预算 |
| #5 | 🟡 P2 | 固定 pipeline 失去「发现意外就深挖」的能力，建议以确定性异常检测（YoY 突变 > 50%、现金流符号翻转）补偿 |
| #6 |  P2 | 快照缺 `ann_date`（公告日）—— 这是契约 C1 成立的**硬前提**，否则 Quant Lab 侧做因子回测会产生 look-ahead bias |

**#1 的解法**（已写入 [RESEARCH.md §3.8](../RESEARCH.md)）：把「事后从 markdown 正则抽数字」改为「Stage5 结构化输出 citation 元组 + Stage6 按 JSON Pointer 解析比对」：

```python
@dataclass(frozen=True)
class Citation:
    snapshot: str                              # "snapshots/20251120_income.json"
    pointer: str                               # "/data/营业总收入"  (RFC 6901)
    display: str                               # "1278.5亿"
    tolerance: float = 0.01
    derived_from: tuple[str, ...] = ()         # 派生值：依赖的其他 pointer

def check_citation(c: Citation, vault_root: Path) -> bool:
    doc = json.loads((vault_root / c.snapshot).read_text(encoding="utf-8"))
    val = resolve_pointer(doc, c.pointer)      # 路径不存在 → 直接失败
    return within_tolerance(parse_display(c.display), val, c.tolerance)
```

即：Stage6 验证的是**声明**，而不是对**文本**做猜测。附带收益是 citation 元组 `(文件, 指针)` 天然满足 ADR-021 §2 的契约原则。

### D5 — 文档一致性：发现 8 处漂移，本次修复 7 处

| # | 位置 | 漂移 | 处理 |
|---|---|---|---|
| **DR-1** | `ARCHITECTURE.md` L852 | 称「8 个 builtin tool」，实际 `pkg/tools/builtin/` 注册 **18 个**（已逐一核对 `Name()` 实现） | ✅ 本次修复 |
| **DR-2** | `ARCHITECTURE.md` L980-983 | Generate / Validate / Evolve Agent + Gene Pool 标「 规划中」，但 `pkg/ai/agents/{generate,validate,evolve}.go`、`pkg/ai/gene_pool/` **均已存在** | ✅ 本次修复（改标为已实现 + ODR-046 deprecated 注记） |
| **DR-3** | `ADR.md` L136 尾注 | 「ODR 累计 42 条」，实际已至 **046**（缺 ODR-043~046） | ✅ 本次修复 |
| **DR-4** | `AGENTS.md` §1/§13/§14 | 多处称 ODR 42 条 / ADR 15 条 | ✅ 本次修复 |
| **DR-5** | `ROADMAP.md` L199 | Phase 4 标「PROPOSED — Awaiting Approval」，但 Phase 4 已执行（`pkg/ai/*` 落地、ODR-045/046 已完成） | ✅ 本次修复 |
| **DR-6** | `VISION.md` L607 | 仍宣称 `pkg/ai/agents/optimize.go` ✅ 存在，与同文件 L395「NOT IMPLEMENTED — 文件从未创建」**自相矛盾** | ✅ 本次修复 |
| **DR-7** | `ARCHITECTURE.md` L438 | `fundamentals` 与 `stock_fundamentals` 字段重叠，文档自承「未来评估合并」但无任务登记 | ✅ 本次登记为 TASKS.md 任务（C-8） |
| **DR-8** | `docs/design/` | 目录语义冲突 —— `design/index.md` 开篇声明「适用范围：Quant Lab 前端 (Vue 3 + Naive UI)」，却存放 EquityDeep 后端规格 | ✅ 本次迁移至 `docs/design/equitydeep/` |

> DR-7 属**结构性欠债**（需迁移 SQL），不在文档修复范围内，已转 TASKS.md 跟踪。

## Consequences

### 正面

- **定位澄清**：明确 Quant Lab = 横截面层、EquityDeep = 纵向层，两者正交可叠加，消除了「是否要重写」的开放问题；
- **缺口被命名**：G1/G2/G3 从「隐约感觉不够」变为可追踪的任务来源，其中 G2 直接对应 VISION Principle 7 从口号到机制的转化；
- **护城河缺陷被提前拦截**：DR 类问题之外，EquityDeep #1 若在 M2 之后才暴露，溯源覆盖率指标已失真 —— 审计在 P0 阶段即发现；
- **文档可信度恢复**：8 处漂移中 7 处当场修复，ODR 索引重新对齐实际编号。

### 负面 / 遗留

- 引入**跨仓契约维护成本**（字段字典 / JSON Schema 双侧同步）；
- 两侧数据源不同（tushare.pro / akshare）产生**对账需求**，初期只告警不仲裁（与 ADR-016 「先注册者优先」一致）；
- DR-7（表重叠）为结构性欠债，需独立迁移，未在本次清偿。

## Artifacts

**新建**

| 文件 | 说明 |
|---|---|
| `docs/adr/adr-021-equitydeep-research-layer.md` | 架构决策：双时间尺度层 + 契约式整合 + 三桥 |
| `docs/RESEARCH.md` | Proposal 主体：Product 设计 + Tech Implementation |
| `docs/odr/odr-047-equitydeep-integration-audit.md` | 本文件 |

**迁移**

| 原路径 | 新路径 |
|---|---|
| `docs/design/EquityDeep_Product_Specification.md` | `docs/design/equitydeep/EquityDeep_Product_Specification.md` |
| `docs/design/EquityDeep_Technical_Specification.md` | `docs/design/equitydeep/EquityDeep_Technical_Specification.md` |

**更新**

| 文件 | 内容 |
|---|---|
| `docs/ADR.md` | 新增 ADR-021 + ODR-047 索引；修正尾注 ODR 计数 |
| `docs/ARCHITECTURE.md` | 修复 DR-1 / DR-2 / DR-7；新增「纵向基本面研究层 (EquityDeep)」章节；数据模型补 `fundamentals_detail`（27 张辅助表） |
| `AGENTS.md` | 修复 DR-4；文档索引 / 数据流 / 已知问题 / 当前状态（v3.0 → v3.1） |
| `docs/TASKS.md` | 登记 Sprint 8 任务 9 项（EQD-P0-1~P3-2，覆盖 C-1~C-9）+ DR-7；v3.19.0 → v3.22.0 |
| `docs/ROADMAP.md` | 修复 DR-5 |
| `docs/VISION.md` | 修复 DR-6 |
| `docs/design/index.md` | 声明目录边界，指向 `equitydeep/` 子目录 |

## Metrics

| 指标 | 值 |
|---|---|
| 审计维度 | 4（定位 / 可行性 / 短板 / 文档一致性） |
| 新设计可行性结论 | 补充（正交），非改变 |
| 识别结构性缺口 | 3（G1 基本面深度 / G2 事实门禁 / G3 叙事解释） |
| EquityDeep 侧发现缺陷 | 6（1×P0 护城河级 + 2×P1 + 3×P2） |
| 文档漂移发现 / 修复 | 8 / 7 |
| 新建文档 | 3（1 ADR + 1 RESEARCH + 1 ODR） |
| 迁移文档 | 2 |
| 新建任务 | 9（`TASKS.md` Sprint 8：EQD-P0-1~P3-2，覆盖 C-1~C-9） |

## Lessons Learned

1. **「承诺」与「机制」之间必须有一行可执行的断言**。EquityDeep 的产品承诺（逐数溯源）在技术规格里被实现成了「字符串在文件里出现过」—— 这类落差不会被任何代码测试捕获，只有**逐条对照产品承诺与实现机制**的审计能发现。建议后续对 VISION 中每条 Principle 都建立「机制在哪一行代码」的映射表。
2. **文档漂移的共性根因是「索引尾注 + 状态列」缺少更新触发点**。8 处漂移中 4 处位于索引文件的尾注或状态列（ADR.md 计数、ROADMAP 状态、AGENTS.md 计数、VISION 组件表），说明现有 Update-on-Change Triggers 表未覆盖「新增决策文档后同步计数」与「状态变更后回写状态列」。已据此在本次修复中同步更新，并建议将此二项显式写入 AGENTS.md Rule 1。
3. **正交性判断应早于技术整合讨论**。「要不要合并成一个系统」在这类讨论中总是最先被提出，但本案中产品层面先做了研究对象正交性判定，后续技术方案（契约而非合并）几乎是自动推导出来的 —— 先问「研究的是什么」再问「怎么实现」。
4. **审计产物走 ODR 的规则是正确的**（AGENTS.md Rule 3）。本次审计如果产出独立 Report，8 处漂移的修复动作就会与报告分离，报告归档后即失效；而 ODR 的 Artifacts 表强制把「发现」与「已修复的文件」绑定在同一处。