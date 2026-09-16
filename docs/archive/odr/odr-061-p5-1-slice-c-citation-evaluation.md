# ODR-061: 阶段 P5 切片 C 评估 — Research Engine 输出携 citation 元组（收窄为 C2：`factor_cache` 因子链）

> **Status**: Completed（评估 + 裁决 + **C2 实施落地并验证**，2026-09-16）
> **Date**: 2026-09-16
> **Category**: Audit
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§2 五分区 / §3 四层架构 / §5 证据服务）
> **Supersedes**: —
> **Related ODRs**: [ODR-060](odr-060-p5-1-frontend-evidence-api.md)（切片 3 — 本记录承接其「未做项」第 3 条 = 切片 C）, [ODR-051](odr-051-l0-1-single-ingest-entry.md)（A 类归档咽喉点 + `as_of` 故意留 NULL）, [ODR-055](odr-055-eqd-p1-2-vertical-factors.md)（5 个纵向因子 = 本切片唯一实际覆盖面）, [ODR-050](odr-050-p1-base-contract-landing.md)（L0-2/L0-3 交付）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-060](odr-060-p5-1-frontend-evidence-api.md) 的「未做项」第 3 条把切片 C 记为：

> `docs/archive/odr/odr-060-p5-1-frontend-evidence-api.md:176` — 「**Research Engine 输出携带 `content_hash`（切片 C）** —— 让 `quant.*` / 因子 / 回测输出带 citation 元组，使乘积侧数字也可回溯。跨 schema 全链路，高风险。」

本记录即对该切片的**评估**。评估完成后交用户裁决，用户选定 **C2 收窄方案**，故本记录同时冻结 C2 的实施边界。

### 名词澄清：**「Research Engine」在代码中不存在**

全仓 Grep `ResearchEngine|research_engine` → **0 命中**。

它是 ADR-022 §3 四层架构中 **L2 编排面的横截面工作面**的概念名，不是实际类型。本切片实际触及的对象是 **L1 计算面（因子引擎）→ `quant.*` 存储**这一段，与代码符号对应关系如下：

| 概念名 | 代码对应 |
|---|---|
| Research Engine（横截面工作面，L2） | `pkg/data`（`FactorComputer` / `FactorAttributor`）+ `cmd/analysis`、`cmd/data` 的 factor handler |
| 横截面输出 | PG `factor_cache` / `factor_returns` / `ic_analysis` / `backtest_jobs.result` / `walk_forward_reports` |

### 契约要求（本切片成立的依据）

切片 C **不是可选项**，两条契约直接要求它：

| 契约 | 原文 | 含义 |
|---|---|---|
| ADR-022 §2 表 | `docs/archive/superseded-adr/adr-022-unified-research-platform.md:63` — C 类「派生计算结果（因子 / 回测 / IC）」的「其他侧副本」= ** 只读计算 API** | 禁止的是**数据副本**；citation 是**坐标**（hash + pointer），不是副本 |
| PRODUCT §6.3 能力表 | `docs/PRODUCT.md:240` — 「**跨层可引用**：报告脚注、**因子定义**、**回测日志**可用同一坐标引用同一数字」 | 因子定义被点名要求可引用 |
| PRODUCT §6.4 指标 | `docs/PRODUCT.md:282` — 「**证据完整性**：报告与**信号**中 100% 数字可用 `content_hash` 回溯到唯一原始记录」 | 判据是「100%」，非「可选」 |
| ADR-022 §5 | `docs/archive/superseded-adr/adr-022-unified-research-platform.md:101-102` — `citation = { source, dataset, key, as_of, content_hash }`；`GET /api/evidence/{content_hash}` → `ingest.raw` 唯一原始记录 | 权威形态**仅 5 字段，不含 pointer**；pointer 是独立配对（§4） |

> **推论 1（切片 C 成立）**：让乘积侧输出携带 citation **不违反**「其他侧仅持 hash」——恰恰相反，它是 §6.3/§6.4 的直接要求。

### 现场取证（四条事实）

| # | 取证 | 方法 | 结论 |
|---|---|---|---|
| **a** | **全链路只有一段是通的** | 见下方链路表 | 缺口不在 C，而在 **A→B** 与 **B→C** 两段 |
| **b** | **`archiveRaw` 已算 hash 又丢弃** | 读 `pkg/data/tushare_raw.go:31-59`（`hash, err := storage.ContentHashOf(body)` → 写入 `RawIngest` → **函数无返回值**）+ 调用点 `pkg/data/tushare.go:179` | A 类归档**已挂真实咽喉点**（ODR-051），但 hash **未回传给调用方** ⇒ 规范化层无从携带 |
| **c** | **`quant.*` 表零 hash 列** | 读 `pkg/storage/postgres.go:145-154`（`factor_cache` DDL）+ `:167`（`factor_returns`）+ `:59-64`（`ohlcv_daily_qfq` / `stock_fundamentals`） | 无列可写；且 `ohlcv_daily_qfq` / `stock_fundamentals` **连 `source` 列都没有** |
| **d** | **纵向面是唯一已有 hash 的链** | 读 `cmd/data/handlers_equitydeep_ingest.go:100`（`snapshotURI := "ingest.raw:" + contentHash`）+ `pkg/domain/fundamentals_detail.go:23`（`SnapshotURI`）+ `pkg/storage/fundamentals_detail.go:94-98,117`（SELECT `snapshot_uri`） | `fundamentals_detail.snapshot_uri` 是**唯一一条已落库的 hash 出参** |

### 链路现状（评估的核心发现）

| 链路 | 现状 | 证据 |
|---|---|---|
| **A→B** OHLCV / 基本面 | ❌ **断** | `archiveRaw` 算了 hash 却**无返回值**（取证 b）；`ohlcv_daily_qfq` / `stock_fundamentals` 无 `source` / `content_hash` 列（取证 c） |
| **A→B** equitydeep 纵向 | ✅ **已建** | 溯源门（`handlers_equitydeep_ingest.go:81` `GetRawIngest` 未命中 → 400）+ `fundamentals_detail.snapshot_uri NOT NULL` |
| **B→C** 因子计算 | ❌ **断** | `pkg/data.FactorStore`（`pkg/data/factor.go:17-27`）中唯一能带出 hash 的是 `GetFundamentalsDetailAsOf`（行含 `SnapshotURI`），而 `loadStatementBook`（`pkg/data/factor_equitydeep.go:187-219`）**读了行却丢弃 `SnapshotURI`**；`GetOHLCVForDateRange` / `GetFundamentalsSnapshot` 无 hash 出参 |
| **C→输出** | ❌ **断** | `pkg/domain/factor.go:53-61` `FactorCacheEntry` 无 citation 字段；`factor_cache` / `factor_returns` / `ic_analysis` / `walk_forward_reports` 无 citation 列 |

> **推论 2（缺的不是 C）**：在 C 层加列而 B 层读不到 hash，列必然**恒为空**——即「幽灵字段」，伪造可回溯性，反而违反 PRODUCT §6.4 的「100%」判据。

### 切片候选与裁决

| 候选 | 说明 | 结果 |
|---|---|---|
| **C1. 全链路** | 改 `archiveRaw` 签名 → OHLCV / `stock_fundamentals` 加 hash 列 → 全部因子 → 回测 / 走查 | ⛔ 不做（跨 schema 全链路，改 A→B 两段存量写入路径，风险面最大） |
| **C2. 收窄：`factor_cache` 因子链** | 只做**已有 hash 出参**的那条链（5 个纵向 fundamentals 派生因子），使 citation 有**真实取值**而非恒空 | ✅ **采纳**（用户裁决） |
| **C3. 零 schema 变更** | 输出时 join 回 `ingest.raw` 反查 | ⛔ 不做（momentum 源自 `ohlcv_daily_qfq`，无 hash 可 join；与 §6.4「100%」冲突） |

**用户三组细化裁决**（本记录据此冻结边界）：

| 议题 | 裁决 |
|---|---|
| citation 列存什么 | **列存 hash，输出面展开** —— 列内只存 `[{"content_hash":"<64hex>"}]`（可无损升级为完整 5 元组）；完整元组由 handler 在输出时解析 |
| 报告面承载 | **报告面推迟** —— `backtest_jobs.result` / `walk_forward_reports` 本切片**不动**（理由见 §「报告面为何推迟」） |
| 覆盖面 | 只覆盖 `factor_cache` 因子链（5 个纵向因子）；momentum / value / quality 空 citation 并显式标注 |

---

## Decision

**C2 收窄：让 `factor_cache` 的每一行携带其来源批次的内容坐标 `[{content_hash}]`，并在输出面展开为 ADR-022 §5 的 5 元组；只覆盖有真实 hash 出参的 5 个纵向基本面因子；报告面与 A→B 前两段明确不做。**

### 1. 存储层（迁移 027）

`factor_cache` 加一列，**只做增量、不改存量列**：

```sql
ALTER TABLE factor_cache
  ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb;
```

- **默认 `'[]'`（空数组）而非 NULL**：`[]` 的语义是「该数字的 A→B 链尚未建立」（momentum / value / quality 及历史行），NULL 则无法与「未采集」区分。
- **幂等**：`ADD COLUMN IF NOT EXISTS`；重复启动等价 no-op。
- **PG 11+ 带默认值加列为 metadata-only**，不重写表、无锁表风险（同迁移 026 的判断口径）。
- **不改写 `CREATE TABLE`（Migration 006 原文）**：与本仓迁移惯例一致（迁移 026 亦保留 `factor_name VARCHAR(20)` 原文，仅以 ALTER 放宽）——迁移**不可改写历史**；新库由 ALTER 收敛到同一形态。
- 文档副本 `docs/migrations/027_factor_cache_citation.sql`，头注格式照 `026_widen_factor_name.sql`（`定位 / 影响面 / 安全性 / 同源副本 / 历史副本不动`）。
- **DML 侧不设 CHECK 约束**：列内形态的可升级性（hash → 完整元组）优先于入库期强校验，与 `research.conclusion.citations`（JSONB 默认 `'[]'`）的既有口径一致。

### 2. 类型层

`pkg/domain/factor.go:53` `FactorCacheEntry` 增字段：

| 字段 | Go 类型 | JSON | 说明 |
|---|---|---|---|
| `Citation` | `json.RawMessage` | `json:"citation,omitempty"` | 列内原样透传。用 `RawMessage` 而非 typed struct 是刻意的：列内形态（今日 hash-only）与输出形态（5 元组）**不同**，且前者需可无损升级 |

### 3. 注入链（C2 的核心，唯一新增信息流）

```
ingest.raw (A)
   └─ scanRaw 归档时算出 hash ──┐（ODR-051 已在咽喉点，但 hash 未回传）
                                │
POST /api/ingest/equitydeep ────┘ 溯源门校验 content_hash
   └─ normalizer.Normalize(snapshot, snapshotURI = "ingest.raw:"+hash, ...)
        └─ fundamentals_detail.snapshot_uri (TEXT NOT NULL, 已落库，取证 d)
             └─ GetFundamentalsDetailAsOf → FundamentalsDetailRow.SnapshotURI（已返回）
                  └─ loadStatementBook ← ❌ 今日在此丢弃          【改动 1：保留】
                       └─ statementField 增 provenance          【改动 2：新增字段】
                            └─ saveVerticalFactor 写入 entry.Citation  【改动 3：收口点】
                                 └─ factor_cache.citation
```

| # | 文件 | 变更 |
|---|---|---|
| 1 | `pkg/data/factor_equitydeep.go` `loadStatementBook`（`:187`） | 保留 `r.SnapshotURI`：剥前缀 `ingest.raw:` 得 hash；**前缀不匹配者跳过**（不改写、不猜） |
| 2 | `pkg/data/factor_equitydeep.go` `statementField`（`:88`） | 增 provenance（与既有 `annDate map[reportPeriod]time.Time` 同构，按 period 记录来源 hash）；`statementBook` 另需一**每标的 hash 集合**，供因子尾段取用 |
| 3 | `pkg/data/factor_equitydeep.go` `saveVerticalFactor`（`:225`） | 5 个纵向因子**唯一收口点**；新增一个来源入参，写入 `entry.Citation` |

> **注**：`pkg/data.FactorStore`（`pkg/data/factor.go:17-27`）与 `pkg/backtest/cache.FactorStore`（`pkg/backtest/cache/factor_cache.go:41`）**接口均不变** —— citation 随 `FactorCacheEntry` 走，无新读方法。`Warm`（`:199`）硬编码 `{Momentum, Value, Quality}` 三个因子，本切片**不受影响**（其 citation 为空数组，语义正确）。

### 4. 输出面

`factor_cache` 的**唯一**输出端点是 data 服务的 `getFactorHandler`：

| 项 | 值 | 证据 |
|---|---|---|
| 路由 | `GET /factors/:factor_name?symbol=&date=` | `cmd/data/main.go:159` |
| 序列化 | `c.JSON(http.StatusOK, entry)` —— 整个 `FactorCacheEntry` 原样出参 | `cmd/data/handlers_factor.go:36,46` |
| 展开可行性 | handler 已持有 `*storage.PostgresStore`，`GetRawIngest` 可直接调用 | `cmd/data/handlers_equitydeep_ingest.go:81` 同一 store 的既有用法 |

**展开规则（handler 内）**：

1. 解码 `entry.Citation` 取得 `[{content_hash}]`；
2. 逐个 `store.GetRawIngest(ctx, hash)` → 命中则输出 `{source, dataset, key, as_of, content_hash}` 完整 5 元组；
3. **未命中（`(nil, nil)`）时只输出 `{"content_hash": "<hash>"}`** —— 其余四字段缺席**即是**「该响应未归档」的信号，与 `GET /api/evidence/{hash}` 的 404 一等语义同源。**不得凭空补字段**，故不引入额外标记位（保持 ADR-022 §5 的权威 5 字段形态）。
4. **`factor_cache` 未命中仍返回 404**（既有语义不变）；citation 解析失败**不得**把整个因子查询变成 5xx。

> **注**：该端点为**单条 GET**（按 `symbol + date + factor_name`），不存在「批量 hash」场景 —— 展开在本次请求内天然逐条。

### 5. 粒度语义（为什么是「请求批次坐标」）

- `archiveRaw` 刻意留 `as_of = NULL`（`pkg/data/tushare_raw.go:48-50`）：一个 tushare 响应可跨数千交易日，`content_hash` 已唯一标识该响应 ⇒ citation 的 `(dataset, key)` 落点是**请求批次**，`as_of` 缺席是设计而非缺失。
- **同一标的的多个 period 可能来自不同批次**（TTM 跨年桥接会用到不同归档的报表）⇒ citation **是集合**，这正是列内用数组的原因。
- **集合取「该标的本因子请求字段所载入的全部批次」**：citation 是**声明**（ADR-022 §5：声明 + JSON Pointer 精确解析两层分工），精确到「哪个数字 ← 哪个批次」由 pointer 层负责。此处取并集**略宽于**最小集，但不虚构坐标。

### 6. 明确不做（切片边界）

| 项 | 状态 | 理由 |
|---|---|---|
| `backtest_jobs.result` 内嵌 citation | ⛔ 不动 | 见下方专节 |
| `walk_forward_reports` 加列 | ⛔ 不动 | 同上 |
| momentum / value / quality 的 citation | ⛔ 不做 | 源自 `ohlcv_daily_qfq` / `stock_fundamentals`，两表**零 hash 列**（取证 c），无 hash 可写 |
| `archiveRaw` 签名改造 / OHLCV 与 `stock_fundamentals` 加 hash 列 | ⛔ 不做 | 即 C1；改 A→B 存量写入路径 |
| `factor_returns` / `ic_analysis` 输出带 citation | ⛔ 不做 | 二者由 `factor_cache` 派生（`pkg/data/factor_attribution.go:43,191`），溯源可回算但本切片不物化；须独立评估 |
| C3（零 schema 变更的反查方案） | ⛔ 不做 | 无 hash 可 join |

#### 报告面为何推迟（用户裁决的依据）

`pkg/backtest/cache/factor_cache.go:199` 的 `Warm` **硬编码** `{Momentum, Value, Quality}` 三个因子 —— 而这三个在 C2 里**全部不可覆盖**（money 源自 `ohlcv_daily_qfq`，value/quality 源自 `stock_fundamentals`，两表零 hash 列）。

⇒ 若照原方案在回测结果内嵌 citation、走查报告加列，两份输出里的 citation 将**恒为空数组** —— 即本评估推论 2 判定的「幽灵字段」。故**报告面整体推迟**，在 TASKS / 本文档标注「待 A→B 链落地」。

---

## Consequences

### 正面

| 收益 | 说明 |
|---|---|
| **乘积侧首次出现可回溯的数字** | 5 个纵向基本面因子的每一个 z-score / percentile 都可经 `content_hash` 回溯到 `ingest.raw` 的唯一原始响应，PRODUCT §6.3「因子定义可用同一坐标引用同一数字」首次在**代码**上成立 |
| **零新增信息流** | hash 早已在 `fundamentals_detail.snapshot_uri` 里落库（取证 d），本切片只是**把它读到、写成坐标**，不新建采集链路、不碰 A 类归档 |
| **零接口变更** | `pkg/data.FactorStore` / `pkg/backtest/cache.FactorStore` 均不变 ⇒ 生产调用方与两个 mock 零改动 |
| **可达性自动获得** | 输出面端点直接序列化 `FactorCacheEntry`，加字段即出参；且 Evidence API 已由 ODR-060 在 L3 面接通 ⇒ hash 到手即刻可查 |
| **列内形态可无损升级** | `[{content_hash}]` → 完整 5 元组是**纯追加**（JSONB 内加字段），未来若决定「列存完整元组」无须再次迁移 |

### 负面 / 代价

| 代价 | 说明 | 缓解 |
|---|---|---|
| `factor_cache` 每行冗余同一批次坐标 | 同一次请求的数百行写同一 hash，存储冗余 | 列 JSONB 极小（64 字符 hash）；`jsonb` 列 PG 内压缩，且「按 (factor, date) 归并」会把简单写入变成需二次查询的写入 |
| 5 个因子的尾段签名变更 | `saveVerticalFactor` +1 入参，5 个调用点同步 | 收口点**唯一**（`factor_equitydeep.go:225`），改动集中 |
| `citation` 在 3 个横截面因子上恒为 `[]` | 同一列存在「有值」与「无值」两种行 | **语义显式**：`[]` = 「该数字的 A→B 链尚未建立」，非「无证据」；并在 ODR/TASKS 标注覆盖面 |
| PRODUCT §6.4「100%」仍**未达标** | 本切片只覆盖 5/11 个因子，且 momentum 完全无链 | 如实记录为未达标项（见「未做项」），不以部分覆盖冒充达标 |

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| `snapshot_uri` 前缀非 `ingest.raw:`（存量/测试数据形如 `file:///fixtures/snapshot.json`，见 `pkg/data/factor_equitydeep_test.go:78`） | 中 | 前缀不匹配 → **跳过该行来源，不猜不造**；citation 只收合规 hash，宁可 `[]` 也不写伪坐标 |
| 迁移 027 在生产库的执行 | 低 | `ADD COLUMN IF NOT EXISTS` + 常量默认值 = metadata-only；与既有 `migrate()` 内联路径一致 |
| `citation` 列被误当作「因子质量」读 | 低 | `[]` 与有值语义写入本文档与 TASKS；输出面在未命中时**只出 hash**，不做任何填充 |

---

## Artifacts

### 已实施（2026-09-16，C2 落地；迁移编号实况 = **迁移 027**）

| 文件 | 变更 |
|---|---|
| `docs/migrations/027_factor_cache_citation.sql` | **新建** —— 迁移 027 文档副本（头注照 026 格式） |
| `pkg/storage/postgres.go` | 内联 `migrate()` 在 Migration 026 之后追加 `ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb`（实际执行路径） |
| `pkg/domain/factor.go` | `FactorCacheEntry` 增 `Citation json.RawMessage`（`json:"citation,omitempty"`） |
| `pkg/storage/cache.go` | `SaveFactorCacheBatch`（INSERT + `ON CONFLICT DO UPDATE` 均带 citation；空值归一为 `'[]'`）/ `GetFactorCache` / `GetFactorCacheRange` SELECT + Scan 加列 |
| `pkg/data/factor_equitydeep.go` | `loadStatementBook` 保留 `SnapshotURI`（剥 `ingest.raw:` 前缀，不匹配者跳过；重述胜者同构记录 provenance）+ `statementField` 增 provenance + `statementBook` 结构化（fields + 每标的去重排序 hash 集合）+ `saveVerticalFactor` 加来源入参写 `entry.Citation`（空集合 marshal 为 `[]`） |
| `cmd/data/handlers_factor.go` | `getFactorHandler` 输出前经 `expandCitation` 展开 hash → 5 元组（未命中只出 `content_hash`；解析失败透传原样不 5xx；`factor_cache` 未命中仍 404） |
| `pkg/data/factor_equitydeep_test.go` | citation 断言与 fixture 适配（`statementBook` 结构化访问）+ 新增 `TestContentHashOfSnapshot` / `TestLoadStatementBookCitation`（排序/去重/非归档重述丢弃批次）/ `TestComputeVerticalFactorCitation`（hash-only 存储 + 空标记语义） |
| `pkg/backtest/state/persistence_test.go` | **顺手修复（与本切片无关的平台缺陷）**：`TestDiskStateStore_ConcurrentSaveLoad` 以 `rune('0'+i)` 生成 ID，扫到 NTFS 非法字符 `: < > ?` 在 Windows 恒失败；改 `fmt.Sprintf("bt-concurrent-%02d", i)` |

### 文档

| 文件 | 变更 |
|---|---|
| `docs/archive/odr/odr-061-p5-1-slice-c-citation-evaluation.md` | **新建**（本记录）+ 实施后回写 Status / Artifacts / Metrics |
| `docs/TASKS.md` | 头部版本 3.36.0 → 3.37.0；阶段 P5 进展注补 C2 实施实况；变更日志新增本条目 |
| `docs/ADR.md` | ODR 索引 ODR-061 状态改 Completed；尾注 ODR 61 → 61（不变）；index 3.17.0 → 3.18.0 |

---

## Metrics / 验证（2026-09-16 实施取证）

| 项 | 判据 | 实测 |
|---|---|---|
| 构建 | `go build ./...` EXIT=0 | ✅ EXIT=0 |
| 测试 | `go test -count=1 ./pkg/storage/ ./pkg/data/ ./pkg/domain/ ./cmd/data/ ./pkg/backtest/...` 全 ok | ✅ 17 包全 ok（含新增 citation 用例） |
| **citation 写入（离线取证）** | 纵向因子行 citation 为 hash-only 数组、排序确定 | ✅ `TestComputeVerticalFactorCitation/persisted_entries_cite_the_batches...`（`[{aa..}]` 排序 + 不含五元组字段） |
| **空语义** | 无归档来源的行 citation = `[]`（非 null / 非缺席） | ✅ `TestComputeVerticalFactorCitation/readings_without_an_archived_snapshot...` |
| **前缀防御** | `snapshot_uri` 非 `ingest.raw:` → 不入 citation、不报错；非归档重述**丢弃**被覆盖批次 | ✅ `TestContentHashOfSnapshot` + `TestLoadStatementBookCitation/a_non-archived_restatement...` |
| **主验收点（运行时）** | `GET /factors/gross_margin_trend?...` 的 `citation[0].content_hash` 命中 `GET /api/evidence/{hash}` → 200 | ✅ **运行时取证通过（同日，TimescaleDB pg16 + Redis 容器 + 全链 HTTP）** —— ① `POST /api/ingest/raw` 归档 → hash `f6afca77…4823`；② `POST /api/ingest/equitydeep?content_hash=` 4 快照 → 8 行 / 0 dropped；③ `POST /sync/factors/gross_margin_trend` → 201；④ `GET /factors/gross_margin_trend?symbol=600999.SH&date=20260915` → citation 展开为 `{source,dataset,key,content_hash}`（`as_of` 因 NULL 正确缺席）；⑤ `GET /api/evidence/{hash}` → **200** + 原始记录；⑥ 未归档 hash → **404**（一等语义）；⑦ 列存直查确认 hash-only 形态。斜率实测 −0.00416 与种子数据 OLS 精确一致 |
| **顺手修复（运行时发现）** | — | ✅ **pgx v5 批次缺陷**：7 处 `tx.SendBatch` 用 `defer results.Close()` 后再 `Commit`，批次未关闭 → `conn busy`，落库必败（离线测试覆盖不到真实 PG，属预存在潜伏缺陷，非本切片引入；`SaveIndexConstituents`/`bulk_insert.go` 早已是正确写法）。修复 `cache.go` ×5（factor_cache / factor_returns / ic_analysis / dividends / splits）+ `fundamentals_detail.go` ×1，统一为显式 `Close()` before `Commit()`；pool 版 4 处（calendar / stocks / fundamentals ×2）无事务提交语义，`defer` 安全不动。修复后全链路一次通过 |
| 全仓回归 | `go test ./...` | `pkg/storage / pkg/data / pkg/domain / cmd/data / pkg/backtest/...` 全 ok；存量环境性失败与本切片无关：`internal/sandbox/runner`（POSIX-only，`exec: "pwd" not found in %PATH%`）与 `pkg/live::TestOrderManager_GetOrders_Snapshot`（时序 flake）—— 二者在未改动基线上同样失败 |

---

## 未做项（明确排除）

1. **`P5-1` 是否关闭** —— C2 已落地（本记录 Status: Completed），但 `P5-1` 描述中的「Research Engine 存量能力对接 L0 单一数据面」半句与旁路取数残留维度（`/market` → `8081` L3 直连、`handlers_proxy.go` 遗留镜像路由、`api/sync.ts` 死端点）均未动 ⇒ `P5-1` **整体仍未关闭**。
2. **报告面（用户裁决「推迟」）** —— `backtest_jobs.result` 内嵌 citation、`walk_forward_reports` 加列；**待 A→B 链落地**（`Warm` 硬编码的 `{Momentum, Value, Quality}` 三因子须先能拿到 hash）。
3. **PRODUCT §6.4「100%」达标** —— 覆盖面为 5/11 个因子；momentum 完全无链，value / quality 无链。本切片**不宣称**达标。
4. **C1（A→B 全链路）** —— `archiveRaw` 签名改造、`ohlcv_daily_qfq` / `stock_fundamentals` 加 `source` / `content_hash` 列。是切片 2 与 §6.4 达标的前置条件。
5. **C3（零 schema 方案）** —— 已裁决不做。
6. **`factor_returns` / `ic_analysis` 输出携 citation** —— 属同一 `quant.*` 派生面的其余两张表，须独立评估（其 citation 可由 `factor_cache` 回算）。
7. **切片 B（`api/sync.ts` 死端点）/ 切片 D（旁路残留清理）** —— 承 ODR-060，本记录不动。
8. **`docs/archive/odr/odr-060-*.md` 原文回写** —— 按本仓既有口径（ODR-057 裁决），**历史决策记录保留原文不回写**；其「未做项」在其时点成立，本记录以 `Related ODRs` 承接。

---

## Lessons Learned

1. **「加列」不等于「有值」—— 先验证信息流是否已到达收口点。** 本切片最初的直觉是「在 `quant.*` 加 citation 列」，但 `FactorStore` 里唯一能带出 hash 的是 `GetFundamentalsDetailAsOf`，而它返回的行**当场就被 `loadStatementBook` 丢弃了 `SnapshotURI`**。若不做这一步取证，加出来的列会恒为空 —— **幽灵字段比没有字段更危险，因为它看起来已经达标**。评估一个「补字段」类需求，第一步应是**沿信息流倒推**：这个值今天在系统里是否已经存在？在哪个变量上断掉？
2. **契约禁止「副本」不等于禁止「坐标」。** ADR-022 §2 表把 C 类的「其他侧副本」标为 ❌，很容易读成「`quant.*` 不许带溯源信息」。但该行禁止的是**数据重复**；citation 是 `ingest.raw` 的一个**指针**，携带它恰恰是 §6.3/§6.4 的要求。**读表时要区分「禁止什么对象」与「禁止什么属性」**：这里禁的是 payload，不是 hash。
3. **承接口径要「声明先行、精确后置」。** 本切片刻意让 citation 存 hash 集合（略宽于最小集）而非精确到「哪个数字 ← 哪一批次」，依据是 ADR-022 §5 的分工：citation 是**声明**，JSON Pointer 才是**精确解析**。**若让声明层承担精确层的工作，代价是每条因子计算的 provenance 都要逐 period 贯穿**（5 个因子 × 每个 period × 每个字段）—— 收益是精度，成本是侵入性。把精度留给 pointer 层，是这个分层设计的价值所在。
4. **把「已知会恒空」的部分明确推迟，比勉强实现更诚实。** 报告面（回测 / 走查）在此切片里**没有任何**可获得 hash 的因子，强行实现只会得到两份恒空 citation。裁决「推迟」并写下前置条件（待 A→B 链落地），使未达标项**可被后续验证**；反之，幽灵字段会让后续的「100% 证据完整性」审计读到虚假通过。

---

_Last updated by: AI Assistant — 2026-09-16 (C2 实施落地：迁移 027 + 注入链 + 输出面展开，Status → Completed；同日主验收点运行时取证通过，顺带修复 pgx v5 批次 conn busy 潜伏缺陷 ×7)_