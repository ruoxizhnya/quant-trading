# ODR-055: 阶段 P3 — EQD-P1-2 摄取链 + 5 个纵向基本面因子（C-3 / C-4）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§2 数据归属 / §3 五分区与计算面 / §4 取数 / §5 证据服务 — 本次落地的依据）
> **Supersedes**: —
> **Related ODRs**: [ODR-053](odr-053-p3-fundamentals-detail-table.md)（EQD-P1-1 表落地，本记录消费其表与 PIT 语义）, [ODR-052](odr-052-p1-contract-freeze-and-spot-check.md)（`contracts/` 契约冻结，本记录以 `field_dictionary.yaml` 为唯一白名单来源）, [ODR-051](odr-051-l0-1-single-ingest-entry.md)（L0-1 `POST /api/ingest/raw`，本记录以它为溯源前置）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计 — C-3 / C-4 缺口来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-053](odr-053-p3-fundamentals-detail-table.md) 落地了 `fundamentals_detail`（契约 C1），但该表当时**既无写入方也无读取方**：有表、有 PIT 列、有 `snapshot_uri` 溯源列，却没有任何代码往里写一行，也没有任何因子读它。同时 [archive/RESEARCH-equitydeep-legacy.md](../RESEARCH-equitydeep-legacy.md) §3.4 的桥 B1 定义了 5 个纵向基本面因子，但**没有任何实现**。EQD-P1-2 的出口判据是：**EquityDeep 快照可在不猜量级的前提下进入类 B，且 5 个因子能从这些数字上算出来并落进 `factor_cache`**。

### 前置事实（本次现场复核）

| 项 | 复核结论 |
|---|---|
| `fundamentals_detail` 写入方（改动前） | **0 个** |
| `fundamentals_detail` 读取方（改动前） | **0 个** |
| `factor_cache.factor_name` 列宽 | `VARCHAR(20)` —— 桥 B1 因子名最长 **24** 字符（`contract_liability_ratio` / `inventory_turnover_delta`），**直接写入会报错** |
| 同源列的其他表 | `factor_returns`（IC 分层归因）、`ic_analysis`（IC 统计）持有同名列，**必须同时放宽**，否则 IC 链路会在新因子名上失败 |
| 归一化所需的白名单 / 换算表来源 | `contracts/field_dictionary.yaml`（[ODR-052](odr-052-p1-contract-freeze-and-spot-check.md) 冻结，契约 C1-a）—— `raw_name → field_code` 与 `unit_scale` **不得在运行时推导** |
| 契约是否内嵌进二进制 | **否**（只有 `docs/openapi.yaml` 内嵌）→ 词典须运行时加载，加载失败时端点必须拒绝服务而非降级猜测 |
| 报表口径（契约） | 利润表 / 现金流量表项目**年内累计**（Q1=3 月、Q4=全年，财年边界归零）；资产负债表项目为**时点值** |
| 既有因子实现范式 | `pkg/data/factor.go` 的横截面因子 —— `ZScore` + `PercentileRank` + `SaveFactorCacheBatch` |
| 批量写入约定 | 统一 `pgx.Batch` + `SendBatch` + `ON CONFLICT ... DO UPDATE`；**拒绝 COPY** |
| PIT 约束 | 因子在回测日 `D` 只能看到 `ann_date <= D` 的行，必须在**读取阶段**强制过滤 |
| 工具链 / Docker | `go` 为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`；Docker Desktop 未运行 → DB-backed 测试按既有约定 SKIP |
| 本仓 git 写对象 | 默认 createObject 模式下 `commit` / `add` 报 `Permission denied`，须 `git -c core.createObject=link` |

### 三项规格冲突（经裁决）

本任务在落地过程中遇到三处 RESEARCH 原文与实现需要的分歧，均经用户裁决后落地：

| # | 分歧点 | 裁决 |
|---|---|---|
| 1 | C-3 摄取入口形态：RESEARCH §3.4/§3.7 原文写「**非 HTTP**」 | **改为新增 HTTP 写入口** `POST /api/ingest/equitydeep` —— 原文要避免的是把 EquityDeep 变成常驻服务，而非 HTTP 本身；解析/归一化仍放在**纯包** `pkg/data/equitydeep/`，与传输层解耦 |
| 2 | C-4 因子落库命名：`factor_name` 为 `VARCHAR(20)`，容不下 24 字符因子名 | **放宽到 `VARCHAR(32)` + 新增 5 个 `FactorType`**，落为迁移 026（3 张表同源列一起放宽） |
| 3 | `snapshot.data` 中的字符串数值（如 `1,278.54亿`）如何解析 | **闭集后缀表 + 去千分位** —— 编译期封闭表映射到契约既有 `unit_scale` 键；剥 `,`；表外形态一律丢弃并 warn，**不猜数量级** |

---

## Decision

**把「快照 → 可算数字 → 因子」这条链一次接通：纯包负责归一化、storage 负责落库与 PIT 读取、HTTP 只做窄门，因子在 TTM/单季口径上重算并复用既有因子落库尾部。**

### 1. C-3 摄取链

#### 1.1 归一化纯包 `pkg/data/equitydeep/`（无 HTTP 依赖）

- `dictionary.go` —— `Dictionary` / `FieldDef`；`LoadDictionary(path)` / `ParseDictionary(raw)`：解析 `contracts/field_dictionary.yaml` 并做**结构完整性**校验（`unit_scale` 非空、`base_unit` 必须是 `unit_scale` 键、每个 `field_code` 有 `raw_names` 且 `unit` 是 `unit_scale` 键）；构建 `raw_name → FieldDef` 索引，**同一 raw_name 被两个 field_code 认领即报错**。`LookupRaw` 的 miss 是合法答案（调用方须丢弃并 warn），**不存在回退映射**。
- `equitydeep.go` —— 契约字段常量、`ToTsCode`、`parseContractDate`、闭集后缀表、`parseNumber`。
  - `ToTsCode`：6/9 开头 → `.SH`，其余 → `.SZ`；已带后缀则校验 SH/SZ/BJ（镜像 `pkg/data/source/hkex/fetcher.go` 的 `normalizeSymbol`）。
  - `parseNumber`：**剥后缀 → 去 `,` 千分位 → `ParseFloat`**。后缀表是**编译期闭集**（`亿元`/`亿` → `CNY_100m`，`万元`/`万` → `CNY_10k`），按**长度降序**匹配以避免 `亿元` 被 `亿` 抢先命中。
- `snapshot.go` —— `Snapshot` / `Normalizer` / `DroppedField` / `Result`。
  - `DecodeSnapshot`：`UseNumber` + `DisallowUnknownFields` + `ensureEOF` —— **契约外的键直接拒绝**，避免上游悄悄加字段而我们悄悄忽略。
  - `Normalize`：校验 `ticker`/`report_period`/`ann_date` 为必需；`unit` 缺省取 `dict.BaseUnit`；输出按 `raw_name` **排序**（确定性）；白名单外或解析失败 → 记 `Dropped{line, raw_name, reason}`，**不中断整批**。
  - `convert`：`nil → nil`（保留「源侧无读数」与 0 的区别）；数值类型按 `ScaleFor` 换算；字符串先试**自声明后缀**（量级优先），再按快照 `unit` 换算。

#### 1.2 落库与读取 `pkg/storage/fundamentals_detail.go`

- `SaveFundamentalsDetailBatch`：单事务 + `pgx.Batch` + `ON CONFLICT (ts_code, end_date, ann_date, field_code, fetched_at) DO UPDATE`。主键含 `fetched_at` ⇒ **重述是新行而非覆盖，历史不丢**；同快照重放幂等。
- `GetFundamentalsDetailAsOf`：`WHERE ann_date <= $1 AND field_code = ANY($2)` —— **PIT 过滤在读取侧强制**；内层 `DISTINCT ON (ts_code, end_date, ann_date, field_code) ... ORDER BY fetched_at DESC` 保证重述只贡献一个值且结果确定。
- `value` 是**唯一可空列**：`nil` 存 NULL，`*float64` 让「源侧缺读数」与「真值 0」永远可区分。

#### 1.3 HTTP 写门 `cmd/data/handlers_equitydeep_ingest.go`

- 端点 `POST /api/ingest/equitydeep`，`application/x-ndjson`，`content_hash` 为 **query 必需参数**（`^[0-9a-f]{64}$`）。
- **溯源门**：`content_hash` 必须命中 `ingest.raw`（`store.GetRawIngest`），未命中 → **400**。归一化行由此获得 `snapshot_uri = "ingest.raw:" + contentHash`，**每个数字都能回到唯一原始响应**。
- **词典缺失 → 503**（`dict == nil`）：宁可拒绝服务，也不猜 `raw_name → field_code` 或单位量级。
- body 上限 32 MiB（`http.MaxBytesReader`）+ 单行上限 1 MiB（`bufio.Scanner`）；逐行 `DecodeSnapshot` → `Normalize`；任何解码错误 → 400；成功后 `SaveFundamentalsDetailBatch` → **201**，返回 `{content_hash, snapshots, rows, dropped}`。

### 2. C-4 五个纵向基本面因子（`pkg/data/factor_equitydeep.go`）

命名逐字对齐 RESEARCH §3.4，最长 24 字符：

| 因子 | 口径 |
|---|---|
| `gross_margin_trend` | 最近 4 期**单季**毛利率的 OLS 斜率：`margin(q) = (单季营收 − 单季成本) / 单季营收`，`factor = Slope([margin(q-3) … margin(q)])`（单位：毛利率/季） |
| `contract_liability_ratio` | `合同负债(最新时点) / 营业总收入(TTM)` |
| `ocf_to_net_profit` | `经营现金流(TTM) / 归母净利润(TTM)` |
| `roe_dupont_leverage` | `总资产(最新时点) / 归母净资产(最新时点)`（杜邦权益乘数） |
| `inventory_turnover_delta` | `存货周转率(当期) − 存货周转率(去年同期)`，`存货周转率(p) = 营业成本(TTM at p) / 存货(p)`（**期末余额**，非两点平均） |

支撑机制：

- `statementField` 同时持有 `values` 与 `annDate`：`loadStatementBook` 读取时**跳过 `value == nil` 与非 3/6/9/12 期末行**；同一期出现多行（重述）时**取 `ann_date` 较晚者**（`!r.AnnDate.After(prev)` 即跳过）—— 与 storage 侧的去重互为冗余，保证「同一期一个值」。
- `ttm(p)`：`TTM(Qn,Y) = YTD(Qn,Y) + YTD(Q4,Y−1) − YTD(Qn,Y−1)`；Q4 直接返回本次累计。
- `singleQuarter(p)`：`Q1` 直接返回；否则 `YTD(Qn,Y) − YTD(Q(n−1),Y)`。
- `reportPeriod.order() = year*4 + quarter − 1`，`shiftQuarters` 以此为坐标做跨年位移。
- 落库尾部复用既有范式：`saveVerticalFactor` = `ZScore` + `PercentileRank` + `SaveFactorCacheBatch`，与 `pkg/data/factor.go` 的横截面因子**同一形状**。
- **不可支撑的标的跳过而非近似**：任一所需字段缺失（或不足以构成 4 期单季序列）则该标的该因子不产出，**不用 0 或插值顶替**。

### 3. 迁移 026（列宽放宽）

`docs/migrations/026_widen_factor_name.sql`：`factor_cache` / `factor_returns` / `ic_analysis` 三表 `factor_name` 同源列 `VARCHAR(20)` → `VARCHAR(32)`。Postgres 加宽 varchar 为 **metadata-only** 变更，不重写表、无锁表风险；ALTER 幂等。实际执行路径为 `pkg/storage/postgres.go` 内联 `migrate()`，SQL 文件为同源文档副本；历史副本（006/008/024）不改写。

---

## Consequences

### 正面

- **EQD-P1-2 出口判据达成**：`fundamentals_detail` 首次拥有真实写入方**与**读取方；`ingest.raw` 的 `content_hash` 成为上游快照与下游数字之间的**唯一连接键**。
- **「不猜」被结构性地保证**：白名单 miss = 丢弃，后缀表外 = 丢弃，词典缺失 = 503。三处都没有「猜一下」的分支 —— 数量级错误的可能性被设计消除，而非靠人工检查。
- **PIT 无法被误用**：过滤条件写在 storage 查询里而不是调用方，任何未来因子只要走 `GetFundamentalsDetailAsOf` 就自动获得 `ann_date <= asOf` 语义。
- **重述可追溯**：主键含 `fetched_at` + `snapshot_uri` 回指 `ingest.raw` ⇒ 「这个数字当时是多少、来自哪份响应」可回答。
- **纯包可测**：`pkg/data/equitydeep/` 不依赖 HTTP / DB / 配置，规则可在毫秒级单测中验证。
- **零存量数据破坏**：唯一 DDL 是列宽放宽，无数据迁移、无表重建。

### 负面 / 代价

- `factor_name` 列宽放宽属**存量表改动**（首次打破 ODR-050/053 的「仅新增不改存量表」序列），活跃表数不变（39）但 DDL 语义不再纯增量。
- 新增 HTTP 端点 + 新依赖（`gopkg.in/yaml.v3` 已在依赖树内，无新第三方模块）。
- 端点无鉴权，与 data 服务其余端点同级（当前内网）。
- `pkg/data/equitydeep/` **无单元测试文件**，其规则仅经由 `pkg/data` 的因子测试间接覆盖（见未做项）。
- DB-backed 用例在当前环境（Docker 未运行）SKIP → `POST /api/ingest/equitydeep` 的**真实写入路径未被端到端验证**。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 后缀闭集表跟不上上游文案变化（新增「千万」「亿港元」等） | 中 | 表外一律丢弃 + warn ⇒ 表现为**数据缺失**而非**错误数据**；扩充表是加一行常量，风险局限于完备性 |
| 无 `ts_code` 维度的读取过滤：`GetFundamentalsDetailAsOf` 按 `field_code` 批量取全市场 | 中 | 当前调用方按 `ts_code` 分组消费；标的多时内存与查询成本线性增长，横截面 v2（阶段 P5）须重新评估 |
| 端点无鉴权，任何可达 data 服务者均可写入 `fundamentals_detail` | 中 | 与 data 服务其余端点同级；对外开放前须纳入统一鉴权 |
| 词典路径靠相对目录探测（`./contracts` → `../contracts` → `../../contracts`），工作目录异常时会静默退化为 503 | 低 | 端点显式返回 503 并有 warn 日志；可用 `equitydeep.field_dictionary` 配置绝对路径固定 |
| 5 因子口径为**实现自定**（RESEARCH §3.4 原文只有因子名与业务含义，无公式） | 中 | 公式已回写 RESEARCH §3.4 并锁定，后续变更须走新 ODR；`inventory_turnover_delta` 采用**期末余额**而非两点平均，与部分教材口径不同，属已声明的选择 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `pkg/data/equitydeep/dictionary.go` | `Dictionary` / `FieldDef` / `LoadDictionary` / `ParseDictionary` / `LookupRaw` / `ScaleFor` / `FieldCodes` + 结构校验 |
| **新建** | `pkg/data/equitydeep/equitydeep.go` | 契约字段常量 / `ToTsCode` / `parseContractDate` / 闭集后缀表 / `parseNumber` |
| **新建** | `pkg/data/equitydeep/snapshot.go` | `Snapshot` / `DecodeSnapshot` / `Normalizer.Normalize` / `DroppedField` / `Result` |
| **新建** | `pkg/data/factor_equitydeep.go` | `statementField`（`ttm`/`singleQuarter`）、`loadStatementBook`、`saveVerticalFactor` + 5 个 `Compute*Factor` |
| **新建** | `pkg/data/factor_equitydeep_test.go` | 16 个 `TestXxx`（口语义 / TTM / 单季 / 重述忽略 / 5 因子 / 顺序并行一致 / 错误传播） |
| **新建** | `pkg/domain/fundamentals_detail.go` | `FundamentalsDetailRow`（`Value *float64` = 唯一可空列） |
| **新建** | `pkg/storage/fundamentals_detail.go` | `SaveFundamentalsDetailBatch` + `GetFundamentalsDetailAsOf`（PIT 强制在读取侧） |
| **新建** | `cmd/data/handlers_equitydeep_ingest.go` | `equityDeepIngestHandler` + `isContentHash` + 32 MiB / 1 MiB 上限 + 溯源门 |
| **新建** | `docs/migrations/026_widen_factor_name.sql` | 三表 `factor_name` → `VARCHAR(32)` |
| **修改** | `pkg/domain/factor.go` | 新增 5 个 `FactorType` 常量 |
| **修改** | `pkg/data/factor.go` | 与纵向因子共用落库尾部（`-39` 行净减，抽取复用） |
| **修改** | `cmd/data/handlers_factor.go` | 因子计算 handler 接入 5 个纵向因子 |
| **修改** | `cmd/analysis/handlers_factor.go` | analysis 侧因子入口同步 |
| **修改** | `cmd/data/main.go` | 注册 `POST /api/ingest/equitydeep` |
| **修改** | `cmd/data/setup.go` | `buildEquityDeepDictionary`（配置项 `equitydeep.field_dictionary` + 相对路径探测；缺失时端点 503） |
| **修改** | `cmd/data/setup_test.go` | 结构不变量守护清单纳入新文件 |
| **修改** | `pkg/storage/postgres.go` | 内联 `migrate()` 纳入迁移 026 |
| **修改** | `docs/archive/RESEARCH-equitydeep-legacy.md` | §3.4 新增「因子口径」段 + ETL 路径改为 HTTP 写门 + §3.7 C-3/C-4 落点修正 |
| **修改** | `docs/TASKS.md` | EQD-P1-2  → ✅；版本 3.29.0 → 3.30.0；统计 9/0/8 → 8/0/9；迁移编号脚注 |
| **修改** | `docs/SPEC.md` | Evidence API 入口表新增 `/api/ingest/equitydeep` + 约束 |
| **修改** | `docs/openapi.yaml` | 新增 `POST /api/ingest/equitydeep` + `EquityDeepIngestResponse` + `EquityDeepDroppedField` |
| **修改** | `docs/ADR.md` | ODR 索引 54 → 55；index 3.10.1 → 3.11.0 |
| **新建** | `docs/archive/odr/odr-055-eqd-p1-2-vertical-factors.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 提交 | 1 个 atomic commit `c37e322` — 17 files changed, 2424 insertions(+), 39 deletions(-) |
| 新建文件 | 9（8 源码 + 1 迁移 SQL）+ 本 ODR |
| 修改文件 | 8（源码 6 + 文档 2） |
| 新增表 | 0；存量表改动 **1 类**（3 张表的 `factor_name` 列宽；活跃表数仍 39） |
| 新增端点 | 1（`POST /api/ingest/equitydeep`） |
| 新增因子 | 5 |
| 新增测试 | 16 个 `TestXxx`（`pkg/data/factor_equitydeep_test.go`） |
| `factor_equitydeep.go` 函数级覆盖率 | 91.7% ~ 100%（`loadStatementBook` 95.8% / `saveVerticalFactor` 91.7% / 5 个 `Compute*` 92.3%~100%） |
| `pkg/data` 包覆盖率 | 32.1%（包级；本次改动前基线未单独记录） |
| `go build ./...` | 通过（exit 0） |
| `go test ./pkg/data/ ./pkg/storage/ ./cmd/data/` | 通过（DB-backed 用例按约定 SKIP） |
| `go vet` / `gofmt` | 通过（`gofmt` 报错经 CRLF→LF 副本判定，确认为行尾误报） |
| 任务增减 | 0（EQD-P1-2 由 ⬜ → ✅，总计 233 不变） |
| 阶段 P3 进度 | 2/3（EQD-P1-1 ✅ + EQD-P1-2 ✅；余 EQD-P3-1） |
| ADR 累计 | 22 |
| ODR 累计 | 55 |

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| `pkg/data/equitydeep/` 纯包单元测试 | 本次规则由 `pkg/data` 因子测试间接覆盖；纯包覆盖率当前为 0%，须补（最高优先的遗留项） |
| `POST /api/ingest/equitydeep` 端到端验证 | 需 Docker + PG；当前环境 DB-backed 用例 SKIP |
| `fundamentals_detail` 的 `ts_code` 维度读取过滤 | 当前按 `field_code` 批量取全市场；横截面 v2（阶段 P5）重评 |
| 端点鉴权 | 与 data 服务其余端点同为内网无鉴权，须统一处理 |
| EQD-P3-1 `fundamentals` / `stock_fundamentals` 表重叠收口 | 独立任务，迁移 025 预留未建 |
| 飞轮打通（`research.profile` MCP / vault 挂载） | 阶段 P4 |

---

## Lessons Learned

1. **「有表」不等于「有链」** —— `fundamentals_detail` 在 ODR-053 之后具备完整的表结构、PIT 列与溯源列，但写入方与读取方都是 0。**表落地只完成了契约的一半；没有写入方与读取方的表，和不存在没有区别** —— 任务拆分时应显式区分「表落地」与「链落地」。
2. **列宽是契约的一部分** —— 因子名最长 24 字符，而 `factor_name` 是 `VARCHAR(20)`：命名方案与存储宽度之间没有任何校验，冲突要到**写入那一刻**才暴露。**领域枚举新增命名时，必须同时检查所有落库列宽**；且同名列分布在 3 张表，只改一张会让 IC 链路在运行期失败。
3. **「不猜」要写成结构性保证，而不是写成注释** —— 白名单 miss、后缀表外、词典缺失三条路径分别对应丢弃、丢弃、503。**只要存在一个「尽力猜一下」的分支，整条链的数据可信度就取决于那个分支的正确性**。
4. **PIT 过滤放在读取侧才守得住** —— 把 `ann_date <= asOf` 写进 storage 查询而不是留给调用方，未来任何新因子只要走这个函数就自动正确。**约束写在唯一入口上，比写在文档里有效**。
5. **归一化必须与传输解耦** —— 虽然裁决改为 HTTP 写入口，但解析/归一化仍留在不依赖 HTTP 的纯包里。**入口形态会变，规则不该跟着变**；这也是纯包能被毫秒级单测覆盖的前提。
6. **上游文案的形态必须当成开放集合处理** —— 字符串数值（`1,278.54亿`）不是「脏数据」，而是上游的合法表达。对开放集合的正确做法是**闭集匹配 + 表外丢弃**，而不是写一个「看起来能覆盖」的正则去猜量级。
7. **环境故障要留下确定性复现与绕过方式** —— 本仓 git 写对象失败（`Permission denied`）在 6 次重试、排除 icacls/lock/GIT_DIR/alternates、并在临时仓库复现成功后被判定为 `createObject` 模式相关问题，`git -c core.createObject=link` 稳定绕过。**把绕过方式写进 ODR，比下次重新排查便宜**。

---

_本记录为 2026-09-15 阶段 P3 EQD-P1-2 落地操作。阶段 P3 剩余 EQD-P3-1（表重叠收口）、阶段 P2/P4/P5 的实施情况应记入新的 ODR，不在本文件追加。_