---
status: evergreen
last-verified: 2026-10-12
verified-by: 文档拆仓后重建 core 文档入口（2026-10-12，见 AGENTS.md 尾注 _ARCHITECTURE.md 建文件纳入_）；R1 frontmatter 由 DOCS-02 补齐
---

# Quant Lab — 架构总览

> **定位**：A 股量化研究平台（Quant Lab）的服务拓扑、数据归属与存储真相。
> 顶层产品定义见 [PRODUCT.md](PRODUCT.md)；技术规格（接口/端点/领域模型）见
> [SPEC.md](SPEC.md)；架构决策见 [ADR.md](ADR.md) + `adr/`。
> 本文件是 AGENTS.md §11 导航与 Rule 1 触发器（加表/端口变更）的落地目标。

---

## 1. 服务拓扑

微服务架构，Analysis Service 作为 API 网关协调 Data Service 与 Strategy
Service。**PG 与 Redis 不在容器内**——各服务经 `host.docker.internal` 连
宿主机实例（compose 只编排 4 个 Go 服务；前端已拆仓，见下）。

```
Browser (Vue SPA，quant-trading-ui 仓 :5173 dev / nginx :8080)
  │  只经 /api/* 打后端（AUD-32：nginx 托管在宿主 8080）
  ├──► analysis-service :8085 ──► PostgreSQL :5432（唯一数据面）
  │                             ──► Redis :6379（缓存）
  ├──► ai-service :8086 ────────►（源码在 quant-trading-agent 仓，
  │                               ADR-027 拆仓；compose 经 additional_contexts
  │                               构建；analysis 对 copilot/pipeline/explore/
  │                               tools 四族路由纯反代）
  ├──► data-service :8081 ──────► tushare.pro 摄取（唯一外部数据入口）
  └──► strategy-service :8082 ──►（备用，ADR-012）
```

| 服务 | 端口 | 入口 | 职责 |
|---|---|---|---|
| analysis-service | 8085 | `cmd/analysis` | API 网关 + 回测/batch/walkforward + 风控/执行（in-process，ODR-021 合并）+ 信号/证据/产业链只读门 |
| data-service | 8081 | `cmd/data` | tushare 摄取、同步任务队列（sync jobs/SSE）、因子计算 |
| strategy-service | 8082 | `cmd/strategy` | 备用（ADR-012，未激活） |
| ai-service | 8086 | agent 仓 | Hermes AI 实验员（L3 编排层，源码不在本仓） |

> **AI 拆仓（ADR-027，2026-10-09）**：`pkg/ai` + CopilotService + cmd/ai 物理
> 迁入 `quant-trading-agent`（独立 module，单向依赖 agent→core）。core 不许
> import `pkg/ai`——`internal/repoguard/ai_boundary_test.go` 白名单护栏钉住。

---

## 2. 数据归属（ADR-023，禁止双写）

可重建数据物理唯一于 PG；不可重建的研究叙事唯一于 vault markdown。共享靠
同一份数据 + `content_hash` 证据坐标，不靠复制。

| 分区 | 内容 | 写入方 |
|---|---|---|
| A 原始源响应 `ingest.raw` | 外部响应的逐字节归档（class A） | data-service 摄取时自动归档（`TushareClient.call`）；`POST /api/ingest/raw` 是唯一外部写入门 |
| B 规范化行情 `public`（`stocks`/`ohlcv_daily_qfq`/`stock_fundamentals`/`trading_calendar`/`splits`/`dividends`/`index_constituents` 等） | 可从 A 重建的规范化数据 | data-service 摄取 |
| C 派生计算 `quant.*` + Redis `factor_cache` | 因子/回测/IC/信号/产业链静态层/实验日志 | 各计算路径（可 DROP 重建） |
| E 研究结构化投影 `research.*` | vault markdown 的可重建投影 | 投影写入（可 DROP 重建） |

---

## 3. 数据库 schema —— 唯一真相在 `migrate()`

**加表 / 加列的唯一路径**：`pkg/storage/postgres.go` 的 `migrate()`（数组构
造在 `migrationStatements()`，每条幂等，编号续最大）。`migrations/` 与
`docs/migrations/` 下的 `.sql` **不被执行**（golang-migrate 封装
`MigrationManager` 已于 2026-09-18 删除），只作历史镜像——新增迁移按 K8 先例
在内联 DDL 追加包级变量（如 `SupplyChainSchemaDDL`）+ 建 `docs/migrations/`
镜像副本，两侧一致性由 `pkg/storage` / `pkg/strategy` 的归一化空白比对测试钉住。

**表数与表清单以源码为准，不要引用任何文档里的数字**（AGENTS.md §11 口径）：

```bash
# 内联 DDL 建表数（当前 42 张，2026-10-12 实测：K8 的 external_signals +
# P2-2 产业链四表并入后）
grep -c "CREATE TABLE IF NOT EXISTS" pkg/storage/postgres.go
# 建 schema 清单
grep -n "CREATE SCHEMA IF NOT EXISTS" pkg/storage/postgres.go
```

分区结构：

- `public`：规范化市场数据（ stocks / ohlcv_daily_qfq【唯一 hypertable，migrate 内 `create_hypertable`】/ stock_fundamentals / trading_calendar / splits / dividends / index_constituents / factor_cache / factor_returns / ic_analysis / backtest_jobs / sync_jobs / sync_schedules / strategies / users / audit_logs 等）
- `ingest.raw`：类 A 原始响应归档（唯一键 `content_hash`）
- `quant.*`：派生计算。信号面 `external_signals`（K8，Migration 048）；产业链静态层 `sw_industry_tree` / `supply_chain_edge` / `industry_vitality` / `etf_constituent`（P2-2，Migration 049，统一 `source`/`as_of` 溯源列 + 幂等唯一约束）；内核模块表 `strategy_state` 等由各模块 store 的 `EnsureSchema` 幂等兜底（未并入 migrate 的以模块注释为准）
- `research.*`：研究投影（conclusion / question / profile）

> ⚠️ **已知过时（登记，未修）**：[SPEC.md](SPEC.md) 的「TimescaleDB Schema」节
> 手抄 DDL 已与代码脱节（`ohlcv_daily` 实为 `ohlcv_daily_qfq`；`factor_cache`
> 的 hypertable 调用已按注释刻意省略）。以本节口径与 `migrate()` 源码为准；
> 该节的修正属独立文档债，不在任何进行中 task 的范围。

### TimescaleDB

仅 `ohlcv_daily_qfq` 建 hypertable（`migrate()` 内 `create_hypertable(...,
if_not_exists => TRUE)`，TimescaleDB 不可用时 Warn 降级不阻断启动）。
`factor_cache` 等时序表按 022 迁移注释**刻意不建** hypertable（需要时单独加）。

---

## 4. 缓存设计（Redis :6379）

`pkg/storage/cache.go`（`storage.Cache` 接口，`RedisCache` 实现）：

| 用途 | 说明 |
|---|---|
| `factor_cache` | 因子计算缓存（PG 表 + Redis 双层；Redis miss 回表） |
| session store | JWT 会话（auth） |
| `sync_job_cache` | data-service 同步任务状态热数据 |
| `DataCache` | data-service 读路径缓存装饰（stocks 等热点查询） |

Redis 不可用时服务不启动即失败（连接在装配期建立）——缓存是必需依赖，
不是可选装饰。

---

## 5. 目录结构（本仓）

```
quant-trading/
├── cmd/analysis/     # 主服务 :8085（API 网关 + 风控/执行 in-process）
├── cmd/data/         # 数据服务 :8081（tushare 摄取 + sync 队列）
├── cmd/strategy/     # 策略服务 :8082（备用）
├── pkg/
│   ├── backtest/     # 回测引擎 + job/batch/walkforward + contracts（RunSpec/ResultSpec 冻结）
│   ├── data/         # 数据管道（tushare 客户端、因子、产业链导入）
│   ├── domain/       # 领域模型（market 子包为行情类型权威）
│   ├── kernel/       # 模块化内核（clock/msgbus/eventstore，K1–K12 线）
│   ├── marketdata/   # 规范层读路径（Router 透明路由 + 1m→15m/1d 聚合，ADR-031 D1）
│   ├── live/         # 实盘/纸交易引擎
│   ├── risk/         # 风控（in-process 并入 analysis）
│   ├── storage/      # PG/Redis 存储——DDL 唯一真相在 postgres.go
│   ├── strategy/     # 策略框架 + 插件 + 表达式 + SignalProvider/信号面
│   └── validation/   # 验证器链（五维纯计算）
├── web/              # Vue 3 前端——⚠️ 已拆仓至 quant-trading-ui（2026-10-09），本目录历史遗留
├── docs/             # 文档（SPEC/ADR/ODR/TASKS/migrations 镜像）
├── contracts/        # 内核模块契约（schema.sql 等）
├── e2e/              # Playwright E2E 历史遗留（已拆仓，真相源在 quant-trading-ui/e2e；AUD-61 测试债工单亦迁至该仓 docs/TASKS.md）
└── tools/            # 运维脚本（不被 CI 执行）
```

---

## 6. 端口速查

| 端口 | 归属 |
|---|---|
| 5432 / 6379 | PostgreSQL / Redis（宿主机） |
| 8081 / 8082 | data-service / strategy-service |
| 8085 | analysis-service（API 网关） |
| 8086 | ai-service（agent 仓构建） |
| 8080 / 5173 | 前端 nginx（宿主）/ Vite dev（quant-trading-ui 仓） |

---

## 7. 规范层数据流（ADR-031 / D1，2026-10-14）

**不变量**：全系统数据取用只认一套规范层 schema——三类字段（OHLCV /
Fundamental / FactorOutput），每条记录**自带频率与可见时间点**；
connector 之外无人感知底层表。字段表 / 频率词表 / 聚合口径 / 时间语义
的文档真相在 [SPEC.md「规范层 Schema」](SPEC.md#规范层-schemacanonical-layer)，
本节只画数据流与护栏边界。

```
                     ┌──────────────────────────────┐
                     │ 物理表（迁移期共存，登记基线）  │
                     │  ohlcv_daily_qfq（1d，qfq）    │
                     │  ohlcv_minute    （1m）        │
                     └───────────┬──────────────────┘
                                 │ 唯一被许可直连的读路径
                     ┌───────────▼──────────────────┐
                     │ pkg/marketdata.Router.Bars   │ 声明频率 → 透明路由
                     │  1d → daily 表原样            │ Frequency + VisibleAt
                     │  1m → minute 表原样           │ 随每条 bar 显式声明
                     │  15m → 1m 聚合（不物化）      │ 未知频率 fail-loud
                     └───────────┬──────────────────┘
                                 │ []market.OHLCV（规范层）
              ┌──────────────────┼───────────────────┐
              ▼                  ▼                   ▼
         Go 内核直吃         NT 侧翻译层            遗留路径（基线登记，
      （回测/因子/信号）   sidecar/mapping.py      只减不增，逐项销号）
                          BAR_FIELD_MAP：
                          加数据类 = 映射加一行；
                          export_bars 是第一个
                          消费者（K13 数据流）
```

护栏（机械判据，全部在 CI 里）：

- **直连棘轮**（`internal/repoguard/canonical_layer_test.go`）：非测试
  Go 文件里的 `ohlcv_daily_qfq` / `ohlcv_minute` 字符串字面量（SQL /
  DDL / 表名常量），只允许 `pkg/marketdata` 与 `directTableAccessBaseline`
  登记的遗留文件；**新开直连 = 红，基线文件被清理 = 逼删条目**
  （只减不增，白名单不许替已消失的违规打掩护）。
- **防漂移**（`pkg/domain/spec_drift_test.go`）：SPEC 三类字段表 +
  频率词表 ↔ Go 类型反射比对（双向）；Python `FREQ_*` ↔ Go `Freq*`
  逐字钉住（`nt-sidecar/tests/test_d1_mapping.py` 读 Go 源码）。
- **回归红线**（`pkg/marketdata/router_integration_test.go`）：Router 的
  1d 路由与遗留 `storage.GetOHLCV` 逐字段对拍（真实 PG 自灌自证，
  DB 不可达 Skip 不 Fail）——改的是取数通道，不是数据。

**上游闸门（ADR-031 / D2，2026-10-14）**：规范层「记录自带可见时间点」
不靠消费方自觉，而是注册期的结构性闸门——connector 注册
（`pkg/data/source.Registry.Register`）时必须声明**时间语义 + 频率口径**，
缺失 / 非法 / 与数据形态矛盾一律 fail-closed 拒收（`validateDeclarations`
三类哨兵 `ErrDeclarationMissing` / `ErrDeclarationInvalid` /
`ErrDeclarationMismatch`）。语义词表唯一真相在 `pkg/domain/market`
（`TimeSemantics` / `FrequencySpec`），存量 connector 的声明登记在
`pkg/data/source/declarations.go`（「声明即登记」）。高危样例：市场活跃度 /
拥挤度 = `after_close`（收盘后可见，盘中用当日值即偷看）。文档真相见
SPEC.md「connector 注册契约」节。

## 8. 因子定义库数据流（ADR-031 / D3，2026-10-14）

**不变量**：因子定义只认「库里公式」——一份种子定义
（`pkg/domain/factor_def_seed.go`，编译期常量）+ 一张持久表
（`factor_definitions`，迁移 051）+ 一个进程内 Registry
（`pkg/data/factor_registry.go`，构造时定稿、只读）。计算线
（`ComputeAllFactors` / `ComputeFactorByName`）的因子清单 =
Registry 投影；**connector 与策略之外无人感知内核分派**。定义
schema / 内核四态 / 版本哈希纪律的文档真相在
[SPEC.md「因子定义 Schema」](SPEC.md#因子定义-schemafactor-definitiond3)，
本节只画数据流与护栏边界。

```
   ┌─────────────────────────────────────────────────────────┐
   │ 定义层（库里公式，唯一权威）                                │
   │  pkg/domain/factor_def_seed.go（种子 11 条，编译期常量）    │
   │    · 字段：名字/公式/域过滤/输入字段/生效日/口径/前视纪律    │
   │    · 版本哈希 = SHA-256(canonical 内容)——改公式=新哈希     │
   └──────────────┬───────────────────────────┬──────────────┘
                  │ SyncFactorDefinitions     │ NewRegistry（启动装载，
                  │ （幂等 upsert）            │ 全量 fail-closed 校验）
                  ▼                           ▼
   ┌──────────────────────────┐   ┌──────────────────────────┐
   │ factor_definitions 表     │   │ data.Registry（只读快照） │
   │ （迁移 051；加载时重新     │   │  · Definition(name)       │
   │  derive 哈希比对，不符     │   │  · CodeKernel 绑定表      │
   │  即拒载——fail-closed）   │   │  · FailClosedError 带清单 │
   └──────────────────────────┘   └────────────┬─────────────┘
                                               │ 计算分派（按名/按批）
              ┌────────────────────┬───────────┴────────────┐
              ▼                    ▼                        ▼
        leaf / formula          code                    pending
      表达式引擎现算         Go 出口闭包            不计算，fail-closed
      （pkg/expression      （CodeRef →             （ErrFactorKernel
       Formula+Validity，    Compute*Factor）        Undefined，显式
       域外即缺失）          纵向 5 因子               进 Skipped 报告）
              └────────────────────┴───────────┬────────────┘
                                               ▼
                              factor_cache（PG）+ Redis 双层
                              产出形状 = D1 FactorOutput（缺失≠0）
```

护栏（机械判据，全部在 CI 里）：

- **注册表棘轮**（`internal/repoguard/factor_registry_test.go`）：
  非测试 Go 文件引用 `Compute*Factor` 内核方法（定义/绑定三文件除外）
  = 绕过 Registry 直调内核 → 红；同一 composite literal 内聚集 ≥3 个
  名册常量 = 库外重建清单 → 红。基线带裁决理由，只减不增。
- **数值回归红线**（`pkg/data/factor_formula_test.go`）：公式库版 vs
  原代码版同输入同输出（momentum/value/quality 三列全等，域过滤与
  历史不足的缺失语义由 Validity / NaN 复现）。
- **防漂移**（`pkg/domain/spec_drift_test.go` + 种子词表测试）：
  SPEC 定义骨架表 ↔ `FactorDefinition` 反射比对；种子字段码 ↔
  `equitydeep.FieldX` 契约常量逐一比对。
- **版本可复现**（`pkg/data/factor_registry_test.go` +
  `pkg/storage/factor_definition_test.go`）：同种子同哈希；
  库里公式被绕过写入路径改坏 → 加载期哈希比对拦住。

迁移方向：size / volatility / growth 三因子（pending）补计算内核；
回测预热清单（`pkg/backtest/cache/factor_cache.go`，基线登记）收敛为
从 Registry 读取（D6）；D4 因子治理细则（登记人/审计/生效日流程）
在本 Schema 上叠加，不改形状。

---

迁移方向：connector 契约已接入（D2 完成，`validateDeclarations` fail-closed
闸门生效）→ 消费方逐个切 Router → 棘轮基线逐条销号 → 物理表直连归零。

---

_Last updated: 2026-10-14（§7 增补 connector 注册契约上游闸门 ADR-031 D2；§7 规范层数据流 ADR-031 D1 + §8 因子定义库数据流 ADR-031 D3；表数口径 42 张为 2026-10-12 实测）_
_维护规则：服务/端口/schema 分区变更时更新本文件（AGENTS.md Rule 1）；表数不手抄，只给 grep 口径。_
