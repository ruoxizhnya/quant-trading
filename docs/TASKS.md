---
status: active
last-verified: 2026-09-17
verified-by: 代码审查（2026-09-16）+ 产品重构讨论；P0-4 落地复核（2026-09-17）
---

# TASKS — 未完成项

> **本文件只放未完成项。** 完成即删除（历史见 `archive/TASKS-history.md`，217KB）。
> 上限 5 页，超出说明该拆项目了。

---

## 当前方向

产品定位已重构为：**AI 实验员操作确定性底座做研究，验证器（被调用，不自主循环）独立证伪，人在异步审阅台上看「质疑清单 + 路径形状」，只对校准后的概率下注。**

三条主线，优先级即下列顺序：

1. **止血** — 让现有回测数字变得可信（P0）
2. **通回路** — 让 AI 真正能操作底座跑完一次循环（P0/P1）
3. **加数据** — 宏观/跨境、产业链（P1/P2）

### 已完成（2026-09-16，已从下方列表移出）

- **P0-1 前视偏差**：财务读取改为按 `COALESCE(ann_date, trade_date)` 过滤
  （`GetFundamentalsSnapshot` / `GetFundamentals`）；回归测试见
  `pkg/storage/fundamentals_pit_test.go`（4 例，红→绿已验证）。
  顺带修掉两个扫描崩溃：`ann_date` 为 NULL 时报错、数值列为 NULL 时报错。
- **P0-3 放开测试**：`.gitignore` 的 `*_test.go` 规则已移除，新测试可正常入库。
  副作用：立刻暴露出此前被隐藏的 `cmd/analysis/handlers_proxy_test.go`。
- **P0-2 建 CI**：`.github/workflows/ci.yml`，四个步骤 —— `go build` / `go vet` /
  `go test ./...`（带 Postgres 服务容器，否则存储层会 skip）/ 文档链接校验。
  顺带修掉全仓基线上的 4 个失败，以及一个真 bug：
  `generateOrderID()` 仅用 `UnixNano()`，Windows 时钟粒度粗导致**连续订单 ID 相撞，
  后一笔静默覆盖前一笔** → 加 atomic 序号（`pkg/live/order_manager.go:258`）。
- **P0-4 鉴权收口**：① 无密钥直接 `Fatal` 拒绝启动（裁决逻辑拆成纯函数
  `decideAuthStartup` 以便测试）；唯一豁免是 `AUTH_INSECURE=true` **且**
  `server.host` 为 loopback —— 非 loopback 时豁免无效。
  ② CORS 从硬编码 `*` 改为按 `server.cors.allowed_origins` 回显，留空即 fail closed；
  analysis / data 两份复制粘贴的中间件收成 `internal/httpserver.CORS`。
  回归测试：`cmd/analysis/auth_bootstrap_test.go`、`internal/httpserver/cors_test.go`，
  两侧各一个装配测试（防"实现改对了但 buildRouter 没接上"）。
  **副作用**：`docker-compose.yml` 的 analysis 服务现在强制要求 `JWT_SECRET`
  （未设则 compose 报错退出）；本地 `go run ./cmd/analysis` 也要带密钥或走豁免，
  见 `guides/local-dev.md`。
- **P0-5 实验链路打通**（根因比原描述深两层，见 [ADR-024](adr/adr-024-expression-as-execution-target.md)）：
  ① `Execute` 在生成 YAML 后**从未把策略注册进 registry**，Stage 5 拿
  `parsedIntent.StrategyName` 回测必然 `strategy not found` —— 补上 `buildAndRegister`；
  ② 更深一层：YAML 生成器只在 intent 自带 `signal_expr` 时才产出 expression 段，
  规则解析出的 intent 只有语义类型（momentum/…），`LoadStrategy` 只认 expression
  → 生成的 YAML 根本加载不成策略。补 `defaultSignalExpression()`：意图类型 →
  确定性默认表达式（价量可表达者）；value / quality 需要 PE/ROE 而表达式引擎
  只有 OHLCV，**明确失败而不是给假数字**。
  ③ LLM 生成的 Go 代码降级为**可审阅 artifact**：编译失败 / LLM 未配置只记录不阻断。
  ④ `Execute` 与 `ExecuteAsync` 的五段逻辑是复制粘贴的两份 → 合并为 `p.run`，
  只修一边就会漏另一边。
- **P0-6 编译校验恒真**：`go tool compile -V=full` 是**打印编译器版本号**的开关，
  根本不读文件，`Compiles` 恒为 true。换成在 module 根下真跑 `go build`；
  找不到 go.mod 时报「无法验证」而非假装通过（fail closed）。

---

## P0 — 正确性（不修，其它一切都是沙上建塔）

**P0-1 ~ P0-6 已于 2026-09-17 全部完成**（明细见上方「已完成」）。
S0 止血阶段的出口判据已满足，见 [ROADMAP](ROADMAP.md)。

---

## P1 — 地基与回路

| # | 任务 | 位置 | 验收 |
|---|---|---|---|
| **P1-1** | **实验日志**：记录 AI 每次尝试（参数向量 / 结果 / 父子关系 / 假设来源） | `experiments` 表 + `pkg/storage/experiments.go` | 能回放一条完整探索路径 |
| **P1-1a** | └ 存储层：表 + 存取（插入 / 收尾 / 单查 / 按 seq 回放） | **✅ 2026-09-17** | 单测 5 例全绿，表已落库 |
| **P1-1b** | └ 生产者：pipeline 每次尝试落一行，**失败也要落**；`cmd/analysis` 启动时注入 sink。⚠️ `UNIQUE(run_id, seq)` 要求 seq 由调用方分配 —— 并发跑时不能让两边各自 +1 | **✅ 2026-09-17** `pkg/ai/pipeline/pipeline.go`、`cmd/analysis/handlers_pipeline.go:46` | 真库端到端取证通过（跑完落 completed，失败落 failed） |
| **P1-1c** | └ 回放：路径可读。单次「试了什么」+ 多步「为什么转到下一步」都已具备（expression / params / hypothesis / 指标 / parent_id） | **✅ 2026-09-17** | 一条 run 从 seq 0 到 seq n 能讲成一个故事 |
| **P1-2** | **循环控制器**：让搜索算法（或 LLM）能连续调用底座 + 响应中断 | **✅ 2026-09-17** `pkg/ai/loop/`（新包；TPE 已复用，遗传仍零调用） | 连续跑 N 次 ✅、中途叫停 ✅、单次失败不中断 ✅、seq 连续 + 父子链 ✅ |
| **P1-2b** | ~~搜了但没生效~~ → **已修**：`defaultSignalExpression` 改为读意图参数 + 开结构化传参通道 `WithParameterOverrides`。⚠️ **搜索空间的参数名必须与意图参数同名**（`lookback_days`）—— 写成 `lookback` 之类别名**不报错、只是静默失效**，整轮退化成同一个策略跑 N 遍 | **✅ 2026-09-17** `pkg/ai/yaml/generator.go`、`pkg/ai/pipeline/pipeline.go`、`pkg/ai/loop/loop.go` | 取证：一轮 6 次窗口各不相同（15/30/33/22/47/35）；另有不依赖库的回归 `TestRun_DifferentParamsProduceDifferentConfig` |
| **P1-3** | **观察页**：三栏（正在试什么 / 结果流 / 当前最优）+ 干预入口 | **✅ 2026-09-17** `web/src/pages/Explore.vue`、`web/src/api/explore.ts`、`cmd/analysis/handlers_explore.go` | 能看见路径形状，能输入方向，能叫停 |
| **P1-4** | **schema 收口**：真实 DDL 硬编码在 Go 里，`migrations/` 无版本管理。**含语义债**：`stock_fundamentals.trade_date` 被两种写入路径复用——fina_indicator 路径存的是报告期截止日，daily_basic 路径存的才是真实交易日（见 P0-1 的 COALESCE 兜底） | `pkg/storage/postgres.go:69-330`、`migration_manager.go:43`（零调用） | migrations 可执行、有版本号、能重放；`trade_date` 语义拆分或改名 |
| **P1-5** | **统一错误中间件**：154 处手写 `gin.H{"error"}`，`c.Error()` 使用 0 次 | `cmd/analysis/setup.go:528-537` | 全局 AppError → HTTP 映射 |
| **P1-6** | **无界 goroutine**：信号量只限并发执行，goroutine 数 = 任务数 | `pkg/backtest/batch/batch.go:173`、`walkforward.go:152` | 固定 worker pool |
| **P1-7** | **N+1 查询**：300 只票 = 600 次 DB 往返 | `pkg/data/factor_attribution.go:89,103` | 改批量查询 |
| **P1-8** | **部署编排三份合一**：compose × 2 + k8s × 8，互相漂移 | `docker-compose*.yml`、`deploy/k8s/` | 一份源 + 生成 |
| **P1-9** | 前端 `/alerts` `/compliance` 缺 `/api` 前缀，dev 下必 404 | `web/src/api/alerts.ts:53`、`compliance.ts:104` | 对齐后端路由 |
| **P1-10** | **`make build` 会失败**：Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录 ODR-021 合并后已不存在 | `Makefile:49-70` | `make build` 通过，或删掉这两个目标 |
| **P1-11** | **`cmd/ai` 无 Dockerfile、不在 `docker-compose.yml`**，只能本地 `go run` | `cmd/ai/`、`docker-compose.yml` | 补 Dockerfile 与 compose 条目，或明确标注为仅本地运行 |
| **P1-12** | **回测的「今天」取自 `time.Now()`**：引擎从不设置 `Portfolio.UpdatedAt`，于是策略回退到 `time.Now()` 判断调仓日（`pkg/strategy/examples/momentum.go:112`）。后果是 weekly / monthly 回测结果**依赖运行当天是星期几** —— 同一份代码周一跑有信号、周二跑零成交，回测不可复现（daily 恒调仓，侥幸不受影响）。取证时实测：weekly 构造下 252 个交易日 0 笔成交 | `pkg/backtest/`（未设 UpdatedAt）、`pkg/strategy/examples/momentum.go:112` | 引擎在推进交易日时把当前回测日期写进 `Portfolio.UpdatedAt`；策略侧不得用 `time.Now()` 作回退 |

---

## P2 — 数据与清理

| # | 任务 | 位置 |
|---|---|---|
| **P2-1** | 补**宏观/跨境数据源**（美股、汇率、利率、大宗）—— 对产业链认知价值最高 | 新建 adapter |
| **P2-2** | 产业链数据底座最小版：`query_supply_chain(name)` | 新建 |
| **P2-3** | 因子加 `hypothesis_source` 字段（因果来源） | `factor_cache` schema |
| **P2-4** | 幸存者偏差：无退市/剔除逻辑，回测 universe 不完整 | `pkg/backtest/engine.go` |
| **P2-5** | 废弃模块清理：`pkg/ai/agents` 标记 DEPRECATED 仍是 `cmd/ai` 主链路 | `cmd/ai/main.go:13` |
| **P2-6** | `drift` 概念漂移检测零调用（孤儿代码）；`get_market_regime` 未进主流程 | `pkg/ai/drift/`、`pkg/tools/builtin/` |
| **P2-7** | 死配置 `config/ai-service.yaml` 从未被读取；`docker-compose.services.yml` 引用不存在的 Dockerfile | config/、deploy/ |
| **P2-8** | 幸存的前视风险复核：复权口径无 hfq 对照 | migrations |
| **P2-9** | **验证器链**：5 个确定性校验器 + 1 个因果审查。被调用、不自主循环；输出**概率估计 + 质疑清单**，不是通过/不通过（决策权在人） | `pkg/validation/`（新包） | 输入提案 + 实验日志 → 输出质疑清单 + 概率估计 |
| **P2-9a** | └ **统计**：多重检验校正（试了 N 次，门槛按 N 收紧）。吃 P1-1 的实验日志 | **✅ 2026-09-17** `pkg/validation/statistical.go` | 同样 Sharpe，试 500 次必须比试 5 次更不可信 |
| **P2-9b** | └ **经济**：扣费后净收益 | **✅ 2026-09-17** `pkg/validation/economic.go` + `turnover.go` | 毛收益扣掉手续费 / 印花税 / 过户费 / 冲击成本后仍成立；附盈亏平衡换手率 |
| **P2-9c** | └ **稳健**：参数敏感度、分年度、分市值 / 行业 | 待办 | 邻域参数不能塌 |
| **P2-9d** | └ **偏差**：前视、幸存者、复权口径 | 待办 | 与 P0-1 / P2-4 / P2-8 的已知债联动 |
| **P2-9e** | └ **冗余**：与已有策略相关性（防「伪分散」） | 待办 | 相关性过高要质疑 |
| **P2-9f** | └ **因果**（L3，需 LLM）：讲得出为什么吗 | 待办 | 六维里唯一需要语言模型的一维 |
| **P2-10** | `domain.Fundamental` 数值字段是 `float64`，而表中列可为空。P0-1 中用 `COALESCE(col,0)` 兜底，导致**缺失值被当作 0 而非"未知"**（PE=0 会被误判为极便宜） | `pkg/domain/market/types.go:69` | 改为 `*float64`，或让因子层显式跳过缺失值 |
| **P2-11** | Hermes Agent 系统设计文档遗失（原在 `.trae/documents/`，目录已删）。SPEC §6 与 hermes 验收测试均引用它 | `docs/hermes/` | 补写设计文档，或在引用处说明以配置为准 |
| **P2-12** | **表达式引擎只暴露 OHLCV**（open/high/low/close/volume/turnover），因此 `value` / `quality` 类意图表达不出 —— P0-5 中它们只能明确失败，而不是套一个无关的价格表达式产出误导性回测数字 | `pkg/strategy/expression/data_provider.go:88` | 把 PE / PB / ROE 等基本面列接入表达式引擎，这两类意图才能执行 |
| **P2-13** | **验证器链缺真实回测的端到端取证**（2026-09-17 已解决一半）。缺口只剩数据：本地库 `stocks` / `trading_calendar` / `ohlcv_daily_qfq` 均 0 行。~~引擎离线跑不了~~ —— 这是误判，引擎三处 HTTP（仓位 / 择时 / 止损）**都有 in-process 分支**，`cmd/analysis/main.go:160` 也已 `SetRiskManager`；取证时用 `marketdata.NewInMemoryProvider()` + `SetRiskManager` 即可完全离线（范式见 `pkg/validation/economic_integration_test.go`） | `pkg/validation/economic_integration_test.go` | 跑数据同步补齐行情后，用同一范式接真库 |

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
