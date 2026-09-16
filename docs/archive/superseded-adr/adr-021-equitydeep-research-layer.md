# ADR-021: EquityDeep 纵向基本面研究层

> **Status**: **Superseded by [ADR-022](adr-022-unified-research-platform.md)**（2026-09-15）
> **Date**: 2026-09-15
> **Category**: Architecture
> **Superseded By**: [ADR-022](adr-022-unified-research-platform.md) — 统一研究平台（单一数据面 + 双工作面）
> **Related ADRs**: [adr-015](../../adr/adr-015-ai-agent-architecture.md) (AI Agent 架构), [adr-020](../../adr/adr-020-engine-decomposition.md) (Engine 分解 / Strategy ISP), [adr-016](../../adr/adr-016-multi-source-data-architecture.md) (多数据源)
> **Related ODRs**: [odr-046](../odr/odr-046-hermes-agent-integration-decision.md) (Hermes Agent 集成), [odr-047](../odr/odr-047-equitydeep-integration-audit.md) (EquityDeep 集成审计), [odr-048](../odr/odr-048-top-level-product-redefinition.md) (顶层产品重定义)
> **Author**: AI Assistant

---

> ⚠️ **本决策已被 [ADR-022](adr-022-unified-research-platform.md) 取代（2026-09-15）。**
>
> **仍然有效的部分**：§1 双时间尺度定位（横截面 vs 纵向正交）、§3 三条桥 B1/B2/B3、以及 [ODR-047](../odr/odr-047-equitydeep-integration-audit.md) 的全部审计发现（加固项 1-6）。
>
> **被取代的部分**：
> - §2「独立代码库，只锁数据契约」 —— ADR-022 改为**单一数据面**（EquityDeep 零数据副本，经只读证据 API 取数），两仓仍是两个代码库但同属一个产品、共享 L0。
> - §4「不统一技术栈 / 不让 vault 改成 PostgreSQL」 —— ADR-022 允许 EquityDeep **接入共享 PostgreSQL 的 `research` schema** 并容器化（叙事事实源仍为 vault markdown）。
>
> **取代理由**：ADR-021 只回答了"两层如何正交"，未回答"它们合起来是什么产品"，且其方案导致同一份财报双写（`snapshots/*.json` ↔ `fundamentals_detail`）。见 [ODR-048](../odr/odr-048-top-level-product-redefinition.md)。
>
> 阅读最新架构请以 **[PRODUCT.md](../../PRODUCT.md)** + **[ADR-022](adr-022-unified-research-platform.md)** 为准。

## Context

Quant Lab 是**横截面**量化研究平台：研究对象是策略与因子；单只股票被抽象为一行 OHLCV 时间序列 + 三个标量基本面（`pe` / `pb` / `roe`，见 `stock_fundamentals` 表）。Phase 4 通过 ADR-015 与 ODR-046 引入了 Hermes Agent 作为自主研究层，但研究单元仍是"因子"，而非"公司"。

这暴露三个结构性缺口（详见 [ODR-047](../odr/odr-047-equitydeep-integration-audit.md)）：

1. **基本面深度缺口** — `stock_fundamentals` 仅 `pe/pb/roe` 三个标量。所谓 value / quality 因子（[SPEC.md §因子定义](../../SPEC.md)）本质是"三个数的分位数"，无三表明细、无现金流质量、无杜邦分解。
2. **"事实正确性"门禁缺失** — [VISION.md](../../VISION.md) Principle 7「Evidence Over Intuition」目前是口号而非机制。现有 L1-L4 验证门禁（[ADR-015](../../adr/adr-015-ai-agent-architecture.md)）只校验**代码正确性**（语法 / IC / 回测 / 走查），不校验**事实正确性**（数字是否真有出处）。
3. **叙事解释层缺失 / L5 审查悬空** — 回测出 Sharpe 0.8 时，系统能给出 IC 却给不出"这家公司怎么了"。且 L5 人工审查 UI 已随 [ODR-045](../odr/odr-045-frontend-ai-component-deprecation.md) 删除，审查需求无处落地。

**EquityDeep** 是一份独立设计（见 [design/equitydeep/](../design/equitydeep/)）：**纵向**（单标的、季度频）基本面研究工作台。它的核心决策是：

- **固定 7-stage pipeline**（刻意拒绝 LangGraph / 自由 agent loop），把 agent 系统问题降维成"脚本 + 3 次 LLM 调用"；
- **文件系统即数据库，Obsidian 即 UI**（无 DB / 无 Web / 无 Docker）；
- **逐数溯源**：报告里每个数字是脚注指向快照 JSON 的某个字段，并有确定性回查脚本强制校验。

本 ADR 决定 EquityDeep 与 Quant Lab 的关系。

---

## Decision

### 1. 定位：双时间尺度层，互补而非替代

```
横截面层（日频）    Quant Lab     ── "市场在定价什么"
       ▲                                      │
       │ 深财务 → 因子 / 档案 → 审查材料       │ MCP: research.profile
       │                                      ▼
纵向层（季度频）    EquityDeep    ── "这家公司到底发生了什么"
```

两个研究对象**正交**，可线性叠加：

| 维度 | Quant Lab（横截面层） | EquityDeep（纵向层） |
|---|---|---|
| 研究单位 | N 只股票 × 1 因子 | 1 只股票 × N 个季度 |
| 时间尺度 | 日频 / 分钟频 | 季度频（财报期） |
| 产出 | 交易信号 / 策略排序 | 研究档案 / 可审读报告 |
| 消费者 | 回测引擎 | 人（1 小时审读） |
| 数据粒度 | OHLCV + 3 个标量基本面 | 三表全量 + 附注 + 研报 |

**结论：EquityDeep 是 Quant Lab 的补充层，不推翻、不重写 Quant Lab。**

### 2. 独立代码库，只锁数据契约

EquityDeep **保持独立代码库**（Python 3.11 / 文件系统 / Obsidian / 无服务），理由：

- "文件系统即数据库"与"固定 pipeline"是 EquityDeep 护城河的直接来源，在 Go 侧重实现会同时摧毁两者（数据主权 + 人机共写记忆 + 成本可预算）；
- Go 重写会把 **M1 数据质量硬门槛**（10 票 × 20 数字，错误率 < 2%）的验证推迟数周，而 M1 尚未通过 —— 在数据可信性未证明前投入重写是高风险投资；
- 两者技术栈、数据源（tushare.pro vs akshare）、UI 消费者均不同，强行统一无收益。

因此本 ADR **不规定** EquityDeep 的内部实现（那是它自己规格的事），**只约束两侧的接触面**：

> **契约原则**：两仓之间只允许通过 **显式声明的数据契约**（JSON Schema + 字段字典）通信。禁止任何一侧读取另一侧的内部数据结构。

### 3. 建立三条桥

| 桥 | 方向 | 内容 |
|---|---|---|
| **B1 深财务 → 因子** | EquityDeep → Quant Lab | EquityDeep Stage2 算出的派生指标（毛利率趋势、合同负债/营收、经营现金流/净利润、杜邦分解）经 ETL 进入 `factor_cache`，供现有 `FactorAware` 策略使用，把 quality 因子从"ROE > 15%"升级为可解释的多维质量因子 |
| **B2 档案 → 研究上下文** | Quant Lab ← EquityDeep | Quant Lab 侧新增 MCP 工具 `research.profile`，Hermes 可查询某标的的档案结论 / 疑点 / `needs_review` 状态，用于因子假设生成与 L5 审查材料 |
| **B3 数据质量门禁共享** | 双向 | EquityDeep 的 M1 抽检方法（10 票 × 20 数字，错误率 < 2%）泛化为 Quant Lab 的**数据源质量门禁**；两侧共享同一套抽检脚本与判定阈值 |

### 4. 明确边界（本 ADR 不做什么）

- ❌ **不**统一技术栈（不把 EquityDeep 改写成 Go，不把 vault 改成 PostgreSQL）。
- ❌ **不**统一方法论（Hermes 保持自由 LLM 驱动 loop；EquityDeep 保持固定 pipeline）。两者服务于不同的研究问题。
- ❌ **不**让 EquityDeep 产出交易信号或目标价 —— 这是 EquityDeep 的**永久产品红线**。
- ❌ **不**让 Quant Lab 把档案结论直接当作策略信号 —— 档案是**审查材料与因子假设来源**，不是信号源。合规边界见 §Cons.

---

## Consequences

### 正面

- Quant Lab 获得基本面纵深与事实门禁，不必自建深财务管道；
- EquityDeep 获得因子回测能力（可直接验证"合同负债占比高"是否有 alpha），不必自建回测引擎；
- 两侧的"可信数据"原则（VISION Principle 7 ↔ EquityDeep 逐数溯源）从口号变为**共享机制**；
- ODR-045 删除 L5 UI 留下的审查空白，由 Obsidian 档案补位，无需重建前端 AI 组件。

### 负面 / 成本

- 引入**跨仓契约维护成本**：字段字典与 JSON Schema 变更需两侧同步，需纳入变更流程；
- 两侧数据源不同（tushare.pro / akshare）会产生**对账需求**（同一指标两个值），初期只做告警不做仲裁（与 [ADR-016](../../adr/adr-016-multi-source-data-architecture.md) 的 "先注册者优先" 一致）；
- Quant Lab 需新增只读挂载 / 同步路径以访问 `_profile.json` 镜像，带来少量运维面。

### 风险

| 风险 | 对策 |
|---|---|
| EquityDeep M1 未通过 → B1 无数据可摄 | **硬依赖门禁**：B1 实施以 M1 通过为前置条件；B1 之前只做 B3（门禁共享，零依赖） |
| 档案被用户手工编辑导致 JSON 镜像过期 | 镜像内写 `generated_at` + `source_mtime`；Cons 使用方必须检查并在过期时降级为"档案截至 X 日期，可能失效" |
| 跨仓契约漂移 | 契约文件纳入两侧仓库版本控制，变更需走 ADR-021 修订或新建 ODR |

---

## Alternatives Considered

| 方案 | 内容 | 否决理由 |
|---|---|---|
| **B. 全量并入 Quant Lab** | Go 重写 EquityDeep pipeline 至 `pkg/research/`，vault → PG 表 + Vue UI | 摧毁"文件系统即数据库"与"固定 pipeline"两个核心决策（即护城河本身）；Go 重写把 M1 验证推迟数周；在数据可信性未证明前投入重写属高风险 |
| **C. 完全独立** | 不做任何整合 | Quant Lab 基本面深度与事实门禁缺口照旧；EquityDeep 重复自建数据管道与评估体系；两侧"可信数据"原则各自为政 |
| **D. 把 EquityDeep 建成 Quant Lab 的第 5 个微服务** | 8xxx 端口 + Docker + PG | 与 EquityDeep 非功能需求直接冲突（"无外部服务依赖 / 全部本地文件 / 无 docker"），且为单用户本地工具引入不必要的运维面 |

---

## References

- 上游规格：[docs/design/equitydeep/](../design/equitydeep/)（Product + Technical Specification）
- 本决策的完整设计方案（Product + Tech Implementation）：[docs/archive/RESEARCH-equitydeep-legacy.md](../RESEARCH-equitydeep-legacy.md)
- 审计发现与文档漂移清单：[ODR-047](../odr/odr-047-equitydeep-integration-audit.md)
- 相关：[VISION.md](../../VISION.md) Principle 7 (Evidence Over Intuition) / Principle 6 (Own Your Data)
- 相关：[ADR-015](../../adr/adr-015-ai-agent-architecture.md)（L1-L5 验证门禁）、[ODR-046](../odr/odr-046-hermes-agent-integration-decision.md)（Hermes 研究层）