# RESEARCH.md — 工作面 1（纵向深研）详案

> **Status**: Active（工作面详案 — 顶层定义见 [PRODUCT.md](PRODUCT.md)）
> **Version**: 0.2.0
> **Date**: 2026-09-15
> **Type**: Design（Product 设计 + Tech Implementation）
> **Top-level**: [PRODUCT.md](PRODUCT.md)（顶层产品定义，canonical）
> **Decision**: [ADR-022](adr/adr-022-unified-research-platform.md)（supersede [ADR-021](adr/adr-021-equitydeep-research-layer.md)）
> **Audit**: [ODR-047](odr/odr-047-equitydeep-integration-audit.md) · **Refactor**: [ODR-048](odr/odr-048-top-level-product-redefinition.md)
> **Upstream Specs**: [design/equitydeep/](design/equitydeep/)
> **Related**: [VISION.md](VISION.md) · [ARCHITECTURE.md](ARCHITECTURE.md) · [ADR-015](adr/adr-015-ai-agent-architecture.md) · [ODR-046](odr/odr-046-hermes-agent-integration-decision.md)

> ⚠️ **待按 ADR-022 修订**（2026-09-15）：本文件原以 ADR-021「两仓独立、只锁数据契约」为前提撰写，
> 该前提已被 [ADR-022](adr/adr-022-unified-research-platform.md) 取代（详见 [ODR-048](odr/odr-048-top-level-product-redefinition.md)）。
> **仍然有效**：§1 三缺口 G1/G2/G3、§2 Product 设计、§3.2 契约 C1/C2、§3.6 抽检、§3.8 加固项。
> **待修订**（以下以 ADR-022 为准）：
> 1. **定位**：EquityDeep 由"独立纵向层"改为**工作面 1**，与横截面工作面（工作面 2）对等；
> 2. **原始快照归属**：`vault/snapshots/*.json` → PG `ingest.raw`，vault 仅保留 `{content_hash, pointer}`；
> 3. **DB / Docker**：接入共享 PG 的 `research` schema（不新建实例）+ `equitydeep-research` worker 容器；
> 4. **取数方式**：改为**严格单一入口** — 经 L0 只读证据 API（adapter 归 L0），不再自抓 akshare；
> 5. **证据坐标**：citation 升级为 `{source, dataset, key, as_of, content_hash}` + `GET /api/evidence/{content_hash}`；
> 6. **执行路线**：§4 路线并入 ADR-022 的 P0~P5（详见 [PRODUCT.md §11](PRODUCT.md)）。

---

## 1. 问题陈述：为什么需要这一层

Quant Lab 已完成从"手动回测工具"到"AI-Native 量化研究平台"的演进。但它的研究单元始终是**因子/策略**，不是**公司**。这留下三个结构性缺口：

| # | 缺口 | 现状证据 |
|---|---|---|
| G1 | **基本面深度不足** | `stock_fundamentals` 仅 `pe` / `pb` / `roe` 三个标量（[ARCHITECTURE.md](ARCHITECTURE.md)）。value / quality 因子本质是"三个数的分位数"，无三表明细、无现金流质量、无杜邦分解 |
| G2 | **缺"事实正确性"门禁** | VISION Principle 7「Evidence Over Intuition」是口号。L1-L4 门禁（ADR-015）只校验代码正确性（语法/IC/回测/走查），不校验数字是否有出处 |
| G3 | **叙事解释层缺失** | 回测出 Sharpe 0.8，系统能给出 IC 但给不出"这家公司发生了什么"。且 L5 人工审查 UI 已随 ODR-045 删除，审查需求无处落地 |

EquityDeep 的设计恰好针对这三处：逐数溯源（G2）、三表全量财务（G1）、可人读的研究档案（G3）。

---

## 2. Product 设计

### 2.1 定位

> **顶层定位以 [PRODUCT.md](PRODUCT.md) 为准**：Quant Lab 是**共享底座**（L0 数据面 + L1 计算面 + L2 编排面），
> EquityDeep 是**工作面 1（纵向深研）**——"这家公司到底发生了什么"，横截面选股是**工作面 2**——"市场在定价什么"。
> 两者不是两个工具，而是**一个产品的两个对等工作面**，由飞轮闭环互相驱动 —— 见 [ADR-022](adr/adr-022-unified-research-platform.md)。

| 维度 | 工作面 2（横截面选股） | 工作面 1（纵向深研 / EquityDeep） |
|---|---|---|
| 研究单位 | N 只股票 × 1 因子 | 1 只股票 × N 个季度 |
| 时间尺度 | 日频 / 分钟频 | 季度频（财报期） |
| 产出 | 交易信号 / 策略排序 | 研究档案 / 可审读报告 |
| 消费者 | 回测引擎 / Hermes Agent | 人（1 小时审读） |
| 数据粒度 | OHLCV + 3 个标量基本面 | 三表全量 + 附注 + 研报 |
| 形态 | Vue SPA（体验层） | Obsidian Vault + worker 容器 |
| **共享底座** | **L0 唯一数据面 + L1 计算面 + L2 编排面（原 Quant Lab 能力降维）** | 同左（同一份数据、同一套证据链） |

> 契约关系：工作面 1 经 L0 只读证据 API 取数（**严格单一入口**），产出档案；纵向结论经桥 B1 转为因子进入 L1
> 计算面，供工作面 2 回测验证；回测结果经桥 B2 回流档案修正理解（飞轮闭环）。

### 2.2 目标用户与用户故事

EquityDeep 自身的用户故事（US-1~US-4）见其产品规格，本层不重复。**本层新增的是跨层场景 —— 也就是本 proposal 的产品价值所在：**

| ID | 用户故事 | 依赖 |
|---|---|---|
| **US-N1 疑点闭环** | 我在 EquityDeep 发现茅台"合同负债/营收 长期 > 30%"这个结论，想知道这在全市场是否有 alpha → 该指标变成因子进 `factor_cache`，跑横截面回测看 IC | 桥 B1 |
| **US-N2 档案驱动的假设生成** | Hermes 在挖因子前，先查目标标的的档案结论与疑点，用"这家公司什么是异常的"来生成更有依据的因子假设，而非盲搜 | 桥 B2 |
| **US-N3 回测结果的叙事解释** | 某策略在白酒板块异常好，我想知道为什么 → 查该板块标的档案，作为 L5 人工审查材料（补 ODR-045 删除的审查 UI） | 桥 B2 |
| **US-N4 数据可信度共享** | 我不确定 tushare 的财务字段准不准 → 复用 EquityDeep 的抽检方法对 tushare 源跑同样的 10 票 × 20 数字抽检 | 桥 B3 |

### 2.3 功能范围

| 层 | 功能 | 优先级 | 归属 |
|---|---|---|---|
| EquityDeep 自身 | F1 固定流程深度研究 / F2 数字溯源 / F3 研究档案 / F4 疑点跟踪 / F5 追问 / F6 评估回归 / F7 研报 PDF 解析 | P0 | EquityDeep 仓（见其规格） |
| **跨层** | **B1 深财务 → 因子** | P1 | Quant Lab 仓 + 契约 |
| **跨层** | **B2 档案查询 MCP 工具** | P1 | Quant Lab 仓 |
| **跨层** | **B3 数据质量门禁共享** | P0 | 双侧（脚本共享） |
| **跨层** | B4 双源对账告警（tushare vs akshare） | P2 | 双侧 |
| **跨层** | B5 档案覆盖度筛选器（"研究过且 healthy 的标的池"） | P2 | Quant Lab 仓 |

### 2.4 产品边界与合规红线

| 边界 | 内容 |
|---|---|
|  **EquityDeep 永久不产出** | 交易建议、目标价、评级（其产品规格已列为永久红线，本层强化为**跨层红线**） |
| ❌ **档案不是信号源** | Quant Lab 绝不把 `_profile.md` 的结论直接当作策略信号。档案的合法用途仅两种：**因子假设来源** 与 **人工审查材料** |
|  **不做实时** | 纵向层按财报期驱动（季度频），不接实时行情、不做盘中监控 |
| ✅ **数据主权** | vault 全部本地文件，不上传第三方（与 VISION Principle 6「Own Your Data」一致） |
| ✅ **免责声明** | 报告显著位置标注"不构成任何投资建议" |

### 2.5 成功指标

| 指标 | 目标 | 测量 |
|---|---|---|
| 跨层复利验证 | 至少 3 个 EquityDeep 结论转化为 Quant Lab 因子并完成 IC 评估 | 人工核对 + `factor_genes` 记录 |
| 档案被引用 | Hermes 会话中 `research.profile` 调用成功率 ≥ 95%（有档案时） | MCP 工具日志 |
| 数据门禁覆盖 | tushare 源完成同标准抽检（10 票 × 20 数字，错误率 < 2%） | 抽检报告 |
| 无新增幻觉面 | 跨层数据流不引入任何未经快照锚定的数字 | 回查脚本 |

---

## 3. Tech Implementation

### 3.1 总体架构

```
┌──────────────────────────────────────────────────────────────────────┐
│  EquityDeep 仓（独立，Python 3.11）                                    │
│  CLI → 7-stage pipeline → vault/{ticker}/{_profile.md, reports/,      │
│                                    snapshots/, _profile.json}         │
└───────────┬──────────────────────────────────┬───────────────────────┘
            │ 契约 C1: 快照 JSON               │ 契约 C2: 档案镜像
            │ (financial snapshot schema)      │ (_profile.json schema)
            ▼                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  契约层 contracts/  (版本化，两仓共用)                                  │
│    field_dictionary.yaml   ← 中文原始字段名 ↔ 规范 field_code          │
│    snapshot.schema.json    ← 快照结构                                 │
│    profile.schema.json     ← 档案镜像结构                             │
└───────────┬──────────────────────────────────┬───────────────────────┘
            │ 桥 B1                           │ 桥 B2
            ▼                                  ▼
┌───────────────────────────────┐  ┌───────────────────────────────────┐
│ Quant Lab (:8085)             │  │ Quant Lab 工具注册表               │
│  ETL → fundamentals_detail    │  │  pkg/tools/builtin/research_tool  │
│  → 因子计算 → factor_cache     │  │  → GET /api/tools (第 19 个工具)   │
│                               │  │  → Hermes 可发现                  │
└───────────────────────────────┘  └───────────────────────────────────┘
```

**设计原则（延续 ODR-046 的五条）**：Go 后端是唯一 DB 写者；EquityDeep 是 vault 的唯一写者（除用户手工编辑外）；两仓通过契约通信，不读取对方内部结构。

### 3.2 契约 C1：深财务快照 → Quant Lab

**表设计**（新增迁移 `docs/migrations/022_equitydeep_fundamentals.sql`）：

```sql
CREATE TABLE IF NOT EXISTS fundamentals_detail (
    ts_code        VARCHAR(12)  NOT NULL,   -- 600519.SH
    end_date       DATE         NOT NULL,   -- 报告期
    ann_date       DATE         NOT NULL,   -- 公告日 ← PIT 对齐必需，见下方⚠️
    field_code     VARCHAR(64)  NOT NULL,   -- 规范字段码 total_revenue
    raw_field_name VARCHAR(128) NOT NULL,   -- 原始字段名 营业总收入（保溯源）
    value          NUMERIC(24,4),
    unit           VARCHAR(16)  NOT NULL,   -- CNY / percent / ratio
    source         VARCHAR(32)  NOT NULL,   -- 'equitydeep:akshare.ths'
    fetched_at     TIMESTAMPTZ  NOT NULL,   -- 快照时间
    snapshot_uri   TEXT         NOT NULL,   -- vault 相对路径（反查快照）
    PRIMARY KEY (ts_code, end_date, ann_date, field_code, fetched_at)
);

CREATE INDEX IF NOT EXISTS idx_fund_detail_lookup
    ON fundamentals_detail (ts_code, field_code, end_date DESC);
```

设计要点：

| 要点 | 理由 |
|---|---|
| 主键含 `fetched_at` | 支持 restatement 保留历史（对齐 EquityDeep §2.2 规则 2） |
| 保留 `raw_field_name` | 可回溯到快照 JSON 的 `data.<raw_field_name>`，形成**完整溯源链**：报告脚注 → 快照 JSON → DB 行 |
| `snapshot_uri` | 从 DB 反向定位 vault 文件，供人工核验 |
| `source` 前缀 `equitydeep:` | 与 tushare 来源数据物理隔离，避免污染 |
| ⚠️ **`ann_date` 是 PIT 对齐的硬要求** | 因子计算必须用**公告日**而非报告期，否则产生 look-ahead bias（用未公布的财报回测）。EquityDeep 当前快照只有 `fetched_at`，**需在 C1 契约中新增 `ann_date` 字段**（见 §3.8 加固项 6） |

**字段字典**（`contracts/field_dictionary.yaml`，显式白名单，禁止启发式翻译）：

```yaml
version: 1
fields:
  - field_code: total_revenue
    raw_names: ["营业总收入", "营业总收入(元)"]   # 只允许白名单内映射
    unit: CNY
    statement: income
  - field_code: contract_liability
    raw_names: ["合同负债", "预收款项"]
    unit: CNY
    statement: balance
  - field_code: ocf_net
    raw_names: ["经营活动产生的现金流量净额", "经营现金流"]
    unit: CNY
    statement: cashflow
  # ... 见 EquityDeep 产品规格「附录：核心数据字段定义」
```

### 3.3 契约 C2：档案镜像 `_profile.json`

**md 是真源，JSON 是派生镜像**（因为用户在 Obsidian 里直接改 md，agent 下次读取所见即用户所改）。

```json
{
  "schema_version": 1,
  "ticker": "600519",
  "name": "贵州茅台",
  "generated_at": "2025-11-20T10:40:02+08:00",
  "source_file": "_profile.md",
  "source_mtime": "2025-11-20T10:39:55+08:00",
  "last_researched": "2025-11-20",
  "needs_review": false,
  "review_reason": "",
  "conclusions": [
    {"id": "C1", "title": "渠道话语权强", "body": "预收款占收入比长期>30%且稳定",
     "as_of": "2025Q3", "confidence": "高", "status": "active",
     "citations": ["snapshots/20251120_balance.json#/data/合同负债"]}
  ],
  "questions": [
    {"id": "Q1", "text": "批价对一批价价差扩大至400元", "status": "open", "raised_at": "2025-11"}
  ],
  "updated_at_log": [{"at": "2025-11-20", "summary": "新增 C3，推翻 C2，解决 Q2"}]
}
```

| 字段 | 用途 |
|---|---|
| `generated_at` + `source_mtime` | **过期检测**：消费方必须比对，`source_mtime > generated_at` 或 `generated_at` 超龄 → 降级使用 |
| `needs_review` | 跨层复用 EquityDeep 的失效机制：Hermes 见到 `true` 时须标注"以下结论需用新数据重验" |
| `citations[]` | 结论的依据锚点，供 Quant Lab 侧做**假设到数据的可追溯**（对应 B1 的因子论证） |

**生成时机**：Stage7 之后自动生成 + 用户可手动 `equitydeep sync` 刷新。生成的必须是**确定性脚本**，不得由 LLM 直接产出。

### 3.4 桥 B1 实施：深财务 → 因子

新增指标（EquityDeep 的 Stage2 已经会算，Quant Lab 侧只需摄取 + 转因子）：

| 因子 | 定义 | 所需字段 | 对应 EquityDeep 概念 |
|---|---|---|---|
| `gross_margin_trend` | 毛利率 4 季线性斜率 | 营业总收入, 营业成本 | 财务质量 |
| `contract_liability_ratio` | 合同负债 / 营业总收入(TTM) | 合同负债, 营业总收入 | 渠道话语权（疑点 Q1 同源） |
| `ocf_to_net_profit` | 经营现金流 / 归母净利润(TTM) | 经营现金流, 归母净利润 | 现金流质量 |
| `roe_dupont_leverage` | 杜邦分解的权益乘数分量 | 总资产, 归母净资产 | 杜邦分解 |
| `inventory_turnover_delta` | 存货周转率同比变化 | 存货, 营业成本 | 存货周转异常（疑点 Q2 同源） |

**因子口径（EQD-P1-2 落地口径）**：上表只给了文字定义，实现时确定了下列数学口径。报表口径依 `contracts/field_dictionary.yaml`：利润表/现金流量表项目**年内累计**（Q1 = 3 月、Q4 = 全年，每个财年边界归零），资产负债表项目为**时点值**；累计项转 TTM 用 `TTM(Qn,Y) = YTD(Qn,Y) + YTD(Q4,Y-1) − YTD(Qn,Y-1)`（Q4 即全年，直接取本次累计）。

| 因子 | 口径 |
|---|---|
| `gross_margin_trend` | 最近 4 期**单季**毛利率的 OLS 斜率：`margin(q) = (单季营业总收入 − 单季营业成本) / 单季营业总收入`，`factor = Slope([margin(q-3) … margin(q)])`（单位：毛利率/季）。用单季而非累计：累计口径下斜率主要在度量财年推进，而非盈利变化。 |
| `contract_liability_ratio` | `合同负债(最新时点) / 营业总收入(TTM)`。 |
| `ocf_to_net_profit` | `经营现金流(TTM) / 归母净利润(TTM)`。 |
| `roe_dupont_leverage` | `总资产(最新时点) / 归母净资产(最新时点)` —— 杜邦分解的权益乘数分量。 |
| `inventory_turnover_delta` | `存货周转率(当期) − 存货周转率(去年同期)`，其中 `存货周转率(p) = 营业成本(TTM at p) / 存货(p)`（期末余额，非两点平均 —— 契约每期仅存一个读数，两点平均需要首期没有的期初余额）。 |

**不可支撑的标的跳过而非近似**：历史不足 4 期、TTM 桥接腿缺失、分母非正（单季营收 / TTM 净利润 / 归母净资产 / 存货）一律跳过该标的，不填替代值 —— 替代值会把财年语义混进因子，下游无法与真实信号区分。`factor_cache` 尾段（横截面 z-score + percentile）与既有三个横截面因子完全一致。

**这是本 proposal 最实质的工程收益**：Quant Lab 的 quality 因子从"`ROE > 15%`"升级为**可解释的多维质量因子**，且每个因子都能追溯到"是哪家公司的哪份档案发现的"。

**ETL 路径**：`equitydeep export --format=jsonl` → `POST /api/ingest/raw` 归档原始快照并取得 `content_hash` → `POST /api/ingest/equitydeep`（ndjson，携带该 `content_hash`）归一化 → `fundamentals_detail` UPSERT → 现有 `FactorComputer` 批量计算 → `factor_cache`。

> **入口形态修正（EQD-P1-2，[ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md)）**：本节原稿写「摄取命令（非 HTTP，避免引入 EquityDeep 服务化）」，落地时经裁决改为**新增 HTTP 写入口**。要避免的是「EquityDeep 常驻服务化」，而不是 HTTP 本身：`POST /api/ingest/equitydeep` 是一道**无状态写门**，不要求上游起服务，与 L0-1 的 `POST /api/ingest/raw`（[ODR-051](odr/odr-051-l0-1-single-ingest-entry.md)）同构，EquityDeep 侧只需一个 HTTP client 即可上报，无需把 Go 二进制嵌进 Python 流程。

**PIT 约束**：因子在回测日 `D` 只能使用 `ann_date <= D` 的行。这是必须在 ETL 阶段强制执行的过滤，否则回测结果虚高。

### 3.5 桥 B2 实施：档案查询 MCP 工具

**零新架构** —— 只是 `pkg/tools/builtin/` 的第 19 个工具，自动出现在 `GET /api/tools` 供 Hermes 发现（延续 ODR-046 的 MCP 桥模式）。

```
POST /api/tools/research.profile
  body:  {"ticker": "600519", "sections": ["conclusions", "questions"]}
  200:   {"ticker","name","last_researched","needs_review","stale":false,
          "generated_at":"...","conclusions":[...],"questions":[...]}
  404:   {"error": "no_profile", "ticker": "600519"}
```

实现要点：
- 新增 `pkg/tools/builtin/research_tool.go`
- vault 路径由环境变量 `EQUITYDEEP_VAULT_PATH` 提供，**只读挂载**（docker-compose.override 或本地直读）
- 只解析 `_profile.json`，**不解析 markdown**（Go 侧解析用户可任意编辑的 md 极其脆弱）
- `stale` 判定：`source_mtime > generated_at` 或 `generated_at` 超龄
- 标的代码归一化：`600519` ↔ `600519.SH`（复用现有 `pkg/domain` 的 ts_code 规则）
- 输出**不含任何未经 citation 锚定的论断** —— 工具只回传档案原文，不做 LLM 二次加工

> **落地状态（EQD-P2-1，[ODR-057](odr/odr-057-eqd-p2-1-research-profile-tool.md)）**：已实现，实现要点 ↔ 实际实现的差异如下 —— ①**来源为双源**：先读 PG `research.*` 投影（`pkg/storage/research.go` 新增读取层），投影无该标的数据时**回退**读 vault `_profile.json`（`source` 字段显式暴露来自哪一侧；`EQD-P2-1` EquityDeep 接 PG 落地后删除回退分支）；②vault 路径取 `v.GetString("equitydeep.vault_path")`（`EQUITYDEEP_VAULT_PATH` 经 viper `AutomaticEnv` + `.`→`_` 映射可达），**只读挂载本身是 C-6 / `EQD-P2-2`，尚未落地**；③`stale` 判定扩展为三条（`generated_at` 零值 / `source_mtime > generated_at` / 超 90 天 `DefaultProfileMaxAge`），并附 `stale_reason`；④标的归一化在工具内自实现（剥交易所后缀/`sh|sz|bj` 前缀 + 闭集后缀校验），**未复用** `pkg/domain` 的 ts_code 规则；⑤`schema_version != 1` 拒绝服务；⑥无档案返回 404 `NOT_FOUND`（而非 200 + 空对象）。

### 3.6 桥 B3 实施：数据质量门禁共享

把 EquityDeep 的 M1 抽检方法泛化为**跨源通用脚本**：

```
evals/data_quality/spot_check.py
  输入: --source {akshare,tushare} --tickers <10 tickers> --fields <20 fields>
  输出: 错误率 + 明细（哪只票哪个字段对不上）
  判定: 错误率 < 2% → PASS
```

| 收益 | 说明 |
|---|---|
| 对 EquityDeep | 复用 Quant Lab 已有的 tushare 数据做**双源交叉校验**（EquityDeep 产品规格 P1 项） |
| 对 Quant Lab | 获得对现有 tushare 数据源的**独立质量门禁**，目前完全没有此类校验 |

**这是零依赖、可立即开工的一项**，不依赖 EquityDeep M1 通过。

### 3.7 Quant Lab 侧改造清单

| # | 改造项 | 文件/位置 | 依赖 |
|---|---|---|---|
| C-1 | 新增 `fundamentals_detail` 表 | `docs/migrations/022_equitydeep_fundamentals.sql` | 契约冻结 |
| C-2 | 契约文件纳入版本控制 | `contracts/`（新目录） | — |
| C-3 | EquityDeep 摄取链（归一化纯包 + 落库/读取 + HTTP 写门） | `pkg/data/equitydeep/`（新包）+ `pkg/domain/fundamentals_detail.go` + `pkg/storage/fundamentals_detail.go` + `cmd/data/handlers_equitydeep_ingest.go` | C-1 |
| C-4 | 新增 5 个基本面因子（桥 B1） | `pkg/data/factor_equitydeep.go` + `pkg/domain/factor.go`（枚举）+ `docs/migrations/026_widen_factor_name.sql` | C-3 |
| C-5 | MCP 工具 `research.profile` | `pkg/tools/builtin/research_tool.go` + `pkg/storage/research.go` | 契约 C2 |
| C-6 | vault 只读挂载配置 | `docker-compose.override.yml` | C-5 |
| C-7 | 抽检脚本泛化 | `evals/data_quality/` | — |
| C-8 | 修复 `fundamentals` / `stock_fundamentals` 表重叠 | `docs/migrations/025_equitydeep_field_consolidation.sql` | 独立 |
| C-9 | 文档漂移修复 8 项 | 见 [ODR-047](odr/odr-047-equitydeep-integration-audit.md) | — |

> **落地状态**: C-1 / C-2 / C-7（[ODR-052](odr/odr-052-p1-contract-freeze-and-spot-check.md) + [ODR-053](odr/odr-053-p3-fundamentals-detail-table.md)）、C-3 / C-4（[ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md)）、C-9（[ODR-047](odr/odr-047-equitydeep-integration-audit.md) + [ODR-054](odr/odr-054-dr-reverification.md)）已落地；**C-8 已收口** —— `fundamentals` 存量并入 `stock_fundamentals` 后 DROP 旧表（`EQD-P3-1` / [ODR-056](odr/odr-056-fundamentals-table-consolidation.md)）；**C-5 已落地** —— `research.profile` 工具（PG 投影优先 + vault 回退 / [ODR-057](odr/odr-057-eqd-p2-1-research-profile-tool.md)）。C-6 待阶段 P4（`EQD-P2-2` vault 只读挂载，需容器化）。

### 3.8 EquityDeep 侧加固项（本审计发现）

> 这 6 项是**本 proposal 对 EquityDeep 上游规格提出的修改建议**，其中 #1 是护城河级别的缺陷。

#### 🔴 #1 回查脚本必须改为字段级锚定（P0，护城河缺陷）

上游规格 §3.1 的实现在做**全局子串匹配**：

```python
snapshot_text = " ".join(p.read_text(...) for p in snap_dir.rglob("*.json"))
if not any(repr_num in snapshot_text for repr_num in variants(val)):
    errors.append(...)
```

**缺陷**：任何数字只要**在快照里任意位置出现过**就算通过。报告写"营收 1000 亿"，只要某不相关字段恰好是 1000 就蒙混过关 → **假阳性**。这会让 M2 里程碑的"溯源覆盖率 100%"指标形同虚设。

**建议改为 citation 元组锚定 + JSON Pointer 解析 + 值比对**：

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
    val = resolve_pointer(doc, c.pointer)      # 路径不存在 → 直接失败（不再靠字符串碰运气）
    return within_tolerance(parse_display(c.display), val, c.tolerance)
```

配套改动：Stage5 必须**结构化输出**数字与 citation 的绑定（而非事后正则从 markdown 抽取）。这样 Stage6 是对**声明**做验证，而不是对**文本**做猜测。

> 附带收益：citation 元组 `(文件, 指针)` 天然满足 [ADR-022 §5](adr/adr-022-unified-research-platform.md) 的证据服务原则 —— 引用的是可机器解析的内容坐标，不是模糊字符串。（原引用的 ADR-021 §2 已被 ADR-022 §5 取代）

#### 🟠 #2 单位换算不得依赖 `variants()` 启发式（P1）

`"1278.5亿"` ↔ `127854000000` 的关系应由**显式换算表 + citation 内的 `display` 字段**决定，而不是穷举字符串变体。启发式会同时产生假阴性（漏）与假阳性（撞）。

#### 🟠 #3 restatement 版本锚定（P1）

同一报告期存在多个快照时，回查**只能针对"生成该报告时实际使用的那个版本"**，而非全部版本。否则旧版本数据也能"通过"回查，restatement 的意义被抵消。建议 citation 的 `snapshot` 字段强制带时间戳文件名。

#### 🟡 #4 成本模型补全（P2）

上行规格的 ¥3.1/票 未计入：akshare 失败重试、Stage5 多轮重写（规格允许 1 次，实际触发率可能 20-30%）、PDF 解析。建议按 **¥5-8** 做预算并在 `runs/{ts}/trace.jsonl` 中累计实际 token 计量。

#### 🟡 #5 把"意外发现"从 LLM 随机性转移到确定性规则（P2）

固定 pipeline 的最大代价是失去 Hermes 那种"发现意外就深挖"的能力。F5 追问只补偿了**用户主动**的深挖。建议 Stage4 增加确定性异常检测：YoY 突变 > 50% 的科目、环比符号翻转的现金流科目，**自动进疑点清单**。这样发现能力不再依赖 LLM 是否恰好注意到。

#### 🟡 #6 快照新增 `ann_date` 字段（P2，B1 前置）

当前快照只有 `fetched_at` 与 `report_period`，**缺公告日**。而 Quant Lab 侧做因子回测必须有公告日才能避免 look-ahead bias。这是契约 C1 能成立的硬前提，建议在 EquityDeep 数据层尽早补齐。

---

## 4. 实施路线

> **编号口径（重要）**: 下表为**本文件自有序号**（记作 **R-P0 ~ R-P5**，仅描述跨仓协作的先后与阻塞关系）。它与 ADR-022 的**执行阶段编号**（P0 顶层定义 / P1 底座契约 / P2 工作面 1 跑通 / P3 计算面补齐 / P4 飞轮打通 / P5 横截面工作面 v2，见 [TASKS.md](TASKS.md) Sprint 8「阶段映射」）**语义不同、不可互译**——例如本文 R-P5（C-8 表合并等收尾项）在 ADR-022 编号下属**阶段 P3**。任务阶段归属一律以 ADR-022 为准（见 [ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)）。

| 阶段 | 内容 | 前置 | 阻塞关系 |
|---|---|---|---|
| **P0** | B3 质量门禁共享（C-7）；EquityDeep 加固项 #1；契约冻结（C-2） | 无 | **不阻塞，可立即开工** |
| **P1** | EquityDeep M1：10 票 × 20 数字抽检，错误率 < 2% | P0 | **全局硬门槛** —— 未通过则 B1 不具备数据基础 |
| **P2** | B1 深财务摄取（C-1 → C-3 → C-4） | P1 通过 + 加固项 #6 | 依赖 |
| **P3** | B2 档案查询 MCP 工具（C-5 → C-6） | EquityDeep M3（档案机制可用） | 弱依赖 |
| **P4** | 交叉验证闭环：EquityDeep 疑点 → 因子 → 回测 → IC | P2 + P3 | — |
| **P5** | 收尾：C-8 表合并、B4 双源对账、B5 覆盖度筛选器 | P4 | — |

**关键判断**：P0 与 P1 是**唯一真正的关键路径**。P2+ 全部可以在 EquityDeep 独立推进的同时于 Quant Lab 侧并行准备（契约冻结后即可写 ETL 代码，用 fixture 数据测试）。

---

## 5. 文档落位

| 产物 | 路径 | 文档类型 |
|---|---|---|
| 本设计方案（Product + Tech） | `docs/RESEARCH.md` | 设计文档（与 VISION/SPEC/ARCHITECTURE 并列） |
| 架构决策 | `docs/adr/adr-022-unified-research-platform.md` | ADR（取代 ADR-021） |
| 审计记录与文档漂移清单 | `docs/odr/odr-047-equitydeep-integration-audit.md` | ODR |
| EquityDeep 上游规格（迁移） | `docs/design/equitydeep/` | 子项目规格（原位于 `docs/design/` 根，与"前端设计系统"目录语义冲突） |
| 索引更新 | `docs/ADR.md` | ADR/ODR 索引 |
| 架构与拓扑更新 | `docs/ARCHITECTURE.md` | 服务拓扑 + 数据模型 |
| 智能体上下文更新 | `AGENTS.md` | 文档索引 / 数据流 / 已知问题 |
| 任务登记 | `docs/TASKS.md` | 统一任务追踪 |

---

## 6. 未决问题

| # | 问题 | 需要谁决策 |
|---|---|---|
| Q-1 | vault 如何被 Quant Lab 访问：同机只读挂载？还是 `equitydeep sync` 推到独立镜像目录？后者跨机可用但多一次同步 | 待定 |
| Q-2 | `contracts/` 的 owner 与变更流程：字段字典改动是否需要双侧同步 PR？ | 待定 |
| Q-3 | 双源对账策略：tushare 与 akshare 同一指标冲突时，是告警、取一方、还是引用第三源？ | 建议 P2 再定，初期只告警 |
| Q-4 | EquityDeep 是否纳入 Quant Lab 的 CI（跨仓契约测试）？ | 待定 |
| Q-5 | EquityDeep 自身是否独立成仓，还是作为 Quant Lab 的 subdirectory？ | 待定（架构上独立，物理位置不影响契约） |