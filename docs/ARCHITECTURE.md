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

_Last updated: 2026-10-12（建文件；表数口径 42 张为当日实测）_
_维护规则：服务/端口/schema 分区变更时更新本文件（AGENTS.md Rule 1）；表数不手抄，只给 grep 口径。_
